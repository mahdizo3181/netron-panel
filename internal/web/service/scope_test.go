package service

import (
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Two resellers and the admin each own one client; inbound a is allowed to shop-a only.
func seedScopeFixture(t *testing.T) (a, b *ResellerView, emails []string, subIDs []string, ids []int, inA, inB int) {
	t.Helper()
	setupResellerDB(t)
	emails = seedEnabledClientsForReset(t, &ClientService{}, 25001, 3, "scope")
	db := database.GetDB()
	var ib model.Inbound
	db.Where("port = ?", 25001).First(&ib)
	other := mkInbound(t, 25002, model.VLESS, `{"clients":[]}`)
	a = mustCreateReseller(t, resellerPayload("shop-a", ib.Id))
	b = mustCreateReseller(t, resellerPayload("shop-b", other.Id))
	db.Model(&model.ClientRecord{}).Where("email = ?", emails[0]).Update("reseller_id", a.Id)
	db.Model(&model.ClientRecord{}).Where("email = ?", emails[1]).Update("reseller_id", b.Id)
	var rows []model.ClientRecord
	db.Where("email IN ?", emails).Order("email").Find(&rows)
	for _, r := range rows {
		subIDs = append(subIDs, r.SubID)
		ids = append(ids, r.Id)
	}
	return a, b, emails, subIDs, ids, ib.Id, other.Id
}

func TestScopeClientFilterShowsOnlyOwnClients(t *testing.T) {
	a, _, emails, _, _, _, _ := seedScopeFixture(t)
	list := func(s Scope) []string {
		var got []string
		database.GetDB().Model(&model.ClientRecord{}).Scopes(s.ClientFilter).Order("email").Pluck("email", &got)
		return got
	}
	if got := list(AdminScope()); len(got) != 3 {
		t.Fatalf("admin scope sees %v, want all 3 clients", got)
	}
	if got := list(ResellerScope(a.Id)); len(got) != 1 || got[0] != emails[0] {
		t.Fatalf("shop-a scope sees %v, want only %q", got, emails[0])
	}
	if got := list(ResellerScope(99999)); len(got) != 0 {
		t.Fatalf("an unknown reseller sees %v, want nothing", got)
	}
}

// A foreign client and a client that does not exist must look the same, so a
// reseller cannot probe which emails, ids or subIds are in use.
func TestScopeOwnershipChecksHideForeignObjects(t *testing.T) {
	a, _, emails, subIDs, ids, _, _ := seedScopeFixture(t)
	tx := database.GetDB()
	sa := ResellerScope(a.Id)

	// Stored mixed-case; asked for in another case, as the API passes it through.
	tx.Model(&model.ClientRecord{}).Where("email = ?", emails[0]).Update("email", "Scope-0@X")
	if err := sa.RequireOwnEmails(tx, []string{"scope-0@x", "SCOPE-0@X"}); err != nil {
		t.Fatalf("own email (any case): %v", err)
	}
	emails[0] = "Scope-0@X"
	for name, err := range map[string]error{
		"other reseller's email": sa.RequireOwnEmails(tx, []string{emails[0], emails[1]}),
		"admin's email":          sa.RequireOwnEmails(tx, []string{emails[2]}),
		"unknown email":          sa.RequireOwnEmails(tx, []string{"nobody@x"}),
		"other reseller's id":    sa.RequireOwnClientIDs(tx, []int{ids[0], ids[1]}),
		"unknown id":             sa.RequireOwnClientIDs(tx, []int{99999}),
		"other reseller's subId": sa.RequireOwnSubID(tx, subIDs[1]),
		"admin's subId":          sa.RequireOwnSubID(tx, subIDs[2]),
		"unknown subId":          sa.RequireOwnSubID(tx, "no-such-sub"),
	} {
		if !errors.Is(err, ErrScopeNotFound) {
			t.Errorf("%s: err = %v, want ErrScopeNotFound", name, err)
		}
	}
	if err := sa.RequireOwnClientIDs(tx, []int{ids[0]}); err != nil {
		t.Fatalf("own id: %v", err)
	}
	if err := sa.RequireOwnSubID(tx, subIDs[0]); err != nil {
		t.Fatalf("own subId: %v", err)
	}
	admin := AdminScope()
	if err := admin.RequireOwnEmails(tx, emails); err != nil {
		t.Fatalf("admin scope must own every client: %v", err)
	}
}

func TestScopeAllowedInbounds(t *testing.T) {
	a, _, _, _, _, inA, inB := seedScopeFixture(t)
	tx := database.GetDB()
	sa := ResellerScope(a.Id)
	if err := sa.RequireAllowedInbounds(tx, []int{inA, inA}); err != nil {
		t.Fatalf("allowed inbound (listed twice): %v", err)
	}
	for name, ids := range map[string][]int{
		"other reseller's inbound":    {inB},
		"one allowed and one foreign": {inA, inB},
		"unknown inbound":             {99999},
	} {
		if err := sa.RequireAllowedInbounds(tx, ids); !errors.Is(err, ErrScopeInbound) {
			t.Errorf("%s: err = %v, want ErrScopeInbound", name, err)
		}
	}
	if err := AdminScope().RequireAllowedInbounds(tx, []int{inA, inB, 99999}); err != nil {
		t.Fatalf("admin scope is never limited by allowed lists: %v", err)
	}
}
