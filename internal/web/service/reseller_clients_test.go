package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func newClientPayload(email string, inboundIds ...int) *ClientCreatePayload {
	return &ClientCreatePayload{
		Client:     model.Client{Email: email, ID: uuid.NewString(), Enable: true},
		InboundIds: inboundIds,
	}
}

func resellerClientRow(t *testing.T, email string) *model.ClientRecord {
	t.Helper()
	var r model.ClientRecord
	err := database.GetDB().Where("LOWER(email) = LOWER(?)", email).First(&r).Error
	if database.IsNotFound(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

func ownerID(r *model.ClientRecord) string {
	if r.ResellerID == nil {
		return "admin"
	}
	return fmt.Sprint(*r.ResellerID)
}

func mustCreateFor(t *testing.T, scope Scope, p *ClientCreatePayload) {
	t.Helper()
	if _, err := (&ClientService{}).CreateFor(scope, &InboundService{}, p); err != nil {
		t.Fatalf("create %s: %v", p.Client.Email, err)
	}
}

func countOwned(t *testing.T, resellerID int) int64 {
	t.Helper()
	var n int64
	database.GetDB().Model(&model.ClientRecord{}).Where("reseller_id = ?", resellerID).Count(&n)
	return n
}

func vless(t *testing.T, port int) *model.Inbound {
	t.Helper()
	return mkInbound(t, port, model.VLESS, `{"clients":[]}`)
}

func TestResellerCreateStampsOwnerOnlyOnAllowedInbounds(t *testing.T) {
	setupResellerDB(t)
	allowed, other := vless(t, 26001), vless(t, 26002)
	r := mustCreateReseller(t, resellerPayload("shop-a", allowed.Id))
	svc, sc := &ClientService{}, ResellerScope(r.Id)

	p := newClientPayload("a@shop", allowed.Id)
	p.Client.Group = "vip"
	p.Client.Reverse = &model.ClientReverse{Tag: "r"}
	mustCreateFor(t, sc, p)
	row := resellerClientRow(t, "a@shop")
	if row == nil || ownerID(row) != fmt.Sprint(r.Id) || row.Group != "" || row.Reverse != "" {
		t.Fatalf("reseller-created client = %+v, want owner %d, no group, no reverse", row, r.Id)
	}

	for name, ids := range map[string][]int{"other inbound": {other.Id}, "allowed plus other": {allowed.Id, other.Id}, "unknown inbound": {99999}} {
		if _, err := svc.CreateFor(sc, &InboundService{}, newClientPayload("x-"+strings.ReplaceAll(name, " ", "-")+"@shop", ids...)); !errors.Is(err, ErrScopeInbound) {
			t.Errorf("%s: err = %v, want ErrScopeInbound", name, err)
		}
	}
	var n int64
	database.GetDB().Model(&model.ClientRecord{}).Where("email LIKE ?", "x-%").Count(&n)
	if n != 0 {
		t.Fatalf("refused creates left %d clients", n)
	}

	mustCreateFor(t, AdminScope(), newClientPayload("b@admin", other.Id))
	if row := resellerClientRow(t, "b@admin"); row == nil || row.ResellerID != nil {
		t.Fatalf("admin-created client = %+v, want no owner", row)
	}
}

// §3.5 item 8: a taken email answers exactly as a duplicate of one's own, and the
// re-add path (same email and subId) must never attach another owner's client.
func TestResellerCreateRefusesForeignEmailsGenerically(t *testing.T) {
	setupResellerDB(t)
	a1, a2 := vless(t, 26011), vless(t, 26012)
	r := mustCreateReseller(t, resellerPayload("shop-a", a1.Id, a2.Id))
	sc := ResellerScope(r.Id)
	taken := newClientPayload("taken@x", a1.Id)
	taken.Client.SubID = "sub-taken"
	mustCreateFor(t, AdminScope(), taken)
	before := resellerClientRow(t, "taken@x")
	mine := newClientPayload("mine@x", a1.Id)
	mine.Client.SubID = "sub-mine"
	mustCreateFor(t, sc, mine)

	_, ownErr := (&ClientService{}).CreateFor(sc, &InboundService{}, &ClientCreatePayload{
		Client: model.Client{Email: "mine@x", SubID: "other-sub", Enable: true}, InboundIds: []int{a2.Id},
	})
	if ownErr == nil {
		t.Fatal("duplicate of an own email was accepted")
	}
	for _, attempt := range []model.Client{
		{Email: "taken@x", SubID: "fresh-sub", Enable: true},
		{Email: "taken@x", SubID: "sub-taken", Enable: true},
		{Email: "TAKEN@X", SubID: "sub-taken", Enable: true},
	} {
		_, err := (&ClientService{}).CreateFor(sc, &InboundService{}, &ClientCreatePayload{Client: attempt, InboundIds: []int{a2.Id}})
		want := strings.Replace(ownErr.Error(), "mine@x", attempt.Email, 1)
		if err == nil || err.Error() != want {
			t.Errorf("create %s (subId %s): err = %v, want %q", attempt.Email, attempt.SubID, err, want)
		}
	}
	after := resellerClientRow(t, "taken@x")
	ids, _ := (&ClientService{}).GetInboundIdsForRecord(after.Id)
	if after.ResellerID != nil || after.UUID != before.UUID || len(ids) != 1 || ids[0] != a1.Id {
		t.Fatalf("the admin's client was touched: owner %s uuid %s inbounds %v", ownerID(after), after.UUID, ids)
	}

	// Re-adding one's own identity to another allowed inbound is still possible.
	if _, err := (&ClientService{}).CreateFor(sc, &InboundService{}, &ClientCreatePayload{
		Client: model.Client{Email: "mine@x", SubID: "sub-mine", Enable: true}, InboundIds: []int{a2.Id},
	}); err != nil {
		t.Fatalf("own re-add: %v", err)
	}
	if n := countOwned(t, r.Id); n != 1 {
		t.Fatalf("own re-add made %d owned rows, want 1", n)
	}
}

func TestResellerClientLimitHoldsForSingleBulkAndConcurrentCreates(t *testing.T) {
	setupResellerDB(t)
	ib := vless(t, 26021)
	p := resellerPayload("shop-a", ib.Id)
	p.MaxClients = 3
	r := mustCreateReseller(t, p)
	sc, svc := ResellerScope(r.Id), &ClientService{}

	mustCreateFor(t, sc, newClientPayload("c1@shop", ib.Id))
	mustCreateFor(t, sc, newClientPayload("c2@shop", ib.Id))
	if _, _, err := svc.BulkCreateFor(sc, &InboundService{}, []ClientCreatePayload{
		*newClientPayload("b1@shop", ib.Id), *newClientPayload("b2@shop", ib.Id),
	}); !errors.Is(err, ErrResellerClientLimit) {
		t.Fatalf("bulk over the limit: err = %v, want ErrResellerClientLimit", err)
	}
	if n := countOwned(t, r.Id); n != 2 {
		t.Fatalf("a refused bulk create left %d owned clients, want 2", n)
	}

	const racers = 10
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.CreateFor(sc, &InboundService{}, newClientPayload(fmt.Sprintf("race%d@shop", i), ib.Id))
		}(i)
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrResellerClientLimit):
			t.Errorf("racer failed for another reason: %v", err)
		}
	}
	if n := countOwned(t, r.Id); won != 1 || n != 3 {
		t.Fatalf("%d concurrent creates won and %d clients are owned; want 1 and 3", won, n)
	}

	for i := range 4 {
		mustCreateFor(t, AdminScope(), newClientPayload(fmt.Sprintf("admin%d@x", i), ib.Id))
	}
}

