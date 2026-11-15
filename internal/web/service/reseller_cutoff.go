package service

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

// resellerClientBlocksNodeRenewal stops a node-hosted client the master may not run.
func resellerClientBlocksNodeRenewal(tx *gorm.DB, email string) bool {
	var rec model.ClientRecord
	if err := tx.Select("reseller_id", "pool_disabled").Where("email = ?", email).First(&rec).Error; err != nil {
		return false
	}
	if rec.ResellerID == nil {
		return false
	}
	if rec.PoolDisabled {
		return true
	}
	var r model.Reseller
	if err := tx.First(&r, *rec.ResellerID).Error; err != nil {
		return false
	}
	now := time.Now().UnixMilli()
	return r.CutoffReason != "" || !r.Usable(now) ||
		(r.PoolBytes > 0 && r.UsedTraffic >= r.PoolBytes)
}

// poolEnforced reads resellerPoolEnforce; an unreadable setting counts as off.
func poolEnforced() bool {
	on, err := (&SettingService{}).GetResellerPoolEnforce()
	return err == nil && on
}

// cutoffReasonOf names why r may not run clients now, or "" while it may.
func cutoffReasonOf(r *model.Reseller, nowMs int64) string {
	switch {
	case !r.Enable:
		return "disabled"
	case r.ExpiryTime > 0 && r.ExpiryTime <= nowMs:
		return "expired"
	case r.PoolBytes > 0 && r.UsedTraffic >= r.PoolBytes:
		return "pool"
	}
	return ""
}

type resellerCutoff struct {
	reseller model.Reseller
	reason   string
	clients  int
}

// enforceResellerCutoffs disables every enabled client of each due reseller the way
// disableInvalidClients does, flags exactly those clients, and records the reason.
func (s *InboundService) enforceResellerCutoffs(tx *gorm.DB, batch *trafficMutationBatch) ([]resellerCutoff, map[int]struct{}, int64, error) {
	if !poolEnforced() {
		return nil, nil, 0, nil
	}
	now := time.Now().UnixMilli()
	var due []model.Reseller
	if err := tx.Where("cutoff_reason = '' AND (enable = ? OR (expiry_time > 0 AND expiry_time <= ?) OR (pool_bytes > 0 AND used_traffic >= pool_bytes))",
		false, now).Order("id").Find(&due).Error; err != nil {
		return nil, nil, 0, err
	}
	var cuts []resellerCutoff
	nodeIDs := map[int]struct{}{}
	var disabled int64
	for i := range due {
		r := due[i]
		var emails []string
		if err := tx.Model(&model.ClientRecord{}).Where("reseller_id = ? AND enable = ?", r.Id, true).
			Order("email").Pluck("email", &emails).Error; err != nil {
			return nil, nil, 0, err
		}
		ids, err := s.disableEmailsInInbounds(tx, emails, batch)
		if err != nil {
			return nil, nil, 0, err
		}
		for id := range ids {
			nodeIDs[id] = struct{}{}
		}
		for _, chunk := range chunkStrings(emails, sqlInChunk) {
			res := tx.Model(xray.ClientTraffic{}).Where("email IN ? AND enable = ?", chunk, true).Update("enable", false)
			if res.Error != nil {
				return nil, nil, 0, res.Error
			}
			disabled += res.RowsAffected
			if err := tx.Model(&model.ClientRecord{}).Where("email IN ?", chunk).
				Updates(map[string]any{"enable": false, "pool_disabled": true, "updated_at": now}).Error; err != nil {
				return nil, nil, 0, err
			}
		}
		reason := cutoffReasonOf(&r, now)
		if err := tx.Model(&model.Reseller{}).Where("id = ?", r.Id).Update("cutoff_reason", reason).Error; err != nil {
			return nil, nil, 0, err
		}
		if err := recordResellerAudit(tx, systemActor, r.Id, "cutoff", "",
			map[string]any{"reason": reason, "clients": len(emails)}); err != nil {
			return nil, nil, 0, err
		}
		r.CutoffReason = reason
		cuts = append(cuts, resellerCutoff{reseller: r, reason: reason, clients: len(emails)})
		publishResellerCutoffNotice(r, reason, len(emails))
	}
	return cuts, nodeIDs, disabled, nil
}
