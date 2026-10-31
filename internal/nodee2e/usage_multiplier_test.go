package nodee2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

type multInbound struct {
	Id              int      `json:"id"`
	Port            int      `json:"port"`
	NodeID          *int     `json:"nodeId"`
	UsageMultiplier *float64 `json:"usageMultiplier"`
}

type multStatus struct {
	Configured bool `json:"configured"`
	Active     bool `json:"active"`
	Nodes      []struct {
		NodeId     int   `json:"nodeId"`
		Known      bool  `json:"known"`
		Applies    bool  `json:"applies"`
		InboundIds []int `json:"inboundIds"`
	} `json:"nodes"`
}

func multiplierOnPort(t *testing.T, p *panel, port int) (float64, bool) {
	t.Helper()
	var list []multInbound
	if err := json.Unmarshal(p.call(http.MethodGet, "/panel/api/inbounds/list", nil), &list); err != nil {
		t.Fatal(err)
	}
	for _, ib := range list {
		if ib.Port == port {
			if ib.UsageMultiplier == nil {
				return 1, true
			}
			return *ib.UsageMultiplier, true
		}
	}
	return 0, false
}

func nodeApplies(t *testing.T, master *panel, nodeID int) (known, applies bool) {
	t.Helper()
	var st multStatus
	if err := json.Unmarshal(master.call(http.MethodPost, "/panel/api/setting/usageMultiplierStatus", nil), &st); err != nil {
		t.Fatal(err)
	}
	for _, n := range st.Nodes {
		if n.NodeId == nodeID {
			return n.Known, n.Applies
		}
	}
	return false, false
}

func enrollNode(t *testing.T, master, node *panel) int {
	t.Helper()
	var view struct {
		Id int `json:"id"`
	}
	obj := master.call(http.MethodPost, "/panel/api/nodes/add", map[string]any{
		"name": node.name, "scheme": "http", "address": "127.0.0.1", "port": node.port, "basePath": "/",
		"apiToken": node.mintToken("mult-link", "node-sync"), "enable": true, "allowPrivateAddress": true,
	})
	if err := json.Unmarshal(obj, &view); err != nil || view.Id == 0 {
		t.Fatalf("enroll %s: %v (%s)", node.name, err, obj)
	}
	return view.Id
}

// setUsageMultiplierSwitch round-trips the full settings object, as the UI does,
// then force-restarts Xray, the only point where the saved mode goes live.
func setUsageMultiplierSwitch(t *testing.T, p *panel, on bool) {
	t.Helper()
	var all map[string]any
	if err := json.Unmarshal(p.call(http.MethodPost, "/panel/api/setting/all", nil), &all); err != nil {
		t.Fatal(err)
	}
	all["usageMultiplierEnable"] = on
	p.call(http.MethodPost, "/panel/api/setting/update", all)
	// No Xray binary in the harness: the restart fails after the mode latches.
	_, _ = p.try(http.MethodPost, "/panel/api/server/restartXrayService", nil)
}

func TestNodeUsageMultiplier(t *testing.T) {
	bin := panelBinary(t)
	master := newPanel(t, bin, "mult-master")
	node := newPanel(t, bin, "mult-node")
	master.start()
	node.start()
	nodeID := enrollNode(t, master, node)

	port := freePort(t)
	body := vlessInbound("tunnel", port, &nodeID, vlessClient("mult-user"))
	body["usageMultiplier"] = 1.2
	master.call(http.MethodPost, "/panel/api/inbounds/add", body)

	eventually(t, settleTimeout, "node stores the pushed x1.2", func() (bool, string) {
		m, ok := multiplierOnPort(t, node, port)
		return ok && m == 1.2, fmt.Sprintf("node multiplier=%v found=%v", m, ok)
	})
	eventually(t, settleTimeout, "master flags the node as counting x1 while its switch is OFF", func() (bool, string) {
		known, applies := nodeApplies(t, master, nodeID)
		return known && !applies, fmt.Sprintf("known=%v applies=%v", known, applies)
	})

	setUsageMultiplierSwitch(t, node, true)
	eventually(t, settleTimeout, "master sees the node apply the multiplier", func() (bool, string) {
		known, applies := nodeApplies(t, master, nodeID)
		return known && applies, fmt.Sprintf("known=%v applies=%v", known, applies)
	})

	time.Sleep(12 * time.Second) // two sync ticks of snapshot adoption
	if m, _ := multiplierOnPort(t, master, port); m != 1.2 {
		t.Fatalf("master multiplier after sync ticks = %v, want 1.2", m)
	}
}

// A stock (pre-feature) node ignores the field: the master must keep its own
// x1.2 through snapshot adoption and flag the node instead of counting x1 silently.
func TestOldNodeKeepsMasterMultiplierAndIsFlagged(t *testing.T) {
	bin := panelBinary(t)
	oldBin := os.Getenv("XUI_NODE_E2E_OLD_BINARY")
	if oldBin == "" {
		t.Skip("XUI_NODE_E2E_OLD_BINARY not set: build a stock v3.11.0 panel to run this")
	}
	master := newPanel(t, bin, "mult-master-new")
	node := newPanel(t, oldBin, "mult-node-old")
	master.start()
	node.start()
	nodeID := enrollNode(t, master, node)

	port := freePort(t)
	body := vlessInbound("tunnel-old", port, &nodeID, vlessClient("old-user"))
	body["usageMultiplier"] = 1.2
	obj := master.call(http.MethodPost, "/panel/api/inbounds/add", body)
	var created multInbound
	_ = json.Unmarshal(obj, &created)

	pr := &pair{t: t, master: master, node: node, nodeID: nodeID}
	pr.waitNode("old node holds the inbound", port, func(ib inboundView) bool { return hasAll(ib.emails(), "old-user") })
	time.Sleep(12 * time.Second) // two sync ticks of snapshot adoption
	if m, _ := multiplierOnPort(t, master, port); m != 1.2 {
		t.Fatalf("old node's snapshot changed the master's x1.2 to x%v (inbound %s)", m, strconv.Itoa(created.Id))
	}
	eventually(t, settleTimeout, "master flags the old node", func() (bool, string) {
		known, applies := nodeApplies(t, master, nodeID)
		return known && !applies, fmt.Sprintf("known=%v applies=%v", known, applies)
	})
}