func TestBlockedResellerCannotAddOrEnableClients(t *testing.T) {
	blocks := map[string]map[string]any{
		"pool used up": {"pool_bytes": 1000, "used_traffic": 1000},
		"expired":      {"expiry_time": time.Now().UnixMilli() - 1},
		"disabled":     {"enable": false},
		"cut off":      {"cutoff_reason": "pool"},
	}
	for name, block := range blocks {
		t.Run(name, func(t *testing.T) {
			setupResellerDB(t)
			ib := vless(t, 26031)
			r := mustCreateReseller(t, resellerPayload("shop-a", ib.Id))
			sc, svc, isvc := ResellerScope(r.Id), &ClientService{}, &InboundService{}
			mustCreateFor(t, sc, newClientPayload("on@shop", ib.Id))
			off := newClientPayload("off@shop", ib.Id)
			off.Client.Enable = false
			mustCreateFor(t, sc, off)
			database.GetDB().Model(&model.Reseller{}).Where("id = ?", r.Id).Updates(block)

			refusals := map[string]error{}
			_, refusals["create"] = svc.CreateFor(sc, isvc, newClientPayload("new@shop", ib.Id))
			_, _, refusals["bulk create"] = svc.BulkCreateFor(sc, isvc, []ClientCreatePayload{*newClientPayload("new2@shop", ib.Id)})
			_, _, refusals["bulk enable"] = svc.BulkSetEnableFor(sc, isvc, []string{"off@shop"}, true)
			enabled := clientAsModel(t, "off@shop")
			enabled.Enable = true
			_, refusals["update to enabled"] = svc.UpdateByEmailFor(sc, isvc, "off@shop", enabled, 0)
			_, refusals["reset traffic"] = svc.ResetTrafficByEmailFor(sc, isvc, "on@shop")
			for op, err := range refusals {
				if !errors.Is(err, ErrResellerBlocked) {
					t.Errorf("%s: err = %v, want ErrResellerBlocked", op, err)
				}
			}
			if row := resellerClientRow(t, "off@shop"); row.Enable {
				t.Fatal("a blocked reseller enabled a client")
			}

			still := clientAsModel(t, "off@shop")
			still.Comment = "edited while blocked"
			if _, err := svc.UpdateByEmailFor(sc, isvc, "off@shop", still, 0); err != nil {
				t.Errorf("edit without enabling: %v", err)
			}
			if _, _, err := svc.BulkSetEnableFor(sc, isvc, []string{"on@shop"}, false); err != nil {
				t.Errorf("disable: %v", err)
			}
			if _, err := svc.DeleteByEmailFor(sc, isvc, "off@shop", false); err != nil {
				t.Errorf("delete: %v", err)
			}
		})
	}
}

