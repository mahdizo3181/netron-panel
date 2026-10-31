package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

const gb = int64(1_000_000_000)

func f64(v float64) *float64 { return &v }

// attachClient creates (or reuses) a client record on ib with its client_traffics row.
func attachClient(t *testing.T, ib *model.Inbound, email string, totalBytes int64) {
	t.Helper()
	c := model.Client{Email: email, ID: uuid.NewString(), Enable: true, TotalGB: totalBytes}
	existing, err := (&ClientService{}).ListForInbound(nil, ib.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&ClientService{}).SyncInbound(nil, ib.Id, append(existing, c)); err != nil {
		t.Fatal(err)
	}
	if err := (&InboundService{}).AddClientStat(database.GetDB(), ib.Id, &c); err != nil {
		t.Fatal(err)
	}
}

func mkMultInbound(t *testing.T, port int, proto model.Protocol, m *float64) *model.Inbound {
	t.Helper()
	ib := mkInbound(t, port, proto, `{"clients":[]}`)
	if m != nil {
		if err := database.GetDB().Model(ib).Update("usage_multiplier", *m).Error; err != nil {
			t.Fatal(err)
		}
		ib.UsageMultiplier = m
	}
	return ib
}

func clientRow(t *testing.T, email string) xray.ClientTraffic {
	t.Helper()
	var ct xray.ClientTraffic
	if err := database.GetDB().Where("email = ?", email).First(&ct).Error; err != nil {
		t.Fatalf("load client_traffics %s: %v", email, err)
	}
	return ct
}

func inboundRow(t *testing.T, inboundID int, email string) model.ClientInboundTraffic {
	t.Helper()
	var r model.ClientInboundTraffic
	if err := database.GetDB().Where("stat_email = ?", usagemult.StatEmail(inboundID, email)).First(&r).Error; err != nil {
		t.Fatalf("load client_inbound_traffics %d/%s: %v", inboundID, email, err)
	}
	return r
}

func addTraffic(t *testing.T, cts ...*xray.ClientTraffic) {
	t.Helper()
	if _, _, err := (&InboundService{}).AddTraffic(nil, cts); err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}
}

func statDelta(ib *model.Inbound, email string, up, down int64) *xray.ClientTraffic {
	return &xray.ClientTraffic{Email: usagemult.StatEmail(ib.Id, email), Up: up, Down: down}
}

// Acceptance: 10 GB on a x1 inbound + 5 GB on a x1.2 inbound bills 16 GB to
// the one shared quota, and the client is cut exactly when the weighted total hits it.
func TestUsageMultiplierBillsWeightedTotalAndCutsAtQuota(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	direct := mkMultInbound(t, 21001, model.VLESS, nil)
	tunnel := mkMultInbound(t, 21002, model.VLESS, f64(1.2))
	attachClient(t, direct, "alice@x", 16*gb)
	attachClient(t, tunnel, "alice@x", 16*gb)

	addTraffic(t, statDelta(direct, "alice@x", 4*gb, 6*gb), statDelta(tunnel, "alice@x", 2*gb, 2*gb))
	if ct := clientRow(t, "alice@x"); ct.Up+ct.Down != 10*gb+4*gb*12/10 || !ct.Enable {
		t.Fatalf("after 10 GB x1 + 4 GB x1.2: used=%d enable=%v, want %d and still enabled", ct.Up+ct.Down, ct.Enable, 10*gb+4*gb*12/10)
	}

	addTraffic(t, statDelta(tunnel, "alice@x", 0, gb))
	ct := clientRow(t, "alice@x")
	if ct.Up+ct.Down != 16*gb {
		t.Fatalf("weighted total = %d, want %d (16 GB)", ct.Up+ct.Down, 16*gb)
	}
	if ct.Enable {
		t.Fatal("client still enabled at 16 GB of a 16 GB quota")
	}

	d, tu := inboundRow(t, direct.Id, "alice@x"), inboundRow(t, tunnel.Id, "alice@x")
	if d.ActualUp != 4*gb || d.ActualDown != 6*gb || d.BillableUp != 4*gb || d.BillableDown != 6*gb {
		t.Fatalf("direct row = %+v", d)
	}
	if tu.ActualUp != 2*gb || tu.ActualDown != 3*gb || tu.BillableUp+tu.BillableDown != 6*gb {
		t.Fatalf("tunnel row = %+v, want actual 2/3 GB and 6 GB billed", tu)
	}
	if tu.Email != "alice@x" || tu.InboundId != tunnel.Id || tu.ClientId == 0 {
		t.Fatalf("tunnel row identity = %+v", tu)
	}
}

func TestUsageMultiplierRounding(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	ib := mkMultInbound(t, 21011, model.Trojan, f64(1.2))
	half := mkMultInbound(t, 21012, model.Trojan, f64(0.5))
	attachClient(t, ib, "r@x", 0)
	attachClient(t, half, "r@x", 0)

	// round(1*1.2)=1, round(3*1.2)=round(3.6)=4, round(1*0.5)=1 (half away from zero), round(3*0.5)=2.
	addTraffic(t, statDelta(ib, "r@x", 1, 3), statDelta(half, "r@x", 1, 3))
	if ct := clientRow(t, "r@x"); ct.Up != 2 || ct.Down != 6 {
		t.Fatalf("rounded billable = %d/%d, want 2/6", ct.Up, ct.Down)
	}
}

