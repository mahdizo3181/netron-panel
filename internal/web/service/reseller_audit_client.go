package service

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

func (s Scope) actorForAudit() AuditActor {
	if s.Actor.Kind != "" {
		return s.Actor
	}
	if s.ResellerID != nil {
		return AuditActor{Kind: "reseller", Id: *s.ResellerID}
	}
	return AuditActor{Kind: "admin"}
}

func (s Scope) auditReseller(action, target string, detail map[string]any) error {
	if s.ResellerID == nil {
		return nil
	}
	return recordResellerAudit(database.GetDB(), s.actorForAudit(), *s.ResellerID, action, target, detail)
}

func auditBulkEmails(action string, count int, emails []string) map[string]any {
	detail := map[string]any{"count": count}
	if len(emails) > 0 {
		const maxSample = 5
		if len(emails) > maxSample {
			emails = emails[:maxSample]
		}
		detail["emails"] = emails
	}
	return detail
}

// auditContainsSecret fails tests when a detail string looks like a credential leak.
func auditContainsSecret(detail string) bool {
	lower := strings.ToLower(detail)
	for _, needle := range []string{"password", "bearer", "token", "secret"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}