func clientAsModel(t *testing.T, email string) model.Client {
	t.Helper()
	row := resellerClientRow(t, email)
	if row == nil {
		t.Fatalf("no client %s", email)
	}
	return model.Client{
		Email: row.Email, ID: row.UUID, SubID: row.SubID, Enable: row.Enable, TotalGB: row.TotalGB,
		ExpiryTime: row.ExpiryTime, LimitIP: row.LimitIP, Comment: row.Comment, Flow: row.Flow,
	}
}

func TestResellerQuotaRuleAndUnlimitedClients(t *testing.T) {
	setupResellerDB(t)
	ib := vless(t, 26041)
	strict := resellerPayload("shop-strict", ib.Id)
	strict.RequireClientQuota = true
	rs := mustCreateReseller(t, strict)
	ru := mustCreateReseller(t, resellerPayload("shop-free", ib.Id))
	svc, isvc := &ClientService{}, &InboundService{}

	if _, err := svc.CreateFor(ResellerScope(rs.Id), isvc, newClientPayload("zero@strict", ib.Id)); !errors.Is(err, ErrResellerQuotaRequired) {
		t.Fatalf("unlimited client under the quota rule: err = %v, want ErrResellerQuotaRequired", err)
	}
	capped := newClientPayload("capped@strict", ib.Id)
	capped.Client.TotalGB = 5 << 30
	mustCreateFor(t, ResellerScope(rs.Id), capped)
	lifted := clientAsModel(t, "capped@strict")
	lifted.TotalGB = 0
	if _, err := svc.UpdateByEmailFor(ResellerScope(rs.Id), isvc, "capped@strict", lifted, 0); !errors.Is(err, ErrResellerQuotaRequired) {
		t.Fatalf("lifting the limit under the quota rule: err = %v", err)
	}

	mustCreateFor(t, ResellerScope(ru.Id), newClientPayload("unlimited@free", ib.Id))
	limited := newClientPayload("limited@free", ib.Id)
	limited.Client.TotalGB = 7 << 30
	mustCreateFor(t, ResellerScope(ru.Id), limited)
	if a, b := resellerClientRow(t, "unlimited@free"), resellerClientRow(t, "limited@free"); a.TotalGB != 0 || b.TotalGB != 7<<30 {
		t.Fatalf("per-client limits: unlimited=%d limited=%d, want 0 and %d", a.TotalGB, b.TotalGB, int64(7<<30))
	}
}

