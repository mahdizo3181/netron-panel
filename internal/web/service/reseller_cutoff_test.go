package service

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// recordingRuntime notes which users a core gained or lost; nothing reaches Xray.
type recordingRuntime struct {
	fakeNodeRuntime
	mu      sync.Mutex
	removed []string
	added   []string
}

func (r *recordingRuntime) RemoveUser(_ context.Context, _ *model.Inbound, email string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, email)
	return nil
}

func (r *recordingRuntime) AddUser(_ context.Context, _ *model.Inbound, user map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email, _ := user["email"].(string)
	r.added = append(r.added, email)
	return nil
}

func (r *recordingRuntime) seen() (removed, added []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Sorted(slices.Values(r.removed)), slices.Sorted(slices.Values(r.added))
}

type cutoffFixture struct {
	a, b   int
	local  *recordingRuntime
	node   *fakeNodeRuntime
	nodeIb *model.Inbound
	ib     *model.Inbound
}

func setEnforce(t *testing.T, on bool) {
	t.Helper()
	if err := (&SettingService{}).SetResellerPoolEnforce(on); err != nil {
		t.Fatal(err)
	}
}

func seedClients(t *testing.T, ib *model.Inbound, clients []model.Client) {
	t.Helper()
	db := database.GetDB()
	if err := db.Model(ib).Update("settings", clientsSettings(t, clients)).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ClientService{}).SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatal(err)
	}
	for i := range clients {
		if err := (&InboundService{}).AddClientStat(db, ib.Id, &clients[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func vlessClient(email string, enable bool) model.Client {
	return model.Client{ID: uuid.NewString(), Email: email, Enable: enable}
}

// newCutoffFixture: shop-a (3 GB pool) owns a1, exp-a, man-a, off-a (disabled by
// hand) and the node-hosted n-a; shop-b owns b1; adm has no owner.
func newCutoffFixture(t *testing.T, enforce bool) *cutoffFixture {
	t.Helper()
	setupPoolDB(t)
	nodeID, node := setupNodeRuntime(t)
	local := &recordingRuntime{}
	runtime.GetManager().SetLocalRuntimeOverride(local)
	ib := mkInbound(t, 26001, model.VLESS, `{"clients":[]}`)
	seedClients(t, ib, []model.Client{
		vlessClient("a1@x", true), vlessClient("off-a@x", false),
		vlessClient("exp-a@x", true), vlessClient("man-a@x", true),
		vlessClient("b1@x", true), vlessClient("adm@x", true),
	})
	nodeIb := nodeInbound(t, nodeID, 26002, nil)
	seedClients(t, nodeIb, []model.Client{vlessClient("n-a@x", true)})

	pa := resellerPayload("shop-a")
	pa.PoolBytes = 3 * gb
	pb := resellerPayload("shop-b")
	pb.PoolBytes = 100 * gb
	f := &cutoffFixture{a: mustCreateReseller(t, pa).Id, b: mustCreateReseller(t, pb).Id, local: local, node: node, nodeIb: nodeIb, ib: ib}
	ownEmails(t, f.a, "a1@x", "off-a@x", "exp-a@x", "man-a@x", "n-a@x")
	ownEmails(t, f.b, "b1@x")
	setEnforce(t, enforce)
	return f
}

type clientState struct{ record, traffic, inSettings, poolDisabled bool }

func stateOf(t *testing.T, email string) clientState {
	t.Helper()
	db := database.GetDB()
	var rec model.ClientRecord
	if err := db.Where("email = ?", email).First(&rec).Error; err != nil {
		t.Fatalf("client %s: %v", email, err)
	}
	st := clientState{record: rec.Enable, traffic: clientRow(t, email).Enable, poolDisabled: rec.PoolDisabled}
	var links []model.ClientInbound
	db.Where("client_id = ?", rec.Id).Find(&links)
	for _, l := range links {
		var ib model.Inbound
		db.First(&ib, l.InboundId)
		var s struct {
			Clients []struct {
				Email  string `json:"email"`
				Enable bool   `json:"enable"`
			} `json:"clients"`
		}
		_ = json.Unmarshal([]byte(ib.Settings), &s)
		for _, c := range s.Clients {
			if c.Email == email {
				st.inSettings = c.Enable
			}
		}
	}
	return st
}

func resellerRow(t *testing.T, id int) model.Reseller {
	t.Helper()
	var r model.Reseller
	if err := database.GetDB().First(&r, id).Error; err != nil {
		t.Fatal(err)
	}
	return r
}

var shopALocal = []string{"a1@x", "exp-a@x", "man-a@x"}

var (
	on     = clientState{record: true, traffic: true, inSettings: true}
	cutOff = clientState{poolDisabled: true}
	off    = clientState{}
)

func expectStates(t *testing.T, when string, want map[string]clientState) {
	t.Helper()
	for email, w := range want {
		if got := stateOf(t, email); got != w {
			t.Errorf("%s: %s = %+v, want %+v", when, email, got, w)
		}
	}
}

func TestCrossingThePoolCutsOffExactlyThatResellersEnabledClients(t *testing.T) {
	f := newCutoffFixture(t, true)
	addTraffic(t, &xray.ClientTraffic{Email: "a1@x", Up: 2 * gb})
	if r := resellerRow(t, f.a); r.CutoffReason != "" {
		t.Fatalf("cut off below the pool: %q", r.CutoffReason)
	}

	addTraffic(t, &xray.ClientTraffic{Email: "a1@x", Up: gb})
	if r := resellerRow(t, f.a); r.CutoffReason != "pool" {
		t.Fatalf("cutoff_reason = %q, want pool in the poll that crossed it", r.CutoffReason)
	}
	expectStates(t, "after the crossing poll", map[string]clientState{
		"a1@x": cutOff, "exp-a@x": cutOff, "man-a@x": cutOff, "n-a@x": cutOff,
		"off-a@x": off, "b1@x": on, "adm@x": on,
	})
	if removed, _ := f.local.seen(); !slices.Equal(removed, shopALocal) {
		t.Errorf("local core removed %v, want %v", removed, shopALocal)
	}
	if f.node.updateInbound.Load() == 0 {
		t.Error("the node never got the updated inbound")
	}
	if r := resellerRow(t, f.b); r.CutoffReason != "" {
		t.Errorf("shop-b cut off too: %q", r.CutoffReason)
	}
}

func TestEnforceOffCountsButCutsNobodyOff(t *testing.T) {
	f := newCutoffFixture(t, false)
	addTraffic(t, &xray.ClientTraffic{Email: "a1@x", Up: 5 * gb})
	r := resellerRow(t, f.a)
	if r.UsedTraffic != 5*gb || r.CutoffReason != "" {
		t.Fatalf("used=%d reason=%q, want 5 GB counted and no cut-off", r.UsedTraffic, r.CutoffReason)
	}
	expectStates(t, "switch off", map[string]clientState{"a1@x": on, "n-a@x": on, "b1@x": on})
	if removed, _ := f.local.seen(); len(removed) != 0 {
		t.Fatalf("local core removed %v with the switch off", removed)
	}
}

func TestExpiredOrDisabledResellerIsCutOffOnTheNextPoll(t *testing.T) {
	for _, tc := range []struct {
		reason string
		update map[string]any
	}{
		{"expired", map[string]any{"expiry_time": time.Now().Add(-time.Minute).UnixMilli()}},
		{"disabled", map[string]any{"enable": false}},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			f := newCutoffFixture(t, true)
			if err := database.GetDB().Model(&model.Reseller{}).Where("id = ?", f.a).Updates(tc.update).Error; err != nil {
				t.Fatal(err)
			}
			addTraffic(t)
			if r := resellerRow(t, f.a); r.CutoffReason != tc.reason {
				t.Fatalf("cutoff_reason = %q, want %q", r.CutoffReason, tc.reason)
			}
			expectStates(t, tc.reason, map[string]clientState{"a1@x": cutOff, "n-a@x": cutOff, "b1@x": on})
		})
	}
}

// Polls are serialised by the traffic writer: two crossing at once cut off once.
func TestTwoConcurrentPollsCrossingThePoolCutOffOnce(t *testing.T) {
	f := newCutoffFixture(t, true)
	startSerializedWriter(t)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { addTraffic(t, &xray.ClientTraffic{Email: "a1@x", Up: 2 * gb}) })
	}
	wg.Wait()
	if r := resellerRow(t, f.a); r.CutoffReason != "pool" || r.UsedTraffic != 4*gb {
		t.Fatalf("reason=%q used=%d, want pool and 4 GB", r.CutoffReason, r.UsedTraffic)
	}
	if removed, _ := f.local.seen(); !slices.Equal(removed, shopALocal) {
		t.Fatalf("local core removed %v, want %v exactly once", removed, shopALocal)
	}
	var cuts int64
	database.GetDB().Model(&model.ResellerAuditLog{}).Where("reseller_id = ? AND action = ?", f.a, "cutoff").Count(&cuts)
	if cuts != 1 {
		t.Fatalf("%d cutoff audit rows, want 1", cuts)
	}
}

