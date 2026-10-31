package job

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray/xraytest"
)

// Over the IP limit, the temporary disconnect must remove and re-add the identity
// the core actually holds — the per-inbound stat email while the mode is active.
func TestDisconnectClientTemporarilyUsesStatEmail(t *testing.T) {
	setupIntegrationDB(t)
	usagemult.SetActive(true)
	t.Cleanup(func() { usagemult.SetActive(false) })

	const email = "ip-limit-mult@x"
	inbound := &model.Inbound{Id: 31, Protocol: model.VLESS, Tag: "ip-limit-mult", Settings: `{"clients":[]}`}
	stat := usagemult.StatEmail(inbound.Id, email)
	apiPort := xraytest.StartCore(t, xraytest.VlessInbound(t, inbound.Tag, stat))

	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	cfg, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"tag": "api", "port": apiPort}}})
	if err := os.WriteFile(filepath.Join(binDir, "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}

	clients := []model.Client{{Email: email, ID: "00000000-0000-0000-0000-000000000031"}}
	(&CheckClientIpJob{}).disconnectClientTemporarily(inbound, email, clients)

	for _, line := range logger.GetLogs(500, "warning") {
		if strings.Contains(line, "Failed to remove user "+email) || strings.Contains(line, "Failed to re-add user "+email) {
			t.Fatalf("disconnect targeted the logical email the core does not hold: %s", line)
		}
	}
	if got := xraytest.InboundUsers(t, apiPort, inbound.Tag); !slices.Equal(got, []string{stat}) {
		t.Fatalf("core users after disconnect cycle = %v, want [%s]", got, stat)
	}
}
