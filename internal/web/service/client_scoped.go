package service

import (
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// ClientOwner is the reseller that owns a client, as the admin sees it.
type ClientOwner struct {
	Id   int    `json:"id" example:"3"`
	Name string `json:"name" example:"Shop A"`
}

// OwnerChange reports a reassignment; OutsideInbounds are attachments outside the new owner's allowed list.
type OwnerChange struct {
	Email           string       `json:"email" example:"alice@example.com"`
	Owner           *ClientOwner `json:"owner"`
	OutsideInbounds []int        `json:"outsideInbounds" example:"[7]"`
}

// The *For methods run a client operation for an actor: the admin goes straight to
// the unscoped method; a reseller is held to its own clients, inbounds and limits.

func (s *ClientService) CreateFor(scope Scope, inboundSvc *InboundService, payload *ClientCreatePayload) (bool, error) {
	if scope.IsAdmin() {
		return s.Create(inboundSvc, payload)
	}
	result, needRestart, err := s.createForReseller(scope, inboundSvc, []*ClientCreatePayload{payload}, false)
	if err == nil && len(result.Skipped) > 0 {
		err = common.NewError(result.Skipped[0].Reason)
	}
	return needRestart, err
}

func (s *ClientService) BulkCreateFor(scope Scope, inboundSvc *InboundService, payloads []ClientCreatePayload) (BulkCreateResult, bool, error) {
	if scope.IsAdmin() {
		return s.BulkCreate(inboundSvc, payloads)
	}
	ptrs := make([]*ClientCreatePayload, len(payloads))
	for i := range payloads {
		ptrs[i] = &payloads[i]
	}
	return s.createForReseller(scope, inboundSvc, ptrs, true)
}

// createForReseller gates the whole batch under the reseller's lock, creates it
// through the normal paths, then stamps the owner on the clients it made.
func (s *ClientService) createForReseller(scope Scope, inboundSvc *InboundService, payloads []*ClientCreatePayload, bulk bool) (BulkCreateResult, bool, error) {
	unlock := lockReseller(*scope.ResellerID)
	defer unlock()
	db := database.GetDB()
	r, err := loadReseller(db, *scope.ResellerID)
	if err != nil {
		return BulkCreateResult{}, false, err
	}
	var fresh []string
	for _, p := range payloads {
		stripResellerClientInput(p)
		// Emails are unique ignoring case, so "TAKEN@X" is the admin's "taken@x".
		var existing model.ClientRecord
		err := db.Where("LOWER(email) = ?", strings.ToLower(p.Client.Email)).First(&existing).Error
		switch {
		case err != nil && !database.IsNotFound(err):
			return BulkCreateResult{}, false, err
		case err != nil:
			fresh = append(fresh, p.Client.Email)
		case existing.ResellerID == nil || *existing.ResellerID != *scope.ResellerID:
			// Word for word what Create says for any taken email (§3.5 item 8).
			return BulkCreateResult{}, false, common.NewError("email already in use:", p.Client.Email)
		}
	}
	if err := gateNewClients(db, scope, r, payloads, len(fresh)); err != nil {
		return BulkCreateResult{}, false, err
	}
	var result BulkCreateResult
	var needRestart bool
	if bulk {
		values := make([]ClientCreatePayload, len(payloads))
		for i, p := range payloads {
			values[i] = *p
		}
		result, needRestart, err = s.BulkCreate(inboundSvc, values)
	} else {
		needRestart, err = s.Create(inboundSvc, payloads[0])
	}
	// Stamped even after a partial failure, so rows made on some inbounds do not fall
	// to the admin; exact emails only, and only ones that did not exist before.
	if len(fresh) > 0 {
		if stampErr := db.Model(&model.ClientRecord{}).
			Where("email IN ? AND reseller_id IS NULL", fresh).
			Update("reseller_id", *scope.ResellerID).Error; stampErr != nil && err == nil {
			err = stampErr
		}
	}
	return result, needRestart, err
}

func (s *ClientService) UpdateByEmailFor(scope Scope, inboundSvc *InboundService, email string, updated model.Client, limitHwid int, inboundFilter ...int) (bool, error) {
	if scope.IsAdmin() {
		return s.UpdateByEmail(inboundSvc, email, updated, limitHwid, inboundFilter...)
	}
	db := database.GetDB()
	if err := scope.RequireOwnEmails(db, []string{email}); err != nil {
		return false, err
	}
	if err := scope.RequireAllowedInbounds(db, inboundFilter); err != nil {
		return false, err
	}
	existing, err := s.GetRecordByEmail(nil, email)
	if err != nil {
		return false, err
	}
	r, err := loadReseller(db, *scope.ResellerID)
	if err != nil {
		return false, err
	}
	if updated.Enable && !existing.Enable && resellerBlocked(r, time.Now().UnixMilli()) {
		return false, ErrResellerBlocked
	}
	if r.RequireClientQuota && updated.TotalGB <= 0 {
		return false, ErrResellerQuotaRequired
	}
	// Admin-only fields keep their stored values; nil host rules means "unchanged".
	updated.Group = existing.Group
	updated.ClientHostRuleIds = nil
	updated.Reverse = nil
	return s.UpdateByEmail(inboundSvc, email, updated, limitHwid, inboundFilter...)
}

func (s *ClientService) DeleteByEmailFor(scope Scope, inboundSvc *InboundService, email string, keepTraffic bool) (bool, error) {
	if err := scope.RequireOwnEmails(database.GetDB(), []string{email}); err != nil {
		return false, err
	}
	return s.DeleteByEmail(inboundSvc, email, keepTraffic)
}

func (s *ClientService) BulkSetEnableFor(scope Scope, inboundSvc *InboundService, emails []string, enable bool) (BulkSetEnableResult, bool, error) {
	if !scope.IsAdmin() {
		db := database.GetDB()
		if err := scope.RequireOwnEmails(db, emails); err != nil {
			return BulkSetEnableResult{}, false, err
		}
		if enable {
			if err := s.refuseIfBlocked(scope); err != nil {
				return BulkSetEnableResult{}, false, err
			}
		}
	}
	return s.BulkSetEnable(inboundSvc, emails, enable)
}

// ResetTrafficByEmailFor is gated like an enable: a reset re-enables a depleted client.
func (s *ClientService) ResetTrafficByEmailFor(scope Scope, inboundSvc *InboundService, email string) (bool, error) {
	if !scope.IsAdmin() {
		if err := scope.RequireOwnEmails(database.GetDB(), []string{email}); err != nil {
			return false, err
		}
		if err := s.refuseIfBlocked(scope); err != nil {
			return false, err
		}
	}
	return s.ResetTrafficByEmail(inboundSvc, email)
}

func (s *ClientService) GetRecordByEmailFor(scope Scope, email string) (*model.ClientRecord, error) {
	if err := scope.RequireOwnEmails(database.GetDB(), []string{email}); err != nil {
		return nil, err
	}
	return s.GetRecordByEmail(nil, email)
}

func (s *ClientService) refuseIfBlocked(scope Scope) error {
	r, err := loadReseller(database.GetDB(), *scope.ResellerID)
	if err != nil {
		return err
	}
	if resellerBlocked(r, time.Now().UnixMilli()) {
		return ErrResellerBlocked
	}
	return nil
}

// ClientOwners maps owner ids to names for the admin's owner field.
func ClientOwners(ids []int) (map[int]*ClientOwner, error) {
	out := map[int]*ClientOwner{}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.Reseller
	if err := database.GetDB().Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Id] = &ClientOwner{Id: r.Id, Name: r.Name}
	}
	return out, nil
}