func TestResellerOperationsNeverReachForeignClients(t *testing.T) {
	setupResellerDB(t)
	mine, theirs, extra := vless(t, 26051), vless(t, 26052), vless(t, 26053)
	ra := mustCreateReseller(t, resellerPayload("shop-a", mine.Id))
	rb := mustCreateReseller(t, resellerPayload("shop-b", theirs.Id))
	sa, svc, isvc := ResellerScope(ra.Id), &ClientService{}, &InboundService{}
	mustCreateFor(t, sa, newClientPayload("own@a", mine.Id))
	mustCreateFor(t, ResellerScope(rb.Id), newClientPayload("other@b", theirs.Id))
	mustCreateFor(t, AdminScope(), newClientPayload("boss@admin", theirs.Id))

	for _, foreign := range []string{"other@b", "boss@admin", "nobody@x"} {
		edited := model.Client{Email: foreign, Comment: "hijacked", Enable: true}
		_, errUpdate := svc.UpdateByEmailFor(sa, isvc, foreign, edited, 0)
		_, errDelete := svc.DeleteByEmailFor(sa, isvc, foreign, false)
		_, errReset := svc.ResetTrafficByEmailFor(sa, isvc, foreign)
		_, errGet := svc.GetRecordByEmailFor(sa, foreign)
		_, _, errBulk := svc.BulkSetEnableFor(sa, isvc, []string{"own@a", foreign}, false)
		for op, err := range map[string]error{"update": errUpdate, "delete": errDelete, "reset": errReset, "get": errGet, "bulk disable": errBulk} {
			if !errors.Is(err, ErrScopeNotFound) {
				t.Errorf("%s %s: err = %v, want ErrScopeNotFound", op, foreign, err)
			}
		}
	}
	if row := resellerClientRow(t, "own@a"); !row.Enable {
		t.Fatal("a refused bulk disable still disabled the reseller's own client")
	}
	for _, e := range []string{"other@b", "boss@admin"} {
		if row := resellerClientRow(t, e); row == nil || row.Comment == "hijacked" || !row.Enable {
			t.Fatalf("foreign client %s changed: %+v", e, row)
		}
	}

	own := clientAsModel(t, "own@a")
	if _, err := svc.UpdateByEmailFor(sa, isvc, "own@a", own, 0, mine.Id, extra.Id); !errors.Is(err, ErrScopeInbound) {
		t.Fatalf("update with a foreign inbound in its filter: err = %v, want ErrScopeInbound", err)
	}
	database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", "own@a").Update("group_name", "set-by-admin")
	own.Comment = "fine"
	own.Group = "set-by-reseller"
	own.Reverse = &model.ClientReverse{Tag: "r"}
	if _, err := svc.UpdateByEmailFor(sa, isvc, "own@a", own, 0); err != nil {
		t.Fatal(err)
	}
	if row := resellerClientRow(t, "own@a"); ownerID(row) != fmt.Sprint(ra.Id) || row.Group != "set-by-admin" || row.Reverse != "" {
		t.Fatalf("an update changed owner %s, group %q or reverse %q", ownerID(row), row.Group, row.Reverse)
	}
}

