package service

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"

	"gorm.io/gorm"
)

var (
	ErrResellerNotFound      = errors.New("reseller not found")
	ErrResellerUsernameTaken = errors.New("username is already in use")
	ErrResellerUsername      = errors.New("username must be 3-32 characters: letters, digits, dot, dash or underscore")
	ErrResellerPassword      = errors.New("password must be at least 10 characters and differ from the username")
	ErrResellerLimits        = errors.New("pool, client limit and expiry must not be negative")
	ErrResellerInbound       = errors.New("inbound does not exist or its protocol is not available to resellers")
	ErrResellerDeleteMode    = errors.New("delete mode must be transfer or delete")
)

const (
	ResellerDeleteTransfer = "transfer"
	ResellerDeleteClients  = "delete"
)

// ResellerPayload is the admin's create/edit body; Username and Password are read on create only.
type ResellerPayload struct {
	Username           string `json:"username" example:"shop-a"`
	Password           string `json:"password" example:"a-long-password"`
	Name               string `json:"name" example:"Shop A"`
	Enable             *bool  `json:"enable" example:"true"`
	PoolBytes          int64  `json:"poolBytes" example:"1073741824000"`
	MaxClients         int    `json:"maxClients" example:"100"`
	ExpiryTime         int64  `json:"expiryTime" example:"0"`
	RequireClientQuota bool   `json:"requireClientQuota" example:"false"`
	InboundIds         []int  `json:"inboundIds" example:"[3,5]"`
}

// ResellerView is a reseller as the admin sees it; RemainingBytes is -1 without a pool limit.
type ResellerView struct {
	Id                 int    `json:"id" example:"3"`
	Username           string `json:"username" example:"shop-a"`
	Name               string `json:"name" example:"Shop A"`
	Enable             bool   `json:"enable" example:"true"`
	PoolBytes          int64  `json:"poolBytes" example:"1073741824000"`
	UsedTraffic        int64  `json:"usedTraffic" example:"53687091200"`
	RemainingBytes     int64  `json:"remainingBytes" example:"1020054732800"`
	MaxClients         int    `json:"maxClients" example:"100"`
	ClientCount        int64  `json:"clientCount" example:"42"`
	ExpiryTime         int64  `json:"expiryTime" example:"0"`
	RequireClientQuota bool   `json:"requireClientQuota" example:"false"`
	CutoffReason       string `json:"cutoffReason" example:""`
	InboundIds         []int  `json:"inboundIds" example:"[3,5]"`
	CreatedAt          int64  `json:"createdAt" example:"1735000000000"`
	UpdatedAt          int64  `json:"updatedAt" example:"1735100000000"`
}

type ResellerService struct{}

var resellerUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

// resellerProtocols are the inbounds a reseller may use; the other protocols'
// option payloads carry server blocks not yet reviewed for resellers (docs/RESELLERS.md Q6).
var resellerProtocols = map[model.Protocol]struct{}{
	model.VLESS: {}, model.VMESS: {}, model.Trojan: {}, model.Shadowsocks: {}, model.Hysteria: {},
}

func validResellerPassword(username, password string) bool {
	return len(password) >= 10 && !strings.EqualFold(username, password)
}

// ResellerUsernameExists reports whether a reseller already logs in with this name.
func ResellerUsernameExists(username string) (bool, error) {
	return usernameExists(database.GetDB(), &model.Reseller{}, username)
}

func usernameExists(tx *gorm.DB, table any, username string) (bool, error) {
	var n int64
	err := tx.Model(table).Where("LOWER(username) = ?", strings.ToLower(username)).Count(&n).Error
	return n > 0, err
}

func validateResellerLimits(p ResellerPayload) error {
	if p.PoolBytes < 0 || p.MaxClients < 0 || p.ExpiryTime < 0 {
		return ErrResellerLimits
	}
	return nil
}

