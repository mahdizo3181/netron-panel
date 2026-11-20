package model

import "encoding/json"

const (
	AdminStatusActive   = "active"
	AdminStatusDisabled = "disabled"

	AdminRoleSlugOwner         = "owner"
	AdminRoleSlugAdministrator = "administrator"
	AdminRoleSlugOperator      = "operator"
)

type AdminRole struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name            string `json:"name" gorm:"uniqueIndex;not null"`
	Slug            string `json:"slug" gorm:"uniqueIndex;not null"`
	BuiltIn         bool   `json:"builtIn" gorm:"column:built_in;default:false"`
	OwnerRole       bool   `json:"ownerRole" gorm:"column:owner_role;default:false"`
	PermissionsJSON string `json:"permissions" gorm:"column:permissions;type:text"`
	LimitsJSON      string `json:"limits" gorm:"column:limits;type:text"`
	CreatedAt       int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt       int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (AdminRole) TableName() string { return "admin_roles" }

func DefaultAdminRoles() []AdminRole {
	return []AdminRole{
		{
			Name:            "owner",
			Slug:            AdminRoleSlugOwner,
			BuiltIn:         true,
			OwnerRole:       true,
			PermissionsJSON: mustRoleJSON(ownerPermissions()),
			LimitsJSON:      mustRoleJSON(defaultRoleLimits()),
		},
		{
			Name:            "Administrator",
			Slug:            AdminRoleSlugAdministrator,
			BuiltIn:         true,
			OwnerRole:       false,
			PermissionsJSON: mustRoleJSON(administratorPermissions()),
			LimitsJSON:      mustRoleJSON(defaultRoleLimits()),
		},
		{
			Name:            "Operator",
			Slug:            AdminRoleSlugOperator,
			BuiltIn:         true,
			OwnerRole:       false,
			PermissionsJSON: mustRoleJSON(operatorPermissions()),
			LimitsJSON:      mustRoleJSON(defaultRoleLimits()),
		},
	}
}

func mustRoleJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func ownerPermissions() map[string]any {
	return map[string]any{
		"users": map[string]any{
			"read":   "all",
			"create": true,
			"update": "all",
			"delete": "all",
		},
		"admins": map[string]any{
			"read":         true,
			"create":       true,
			"update":       true,
			"delete":       true,
			"reset_usage":  true,
		},
	}
}

func administratorPermissions() map[string]any {
	return map[string]any{
		"users": map[string]any{
			"read":   map[string]any{"scope": 1},
			"create": true,
			"update": map[string]any{"scope": 1},
			"delete": map[string]any{"scope": 1},
		},
		"admins": map[string]any{
			"read":         true,
			"create":       true,
			"update":       true,
			"delete":       true,
			"reset_usage":  true,
		},
	}
}

func operatorPermissions() map[string]any {
	return map[string]any{
		"users": map[string]any{
			"read":   map[string]any{"scope": 1},
			"create": true,
			"update": map[string]any{"scope": 1},
			"delete": map[string]any{"scope": 1},
		},
	}
}

func defaultRoleLimits() map[string]any {
	return map[string]any{
		"maxUsers":      nil,
		"minDataLimit":  nil,
		"maxDataLimit":  nil,
		"minExpireDays": nil,
		"maxExpireDays": nil,
	}
}