package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// multNodePanel answers every API call through the real node caps middleware;
// advertise decides whether it claims the per-inbound mode.
func multNodePanel(t *testing.T, advertise *bool) *model.Node {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		prev := usagemult.Active()
		usagemult.SetActive(*advertise)
		defer usagemult.SetActive(prev)
		middleware.ConfigEnvelopeMiddleware()(c)
	})
	engine.NoRoute(func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": true, "obj": []any{}}) })
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return &model.Node{Name: "node-" + u.Port(), Scheme: "http", Address: u.Hostname(), Port: port, ApiToken: "tok", AllowPrivateAddress: true, Enable: true, Status: "online", TlsVerifyMode: "verify", Guid: "g-" + u.Port()}
}

func withRuntimeManager(t *testing.T) *runtime.Manager {
	t.Helper()
	prev := runtime.GetManager()
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(prev) })
	return mgr
}

func contactNode(t *testing.T, mgr *runtime.Manager, n *model.Node) *runtime.Remote {
	t.Helper()
	rt, err := mgr.RemoteFor(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.FetchTrafficSnapshot(context.Background()); err != nil {
		t.Fatalf("contact node: %v", err)
	}
	return rt
}

func TestUsageMultiplierStatusReportsNodesThatDoNotApplyIt(t *testing.T) {
	initUsageMultTestDB(t)
	mgr := withRuntimeManager(t)
	oldNode, newNode := false, true
	a, b := multNodePanel(t, &oldNode), multNodePanel(t, &newNode)
	for _, n := range []*model.Node{a, b} {
		if err := database.GetDB().Create(n).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk := func(nodeID, port int, m float64) *model.Inbound {
		nid := nodeID
		ib := &model.Inbound{UserId: 1, Tag: "n-" + strconv.Itoa(port), Enable: true, Port: port, Protocol: model.VLESS, NodeID: &nid, UsageMultiplier: &m}
		if err := database.GetDB().Create(ib).Error; err != nil {
			t.Fatal(err)
		}
		return ib
	}
	weightedOnOld := mk(a.Id, 48001, 1.2)
	mk(a.Id, 48002, 1)
	weightedOnNew := mk(b.Id, 48003, 2)
	if err := (&SettingService{}).setBool(usageMultiplierEnableKey, true); err != nil {
		t.Fatal(err)
	}

	st, err := (&InboundService{}).GetUsageMultiplierStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Configured || st.Active {
		t.Fatalf("configured=%v active=%v, want saved ON but not yet latched", st.Configured, st.Active)
	}
	if len(st.Nodes) != 2 || st.Nodes[0].Known || st.Nodes[1].Known {
		t.Fatalf("before any contact both nodes must be listed as unknown: %+v", st.Nodes)
	}

	contactNode(t, mgr, a)
	contactNode(t, mgr, b)
	st, err = (&InboundService{}).GetUsageMultiplierStatus()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int]UsageMultiplierNodeStatus{}
	for _, n := range st.Nodes {
		byID[n.NodeId] = n
	}
	if s := byID[a.Id]; !s.Known || s.Applies || len(s.InboundIds) != 1 || s.InboundIds[0] != weightedOnOld.Id {
		t.Fatalf("old node status = %+v, want known, not applying, inbound %d only", s, weightedOnOld.Id)
	}
	if s := byID[b.Id]; !s.Known || !s.Applies || len(s.InboundIds) != 1 || s.InboundIds[0] != weightedOnNew.Id {
		t.Fatalf("new node status = %+v", s)
	}
}

func warningsMentioning(sub string) int {
	n := 0
	for _, line := range logger.GetLogs(500, "warning") {
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

// The log warns once when a node starts counting a weighted inbound at x1, not
// every 5s tick, and warns again only after the node recovered and regressed.
func TestWarnUsageMultiplierNodeLogsOncePerTransition(t *testing.T) {
	initUsageMultTestDB(t)
	mgr := withRuntimeManager(t)
	applies := false
	n := multNodePanel(t, &applies)
	if err := database.GetDB().Create(n).Error; err != nil {
		t.Fatal(err)
	}
	m := 1.5
	nid := n.Id
	if err := database.GetDB().Create(&model.Inbound{UserId: 1, Tag: "warn-in", Enable: true, Port: 48101, Protocol: model.VLESS, NodeID: &nid, UsageMultiplier: &m}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	marker := "node " + n.Name + " counts"
	before := warningsMentioning(marker)
	rt := contactNode(t, mgr, n)
	svc.WarnUsageMultiplierNode(n, rt)
	svc.WarnUsageMultiplierNode(n, rt)
	if got := warningsMentioning(marker) - before; got != 1 {
		t.Fatalf("warnings after two ticks = %d, want exactly 1", got)
	}
	applies = true
	contactNode(t, mgr, n)
	svc.WarnUsageMultiplierNode(n, rt)
	applies = false
	contactNode(t, mgr, n)
	svc.WarnUsageMultiplierNode(n, rt)
	if got := warningsMentioning(marker) - before; got != 2 {
		t.Fatalf("warnings after recovery and regression = %d, want 2", got)
	}
}
