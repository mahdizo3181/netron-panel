package session

import (
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Reseller logins use their own keys: an admin id never reads as a reseller id
// and back, although both tables start counting at 1.
const (
	loginResellerKey = "LOGIN_RESELLER"
	resellerEpochKey = "RESELLER_EPOCH"
)

// SetLoginReseller makes the session a reseller's and drops any admin login in it.
func SetLoginReseller(c *gin.Context, r *model.Reseller) error {
	if r == nil {
		return nil
	}
	s := sessions.Default(c)
	s.Delete(loginUserKey)
	s.Delete(loginEpochKey)
	s.Set(loginResellerKey, r.Id)
	s.Set(resellerEpochKey, r.LoginEpoch)
	return s.Save()
}

// GetLoginReseller reloads the session's reseller every call, so a disabled,
// expired, deleted or re-passworded account stops on its next request.
func GetLoginReseller(c *gin.Context) *model.Reseller {
	s := sessions.Default(c)
	obj := s.Get(loginResellerKey)
	if obj == nil {
		return nil
	}
	id, ok := sessionUserID(obj)
	var r model.Reseller
	valid := ok && database.GetDB() != nil &&
		database.GetDB().First(&r, id).Error == nil &&
		sessionEpochMatches(s.Get(resellerEpochKey), r.LoginEpoch) &&
		r.Usable(time.Now().UnixMilli())
	if !valid {
		dropResellerLogin(s)
		if err := s.Save(); err != nil {
			logger.Warning("session: failed to drop an ended reseller login:", err)
		}
		return nil
	}
	return &r
}

func dropResellerLogin(s sessions.Session) {
	s.Delete(loginResellerKey)
	s.Delete(resellerEpochKey)
}
