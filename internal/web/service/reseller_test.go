package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/testpg"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
)

const resellerTestPassword = "correct-horse-battery"

// setupResellerDB is setupBulkDB with a fresh schema per test on PostgreSQL.
func setupResellerDB(t *testing.T) {
	t.Helper()
	cleanup, err := testpg.IsolatePackage("reseller")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	setupBulkDB(t)
}

func resellerPayload(username string, inboundIds ...int) ResellerPayload {
	return ResellerPayload{Username: username, Password: resellerTestPassword, Name: "Shop " + username, InboundIds: inboundIds}
}

func mustCreateReseller(t *testing.T, p ResellerPayload) *ResellerView {
	t.Helper()
	view, err := (&ResellerService{}).Create(p)
	if err != nil {
		t.Fatalf("create reseller %q: %v", p.Username, err)
	}
	return view
}

func ownClients(t *testing.T, resellerID int, emails []string) {
	t.Helper()
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email IN ?", emails).
		Updates(map[string]any{"reseller_id": resellerID, "pool_disabled": true}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestResellerCreateStoresLimitsAndAllowedInbounds(t *testing.T) {
	setupResellerDB(t)
	ib := mkInbound(t, 24001, model.VLESS, `{"clients":[]}`)
	p := resellerPayload("shop-a", ib.Id)
	p.PoolBytes, p.MaxClients, p.ExpiryTime, p.RequireClientQuota = 1000<<30, 50, 1893456000000, true

	created := mustCreateReseller(t, p)
	got, err := (&ResellerService{}).Get(created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "shop-a" || got.Name != "Shop shop-a" || !got.Enable ||
		got.PoolBytes != 1000<<30 || got.UsedTraffic != 0 || got.RemainingBytes != 1000<<30 ||
		got.MaxClients != 50 || got.ExpiryTime != 1893456000000 || !got.RequireClientQuota ||
		len(got.InboundIds) != 1 || got.InboundIds[0] != ib.Id || got.ClientCount != 0 {
		t.Fatalf("stored reseller = %+v", *got)
	}
}

// The login form is shared, so one name must never match an admin and a reseller.
func TestResellerUsernameIsUniqueAcrossAdminsAndResellers(t *testing.T) {
	setupResellerDB(t)
	var admin model.User
	if err := database.GetDB().First(&admin).Error; err != nil {
		t.Fatalf("seeded admin: %v", err)
	}
	svc := &ResellerService{}
	if _, err := svc.Create(resellerPayload(strings.ToUpper(admin.Username))); !errors.Is(err, ErrResellerUsernameTaken) {
		t.Fatalf("create with the admin's username (other case): err = %v, want ErrResellerUsernameTaken", err)
	}
	mustCreateReseller(t, resellerPayload("shop-a"))
	if _, err := svc.Create(resellerPayload("Shop-A")); !errors.Is(err, ErrResellerUsernameTaken) {
		t.Fatalf("duplicate reseller username: err = %v, want ErrResellerUsernameTaken", err)
	}
	taken, err := ResellerUsernameExists("SHOP-A")
	if err != nil || !taken {
		t.Fatalf("ResellerUsernameExists(SHOP-A) = %v, %v; want true", taken, err)
	}
	if taken, _ := ResellerUsernameExists("nobody"); taken {
		t.Fatal("ResellerUsernameExists(nobody) = true")
	}
}

func TestResellerCreateRejectsBadInput(t *testing.T) {
	setupResellerDB(t)
	wg := mkInbound(t, 24002, model.WireGuard, `{}`)
	cases := []struct {
		name   string
		mutate func(*ResellerPayload)
		want   error
	}{
		{"short username", func(p *ResellerPayload) { p.Username = "ab" }, ErrResellerUsername},
		{"username with a space", func(p *ResellerPayload) { p.Username = "shop a" }, ErrResellerUsername},
		{"password under 10 chars", func(p *ResellerPayload) { p.Password = "123456789" }, ErrResellerPassword},
		{"password equal to username", func(p *ResellerPayload) { p.Username, p.Password = "longusername", "LongUserName" }, ErrResellerPassword},
		{"negative pool", func(p *ResellerPayload) { p.PoolBytes = -1 }, ErrResellerLimits},
		{"negative client limit", func(p *ResellerPayload) { p.MaxClients = -1 }, ErrResellerLimits},
		{"unknown inbound", func(p *ResellerPayload) { p.InboundIds = []int{99999} }, ErrResellerInbound},
		{"wireguard inbound", func(p *ResellerPayload) { p.InboundIds = []int{wg.Id} }, ErrResellerInbound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := resellerPayload("shop-bad")
			tc.mutate(&p)
			if _, err := (&ResellerService{}).Create(p); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var n int64
			database.GetDB().Model(&model.Reseller{}).Count(&n)
			if n != 0 {
				t.Fatalf("a rejected create left %d reseller rows", n)
			}
		})
	}
}

func TestResellerPasswordIsBcryptAndNeverSerialized(t *testing.T) {
	setupResellerDB(t)
	view := mustCreateReseller(t, resellerPayload("shop-a"))

	var row model.Reseller
	if err := database.GetDB().First(&row, view.Id).Error; err != nil {
		t.Fatal(err)
	}
	if row.Password == resellerTestPassword || !crypto.CheckPasswordHash(row.Password, resellerTestPassword) {
		t.Fatalf("stored password is not a bcrypt hash of the given one: %q", row.Password)
	}
	list, err := (&ResellerService{}).List()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{view, list, row} {
		b, _ := json.Marshal(v)
		for _, secret := range []string{row.Password, resellerTestPassword, "assword", "totp"} {
			if strings.Contains(string(b), secret) {
				t.Fatalf("JSON of %T leaks %q: %s", v, secret, b)
			}
		}
	}
}

func TestResellerSetPasswordReplacesHashAndEndsSessions(t *testing.T) {
	setupResellerDB(t)
	view := mustCreateReseller(t, resellerPayload("shop-a"))
	svc := &ResellerService{}
	if err := svc.SetPassword(view.Id, "short"); !errors.Is(err, ErrResellerPassword) {
		t.Fatalf("weak password: err = %v, want ErrResellerPassword", err)
	}
	if err := svc.SetPassword(view.Id, "another-long-secret"); err != nil {
		t.Fatal(err)
	}
	var row model.Reseller
	database.GetDB().First(&row, view.Id)
	if !crypto.CheckPasswordHash(row.Password, "another-long-secret") || row.LoginEpoch != 1 {
		t.Fatalf("after SetPassword: hash matches=%v epoch=%d, want true and 1",
			crypto.CheckPasswordHash(row.Password, "another-long-secret"), row.LoginEpoch)
	}
	if err := svc.SetPassword(99999, "another-long-secret"); !errors.Is(err, ErrResellerNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrResellerNotFound", err)
	}
}

// Update is a full-row edit of the admin-set fields; it must not touch the
// login name, the credentials or the usage counter.
func TestResellerUpdateEditsLimitsOnly(t *testing.T) {
	setupResellerDB(t)
	a := mkInbound(t, 24003, model.VLESS, `{"clients":[]}`)
	b := mkInbound(t, 24004, model.Trojan, `{"clients":[]}`)
	view := mustCreateReseller(t, resellerPayload("shop-a", a.Id))
	db := database.GetDB()
	db.Model(&model.Reseller{}).Where("id = ?", view.Id).Update("used_traffic", 300)
	var before model.Reseller
	db.First(&before, view.Id)

	off := false
	got, err := (&ResellerService{}).Update(view.Id, ResellerPayload{
		Username: "renamed", Password: "ignored-password-1", Name: "New name", Enable: &off,
		PoolBytes: 1000, MaxClients: 7, ExpiryTime: 5, RequireClientQuota: true, InboundIds: []int{b.Id},
	})
	if err != nil {
		t.Fatal(err)
	}
	var after model.Reseller
	db.First(&after, view.Id)
	if after.Username != "shop-a" || after.Password != before.Password || after.LoginEpoch != before.LoginEpoch || after.UsedTraffic != 300 {
		t.Fatalf("update touched identity or usage: %+v", after)
	}
	if got.Name != "New name" || got.Enable || got.PoolBytes != 1000 || got.RemainingBytes != 700 || got.MaxClients != 7 ||
		got.ExpiryTime != 5 || !got.RequireClientQuota || len(got.InboundIds) != 1 || got.InboundIds[0] != b.Id {
		t.Fatalf("updated view = %+v", *got)
	}
	if _, err := (&ResellerService{}).Update(99999, ResellerPayload{}); !errors.Is(err, ErrResellerNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrResellerNotFound", err)
	}
}

func TestResellerListCountsOwnClientsAndRemaining(t *testing.T) {
	setupResellerDB(t)
	a := mustCreateReseller(t, resellerPayload("shop-a"))
	p := resellerPayload("shop-b")
	p.PoolBytes = 500
	b := mustCreateReseller(t, p)
	emails := seedEnabledClientsForReset(t, &ClientService{}, 24005, 3, "own")
	ownClients(t, b.Id, emails[:2])
	database.GetDB().Model(&model.Reseller{}).Where("id = ?", b.Id).Update("used_traffic", 800)

	list, err := (&ResellerService{}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Id != a.Id || list[1].Id != b.Id {
		t.Fatalf("list = %+v, want shop-a then shop-b", list)
	}
	if list[0].ClientCount != 0 || list[0].RemainingBytes != -1 {
		t.Fatalf("shop-a (no pool, no clients) = count %d remaining %d, want 0 and -1", list[0].ClientCount, list[0].RemainingBytes)
	}
	if list[1].ClientCount != 2 || list[1].RemainingBytes != 0 {
		t.Fatalf("shop-b (overshot pool, 2 clients) = count %d remaining %d, want 2 and 0", list[1].ClientCount, list[1].RemainingBytes)
	}
}

func TestResellerDeleteTransferKeepsClientsWithAdmin(t *testing.T) {
	setupResellerDB(t)
	svc := &ResellerService{}
	emails := seedEnabledClientsForReset(t, &ClientService{}, 24006, 3, "tr")
	var ib model.Inbound
	database.GetDB().Where("port = ?", 24006).First(&ib)
	view := mustCreateReseller(t, resellerPayload("shop-a", ib.Id))
	ownClients(t, view.Id, emails[:2])

	if _, err := svc.Delete(&InboundService{}, view.Id, "purge"); !errors.Is(err, ErrResellerDeleteMode) {
		t.Fatalf("unknown mode: err = %v, want ErrResellerDeleteMode", err)
	}
	if _, err := svc.Delete(&InboundService{}, view.Id, ""); err != nil {
		t.Fatalf("delete (default mode): %v", err)
	}
	db := database.GetDB()
	var clients, owned, flagged, resellers, links int64
	db.Model(&model.ClientRecord{}).Where("email IN ?", emails).Count(&clients)
	db.Model(&model.ClientRecord{}).Where("reseller_id IS NOT NULL").Count(&owned)
	db.Model(&model.ClientRecord{}).Where("pool_disabled = ?", true).Count(&flagged)
	db.Model(&model.Reseller{}).Count(&resellers)
	db.Model(&model.ResellerInbound{}).Count(&links)
	if clients != 3 || owned != 0 || flagged != 0 || resellers != 0 || links != 0 {
		t.Fatalf("after transfer delete: clients=%d owned=%d flagged=%d resellers=%d links=%d; want 3,0,0,0,0",
			clients, owned, flagged, resellers, links)
	}
	if _, err := svc.Delete(&InboundService{}, view.Id, ""); !errors.Is(err, ErrResellerNotFound) {
		t.Fatalf("second delete: err = %v, want ErrResellerNotFound", err)
	}
}

func TestResellerDeleteWithClientsRemovesOnlyItsClients(t *testing.T) {
	setupResellerDB(t)
	emails := seedEnabledClientsForReset(t, &ClientService{}, 24007, 3, "rm")
	view := mustCreateReseller(t, resellerPayload("shop-a"))
	ownClients(t, view.Id, emails[:2])

	if _, err := (&ResellerService{}).Delete(&InboundService{}, view.Id, ResellerDeleteClients); err != nil {
		t.Fatalf("delete with clients: %v", err)
	}
	var left []string
	database.GetDB().Model(&model.ClientRecord{}).Order("email").Pluck("email", &left)
	if len(left) != 1 || left[0] != emails[2] {
		t.Fatalf("clients left = %v, want only the admin's %q", left, emails[2])
	}
	var resellers, stats int64
	database.GetDB().Model(&model.Reseller{}).Count(&resellers)
	database.GetDB().Table("client_traffics").Where("email IN ?", emails[:2]).Count(&stats)
	if resellers != 0 || stats != 0 {
		t.Fatalf("resellers=%d, deleted clients' traffic rows=%d; want 0, 0", resellers, stats)
	}
}

func TestDeletingAnInboundDropsItFromAllowedLists(t *testing.T) {
	setupResellerDB(t)
	a := mkInbound(t, 24008, model.VLESS, `{"clients":[]}`)
	b := mkInbound(t, 24009, model.VLESS, `{"clients":[]}`)
	view := mustCreateReseller(t, resellerPayload("shop-a", a.Id, b.Id))

	if _, err := (&InboundService{}).DelInbound(a.Id); err != nil {
		t.Fatalf("DelInbound: %v", err)
	}
	got, err := (&ResellerService{}).Get(view.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.InboundIds) != 1 || got.InboundIds[0] != b.Id {
		t.Fatalf("allowed inbounds after deleting %d = %v, want [%d]", a.Id, got.InboundIds, b.Id)
	}
}

// GORM writes the column default back into the struct after insert, so a stored
// false must be read from the payload, not from the inserted row.
func TestResellerCreatedDisabledStaysDisabled(t *testing.T) {
	setupResellerDB(t)
	off := false
	p := resellerPayload("shop-off")
	p.Enable = &off
	created := mustCreateReseller(t, p)
	got, err := (&ResellerService{}).Get(created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enable || created.Enable {
		t.Fatalf("created with enable=false, stored enable=%v view enable=%v", got.Enable, created.Enable)
	}
}
