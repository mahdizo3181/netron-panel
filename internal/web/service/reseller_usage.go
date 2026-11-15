package service

import (
	"maps"
	"slices"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

func billUsageAdd(usage map[string]int64, email string, bytes int64) {
	if bytes > 0 && email != "" {
		usage[email] += bytes
	}
}

// addResellerUsage bills the owned emails' bytes to their resellers' pools in tx.
// Unowned emails cost one indexed SELECT and touch no counter (docs §4.3).
func addResellerUsage(tx *gorm.DB, usage map[string]int64) error {
	if len(usage) == 0 {
		return nil
	}
	perReseller := map[int]int64{}
	for _, batch := range chunkStrings(slices.Sorted(maps.Keys(usage)), sqlInChunk) {
		var rows []struct {
			Email      string
			ResellerID int
		}
		if err := tx.Model(&model.ClientRecord{}).Select("email, reseller_id").
			Where("email IN ? AND reseller_id IS NOT NULL", batch).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			perReseller[r.ResellerID] += usage[r.Email]
		}
	}
	// Ascending ids keep the lock order stable for concurrent writers on PostgreSQL.
	for _, id := range slices.Sorted(maps.Keys(perReseller)) {
		if err := tx.Model(&model.Reseller{}).Where("id = ?", id).
			Update("used_traffic", gorm.Expr(database.ClampedAddExpr("used_traffic"), perReseller[id])).Error; err != nil {
			return err
		}
	}
	return bumpResellerPoolNotices(tx, slices.Sorted(maps.Keys(perReseller)))
}
