package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestGetInboundOptionsOperatorSeesOwnerInbounds(t *testing.T) {
	db := initTrafficTestDB(t)

	var ownerRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOwner).First(&ownerRole).Error; err != nil {
		t.Fatalf("owner role: %v", err)
	}
	var opRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOperator).First(&opRole).Error; err != nil {
		t.Fatalf("operator role: %v", err)
	}
	owner := model.User{Username: "owner", RoleId: ownerRole.Id, Status: model.AdminStatusActive}
	operator := model.User{Username: "reseller", RoleId: opRole.Id, Status: model.AdminStatusActive}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := db.Create(&operator).Error; err != nil {
		t.Fatalf("create operator: %v", err)
	}
	ownerInbound := model.Inbound{
		UserId:   owner.Id,
		Tag:      "in-10443-tcp",
		Enable:   true,
		Port:     10443,
		Protocol: model.VLESS,
		Settings: `{"clients":[],"decryption":"none"}`,
	}
	if err := db.Create(&ownerInbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	svc := &InboundService{}
	options, err := svc.GetInboundOptions(operator.Id)
	if err != nil {
		t.Fatalf("GetInboundOptions operator: %v", err)
	}
	if len(options) != 1 || options[0].Id != ownerInbound.Id {
		t.Fatalf("operator options = %+v, want inbound id %d", options, ownerInbound.Id)
	}

	ownedOnly, err := svc.GetInboundOptions(owner.Id)
	if err != nil {
		t.Fatalf("GetInboundOptions owner: %v", err)
	}
	if len(ownedOnly) != 1 {
		t.Fatalf("owner options len = %d, want 1", len(ownedOnly))
	}
}