func TestListPagedForScopesEverythingAndFiltersByOwner(t *testing.T) {
	setupResellerDB(t)
	ib := vless(t, 26061)
	ra := mustCreateReseller(t, resellerPayload("shop-a", ib.Id))
	rb := mustCreateReseller(t, resellerPayload("shop-b", ib.Id))
	adminClient := newClientPayload("boss@admin", ib.Id)
	adminClient.Client.Group = "staff"
	mustCreateFor(t, AdminScope(), adminClient)
	mustCreateFor(t, AdminScope(), newClientPayload("boss2@admin", ib.Id))
	mustCreateFor(t, ResellerScope(ra.Id), newClientPayload("a1@shop", ib.Id))
	mustCreateFor(t, ResellerScope(ra.Id), newClientPayload("a2@shop", ib.Id))
	mustCreateFor(t, ResellerScope(rb.Id), newClientPayload("b1@shop", ib.Id))
	isvc := &InboundService{}
	previous := currentXrayProcess()
	xrayState.replace(xray.NewTestProcess(&xray.Config{}, t.TempDir()+"/config.json"))
	t.Cleanup(func() { xrayState.replace(previous) })
	isvc.RefreshLocalOnlineClients([]string{"boss@admin", "a1@shop"}, nil)
	svc := &ClientService{}

	emails := func(resp *ClientPageResponse) string {
		var out []string
		for _, it := range resp.Items {
			out = append(out, it.Email)
		}
		return strings.Join(out, ",")
	}
	asA, err := svc.ListPagedFor(ResellerScope(ra.Id), isvc, nil, ClientPageParams{Sort: "email", Order: "asc", Owner: fmt.Sprint(rb.Id)})
	if err != nil {
		t.Fatal(err)
	}
	if emails(asA) != "a1@shop,a2@shop" || asA.Total != 2 || asA.Filtered != 2 || asA.Summary.Total != 2 ||
		len(asA.Groups) != 0 || strings.Join(asA.Summary.Online, ",") != "a1@shop" {
		t.Fatalf("shop-a sees items %s total %d filtered %d summary %+v groups %v",
			emails(asA), asA.Total, asA.Filtered, asA.Summary, asA.Groups)
	}
	if found, _ := svc.ListPagedFor(ResellerScope(ra.Id), isvc, nil, ClientPageParams{Search: "boss"}); len(found.Items) != 0 || found.Filtered != 0 {
		t.Fatalf("shop-a's search found the admin's clients: %s", emails(found))
	}

	all, _ := svc.ListPagedFor(AdminScope(), isvc, nil, ClientPageParams{Sort: "email", Order: "asc"})
	owners := map[string]string{}
	for _, it := range all.Items {
		owners[it.Email] = "admin"
		if it.Owner != nil {
			owners[it.Email] = fmt.Sprintf("%d:%s", it.Owner.Id, it.Owner.Name)
		}
	}
	want := map[string]string{
		"a1@shop": fmt.Sprintf("%d:Shop shop-a", ra.Id), "a2@shop": fmt.Sprintf("%d:Shop shop-a", ra.Id),
		"b1@shop": fmt.Sprintf("%d:Shop shop-b", rb.Id), "boss@admin": "admin", "boss2@admin": "admin",
	}
	if fmt.Sprint(owners) != fmt.Sprint(want) || all.Total != 5 {
		t.Fatalf("admin list owners = %v (total %d), want %v", owners, all.Total, want)
	}
	for filter, wantEmails := range map[string]string{
		"admin": "boss2@admin,boss@admin", fmt.Sprint(rb.Id): "b1@shop", fmt.Sprint(ra.Id): "a1@shop,a2@shop", "99999": "",
	} {
		got, err := svc.ListPagedFor(AdminScope(), isvc, nil, ClientPageParams{Sort: "email", Order: "asc", Owner: filter})
		if err != nil {
			t.Fatal(err)
		}
		if emails(got) != wantEmails || got.Filtered != strings.Count(wantEmails, "@") || got.Total != 5 {
			t.Errorf("owner=%s: items %q filtered %d total %d, want %q", filter, emails(got), got.Filtered, got.Total, wantEmails)
		}
	}
}

