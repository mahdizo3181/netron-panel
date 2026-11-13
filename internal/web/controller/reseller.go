package controller

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
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
	return a
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
	view, err := a.resellerService.Create(payload)
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
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.password"), a.resellerService.SetPassword(id, body.Password))
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
	needRestart, err := a.resellerService.Delete(&a.inboundService, id, body.Clients)
	// Flagged before the error check: a partly-applied client delete still needs the restart.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	jsonMsg(c, I18nWeb(c, "pages.resellers.toasts.delete"), err)
}
