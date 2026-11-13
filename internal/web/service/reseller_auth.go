package service

import (
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/util/totp"

	"gorm.io/gorm"
)

var (
	// ErrResellerTwoFactorRequired is returned only after the password matched.
	ErrResellerTwoFactorRequired = errors.New("two-factor code required")
	ErrResellerTotpSecret        = errors.New("two-factor secret must be base32, at least 16 characters")
	ErrResellerTotpCode          = errors.New("invalid 2fa code")
	ErrResellerTotpActive        = errors.New("two-factor authentication is already on; turn it off first")
	ErrResellerTotpInactive      = errors.New("two-factor authentication is not on")
)

// ResellerSelfView is what a reseller sees about its own account.
type ResellerSelfView struct {
	Username           string `json:"username" example:"shop-a"`
	Name               string `json:"name" example:"Shop A"`
	PoolBytes          int64  `json:"poolBytes" example:"1073741824000"`
	UsedTraffic        int64  `json:"usedTraffic" example:"53687091200"`
	RemainingBytes     int64  `json:"remainingBytes" example:"1020054732800"`
	MaxClients         int    `json:"maxClients" example:"100"`
	ClientCount        int64  `json:"clientCount" example:"42"`
	ExpiryTime         int64  `json:"expiryTime" example:"0"`
	RequireClientQuota bool   `json:"requireClientQuota" example:"false"`
	State              string `json:"state" example:"ok"`
	TwoFactorEnabled   bool   `json:"twoFactorEnabled" example:"false"`
}

// ResellerMe answers "who am I": role "admin", or "reseller" with its own numbers.
type ResellerMe struct {
	Role     string            `json:"role" example:"reseller"`
	Reseller *ResellerSelfView `json:"reseller,omitempty"`
}

var errResellerInvalidCredentials = errors.New("invalid credentials")

var totpSecretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func loadReseller(tx *gorm.DB, id int) (*model.Reseller, error) {
	var r model.Reseller
	if err := tx.First(&r, id).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrResellerNotFound
		}
		return nil, err
	}
	return &r, nil
}

// CheckLogin verifies a reseller login. Unknown, wrong-password, disabled and
// expired accounts all return the same error; 2FA is only mentioned after the password matched.
func (s *ResellerService) CheckLogin(username, password, code string, now time.Time) (*model.Reseller, error) {
	var r model.Reseller
	err := database.GetDB().Where("LOWER(username) = ?", strings.ToLower(strings.TrimSpace(username))).First(&r).Error
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errResellerInvalidCredentials
		}
		return nil, err
	}
	if !crypto.CheckPasswordHash(r.Password, password) || !r.Usable(now.UnixMilli()) {
		return nil, errResellerInvalidCredentials
	}
	if r.TotpSecret != "" {
		if code == "" {
			return nil, ErrResellerTwoFactorRequired
		}
		if !totp.VerifyWithSkew(r.TotpSecret, code, now) {
			return nil, ErrResellerTotpCode
		}
	}
	return &r, nil
}

func resellerState(r *model.Reseller, nowMs int64) string {
	switch {
	case !r.Enable:
		return "disabled"
	case r.ExpiryTime > 0 && r.ExpiryTime <= nowMs:
		return "expired"
	case r.PoolBytes > 0 && r.UsedTraffic >= r.PoolBytes:
		return "pool"
	}
	return "ok"
}

func (s *ResellerService) Me(id int, now time.Time) (*ResellerMe, error) {
	db := database.GetDB()
	r, err := loadReseller(db, id)
	if err != nil {
		return nil, err
	}
	views, err := resellerViews(db, []model.Reseller{*r})
	if err != nil {
		return nil, err
	}
	v := views[0]
	return &ResellerMe{Role: "reseller", Reseller: &ResellerSelfView{
		Username: v.Username, Name: v.Name, PoolBytes: v.PoolBytes, UsedTraffic: v.UsedTraffic,
		RemainingBytes: v.RemainingBytes, MaxClients: v.MaxClients, ClientCount: v.ClientCount,
		ExpiryTime: v.ExpiryTime, RequireClientQuota: v.RequireClientQuota,
		State: resellerState(r, now.UnixMilli()), TwoFactorEnabled: r.TotpSecret != "",
	}}, nil
}

// SetTotp turns on 2FA once the reseller proves the authenticator holds the secret.
func (s *ResellerService) SetTotp(id int, secret, code string, now time.Time) error {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	if len(secret) < 16 {
		return ErrResellerTotpSecret
	}
	if _, err := totpSecretEncoding.DecodeString(secret); err != nil {
		return ErrResellerTotpSecret
	}
	db := database.GetDB()
	r, err := loadReseller(db, id)
	if err != nil {
		return err
	}
	if r.TotpSecret != "" {
		return ErrResellerTotpActive
	}
	if !totp.VerifyWithSkew(secret, code, now) {
		return ErrResellerTotpCode
	}
	return db.Model(&model.Reseller{}).Where("id = ?", id).Update("totp_secret", secret).Error
}

func (s *ResellerService) DisableTotp(id int, code string, now time.Time) error {
	db := database.GetDB()
	r, err := loadReseller(db, id)
	if err != nil {
		return err
	}
	if r.TotpSecret == "" {
		return ErrResellerTotpInactive
	}
	if !totp.VerifyWithSkew(r.TotpSecret, code, now) {
		return ErrResellerTotpCode
	}
	return db.Model(&model.Reseller{}).Where("id = ?", id).Update("totp_secret", "").Error
}

// ResetTotp is the admin's way out for a reseller who lost its authenticator.
func (s *ResellerService) ResetTotp(id int) error {
	db := database.GetDB()
	if _, err := loadReseller(db, id); err != nil {
		return err
	}
	return db.Model(&model.Reseller{}).Where("id = ?", id).Update("totp_secret", "").Error
}

var ErrResellerCurrentPassword = errors.New("the current password is wrong")

// ChangeOwnPassword sets a new password after checking the current one, and
// returns the updated row so the caller can keep its own session.
func (s *ResellerService) ChangeOwnPassword(id int, current, next string) (*model.Reseller, error) {
	db := database.GetDB()
	r, err := loadReseller(db, id)
	if err != nil {
		return nil, err
	}
	if !crypto.CheckPasswordHash(r.Password, current) {
		return nil, ErrResellerCurrentPassword
	}
	if err := s.SetPassword(id, next); err != nil {
		return nil, err
	}
	return loadReseller(db, id)
}
