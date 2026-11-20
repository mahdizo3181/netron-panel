package panel

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	core "github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"gorm.io/gorm"
)

type AdminService struct{}

type AdminPayload struct {
	Username            string         `json:"username" form:"username" example:"reseller1"`
	Password            string         `json:"password" form:"password"`
	RoleId              int            `json:"roleId" form:"roleId" example:"3"`
	Status              string         `json:"status" form:"status" example:"active"`
	DataLimit           int64          `json:"dataLimit" form:"dataLimit" example:"10737418240"`
	ProfileTitle        string         `json:"profileTitle" form:"profileTitle" example:"VIP Clients"`
	SupportURL          string         `json:"supportUrl" form:"supportUrl" example:"https://t.me/support"`
	SubscriptionDomain  string         `json:"subscriptionDomain" form:"subscriptionDomain" example:"sub.example.com"`
	TelegramID          string         `json:"telegramId" form:"telegramId" example:"123456789"`
	PermissionOverrides map[string]any `json:"permissionOverrides"`
}

type AdminView struct {
	Id                  int            `json:"id" example:"2"`
	Username            string         `json:"username" example:"reseller1"`
	RoleId              int            `json:"roleId" example:"3"`
	RoleName            string         `json:"roleName" example:"Operator"`
	RoleSlug            string         `json:"roleSlug" example:"operator"`
	Status              string         `json:"status" example:"active"`
	DataLimit           int64          `json:"dataLimit" example:"10737418240"`
	UsedBytes           int64          `json:"usedBytes" example:"0"`
	TotalUsers          int64          `json:"totalUsers" example:"5"`
	ProfileTitle        string         `json:"profileTitle" example:"VIP Clients"`
	SupportURL          string         `json:"supportUrl" example:"https://t.me/support"`
	SubscriptionDomain  string         `json:"subscriptionDomain" example:"sub.example.com"`
	TelegramID          string         `json:"telegramId" example:"123456789"`
	PermissionOverrides map[string]any `json:"permissionOverrides"`
}

type AdminStats struct {
	Total    int64 `json:"total"`
	Active   int64 `json:"active"`
	Disabled int64 `json:"disabled"`
	Limited  int64 `json:"limited"`
}

func validAdminStatus(status string) bool {
	return status == model.AdminStatusActive || status == model.AdminStatusDisabled
}

func normalizeAdminStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return model.AdminStatusActive
	}
	return status
}

