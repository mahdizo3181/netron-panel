package service

import (
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// FoldStatEmailTraffic maps Xray's per-inbound stat emails back to client emails
// with real bytes, for consumers that display or forward traffic but never bill it.
func (s *InboundService) FoldStatEmailTraffic(traffics []*xray.ClientTraffic) []*xray.ClientTraffic {
	emails := make([]string, 0, len(traffics))
	for _, tr := range traffics {
		if tr != nil {
			emails = append(emails, tr.Email)
		}
	}
	owners, err := resolveStatEmailOwners(database.GetDB(), emails)
	if err != nil {
		logger.Warning("usage multiplier: resolve stat emails for display failed:", err)
		return traffics
	}
	if len(owners) == 0 {
		return traffics
	}
	out := make([]*xray.ClientTraffic, 0, len(traffics))
	byEmail := make(map[string]*xray.ClientTraffic, len(traffics))
	for _, tr := range traffics {
		if tr == nil {
			continue
		}
		email := tr.Email
		if owner, ok := owners[email]; ok {
			email = owner.email
		}
		if merged := byEmail[email]; merged != nil {
			merged.Up += tr.Up
			merged.Down += tr.Down
			continue
		}
		cp := *tr
		cp.Email = email
		byEmail[email] = &cp
		out = append(out, &cp)
	}
	return out
}

// foldStatEmailOnlineUsers merges a client's per-inbound online entries into one,
// keeping each source IP's most recent sighting.
func foldStatEmailOnlineUsers(users []xray.OnlineUser) []xray.OnlineUser {
	emails := make([]string, 0, len(users))
	for _, u := range users {
		emails = append(emails, u.Email)
	}
	owners, err := resolveStatEmailOwners(database.GetDB(), emails)
	if err != nil {
		logger.Warning("usage multiplier: resolve stat emails for online users failed:", err)
		return users
	}
	if len(owners) == 0 {
		return users
	}
	out := make([]xray.OnlineUser, 0, len(users))
	index := make(map[string]int, len(users))
	for _, u := range users {
		if owner, ok := owners[u.Email]; ok {
			u.Email = owner.email
		}
		i, seen := index[u.Email]
		if !seen {
			index[u.Email] = len(out)
			out = append(out, xray.OnlineUser{Email: u.Email})
			i = len(out) - 1
		}
		out[i].IPs = mergeOnlineIPs(out[i].IPs, u.IPs)
	}
	return out
}

func mergeOnlineIPs(into, from []xray.OnlineIP) []xray.OnlineIP {
	for _, ip := range from {
		found := false
		for k := range into {
			if into[k].IP == ip.IP {
				into[k].LastSeen = max(into[k].LastSeen, ip.LastSeen)
				found = true
				break
			}
		}
		if !found {
			into = append(into, ip)
		}
	}
	return into
}
