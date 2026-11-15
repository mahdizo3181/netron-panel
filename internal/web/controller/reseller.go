package controller

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

// ResellerController is the admin's reseller management (docs/RESELLERS.md, stage A).
type ResellerController struct {
	resellerService service.ResellerService
	inboundService  service.InboundService
	xrayService     service.XrayService
}

func NewResellerController(g *gin.RouterGroup) *ResellerController {
	a := &ResellerController{}
	g = g.Group("/resellers")
	g.GET("", a.list)
	g.GET("/:id", a.get)
	g.POST("", a.create)
	g.POST("/:id", a.update)
	g.POST("/:id/password", a.setPassword)
	g.POST("/:id/del", a.del)
	g.POST("/:id/resetTotp", a.resetTotp)
	g.POST("/:id/addVolume", a.addVolume)
	g.POST("/:id/resetUsage", a.resetUsage)
	g.POST("/:id/extendExpiry", a.extendExpiry)
	g.POST("/:id/disableClients", a.disableClients)
	g.POST("/:id/enableClients", a.enableClients)
	g.GET("/:id/audit", a.audit)
	g.GET("/poolEnforce", a.getPoolEnforce)
	g.POST("/poolEnforce", a.setPoolEnforce)
	return a
}

func (a *ResellerController) getPoolEnforce(c *gin.Context) {
	on, err := (&service.SettingService{}).GetResellerPoolEnforce()
	jsonMsgObj(c, I18nWeb(c, "pages.resellers.toasts.poolEnforce"), service.ResellerPoolEnforceView{On: on}, err)
}

func resellerID(c *gin.Context) (int, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid id %q", c.Param("id"))
	}
	return id, nil
}

// bindOptionalJSON reads a JSON body when there is one; an empty body leaves dst at its defaults.
func bindOptionalJSON(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (a *ResellerController) list(c *gin.Context) {
	rows, err := a.resellerService.List()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.obtain"), err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *ResellerController) get(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.obtain"), err)
		return
	}
	view, err := a.resellerService.Get(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.obtain"), err)
		return
	}
	jsonObj(c, view, nil)
}

func (a *ResellerController) create(c *gin.Context) {
	var payload service.ResellerPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.create"), err)
		return
	}
	view, err := a.resellerService.CreateAs(adminAuditActor(c), payload)
	jsonMsgObj(c, I18nWeb(c, "pages.resellers.toasts.create"), view, err)
}

func (a *ResellerController) update(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.update"), err)
		return
	}
	var payload service.ResellerPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.update"), err)
		return
	}
	view, err := a.resellerService.Update(id, payload)
	jsonMsgObj(c, I18nWeb(c, "pages.resellers.toasts.update"), view, err)
}

func (a *ResellerController) setPassword(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.password"), err)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.password"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.password"), a.resellerService.SetPasswordAs(adminAuditActor(c), id, body.Password))
}

func (a *ResellerController) del(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.delete"), err)
		return
	}
	var body struct {
		Clients string `json:"clients"`
	}
	if err := bindOptionalJSON(c, &body); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.delete"), err)
		return
	}
	needRestart, err := a.resellerService.DeleteAs(adminAuditActor(c), &a.inboundService, id, body.Clients)
	// Flagged before the error check: a partly-applied client delete still needs the restart.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.delete"), err)
}

// resetTotp turns off a reseller's 2FA when it lost its authenticator.
func (a *ResellerController) resetTotp(c *gin.Context) {
	id, err := resellerID(c)
	if err == nil {
		err = a.resellerService.ResetTotp(id)
	}
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.totp"), err)
}

func adminAuditActor(c *gin.Context) service.AuditActor {
	actor := service.AuditActor{Kind: "admin", Ip: c.ClientIP()}
	if u := session.GetLoginUser(c); u != nil {
		actor.Id = u.Id
	}
	return actor
}

func (a *ResellerController) addVolume(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.addVolume"), err)
		return
	}
	var body struct {
		Bytes int64 `json:"bytes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.addVolume"), err)
		return
	}
	err = a.resellerService.AddVolume(adminAuditActor(c), id, body.Bytes)
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.addVolume"), err)
}

func (a *ResellerController) resetUsage(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.resetUsage"), err)
		return
	}
	err = a.resellerService.ResetUsage(adminAuditActor(c), id)
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.resetUsage"), err)
}

func (a *ResellerController) extendExpiry(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.extendExpiry"), err)
		return
	}
	var body struct {
		ExpiryTime int64 `json:"expiryTime"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.extendExpiry"), err)
		return
	}
	err = a.resellerService.ExtendExpiry(adminAuditActor(c), id, body.ExpiryTime)
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.extendExpiry"), err)
}

func (a *ResellerController) disableClients(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.disableClients"), err)
		return
	}
	n, err := a.resellerService.DisableClients(adminAuditActor(c), id)
	jsonMsgObj(c, I18nWeb(c, "pages.resellers.toasts.disableClients"), map[string]int{"changed": n}, err)
}

func (a *ResellerController) enableClients(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.enableClients"), err)
		return
	}
	n, err := a.resellerService.EnableClients(adminAuditActor(c), id)
	jsonMsgObj(c, I18nWeb(c, "pages.resellers.toasts.enableClients"), map[string]int{"changed": n}, err)
}

func (a *ResellerController) audit(c *gin.Context) {
	id, err := resellerID(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.audit"), err)
		return
	}
	var p service.ResellerAuditPageParams
	if err := c.ShouldBindQuery(&p); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.audit"), err)
		return
	}
	p.ResellerId = id
	page, err := a.resellerService.ListAudit(service.AdminScope(), p)
	jsonMsgObj(c, I18nWeb(c, "pages.resellers.toasts.audit"), page, err)
}

func (a *ResellerController) setPoolEnforce(c *gin.Context) {
	var body struct {
		On bool `json:"on"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.poolEnforce"), err)
		return
	}
	err := a.resellerService.SetPoolEnforce(adminAuditActor(c), body.On)
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.poolEnforce"), err)
}
