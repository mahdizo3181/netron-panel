package sub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

const (
	infoTestExpiredTemplate  = "⛔ {{EMAIL}} | پایان #1 + 🚀"
	infoTestDepletedTemplate = "🚫 {{EMAIL}} | {{TRAFFIC_USED}}/{{TRAFFIC_TOTAL}}"
)

// assertInfoVlessLink fails unless link is the non-functional VLESS info link and returns its remark.
func assertInfoVlessLink(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("info link %q does not parse: %v", link, err)
	}
	if u.Scheme != "vless" || u.User.Username() != "00000000-0000-0000-0000-000000000000" || u.Hostname() != "127.0.0.1" || u.Port() != "1" {
		t.Fatalf("info link %q: scheme/uuid/host/port = %s/%s/%s/%s, want vless/zero-uuid/127.0.0.1/1",
			link, u.Scheme, u.User.Username(), u.Hostname(), u.Port())
	}
	q := u.Query()
	if q.Get("encryption") != "none" || q.Get("security") != "none" || q.Get("type") != "tcp" {
		t.Fatalf("info link %q query = %v, want encryption=none security=none type=tcp", link, q)
	}
	return u.Fragment
}

func TestInfoNodeLink_VlessRemarkRoundTrip(t *testing.T) {
	remarks := []string{
		"plain",
		"🚀 user@test.com | 📊8.00GB | ⏳30D",
		"کاربر عزیز | حجم باقیمانده: ۵ گیگ",
		"a #hash# and | pipe",
		"plus+sign and %25 percent",
		"  leading and trailing  ",
	}
	for _, remark := range remarks {
		t.Run(remark, func(t *testing.T) {
			link := infoNodeLink("vless", remark)
			if strings.Contains(link, "+") {
				t.Fatalf("info link %q carries a literal +, which clients decode as a space", link)
			}
			if got := assertInfoVlessLink(t, link); got != remark {
				t.Fatalf("remark round-trip = %q, want %q", got, remark)
			}
		})
	}
}

func TestInfoNodeLink_UnknownTypeFallsBackToVless(t *testing.T) {
	if got := assertInfoVlessLink(t, infoNodeLink("", "r")); got != "r" {
		t.Fatalf("remark = %q, want r", got)
	}
}

func setSubSetting(t *testing.T, key, value string) {
	t.Helper()
	row := &model.Setting{Key: key}
	if err := database.GetDB().Where("key = ?", key).Assign(model.Setting{Value: value}).FirstOrCreate(row).Error; err != nil {
		t.Fatal(err)
	}
}

type infoNodeCase struct {
	name     string
	subID    string
	email    string
	expiry   int64
	used     int64
	wantDocs int
}

func infoNodeCases() []infoNodeCase {
	const gb = int64(1073741824)
	return []infoNodeCase{
		{name: "info", subID: "sub-info", email: "user1@test.com", used: 2 * gb, wantDocs: 2},
		{name: "expired", subID: "sub-exp", email: "exp@test.com", expiry: time.Now().Add(-24 * time.Hour).UnixMilli(), wantDocs: 1},
		{name: "depleted", subID: "sub-dep", email: "dep@test.com", used: 5 * gb, wantDocs: 1},
	}
}

