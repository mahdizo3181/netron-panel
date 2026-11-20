package service

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
)

type ClientAccessMode string

const (
	ClientAccessAll  ClientAccessMode = "all"
	ClientAccessOwn  ClientAccessMode = "own"
	ClientAccessNone ClientAccessMode = "none"
)

type ClientAccessScope struct {
	AdminID int              `json:"-"`
	Mode    ClientAccessMode `json:"-"`
}

func normalizeClientAccessScope(scope ClientAccessScope) ClientAccessScope {
	if scope.Mode == "" {
		scope.Mode = ClientAccessAll
	}
	if scope.Mode == ClientAccessOwn && scope.AdminID <= 0 {
		scope.Mode = ClientAccessNone
	}
	return scope
}

func roleUserPermissionValue(role *model.AdminRole, key string) any {
	if role == nil {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(role.PermissionsJSON), &root); err != nil {
		return nil
	}
	users, ok := root["users"].(map[string]any)
	if !ok {
		return nil
	}
	keys := []string{key}
	if key != "read" {
		keys = append(keys, "read")
	} else {
		keys = []string{"read", "view"}
	}
	for _, candidate := range keys {
		if value, ok := users[candidate]; ok {
			return value
		}
	}
	return nil
}

func clientAccessModeFromPermission(v any) ClientAccessMode {
	switch t := v.(type) {
	case map[string]any:
		return clientAccessModeFromPermission(t["scope"])
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "all", "2":
			return ClientAccessAll
		case "own", "1":
			return ClientAccessOwn
		}
	case bool:
		if t {
			return ClientAccessAll
		}
		return ClientAccessNone
	case float64:
		switch int(t) {
		case 2:
			return ClientAccessAll
		case 1:
			return ClientAccessOwn
		}
	case int:
		switch t {
		case 2:
			return ClientAccessAll
		case 1:
			return ClientAccessOwn
		}
	}
	return ClientAccessNone
}

func (s *ClientService) adminRoleForUser(user *model.User) (*model.AdminRole, error) {
	if user == nil || user.Id <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if user.Status != "" && user.Status != model.AdminStatusActive {
		return nil, gorm.ErrRecordNotFound
	}
	if user.RoleId <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var role model.AdminRole
	if err := database.GetDB().Where("id = ?", user.RoleId).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (s *ClientService) ClientAccessScopeForAdmin(user *model.User, permission string) ClientAccessScope {
	role, err := s.adminRoleForUser(user)
	if err != nil {
		return ClientAccessScope{Mode: ClientAccessNone}
	}
	if role.OwnerRole {
		return normalizeClientAccessScope(ClientAccessScope{
			AdminID: user.Id,
			Mode:    ClientAccessAll,
		})
	}
	mode := clientAccessModeFromPermission(roleUserPermissionValue(role, permission))
	return normalizeClientAccessScope(ClientAccessScope{
		AdminID: user.Id,
		Mode:    mode,
	})
}

func (s *ClientService) ClientRecordAllowed(scope ClientAccessScope, rec *model.ClientRecord) bool {
	scope = normalizeClientAccessScope(scope)
	if rec == nil {
		return false
	}
	switch scope.Mode {
	case ClientAccessAll:
		return true
	case ClientAccessOwn:
		return rec.OwnerAdminId > 0 && rec.OwnerAdminId == scope.AdminID
	default:
		return false
	}
}

func applyClientAccessScope(db *gorm.DB, scope ClientAccessScope) *gorm.DB {
	scope = normalizeClientAccessScope(scope)
	switch scope.Mode {
	case ClientAccessAll:
		return db
	case ClientAccessOwn:
		return db.Where("owner_admin_id = ?", scope.AdminID)
	default:
		return db.Where("1 = 0")
	}
}

func (s *ClientService) RequireClientForScopeByEmail(scope ClientAccessScope, email string) (*model.ClientRecord, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var rec model.ClientRecord
	err := applyClientAccessScope(database.GetDB().Model(&model.ClientRecord{}), scope).
		Where("email = ?", email).
		First(&rec).Error
	if err != nil {
		return nil, err
	}
	if !s.ClientRecordAllowed(scope, &rec) {
		return nil, gorm.ErrRecordNotFound
	}
	return &rec, nil
}

func assignClientOwnerAdmin(email string, adminID int) error {
	if adminID <= 0 || strings.TrimSpace(email) == "" {
		return nil
	}
	return database.GetDB().Model(&model.ClientRecord{}).
		Where("email = ? AND (owner_admin_id IS NULL OR owner_admin_id = 0)", email).
		Update("owner_admin_id", adminID).Error
}

func (s *ClientService) CreateForAdmin(inboundSvc *InboundService, payload *ClientCreatePayload, user *model.User) (bool, error) {
	if payload == nil {
		return false, common.NewError("empty payload")
	}
	email := strings.TrimSpace(payload.Client.Email)
	if user != nil {
		scope := s.ClientAccessScopeForAdmin(user, "create")
		if scope.Mode == ClientAccessNone {
			return false, common.NewError("client create permission required")
		}
		if email != "" {
			if existing, err := s.GetRecordByEmail(nil, email); err == nil {
				if !s.ClientRecordAllowed(scope, existing) {
					return false, gorm.ErrRecordNotFound
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return false, err
			}
		}
	}
	needRestart, err := s.Create(inboundSvc, payload)
	if err != nil {
		return needRestart, err
	}
	if user != nil && email != "" {
		if err := assignClientOwnerAdmin(email, user.Id); err != nil {
			return needRestart, err
		}
	}
	return needRestart, nil
}