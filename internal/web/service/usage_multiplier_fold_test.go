package service

import (
	"slices"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// The dashboard speed column, onlines and the external traffic inform must see
// the client's email with real bytes, never internal stat emails or billed bytes.
func TestFoldStatEmailTrafficUsesRealBytesPerEmail(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	a := mkMultInbound(t, 22001, model.VLESS, nil)
	b := mkMultInbound(t, 22002, model.VLESS, f64(2))
	attachClient(t, a, "alice@x", 0)
	attachClient(t, b, "alice@x", 0)

	in := []*xray.ClientTraffic{
		statDelta(a, "alice@x", 10, 20),
		statDelta(b, "alice@x", 1, 2),
		{Email: "plain@x", Up: 5, Down: 6},
		statDelta(b, "ghost@x", 0, 0),
	}
	got := map[string][2]int64{}
	for _, ct := range (&InboundService{}).FoldStatEmailTraffic(in) {
		got[ct.Email] = [2]int64{ct.Up, ct.Down}
	}
	want := map[string][2]int64{
		"alice@x":                            {11, 22},
		"plain@x":                            {5, 6},
		usagemult.StatEmail(b.Id, "ghost@x"): {0, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("folded = %v, want %v", got, want)
	}
	for e, w := range want {
		if got[e] != w {
			t.Fatalf("folded[%s] = %v, want %v (all: %v)", e, got[e], w, got)
		}
	}
}

// IP-limit counts a client's addresses across all its inbounds, so the two
// per-inbound identities must merge, keeping each IP's latest sighting.
func TestFoldStatEmailOnlineUsersMergesIPs(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	a := mkMultInbound(t, 22011, model.VLESS, nil)
	b := mkMultInbound(t, 22012, model.Trojan, f64(1.2))
	attachClient(t, a, "alice@x", 0)
	attachClient(t, b, "alice@x", 0)

	users := []xray.OnlineUser{
		{Email: usagemult.StatEmail(a.Id, "alice@x"), IPs: []xray.OnlineIP{{IP: "1.1.1.1", LastSeen: 100}}},
		{Email: usagemult.StatEmail(b.Id, "alice@x"), IPs: []xray.OnlineIP{{IP: "1.1.1.1", LastSeen: 200}, {IP: "2.2.2.2", LastSeen: 50}}},
		{Email: "bob@x", IPs: []xray.OnlineIP{{IP: "3.3.3.3", LastSeen: 7}}},
	}
	folded := foldStatEmailOnlineUsers(users)
	byEmail := map[string][]xray.OnlineIP{}
	for _, u := range folded {
		ips := slices.Clone(u.IPs)
		slices.SortFunc(ips, func(x, y xray.OnlineIP) int {
			if x.IP < y.IP {
				return -1
			}
			return 1
		})
		byEmail[u.Email] = ips
	}
	if len(byEmail) != 2 {
		t.Fatalf("folded users = %+v, want alice and bob only", folded)
	}
	if want := []xray.OnlineIP{{IP: "1.1.1.1", LastSeen: 200}, {IP: "2.2.2.2", LastSeen: 50}}; !slices.Equal(byEmail["alice@x"], want) {
		t.Fatalf("alice IPs = %+v, want %+v", byEmail["alice@x"], want)
	}
	if want := []xray.OnlineIP{{IP: "3.3.3.3", LastSeen: 7}}; !slices.Equal(byEmail["bob@x"], want) {
		t.Fatalf("bob IPs = %+v", byEmail["bob@x"])
	}
}
