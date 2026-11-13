package web

import (
	"os"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/testsafety"
	"github.com/mhsanaei/3x-ui/v3/internal/web/global"
)

// The route walks reach every handler if a gate regresses; testsafety turns the
// update, restart and download paths behind them into recorded test failures.
func TestMain(m *testing.M) {
	os.Exit(testsafety.Run(m, func(g *testsafety.Guard) {
		global.SetRestartHook(func() { g.Record("panel restart requested (would send SIGHUP)") })
	}))
}
