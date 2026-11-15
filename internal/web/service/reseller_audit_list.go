package service

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

// ResellerAuditPageParams filters the admin or reseller audit view.
type ResellerAuditPageParams struct {
	ResellerId int    `form:"resellerId"`
	Action     string `form:"action"`
	From       int64  `form:"from"`
	To         int64  `form:"to"`
	Page       int    `form:"page"`
	PageSize   int    `form:"pageSize"`
}

type ResellerAuditPage struct {
	Total int64                    `json:"total" example:"42"`
	Rows  []model.ResellerAuditLog `json:"rows"`
}

func (s *ResellerService) ListAudit(scope Scope, p ResellerAuditPageParams) (*ResellerAuditPage, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = 25
	}
	q := database.GetDB().Model(&model.ResellerAuditLog{})
	if !scope.IsAdmin() {
		if scope.ResellerID == nil {
			return nil, ErrResellerNotFound
		}
		q = q.Where("reseller_id = ?", *scope.ResellerID)
	} else if p.ResellerId > 0 {
		q = q.Where("reseller_id = ?", p.ResellerId)
	}
	if p.Action != "" {
		q = q.Where("action = ?", p.Action)
	}
	if p.From > 0 {
		q = q.Where("at >= ?", p.From)
	}
	if p.To > 0 {
		q = q.Where("at <= ?", p.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []model.ResellerAuditLog
	offset := (p.Page - 1) * p.PageSize
	if err := q.Order("at DESC, id DESC").Offset(offset).Limit(p.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	return &ResellerAuditPage{Total: total, Rows: rows}, nil
}

// PruneResellerAuditLogs deletes rows older than the configured retention.
func PruneResellerAuditLogs() error {
	days, err := (&SettingService{}).GetResellerAuditRetentionDays()
	if err != nil || days <= 0 {
		days = 180
	}
	cutoff := time.Now().AddDate(0, 0, -days).UnixMilli()
	return database.GetDB().Where("at < ?", cutoff).Delete(&model.ResellerAuditLog{}).Error
}

func auditorFromScope(scope Scope, ip string) AuditActor {
	if scope.IsAdmin() {
		return AuditActor{Kind: "admin", Ip: ip}
	}
	return AuditActor{Kind: "reseller", Id: *scope.ResellerID, Ip: ip}
}

func recordResellerAuditDB(actor AuditActor, resellerID int, action, target string, detail map[string]any) error {
	return recordResellerAudit(database.GetDB(), actor, resellerID, action, target, detail)
}

func auditResellerCreate(tx *gorm.DB, actor AuditActor, id int) error {
	return recordResellerAudit(tx, actor, id, "create", "", nil)
}