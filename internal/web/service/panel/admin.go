package panel

import (
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
	Username  string `json:"username" form:"username"`
	Password  string `json:"password" form:"password"`
	RoleId    int    `json:"roleId" form:"roleId"`
	Status    string `json:"status" form:"status"`
	DataLimit int64  `json:"dataLimit" form:"dataLimit"`
}

type AdminView struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	RoleId      int    `json:"roleId"`
	RoleName    string `json:"roleName"`
	RoleSlug    string `json:"roleSlug"`
	Status      string `json:"status"`
	DataLimit   int64  `json:"dataLimit"`
	UsedBytes   int64  `json:"usedBytes"`
	TotalUsers  int64  `json:"totalUsers"`
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

func adminToView(user *model.User, role *model.AdminRole, totalUsers int64) *AdminView {
	roleName := ""
	roleSlug := ""
	if role != nil {
		roleName = role.Name
		roleSlug = role.Slug
	}
	return &AdminView{
		Id:         user.Id,
		Username:   user.Username,
		RoleId:     user.RoleId,
		RoleName:   roleName,
		RoleSlug:   roleSlug,
		Status:     user.Status,
		DataLimit:  user.DataLimit,
		UsedBytes:  user.UsedBytes,
		TotalUsers: totalUsers,
	}
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

	user := &model.User{
		Username:  username,
		Password:  hash,
		RoleId:    payload.RoleId,
		Status:    status,
		DataLimit: payload.DataLimit,
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
		"username":    username,
		"role_id":     payload.RoleId,
		"status":      status,
		"data_limit":  payload.DataLimit,
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