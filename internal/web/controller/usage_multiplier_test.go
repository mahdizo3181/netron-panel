package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

type multAPI struct {
	t      *testing.T
	engine *gin.Engine
}

func newMultAPI(t *testing.T) *multAPI {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
	prev := runtime.GetManager()
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}}))
	t.Cleanup(func() { runtime.SetManager(prev) })
	row := &model.ApiToken{Name: "admin", Token: crypto.HashTokenSHA256("admin-token"), Enabled: true, Scope: model.ApiScopeAdmin}
	if err := database.GetDB().Create(row).Error; err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	a := &APIController{}
	api := engine.Group("/panel/api")
	api.Use(a.checkAPIAuth, a.enforceTokenScope)
	NewInboundController(api.Group("/inbounds"))
	return &multAPI{t: t, engine: engine}
}

func (m *multAPI) post(path string, form url.Values) string {
	m.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	m.engine.ServeHTTP(w, req)
	return w.Body.String()
}

func inboundForm(proto string, port int, mult string) url.Values {
	f := url.Values{
		"protocol": {proto},
		"port":     {strconv.Itoa(port)},
		"tag":      {"mult-" + strconv.Itoa(port)},
		"enable":   {"true"},
		"settings": {`{"clients":[]}`},
	}
	if mult != "" {
		f.Set("usageMultiplier", mult)
	}
	return f
}

func storedInbound(t *testing.T, port int) model.Inbound {
	t.Helper()
	var ib model.Inbound
	if err := database.GetDB().Where("port = ?", port).First(&ib).Error; err != nil {
		t.Fatalf("inbound on port %d not stored: %v", port, err)
	}
	return ib
}

func effective(t *testing.T, port int) float64 {
	t.Helper()
	ib := storedInbound(t, port)
	m, _ := ib.EffectiveUsageMultiplier()
	return m
}

func TestUsageMultiplierAPIWrites(t *testing.T) {
	api := newMultAPI(t)
	ok := func(body string) bool { return strings.Contains(body, `"success":true`) }

	cases := []struct {
		name  string
		proto string
		mult  string
		ok    bool
		want  float64
	}{
		{"omitted field is x1", "vless", "", true, 1},
		{"explicit zero is stored as free", "vless", "0", true, 0},
		{"fraction below one", "trojan", "0.5", true, 0.5},
		{"tunnel x1.2", "vmess", "1.2", true, 1.2},
		{"upper bound", "shadowsocks", "10", true, 10},
		{"above max rejected", "vless", "10.5", false, 0},
		{"negative rejected", "vless", "-1", false, 0},
		{"NaN rejected", "vless", "NaN", false, 0},
		{"wireguard cannot use it", "wireguard", "1.2", false, 0},
		{"tuic cannot use it", "tuic", "2", false, 0},
		{"mtproto cannot use it", "mtproto", "0", false, 0},
		{"unsupported protocol at x1 is fine", "mixed", "1", true, 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := 46000 + i
			body := api.post("/panel/api/inbounds/add", inboundForm(tc.proto, port, tc.mult))
			if ok(body) != tc.ok {
				t.Fatalf("add %s x%s: success=%v, want %v: %s", tc.proto, tc.mult, ok(body), tc.ok, body)
			}
			if !tc.ok {
				var n int64
				database.GetDB().Model(&model.Inbound{}).Where("port = ?", port).Count(&n)
				if n != 0 {
					t.Fatal("a rejected multiplier was stored")
				}
				return
			}
			if got := effective(t, port); got != tc.want {
				t.Fatalf("stored multiplier = %v, want %v", got, tc.want)
			}
		})
	}
}

