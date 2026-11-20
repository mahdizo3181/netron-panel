package service

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestValidateClientAgainstRoleLimitsDataLimit(t *testing.T) {
	limits := adminRoleClientLimits{
		MinDataLimitSet: true,
		MinDataLimit:    10 * roleLimitBytesPerGB,
		MaxDataLimitSet: true,
		MaxDataLimit:    100 * roleLimitBytesPerGB,
	}

	if err := validateClientAgainstRoleLimits(limits, model.Client{TotalGB: 50 * roleLimitBytesPerGB}, 0); err != nil {
		t.Fatalf("expected data limit inside range to pass: %v", err)
	}
	if err := validateClientAgainstRoleLimits(limits, model.Client{TotalGB: 0}, 0); err == nil {
		t.Fatal("expected unlimited traffic to be denied when data limits are configured")
	}
	if err := validateClientAgainstRoleLimits(limits, model.Client{TotalGB: 5 * roleLimitBytesPerGB}, 0); err == nil {
		t.Fatal("expected data limit below minimum to be denied")
	}
	if err := validateClientAgainstRoleLimits(limits, model.Client{TotalGB: 101 * roleLimitBytesPerGB}, 0); err == nil {
		t.Fatal("expected data limit above maximum to be denied")
	}
}

func TestEnsureAdminMayCreateOrEnableBlocksAtQuota(t *testing.T) {
	db := initTrafficTestDB(t)
	admin := model.User{Username: "full", Status: model.AdminStatusActive, DataLimit: 1000, UsedBytes: 1000, RoleId: 1}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	var opRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOperator).First(&opRole).Error; err != nil {
		t.Fatalf("operator role: %v", err)
	}
	admin.RoleId = opRole.Id
	if err := db.Save(&admin).Error; err != nil {
		t.Fatalf("save admin role: %v", err)
	}

	svc := &ClientService{}
	if err := svc.EnsureAdminMayCreateOrEnable(&admin, 0); err == nil {
		t.Fatal("expected create/enable blocked when admin already at data_limit")
	}
	if err := svc.EnsureAdminMayCreateOrEnable(&admin, 1); err == nil {
		t.Fatal("expected create/enable blocked when over data_limit")
	}
	if err := svc.EnsureAdminMayCreateOrEnable(&admin, 0); err != nil && !strings.Contains(err.Error(), "quota") {
		t.Fatalf("error = %v, want quota message", err)
	}
}

func TestEnsureAdminMayCreateOrEnableRejectsDisabledAdmin(t *testing.T) {
	db := initTrafficTestDB(t)
	admin := model.User{Username: "off", Status: model.AdminStatusDisabled, DataLimit: 1_000_000}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := (&ClientService{}).EnsureAdminMayCreateOrEnable(&admin, 1); err == nil {
		t.Fatal("expected disabled admin rejected")
	}
}