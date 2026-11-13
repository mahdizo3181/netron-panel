package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// The SPA picks the reseller UI from the page itself, so no admin call is fired
// before it knows; an admin page must look exactly as before.
func TestPanelPageTellsTheSPAItsRole(t *testing.T) {
	h := newPanelHarness(t, "admin-role")
	h.reseller("shop-role")
	r := h.browser()
	r.mustLogin("shop-role", e2eResellerPassword)
	a := h.browser()
	a.mustLogin("admin-role", e2eAdminPassword)

	_, resellerPage := r.do(http.MethodGet, "/panel/clients", nil)
	_, adminPage := a.do(http.MethodGet, "/panel/clients", nil)
	_, loginPage := h.browser().do(http.MethodGet, "/", nil)
	if !strings.Contains(resellerPage, `window.X_UI_ROLE="reseller"`) {
		t.Fatalf("reseller SPA page lacks the role: %s", resellerPage)
	}
	for name, page := range map[string]string{"admin": adminPage, "login": loginPage} {
		if strings.Contains(page, "X_UI_ROLE") {
			t.Fatalf("%s page carries a role: %s", name, page)
		}
	}
}

func TestResellerChangesItsOwnPassword(t *testing.T) {
	h := newPanelHarness(t, "admin-pw")
	h.reseller("shop-pw")
	current := h.browser()
	current.mustLogin("shop-pw", e2eResellerPassword)
	other := h.browser()
	other.mustLogin("shop-pw", e2eResellerPassword)
	change := func(old, next string) envelope {
		_, e, _ := current.env(http.MethodPost, "/panel/api/reseller/me/password", url.Values{"currentPassword": {old}, "newPassword": {next}})
		return e
	}

	if e := change("not-my-password", "a-fresh-long-secret"); e.Success {
		t.Fatal("a wrong current password was accepted")
	}
	if e := change(e2eResellerPassword, "short"); e.Success || !strings.Contains(e.Msg, "at least 10 characters") {
		t.Fatalf("weak new password: %+v", e)
	}
	if code, _ := other.me(); code != http.StatusOK {
		t.Fatal("a refused change ended another session")
	}
	if e := change(e2eResellerPassword, "a-fresh-long-secret"); !e.Success {
		t.Fatalf("change: %+v", e)
	}
	if code, _ := current.me(); code != http.StatusOK {
		t.Fatalf("the session that changed the password ended: %d", code)
	}
	if code, _ := other.me(); code != http.StatusUnauthorized {
		t.Fatalf("another session survived the change: %d", code)
	}
	if e, raw := h.browser().login("shop-pw", "a-fresh-long-secret", ""); !e.Success {
		t.Fatalf("login with the new password: %s", raw)
	}
	_, e, _ := h.browser().env(http.MethodPost, "/panel/api/reseller/me/password", url.Values{"currentPassword": {"x"}, "newPassword": {"y"}})
	if e.Success {
		t.Fatal("password change without a session")
	}
}

// A reseller needs the subscription address and display settings to show links;
// nothing about bots, domains, logs or the multiplier.
func TestResellerDefaultSettingsAreTheWhitelist(t *testing.T) {
	h := newPanelHarness(t, "admin-defs")
	h.reseller("shop-defs")
	r := h.browser()
	r.mustLogin("shop-defs", e2eResellerPassword)
	a := h.browser()
	a.mustLogin("admin-defs", e2eAdminPassword)

	keys := func(b *browser) []string {
		_, e, raw := b.env(http.MethodPost, "/panel/api/setting/defaultSettings", url.Values{})
		var m map[string]any
		if !e.Success || json.Unmarshal(e.Obj, &m) != nil {
			t.Fatalf("defaultSettings: %s", raw)
		}
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	got := strings.Join(keys(r), ",")
	want := "datepicker,expireDiff,pageSize,subClashEnable,subClashURI,subEnable,subJsonEnable,subJsonURI,subTitle,subURI,trafficDiff"
	if got != want {
		t.Fatalf("reseller defaultSettings keys = %s, want %s", got, want)
	}
	admin := strings.Join(keys(a), ",")
	for _, k := range []string{"tgBotEnable", "webDomain", "usageMultiplierEnable", "accessLogEnable"} {
		if !strings.Contains(admin, k) {
			t.Errorf("the admin lost %s from defaultSettings: %s", k, admin)
		}
	}
}