// replaceResellerInbounds stores the allowed list, refusing unknown inbounds and
// protocols outside resellerProtocols.
func replaceResellerInbounds(tx *gorm.DB, resellerID int, inboundIds []int) error {
	ids := slices.Compact(slices.Sorted(slices.Values(inboundIds)))
	var inbounds []model.Inbound
	if len(ids) > 0 {
		if err := tx.Select("id", "protocol").Where("id IN ?", ids).Find(&inbounds).Error; err != nil {
			return err
		}
	}
	if len(inbounds) != len(ids) {
		return ErrResellerInbound
	}
	rows := make([]model.ResellerInbound, 0, len(ids))
	for _, ib := range inbounds {
		if _, ok := resellerProtocols[ib.Protocol]; !ok {
			return ErrResellerInbound
		}
		rows = append(rows, model.ResellerInbound{ResellerId: resellerID, InboundId: ib.Id})
	}
	if err := tx.Where("reseller_id = ?", resellerID).Delete(&model.ResellerInbound{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

func resellerViews(tx *gorm.DB, rows []model.Reseller) ([]ResellerView, error) {
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Id)
	}
	counts := map[int]int64{}
	inbounds := map[int][]int{}
	if len(ids) > 0 {
		var countRows []struct {
			ResellerID int
			N          int64
		}
		if err := tx.Model(&model.ClientRecord{}).Select("reseller_id, COUNT(*) AS n").
			Where("reseller_id IN ?", ids).Group("reseller_id").Scan(&countRows).Error; err != nil {
			return nil, err
		}
		for _, c := range countRows {
			counts[c.ResellerID] = c.N
		}
		var links []model.ResellerInbound
		if err := tx.Where("reseller_id IN ?", ids).Order("inbound_id").Find(&links).Error; err != nil {
			return nil, err
		}
		for _, l := range links {
			inbounds[l.ResellerId] = append(inbounds[l.ResellerId], l.InboundId)
		}
	}
	views := make([]ResellerView, 0, len(rows))
	for _, r := range rows {
		remaining := int64(-1)
		if r.PoolBytes > 0 {
			remaining = max(r.PoolBytes-r.UsedTraffic, 0)
		}
		allowed := inbounds[r.Id]
		if allowed == nil {
			allowed = []int{}
		}
		views = append(views, ResellerView{
			Id: r.Id, Username: r.Username, Name: r.Name, Enable: r.Enable,
			PoolBytes: r.PoolBytes, UsedTraffic: r.UsedTraffic, RemainingBytes: remaining,
			MaxClients: r.MaxClients, ClientCount: counts[r.Id], ExpiryTime: r.ExpiryTime,
			RequireClientQuota: r.RequireClientQuota, CutoffReason: r.CutoffReason,
			InboundIds: allowed, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return views, nil
}

func (s *ResellerService) List() ([]ResellerView, error) {
	db := database.GetDB()
	var rows []model.Reseller
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return resellerViews(db, rows)
}

func (s *ResellerService) Get(id int) (*ResellerView, error) {
	return getResellerView(database.GetDB(), id)
}

func getResellerView(tx *gorm.DB, id int) (*ResellerView, error) {
	var row model.Reseller
	if err := tx.First(&row, id).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrResellerNotFound
		}
		return nil, err
	}
	views, err := resellerViews(tx, []model.Reseller{row})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (s *ResellerService) Create(p ResellerPayload) (*ResellerView, error) {
	p.Username = strings.TrimSpace(p.Username)
	if !resellerUsernamePattern.MatchString(p.Username) {
		return nil, ErrResellerUsername
	}
	if !validResellerPassword(p.Username, p.Password) {
		return nil, ErrResellerPassword
	}
	if err := validateResellerLimits(p); err != nil {
		return nil, err
	}
	hash, err := crypto.HashPasswordAsBcrypt(p.Password)
	if err != nil {
		return nil, err
	}
	var view *ResellerView
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{&model.User{}, &model.Reseller{}} {
			taken, err := usernameExists(tx, table, p.Username)
			if err != nil {
				return err
			}
			if taken {
				return ErrResellerUsernameTaken
			}
		}
		enable := p.Enable == nil || *p.Enable
		row := model.Reseller{
			Username: p.Username, Password: hash, Name: strings.TrimSpace(p.Name),
			Enable: enable, PoolBytes: p.PoolBytes, MaxClients: p.MaxClients,
			ExpiryTime: p.ExpiryTime, RequireClientQuota: p.RequireClientQuota,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		// gorm inserts the column default (true) for a zero bool and copies it back into row.
		if !enable {
			if err := tx.Model(&row).Update("enable", false).Error; err != nil {
				return err
			}
		}
		if err := replaceResellerInbounds(tx, row.Id, p.InboundIds); err != nil {
			return err
		}
		view, err = getResellerView(tx, row.Id)
		return err
	})
	return view, err
}

func (s *ResellerService) Update(id int, p ResellerPayload) (*ResellerView, error) {
	if err := validateResellerLimits(p); err != nil {
		return nil, err
	}
	var view *ResellerView
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		current, err := getResellerView(tx, id)
		if err != nil {
			return err
		}
		enable := current.Enable
		if p.Enable != nil {
			enable = *p.Enable
		}
		if err := tx.Model(&model.Reseller{}).Where("id = ?", id).Updates(map[string]any{
			"name": strings.TrimSpace(p.Name), "enable": enable, "pool_bytes": p.PoolBytes,
			"max_clients": p.MaxClients, "expiry_time": p.ExpiryTime,
			"require_client_quota": p.RequireClientQuota, "updated_at": time.Now().UnixMilli(),
		}).Error; err != nil {
			return err
		}
		if err := replaceResellerInbounds(tx, id, p.InboundIds); err != nil {
			return err
		}
		view, err = getResellerView(tx, id)
		return err
	})
	return view, err
}

