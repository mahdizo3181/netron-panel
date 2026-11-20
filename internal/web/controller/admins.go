package controller

import (
	"strconv"

	core "github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"

	"github.com/gin-gonic/gin"
)

type AdminController struct {
	adminService panel.AdminService
}

func NewAdminController(g *gin.RouterGroup) *AdminController {
	a := &AdminController{}
	a.initRouter(g)
	return a
}

func (a *AdminController) initRouter(g *gin.RouterGroup) {
	g.GET("/current", a.current)
	g.GET("/stats", requireAdminPermission("admins", "read"), a.stats)
	g.GET("/list", requireAdminPermission("admins", "read"), a.list)
	g.GET("/get/:id", requireAdminPermission("admins", "read"), a.get)

	g.POST("/add", requireAdminPermission("admins", "create"), a.add)
	g.POST("/update/:id", requireAdminPermission("admins", "update"), a.update)
	g.POST("/del/:id", requireAdminPermission("admins", "delete"), a.del)
	g.POST("/enable/:id", requireAdminPermission("admins", "update"), a.enable)
	g.POST("/disable/:id", requireAdminPermission("admins", "update"), a.disable)
	g.POST("/resetUsage/:id", requireAdminPermission("admins", "reset_usage"), a.resetUsage)
	g.POST("/users/disableActive/:id", requireAdminPermission("admins", "update"), a.disableActiveClients)
	g.POST("/users/activateDisabled/:id", requireAdminPermission("admins", "update"), a.activateDisabledClients)
	g.POST("/users/removeAll/:id", requireAdminPermission("admins", "update"), a.removeAllClients)
}

func (a *AdminController) stats(c *gin.Context) {
	stats, err := a.adminService.Stats()
	jsonObj(c, stats, err)
}

func (a *AdminController) current(c *gin.Context) {
	user, role, ok := loginActiveAdminRole(c)
	if !ok {
		return
	}

	jsonObj(c, gin.H{
		"id":          user.Id,
		"username":    user.Username,
		"status":      user.Status,
		"roleId":      user.RoleId,
		"dataLimit":   user.DataLimit,
		"usedBytes":   user.UsedBytes,
		"permissions": role.PermissionsJSON,
		"limits":      role.LimitsJSON,
		"role": gin.H{
			"id":          role.Id,
			"name":        role.Name,
			"slug":        role.Slug,
			"ownerRole":   role.OwnerRole,
			"permissions": role.PermissionsJSON,
			"limits":      role.LimitsJSON,
		},
	}, nil)
}

func (a *AdminController) list(c *gin.Context) {
	rows, err := a.adminService.List()
	jsonObj(c, rows, err)
}

func (a *AdminController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	row, err := a.adminService.Get(id)
	jsonObj(c, row, err)
}

func (a *AdminController) add(c *gin.Context) {
	var payload panel.AdminPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, "create admin", err)
		return
	}
	row, err := a.adminService.Create(payload)
	if err != nil {
		jsonMsg(c, "create admin", err)
		return
	}
	jsonMsgObj(c, "create admin", row, nil)
}

func (a *AdminController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	var payload panel.AdminPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, "update admin", err)
		return
	}
	row, err := a.adminService.Update(id, payload)
	if err != nil {
		jsonMsg(c, "update admin", err)
		return
	}
	jsonMsgObj(c, "update admin", row, nil)
}

func (a *AdminController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	jsonMsg(c, "delete admin", a.adminService.Delete(id))
}

func (a *AdminController) enable(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	jsonMsg(c, "enable admin", a.adminService.SetEnabled(id, true))
}

func (a *AdminController) disable(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	jsonMsg(c, "disable admin", a.adminService.SetEnabled(id, false))
}

func (a *AdminController) resetUsage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	jsonMsg(c, "reset admin usage", a.adminService.ResetUsage(id))
}

func (a *AdminController) disableActiveClients(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	inboundSvc := &core.InboundService{}
	needRestart, count, opErr := inboundSvc.DisableClientsByOwnerAdminID(id)
	if opErr != nil {
		jsonMsg(c, "disable admin clients", opErr)
		return
	}
	if needRestart {
		(&core.XrayService{}).SetToNeedRestart()
	}
	jsonMsgObj(c, "disable admin clients", gin.H{"affected": count}, nil)
}

func (a *AdminController) activateDisabledClients(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	inboundSvc := &core.InboundService{}
	needRestart, count, opErr := inboundSvc.EnableClientsDisabledByOwnerAdminID(id)
	if opErr != nil {
		jsonMsg(c, "activate admin clients", opErr)
		return
	}
	if needRestart {
		(&core.XrayService{}).SetToNeedRestart()
	}
	jsonMsgObj(c, "activate admin clients", gin.H{"affected": count}, nil)
}

func (a *AdminController) removeAllClients(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	deleted, needRestart, opErr := a.adminService.RemoveAllClients(id)
	if opErr != nil {
		jsonMsg(c, "remove admin clients", opErr)
		return
	}
	if needRestart {
		(&core.XrayService{}).SetToNeedRestart()
	}
	jsonMsgObj(c, "remove admin clients", gin.H{"deleted": deleted}, nil)
}