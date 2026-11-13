package controller

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

var errNotReseller = errors.New("only reseller accounts can use this")

// ResellerSelfController serves a reseller's own account; RoutePolicies admits resellers here.
type ResellerSelfController struct {
	resellerService service.ResellerService
}

func NewResellerSelfController(g *gin.RouterGroup) *ResellerSelfController {
	a := &ResellerSelfController{}
	g = g.Group("/reseller/me")
	g.GET("", a.me)
	g.POST("/totp", a.setTotp)
	g.POST("/totp/disable", a.disableTotp)
	g.POST("/password", a.changePassword)
	return a
}

func (a *ResellerSelfController) me(c *gin.Context) {
	id, isReseller := actorResellerID(c)
	if !isReseller {
		jsonObj(c, service.ResellerMe{Role: "admin"}, nil)
		return
	}
	me, err := a.resellerService.Me(id, time.Now())
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.obtain"), err)
		return
	}
	jsonObj(c, me, nil)
}

func (a *ResellerSelfController) setTotp(c *gin.Context) {
	id, isReseller := actorResellerID(c)
	if !isReseller {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.totp"), errNotReseller)
		return
	}
	err := a.resellerService.SetTotp(id, c.PostForm("secret"), c.PostForm("code"), time.Now())
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.totp"), err)
}

func (a *ResellerSelfController) disableTotp(c *gin.Context) {
	id, isReseller := actorResellerID(c)
	if !isReseller {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.totp"), errNotReseller)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.totp"), a.resellerService.DisableTotp(id, c.PostForm("code"), time.Now()))
}

// changePassword needs the current password; it ends the reseller's other
// sessions and keeps this one by re-stamping its epoch.
func (a *ResellerSelfController) changePassword(c *gin.Context) {
	id, isReseller := actorResellerID(c)
	if !isReseller {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.password"), errNotReseller)
		return
	}
	r, err := a.resellerService.ChangeOwnPassword(id, c.PostForm("currentPassword"), c.PostForm("newPassword"))
	if err == nil {
		err = session.SetLoginReseller(c, r)
	}
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.password"), err)
}

// resellerDefaultSettingsKeys are the subscription and display settings a reseller
// needs to show its clients' subscription links.
var resellerDefaultSettingsKeys = []string{
	"subEnable", "subURI", "subJsonEnable", "subJsonURI", "subClashEnable", "subClashURI",
	"subTitle", "datepicker", "pageSize", "expireDiff", "trafficDiff",
}

func resellerDefaultSettings(all any) any {
	m, ok := all.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(resellerDefaultSettingsKeys))
	for _, k := range resellerDefaultSettingsKeys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}
