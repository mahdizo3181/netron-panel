package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestSyncEnforceOnLiveDB(t *testing.T) {
	if os.Getenv("XUI_SYNC_ENFORCE") == "" {
		t.Skip("set XUI_SYNC_ENFORCE=1 with XUI_DB_FOLDER for live quota sync")
	}
	folder := os.Getenv("XUI_DB_FOLDER")
	if folder == "" {
		t.Fatal("XUI_DB_FOLDER is required")
	}
	if err := database.InitDB(filepath.Join(folder, "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if _, _, err := (&InboundService{}).SyncAndEnforceAdminUsageLimits(); err != nil {
		t.Fatalf("SyncAndEnforceAdminUsageLimits: %v", err)
	}
}

func TestCreateClientRejectedWhenMaxUsersReached(t *testing.T) {
	db := initTrafficTestDB(t)

	var opRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOperator).First(&opRole).Error; err != nil {
		t.Fatalf("operator role: %v", err)
	}
	opRole.LimitsJSON = `{"maxUsers":1}`
	if err := db.Save(&opRole).Error; err != nil {
		t.Fatalf("update role limits: %v", err)
	}

	admin := model.User{Username: "op1", Status: model.AdminStatusActive, RoleId: opRole.Id}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	if err := db.Create(&model.ClientRecord{Email: "existing@x", OwnerAdminId: admin.Id}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	svc := &ClientService{}
	err := svc.ValidateClientLimitsForAdmin(&admin, model.Client{Email: "new@x", TotalGB: 1 * roleLimitBytesPerGB}, true)
	if err == nil {
		t.Fatal("expected max users rejection")
	}
}

func TestQuotaDisableThenSelectiveReenable(t *testing.T) {
	db := initTrafficTestDB(t)

	admin := model.User{
		Username:  "quota-op",
		Status:    model.AdminStatusActive,
		DataLimit: 500,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	const emailQuota = "quota-a@x"
	const emailManual = "manual-off@x"

	if err := db.Create(&model.ClientRecord{Email: emailQuota, OwnerAdminId: admin.Id, Enable: true}).Error; err != nil {
		t.Fatalf("create quota client: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: emailManual, OwnerAdminId: admin.Id}).Error; err != nil {
		t.Fatalf("create manual client: %v", err)
	}
	if err := db.Model(&model.ClientRecord{}).Where("email = ?", emailManual).Update("enable", false).Error; err != nil {
		t.Fatalf("disable manual client: %v", err)
	}
	for _, email := range []string{emailQuota, emailManual} {
		if err := db.Create(&xray.ClientTraffic{Email: email, Up: 300, Down: 0, Enable: true}).Error; err != nil {
			t.Fatalf("traffic %s: %v", email, err)
		}
	}
	if err := db.Model(&model.User{}).Where("id = ?", admin.Id).Update("used_bytes", 600).Error; err != nil {
		t.Fatalf("seed used_bytes: %v", err)
	}

	inboundSvc := &InboundService{}
	if _, _, err := inboundSvc.SyncAndEnforceAdminUsageLimits(); err != nil {
		t.Fatalf("enforce: %v", err)
	}

	var quotaRec model.ClientRecord
	if err := db.Where("email = ?", emailQuota).First(&quotaRec).Error; err != nil {
		t.Fatalf("read quota client: %v", err)
	}
	if quotaRec.Enable || quotaRec.DisabledByOwnerAdminId != admin.Id {
		t.Fatalf("quota client should be quota-disabled: enable=%v marker=%d", quotaRec.Enable, quotaRec.DisabledByOwnerAdminId)
	}

	var manualRec model.ClientRecord
	if err := db.Where("email = ?", emailManual).First(&manualRec).Error; err != nil {
		t.Fatalf("read manual client: %v", err)
	}
	if manualRec.Enable {
		t.Fatal("manually disabled client must stay off after quota enforcement")
	}

	if err := db.Model(&model.User{}).Where("id = ?", admin.Id).Updates(map[string]any{"data_limit": 10_000, "used_bytes": 300}).Error; err != nil {
		t.Fatalf("raise quota: %v", err)
	}
	if _, n, err := inboundSvc.EnableClientsDisabledByOwnerAdminID(admin.Id); err != nil {
		t.Fatalf("reenable: %v", err)
	} else if n != 1 {
		t.Fatalf("reenabled count = %d, want 1", n)
	}

	if err := db.Where("email = ?", emailQuota).First(&quotaRec).Error; err != nil {
		t.Fatalf("read quota client after reenable: %v", err)
	}
	if !quotaRec.Enable {
		t.Fatal("quota-disabled client should be back on after headroom")
	}
	if err := db.Where("email = ?", emailManual).First(&manualRec).Error; err != nil {
		t.Fatalf("read manual client after reenable: %v", err)
	}
	if manualRec.Enable {
		t.Fatal("manually disabled client must remain off after quota reenable")
	}
}

func TestAdminQuotaBlocksCreateWhenFull(t *testing.T) {
	db := initTrafficTestDB(t)
	admin := model.User{Username: "full-admin", Status: model.AdminStatusActive, DataLimit: 1000, UsedBytes: 950}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	svc := &ClientService{}
	if err := svc.EnsureAdminMayCreateOrEnable(&admin, 100); err == nil {
		t.Fatal("expected create blocked when quota exceeded")
	}
}