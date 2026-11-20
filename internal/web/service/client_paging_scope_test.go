package service

import (
	"strconv"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestListPagedScopesByOwnerAdmin(t *testing.T) {
	db := initTrafficTestDB(t)
	inboundSvc := &InboundService{}
	settingSvc := &SettingService{}
	svc := &ClientService{}

	adminA := model.User{Username: "a", Status: model.AdminStatusActive}
	adminB := model.User{Username: "b", Status: model.AdminStatusActive}
	if err := db.Create(&adminA).Error; err != nil {
		t.Fatalf("admin A: %v", err)
	}
	if err := db.Create(&adminB).Error; err != nil {
		t.Fatalf("admin B: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "own-a@x", OwnerAdminId: adminA.Id}).Error; err != nil {
		t.Fatalf("client A: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "own-b@x", OwnerAdminId: adminB.Id}).Error; err != nil {
		t.Fatalf("client B: %v", err)
	}

	respA, err := svc.ListPaged(inboundSvc, settingSvc, ClientPageParams{PageSize: 50}, ClientAccessScope{AdminID: adminA.Id, Mode: ClientAccessOwn})
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if respA.Total != 1 || len(respA.Items) != 1 || respA.Items[0].Email != "own-a@x" {
		t.Fatalf("admin A list = %+v, want only own-a@x", respA.Items)
	}

	respB, err := svc.ListPaged(inboundSvc, settingSvc, ClientPageParams{PageSize: 50}, ClientAccessScope{AdminID: adminB.Id, Mode: ClientAccessOwn})
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if respB.Total != 1 || respB.Items[0].Email != "own-b@x" {
		t.Fatalf("admin B list = %+v, want own-b@x", respB.Items)
	}
}

func TestListPagedOwnerFilterNarrowsRows(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &ClientService{}
	adminA := model.User{Username: "fa", Status: model.AdminStatusActive}
	adminB := model.User{Username: "fb", Status: model.AdminStatusActive}
	db.Create(&adminA)
	db.Create(&adminB)
	db.Create(&model.ClientRecord{Email: "c-a@x", OwnerAdminId: adminA.Id})
	db.Create(&model.ClientRecord{Email: "c-b@x", OwnerAdminId: adminB.Id})

	params := ClientPageParams{
		PageSize:         50,
		Owner:            strconv.Itoa(adminA.Id),
		AllowOwnerFilter: true,
	}
	resp, err := svc.ListPaged(&InboundService{}, &SettingService{}, params, ClientAccessScope{Mode: ClientAccessAll})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if resp.Filtered != 1 || len(resp.Items) != 1 || resp.Items[0].Email != "c-a@x" {
		t.Fatalf("filtered=%d items=%+v, want c-a@x only", resp.Filtered, resp.Items)
	}
}

func TestListScopesByOwnerAdmin(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &ClientService{}

	adminA := model.User{Username: "a", Status: model.AdminStatusActive}
	adminB := model.User{Username: "b", Status: model.AdminStatusActive}
	if err := db.Create(&adminA).Error; err != nil {
		t.Fatalf("admin A: %v", err)
	}
	if err := db.Create(&adminB).Error; err != nil {
		t.Fatalf("admin B: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "own-a@x", OwnerAdminId: adminA.Id}).Error; err != nil {
		t.Fatalf("client A: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "own-b@x", OwnerAdminId: adminB.Id}).Error; err != nil {
		t.Fatalf("client B: %v", err)
	}

	listA, err := svc.List(ClientAccessScope{AdminID: adminA.Id, Mode: ClientAccessOwn})
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(listA) != 1 || listA[0].Email != "own-a@x" {
		t.Fatalf("admin A list = %+v, want only own-a@x", listA)
	}
	for _, row := range listA {
		if row.Email == "own-b@x" {
			t.Fatalf("admin A must not see own-b@x")
		}
	}

	listAll, err := svc.List(ClientAccessScope{Mode: ClientAccessAll})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(listAll) != 2 {
		t.Fatalf("owner list len=%d, want 2", len(listAll))
	}
}