// SetPassword replaces the password and bumps the login epoch so open sessions end.
func (s *ResellerService) SetPassword(id int, password string) error {
	db := database.GetDB()
	var row model.Reseller
	if err := db.Select("id", "username").First(&row, id).Error; err != nil {
		if database.IsNotFound(err) {
			return ErrResellerNotFound
		}
		return err
	}
	if !validResellerPassword(row.Username, password) {
		return ErrResellerPassword
	}
	hash, err := crypto.HashPasswordAsBcrypt(password)
	if err != nil {
		return err
	}
	return db.Model(&model.Reseller{}).Where("id = ?", id).Updates(map[string]any{
		"password": hash, "login_epoch": gorm.Expr("login_epoch + 1"), "updated_at": time.Now().UnixMilli(),
	}).Error
}

// Delete removes a reseller. Its clients go to the admin (transfer, the default)
// or are deleted through the normal client delete path, which reaches nodes.
func (s *ResellerService) Delete(inboundSvc *InboundService, id int, mode string) (bool, error) {
	if mode == "" {
		mode = ResellerDeleteTransfer
	}
	if mode != ResellerDeleteTransfer && mode != ResellerDeleteClients {
		return false, ErrResellerDeleteMode
	}
	db := database.GetDB()
	if _, err := getResellerView(db, id); err != nil {
		return false, err
	}
	needRestart := false
	if mode == ResellerDeleteClients {
		var emails []string
		if err := db.Model(&model.ClientRecord{}).Where("reseller_id = ?", id).Pluck("email", &emails).Error; err != nil {
			return false, err
		}
		result, restart, err := (&ClientService{}).BulkDelete(inboundSvc, emails, false)
		needRestart = restart
		if err != nil {
			return needRestart, err
		}
		// The reseller stays until every client is gone, so a retry can finish the job.
		if len(result.Skipped) > 0 {
			return needRestart, fmt.Errorf("%d of %d clients could not be deleted (first: %s: %s)",
				len(result.Skipped), len(emails), result.Skipped[0].Email, result.Skipped[0].Reason)
		}
	}
	return needRestart, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ClientRecord{}).Where("reseller_id = ?", id).
			Updates(map[string]any{"reseller_id": nil, "pool_disabled": false}).Error; err != nil {
			return err
		}
		if err := tx.Where("reseller_id = ?", id).Delete(&model.ResellerInbound{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Reseller{}, id).Error
	})
}
