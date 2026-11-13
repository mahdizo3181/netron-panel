package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/xlzd/gotp"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/testpg"
	"github.com/mhsanaei/3x-ui/v3/internal/web/controller"
	"github.com/mhsanaei/3x-ui/v3/internal/web/global"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

const (
	e2eResellerPassword = "correct-horse-battery"
	e2eAdminPassword    = "admin-pass-e2e-123"
)

type panelHarness struct {
	t      *testing.T
	url    string
	routes []gin_route
}

type gin_route struct{ Method, Path string }

// newPanelHarness serves the real panel router (every middleware) over HTTP;
// adminName differs per test so the shared login limiter never couples tests.
func newPanelHarness(t *testing.T, adminName string) *panelHarness {
	t.Helper()
	cleanup, err := testpg.IsolatePackage("reseller_web")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	if err := (&panel.UserService{}).UpdateFirstUser(adminName, e2eAdminPassword); err != nil {
		t.Fatal(err)
	}
	previous := global.GetWebServer()
	s := NewServer(nil)
	s.cron = cron.New(cron.WithLocation(time.Local), cron.WithSeconds())
	global.SetWebServer(s)
	engine, err := s.initRouter()
	if err != nil {
		t.Fatalf("init router: %v", err)
	}
	srv := httptest.NewServer(engine)
	t.Cleanup(func() {
		srv.Close()
		s.cancel()
		global.SetWebServer(previous)
	})
	h := &panelHarness{t: t, url: srv.URL}
	for _, r := range engine.Routes() {
		h.routes = append(h.routes, gin_route{r.Method, r.Path})
	}
	return h
}

