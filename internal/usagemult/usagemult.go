// Package usagemult derives the per-(inbound, client) Xray stats identity behind
// the per-inbound usage multiplier and holds the running mode latch. Leaf package.
package usagemult

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	statEmailPrefix = "nxm_"
	hashLen         = 16
)

var hashEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// StatEmail is the email Xray sees for email on inboundID while the mode is active.
// Lower-cased base32 keeps it stable whatever case the core applies to emails.
func StatEmail(inboundID int, email string) string {
	return statEmailPrefix + strconv.Itoa(inboundID) + "_" + hashPart(inboundID, email)
}

func hashPart(inboundID int, email string) string {
	sum := sha256.Sum256([]byte(strconv.Itoa(inboundID) + ":" + strings.ToLower(email)))
	return strings.ToLower(hashEncoding.EncodeToString(sum[:]))[:hashLen]
}

// ParseStatEmail returns the inbound id a stat-email-shaped string claims; ok
// alone does not prove it is one — confirm with Matches against a real client.
func ParseStatEmail(s string) (inboundID int, ok bool) {
	rest, found := strings.CutPrefix(s, statEmailPrefix)
	if !found {
		return 0, false
	}
	idPart, hash, found := strings.Cut(rest, "_")
	if !found || len(hash) != hashLen || !isLowerBase32(hash) {
		return 0, false
	}
	id, err := strconv.Atoi(idPart)
	if err != nil || id <= 0 || strconv.Itoa(id) != idPart {
		return 0, false
	}
	return id, true
}

// Matches reports whether s is the stat email of email on inboundID.
func Matches(s string, inboundID int, email string) bool {
	return s == StatEmail(inboundID, email)
}

func isLowerBase32(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return false
		}
	}
	return true
}

// SupportsProtocol reports whether an inbound's clients reach Xray as regular
// users with their own email, so a per-inbound stats identity can be given.
func SupportsProtocol(protocol string) bool {
	switch protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria", "masque":
		return true
	}
	return false
}

var active atomic.Bool

// Active is the mode the running Xray was started with; it only changes on a
// forced restart so a hot apply can never swap emails under a live core.
func Active() bool { return active.Load() }

// SetActive latches the mode for the Xray process about to start.
func SetActive(on bool) { active.Store(on) }

// XrayEmail is the email to hand Xray for email on an inbound of protocol.
func XrayEmail(inboundID int, protocol, email string) string {
	if email == "" || !Active() || !SupportsProtocol(protocol) {
		return email
	}
	return StatEmail(inboundID, email)
}

// RewriteSettingsEmails swaps every settings.clients[].email for its XrayEmail;
// input it cannot parse is returned unchanged for Xray to judge.
func RewriteSettingsEmails(inboundID int, protocol, settings string) string {
	if !Active() || !SupportsProtocol(protocol) {
		return settings
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		return settings
	}
	clients, ok := parsed["clients"].([]any)
	if !ok {
		return settings
	}
	for _, c := range clients {
		if client, ok := c.(map[string]any); ok {
			if email, ok := client["email"].(string); ok {
				client["email"] = XrayEmail(inboundID, protocol, email)
			}
		}
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return settings
	}
	return string(out)
}

// NodeCap is advertised in the node caps header while this panel's running core
// bills per inbound; an old build or the OFF mode leaves it out.
const NodeCap = "usage-mult"

// NodeCaps appends NodeCap to a node's base caps header value when active.
func NodeCaps(base string) string {
	if !Active() {
		return base
	}
	return base + "," + NodeCap
}
