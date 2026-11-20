package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestAdminUsageSyncIncreasesAndDecreasesWithClientTraffic(t *testing.T) {
	db := initTrafficTestDB(t)

	admin := model.User{
		Username:  "quota-admin",
		DataLimit: 8 * 1024 * 1024 * 1024,
		Status:    model.AdminStatusActive,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	client := model.ClientRecord{
		Email:        "quota-client@example.com",
		OwnerAdminId: admin.Id,
		Enable:       true,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	if err := db.Create(&xray.ClientTraffic{
		Email: client.Email,
		Up:    40_000,
		Down:  60_000,
	}).Error; err != nil {
		t.Fatalf("create traffic: %v", err)
	}

	usage, err := (&InboundService{}).SyncAdminUsedBytes()
	if err != nil {
		t.Fatalf("sync usage: %v", err)
	}
	if got, want := usage[admin.Id], int64(100_000); got != want {
		t.Fatalf("used_bytes after traffic = %d, want %d", got, want)
	}

	if err := (&InboundService{}).ResetClientTrafficByEmail(client.Email); err != nil {
		t.Fatalf("reset client traffic: %v", err)
	}

	usage, err = (&InboundService{}).SyncAdminUsedBytes()
	if err != nil {
		t.Fatalf("sync after reset: %v", err)
	}
	if got, want := usage[admin.Id], int64(0); got != want {
		t.Fatalf("used_bytes after reset = %d, want %d", got, want)
	}

	var row model.User
	if err := db.First(&row, admin.Id).Error; err != nil {
		t.Fatalf("reload admin: %v", err)
	}
	if row.UsedBytes != 0 {
		t.Fatalf("persisted used_bytes = %d, want 0", row.UsedBytes)
	}
}

func TestAdminClientScopeIsolatesOwners(t *testing.T) {
	db := initTrafficTestDB(t)

	adminA := model.User{Username: "admin-a", Status: model.AdminStatusActive, RoleId: 1}
	adminB := model.User{Username: "admin-b", Status: model.AdminStatusActive, RoleId: 1}
	if err := db.Create(&adminA).Error; err != nil {
		t.Fatalf("create admin A: %v", err)
	}
	if err := db.Create(&adminB).Error; err != nil {
		t.Fatalf("create admin B: %v", err)
	}

	clientA := model.ClientRecord{Email: "a@example.com", OwnerAdminId: adminA.Id}
	clientB := model.ClientRecord{Email: "b@example.com", OwnerAdminId: adminB.Id}
	if err := db.Create(&clientA).Error; err != nil {
		t.Fatalf("create client A: %v", err)
	}
	if err := db.Create(&clientB).Error; err != nil {
		t.Fatalf("create client B: %v", err)
	}

	svc := &ClientService{}
	scopeA := ClientAccessScope{AdminID: adminA.Id, Mode: ClientAccessOwn}

	if _, err := svc.RequireClientForScopeByEmail(scopeA, clientA.Email); err != nil {
		t.Fatalf("admin A should read own client: %v", err)
	}
	if _, err := svc.RequireClientForScopeByEmail(scopeA, clientB.Email); err == nil {
		t.Fatal("admin A must not read admin B client")
	}
	if !svc.ClientRecordAllowed(scopeA, &clientB) {
		// expected deny
	} else {
		t.Fatal("admin A must not be allowed on admin B client")
	}
}

func TestCreateAdminWithDataLimitPersists(t *testing.T) {
	db := initTrafficTestDB(t)

	var operatorRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOperator).First(&operatorRole).Error; err != nil {
		t.Fatalf("operator role: %v", err)
	}

	admin := model.User{
		Username:  "limited-operator",
		Password:  "hash",
		RoleId:    operatorRole.Id,
		Status:    model.AdminStatusActive,
		DataLimit: 3 * 1024 * 1024 * 1024,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	var got model.User
	if err := db.First(&got, admin.Id).Error; err != nil {
		t.Fatalf("reload admin: %v", err)
	}
	if got.DataLimit != admin.DataLimit {
		t.Fatalf("data_limit = %d, want %d", got.DataLimit, admin.DataLimit)
	}
}