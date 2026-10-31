package model

import "math"

const (
	UsageMultiplierMin = 0.0
	UsageMultiplierMax = 10.0
)

// EffectiveUsageMultiplier is the factor applied to this inbound's client traffic.
// NULL is x1; a corrupt value never turns into 0 (free), so clamped reports it.
func (ib *Inbound) EffectiveUsageMultiplier() (multiplier float64, clamped bool) {
	if ib == nil || ib.UsageMultiplier == nil {
		return 1, false
	}
	m := *ib.UsageMultiplier
	switch {
	case math.IsNaN(m) || m < UsageMultiplierMin:
		return 1, true
	case m > UsageMultiplierMax:
		return UsageMultiplierMax, true
	}
	return m, false
}

// ClientInboundTraffic is one client's traffic on one inbound under the per-inbound
// multiplier: the real bytes Xray reported and the weighted bytes billed to the client.
type ClientInboundTraffic struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientId     int    `json:"clientId" gorm:"index;not null"`
	InboundId    int    `json:"inboundId" gorm:"index;not null"`
	Email        string `json:"email" gorm:"index;not null"`
	StatEmail    string `json:"statEmail" gorm:"uniqueIndex;not null"`
	ActualUp     int64  `json:"actualUp"`
	ActualDown   int64  `json:"actualDown"`
	BillableUp   int64  `json:"billableUp"`
	BillableDown int64  `json:"billableDown"`
	LastOnline   int64  `json:"lastOnline"`
	CreatedAt    int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
