package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A pre-multiplier panel's inbound row must come back as x1 after the upgrade,
// never as 0 (free): the failure an old backup restore would otherwise cause.
func TestMigrateUsageMultiplierLegacyRowIsX1(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "x-ui.db")
	legacy, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := legacy.Exec(legacyInboundNoExcludeFromSubDDL).Error; err != nil {
		t.Fatalf("create legacy inbounds: %v", err)
	}
	if err := legacy.Exec(
		`INSERT INTO inbounds (user_id, remark, enable, port, protocol, settings, stream_settings, tag, sniffing)
		 VALUES (1, 'old', 1, 443, 'vless', '{"clients":[]}', '{}', 'in-443-tcp', '{}')`,
	).Error; err != nil {
		t.Fatalf("seed legacy inbound: %v", err)
	}
	sqlDB, _ := legacy.DB()
	_ = sqlDB.Close()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB over legacy schema: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	m := GetDB().Migrator()
	if !m.HasColumn(&model.Inbound{}, "usage_multiplier") {
		t.Fatal("inbounds.usage_multiplier missing after upgrade")
	}
	if !m.HasTable(&model.ClientInboundTraffic{}) {
		t.Fatal("client_inbound_traffics missing after upgrade")
	}

	var row model.Inbound
	if err := GetDB().Where("tag = ?", "in-443-tcp").First(&row).Error; err != nil {
		t.Fatalf("legacy inbound lost: %v", err)
	}
	if got, _ := row.EffectiveUsageMultiplier(); got != 1 {
		t.Fatalf("legacy inbound multiplier = %v, want 1", got)
	}

	// Second open of the same file: the migration must be a no-op.
	_ = CloseDB()
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("second InitDB (idempotency): %v", err)
	}
	if err := GetDB().Where("tag = ?", "in-443-tcp").First(&row).Error; err != nil {
		t.Fatalf("legacy inbound lost on reopen: %v", err)
	}
	if got, _ := row.EffectiveUsageMultiplier(); got != 1 {
		t.Fatalf("legacy inbound multiplier after reopen = %v, want 1", got)
	}
}

func TestUsageMultiplierPersistence(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	zero, tunnel := 0.0, 1.2
	cases := []struct {
		name string
		in   *float64
		want float64
	}{
		{"omitted field stays x1", nil, 1},
		{"explicit zero is free", &zero, 0},
		{"fractional multiplier kept", &tunnel, 1.2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ib := model.Inbound{UserId: 1, Port: 1000 + i, Protocol: model.VLESS, Tag: tc.name, UsageMultiplier: tc.in}
			if err := GetDB().Create(&ib).Error; err != nil {
				t.Fatalf("create: %v", err)
			}
			var back model.Inbound
			if err := GetDB().First(&back, ib.Id).Error; err != nil {
				t.Fatalf("reload: %v", err)
			}
			if got, _ := back.EffectiveUsageMultiplier(); got != tc.want {
				t.Fatalf("multiplier = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClientInboundTrafficStatEmailUnique(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	row := model.ClientInboundTraffic{ClientId: 1, InboundId: 2, Email: "a@x", StatEmail: "nxm_2_aaaaaaaaaaaaaaaa"}
	if err := GetDB().Create(&row).Error; err != nil {
		t.Fatalf("first insert: %v", err)
	}
	dup := row
	dup.Id = 0
	if err := GetDB().Create(&dup).Error; err == nil {
		t.Fatal("duplicate stat_email accepted; the unique index is missing")
	}
}
