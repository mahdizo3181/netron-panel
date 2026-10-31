package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"
)

// The master learns from a node's real envelope middleware whether that node
// applies the multiplier: only a new build with the mode active advertises it.
func TestRemoteLearnsWhetherNodeAppliesUsageMultiplier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { usagemult.SetActive(false) })
	engine := gin.New()
	engine.Use(middleware.ConfigEnvelopeMiddleware())
	engine.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": true}) })
	srv := httptest.NewServer(engine)
	defer srv.Close()
	r := NewRemote(nodeForPlainServer(t, srv, "verify", "tok"), nil)

	if known, _ := r.UsageMultiplierApplied(); known {
		t.Fatal("capability reported before the node ever answered")
	}
	for _, nodeActive := range []bool{false, true, false} {
		usagemult.SetActive(nodeActive)
		if _, err := r.do(context.Background(), http.MethodGet, "ping", nil); err != nil {
			t.Fatalf("call node: %v", err)
		}
		known, applied := r.UsageMultiplierApplied()
		if !known || applied != nodeActive {
			t.Fatalf("node mode %v: master sees known=%v applied=%v", nodeActive, known, applied)
		}
	}
}

// An old node sends only the caps it knew; the master must read that as "not applied".
func TestRemoteTreatsOldNodeAsNotApplying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-3x-Node-Caps", "zstd,awg-kernel")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	r := NewRemote(nodeForPlainServer(t, srv, "verify", "tok"), nil)
	if _, err := r.do(context.Background(), http.MethodGet, "ping", nil); err != nil {
		t.Fatal(err)
	}
	if known, applied := r.UsageMultiplierApplied(); !known || applied {
		t.Fatalf("old node: known=%v applied=%v, want known and not applied", known, applied)
	}
}
