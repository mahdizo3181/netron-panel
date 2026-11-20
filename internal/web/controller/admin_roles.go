package controller

import (
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"

	"github.com/gin-gonic/gin"
)

type AdminRoleController struct {
	roleService panel.AdminRoleService
}

func NewAdminRoleController(g *gin.RouterGroup) *AdminRoleController {
	a := &AdminRoleController{}
	a.initRouter(g)
	return a
}

func (a *AdminRoleController) initRouter(g *gin.RouterGroup) {
	g.GET("/list", requireAdminPermission("admins", "read"), a.list)
	g.GET("/get/:id", requireAdminPermission("admins", "read"), a.get)
	g.POST("/add", requireAdminPermission("admins", "create"), a.add)
	g.POST("/update/:id", requireAdminPermission("admins", "update"), a.update)
	g.POST("/del/:id", requireAdminPermission("admins", "delete"), a.del)
}

func (a *AdminRoleController) list(c *gin.Context) {
	rows, err := a.roleService.List()
	jsonObj(c, rows, err)
}

func (a *AdminRoleController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	row, err := a.roleService.Get(id)
	jsonObj(c, row, err)
}

func (a *AdminRoleController) add(c *gin.Context) {
	var payload panel.AdminRolePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, "create admin role", err)
		return
	}
	row, err := a.roleService.Create(payload)
	if err != nil {
		jsonMsg(c, "create admin role", err)
		return
	}
	jsonMsgObj(c, "create admin role", row, nil)
}

func (a *AdminRoleController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	var payload panel.AdminRolePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, "update admin role", err)
		return
	}
	row, err := a.roleService.Update(id, payload)
	if err != nil {
		jsonMsg(c, "update admin role", err)
		return
	}
	jsonMsgObj(c, "update admin role", row, nil)
}

func (a *AdminRoleController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	jsonMsg(c, "delete admin role", a.roleService.Delete(id))
}