// A node renews its own clients; the master must not take a renewal that would
// re-enable a client its reseller may not run, and must re-push its state.
func TestNodeRenewalOfABlockedResellersClientIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enforce bool
		block   func(t *testing.T, resellerID int)
	}{
		{"expired reseller", false, func(t *testing.T, id int) {
			database.GetDB().Model(&model.Reseller{}).Where("id = ?", id).Update("expiry_time", time.Now().Add(-time.Minute).UnixMilli())
		}},
		{"pool cut-off", true, func(t *testing.T, id int) {
			database.GetDB().Model(&model.Reseller{}).Where("id = ?", id).Update("cutoff_reason", "pool")
			database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", "rn@x").Update("pool_disabled", true)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupPoolDB(t)
			db := database.GetDB()
			node := &model.Node{Name: "n1", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
			if err := db.Create(node).Error; err != nil || node.Id != 1 {
				t.Fatalf("node id %d err %v", node.Id, err)
			}
			id := ownedNodeClient(t, "rn@x")
			setEnforce(t, tc.enforce)
			const first = int64(1893456000000)
			nodeTick(t, xray.ClientTraffic{Email: "rn@x", Up: 2 * gb, ExpiryTime: first, Reset: 30, Enable: true})
			tc.block(t, id)
			db.Model(xray.ClientTraffic{}).Where("email = ?", "rn@x").Update("enable", false)
			db.Model(&model.ClientRecord{}).Where("email = ?", "rn@x").Update("enable", false)
			db.Model(&model.Node{}).Where("id = ?", 1).Update("config_dirty", false)

			nodeTick(t, xray.ClientTraffic{Email: "rn@x", Down: gb, ExpiryTime: first + 30*86400000, Reset: 30, Enable: true})
			ct := readTraffic(t, db, "rn@x")
			if ct.Enable || ct.ExpiryTime != first {
				t.Fatalf("master took the node's renewal: enable=%v expiry=%d", ct.Enable, ct.ExpiryTime)
			}
			var n model.Node
			db.First(&n, 1)
			if !n.ConfigDirty {
				t.Fatal("node not marked dirty, so it keeps serving the renewed client")
			}
		})
	}
}
