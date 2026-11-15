package service

import (
	"errors"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

var (
	ErrResellerVolume        = errors.New("the volume to add must be more than zero")
	ErrResellerUnlimitedPool = errors.New("this reseller has no pool limit to add volume to")
	ErrResellerExpiryPast    = errors.New("the new expiry must be in the future, or 0 for never")
)

// updateResellerRow applies fields to one reseller and audits it in one transaction.
func updateResellerRow(actor AuditActor, id int, fields map[string]any, action string, detail map[string]any) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Reseller{}).Where("id = ?", id).Updates(fields)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrResellerNotFound
		}
		return recordResellerAudit(tx, actor, id, action, "", detail)
	})
}

// AddVolume raises the pool and starts a new notification cycle, then releases a cut-off.
func (s *ResellerService) AddVolume(actor AuditActor, id int, bytes int64) error {
	if bytes <= 0 {
		return ErrResellerVolume
	}
	r, err := loadReseller(database.GetDB(), id)
	if err != nil {
		return err
	}
	if r.PoolBytes == 0 {
		return ErrResellerUnlimitedPool
	}
	if err := updateResellerRow(actor, id, map[string]any{
		"pool_bytes":     gorm.Expr(database.ClampedAddExpr("pool_bytes"), bytes),
		"notified_level": 0, "updated_at": time.Now().UnixMilli(),
	}, "addVolume", map[string]any{"bytes": bytes}); err != nil {
		return err
	}
	return s.releaseResellerCutoff(actor, id)
}

// ResetUsage is the only way the pool counter goes down (docs §4.3).
func (s *ResellerService) ResetUsage(actor AuditActor, id int) error {
	r, err := loadReseller(database.GetDB(), id)
	if err != nil {
		return err
	}
	if err := updateResellerRow(actor, id, map[string]any{
		"used_traffic": 0, "notified_level": 0, "updated_at": time.Now().UnixMilli(),
	}, "resetUsage", map[string]any{"previousBytes": r.UsedTraffic}); err != nil {
		return err
	}
	return s.releaseResellerCutoff(actor, id)
}

func (s *ResellerService) ExtendExpiry(actor AuditActor, id int, expiry int64) error {
	if expiry != 0 && expiry <= time.Now().UnixMilli() {
		return ErrResellerExpiryPast
	}
	if err := updateResellerRow(actor, id, map[string]any{"expiry_time": expiry, "updated_at": time.Now().UnixMilli()},
		"extendExpiry", map[string]any{"expiryTime": expiry}); err != nil {
		return err
	}
	return s.releaseResellerCutoff(actor, id)
}

// SetPoolEnforce flips resellerPoolEnforce; turning it off releases every cut-off (rollback).
func (s *ResellerService) SetPoolEnforce(actor AuditActor, on bool) error {
	if err := (&SettingService{}).SetResellerPoolEnforce(on); err != nil {
		return err
	}
	if err := recordResellerAudit(database.GetDB(), actor, 0, "poolEnforce", "", map[string]any{"on": on}); err != nil {
		return err
	}
	if on {
		return nil
	}
	var ids []int
	if err := database.GetDB().Model(&model.Reseller{}).Where("cutoff_reason <> ''").Order("id").Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.releaseResellerCutoff(actor, id); err != nil {
			return err
		}
	}
	return nil
}

func ownedEmails(db *gorm.DB, id int) ([]string, error) {
	var emails []string
	err := db.Model(&model.ClientRecord{}).Where("reseller_id = ?", id).Order("email").Pluck("email", &emails).Error
	return emails, err
}

// withValidOwnState keeps the emails not expired and not over their own quota.
func withValidOwnState(db *gorm.DB, emails []string) ([]string, error) {
	cond, args := depletedCond(db)
	var out []string
	for _, chunk := range chunkStrings(emails, sqlInChunk) {
		var page []string
		if err := db.Model(&model.ClientRecord{}).Where("email IN ?", chunk).
			Where("email NOT IN (?)", db.Model(xray.ClientTraffic{}).Select("email").Where(cond, args...)).
			Order("email").Pluck("email", &page).Error; err != nil {
			return nil, err
		}
		out = append(out, page...)
	}
	return out, nil
}

func setClientsEnable(emails []string, enable bool) (int, error) {
	if len(emails) == 0 {
		return 0, nil
	}
	res, needRestart, err := (&ClientService{}).BulkSetEnable(&InboundService{}, emails, enable)
	if needRestart {
		(&XrayService{}).SetToNeedRestart()
	}
	return res.Changed, err
}

// DisableClients turns off every client of the reseller; as a manual disable it clears pool flags.
func (s *ResellerService) DisableClients(actor AuditActor, id int) (int, error) {
	db := database.GetDB()
	if _, err := loadReseller(db, id); err != nil {
		return 0, err
	}
	emails, err := ownedEmails(db, id)
	if err != nil {
		return 0, err
	}
	n, err := setClientsEnable(emails, false)
	if err != nil {
		return n, err
	}
	return n, recordResellerAudit(db, actor, id, "disableClients", "", map[string]any{"clients": n})
}

// EnableClients turns on the reseller's clients whose own time and quota still allow it.
func (s *ResellerService) EnableClients(actor AuditActor, id int) (int, error) {
	db := database.GetDB()
	r, err := loadReseller(db, id)
	if err != nil {
		return 0, err
	}
	if resellerBlocked(r, time.Now().UnixMilli()) {
		return 0, ErrResellerBlocked
	}
	emails, err := ownedEmails(db, id)
	if err != nil {
		return 0, err
	}
	if emails, err = withValidOwnState(db, emails); err != nil {
		return 0, err
	}
	n, err := setClientsEnable(emails, true)
	if err != nil {
		return n, err
	}
	return n, recordResellerAudit(db, actor, id, "enableClients", "", map[string]any{"clients": n})
}

// releaseResellerCutoff re-enables the pool-disabled clients still valid on their own,
// then clears every flag and the reason. A no-op while the reseller is still due.
func (s *ResellerService) releaseResellerCutoff(actor AuditActor, id int) error {
	db := database.GetDB()
	r, err := loadReseller(db, id)
	if err != nil {
		return err
	}
	if poolEnforced() && cutoffReasonOf(r, time.Now().UnixMilli()) != "" {
		return nil
	}
	var flagged []string
	if err := db.Model(&model.ClientRecord{}).Where("reseller_id = ? AND pool_disabled = ?", id, true).
		Order("email").Pluck("email", &flagged).Error; err != nil {
		return err
	}
	if len(flagged) == 0 && r.CutoffReason == "" {
		return nil
	}
	valid, err := withValidOwnState(db, flagged)
	if err != nil {
		return err
	}
	if _, err := setClientsEnable(valid, true); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ClientRecord{}).Where("reseller_id = ? AND pool_disabled = ?", id, true).
			Update("pool_disabled", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Reseller{}).Where("id = ?", id).Update("cutoff_reason", "").Error; err != nil {
			return err
		}
		if err := recordResellerAudit(tx, actor, id, "release", "",
			map[string]any{"reason": r.CutoffReason, "clients": len(valid), "skipped": len(flagged) - len(valid)}); err != nil {
			return err
		}
		var fresh model.Reseller
		if err := tx.First(&fresh, id).Error; err != nil {
			return err
		}
		publishResellerReleaseNotice(fresh, len(valid))
		return nil
	})
}
