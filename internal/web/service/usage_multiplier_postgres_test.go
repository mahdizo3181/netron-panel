package service

import (
	"os"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/testpg"
)

// Runs the multiplier suite on Postgres, where the per-inbound upsert, the
// qualified ON CONFLICT columns and LOWER(email) matching differ from SQLite.
func TestUsageMultiplierPostgres(t *testing.T) {
	if os.Getenv("XUI_DB_TYPE") != "postgres" || os.Getenv("XUI_DB_DSN") == "" {
		t.Skip("set XUI_DB_TYPE=postgres and XUI_DB_DSN")
	}
	for _, tc := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"weighted total and quota cut", TestUsageMultiplierBillsWeightedTotalAndCutsAtQuota},
		{"rounding", TestUsageMultiplierRounding},
		{"free inbound", TestUsageMultiplierZeroIsFreeButStaysOnline},
		{"legacy pass-through", TestUsageMultiplierLegacyEmailsPassThrough},
		{"corrupt value", TestUsageMultiplierCorruptValueNeverFree},
		{"ON with x1 equals OFF", TestUsageMultiplierOnWithAllX1MatchesOff},
		{"reset and delete", TestUsageMultiplierResetAndDeleteDropPerInboundRows},
		{"depleted", TestUsageMultiplierDelDepletedDropsPerInboundRows},
		{"depleted on one inbound", TestUsageMultiplierDelDepletedOnOneInboundKeepsStillAttachedRows},
		{"orphan reaper", TestUsageMultiplierReapSyncOrphansDropsPerInboundRows},
		{"fold traffic", TestFoldStatEmailTrafficUsesRealBytesPerEmail},
		{"fold online users", TestFoldStatEmailOnlineUsersMergesIPs},
		{"config stat emails", TestGetXrayConfigOnUsesStatEmails},
		{"snapshot adoption", TestNodeSnapshotAdoptsMultiplierOnlyWhenPresentAndValid},
		{"node totals not reweighted", TestNodeWeightedTotalsMergeOnceWithoutReweighting},
		{"status", TestUsageMultiplierStatusReportsNodesThatDoNotApplyIt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanup, err := testpg.IsolatePackage("usage_mult")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			tc.run(t)
		})
	}
}
