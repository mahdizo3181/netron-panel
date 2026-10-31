package service

import (
	"encoding/json"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
)

// expandRoutingUserStatEmails adds each client's stat emails next to its email in
// template rules' "user" lists, so they keep matching; "regexp:" entries cannot be.
func (s *XrayService) expandRoutingUserStatEmails(router json_util.RawMessage, inbounds []*model.Inbound) json_util.RawMessage {
	if !usagemult.Active() || len(router) == 0 {
		return router
	}
	var cfg map[string]any
	if err := json.Unmarshal(router, &cfg); err != nil {
		return router
	}
	rules, _ := cfg["rules"].([]any)
	if !rulesMatchUsers(rules) {
		return router
	}
	statByEmail := map[string][]string{}
	for _, ib := range inbounds {
		if !ib.Enable || ib.NodeID != nil || !usagemult.SupportsProtocol(string(ib.Protocol)) {
			continue
		}
		clients, err := s.inboundService.clientService.ListForInbound(nil, ib.Id)
		if err != nil {
			logger.Warning("usage multiplier: list clients for routing users failed:", err)
			return router
		}
		for _, c := range clients {
			key := strings.ToLower(c.Email)
			statByEmail[key] = append(statByEmail[key], usagemult.StatEmail(ib.Id, c.Email))
		}
	}
	for _, r := range rules {
		rule, _ := r.(map[string]any)
		users, _ := rule["user"].([]any)
		for _, u := range users {
			email, _ := u.(string)
			for _, stat := range statByEmail[strings.ToLower(email)] {
				users = append(users, stat)
			}
		}
		if rule != nil && len(users) > 0 {
			rule["user"] = users
		}
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return router
	}
	return out
}

func rulesMatchUsers(rules []any) bool {
	for _, r := range rules {
		if rule, ok := r.(map[string]any); ok {
			if users, ok := rule["user"].([]any); ok && len(users) > 0 {
				return true
			}
		}
	}
	return false
}
