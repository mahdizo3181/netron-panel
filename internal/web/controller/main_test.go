package controller

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/testsafety"
	"github.com/mhsanaei/3x-ui/v3/internal/web/global"
)

// Handler tests here call update, restart and import handlers directly; a
// missed stub must fail the test, not reach the host.
func TestMain(m *testing.M) {
	testsafety.Main(m, func(g *testsafety.Guard) {
		global.SetRestartHook(func() { g.Record("panel restart requested (would send SIGHUP)") })
	})
}
