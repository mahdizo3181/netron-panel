package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// usageMultiplierStatus reports the saved/running per-inbound multiplier mode and
// the nodes that would count weighted inbounds at x1.
func (a *SettingController) usageMultiplierStatus(c *gin.Context) {
	st, err := (&service.InboundService{}).GetUsageMultiplierStatus()
	jsonObj(c, st, err)
}
