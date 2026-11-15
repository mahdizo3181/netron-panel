package service

import (
	"encoding/json"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

// AuditActor is who did an audited action; Kind is admin, reseller or system.
type AuditActor struct {
	Kind string
	Id   int
	Ip   string
}

var systemActor = AuditActor{Kind: "system"}

// recordResellerAudit writes one audit row in tx; detail holds counts and field
// names only, never secrets or request bodies (docs §8.3).
func recordResellerAudit(tx *gorm.DB, actor AuditActor, resellerID int, action, target string, detail map[string]any) error {
	raw := ""
	if len(detail) > 0 {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	return tx.Create(&model.ResellerAuditLog{
		At: time.Now().UnixMilli(), ActorKind: actor.Kind, ActorId: actor.Id, ResellerId: resellerID,
		Action: action, Target: target, Detail: raw, Ip: actor.Ip,
	}).Error
}
