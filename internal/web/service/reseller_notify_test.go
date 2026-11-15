package service

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
)

func TestPoolThresholdNotifiedOncePerCycle(t *testing.T) {
	setupPoolDB(t)
	bus := eventbus.New(16)
	var published atomic.Int32
	bus.Subscribe("test", func(e eventbus.Event) {
		if e.Type == eventbus.EventResellerPool {
			published.Add(1)
		}
	})
	SetEventBus(bus)
	t.Cleanup(func() { SetEventBus(nil); bus.Stop() })
	if err := (&SettingService{}).SetResellerPoolNotify(true); err != nil {
		t.Fatal(err)
	}
	r := mustCreateReseller(t, resellerPayload("notify"))
	ownEmails(t, r.Id, "n@x")
	attachClient(t, mkInbound(t, 25110, model.VLESS, `{"clients":[]}`), "n@x", 0)
	db := database.GetDB()
	if err := db.Model(&model.Reseller{}).Where("id = ?", r.Id).Updates(map[string]any{
		"pool_bytes": 10 * gb, "used_traffic": 8 * gb,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := bumpResellerPoolNotices(db, []int{r.Id}); err != nil {
		t.Fatal(err)
	}
	if err := bumpResellerPoolNotices(db, []int{r.Id}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for published.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := published.Load(); n != 1 {
		t.Fatalf("%d reseller.pool events, want 1", n)
	}
	var row model.Reseller
	if err := db.First(&row, r.Id).Error; err != nil || row.NotifiedLevel != 80 {
		t.Fatalf("notified_level = %d, want 80", row.NotifiedLevel)
	}
}