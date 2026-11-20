package controller

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"

	"github.com/gin-gonic/gin"
)

func loginActiveAdminRole(c *gin.Context) (*model.User, *model.AdminRole, bool) {
	if c.GetBool("api_authed") {
		pureJsonMsg(c, http.StatusForbidden, false, "browser session required")
		c.Abort()
		return nil, nil, false
	}

	user := session.GetLoginUser(c)
	if user == nil {
		pureJsonMsg(c, http.StatusUnauthorized, false, "login required")
		c.Abort()
		return nil, nil, false
	}

	if user.Status != "" && user.Status != model.AdminStatusActive {
		pureJsonMsg(c, http.StatusForbidden, false, "admin account is disabled")
		c.Abort()
		return nil, nil, false
	}

	db := database.GetDB()
	if db == nil {
		pureJsonMsg(c, http.StatusInternalServerError, false, "database is not initialized")
		c.Abort()
		return nil, nil, false
	}

	var role model.AdminRole
	if err := db.Where("id = ?", user.RoleId).First(&role).Error; err != nil {
		pureJsonMsg(c, http.StatusForbidden, false, "admin role not found")
		c.Abort()
		return nil, nil, false
	}

	return user, &role, true
}

func requireAdminPermission(section string, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, role, ok := loginActiveAdminRole(c)
		if !ok {
			return
		}

		if role.OwnerRole {
			c.Next()
			return
		}

		if !roleAllowsPermission(role, section, permission) {
			pureJsonMsg(c, http.StatusForbidden, false, section+"."+permission+" permission required")
			c.Abort()
			return
		}

		c.Next()
	}
}

func roleAllowsPermission(role *model.AdminRole, section string, permission string) bool {
	if role == nil {
		return false
	}
	if role.OwnerRole {
		return true
	}

	root := map[string]any{}
	if err := json.Unmarshal([]byte(role.PermissionsJSON), &root); err != nil {
		return false
	}

	sectionValue, ok := root[section]
	if !ok {
		return false
	}
	sectionMap, ok := sectionValue.(map[string]any)
	if !ok {
		return false
	}

	for _, permissionKey := range permissionKeys(permission) {
		if permissionValueAllowed(sectionMap[permissionKey]) {
			return true
		}
	}
	return false
}

func permissionKeys(permission string) []string {
	switch permission {
	case "view", "read":
		return []string{"view", "read"}
	case "resetUsage", "reset_usage":
		return []string{"resetUsage", "reset_usage"}
	default:
		return []string{permission}
	}
}

func permissionValueAllowed(value any) bool {
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
		return permissionValueAllowed(v["scope"])
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}