func parseAdminOverridesJSON(raw string) map[string]any {
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

func marshalPermissionOverrides(overrides map[string]any) (string, error) {
	if overrides == nil || len(overrides) == 0 {
		return "", nil
	}
	b, err := json.Marshal(overrides)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func adminToView(user *model.User, role *model.AdminRole, totalUsers int64) *AdminView {
	roleName := ""
	roleSlug := ""
	if role != nil {
		roleName = role.Name
		roleSlug = role.Slug
	}
	return &AdminView{
		Id:                  user.Id,
		Username:            user.Username,
		RoleId:              user.RoleId,
		RoleName:            roleName,
		RoleSlug:            roleSlug,
		Status:              user.Status,
		DataLimit:           user.DataLimit,
		UsedBytes:           user.UsedBytes,
		TotalUsers:          totalUsers,
		ProfileTitle:        user.ProfileTitle,
		SupportURL:          user.SupportURL,
		SubscriptionDomain:  user.SubscriptionDomain,
		TelegramID:          user.TelegramID,
		PermissionOverrides: parseAdminOverridesJSON(user.PermissionOverridesJSON),
	}
}

func (s *AdminService) Stats() (*AdminStats, error) {
	db := database.GetDB()
	var users []model.User
	if err := db.Find(&users).Error; err != nil {
		return nil, err
	}
	stats := &AdminStats{}
	for _, user := range users {
		stats.Total++
		switch user.Status {
		case model.AdminStatusDisabled:
			stats.Disabled++
		default:
			stats.Active++
		}
		if user.DataLimit > 0 {
			stats.Limited++
		}
	}
	return stats, nil
}

func (s *AdminService) RemoveAllClients(id int) (int64, bool, error) {
	if id <= 0 {
		return 0, false, common.NewError("invalid admin id")
	}
	db := database.GetDB()
	var emails []string
	if err := db.Model(&model.ClientRecord{}).
		Where("owner_admin_id = ?", id).
		Pluck("email", &emails).Error; err != nil {
		return 0, false, err
	}
	if len(emails) == 0 {
		return 0, false, nil
	}
	res, needRestart, err := (&core.ClientService{}).BulkDelete(&core.InboundService{}, emails, false)
	if err != nil {
		return int64(res.Deleted), needRestart, err
	}
	if _, syncErr := (&core.InboundService{}).SyncAdminUsedBytes(); syncErr != nil {
		return int64(res.Deleted), needRestart, syncErr
	}
	return int64(res.Deleted), needRestart, nil
}

func (s *AdminService) roleByID(tx *gorm.DB, roleID int) (*model.AdminRole, error) {
	if roleID <= 0 {
		return nil, common.NewError("role is required")
	}
	var role model.AdminRole
	if err := tx.Where("id = ?", roleID).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (s *AdminService) ownerRoleID(tx *gorm.DB) (int, error) {
	var role model.AdminRole
	if err := tx.Where("slug = ?", model.AdminRoleSlugOwner).First(&role).Error; err != nil {
		return 0, err
	}
	return role.Id, nil
}

func (s *AdminService) List() ([]*AdminView, error) {
	db := database.GetDB()
	var users []model.User
	if err := db.Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}

	if _, err := (&core.InboundService{}).SyncAdminUsedBytes(); err != nil {
		return nil, err
	}
	if err := db.Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}

	roleIDs := make([]int, 0, len(users))
	adminIDs := make([]int, 0, len(users))
	for _, user := range users {
		roleIDs = append(roleIDs, user.RoleId)
		adminIDs = append(adminIDs, user.Id)
	}

	roles := map[int]model.AdminRole{}
	if len(roleIDs) > 0 {
		var rows []model.AdminRole
		if err := db.Where("id IN ?", roleIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			roles[row.Id] = row
		}
	}

	userCounts := map[int]int64{}
	if len(adminIDs) > 0 {
		type countRow struct {
			OwnerAdminId int
			Count        int64
		}
		var grouped []countRow
		if err := db.Model(&model.ClientRecord{}).
			Select("owner_admin_id, COUNT(*) AS count").
			Where("owner_admin_id IN ?", adminIDs).
			Group("owner_admin_id").
			Scan(&grouped).Error; err != nil {
			return nil, err
		}
		for _, row := range grouped {
			userCounts[row.OwnerAdminId] = row.Count
		}
	}

	out := make([]*AdminView, 0, len(users))
	for i := range users {
		role, ok := roles[users[i].RoleId]
		var rolePtr *model.AdminRole
		if ok {
			rolePtr = &role
		}
		out = append(out, adminToView(&users[i], rolePtr, userCounts[users[i].Id]))
	}
	return out, nil
}

func (s *AdminService) Get(id int) (*AdminView, error) {
	if id <= 0 {
		return nil, common.NewError("invalid admin id")
	}
	db := database.GetDB()
	if _, err := (&core.InboundService{}).SyncAdminUsedBytes(); err != nil {
		return nil, err
	}

	var user model.User
	if err := db.Where("id = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	var role model.AdminRole
	_ = db.Where("id = ?", user.RoleId).First(&role).Error
	var totalUsers int64
	if err := db.Model(&model.ClientRecord{}).Where("owner_admin_id = ?", id).Count(&totalUsers).Error; err != nil {
		return nil, err
	}
	return adminToView(&user, &role, totalUsers), nil
}

func (s *AdminService) Create(payload AdminPayload) (*AdminView, error) {
	username := strings.TrimSpace(payload.Username)
	if username == "" {
		return nil, common.NewError("username is required")
	}
	if payload.Password == "" {
		return nil, common.NewError("password is required")
	}

	status := normalizeAdminStatus(payload.Status)
	if !validAdminStatus(status) {
		return nil, common.NewError("invalid admin status")
	}

	db := database.GetDB()
	role, err := s.roleByID(db, payload.RoleId)
	if err != nil {
		return nil, err
	}
	if role.OwnerRole {
		return nil, common.NewError("owner role cannot be assigned here")
	}

	var count int64
	if err := db.Model(&model.User{}).Where("username = ?", username).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, common.NewError("username already exists")
	}

	hash, err := crypto.HashPasswordAsBcrypt(payload.Password)
	if err != nil {
		return nil, err
	}

	overridesJSON, err := marshalPermissionOverrides(payload.PermissionOverrides)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Username:                username,
		Password:                hash,
		RoleId:                  payload.RoleId,
		Status:                  status,
		DataLimit:               payload.DataLimit,
		ProfileTitle:            strings.TrimSpace(payload.ProfileTitle),
		SupportURL:              strings.TrimSpace(payload.SupportURL),
		SubscriptionDomain:      strings.TrimSpace(payload.SubscriptionDomain),
		TelegramID:              strings.TrimSpace(payload.TelegramID),
		PermissionOverridesJSON: overridesJSON,
	}

	if err := db.Create(user).Error; err != nil {
		return nil, err
	}
	return adminToView(user, role, 0), nil
}