func TestAutoRenewSkipsClientsOfBlockedResellers(t *testing.T) {
	setupResellerDB(t)
	svc := &InboundService{}
	db := database.GetDB()
	past := time.Now().Add(-time.Hour).UnixMilli()
	clients := []model.Client{
		{Email: "boss@admin", ID: uuid.NewString(), Enable: false, Reset: 30, ExpiryTime: past},
		{Email: "blocked@shop", ID: uuid.NewString(), Enable: false, Reset: 30, ExpiryTime: past},
		{Email: "flagged@shop", ID: uuid.NewString(), Enable: false, Reset: 30, ExpiryTime: past},
	}
	ib := mkInbound(t, 26071, model.VLESS, clientsSettings(t, clients))
	if err := svc.clientService.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatal(err)
	}
	for _, c := range clients {
		db.Create(&xray.ClientTraffic{InboundId: ib.Id, Email: c.Email, Enable: false, Up: 5, Down: 6, Reset: 30, ExpiryTime: past})
	}
	blocked := mustCreateReseller(t, resellerPayload("shop-off", ib.Id))
	db.Model(&model.Reseller{}).Where("id = ?", blocked.Id).Updates(map[string]any{"pool_bytes": 10, "used_traffic": 10})
	open := mustCreateReseller(t, resellerPayload("shop-on", ib.Id))
	db.Model(&model.ClientRecord{}).Where("email = ?", "blocked@shop").Update("reseller_id", blocked.Id)
	db.Model(&model.ClientRecord{}).Where("email = ?", "flagged@shop").Updates(map[string]any{"reseller_id": open.Id, "pool_disabled": true})

	if _, count, err := svc.autoRenewClients(db, newTrafficMutationBatch()); err != nil || count != 1 {
		t.Fatalf("autoRenewClients renewed %d (err %v), want only the admin's client", count, err)
	}
	for email, wantRenewed := range map[string]bool{"boss@admin": true, "blocked@shop": false, "flagged@shop": false} {
		var row xray.ClientTraffic
		db.Where("email = ?", email).First(&row)
		if renewed := row.Enable && row.ExpiryTime > past; renewed != wantRenewed {
			t.Errorf("%s renewed = %v, want %v", email, renewed, wantRenewed)
		}
	}
}

