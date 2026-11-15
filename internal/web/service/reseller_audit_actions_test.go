package service

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func auditRows(t *testing.T, resellerID int, action string) []model.ResellerAuditLog {
	t.Helper()
	return auditRowsWhere(t, "reseller_id = ? AND action = ?", resellerID, action)
}

func auditRowsWhere(t *testing.T, query string, args ...any) []model.ResellerAuditLog {
	t.Helper()
	var rows []model.ResellerAuditLog
	if err := database.GetDB().Where(query, args...).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func assertNoSecretsInAudit(t *testing.T, rows []model.ResellerAuditLog) {
	t.Helper()
	for _, r := range rows {
		if auditContainsSecret(r.Detail) || auditContainsSecret(r.Target) {
			t.Fatalf("audit row %d leaks a credential: detail=%q target=%q", r.Id, r.Detail, r.Target)
		}
		if strings.Contains(strings.ToLower(r.Detail), resellerTestPassword) {
			t.Fatalf("audit detail contains the test password: %q", r.Detail)
		}
	}
}

func TestAuditResellerClientCreateAndDelete(t *testing.T) {
	setupResellerDB(t)
	ib := mkInbound(t, 25201, model.VLESS, `{"clients":[]}`)
	sc := ResellerScope(mustCreateReseller(t, resellerPayload("aud-shop", ib.Id)).Id)
	sc.Actor = AuditActor{Kind: "reseller", Id: *sc.ResellerID}
	isvc := &InboundService{}
	mustCreateFor(t, sc, newClientPayload("aud@shop", ib.Id))
	rows := auditRowsWhere(t, "reseller_id = ? AND action = ? AND target = ?", *sc.ResellerID, "create", "aud@shop")
	if len(rows) != 1 {
		t.Fatalf("create audit = %+v, want one row for aud@shop", rows)
	}
	assertNoSecretsInAudit(t, rows)
	if _, err := (&ClientService{}).DeleteByEmailFor(sc, isvc, "aud@shop", false); err != nil {
		t.Fatal(err)
	}
	del := auditRows(t, *sc.ResellerID, "delete")
	if len(del) != 1 {
		t.Fatalf("%d delete audit rows, want 1", len(del))
	}
	assertNoSecretsInAudit(t, del)
}

func TestAuditAdminCreatesResellerAndSetPassword(t *testing.T) {
	setupResellerDB(t)
	ib := mkInbound(t, 25202, model.VLESS, `{"clients":[]}`)
	view, err := (&ResellerService{}).CreateAs(testAdmin, resellerPayload("aud-admin", ib.Id))
	if err != nil {
		t.Fatal(err)
	}
	rows := auditRows(t, view.Id, "create")
	if len(rows) != 1 {
		t.Fatalf("%d create audit rows, want 1", len(rows))
	}
	assertNoSecretsInAudit(t, rows)
	if err := (&ResellerService{}).SetPasswordAs(testAdmin, view.Id, "new-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	pw := auditRows(t, view.Id, "setPassword")
	if len(pw) != 1 || pw[0].Detail != "" {
		t.Fatalf("setPassword audit = %+v, want empty detail", pw[0])
	}
	assertNoSecretsInAudit(t, pw)
}

func TestAuditSetOwnerWritesBothResellers(t *testing.T) {
	setupResellerDB(t)
	ib := mkInbound(t, 25203, model.VLESS, `{"clients":[]}`)
	ra := mustCreateReseller(t, resellerPayload("own-a", ib.Id))
	rb := mustCreateReseller(t, resellerPayload("own-b", ib.Id))
	mustCreateFor(t, AdminScope(), newClientPayload("move@aud", ib.Id))
	svc := &ClientService{}
	if _, err := svc.SetOwner(testAdmin, "move@aud", &ra.Id); err != nil {
		t.Fatal(err)
	}
	if len(auditRows(t, ra.Id, "setOwner")) != 1 {
		t.Fatal("missing audit on new owner")
	}
	if _, err := svc.SetOwner(testAdmin, "move@aud", &rb.Id); err != nil {
		t.Fatal(err)
	}
	assertNoSecretsInAudit(t, auditRows(t, ra.Id, "setOwner"))
	assertNoSecretsInAudit(t, auditRows(t, rb.Id, "setOwner"))
}

func TestAuditResellerBulkAndResetTraffic(t *testing.T) {
	setupResellerDB(t)
	ib := mkInbound(t, 25204, model.VLESS, `{"clients":[]}`)
	view := mustCreateReseller(t, resellerPayload("aud-bulk", ib.Id))
	sc := ResellerScope(view.Id)
	sc.Actor = AuditActor{Kind: "reseller", Id: view.Id}
	isvc := &InboundService{}
	svc := &ClientService{}
	mustCreateFor(t, sc, newClientPayload("bulk1@shop", ib.Id))
	mustCreateFor(t, sc, newClientPayload("bulk2@shop", ib.Id))
	if _, _, err := svc.BulkSetEnableFor(sc, isvc, []string{"bulk1@shop", "bulk2@shop"}, false); err != nil {
		t.Fatal(err)
	}
	if len(auditRows(t, view.Id, "bulkDisable")) != 1 {
		t.Fatal("missing bulkDisable audit")
	}
	if _, _, err := svc.BulkSetEnableFor(sc, isvc, []string{"bulk1@shop"}, true); err != nil {
		t.Fatal(err)
	}
	if len(auditRows(t, view.Id, "bulkEnable")) != 1 {
		t.Fatal("missing bulkEnable audit")
	}
	if _, err := svc.ResetTrafficByEmailFor(sc, isvc, "bulk1@shop"); err != nil {
		t.Fatal(err)
	}
	rt := auditRowsWhere(t, "reseller_id = ? AND action = ? AND target = ?", view.Id, "resetTraffic", "bulk1@shop")
	if len(rt) != 1 {
		t.Fatalf("resetTraffic audit = %+v, want one row", rt)
	}
	assertNoSecretsInAudit(t, auditRows(t, view.Id, "bulkDisable"))
	assertNoSecretsInAudit(t, auditRows(t, view.Id, "bulkEnable"))
	assertNoSecretsInAudit(t, rt)
}

func TestAuditAdminDeleteReseller(t *testing.T) {
	setupResellerDB(t)
	ib := mkInbound(t, 25205, model.VLESS, `{"clients":[]}`)
	view, err := (&ResellerService{}).CreateAs(testAdmin, resellerPayload("del-aud", ib.Id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&ResellerService{}).DeleteAs(testAdmin, &InboundService{}, view.Id, ResellerDeleteTransfer); err != nil {
		t.Fatal(err)
	}
	del := auditRows(t, view.Id, "delete")
	if len(del) != 1 || del[0].Detail == "" {
		t.Fatalf("delete audit = %+v, want one row with client mode in detail", del)
	}
	assertNoSecretsInAudit(t, del)
}
