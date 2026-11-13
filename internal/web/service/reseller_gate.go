package service

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

var (
	ErrResellerBlocked       = errors.New("this reseller account cannot add or enable clients now: its volume pool is used up, or it is expired or disabled")
	ErrResellerClientLimit   = errors.New("client limit reached for this reseller account")
	ErrResellerQuotaRequired = errors.New("every client of this reseller account needs a traffic limit")
)

// resellerLocks serialises one reseller's client-count changes, so a count check
// and the create it allows cannot interleave with another create.
// ponytail: per-process lock; two panel processes on one database would need a row lock.
var resellerLocks sync.Map

func lockReseller(id int) func() {
	mu, _ := resellerLocks.LoadOrStore(id, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return mu.(*sync.Mutex).Unlock
}

// resellerBlocked is the one rule for "may not add or enable clients" (docs §4.1).
func resellerBlocked(r *model.Reseller, nowMs int64) bool {
	return !r.Usable(nowMs) || r.CutoffReason != "" || (r.PoolBytes > 0 && r.UsedTraffic >= r.PoolBytes)
}

// blockedResellerIDs selects the blocked resellers as a subquery, for SQL filters.
func blockedResellerIDs(tx *gorm.DB, nowMs int64) *gorm.DB {
	return tx.Model(&model.Reseller{}).Select("id").Where(
		"enable = ? OR (expiry_time > 0 AND expiry_time <= ?) OR (pool_bytes > 0 AND used_traffic >= pool_bytes) OR cutoff_reason <> ''",
		false, nowMs)
}

// gateNewClients checks a batch of new clients against the account, its client
// limit, its allowed inbounds and its quota rule; newEmails are the ones not yet owned.
func gateNewClients(tx *gorm.DB, scope Scope, r *model.Reseller, payloads []*ClientCreatePayload, newEmails int) error {
	if resellerBlocked(r, time.Now().UnixMilli()) {
		return ErrResellerBlocked
	}
	var inboundIds []int
	for _, p := range payloads {
		if r.RequireClientQuota && p.Client.TotalGB <= 0 {
			return ErrResellerQuotaRequired
		}
		if len(p.InboundIds) == 0 {
			continue
		}
		inboundIds = append(inboundIds, p.InboundIds...)
	}
	if err := scope.RequireAllowedInbounds(tx, inboundIds); err != nil {
		return err
	}
	if r.MaxClients <= 0 || newEmails == 0 {
		return nil
	}
	var owned int64
	if err := tx.Model(&model.ClientRecord{}).Scopes(scope.ClientFilter).Count(&owned).Error; err != nil {
		return err
	}
	if owned+int64(newEmails) > int64(r.MaxClients) {
		return ErrResellerClientLimit
	}
	return nil
}

// stripResellerClientInput drops what a reseller may not set: groups, host rules
// and VLESS reverse are admin tools (docs §8.1).
func stripResellerClientInput(p *ClientCreatePayload) {
	p.Client.Group = ""
	p.Client.ClientHostRuleIds = nil
	p.Client.Reverse = nil
	p.Client.Email = strings.TrimSpace(p.Client.Email)
}