// SetOwner hands a client to a reseller (or back to the admin with nil). Past
// pool usage stays with the old owner; only future traffic bills the new one (§5.2).
func (s *ClientService) SetOwner(email string, resellerID *int) (*OwnerChange, error) {
	db := database.GetDB()
	rec, err := s.GetRecordByEmail(nil, email)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, ErrScopeNotFound
		}
		return nil, err
	}
	change := &OwnerChange{Email: rec.Email, OutsideInbounds: []int{}}
	if resellerID != nil {
		unlock := lockReseller(*resellerID)
		defer unlock()
		r, err := loadReseller(db, *resellerID)
		if err != nil {
			return nil, err
		}
		alreadyOwned := rec.ResellerID != nil && *rec.ResellerID == r.Id
		if !alreadyOwned && r.MaxClients > 0 {
			var owned int64
			if err := db.Model(&model.ClientRecord{}).Where("reseller_id = ?", r.Id).Count(&owned).Error; err != nil {
				return nil, err
			}
			if owned >= int64(r.MaxClients) {
				return nil, ErrResellerClientLimit
			}
		}
		change.Owner = &ClientOwner{Id: r.Id, Name: r.Name}
		attached, err := s.GetInboundIdsForRecord(rec.Id)
		if err != nil {
			return nil, err
		}
		for _, id := range attached {
			if ResellerScope(r.Id).RequireAllowedInbounds(db, []int{id}) != nil {
				change.OutsideInbounds = append(change.OutsideInbounds, id)
			}
		}
	}
	return change, db.Model(&model.ClientRecord{}).Where("id = ?", rec.Id).
		Updates(map[string]any{"reseller_id": resellerID, "pool_disabled": false}).Error
}

// GetInboundOptionsFor lists the inbounds the actor may put clients on.
func (s *InboundService) GetInboundOptionsFor(scope Scope, userId int) ([]InboundOption, error) {
	options, err := s.GetInboundOptions(userId)
	if err != nil || scope.IsAdmin() {
		return options, err
	}
	var allowed []int
	if err := database.GetDB().Model(&model.ResellerInbound{}).
		Where("reseller_id = ?", *scope.ResellerID).Pluck("inbound_id", &allowed).Error; err != nil {
		return nil, err
	}
	kept := options[:0]
	for _, o := range options {
		if slices.Contains(allowed, o.Id) {
			kept = append(kept, o)
		}
	}
	return kept, nil
}
