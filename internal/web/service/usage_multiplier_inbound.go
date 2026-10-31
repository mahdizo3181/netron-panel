package service

import (
	"fmt"
	"math"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
)

// validateUsageMultiplier guards every inbound write: REST, UI, import, node push.
// nil means "not sent" and is always valid; the stored value or x1 applies.
func validateUsageMultiplier(protocol model.Protocol, m *float64) error {
	if m == nil {
		return nil
	}
	v := *m
	if math.IsNaN(v) || v < model.UsageMultiplierMin || v > model.UsageMultiplierMax {
		return fmt.Errorf("usage multiplier %v is outside %v..%v", v, model.UsageMultiplierMin, model.UsageMultiplierMax)
	}
	if v != 1 && !usagemult.SupportsProtocol(string(protocol)) {
		return fmt.Errorf("usage multiplier must stay 1 on %s inbounds: their traffic is not counted per client by Xray", protocol)
	}
	return nil
}

// applyUsageMultiplierUpdate takes the incoming multiplier only when the update
// carries one, then validates the result against the inbound's final protocol.
func applyUsageMultiplierUpdate(stored, incoming *model.Inbound) error {
	if incoming.UsageMultiplier != nil {
		v := *incoming.UsageMultiplier
		stored.UsageMultiplier = &v
	}
	return validateUsageMultiplier(stored.Protocol, stored.UsageMultiplier)
}

// adoptedUsageMultiplier is the node-reported multiplier worth adopting: nil when
// the node is an old build (no field) or reports a value no write path accepts.
func adoptedUsageMultiplier(snapIb *model.Inbound) *float64 {
	if snapIb.UsageMultiplier == nil || validateUsageMultiplier(snapIb.Protocol, snapIb.UsageMultiplier) != nil {
		return nil
	}
	v := *snapIb.UsageMultiplier
	return &v
}
