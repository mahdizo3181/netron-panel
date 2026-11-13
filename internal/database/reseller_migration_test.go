package database

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func copyStockFixture(t *testing.T, name string) string {
	t.Helper()
	src, err := os.Open(filepath.Join("..", "..", "deploy", "test", "upgrade", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	path := filepath.Join(t.TempDir(), "x-ui.db")
	dst, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	return path
}

// Upgrading a stock panel must leave every existing client owned by the admin
// and not cut off; a non-NULL owner would hide clients from nobody today but bill a reseller later.
func TestResellerMigrationLeavesStockClientsWithAdmin(t *testing.T) {
	for _, name := range []string{"stock-3.10.2.db", "stock-3.11.0.db"} {
		t.Run(name, func(t *testing.T) {
			path := copyStockFixture(t, name)
			if err := InitDB(path); err != nil {
				t.Fatalf("InitDB over %s: %v", name, err)
			}
			t.Cleanup(func() { _ = CloseDB() })

			m := GetDB().Migrator()
			for _, table := range []any{&model.Reseller{}, &model.ResellerInbound{}, &model.ResellerAuditLog{}} {
				if !m.HasTable(table) {
					t.Fatalf("table for %T missing after upgrade", table)
				}
			}
			var clients, owned, cutOff, resellers int64
			GetDB().Model(&model.ClientRecord{}).Count(&clients)
			GetDB().Model(&model.ClientRecord{}).Where("reseller_id IS NOT NULL").Count(&owned)
			GetDB().Model(&model.ClientRecord{}).Where("pool_disabled = ?", true).Count(&cutOff)
			GetDB().Model(&model.Reseller{}).Count(&resellers)
			if clients == 0 {
				t.Fatalf("fixture %s has no clients; the test would prove nothing", name)
			}
			if owned != 0 || cutOff != 0 || resellers != 0 {
				t.Fatalf("after upgrade: %d of %d clients owned, %d cut off, %d resellers; want 0, 0, 0", owned, clients, cutOff, resellers)
			}

			// Second open: the migration must be a no-op.
			_ = CloseDB()
			if err := InitDB(path); err != nil {
				t.Fatalf("second InitDB: %v", err)
			}
		})
	}
}
