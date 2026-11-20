package service

import (
	"errors"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

type adminUsageByOwnerRow struct {
	AdminID   int   `gorm:"column:admin_id"`
	UsedBytes int64 `gorm:"column:used_bytes"`
}

func adminUsageByOwnerID(db *gorm.DB) (map[int]int64, error) {
	out := map[int]int64{}
	if db == nil {
		return out, errors.New("database is not initialized")
	}

	var rows []adminUsageByOwnerRow
	err := db.Table("clients AS c").
		Select(`c.owner_admin_id AS admin_id,
			COALESCE(SUM(COALESCE(ct.up, 0) + COALESCE(ct.down, 0)), 0) AS used_bytes`).
		Joins(`LEFT JOIN client_traffics ct ON ct.email = c.email`).
		Where("c.owner_admin_id > 0").
		Group("c.owner_admin_id").
		Scan(&rows).Error
	if err != nil {
		return out, err
	}

	for _, row := range rows {
		if row.AdminID > 0 {
			out[row.AdminID] = row.UsedBytes
		}
	}
	return out, nil
}

func (s *InboundService) SyncAdminUsedBytes() (map[int]int64, error) {
	db := database.GetDB()
	if db == nil {
		return nil, errors.New("database is not initialized")
	}

	usageByAdmin, err := adminUsageByOwnerID(db)
	if err != nil {
		return nil, err
	}

	var users []model.User
	if err := db.Model(&model.User{}).Select("id", "used_bytes").Find(&users).Error; err != nil {
		return nil, err
	}

	for _, user := range users {
		used := usageByAdmin[user.Id]
		if used < 0 {
			used = 0
		}
		usageByAdmin[user.Id] = used
		if user.UsedBytes == used {
			continue
		}
		if err := db.Model(&model.User{}).
			Where("id = ?", user.Id).
			Update("used_bytes", used).Error; err != nil {
			return nil, err
		}
	}

	return usageByAdmin, nil
}