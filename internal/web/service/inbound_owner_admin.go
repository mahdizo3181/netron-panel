package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

func FilterVisibleClientEmails(emails []string) []string {
	if len(emails) == 0 {
		return nil
	}
	out := make([]string, 0, len(emails))
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" || isClientEmailTombstoned(email) {
			continue
		}
		out = append(out, email)
	}
	return out
}

func (s *InboundService) DisableClientsByOwnerAdminID(ownerAdminID int) (bool, int64, error) {
	return s.disableClientsByOwnerAdminID(ownerAdminID, 0)
}

func (s *InboundService) DisableClientsByQuotaOwnerAdminID(ownerAdminID int) (bool, int64, error) {
	return s.disableClientsByOwnerAdminID(ownerAdminID, ownerAdminID)
}

func (s *InboundService) disableClientsByOwnerAdminID(ownerAdminID int, disabledByOwnerAdminID int) (bool, int64, error) {
	if ownerAdminID <= 0 {
		return false, 0, common.NewError("invalid admin id")
	}
	db := database.GetDB()
	if db == nil {
		return false, 0, common.NewError("database is not initialized")
	}

	var rawEmails []string
	if err := db.Model(&model.ClientRecord{}).
		Where("owner_admin_id = ? AND enable = ?", ownerAdminID, true).
		Pluck("email", &rawEmails).Error; err != nil {
		return false, 0, err
	}
	emails := FilterVisibleClientEmails(rawEmails)
	if len(emails) == 0 {
		return false, 0, nil
	}

	cleanEmails := uniqueEmails(emails)
	if len(cleanEmails) == 0 {
		return false, 0, nil
	}

	type target struct {
		InboundID int  `gorm:"column:inbound_id"`
		NodeID    *int `gorm:"column:node_id"`
		Email     string
	}
	var targets []target
	if err := db.Raw(`
		SELECT inbounds.id AS inbound_id, inbounds.node_id AS node_id, clients.email AS email
		FROM clients
		JOIN client_inbounds ON client_inbounds.client_id = clients.id
		JOIN inbounds        ON inbounds.id = client_inbounds.inbound_id
		WHERE clients.email IN ?
	`, cleanEmails).Scan(&targets).Error; err != nil {
		return false, 0, err
	}

	needRestart := false
	byInbound := make(map[int]map[string]struct{})
	disabledNodes := map[int]struct{}{}
	for _, t := range targets {
		if byInbound[t.InboundID] == nil {
			byInbound[t.InboundID] = make(map[string]struct{})
		}
		byInbound[t.InboundID][t.Email] = struct{}{}
		if t.NodeID != nil {
			disabledNodes[*t.NodeID] = struct{}{}
		}
	}

	for inboundID, group := range byInbound {
		if _, _, mErr := s.markClientsDisabledInSettings(db, inboundID, group); mErr != nil {
			logger.Warning("disableClientsByOwnerAdminID: settings sync failed inbound", inboundID, mErr)
			needRestart = true
		}
	}
	for nodeID := range disabledNodes {
		if dirtyErr := (&NodeService{}).MarkNodeDirty(nodeID); dirtyErr != nil {
			return needRestart, int64(len(cleanEmails)), dirtyErr
		}
	}

	now := time.Now().UnixMilli()
	if err := db.Model(xray.ClientTraffic{}).
		Where("email IN ?", cleanEmails).
		Update("enable", false).Error; err != nil {
		return needRestart, int64(len(cleanEmails)), err
	}

	recordUpdates := map[string]any{
		"enable":                     false,
		"disabled_by_owner_admin_id": disabledByOwnerAdminID,
		"updated_at":                 now,
	}
	if disabledByOwnerAdminID <= 0 {
		recordUpdates["disabled_by_owner_admin_id"] = 0
	}
	if err := db.Model(&model.ClientRecord{}).
		Where("owner_admin_id = ? AND email IN ?", ownerAdminID, cleanEmails).
		Updates(recordUpdates).Error; err != nil {
		return needRestart, int64(len(cleanEmails)), err
	}

	return needRestart, int64(len(cleanEmails)), nil
}

