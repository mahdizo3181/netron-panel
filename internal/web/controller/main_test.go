package controller

import (
	"os"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/testsafety"
	"github.com/mhsanaei/3x-ui/v3/internal/web/global"
)

// Handler tests here call update, restart and import handlers directly; a
// missed stub must fail the test, not reach the host.
func TestMain(m *testing.M) {
	os.Exit(testsafety.Run(m, func(g *testsafety.Guard) {
		global.SetRestartHook(func() { g.Record("panel restart requested (would send SIGHUP)") })
	}))
}