func TestSetOwnerMovesOwnershipAndNothingElse(t *testing.T) {
	setupResellerDB(t)
	shared, private := vless(t, 26081), vless(t, 26082)
	pa := resellerPayload("shop-a", shared.Id)
	pa.MaxClients = 1
	ra := mustCreateReseller(t, pa)
	rb := mustCreateReseller(t, resellerPayload("shop-b", shared.Id))
	db := database.GetDB()
	db.Model(&model.Reseller{}).Where("id = ?", ra.Id).Update("used_traffic", 111)
	db.Model(&model.Reseller{}).Where("id = ?", rb.Id).Update("used_traffic", 222)
	mustCreateFor(t, AdminScope(), newClientPayload("move@x", shared.Id, private.Id))
	mustCreateFor(t, AdminScope(), newClientPayload("second@x", shared.Id))
	db.Model(&model.ClientRecord{}).Where("email = ?", "move@x").Update("pool_disabled", true)
	svc := &ClientService{}

	change, err := svc.SetOwner("move@x", &ra.Id)
	if err != nil {
		t.Fatal(err)
	}
	row := resellerClientRow(t, "move@x")
	if ownerID(row) != fmt.Sprint(ra.Id) || row.PoolDisabled || change.Owner == nil || change.Owner.Name != "Shop shop-a" ||
		fmt.Sprint(change.OutsideInbounds) != fmt.Sprint([]int{private.Id}) {
		t.Fatalf("after SetOwner: row owner %s poolDisabled %v, change %+v", ownerID(row), row.PoolDisabled, change)
	}
	if _, err := svc.SetOwner("second@x", &ra.Id); !errors.Is(err, ErrResellerClientLimit) {
		t.Fatalf("over the new owner's limit: err = %v, want ErrResellerClientLimit", err)
	}
	if _, err := svc.SetOwner("move@x", &ra.Id); err != nil {
		t.Fatalf("re-assigning to the current owner at its limit: %v", err)
	}
	if _, err := svc.SetOwner("move@x", &rb.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetOwner("move@x", nil); err != nil {
		t.Fatal(err)
	}
	if row := resellerClientRow(t, "move@x"); row.ResellerID != nil {
		t.Fatalf("SetOwner(nil) left owner %s", ownerID(row))
	}
	var used []int64
	db.Model(&model.Reseller{}).Order("id").Pluck("used_traffic", &used)
	if fmt.Sprint(used) != "[111 222]" {
		t.Fatalf("reassignment rewrote pool usage: %v, want [111 222]", used)
	}
	missing := 99999
	if _, err := svc.SetOwner("move@x", &missing); !errors.Is(err, ErrResellerNotFound) {
		t.Fatalf("unknown reseller: err = %v", err)
	}
	if _, err := svc.SetOwner("nobody@x", nil); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("unknown client: err = %v", err)
	}
}

// A reseller's writes take the admin's runtime.Runtime path, so nodes follow:
// the same operations make the same node calls, whoever makes them.
func TestResellerWritesReachNodesLikeTheAdmins(t *testing.T) {
	setupResellerDB(t)
	startSerializedWriter(t)
	mgr := useTestRuntimeManager(t)
	fake := &fakeNodeRuntime{}
	node := &model.Node{Name: t.Name(), Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatal(err)
	}
	mgr.SetRuntimeOverride(node.Id, fake)
	ib := nodeInbound(t, node.Id, 26091, nil)
	r := mustCreateReseller(t, resellerPayload("shop-a", ib.Id))
	svc, isvc := &ClientService{}, &InboundService{}

	calls := func() [5]int32 {
		return [5]int32{fake.addClient.Load(), fake.updateInbound.Load(), fake.updateUser.Load(), fake.deleteClient.Load(), fake.deleteUser.Load()}
	}
	run := func(scope Scope, email string) [5]int32 {
		before := calls()
		mustCreateFor(t, scope, newClientPayload(email, ib.Id))
		edited := clientAsModel(t, email)
		edited.Comment = "edited"
		if _, err := svc.UpdateByEmailFor(scope, isvc, email, edited, 0); err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.BulkSetEnableFor(scope, isvc, []string{email}, false); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DeleteByEmailFor(scope, isvc, email, false); err != nil {
			t.Fatal(err)
		}
		after := calls()
		var delta [5]int32
		for i := range delta {
			delta[i] = after[i] - before[i]
		}
		return delta
	}
	admin := run(AdminScope(), "boss@node")
	reseller := run(ResellerScope(r.Id), "shop@node")
	if admin != reseller || admin == [5]int32{} {
		t.Fatalf("node calls (add, updateInbound, updateUser, deleteClient, deleteUser): admin %v, reseller %v", admin, reseller)
	}
}
