package database

import (
	"path/filepath"
	"testing"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestInitDBSeedsRBACRoles(t *testing.T) {
	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	var n int64
	if err := db.Model(&model.AdminRole{}).Count(&n).Error; err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if n != 3 {
		var seeders []string
		_ = db.Model(&model.HistoryOfSeeders{}).Pluck("seeder_name", &seeders).Error
		t.Fatalf("admin_roles = %d, want 3; seeders=%v", n, seeders)
	}
}

func TestEnsureFirstUserGetsOwnerRole(t *testing.T) {
	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	if _, _, err := EnsureFirstUser(); err != nil {
		t.Fatalf("EnsureFirstUser: %v", err)
	}
	var owner model.User
	if err := db.Order("id ASC").First(&owner).Error; err != nil {
		t.Fatalf("read user: %v", err)
	}
	var role model.AdminRole
	if err := db.Where("id = ?", owner.RoleId).First(&role).Error; err != nil || !role.OwnerRole {
		t.Fatalf("first user role_id=%d owner=%v, want owner role", owner.RoleId, role.OwnerRole)
	}
}