func (s *AdminService) Update(id int, payload AdminPayload) (*AdminView, error) {
	if id <= 0 {
		return nil, common.NewError("invalid admin id")
	}
	db := database.GetDB()
	var existing model.User
	if err := db.Where("id = ?", id).First(&existing).Error; err != nil {
		return nil, err
	}

	role, err := s.roleByID(db, payload.RoleId)
	if err != nil {
		return nil, err
	}
	ownerRoleID, err := s.ownerRoleID(db)
	if err != nil {
		return nil, err
	}
	if existing.RoleId == ownerRoleID {
		return nil, common.NewError("owner admin cannot be modified here")
	}
	if role.OwnerRole {
		return nil, common.NewError("owner role cannot be assigned here")
	}

	username := strings.TrimSpace(payload.Username)
	if username == "" {
		return nil, common.NewError("username is required")
	}
	status := normalizeAdminStatus(payload.Status)
	if !validAdminStatus(status) {
		return nil, common.NewError("invalid admin status")
	}

	var count int64
	if err := db.Model(&model.User{}).
		Where("username = ? AND id <> ?", username, id).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, common.NewError("username already exists")
	}

	updates := map[string]any{
		"username":             username,
		"role_id":              payload.RoleId,
		"status":               status,
		"data_limit":           payload.DataLimit,
		"profile_title":        strings.TrimSpace(payload.ProfileTitle),
		"support_url":          strings.TrimSpace(payload.SupportURL),
		"subscription_domain":  strings.TrimSpace(payload.SubscriptionDomain),
		"telegram_id":          strings.TrimSpace(payload.TelegramID),
	}
	if payload.PermissionOverrides != nil {
		overridesJSON, overridesErr := marshalPermissionOverrides(payload.PermissionOverrides)
		if overridesErr != nil {
			return nil, overridesErr
		}
		updates["permission_overrides"] = overridesJSON
	}

	if payload.Password != "" {
		hash, hashErr := crypto.HashPasswordAsBcrypt(payload.Password)
		if hashErr != nil {
			return nil, hashErr
		}
		updates["password"] = hash
		updates["login_epoch"] = gorm.Expr("login_epoch + 1")
	}

	if err := db.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(id)
}

func (s *AdminService) Delete(id int) error {
	if id <= 0 {
		return common.NewError("invalid admin id")
	}
	db := database.GetDB()
	var existing model.User
	if err := db.Where("id = ?", id).First(&existing).Error; err != nil {
		return err
	}

	ownerRoleID, err := s.ownerRoleID(db)
	if err != nil {
		return err
	}
	if existing.RoleId == ownerRoleID {
		return common.NewError("owner admin cannot be deleted")
	}

	var ownedClients int64
	if err := db.Model(&model.ClientRecord{}).Where("owner_admin_id = ?", id).Count(&ownedClients).Error; err != nil {
		return err
	}
	if ownedClients > 0 {
		return common.NewError("admin owns clients and cannot be deleted")
	}

	res := db.Where("id = ?", id).Delete(&model.User{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("admin not found")
	}
	return nil
}

func (s *AdminService) SetEnabled(id int, enabled bool) error {
	if id <= 0 {
		return common.NewError("invalid admin id")
	}
	db := database.GetDB()
	var existing model.User
	if err := db.Where("id = ?", id).First(&existing).Error; err != nil {
		return err
	}
	ownerRoleID, err := s.ownerRoleID(db)
	if err != nil {
		return err
	}
	if existing.RoleId == ownerRoleID {
		return common.NewError("owner admin cannot be disabled")
	}

	status := model.AdminStatusDisabled
	if enabled {
		status = model.AdminStatusActive
	}
	res := db.Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":      status,
			"login_epoch": gorm.Expr("login_epoch + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("admin not found")
	}
	return nil
}

func (s *AdminService) ResetUsage(id int) error {
	if id <= 0 {
		return common.NewError("invalid admin id")
	}
	db := database.GetDB()

	var emails []string
	if err := db.Model(&model.ClientRecord{}).
		Where("owner_admin_id = ?", id).
		Pluck("email", &emails).Error; err != nil {
		return err
	}

	if len(emails) > 0 {
		affected, resetErr := (&core.ClientService{}).BulkResetTraffic(&core.InboundService{}, emails)
		if resetErr != nil {
			return resetErr
		}
		if affected > 0 {
			(&core.XrayService{}).SetToNeedRestart()
		}
	}

	if _, err := (&core.InboundService{}).SyncAdminUsedBytes(); err != nil {
		return err
	}
	return nil
}