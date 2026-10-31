package runtime

import (
	"maps"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
)

// xrayInbound is ib as the core must see it: a copy whose clients carry their
// per-inbound stat emails while the usage multiplier mode is active.
func xrayInbound(ib *model.Inbound) *model.Inbound {
	settings := usagemult.RewriteSettingsEmails(ib.Id, string(ib.Protocol), ib.Settings)
	if settings == ib.Settings {
		return ib
	}
	cp := *ib
	cp.Settings = settings
	return &cp
}

func xrayUserEmail(ib *model.Inbound, email string) string {
	return usagemult.XrayEmail(ib.Id, string(ib.Protocol), email)
}

func xrayUser(ib *model.Inbound, user map[string]any) map[string]any {
	email, _ := user["email"].(string)
	if xe := xrayUserEmail(ib, email); xe != email {
		user = maps.Clone(user)
		user["email"] = xe
	}
	return user
}
