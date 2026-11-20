package service

import (
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// AdminQuotaNotifySend notifies a reseller's Telegram chat about quota thresholds.
// kind is "80" or "100". Return true when a message was actually sent.
var AdminQuotaNotifySend func(adminID int, chatID int64, kind string, usedBytes, dataLimit int64) bool

func parseAdminTelegramChatID(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id != 0
}

func (s *InboundService) SyncAdminQuotaTelegramNotices() error {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	var users []model.User
	if err := db.Where("data_limit > 0").Find(&users).Error; err != nil {
		return err
	}

	sendFn := AdminQuotaNotifySend
	for i := range users {
		user := users[i]
		if adminUserIsOwner(db, user.RoleId) {
			continue
		}
		limit := user.DataLimit
		if limit <= 0 {
			continue
		}
		used := user.UsedBytes
		if used < 0 {
			used = 0
		}

		ratio := float64(used) / float64(limit)
		chatID, hasChat := parseAdminTelegramChatID(user.TelegramID)
		updates := map[string]any{}

		if ratio < 0.8 {
			if user.QuotaWarn80Sent || user.QuotaWarn100Sent {
				updates["quota_warn_80_sent"] = false
				updates["quota_warn_100_sent"] = false
			}
		} else if sendFn != nil && hasChat {
			if ratio >= 1.0 && !user.QuotaWarn80Sent {
				if sendFn(user.Id, chatID, "80", used, limit) {
					updates["quota_warn_80_sent"] = true
					user.QuotaWarn80Sent = true
				}
			}
			if ratio >= 0.8 && ratio < 1.0 && !user.QuotaWarn80Sent {
				if sendFn(user.Id, chatID, "80", used, limit) {
					updates["quota_warn_80_sent"] = true
				}
			}
			if ratio >= 1.0 && !user.QuotaWarn100Sent {
				if sendFn(user.Id, chatID, "100", used, limit) {
					updates["quota_warn_100_sent"] = true
				}
			}
		}

		if len(updates) == 0 {
			continue
		}
		if err := db.Model(&model.User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}