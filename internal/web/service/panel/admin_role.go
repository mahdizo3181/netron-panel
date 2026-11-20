package panel

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

type AdminRoleService struct{}

type AdminRolePayload struct {
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Permissions map[string]any `json:"permissions"`
	Limits      map[string]any `json:"limits"`
}

type AdminRoleView struct {
	Id          int            `json:"id"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	BuiltIn     bool           `json:"builtIn"`
	OwnerRole   bool           `json:"ownerRole"`
	Permissions map[string]any `json:"permissions"`
	Limits      map[string]any `json:"limits"`
}

func parseJSONObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func roleToView(role *model.AdminRole) *AdminRoleView {
	if role == nil {
		return nil
	}
	return &AdminRoleView{
		Id:          role.Id,
		Name:        role.Name,
		Slug:        role.Slug,
		BuiltIn:     role.BuiltIn,
		OwnerRole:   role.OwnerRole,
		Permissions: parseJSONObject(role.PermissionsJSON),
		Limits:      parseJSONObject(role.LimitsJSON),
	}
}

func (s *AdminRoleService) List() ([]*AdminRoleView, error) {
	db := database.GetDB()
	var rows []model.AdminRole
	if err := db.Order("built_in DESC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*AdminRoleView, 0, len(rows))
	for i := range rows {
		out = append(out, roleToView(&rows[i]))
	}
	return out, nil
}

func (s *AdminRoleService) Get(id int) (*AdminRoleView, error) {
	if id <= 0 {
		return nil, common.NewError("invalid role id")
	}
	var role model.AdminRole
	if err := database.GetDB().Where("id = ?", id).First(&role).Error; err != nil {
		return nil, err
	}
	return roleToView(&role), nil
}

func (s *AdminRoleService) Create(payload AdminRolePayload) (*AdminRoleView, error) {
	name := strings.TrimSpace(payload.Name)
	slug := strings.TrimSpace(strings.ToLower(payload.Slug))
	if name == "" {
		return nil, common.NewError("name is required")
	}
	if slug == "" {
		return nil, common.NewError("slug is required")
	}

	db := database.GetDB()
	var count int64
	if err := db.Model(&model.AdminRole{}).Where("slug = ? OR name = ?", slug, name).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, common.NewError("role name or slug already exists")
	}

	perms := payload.Permissions
	if perms == nil {
		perms = map[string]any{}
	}
	limits := payload.Limits
	if limits == nil {
		limits = map[string]any{}
	}

	role := &model.AdminRole{
		Name:            name,
		Slug:            slug,
		BuiltIn:         false,
		OwnerRole:       false,
		PermissionsJSON: mustRoleJSON(perms),
		LimitsJSON:      mustRoleJSON(limits),
	}
	if err := db.Create(role).Error; err != nil {
		return nil, err
	}
	return roleToView(role), nil
}

func (s *AdminRoleService) Update(id int, payload AdminRolePayload) (*AdminRoleView, error) {
	if id <= 0 {
		return nil, common.NewError("invalid role id")
	}
	db := database.GetDB()
	var existing model.AdminRole
	if err := db.Where("id = ?", id).First(&existing).Error; err != nil {
		return nil, err
	}
	if existing.BuiltIn || existing.OwnerRole {
		return nil, common.NewError("built-in role cannot be modified")
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, common.NewError("name is required")
	}

	var count int64
	if err := db.Model(&model.AdminRole{}).
		Where("(name = ? OR slug = ?) AND id <> ?", name, strings.TrimSpace(strings.ToLower(payload.Slug)), id).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, common.NewError("role name or slug already exists")
	}

	updates := map[string]any{"name": name}
	if perms := payload.Permissions; perms != nil {
		updates["permissions"] = mustRoleJSON(perms)
	}
	if limits := payload.Limits; limits != nil {
		updates["limits"] = mustRoleJSON(limits)
	}
	if err := db.Model(&model.AdminRole{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(id)
}

func (s *AdminRoleService) Delete(id int) error {
	if id <= 0 {
		return common.NewError("invalid role id")
	}
	db := database.GetDB()
	var existing model.AdminRole
	if err := db.Where("id = ?", id).First(&existing).Error; err != nil {
		return err
	}
	if existing.BuiltIn || existing.OwnerRole {
		return common.NewError("built-in role cannot be deleted")
	}

	var assigned int64
	if err := db.Model(&model.User{}).Where("role_id = ?", id).Count(&assigned).Error; err != nil {
		return err
	}
	if assigned > 0 {
		return common.NewError("role is assigned to administrators")
	}

	res := db.Where("id = ?", id).Delete(&model.AdminRole{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("role not found")
	}
	return nil
}

func mustRoleJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}