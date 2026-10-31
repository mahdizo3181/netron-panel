package service

import (
	"maps"
	"slices"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// UsageMultiplierNodeStatus is one node hosting inbounds whose multiplier is not 1.
type UsageMultiplierNodeStatus struct {
	NodeId int    `json:"nodeId" example:"2"`
	Name   string `json:"name" example:"de-1"`
	// Known is false until the node has answered this panel once.
	Known      bool  `json:"known" example:"true"`
	Applies    bool  `json:"applies" example:"false"`
	InboundIds []int `json:"inboundIds" example:"[5,7]"`
}

// UsageMultiplierStatus tells the UI whether the saved kill switch is live and
// which nodes would count a weighted inbound at x1.
type UsageMultiplierStatus struct {
	Configured bool                        `json:"configured" example:"true"`
	Active     bool                        `json:"active" example:"false"`
	Nodes      []UsageMultiplierNodeStatus `json:"nodes"`
}

// GetUsageMultiplierStatus reports the saved and running mode plus every node
// with a weighted inbound and whether that node applies the multiplier.
func (s *InboundService) GetUsageMultiplierStatus() (*UsageMultiplierStatus, error) {
	configured, err := (&SettingService{}).GetUsageMultiplierEnable()
	if err != nil {
		return nil, err
	}
	st := &UsageMultiplierStatus{Configured: configured, Active: usagemult.Active(), Nodes: []UsageMultiplierNodeStatus{}}
	byNode, err := weightedNodeInbounds(0)
	if err != nil || len(byNode) == 0 {
		return st, err
	}
	var nodes []*model.Node
	if err := database.GetDB().Where("id IN ?", slices.Collect(maps.Keys(byNode))).Order("id").Find(&nodes).Error; err != nil {
		return nil, err
	}
	mgr := runtime.GetManager()
	for _, n := range nodes {
		ns := UsageMultiplierNodeStatus{NodeId: n.Id, Name: n.Name, InboundIds: byNode[n.Id]}
		if mgr != nil {
			if rt, rerr := mgr.RemoteFor(n); rerr == nil {
				ns.Known, ns.Applies = rt.UsageMultiplierApplied()
			}
		}
		st.Nodes = append(st.Nodes, ns)
	}
	return st, nil
}

// weightedNodeInbounds maps node id to its inbounds whose multiplier is not 1;
// nodeID 0 means every node.
func weightedNodeInbounds(nodeID int) (map[int][]int, error) {
	q := database.GetDB().Model(&model.Inbound{}).Select("id", "node_id", "usage_multiplier").
		Where("node_id IS NOT NULL AND usage_multiplier IS NOT NULL AND usage_multiplier <> 1")
	if nodeID > 0 {
		q = q.Where("node_id = ?", nodeID)
	}
	var rows []model.Inbound
	if err := q.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int][]int{}
	for _, r := range rows {
		if m, _ := r.EffectiveUsageMultiplier(); m != 1 {
			out[*r.NodeID] = append(out[*r.NodeID], r.Id)
		}
	}
	return out, nil
}

var usageMultNodeWarned sync.Map

// WarnUsageMultiplierNode logs once per state change when a node would count its
// weighted inbounds at x1: an old build, or the mode is off on that node.
func (s *InboundService) WarnUsageMultiplierNode(n *model.Node, rt *runtime.Remote) {
	known, applies := rt.UsageMultiplierApplied()
	if !known {
		return
	}
	byNode, err := weightedNodeInbounds(n.Id)
	if err != nil {
		logger.Warning("usage multiplier: load weighted inbounds for node", n.Name, "failed:", err)
		return
	}
	failing := !applies && len(byNode[n.Id]) > 0
	prev, _ := usageMultNodeWarned.Swap(n.Id, failing)
	if failing && prev != true {
		logger.Warningf("usage multiplier: node %s counts inbounds %v at x1 (old panel version or per-inbound multiplier disabled on the node)", n.Name, byNode[n.Id])
	} else if !failing && prev == true {
		logger.Infof("usage multiplier: node %s now applies the per-inbound multiplier", n.Name)
	}
}
