package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// ResellerAuditPruneJob drops reseller audit rows past the configured retention.
type ResellerAuditPruneJob struct{}

func NewResellerAuditPruneJob() *ResellerAuditPruneJob {
	return &ResellerAuditPruneJob{}
}

func (j *ResellerAuditPruneJob) Run() {
	if err := service.PruneResellerAuditLogs(); err != nil {
		logger.Warning("reseller audit prune:", err)
	}
}