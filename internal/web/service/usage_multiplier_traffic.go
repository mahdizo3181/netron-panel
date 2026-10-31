package service

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type statEmailOwner struct {
	clientID   int
	inboundID  int
	email      string
	multiplier float64
}

var warnedClampedMultiplier sync.Map

// resolveStatEmailTraffic replaces each stat-email delta with its client's billed
// delta and records real and billed bytes per inbound; other entries pass through.
func resolveStatEmailTraffic(tx *gorm.DB, traffics []*xray.ClientTraffic) ([]*xray.ClientTraffic, error) {
	owners, err := loadStatEmailOwners(tx, traffics)
	if err != nil || len(owners) == 0 {
		return traffics, err
	}
	now := time.Now().UnixMilli()
	out := make([]*xray.ClientTraffic, 0, len(traffics))
	var freeOnly []string
	for _, tr := range traffics {
		if tr == nil {
			continue
		}
		owner, ok := owners[tr.Email]
		if !ok {
			out = append(out, tr)
			continue
		}
		up, down := billableBytes(tr.Up, owner.multiplier), billableBytes(tr.Down, owner.multiplier)
		if err := upsertClientInboundTraffic(tx, owner, tr, up, down, now); err != nil {
			return nil, err
		}
		if up == 0 && down == 0 {
			freeOnly = append(freeOnly, owner.email)
			continue
		}
		out = append(out, &xray.ClientTraffic{Email: owner.email, Up: up, Down: down})
	}
	// A free inbound bills nothing, so the traffic UPDATE never runs for it.
	for _, batch := range chunkStrings(freeOnly, sqlInChunk) {
		if err := tx.Exec(
			fmt.Sprintf(`UPDATE client_traffics SET last_online = %s WHERE email IN ?`, database.GreatestExpr("last_online", "?")),
			now, batch,
		).Error; err != nil {
			return nil, err
		}
	}
	return out, nil
}

// loadStatEmailOwners resolves the stat-email-shaped entries that moved bytes.
func loadStatEmailOwners(tx *gorm.DB, traffics []*xray.ClientTraffic) (map[string]statEmailOwner, error) {
	var candidates []string
	for _, tr := range traffics {
		if tr != nil && tr.Up+tr.Down != 0 {
			candidates = append(candidates, tr.Email)
		}
	}
	return resolveStatEmailOwners(tx, candidates)
}

// resolveStatEmailOwners maps each stat-email-shaped string to its client, verified
// against the clients really attached to the inbound the string names.
func resolveStatEmailOwners(tx *gorm.DB, emails []string) (map[string]statEmailOwner, error) {
	candidates := map[string]struct{}{}
	inboundSet := map[int]struct{}{}
	for _, e := range emails {
		if id, ok := usagemult.ParseStatEmail(e); ok {
			candidates[e] = struct{}{}
			inboundSet[id] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(inboundSet))
	for id := range inboundSet {
		ids = append(ids, id)
	}
	var inbounds []model.Inbound
	if err := tx.Select("id", "usage_multiplier").Where("id IN ?", ids).Find(&inbounds).Error; err != nil {
		return nil, err
	}
	multiplier := make(map[int]float64, len(inbounds))
	for i := range inbounds {
		m, clamped := inbounds[i].EffectiveUsageMultiplier()
		if _, warned := warnedClampedMultiplier.LoadOrStore(inbounds[i].Id, true); clamped && !warned {
			logger.Warningf("usage multiplier: inbound %d stores an out-of-range multiplier, billing x%v", inbounds[i].Id, m)
		}
		multiplier[inbounds[i].Id] = m
	}
	var links []struct {
		InboundId int
		ClientId  int
		Email     string
	}
	if err := tx.Table("client_inbounds").
		Select("client_inbounds.inbound_id, clients.id AS client_id, clients.email").
		Joins("JOIN clients ON clients.id = client_inbounds.client_id").
		Where("client_inbounds.inbound_id IN ?", ids).
		Scan(&links).Error; err != nil {
		return nil, err
	}
	owners := make(map[string]statEmailOwner, len(candidates))
	for _, l := range links {
		stat := usagemult.StatEmail(l.InboundId, l.Email)
		m, known := multiplier[l.InboundId]
		if _, wanted := candidates[stat]; wanted && known {
			owners[stat] = statEmailOwner{clientID: l.ClientId, inboundID: l.InboundId, email: l.Email, multiplier: m}
		}
	}
	return owners, nil
}

// billableBytes keeps x1 exact even past float64's integer range.
func billableBytes(real int64, multiplier float64) int64 {
	if multiplier == 1 || real <= 0 {
		return real
	}
	v := math.Round(float64(real) * multiplier)
	if v >= float64(database.TrafficMax) {
		return database.TrafficMax
	}
	return int64(v)
}

func upsertClientInboundTraffic(tx *gorm.DB, o statEmailOwner, tr *xray.ClientTraffic, up, down, now int64) error {
	add := func(col string, v int64) clause.Expr {
		return gorm.Expr(database.ClampedAddExpr("client_inbound_traffics."+col), v)
	}
	row := model.ClientInboundTraffic{
		ClientId: o.clientID, InboundId: o.inboundID, Email: o.email, StatEmail: tr.Email,
		ActualUp: tr.Up, ActualDown: tr.Down, BillableUp: up, BillableDown: down,
		LastOnline: now, CreatedAt: now, UpdatedAt: now,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stat_email"}},
		DoUpdates: clause.Assignments(map[string]any{
			"client_id":     o.clientID,
			"inbound_id":    o.inboundID,
			"email":         o.email,
			"actual_up":     add("actual_up", tr.Up),
			"actual_down":   add("actual_down", tr.Down),
			"billable_up":   add("billable_up", up),
			"billable_down": add("billable_down", down),
			"last_online":   now,
			"updated_at":    now,
		}),
	}).Create(&row).Error
}