func TestUsageMultiplierZeroIsFreeButStaysOnline(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	free := mkMultInbound(t, 21021, model.VLESS, f64(0))
	attachClient(t, free, "f@x", gb)

	addTraffic(t, statDelta(free, "f@x", 5*gb, 5*gb))
	ct := clientRow(t, "f@x")
	if ct.Up != 0 || ct.Down != 0 || !ct.Enable {
		t.Fatalf("free inbound billed %d/%d enable=%v", ct.Up, ct.Down, ct.Enable)
	}
	if ct.LastOnline == 0 {
		t.Fatal("traffic on a free inbound did not bump last_online")
	}
	if r := inboundRow(t, free.Id, "f@x"); r.ActualUp != 5*gb || r.BillableUp != 0 {
		t.Fatalf("free row = %+v, want real bytes kept and nothing billed", r)
	}
}

// Plain emails (pre-switch counters, AmneziaWG/TUIC relays) and stat-shaped
// strings that match no attached client keep the stock x1 path.
func TestUsageMultiplierLegacyEmailsPassThrough(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	ib := mkMultInbound(t, 21031, model.VLESS, f64(2))
	attachClient(t, ib, "plain@x", 0)
	lookalike := usagemult.StatEmail(ib.Id, "nobody@x")
	if err := database.GetDB().Create(&xray.ClientTraffic{Email: lookalike, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}

	addTraffic(t, &xray.ClientTraffic{Email: "plain@x", Up: 100, Down: 200}, &xray.ClientTraffic{Email: lookalike, Up: 7, Down: 9})
	if ct := clientRow(t, "plain@x"); ct.Up != 100 || ct.Down != 200 {
		t.Fatalf("plain email traffic = %d/%d, want x1 100/200", ct.Up, ct.Down)
	}
	if ct := clientRow(t, lookalike); ct.Up != 7 || ct.Down != 9 {
		t.Fatalf("unmatched stat-shaped email = %d/%d, want untouched 7/9", ct.Up, ct.Down)
	}
	var n int64
	database.GetDB().Model(&model.ClientInboundTraffic{}).Count(&n)
	if n != 0 {
		t.Fatalf("legacy traffic created %d per-inbound rows", n)
	}
}

func TestUsageMultiplierCorruptValueNeverFree(t *testing.T) {
	initUsageMultTestDB(t)
	usagemult.SetActive(true)
	ib := mkMultInbound(t, 21041, model.VLESS, f64(-3))
	attachClient(t, ib, "c@x", 0)
	addTraffic(t, statDelta(ib, "c@x", 10, 20))
	if ct := clientRow(t, "c@x"); ct.Up != 10 || ct.Down != 20 {
		t.Fatalf("corrupt multiplier billed %d/%d, want x1 10/20", ct.Up, ct.Down)
	}
}

type quotaOutcome struct {
	up, down int64
	enable   bool
}

// runInvariantScenario feeds the same real bytes either as Xray reports them
// with the mode OFF (plain emails) or ON (stat emails per inbound).
func runInvariantScenario(t *testing.T, on bool) map[string]quotaOutcome {
	t.Helper()
	initUsageMultTestDB(t)
	usagemult.SetActive(on)
	a := mkMultInbound(t, 21051, model.VLESS, nil)
	b := mkMultInbound(t, 21052, model.Trojan, f64(1))
	for _, ib := range []*model.Inbound{a, b} {
		attachClient(t, ib, "u1@x", 3000)
		attachClient(t, ib, "u2@x", 0)
	}
	attachClient(t, a, "solo@x", 500)

	ticks := [][]struct {
		ib       *model.Inbound
		email    string
		up, down int64
	}{
		{{a, "u1@x", 400, 600}, {b, "u1@x", 100, 200}, {a, "solo@x", 100, 100}, {b, "u2@x", 5, 5}},
		{{a, "u1@x", 300, 300}, {b, "u1@x", 600, 600}, {a, "solo@x", 200, 200}},
		{{b, "u2@x", 1 << 40, 1 << 40}},
	}
	for _, tick := range ticks {
		var batch []*xray.ClientTraffic
		plain := map[string]*xray.ClientTraffic{}
		for _, d := range tick {
			if on {
				batch = append(batch, statDelta(d.ib, d.email, d.up, d.down))
				continue
			}
			if plain[d.email] == nil {
				plain[d.email] = &xray.ClientTraffic{Email: d.email}
				batch = append(batch, plain[d.email])
			}
			plain[d.email].Up += d.up
			plain[d.email].Down += d.down
		}
		addTraffic(t, batch...)
	}
	out := map[string]quotaOutcome{}
	for _, e := range []string{"u1@x", "u2@x", "solo@x"} {
		ct := clientRow(t, e)
		out[e] = quotaOutcome{ct.Up, ct.Down, ct.Enable}
	}
	return out
}

func TestUsageMultiplierOnWithAllX1MatchesOff(t *testing.T) {
	off := runInvariantScenario(t, false)
	on := runInvariantScenario(t, true)
	for email, want := range off {
		if on[email] != want {
			t.Errorf("%s: ON+x1 = %+v, OFF = %+v", email, on[email], want)
		}
	}
	if off["u1@x"].enable || off["solo@x"].enable || !off["u2@x"].enable {
		t.Fatalf("scenario lost its quota crossings, invariant proves nothing: %+v", off)
	}
}
