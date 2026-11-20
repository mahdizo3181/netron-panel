package service

import (
	"encoding/json"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func adminRoleSectionValue(role *model.AdminRole, section string) map[string]any {
	if role == nil {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(role.PermissionsJSON), &root); err != nil {
		return nil
	}
	raw, ok := root[section].(map[string]any)
	if !ok {
		return nil
	}
	return raw
}

func adminPermissionValueAllowed(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "1", "own", "all":
			return true
		default:
			return false
		}
	case map[string]any:
		return adminPermissionValueAllowed(v["scope"])
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}

func adminRoleAllowsPermission(role *model.AdminRole, section, permission string) bool {
	if role != nil && role.OwnerRole {
		return true
	}
	sectionMap := adminRoleSectionValue(role, section)
	if sectionMap == nil {
		return false
	}
	keys := []string{permission}
	switch permission {
	case "read", "view":
		keys = []string{"view", "read"}
	case "read_simple":
		keys = []string{"read_simple", "read"}
	}
	for _, key := range keys {
		if adminPermissionValueAllowed(sectionMap[key]) {
			return true
		}
	}
	return false
}

// Resellers pick from panel inbounds when creating clients; they do not own
// those rows in the inbounds table.
func inboundOptionsListAllInbounds(role *model.AdminRole) bool {
	if role != nil && role.OwnerRole {
		return true
	}
	if adminRoleAllowsPermission(role, "inbounds", "read") ||
		adminRoleAllowsPermission(role, "inbounds", "read_simple") {
		return true
	}
	return adminRoleAllowsPermission(role, "users", "create")
}