func (s *InboundService) EnableClientsDisabledByOwnerAdminID(ownerAdminID int) (bool, int64, error) {
	if ownerAdminID <= 0 {
		return false, 0, common.NewError("invalid admin id")
	}
	db := database.GetDB()
	if db == nil {
		return false, 0, common.NewError("database is not initialized")
	}

	now := time.Now().UnixMilli()
	var rawEmails []string
	if err := db.Table("clients AS c").
		Select("c.email").
		Joins("JOIN client_traffics AS ct ON ct.email = c.email").
		Where("c.owner_admin_id = ? AND c.disabled_by_owner_admin_id = ? AND c.enable = ? AND ct.enable = ?",
			ownerAdminID, ownerAdminID, false, false).
		Where("(ct.total <= 0 OR COALESCE(ct.up, 0)+COALESCE(ct.down, 0) < ct.total)").
		Where("(ct.expiry_time <= 0 OR ct.expiry_time > ?)", now).
		Pluck("c.email", &rawEmails).Error; err != nil {
		return false, 0, err
	}

	cleanEmails := uniqueEmails(FilterVisibleClientEmails(rawEmails))
	if len(cleanEmails) == 0 {
		return false, 0, nil
	}

	if err := db.Model(xray.ClientTraffic{}).
		Where("email IN ?", cleanEmails).
		Update("enable", true).Error; err != nil {
		return false, int64(len(cleanEmails)), err
	}
	if err := db.Model(&model.ClientRecord{}).
		Where("owner_admin_id = ? AND disabled_by_owner_admin_id = ? AND email IN ?", ownerAdminID, ownerAdminID, cleanEmails).
		Updates(map[string]any{"enable": true, "disabled_by_owner_admin_id": 0, "updated_at": now}).Error; err != nil {
		return false, int64(len(cleanEmails)), err
	}

	needRestart := false
	for inboundID, group := range emailsByInbound(db, cleanEmails) {
		if restart, err := s.enableClientsInSettings(db, inboundID, group); err != nil {
			return needRestart, int64(len(cleanEmails)), err
		} else if restart {
			needRestart = true
		}
	}
	return needRestart, int64(len(cleanEmails)), nil
}

func emailsByInbound(db *gorm.DB, emails []string) map[int]map[string]struct{} {
	type row struct {
		InboundID int    `gorm:"column:inbound_id"`
		Email     string `gorm:"column:email"`
	}
	var rows []row
	_ = db.Raw(`
		SELECT client_inbounds.inbound_id AS inbound_id, clients.email AS email
		FROM clients
		JOIN client_inbounds ON client_inbounds.client_id = clients.id
		WHERE clients.email IN ?
	`, emails).Scan(&rows)
	out := map[int]map[string]struct{}{}
	for _, r := range rows {
		if out[r.InboundID] == nil {
			out[r.InboundID] = make(map[string]struct{})
		}
		out[r.InboundID][r.Email] = struct{}{}
	}
	return out
}

func (s *InboundService) enableClientsInSettings(db *gorm.DB, inboundID int, emails map[string]struct{}) (bool, error) {
	var ib model.Inbound
	if err := db.Model(&model.Inbound{}).Where("id = ?", inboundID).First(&ib).Error; err != nil {
		return false, err
	}
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
		return false, err
	}
	clients, _ := settings["clients"].([]any)
	now := time.Now().UnixMilli()
	mutated := false
	for i := range clients {
		entry, ok := clients[i].(map[string]any)
		if !ok {
			continue
		}
		email, _ := entry["email"].(string)
		if _, hit := emails[email]; !hit {
			continue
		}
		entry["enable"] = true
		entry["updated_at"] = now
		clients[i] = entry
		mutated = true
	}
	if !mutated {
		return false, nil
	}
	settings["clients"] = clients
	raw, err := json.Marshal(settings)
	if err != nil {
		return false, err
	}
	return ib.NodeID != nil, db.Model(&ib).Update("settings", string(raw)).Error
}

func uniqueEmails(emails []string) []string {
	if len(emails) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(emails))
	out := make([]string, 0, len(emails))
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		key := strings.ToLower(email)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, email)
	}
	return out
}

func (s *InboundService) SyncAndEnforceAdminUsageLimits() (bool, bool, error) {
	db := database.GetDB()
	if db == nil {
		return false, false, fmt.Errorf("database is not initialized")
	}

	if _, err := s.SyncAdminUsedBytes(); err != nil {
		return false, false, err
	}

	var users []model.User
	if err := db.Where("data_limit > 0").Find(&users).Error; err != nil {
		return false, false, err
	}

	needRestart := false
	clientsDisabled := false
	for _, user := range users {
		if adminUserIsOwner(db, user.RoleId) {
			continue
		}
		if user.DataLimit > 0 && user.UsedBytes >= user.DataLimit {
			restart, n, err := s.DisableClientsByQuotaOwnerAdminID(user.Id)
			if err != nil {
				return needRestart, clientsDisabled, err
			}
			if n > 0 {
				clientsDisabled = true
			}
			if restart {
				needRestart = true
			}
			continue
		}
		restart, n, err := s.EnableClientsDisabledByOwnerAdminID(user.Id)
		if err != nil {
			return needRestart, clientsDisabled, err
		}
		if n > 0 && restart {
			needRestart = true
		}
	}
	if err := s.SyncAdminQuotaTelegramNotices(); err != nil {
		return needRestart, clientsDisabled, err
	}
	return needRestart, clientsDisabled, nil
}

func adminUserIsOwner(db *gorm.DB, roleID int) bool {
	if roleID <= 0 {
		return false
	}
	var role model.AdminRole
	if err := db.Where("id = ?", roleID).First(&role).Error; err != nil {
		return false
	}
	return role.OwnerRole
}