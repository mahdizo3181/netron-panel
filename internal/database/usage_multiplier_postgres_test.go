package database

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/testpg"
)

const legacyPostgresInboundsDDL = `CREATE TABLE inbounds (id bigserial PRIMARY KEY, user_id bigint, up bigint, down bigint, total bigint, remark text, enable boolean, expiry_time bigint, listen text, port bigint, protocol text, settings text, stream_settings text, tag text UNIQUE, sniffing text)`

// The upgrade must add the column with x1 for existing rows and stay a no-op on
// the next start, on Postgres too.
func TestMigrateUsageMultiplierPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("XUI_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set XUI_TEST_PG_DSN to a reachable Postgres to run this test")
	}
	t.Setenv("XUI_DB_TYPE", "postgres")
	t.Setenv("XUI_DB_DSN", dsn)
	cleanup, err := testpg.IsolatePackage("usage_mult_migration")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	legacy, err := gorm.Open(postgres.Open(os.Getenv("XUI_DB_DSN")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(legacyPostgresInboundsDDL).Error; err != nil {
		t.Fatalf("legacy table: %v", err)
	}
	if err := legacy.Exec(`INSERT INTO inbounds (user_id, remark, enable, port, protocol, settings, stream_settings, tag, sniffing) VALUES (1, 'old', true, 443, 'vless', '{"clients":[]}', '{}', 'pg-old', '{}')`).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := legacy.DB()
	_ = sqlDB.Close()

	for pass := 1; pass <= 2; pass++ {
		if err := InitDB(""); err != nil {
			t.Fatalf("InitDB pass %d: %v", pass, err)
		}
		var row model.Inbound
		if err := GetDB().Where("tag = ?", "pg-old").First(&row).Error; err != nil {
			t.Fatalf("pass %d: legacy row lost: %v", pass, err)
		}
		if m, _ := row.EffectiveUsageMultiplier(); m != 1 {
			t.Fatalf("pass %d: legacy row multiplier = %v, want 1", pass, m)
		}
		if !GetDB().Migrator().HasTable(&model.ClientInboundTraffic{}) {
			t.Fatalf("pass %d: client_inbound_traffics missing", pass)
		}
		_ = CloseDB()
	}
}
