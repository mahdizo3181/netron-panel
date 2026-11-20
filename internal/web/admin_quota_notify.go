package web

import (
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func (s *Server) wireAdminQuotaTelegramNotify() {
	service.AdminQuotaNotifySend = s.sendAdminQuotaTelegramNotice
}

func (s *Server) sendAdminQuotaTelegramNotice(_ int, chatID int64, kind string, usedBytes, dataLimit int64) bool {
	if !s.tgbotService.IsRunning() {
		return false
	}
	percent := 0
	if dataLimit > 0 {
		percent = int(usedBytes * 100 / dataLimit)
	}
	used := common.FormatTraffic(usedBytes)
	limit := common.FormatTraffic(dataLimit)
	params := []string{
		"Percent==" + strconv.Itoa(percent),
		"Used==" + used,
		"Limit==" + limit,
	}
	var key string
	if kind == "100" {
		key = "tgbot.messages.adminQuotaFull"
	} else {
		key = "tgbot.messages.adminQuotaWarn80"
	}
	msg := "⚠️ " + s.tgbotService.I18nBot(key, params...)
	if s.tgbotService.SendMsgToTgbot(chatID, msg) <= 0 {
		return false
	}
	return true
}