func (h *panelHarness) reseller(username string) *model.Reseller {
	h.t.Helper()
	v, err := (&service.ResellerService{}).Create(service.ResellerPayload{
		Username: username, Password: e2eResellerPassword, Name: "Shop", PoolBytes: 1000, MaxClients: 3,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	var r model.Reseller
	database.GetDB().First(&r, v.Id)
	return &r
}

type envelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

type browser struct {
	t    *testing.T
	h    *panelHarness
	c    *http.Client
	csrf string
}

func (h *panelHarness) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: h.t, h: h, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (b *browser) do(method, path string, form url.Values) (int, string) {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, b.h.url+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if b.csrf != "" {
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func (b *browser) env(method, path string, form url.Values) (int, envelope, string) {
	b.t.Helper()
	code, raw := b.do(method, path, form)
	var e envelope
	_ = json.Unmarshal([]byte(raw), &e)
	return code, e, raw
}

func (b *browser) login(user, pass, code string) (envelope, string) {
	b.t.Helper()
	if b.csrf == "" {
		_, e, raw := b.env(http.MethodGet, "/csrf-token", nil)
		if err := json.Unmarshal(e.Obj, &b.csrf); err != nil || b.csrf == "" {
			b.t.Fatalf("csrf-token: %s", raw)
		}
	}
	_, e, raw := b.env(http.MethodPost, "/login", url.Values{"username": {user}, "password": {pass}, "twoFactorCode": {code}})
	return e, raw
}

func (b *browser) mustLogin(user, pass string) {
	b.t.Helper()
	if e, raw := b.login(user, pass, ""); !e.Success {
		b.t.Fatalf("login %s: %s", user, raw)
	}
}

type meWire struct {
	Role     string `json:"role"`
	Reseller *struct {
		Username    string `json:"username"`
		PoolBytes   int64  `json:"poolBytes"`
		MaxClients  int    `json:"maxClients"`
		ClientCount int64  `json:"clientCount"`
		State       string `json:"state"`
	} `json:"reseller"`
}

func (b *browser) me() (int, meWire) {
	b.t.Helper()
	code, e, _ := b.env(http.MethodGet, "/panel/api/reseller/me", nil)
	var m meWire
	_ = json.Unmarshal(e.Obj, &m)
	return code, m
}

func TestResellerLoginSeesOnlyItsOwnAccountAndCanLogOut(t *testing.T) {
	h := newPanelHarness(t, "admin-me")
	h.reseller("shop-a")
	b := h.browser()
	b.mustLogin("shop-a", e2eResellerPassword)

	code, me := b.me()
	if code != http.StatusOK || me.Role != "reseller" || me.Reseller == nil || me.Reseller.Username != "shop-a" ||
		me.Reseller.PoolBytes != 1000 || me.Reseller.MaxClients != 3 || me.Reseller.State != "ok" {
		t.Fatalf("/reseller/me = %d %+v", code, me)
	}
	_, raw := b.do(http.MethodGet, "/panel/api/reseller/me", nil)
	for _, secret := range []string{"password", "totp", e2eResellerPassword} {
		if strings.Contains(strings.ToLower(raw), secret) {
			t.Fatalf("/reseller/me leaks %q: %s", secret, raw)
		}
	}
	if code, _ := b.do(http.MethodGet, "/panel/", nil); code != http.StatusOK {
		t.Fatalf("SPA shell for a reseller = %d, want 200", code)
	}
	if _, e, raw := b.env(http.MethodPost, "/logout", url.Values{}); !e.Success {
		t.Fatalf("logout: %s", raw)
	}
	if code, _ := b.me(); code != http.StatusUnauthorized {
		t.Fatalf("/reseller/me after logout = %d, want 401", code)
	}
}

var routeParam = regexp.MustCompile(`[:*][A-Za-z_]+`)

func concretePath(p string) string { return routeParam.ReplaceAllString(p, "1") }

// Deny by default: with a reseller session every admin-only API route answers
// 403 and every other admin-only route answers exactly as it does to a stranger.
func TestResellerSessionIsRefusedEveryAdminRoute(t *testing.T) {
	h := newPanelHarness(t, "admin-deny")
	h.reseller("shop-a")
	reseller := h.browser()
	reseller.mustLogin("shop-a", e2eResellerPassword)
	stranger := h.browser()
	stranger.login("nobody", "nothing", "") // same CSRF/session shape, no identity

	checked := map[string]bool{}
	for _, r := range h.routes {
		if controller.RoutePolicyOf(r.Method, r.Path) != controller.PolicyAdminOnly {
			continue
		}
		path := concretePath(r.Path)
		got, body := reseller.do(r.Method, path, url.Values{})
		if strings.HasPrefix(r.Path, "/panel/api/") {
			if got != http.StatusForbidden {
				t.Errorf("%s %s with a reseller session = %d %s, want 403", r.Method, r.Path, got, body)
			}
		} else if want, _ := stranger.do(r.Method, path, url.Values{}); got != want {
			t.Errorf("%s %s with a reseller session = %d, a stranger gets %d", r.Method, r.Path, got, want)
		}
		checked[r.Method+" "+r.Path] = true
	}
	for _, must := range []string{
		"GET /ws", "GET /panel/api/server/getDb", "GET /panel/api/server/getMigration", "POST /panel/api/server/importDB",
		"POST /panel/api/backuptotgbot", "GET /panel/api/clients/export", "POST /panel/api/server/logs/:count",
		"POST /panel/api/server/xraylogs/:count", "POST /panel/api/setting/all", "POST /panel/api/setting/update",
		"GET /panel/api/setting/apiTokens", "GET /panel/api/inbounds/list", "GET /panel/api/clients/list",
		"GET /panel/api/resellers", "POST /panel/api/resellers/:id/password", "GET /panel/api/openapi.json",
	} {
		if !checked[must] {
			t.Errorf("%s was not checked: it is missing from the router or classified as reachable", must)
		}
	}
	if len(checked) < 150 {
		t.Fatalf("only %d admin-only routes checked; the route walk is broken", len(checked))
	}
}

func TestResellerReachableRoutesAreExactlyTheSelfRoutes(t *testing.T) {
	var reachable []string
	for route, p := range controller.RoutePolicies {
		if p == controller.PolicyReseller && strings.HasPrefix(strings.Fields(route)[1], "/panel/api/") {
			reachable = append(reachable, route)
		}
	}
	sort.Strings(reachable)
	want := []string{
		"GET /panel/api/reseller/me",
		"POST /panel/api/reseller/me/totp",
		"POST /panel/api/reseller/me/totp/disable",
	}
	if strings.Join(reachable, ",") != strings.Join(want, ",") {
		t.Fatalf("API routes a reseller may call = %v, want exactly %v", reachable, want)
	}
}

// Admin id 1 and reseller id 1 coexist: neither session may be read as the other.
func TestAdminAndResellerSessionsNeverStandInForEachOther(t *testing.T) {
	h := newPanelHarness(t, "admin-sep")
	r := h.reseller("shop-a")
	var admin model.User
	database.GetDB().First(&admin)
	if r.Id != admin.Id {
		t.Fatalf("fixture: reseller id %d, admin id %d; the test needs them equal", r.Id, admin.Id)
	}

	rb := h.browser()
	rb.mustLogin("shop-a", e2eResellerPassword)
	if code, _ := rb.do(http.MethodGet, "/panel/api/server/status", nil); code != http.StatusForbidden {
		t.Fatalf("reseller session on an admin route = %d, want 403", code)
	}

	ab := h.browser()
	ab.mustLogin("admin-sep", e2eAdminPassword)
	if code, me := ab.me(); code != http.StatusOK || me.Role != "admin" || me.Reseller != nil {
		t.Fatalf("admin /reseller/me = %d %+v, want role admin and no reseller data", code, me)
	}
	if _, e, raw := ab.env(http.MethodPost, "/panel/api/reseller/me/totp/disable", url.Values{"code": {"123456"}}); e.Success || !strings.Contains(e.Msg, "reseller accounts") {
		t.Fatalf("admin on a reseller self route: %s", raw)
	}
	if code, e, _ := ab.env(http.MethodGet, "/panel/api/resellers", nil); code != http.StatusOK || !e.Success {
		t.Fatalf("admin keeps admin routes: %d", code)
	}

	// One cookie jar, both orders: the newest login is the only identity.
	rb.mustLogin("admin-sep", e2eAdminPassword)
	if code, me := rb.me(); code != http.StatusOK || me.Role != "admin" {
		t.Fatalf("reseller then admin login: me = %d %+v, want admin", code, me)
	}
	ab.mustLogin("shop-a", e2eResellerPassword)
	if code, _ := ab.do(http.MethodGet, "/panel/api/server/status", nil); code != http.StatusForbidden {
		t.Fatalf("admin then reseller login: admin route = %d, want 403", code)
	}
	if code, me := ab.me(); code != http.StatusOK || me.Role != "reseller" {
		t.Fatalf("admin then reseller login: me = %d %+v, want reseller", code, me)
	}
	// The replaced reseller login must be gone, not waiting behind the admin's.
	if err := (&panel.UserService{}).BumpLoginEpoch(); err != nil {
		t.Fatal(err)
	}
	if code, me := rb.me(); code != http.StatusUnauthorized {
		t.Fatalf("admin session ended: me = %d %+v, want 401, not the earlier reseller", code, me)
	}
}

func TestResellerSessionEndsImmediately(t *testing.T) {
	cases := map[string]func(id int){
		"password set by the admin": func(id int) {
			if err := (&service.ResellerService{}).SetPassword(id, "a-brand-new-password"); err != nil {
				t.Fatal(err)
			}
		},
		"disabled": func(id int) {
			off := false
			if _, err := (&service.ResellerService{}).Update(id, service.ResellerPayload{Enable: &off}); err != nil {
				t.Fatal(err)
			}
		},
		"expired": func(id int) {
			database.GetDB().Model(&model.Reseller{}).Where("id = ?", id).Update("expiry_time", time.Now().UnixMilli()-1000)
		},
		"deleted": func(id int) {
			if _, err := (&service.ResellerService{}).Delete(&service.InboundService{}, id, ""); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, end := range cases {
		t.Run(name, func(t *testing.T) {
			h := newPanelHarness(t, "admin-end")
			r := h.reseller("shop-a")
			b := h.browser()
			b.mustLogin("shop-a", e2eResellerPassword)
			if code, _ := b.me(); code != http.StatusOK {
				t.Fatalf("before: me = %d", code)
			}
			end(r.Id)
			if code, _ := b.me(); code != http.StatusUnauthorized {
				t.Fatalf("after %s: /reseller/me = %d, want 401", name, code)
			}
			stranger, _ := h.browser().do(http.MethodGet, "/panel/", nil)
			if code, _ := b.do(http.MethodGet, "/panel/", nil); code != stranger {
				t.Fatalf("after %s: SPA shell = %d, a stranger gets %d", name, code, stranger)
			}
		})
	}
}

// The login answer must not tell an admin name from a reseller name, and the
// limiter must treat both the same. Its names are its own: the limiter is process-wide.
func TestLoginRefusalsDoNotRevealTheAccountKind(t *testing.T) {
	h := newPanelHarness(t, "admin-kind")
	h.reseller("shop-kind")
	disabled := h.reseller("shop-kind-off")
	database.GetDB().Model(&model.Reseller{}).Where("id = ?", disabled.Id).Update("enable", false)
	expired := h.reseller("shop-kind-old")
	database.GetDB().Model(&model.Reseller{}).Where("id = ?", expired.Id).Update("expiry_time", 1)

	attempts := []struct{ name, user, pass string }{
		{"admin, wrong password", "admin-kind", "wrong-password"},
		{"reseller, wrong password", "shop-kind", "wrong-password"},
		{"unknown user", "nobody-here", "wrong-password"},
		{"disabled reseller, right password", "shop-kind-off", e2eResellerPassword},
		{"expired reseller, right password", "shop-kind-old", e2eResellerPassword},
	}
	var first string
	for _, a := range attempts {
		_, raw := h.browser().login(a.user, a.pass, "")
		if first == "" {
			first = raw
			if strings.Contains(raw, `"success":true`) {
				t.Fatalf("%s logged in: %s", a.name, raw)
			}
		} else if raw != first {
			t.Errorf("%s answers %s, the admin's wrong password answers %s", a.name, raw, first)
		}
	}

	for _, user := range []string{"admin-kind", "shop-kind"} {
		pass := e2eResellerPassword
		if user == "admin-kind" {
			pass = e2eAdminPassword
		}
		for range 5 {
			h.browser().login(user, "wrong-password", "")
		}
		if _, raw := h.browser().login(user, pass, ""); raw != first {
			t.Errorf("%s after 5 failures, right password: %s, want the same refusal %s", user, raw, first)
		}
	}
}

func TestResellerTwoFactorLogin(t *testing.T) {
	h := newPanelHarness(t, "admin-2fa")
	r := h.reseller("shop-a")
	const secret = "JBSWY3DPEHPK3PXPJBSWY3DP"
	now := time.Now()
	if err := (&service.ResellerService{}).SetTotp(r.Id, secret, gotp.NewDefaultTOTP(secret).AtTime(now), now); err != nil {
		t.Fatal(err)
	}
	_, wrongPass := h.browser().login("shop-a", "wrong-password", "")

	e, raw := h.browser().login("shop-a", e2eResellerPassword, "")
	var prompt struct {
		TwoFactorRequired bool `json:"twoFactorRequired"`
	}
	if e.Success || json.Unmarshal(e.Obj, &prompt) != nil || !prompt.TwoFactorRequired || e.Msg == "" {
		t.Fatalf("right password, no code: %s, want a two-factor prompt", raw)
	}
	if _, raw := h.browser().login("shop-a", e2eResellerPassword, "000000"); raw != wrongPass {
		t.Fatalf("wrong code answers %s, a wrong password answers %s", raw, wrongPass)
	}
	b := h.browser()
	if e, raw := b.login("shop-a", e2eResellerPassword, gotp.NewDefaultTOTP(secret).Now()); !e.Success {
		t.Fatalf("right code: %s", raw)
	}
	if code, me := b.me(); code != http.StatusOK || me.Role != "reseller" {
		t.Fatalf("after 2FA login: me = %d %+v", code, me)
	}
}
