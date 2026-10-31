// Package usagemult derives the per-(inbound, client) Xray stats identity behind
// the per-inbound usage multiplier and holds the running mode latch. Leaf package.
package usagemult

import (
	"crypto/sha256"
	"encoding/base32"
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