// seedInfoNodeCase seeds one VLESS client with a 5 GB quota and enables the info node.
func seedInfoNodeCase(t *testing.T, c infoNodeCase) {
	t.Helper()
	setupInfoNodeTestDB(t)
	db := database.GetDB()
	const total = int64(5368709120)
	ib := &model.Inbound{
		Id: 1, UserId: 1, Remark: "Germany-VLESS", Enable: true, Port: 443, Protocol: model.VLESS,
		Settings: fmt.Sprintf(`{"clients":[{"id":"c1-uuid","email":%q,"subId":%q,"enable":true,"totalGB":%d,"expiryTime":%d}]}`,
			c.email, c.subID, total, c.expiry),
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientRecord{Id: 1, Email: c.email, SubID: c.subID, UUID: "c1-uuid", Enable: true, TotalGB: total, ExpiryTime: c.expiry}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientInbound{InboundId: 1, ClientId: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: 1, Email: c.email, Up: c.used, Total: total, ExpiryTime: c.expiry, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	setSubSetting(t, "subInfoNodeEnable", "true")
	setSubSetting(t, "subExpiredTemplate", infoTestExpiredTemplate)
	setSubSetting(t, "subTrafficDepletedTemplate", infoTestDepletedTemplate)
}

func wantInfoRemark(c infoNodeCase) string {
	switch c.name {
	case "expired":
		return "⛔ " + c.email + " | پایان #1 + 🚀"
	case "depleted":
		return "🚫 " + c.email + " | 5.00GB/5.00GB"
	}
	return c.email + "|📊3.00GB"
}

func getInfoNodeRaw(t *testing.T, subID string) ([]string, []string) {
	t.Helper()
	svc := NewSubService("{{EMAIL}}|📊{{TRAFFIC_LEFT}}")
	svc.subscriptionBody = true
	links, emails, _, _, err := svc.GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	return links, emails
}

func getInfoNodeJSON(t *testing.T, subID string) []map[string]any {
	t.Helper()
	jsonSvc := NewSubJsonService("", "", "", "", NewSubService("{{EMAIL}}|📊{{TRAFFIC_LEFT}}"))
	out, _, err := jsonSvc.GetJson(subID, "sub.example.com", true)
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	return parseSubJsonDocs(t, out)
}

func firstOutbound(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	outbounds, _ := doc["outbounds"].([]any)
	if len(outbounds) == 0 {
		t.Fatalf("doc has no outbounds: %v", doc)
	}
	ob, _ := outbounds[0].(map[string]any)
	return ob
}

func TestSubRaw_InfoNodeVlessByDefault(t *testing.T) {
	for _, c := range infoNodeCases() {
		t.Run(c.name, func(t *testing.T) {
			seedInfoNodeCase(t, c)
			links, emails := getInfoNodeRaw(t, c.subID)
			if len(links) != c.wantDocs {
				t.Fatalf("links = %d, want %d: %v", len(links), c.wantDocs, links)
			}
			if got := assertInfoVlessLink(t, links[0]); got != wantInfoRemark(c) {
				t.Fatalf("info remark = %q, want %q", got, wantInfoRemark(c))
			}
			if c.wantDocs == 2 && (!strings.HasPrefix(links[1], "vless://c1-uuid@") || strings.Contains(links[1], "00000000-0000")) {
				t.Fatalf("second link must be the real client link, got %q", links[1])
			}
			if len(emails) > 1 || (len(emails) == 1 && emails[0] != c.email) {
				t.Fatalf("emails = %v, the info link must not add a client", emails)
			}
		})
	}
}

func TestSubJSON_InfoNodeVlessByDefault(t *testing.T) {
	for _, c := range infoNodeCases() {
		t.Run(c.name, func(t *testing.T) {
			seedInfoNodeCase(t, c)
			docs := getInfoNodeJSON(t, c.subID)
			if len(docs) != c.wantDocs {
				t.Fatalf("docs = %d, want %d", len(docs), c.wantDocs)
			}
			if docs[0]["remarks"] != wantInfoRemark(c) {
				t.Fatalf("info remarks = %q, want %q", docs[0]["remarks"], wantInfoRemark(c))
			}
			ob := firstOutbound(t, docs[0])
			settings, _ := ob["settings"].(map[string]any)
			stream, _ := ob["streamSettings"].(map[string]any)
			if ob["protocol"] != "vless" || ob["tag"] != "proxy" ||
				settings["address"] != "127.0.0.1" || settings["port"] != float64(1) ||
				settings["id"] != "00000000-0000-0000-0000-000000000000" || settings["encryption"] != "none" ||
				stream["network"] != "tcp" || stream["security"] != "none" {
				t.Fatalf("info outbound = %v, want non-functional vless to 127.0.0.1:1", ob)
			}
			if c.wantDocs == 2 {
				real, _ := firstOutbound(t, docs[1])["settings"].(map[string]any)
				if real["id"] != "c1-uuid" {
					t.Fatalf("second doc must carry the real client, got %v", real)
				}
			}
		})
	}
}

// Captured from the SOCKS-only implementation before the link type option existed.
func TestSubRaw_InfoNodeLegacySocksUnchanged(t *testing.T) {
	c := infoNodeCases()[1]
	seedInfoNodeCase(t, c)
	setSubSetting(t, "subInfoNodeLinkType", "socks")
	links, _ := getInfoNodeRaw(t, c.subID)
	want := "socks://127.0.0.1:1080#%E2%9B%94%20exp%40test.com%20%7C%20%D9%BE%D8%A7%DB%8C%D8%A7%D9%86%20%231%20%2B%20%F0%9F%9A%80"
	if len(links) != 1 || links[0] != want {
		t.Fatalf("legacy socks links = %q, want [%q]", links, want)
	}
}

func TestSubJSON_InfoNodeLegacySocksUnchanged(t *testing.T) {
	c := infoNodeCases()[2]
	seedInfoNodeCase(t, c)
	setSubSetting(t, "subInfoNodeLinkType", "socks")
	jsonSvc := NewSubJsonService("", "", "", "", NewSubService(""))
	out, _, err := jsonSvc.GetJson(c.subID, "sub.example.com", true)
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var docs []struct {
		Outbounds []json.RawMessage `json:"outbounds"`
		Remarks   string            `json:"remarks"`
	}
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("GetJson output: %v\n%s", err, out)
	}
	if len(docs) != 1 || docs[0].Remarks != wantInfoRemark(c) || len(docs[0].Outbounds) == 0 {
		t.Fatalf("legacy socks docs = %s", out)
	}
	var got bytes.Buffer
	if err := json.Compact(&got, docs[0].Outbounds[0]); err != nil {
		t.Fatal(err)
	}
	want := `{"protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":1080}]},"tag":"proxy"}`
	if got.String() != want {
		t.Fatalf("legacy socks outbound = %s, want %s", got.String(), want)
	}
}