// An update that does not carry the field (old client, old master, old node)
// must keep the stored multiplier, never reset it to 0 or 1.
func TestUsageMultiplierAPIUpdateKeepsValueWhenFieldAbsent(t *testing.T) {
	api := newMultAPI(t)
	const port = 46100
	if body := api.post("/panel/api/inbounds/add", inboundForm("vless", port, "1.2")); !strings.Contains(body, `"success":true`) {
		t.Fatalf("add: %s", body)
	}
	id := strconv.Itoa(storedInbound(t, port).Id)
	update := func(mult string) string {
		return api.post("/panel/api/inbounds/update/"+id, inboundForm("vless", port, mult))
	}

	if body := update(""); !strings.Contains(body, `"success":true`) {
		t.Fatalf("update without field: %s", body)
	}
	if got := effective(t, port); got != 1.2 {
		t.Fatalf("update without the field changed the multiplier to %v", got)
	}
	if body := update("0"); !strings.Contains(body, `"success":true`) || effective(t, port) != 0 {
		t.Fatalf("update to 0 (free): %s, stored %v", body, effective(t, port))
	}
	if body := update("11"); strings.Contains(body, `"success":true`) || effective(t, port) != 0 {
		t.Fatalf("update to 11 accepted or changed the row: %s", body)
	}
	if body := update("1.5"); !strings.Contains(body, `"success":true`) || effective(t, port) != 1.5 {
		t.Fatalf("update to 1.5: %s, stored %v", body, effective(t, port))
	}
	wg := inboundForm("wireguard", port, "")
	if body := api.post("/panel/api/inbounds/update/"+id, wg); strings.Contains(body, `"success":true`) {
		t.Fatalf("switching a x1.5 inbound to wireguard must be refused: %s", body)
	}
}

// An inbound exported before the feature carries no usageMultiplier; importing
// it must give x1, not 0.
func TestUsageMultiplierAPIImportOldExportIsX1(t *testing.T) {
	api := newMultAPI(t)
	old := `{"port":46200,"protocol":"vless","tag":"old-export","enable":true,"settings":"{\"clients\":[]}","streamSettings":"","sniffing":""}`
	if body := api.post("/panel/api/inbounds/import", url.Values{"data": {old}}); !strings.Contains(body, `"success":true`) {
		t.Fatalf("import old export: %s", body)
	}
	if got := effective(t, 46200); got != 1 {
		t.Fatalf("old export imported with multiplier %v, want 1", got)
	}
	bad := `{"port":46201,"protocol":"vless","tag":"bad-export","enable":true,"settings":"{\"clients\":[]}","usageMultiplier":42}`
	if body := api.post("/panel/api/inbounds/import", url.Values{"data": {bad}}); strings.Contains(body, `"success":true`) {
		t.Fatalf("import accepted multiplier 42: %s", body)
	}
}

// Free end to end: an inbound saved through the API with 0 bills nothing.
func TestUsageMultiplierAPIZeroBillsNothing(t *testing.T) {
	api := newMultAPI(t)
	usagemult.SetActive(true)
	t.Cleanup(func() { usagemult.SetActive(false) })
	const port = 46300
	if body := api.post("/panel/api/inbounds/add", inboundForm("vless", port, "0")); !strings.Contains(body, `"success":true`) {
		t.Fatalf("add: %s", body)
	}
	ib := storedInbound(t, port)
	c := model.Client{Email: "free@x", ID: "00000000-0000-0000-0000-000000046300", Enable: true, TotalGB: 1000}
	if err := (&service.ClientService{}).SyncInbound(nil, ib.Id, []model.Client{c}); err != nil {
		t.Fatal(err)
	}
	if err := (&service.InboundService{}).AddClientStat(database.GetDB(), ib.Id, &c); err != nil {
		t.Fatal(err)
	}
	delta := &xray.ClientTraffic{Email: usagemult.StatEmail(ib.Id, "free@x"), Up: 5000, Down: 5000}
	if _, _, err := (&service.InboundService{}).AddTraffic(nil, []*xray.ClientTraffic{delta}); err != nil {
		t.Fatal(err)
	}
	var ct xray.ClientTraffic
	if err := database.GetDB().Where("email = ?", "free@x").First(&ct).Error; err != nil {
		t.Fatal(err)
	}
	if ct.Up != 0 || ct.Down != 0 || !ct.Enable {
		t.Fatalf("free inbound billed %d/%d enable=%v over a 1000-byte quota", ct.Up, ct.Down, ct.Enable)
	}
}
