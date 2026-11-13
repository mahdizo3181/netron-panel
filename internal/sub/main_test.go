package sub

import (
	"os"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/testsafety"
	"github.com/mhsanaei/3x-ui/v3/internal/web/global"
)

// Reseller client tests here drive the real client services; testsafety turns
// any command, download or panel restart behind them into a recorded failure.
func TestMain(m *testing.M) {
	os.Exit(testsafety.Run(m, func(g *testsafety.Guard) {
		global.SetRestartHook(func() { g.Record("panel restart requested (would send SIGHUP)") })
	}))
}
