package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestResellerAuditRetentionPrunesOldRows(t *testing.T) {
	setupResellerDB(t)
	db := database.GetDB()
	old := time.Now().AddDate(0, 0, -200).UnixMilli()
	for _, action := range []string{"cutoff", "release"} {
		if err := recordResellerAudit(db, systemActor, 1, action, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if res := db.Model(&model.ResellerAuditLog{}).Where("id > 0").Update("at", old); res.Error != nil || res.RowsAffected != 2 {
		t.Fatalf("stamp at: affected %d err %v", res.RowsAffected, res.Error)
	}
	if err := PruneResellerAuditLogs(); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&model.ResellerAuditLog{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d audit rows left after prune, want 0", n)
	}
}

func TestResellerAuditScopedToOwnRows(t *testing.T) {
	setupResellerDB(t)
	a := mustCreateReseller(t, resellerPayload("a-shop"))
	b := mustCreateReseller(t, resellerPayload("b-shop"))
	db := database.GetDB()
	if err := recordResellerAudit(db, systemActor, a.Id, "cutoff", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := recordResellerAudit(db, systemActor, b.Id, "cutoff", "", nil); err != nil {
		t.Fatal(err)
	}
	page, err := (&ResellerService{}).ListAudit(ResellerScope(a.Id), ResellerAuditPageParams{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Rows) != 1 || page.Rows[0].ResellerId != a.Id {
		t.Fatalf("reseller A sees %+v, want one own row", page)
	}
}