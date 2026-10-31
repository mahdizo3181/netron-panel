package runtime

import (
	"maps"
	"net/url"
	"strconv"

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

// setWireUsageMultiplier sends the multiplier only when set, so an unset one
// reaches the node as "not sent" (keep) instead of an explicit x1.
func setWireUsageMultiplier(v url.Values, ib *model.Inbound) {
	if ib.UsageMultiplier != nil {
		v.Set("usageMultiplier", strconv.FormatFloat(*ib.UsageMultiplier, 'f', -1, 64))
	}
}

// UsageMultiplierApplied reports whether the node's last response said it bills
// per inbound; known is false until the node has answered once.
func (r *Remote) UsageMultiplierApplied() (known, applied bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capsSeen, r.appliesUsageMult
}
