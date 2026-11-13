package service

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

var (
	// ErrScopeNotFound hides whether a foreign object exists at all.
	ErrScopeNotFound = errors.New("not found")
	ErrScopeInbound  = errors.New("inbound is not available to this account")
)

// Scope limits a service call to what its actor may touch; the zero value is
// the admin, who is never limited (docs/RESELLERS.md §3.3).
type Scope struct {
	ResellerID *int
}

func AdminScope() Scope { return Scope{} }

func ResellerScope(id int) Scope { return Scope{ResellerID: &id} }

func (s Scope) IsAdmin() bool { return s.ResellerID == nil }

// ClientFilter restricts a query on clients to the actor's own; use with db.Scopes.
func (s Scope) ClientFilter(db *gorm.DB) *gorm.DB {
	if s.IsAdmin() {
		return db
	}
	return db.Where("clients.reseller_id = ?", *s.ResellerID)
}

// requireOwned counts the actor's rows matching the distinct keys; any shortfall is not found.
func (s Scope) requireOwned(tx *gorm.DB, column string, keys []any) error {
	if s.IsAdmin() || len(keys) == 0 {
		return nil
	}
	var n int64
	if err := tx.Model(&model.ClientRecord{}).Scopes(s.ClientFilter).Where(column+" IN ?", keys).Count(&n).Error; err != nil {
		return err
	}
	if n != int64(len(keys)) {
		return ErrScopeNotFound
	}
	return nil
}

func (s Scope) RequireOwnEmails(tx *gorm.DB, emails []string) error {
	keys := make([]any, 0, len(emails))
	seen := map[string]struct{}{}
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if _, dup := seen[e]; !dup {
			seen[e] = struct{}{}
			keys = append(keys, e)
		}
	}
	return s.requireOwned(tx, "LOWER(clients.email)", keys)
}

func (s Scope) RequireOwnClientIDs(tx *gorm.DB, ids []int) error {
	keys := make([]any, 0, len(ids))
	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		keys = append(keys, id)
	}
	return s.requireOwned(tx, "clients.id", keys)
}

func (s Scope) RequireOwnSubID(tx *gorm.DB, subID string) error {
	if s.IsAdmin() {
		return nil
	}
	var n int64
	if err := tx.Model(&model.ClientRecord{}).Scopes(s.ClientFilter).Where("clients.sub_id = ?", subID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return ErrScopeNotFound
	}
	return nil
}

// RequireAllowedInbounds fails unless every id is in the reseller's allowed list.
func (s Scope) RequireAllowedInbounds(tx *gorm.DB, inboundIds []int) error {
	ids := slices.Compact(slices.Sorted(slices.Values(inboundIds)))
	if s.IsAdmin() || len(ids) == 0 {
		return nil
	}
	var n int64
	if err := tx.Model(&model.ResellerInbound{}).
		Where("reseller_id = ? AND inbound_id IN ?", *s.ResellerID, ids).Count(&n).Error; err != nil {
		return err
	}
	if n != int64(len(ids)) {
		return ErrScopeInbound
	}
	return nil
}

// resellerHiddenKeys are admin-only client fields (owner, groups, host rules, reverse,
// MTProto ad tag) and the server's own addresses and keys (docs §8.2).
var resellerHiddenKeys = map[string]struct{}{
	"owner": {}, "group": {}, "groups": {}, "clientHostRuleIds": {}, "reverse": {}, "adTag": {},
	"nodeId": {}, "nodeAddress": {}, "listen": {}, "shareAddr": {}, "shareAddrStrategy": {},
	"awgServer": {}, "tuicServer": {}, "wgPublicKey": {}, "wgMtu": {}, "wgDns": {}, "mtprotoDomain": {},
	"usageMultiplier": {},
}

// StripForReseller returns v as JSON without resellerHiddenKeys at any depth; every
// response a reseller gets goes through it.
func StripForReseller(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	return json.Marshal(dropResellerHiddenKeys(tree))
}

func dropResellerHiddenKeys(node any) any {
	switch n := node.(type) {
	case map[string]any:
		for k, child := range n {
			if _, hidden := resellerHiddenKeys[k]; hidden {
				delete(n, k)
				continue
			}
			n[k] = dropResellerHiddenKeys(child)
		}
	case []any:
		for i := range n {
			n[i] = dropResellerHiddenKeys(n[i])
		}
	}
	return node
}
