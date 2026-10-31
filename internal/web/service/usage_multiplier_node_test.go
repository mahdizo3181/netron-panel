package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func seedMultNode(t *testing.T, id int) {
	t.Helper()
	if err := database.GetDB().Create(&model.Node{Id: id, Name: "mult-node", Address: "10.0.0.9", Port: 2053, ApiToken: "t", Guid: "mult-node-guid"}).Error; err != nil {
		t.Fatal(err)
	}
}

func centralMultiplier(t *testing.T, tag string) float64 {
	t.Helper()
	var ib model.Inbound
	if err := database.GetDB().Where("tag = ?", tag).First(&ib).Error; err != nil {
		t.Fatalf("central inbound %s: %v", tag, err)
	}
	m, _ := ib.EffectiveUsageMultiplier()
	return m
}

func snapInbound(tag string, port int, m *float64) *model.Inbound {
	return &model.Inbound{Id: port, Tag: tag, Enable: true, Port: port, Protocol: model.VLESS, Settings: `{"clients":[]}`, UsageMultiplier: m}
}

// The master is authoritative for the multiplier it pushed; an old node's
// snapshot has no field and must neither zero it (free) nor reset it to 1.
func TestNodeSnapshotAdoptsMultiplierOnlyWhenPresentAndValid(t *testing.T) {
	initUsageMultTestDB(t)
	const nodeID = 9
	seedMultNode(t, nodeID)
	svc := &InboundService{}
	sync := func(ibs ...*model.Inbound) {
		t.Helper()
		if _, err := svc.setRemoteTrafficLocked(nodeID, &runtime.TrafficSnapshot{Inbounds: ibs}, false, false); err != nil {
			t.Fatal(err)
		}
	}

	sync(snapInbound("node-new-old-build", 47001, nil), snapInbound("node-new-x2", 47002, f64(2)), snapInbound("node-new-bad", 47003, f64(42)))
	if got := centralMultiplier(t, "node-new-old-build"); got != 1 {
		t.Fatalf("inbound adopted from an old node = x%v, want x1", got)
	}
	if got := centralMultiplier(t, "node-new-x2"); got != 2 {
		t.Fatalf("inbound adopted from a new node = x%v, want x2", got)
	}
	if got := centralMultiplier(t, "node-new-bad"); got != 1 {
		t.Fatalf("invalid snapshot multiplier adopted as x%v", got)
	}

	if err := database.GetDB().Model(&model.Inbound{}).Where("tag = ?", "node-new-old-build").Update("usage_multiplier", 1.2).Error; err != nil {
		t.Fatal(err)
	}
	sync(snapInbound("node-new-old-build", 47001, nil))
	if got := centralMultiplier(t, "node-new-old-build"); got != 1.2 {
		t.Fatalf("old node snapshot without the field changed the master's x1.2 to x%v", got)
	}
	sync(snapInbound("node-new-old-build", 47001, f64(0)))
	if got := centralMultiplier(t, "node-new-old-build"); got != 0 {
		t.Fatalf("node-side edit to free not adopted: x%v", got)
	}
	sync(snapInbound("node-new-old-build", 47001, f64(-5)))
	if got := centralMultiplier(t, "node-new-old-build"); got != 0 {
		t.Fatalf("invalid snapshot value overwrote the stored one: x%v", got)
	}
}

// The node bills with its own multiplier; the master must take the node's
// weighted total as-is (no second x1.2) and once, though two node inbounds report it.
func TestNodeWeightedTotalsMergeOnceWithoutReweighting(t *testing.T) {
	db := initTrafficTestDB(t)
	createNodeInbound(t, db, 1, "n1-direct", 41101)
	createNodeInbound(t, db, 1, "n1-tunnel", 41102)
	if err := db.Model(&model.Inbound{}).Where("tag = ?", "n1-tunnel").Update("usage_multiplier", 1.2).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	sync := func(up, down int64) {
		t.Helper()
		row := xray.ClientTraffic{Email: "node-mult@x", Up: up, Down: down, Enable: true}
		snap := &runtime.TrafficSnapshot{Inbounds: []*model.Inbound{
			{Tag: "n1-direct", ClientStats: []xray.ClientTraffic{row}},
			{Tag: "n1-tunnel", ClientStats: []xray.ClientTraffic{row}},
		}}
		if _, err := svc.setRemoteTrafficLocked(1, snap, false, false); err != nil {
			t.Fatal(err)
		}
	}
	sync(0, 0)
	// Node billed 10 GB on direct x1 + 5 GB on tunnel x1.2 = 16 GB into its one row.
	sync(7*gb, 9*gb)
	if ct := readTraffic(t, db, "node-mult@x"); ct.Up != 7*gb || ct.Down != 9*gb {
		t.Fatalf("master total = %d/%d, want the node's 7/9 GB once and unweighted", ct.Up, ct.Down)
	}
}
