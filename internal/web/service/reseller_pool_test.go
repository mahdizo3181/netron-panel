package service

import (
	"fmt"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func setupPoolDB(t *testing.T) {
	t.Helper()
	setupResellerDB(t)
	t.Cleanup(func() { usagemult.SetActive(false) })
}

func ownEmails(t *testing.T, resellerID int, emails ...string) {
	t.Helper()
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email IN ?", emails).
		Update("reseller_id", resellerID).Error; err != nil {
		t.Fatal(err)
	}
}

func poolUsed(t *testing.T, id int) int64 {
	t.Helper()
	var r model.Reseller
	if err := database.GetDB().First(&r, id).Error; err != nil {
		t.Fatal(err)
	}
	return r.UsedTraffic
}

func TestPoolCounterAddsBilledBytesOfOwnedClientsOnly(t *testing.T) {
	setupPoolDB(t)
	usagemult.SetActive(true)
	plain := mkMultInbound(t, 25001, model.VLESS, nil)
	tunnel := mkMultInbound(t, 25002, model.VLESS, f64(1.5))
	attachClient(t, plain, "a@x", 0)
	attachClient(t, tunnel, "a@x", 0)
	attachClient(t, plain, "b@x", 0)
	attachClient(t, plain, "admin@x", 0)
	a := mustCreateReseller(t, resellerPayload("shop-a"))
	b := mustCreateReseller(t, resellerPayload("shop-b"))
	ownEmails(t, a.Id, "a@x")
	ownEmails(t, b.Id, "b@x")

	addTraffic(t,
		statDelta(tunnel, "a@x", gb, gb),
		statDelta(plain, "a@x", 0, gb),
		statDelta(plain, "admin@x", 7*gb, 0),
		// A protocol outside the multiplier reports the bare email and bills at x1.
		&xray.ClientTraffic{Email: "b@x", Up: 2 * gb, Down: 0},
		&xray.ClientTraffic{Email: "nobody@x", Up: 9 * gb},
	)
	if got, want := poolUsed(t, a.Id), 4*gb; got != want {
		t.Fatalf("shop-a pool = %d, want %d (2 GB x1.5 + 1 GB x1)", got, want)
	}
	if ct := clientRow(t, "a@x"); ct.Up+ct.Down != 4*gb {
		t.Fatalf("client row = %d, the pool must move by the same billed bytes", ct.Up+ct.Down)
	}
	if got := poolUsed(t, b.Id); got != 2*gb {
		t.Fatalf("shop-b pool = %d, want %d", got, 2*gb)
	}

	addTraffic(t, statDelta(plain, "a@x", gb, 0))
	if got := poolUsed(t, a.Id); got != 5*gb {
		t.Fatalf("second poll: shop-a pool = %d, want %d", got, 5*gb)
	}
}

// The pool is the reseller's bill: nothing done to its clients may lower it.
func TestPoolCounterNeverDropsOnClientResetRenewOrDelete(t *testing.T) {
	setupPoolDB(t)
	ib := mkMultInbound(t, 25011, model.VLESS, nil)
	for _, e := range []string{"r1@x", "r2@x", "r3@x"} {
		attachClient(t, ib, e, 0)
	}
	// Auto-renew reads the clients from the settings JSON.
	renewing := mkInbound(t, 25012, model.VLESS, `{"clients":[{"email":"r4@x","enable":true}]}`)
	attachClient(t, renewing, "r4@x", 0)
	a := mustCreateReseller(t, resellerPayload("shop-a"))
	ownEmails(t, a.Id, "r1@x", "r2@x", "r3@x", "r4@x")
	addTraffic(t,
		&xray.ClientTraffic{Email: "r1@x", Up: gb}, &xray.ClientTraffic{Email: "r2@x", Up: gb},
		&xray.ClientTraffic{Email: "r3@x", Up: gb}, &xray.ClientTraffic{Email: "r4@x", Up: gb},
	)
	if got := poolUsed(t, a.Id); got != 4*gb {
		t.Fatalf("pool = %d, want %d", got, 4*gb)
	}

	cs, is := &ClientService{}, &InboundService{}
	if _, err := cs.ResetTrafficByEmail(is, "r1@x"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.BulkResetTraffic(is, []string{"r2@x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.DeleteByEmail(is, "r3@x", false); err != nil {
		t.Fatal(err)
	}
	// An auto-renew window that has passed resets the counters of r4.
	if err := database.GetDB().Model(xray.ClientTraffic{}).Where("email = ?", "r4@x").
		Updates(map[string]any{"reset": 30, "expiry_time": 1}).Error; err != nil {
		t.Fatal(err)
	}
	addTraffic(t)
	if ct := clientRow(t, "r4@x"); ct.Up != 0 {
		t.Fatalf("auto-renew did not run (up=%d); the test would prove nothing", ct.Up)
	}
	// A poll after all that must add to the bill, never rebuild it from the client rows.
	addTraffic(t, &xray.ClientTraffic{Email: "r4@x", Up: gb})
	if got := poolUsed(t, a.Id); got != 5*gb {
		t.Fatalf("pool after reset, bulk reset, delete, renew and 1 GB more = %d, want %d", got, 5*gb)
	}
}

func nodeInboundTag(nodeID int) string {
	return fmt.Sprintf("n%d-in", nodeID)
}

// nodeTick is one node sync carrying the client in its settings, as real nodes report it.
func nodeTick(t *testing.T, cs xray.ClientTraffic) {
	t.Helper()
	nodeTickOn(t, 1, nodeInboundTag(1), cs)
}

func nodeTickOn(t *testing.T, nodeID int, tag string, cs xray.ClientTraffic) {
	t.Helper()
	settings := fmt.Sprintf(`{"clients":[{"email":%q,"enable":%v,"expiryTime":%d,"reset":%d}]}`,
		cs.Email, cs.Enable, cs.ExpiryTime, cs.Reset)
	syncNodeWithSettings(t, &InboundService{}, nodeID, tag, settings, cs)
}

func ownedNodeClient(t *testing.T, email string) int {
	t.Helper()
	return ownedNodeClientOn(t, 1, nodeInboundTag(1), email)
}

func ownedNodeClientOn(t *testing.T, nodeID int, tag, email string) int {
	t.Helper()
	db := database.GetDB()
	createNodeInboundWithClient(t, db, nodeID, tag, 25101, email)
	nodeTickOn(t, nodeID, tag, xray.ClientTraffic{Email: email, Enable: true})
	var n int64
	if db.Model(&model.ClientRecord{}).Where("email = ?", email).Count(&n); n != 1 {
		t.Fatalf("node sync left %d client records for %s", n, email)
	}
	r := mustCreateReseller(t, resellerPayload("shop-n"))
	ownEmails(t, r.Id, email)
	return r.Id
}

// A node reports totals that are already weighted; the master adds each delta once.
func TestPoolCounterBillsNodeDeltasOnce(t *testing.T) {
	setupPoolDB(t)
	id := ownedNodeClient(t, "n@x")

	nodeTick(t, xray.ClientTraffic{Email: "n@x", Up: 3 * gb, Down: gb, Enable: true})
	if got := poolUsed(t, id); got != 4*gb {
		t.Fatalf("after first node delta: pool = %d, want %d", got, 4*gb)
	}
	// The same totals again carry no new bytes.
	nodeTick(t, xray.ClientTraffic{Email: "n@x", Up: 3 * gb, Down: gb, Enable: true})
	nodeTick(t, xray.ClientTraffic{Email: "n@x", Up: 4 * gb, Down: gb, Enable: true})
	if got := poolUsed(t, id); got != 5*gb {
		t.Fatalf("after a repeat and +1 GB: pool = %d, want %d", got, 5*gb)
	}
	if ct := readTraffic(t, database.GetDB(), "n@x"); ct.Up+ct.Down != 5*gb {
		t.Fatalf("client row = %d; the pool must move exactly as the row does", ct.Up+ct.Down)
	}
}

// A node that renews a client restarts its counters: the new window is billed, the old one stays.
func TestPoolCounterBillsTheWindowAfterANodeRenewal(t *testing.T) {
	setupPoolDB(t)
	id := ownedNodeClient(t, "rn@x")
	const first = int64(1893456000000)
	nodeTick(t, xray.ClientTraffic{Email: "rn@x", Up: 2 * gb, ExpiryTime: first, Reset: 30, Enable: true})
	nodeTick(t, xray.ClientTraffic{Email: "rn@x", Up: 0, Down: gb, ExpiryTime: first + 30*86400000, Reset: 30, Enable: true})
	if ct := readTraffic(t, database.GetDB(), "rn@x"); ct.ExpiryTime != first+30*86400000 {
		t.Fatalf("renewal not taken (expiry=%d); the test would prove nothing", ct.ExpiryTime)
	}
	if got := poolUsed(t, id); got != 3*gb {
		t.Fatalf("pool = %d, want %d (2 GB before the renewal + 1 GB after)", got, 3*gb)
	}
}
