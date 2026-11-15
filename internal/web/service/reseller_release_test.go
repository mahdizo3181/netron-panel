package service

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

var testAdmin = AuditActor{Kind: "admin", Id: 1, Ip: "203.0.113.9"}

// cutOffFixture crosses shop-a's pool, then exp-a runs out of its own time and
// shop-a disables man-a by hand, both while cut off.
func cutOffFixture(t *testing.T) *cutoffFixture {
	t.Helper()
	f := newCutoffFixture(t, true)
	addTraffic(t, &xray.ClientTraffic{Email: "a1@x", Up: 3 * gb})
	if r := resellerRow(t, f.a); r.CutoffReason != "pool" {
		t.Fatalf("fixture not cut off: %q", r.CutoffReason)
	}
	database.GetDB().Model(xray.ClientTraffic{}).Where("email = ?", "exp-a@x").
		Update("expiry_time", time.Now().Add(-time.Minute).UnixMilli())
	if _, _, err := (&ClientService{}).BulkSetEnableFor(ResellerScope(f.a), &InboundService{}, []string{"man-a@x"}, false); err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, "man-a@x"); st.poolDisabled {
		t.Fatal("a manual disable during the cut-off left the pool flag set")
	}
	return f
}

func nodeCalls(n *fakeNodeRuntime) int32 {
	return n.updateInbound.Load() + n.addClient.Load() + n.updateUser.Load() + n.addInbound.Load()
}

var released = map[string]clientState{
	"a1@x": on, "n-a@x": on, "exp-a@x": off, "man-a@x": off, "off-a@x": off, "b1@x": on, "adm@x": on,
}

func TestReleaseReEnablesOnlyFlaggedClientsWithValidOwnState(t *testing.T) {
	for name, act := range map[string]func(svc *ResellerService, id int) error{
		"add volume":  func(svc *ResellerService, id int) error { return svc.AddVolume(testAdmin, id, 10*gb) },
		"reset usage": func(svc *ResellerService, id int) error { return svc.ResetUsage(testAdmin, id) },
	} {
		t.Run(name, func(t *testing.T) {
			f := cutOffFixture(t)
			svc := &ResellerService{}
			before := nodeCalls(f.node)
			if err := act(svc, f.a); err != nil {
				t.Fatal(err)
			}
			expectStates(t, name, released)
			if r := resellerRow(t, f.a); r.CutoffReason != "" {
				t.Fatalf("cutoff_reason = %q after %s", r.CutoffReason, name)
			}
			if _, added := f.local.seen(); !slices.Equal(added, []string{"a1@x"}) {
				t.Fatalf("local core re-added %v, want [a1@x]", added)
			}
			if nodeCalls(f.node) == before {
				t.Fatal("the node never got n-a back")
			}

			// Idempotent: a second release finds nothing to do.
			if err := svc.releaseResellerCutoff(testAdmin, f.a); err != nil {
				t.Fatal(err)
			}
			if _, added := f.local.seen(); len(added) != 1 {
				t.Fatalf("second release re-added again: %v", added)
			}
		})
	}
}

func TestTopUpThatStillLeavesThePoolUsedUpKeepsTheCutOff(t *testing.T) {
	f := cutOffFixture(t)
	if err := database.GetDB().Model(&model.Reseller{}).Where("id = ?", f.a).Update("used_traffic", 20*gb).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ResellerService{}).AddVolume(testAdmin, f.a, gb); err != nil {
		t.Fatal(err)
	}
	if r := resellerRow(t, f.a); r.CutoffReason != "pool" || r.PoolBytes != 4*gb {
		t.Fatalf("reason=%q pool=%d, want still cut off with 4 GB", r.CutoffReason, r.PoolBytes)
	}
	expectStates(t, "insufficient top-up", map[string]clientState{"a1@x": cutOff, "n-a@x": cutOff})
}

func TestExpiredAndDisabledResellersAreReleasedByExtendAndEnable(t *testing.T) {
	cases := map[string]struct {
		block   map[string]any
		release func(svc *ResellerService, id int) error
	}{
		"expired": {
			map[string]any{"expiry_time": time.Now().Add(-time.Minute).UnixMilli()},
			func(svc *ResellerService, id int) error {
				return svc.ExtendExpiry(testAdmin, id, time.Now().Add(24*time.Hour).UnixMilli())
			},
		},
		"disabled": {
			map[string]any{"enable": false},
			func(svc *ResellerService, id int) error {
				p := resellerPayload("shop-a")
				p.PoolBytes, p.Enable = 3*gb, new(true)
				_, err := svc.UpdateAs(testAdmin, id, p)
				return err
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCutoffFixture(t, true)
			database.GetDB().Model(&model.Reseller{}).Where("id = ?", f.a).Updates(tc.block)
			addTraffic(t)
			if r := resellerRow(t, f.a); r.CutoffReason != name {
				t.Fatalf("cutoff_reason = %q, want %q", r.CutoffReason, name)
			}
			if err := tc.release(&ResellerService{}, f.a); err != nil {
				t.Fatal(err)
			}
			expectStates(t, name+" released", map[string]clientState{"a1@x": on, "n-a@x": on, "off-a@x": off})
			if r := resellerRow(t, f.a); r.CutoffReason != "" {
				t.Fatalf("cutoff_reason = %q after release", r.CutoffReason)
			}
		})
	}
}

// Rollback: turning the switch off releases everyone it cut off.
func TestTurningEnforceOffReleasesCutOffResellers(t *testing.T) {
	f := cutOffFixture(t)
	if err := (&ResellerService{}).SetPoolEnforce(testAdmin, false); err != nil {
		t.Fatal(err)
	}
	expectStates(t, "switch off", released)
	if r := resellerRow(t, f.a); r.CutoffReason != "" {
		t.Fatalf("cutoff_reason = %q after the switch went off", r.CutoffReason)
	}
	if on, _ := (&SettingService{}).GetResellerPoolEnforce(); on {
		t.Fatal("switch still on")
	}
	addTraffic(t, &xray.ClientTraffic{Email: "a1@x", Up: gb})
	expectStates(t, "a poll with the switch off", map[string]clientState{"a1@x": on})
}

func TestAdminDisableAllAndEnableAllClientsOfAReseller(t *testing.T) {
	f := cutOffFixture(t)
	svc := &ResellerService{}
	if _, err := svc.EnableClients(testAdmin, f.a); !errors.Is(err, ErrResellerBlocked) {
		t.Fatalf("enable all while cut off: err = %v, want ErrResellerBlocked", err)
	}
	n, err := svc.DisableClients(testAdmin, f.a)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("disable all touched %d clients, want 5", n)
	}
	// Disable all is a manual intent: a top-up afterwards re-enables nobody.
	if err := svc.AddVolume(testAdmin, f.a, 10*gb); err != nil {
		t.Fatal(err)
	}
	expectStates(t, "after disable all and a top-up", map[string]clientState{"a1@x": off, "n-a@x": off, "b1@x": on})

	if n, err = svc.EnableClients(testAdmin, f.a); err != nil {
		t.Fatal(err)
	}
	// exp-a is past its own expiry, so enabling it would only flap.
	if n != 4 {
		t.Fatalf("enable all enabled %d clients, want 4", n)
	}
	expectStates(t, "after enable all", map[string]clientState{
		"a1@x": on, "n-a@x": on, "man-a@x": on, "off-a@x": on, "exp-a@x": off, "b1@x": on,
	})
}
