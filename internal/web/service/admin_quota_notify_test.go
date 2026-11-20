package service

import (
	"sync/atomic"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAdminQuota80NotifyFiresOncePerThresholdPeriod(t *testing.T) {
	db := initTrafficTestDB(t)

	var sends atomic.Int32
	prev := AdminQuotaNotifySend
	AdminQuotaNotifySend = func(_ int, _ int64, kind string, _, _ int64) bool {
		if kind == "80" {
			sends.Add(1)
		}
		return true
	}
	t.Cleanup(func() { AdminQuotaNotifySend = prev })

	admin := model.User{
		Username:   "tg-op",
		Status:     model.AdminStatusActive,
		DataLimit:  1000,
		UsedBytes:  850,
		TelegramID: "424242",
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	svc := &InboundService{}
	if err := svc.SyncAdminQuotaTelegramNotices(); err != nil {
		t.Fatalf("first notify: %v", err)
	}
	if err := svc.SyncAdminQuotaTelegramNotices(); err != nil {
		t.Fatalf("second notify: %v", err)
	}
	if got := sends.Load(); got != 1 {
		t.Fatalf("80%% sends = %d, want 1 (same threshold period)", got)
	}

	var row model.User
	if err := database.GetDB().Where("id = ?", admin.Id).First(&row).Error; err != nil {
		t.Fatalf("reload admin: %v", err)
	}
	if !row.QuotaWarn80Sent {
		t.Fatal("expected quota_warn_80_sent after first notify")
	}
}

func TestAdminQuotaNotifyResetsBelow80(t *testing.T) {
	db := initTrafficTestDB(t)

	admin := model.User{
		Username:         "tg-op2",
		Status:           model.AdminStatusActive,
		DataLimit:        1000,
		UsedBytes:        500,
		TelegramID:       "1",
		QuotaWarn80Sent:  true,
		QuotaWarn100Sent: true,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	svc := &InboundService{}
	if err := svc.SyncAdminQuotaTelegramNotices(); err != nil {
		t.Fatalf("notify: %v", err)
	}

	var row model.User
	if err := db.Where("id = ?", admin.Id).First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.QuotaWarn80Sent || row.QuotaWarn100Sent {
		t.Fatalf("flags = 80:%v 100:%v, want both false below 80%%", row.QuotaWarn80Sent, row.QuotaWarn100Sent)
	}
}