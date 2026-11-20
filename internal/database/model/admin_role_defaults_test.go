package model

import (
	"encoding/json"
	"testing"
)

func TestDefaultAdminRolesContainBuiltInSlugs(t *testing.T) {
	roles := DefaultAdminRoles()
	if len(roles) != 3 {
		t.Fatalf("default roles count = %d, want 3", len(roles))
	}
	seen := map[string]AdminRole{}
	for _, role := range roles {
		if !role.BuiltIn {
			t.Fatalf("role %q must be built-in", role.Slug)
		}
		seen[role.Slug] = role
	}
	for _, slug := range []string{AdminRoleSlugOwner, AdminRoleSlugAdministrator, AdminRoleSlugOperator} {
		if _, ok := seen[slug]; !ok {
			t.Fatalf("missing built-in role %q", slug)
		}
	}
	if !seen[AdminRoleSlugOwner].OwnerRole {
		t.Fatal("owner role must have ownerRole=true")
	}
}

func TestDefaultRoleLimitsIncludeQuotaKeys(t *testing.T) {
	limits := defaultRoleLimits()
	keys := []string{"maxUsers", "minDataLimit", "maxDataLimit", "minExpireDays", "maxExpireDays"}
	for _, key := range keys {
		if _, ok := limits[key]; !ok {
			t.Fatalf("limits missing key %q", key)
		}
	}
}

func TestOwnerRoleGrantsAdminManagement(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal([]byte(mustRoleJSON(ownerPermissions())), &root); err != nil {
		t.Fatalf("unmarshal permissions: %v", err)
	}
	admins, ok := root["admins"].(map[string]any)
	if !ok {
		t.Fatal("owner admins permissions missing")
	}
	for _, key := range []string{"create", "update", "delete", "reset_usage"} {
		if v, ok := admins[key]; !ok || v != true {
			t.Fatalf("owner admins.%s = %v, want true", key, v)
		}
	}
}