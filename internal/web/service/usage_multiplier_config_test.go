package service

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

type statEmailFixture struct {
	vless, trojan, wg *model.Inbound
}

func seedStatEmailFixture(t *testing.T) statEmailFixture {
	t.Helper()
	initUsageMultTestDB(t)
	alice := model.Client{Email: "Alice@x", ID: uuid.NewString(), Password: "pa", Enable: true}
	bob := model.Client{Email: "bob@x", ID: uuid.NewString(), Password: "pb", Enable: true}
	aliceWG := alice
	aliceWG.PublicKey = "mJ0Gq7cQnAJ4wB9OEaYjFHEX6x3sq0P0pYk1fY9qKHE="
	aliceWG.AllowedIPs = []string{"10.0.0.2/32"}

	f := statEmailFixture{
		vless:  mkInbound(t, 20001, model.VLESS, clientsSettings(t, []model.Client{alice, bob})),
		trojan: mkInbound(t, 20002, model.Trojan, clientsSettings(t, []model.Client{alice})),
		wg:     mkInbound(t, 20003, model.WireGuard, clientsSettings(t, []model.Client{aliceWG})),
	}
	for ib, cs := range map[*model.Inbound][]model.Client{f.vless: {alice, bob}, f.trojan: {alice}, f.wg: {aliceWG}} {
		if err := (&ClientService{}).SyncInbound(nil, ib.Id, cs); err != nil {
			t.Fatal(err)
		}
	}
	template := map[string]any{
		"log": map[string]any{}, "inbounds": []any{}, "outbounds": []any{map[string]any{"protocol": "freedom", "tag": "direct"}},
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "user": []string{"alice@x"}, "outboundTag": "direct"},
			map[string]any{"type": "field", "user": []string{"regexp:^bob"}, "outboundTag": "direct"},
		}},
	}
	if err := (&SettingService{}).saveSetting("xrayTemplateConfig", string(mustJSON(t, template))); err != nil {
		t.Fatal(err)
	}
	return f
}

func configEmails(t *testing.T, cfg *xray.Config, tag string) []string {
	t.Helper()
	for _, ib := range cfg.InboundConfigs {
		if ib.Tag != tag {
			continue
		}
		var s struct {
			Clients []struct{ Email string } `json:"clients"`
			Peers   []struct{ Email string } `json:"peers"`
		}
		if err := json.Unmarshal(ib.Settings, &s); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range append(s.Clients, s.Peers...) {
			out = append(out, c.Email)
		}
		slices.Sort(out)
		return out
	}
	t.Fatalf("inbound %s not in generated config", tag)
	return nil
}

func routingUsers(t *testing.T, cfg *xray.Config) [][]string {
	t.Helper()
	var r struct {
		Rules []struct {
			User []string `json:"user"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(cfg.RouterConfig, &r); err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, rule := range r.Rules {
		u := slices.Clone(rule.User)
		slices.Sort(u)
		out = append(out, u)
	}
	return out
}

func TestGetXrayConfigOffKeepsStockEmails(t *testing.T) {
	f := seedStatEmailFixture(t)
	cfg, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := configEmails(t, cfg, f.vless.Tag); !slices.Equal(got, []string{"Alice@x", "bob@x"}) {
		t.Fatalf("vless emails = %v", got)
	}
	users := routingUsers(t, cfg)
	if !slices.Equal(users[0], []string{"alice@x"}) || !slices.Equal(users[1], []string{"regexp:^bob"}) {
		t.Fatalf("routing users changed while OFF: %v", users)
	}
}

func TestGetXrayConfigOnUsesStatEmails(t *testing.T) {
	f := seedStatEmailFixture(t)
	usagemult.SetActive(true)
	cfg, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	vless := []string{usagemult.StatEmail(f.vless.Id, "alice@x"), usagemult.StatEmail(f.vless.Id, "bob@x")}
	slices.Sort(vless)
	if got := configEmails(t, cfg, f.vless.Tag); !slices.Equal(got, vless) {
		t.Fatalf("vless emails = %v, want %v", got, vless)
	}
	if got := configEmails(t, cfg, f.trojan.Tag); !slices.Equal(got, []string{usagemult.StatEmail(f.trojan.Id, "alice@x")}) {
		t.Fatalf("trojan emails = %v", got)
	}
	if got := configEmails(t, cfg, f.wg.Tag); !slices.Equal(got, []string{"Alice@x"}) {
		t.Fatalf("unsupported wireguard peer email rewritten: %v", got)
	}

	users := routingUsers(t, cfg)
	wantAlice := []string{"alice@x", usagemult.StatEmail(f.vless.Id, "alice@x"), usagemult.StatEmail(f.trojan.Id, "alice@x")}
	slices.Sort(wantAlice)
	if !slices.Equal(users[0], wantAlice) {
		t.Fatalf("alice's routing rule = %v, want %v (her stat emails, not bob's)", users[0], wantAlice)
	}
	if !slices.Equal(users[1], []string{"regexp:^bob"}) {
		t.Fatalf("regexp user entries must stay as written: %v", users[1])
	}
}
