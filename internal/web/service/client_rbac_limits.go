package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const (
	roleLimitBytesPerGB = 1073741824
	roleLimitDayMillis  = 86400000
)

type adminRoleClientLimits struct {
	MaxUsersSet     bool
	MaxUsers        int64
	MinDataLimitSet bool
	MinDataLimit    int64
	MaxDataLimitSet bool
	MaxDataLimit    int64
	MinExpireDaysSet bool
	MinExpireDays   int64
	MaxExpireDaysSet bool
	MaxExpireDays   int64
}

func positiveInt64RoleLimit(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return int64(t), true
		}
	case int:
		if t > 0 {
			return int64(t), true
		}
	case int64:
		if t > 0 {
			return t, true
		}
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return 0, false
		}
		var n int64
		for _, r := range t {
			if r < '0' || r > '9' {
				return 0, false
			}
			n = n*10 + int64(r-'0')
		}
		if n > 0 {
			return n, true
		}
	}
	return 0, false
}

func roleClientLimits(role *model.AdminRole) adminRoleClientLimits {
	var limits adminRoleClientLimits
	if role == nil || strings.TrimSpace(role.LimitsJSON) == "" {
		return limits
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(role.LimitsJSON), &root); err != nil {
		return limits
	}
	limits.MaxUsers, limits.MaxUsersSet = positiveInt64RoleLimit(root["maxUsers"])
	if n, ok := positiveInt64RoleLimit(root["minDataLimit"]); ok {
		limits.MinDataLimit = n * roleLimitBytesPerGB
		limits.MinDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["maxDataLimit"]); ok {
		limits.MaxDataLimit = n * roleLimitBytesPerGB
		limits.MaxDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["data_limit_min"]); ok {
		limits.MinDataLimit = n
		limits.MinDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["data_limit_max"]); ok {
		limits.MaxDataLimit = n
		limits.MaxDataLimitSet = true
	}
	limits.MinExpireDays, limits.MinExpireDaysSet = positiveInt64RoleLimit(root["minExpireDays"])
	limits.MaxExpireDays, limits.MaxExpireDaysSet = positiveInt64RoleLimit(root["maxExpireDays"])
	return limits
}

func applyAdminPermissionOverrides(limits adminRoleClientLimits, user *model.User) adminRoleClientLimits {
	if user == nil || strings.TrimSpace(user.PermissionOverridesJSON) == "" {
		return limits
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(user.PermissionOverridesJSON), &root); err != nil {
		return limits
	}
	if n, ok := positiveInt64RoleLimit(root["max_users"]); ok {
		limits.MaxUsers = n
		limits.MaxUsersSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["minDataLimit"]); ok {
		limits.MinDataLimit = n * roleLimitBytesPerGB
		limits.MinDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["maxDataLimit"]); ok {
		limits.MaxDataLimit = n * roleLimitBytesPerGB
		limits.MaxDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["data_limit_min"]); ok {
		limits.MinDataLimit = n
		limits.MinDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["data_limit_max"]); ok {
		limits.MaxDataLimit = n
		limits.MaxDataLimitSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["expire_days_min"]); ok {
		limits.MinExpireDays = n
		limits.MinExpireDaysSet = true
	}
	if n, ok := positiveInt64RoleLimit(root["expire_days_max"]); ok {
		limits.MaxExpireDays = n
		limits.MaxExpireDaysSet = true
	}
	return limits
}

