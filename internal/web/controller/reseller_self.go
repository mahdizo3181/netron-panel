package controller

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
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
