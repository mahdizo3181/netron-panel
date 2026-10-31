package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
)

type resetFixture struct {
	a, b *model.Inbound
}

// seedResetFixture bills alice and bob on two inbounds so both own per-inbound rows.
func seedResetFixture(t *testing.T) resetFixture {
	t.Helper()
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	t.Cleanup(func() { withdrawClientTombstones("alice@x", "bob@x") })
	f := resetFixture{
		a: mkMultInbound(t, 23001, model.VLESS, nil),
		b: mkMultInbound(t, 23002, model.VLESS, f64(1.5)),
	}
	for _, ib := range []*model.Inbound{f.a, f.b} {
		attachClient(t, ib, "alice@x", 0)
		attachClient(t, ib, "bob@x", 0)
	}
	addTraffic(t,
		statDelta(f.a, "alice@x", 10, 10), statDelta(f.b, "alice@x", 10, 10),
		statDelta(f.a, "bob@x", 10, 10), statDelta(f.b, "bob@x", 10, 10),
	)
	if n := perInboundRows(t, "alice@x"); n != 2 {
		t.Fatalf("fixture: alice has %d per-inbound rows, want 2", n)
	}
	return f
}

func perInboundRows(t *testing.T, email string) int64 {
	t.Helper()
	var n int64
	if err := database.GetDB().Model(&model.ClientInboundTraffic{}).Where("email = ?", email).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func clientIDByEmail(t *testing.T, email string) int {
	t.Helper()
	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", email).First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	return rec.Id
}

// After a reset or delete the client's breakdown must restart with its counter,
// or the next period would show the old inbound split under a fresh total.
func TestUsageMultiplierResetAndDeleteDropPerInboundRows(t *testing.T) {
	cs, is := &ClientService{}, &InboundService{}
	cases := []struct {
		name      string
		op        func(t *testing.T, f resetFixture) error
		bobKept   bool
		aliceKept bool
	}{
		{"single reset", func(t *testing.T, f resetFixture) error {
			_, err := cs.ResetTrafficByEmail(is, "alice@x")
			return err
		}, true, false},
		{"single reset via inbound", func(t *testing.T, f resetFixture) error {
			_, err := is.ResetClientTraffic(f.a.Id, "alice@x")
			return err
		}, true, false},
		{"bulk reset", func(t *testing.T, f resetFixture) error {
			_, err := cs.BulkResetTraffic(is, []string{"alice@x"})
			return err
		}, true, false},
		{"periodic reset of an inbound's clients", func(t *testing.T, f resetFixture) error {
			_, err := cs.ResetAllClientTraffics(is, f.a.Id)
			return err
		}, false, false},
		{"reset all traffics", func(t *testing.T, f resetFixture) error {
			_, err := cs.ResetAllTraffics()
			return err
		}, false, false},
		{"delete client", func(t *testing.T, f resetFixture) error {
			_, err := cs.Delete(is, clientIDByEmail(t, "alice@x"), false)
			return err
		}, true, false},
		{"delete client keeping traffic", func(t *testing.T, f resetFixture) error {
			_, err := cs.Delete(is, clientIDByEmail(t, "alice@x"), true)
			return err
		}, true, true},
		{"bulk delete", func(t *testing.T, f resetFixture) error {
			_, _, err := cs.BulkDelete(is, []string{"alice@x"}, false)
			return err
		}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := seedResetFixture(t)
			if err := tc.op(t, f); err != nil {
				t.Fatalf("op: %v", err)
			}
			if got := perInboundRows(t, "alice@x") > 0; got != tc.aliceKept {
				t.Fatalf("alice per-inbound rows kept = %v, want %v", got, tc.aliceKept)
			}
			if got := perInboundRows(t, "bob@x") > 0; got != tc.bobKept {
				t.Fatalf("bob per-inbound rows kept = %v, want %v", got, tc.bobKept)
			}
		})
	}
}

func TestUsageMultiplierDelDepletedDropsPerInboundRows(t *testing.T) {
	seedResetFixture(t)
	if err := database.GetDB().Exec("UPDATE client_traffics SET total = 1 WHERE email = ?", "alice@x").Error; err != nil {
		t.Fatal(err)
	}
	if err := (&InboundService{}).DelDepletedClients(-1); err != nil {
		t.Fatalf("DelDepletedClients: %v", err)
	}
	if n := perInboundRows(t, "alice@x"); n != 0 {
		t.Fatalf("depleted alice keeps %d per-inbound rows", n)
	}
	if n := perInboundRows(t, "bob@x"); n != 2 {
		t.Fatalf("bob lost per-inbound rows: %d", n)
	}
}

// Removing depleted clients from one inbound keeps a client still attached
// elsewhere, and its counter; its breakdown must stay with it.
func TestUsageMultiplierDelDepletedOnOneInboundKeepsStillAttachedRows(t *testing.T) {
	f := seedResetFixture(t)
	other := mkMultInbound(t, 23003, model.Trojan, nil)
	attachClient(t, other, "alice@x", 0)
	if err := database.GetDB().Exec("UPDATE client_traffics SET total = 1 WHERE email = ?", "alice@x").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(f.a).Update("settings", clientsSettings(t, []model.Client{{Email: "alice@x"}, {Email: "bob@x"}})).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(other).Update("settings", clientsSettings(t, []model.Client{{Email: "alice@x"}})).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&InboundService{}).DelDepletedClients(f.a.Id); err != nil {
		t.Fatalf("DelDepletedClients: %v", err)
	}
	if n := perInboundRows(t, "alice@x"); n != 2 {
		t.Fatalf("alice still attached elsewhere lost her breakdown: %d rows", n)
	}
}

func TestUsageMultiplierReapSyncOrphansDropsPerInboundRows(t *testing.T) {
	seedResetFixture(t)
	db := database.GetDB()
	id := clientIDByEmail(t, "alice@x")
	if err := db.Where("client_id = ?", id).Delete(&model.ClientInbound{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ClientRecord{}).Where("id = ?", id).Update("sync_orphaned_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := (&ClientService{}).ReapSyncOrphans(); err != nil || n != 1 {
		t.Fatalf("ReapSyncOrphans = (%d, %v), want (1, nil)", n, err)
	}
	if n := perInboundRows(t, "alice@x"); n != 0 {
		t.Fatalf("reaped alice keeps %d per-inbound rows", n)
	}
	if n := perInboundRows(t, "bob@x"); n != 2 {
		t.Fatalf("bob lost rows: %d", n)
	}
}