func clientRoleLimitDurationMillis(expiryTime int64, nowMs int64) (int64, bool) {
	if expiryTime == 0 {
		return 0, false
	}
	if expiryTime < 0 {
		return -expiryTime, true
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	if expiryTime <= nowMs {
		return expiryTime - nowMs, true
	}
	return expiryTime - nowMs, true
}

func validateClientAgainstRoleLimits(limits adminRoleClientLimits, client model.Client, nowMs int64) error {
	if limits.MinDataLimitSet || limits.MaxDataLimitSet {
		if client.TotalGB <= 0 {
			return common.NewError("client traffic limit is required for this account")
		}
		if limits.MinDataLimitSet && client.TotalGB < limits.MinDataLimit {
			return common.NewError("client traffic below minimum allowed for this account")
		}
		if limits.MaxDataLimitSet && client.TotalGB > limits.MaxDataLimit {
			return common.NewError("client traffic above maximum allowed for this account")
		}
	}
	if limits.MinExpireDaysSet || limits.MaxExpireDaysSet {
		duration, ok := clientRoleLimitDurationMillis(client.ExpiryTime, nowMs)
		if !ok {
			return common.NewError("client expiry is required for this account")
		}
		days := duration / roleLimitDayMillis
		if limits.MinExpireDaysSet && days < limits.MinExpireDays {
			return common.NewError("client expiry below minimum allowed for this account")
		}
		if limits.MaxExpireDaysSet && days > limits.MaxExpireDays {
			return common.NewError("client expiry above maximum allowed for this account")
		}
	}
	return nil
}

func (s *ClientService) ValidateClientLimitsForAdmin(user *model.User, client model.Client, checkMaxUsers bool) error {
	if user == nil {
		return nil
	}
	role, err := s.adminRoleForUser(user)
	if err != nil {
		return common.NewError("active admin role required")
	}
	if role.OwnerRole {
		return nil
	}
	limits := applyAdminPermissionOverrides(roleClientLimits(role), user)
	if checkMaxUsers && limits.MaxUsersSet {
		var count int64
		if err := database.GetDB().
			Model(&model.ClientRecord{}).
			Where("owner_admin_id = ?", user.Id).
			Count(&count).Error; err != nil {
			return err
		}
		if count >= limits.MaxUsers {
			return common.NewErrorf("client count limit reached (%d)", limits.MaxUsers)
		}
	}
	return validateClientAgainstRoleLimits(limits, client, time.Now().UnixMilli())
}

func (s *ClientService) EnsureAdminMayCreateOrEnable(user *model.User, additionalBytes int64) error {
	if user == nil {
		return nil
	}
	if user.Status != "" && user.Status != model.AdminStatusActive {
		return common.NewError("admin account is disabled")
	}
	role, err := s.adminRoleForUser(user)
	if err != nil {
		return err
	}
	if role.OwnerRole || user.DataLimit <= 0 {
		return nil
	}
	db := database.GetDB()
	if db == nil {
		return common.NewError("database is not initialized")
	}
	var fresh model.User
	if err := db.Select("used_bytes", "data_limit").Where("id = ?", user.Id).First(&fresh).Error; err != nil {
		return err
	}
	used := fresh.UsedBytes
	if used < 0 {
		used = 0
	}
	if used >= fresh.DataLimit {
		return fmt.Errorf("admin data quota exceeded (%d/%d bytes)", used, fresh.DataLimit)
	}
	projected := used + additionalBytes
	if projected > fresh.DataLimit {
		return fmt.Errorf("admin data quota exceeded (%d/%d bytes)", projected, fresh.DataLimit)
	}
	return nil
}

func (s *ClientService) UpdateForAdmin(inboundSvc *InboundService, user *model.User, scope ClientAccessScope, email string, updated model.Client, limitHwid int, inboundFilter ...int) (bool, error) {
	rec, err := s.RequireClientForScopeByEmail(scope, email)
	if err != nil {
		return false, err
	}
	if err := s.ValidateClientLimitsForAdmin(user, updated, false); err != nil {
		return false, err
	}
	if updated.Enable && !rec.Enable {
		add := int64(0)
		if updated.TotalGB > 0 {
			add = updated.TotalGB
		}
		if err := s.EnsureAdminMayCreateOrEnable(user, add); err != nil {
			return false, err
		}
	}
	return s.UpdateByEmail(inboundSvc, email, updated, limitHwid, inboundFilter...)
}