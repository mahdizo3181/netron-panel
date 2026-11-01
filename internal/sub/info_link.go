package sub

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// The info config only displays its remark: loopback port 1 with the nil UUID can never carry traffic.
const (
	infoNodeAddress = "127.0.0.1"
	infoNodePort    = 1
	infoNodeUUID    = "00000000-0000-0000-0000-000000000000"
)

// infoNodeLink is the raw-subscription info entry; spaces stay %20 because some clients keep a literal +.
func infoNodeLink(linkType, remark string) string {
	fragment := strings.ReplaceAll(url.QueryEscape(remark), "+", "%20")
	if linkType == service.SubInfoNodeLinkSocks {
		return "socks://127.0.0.1:1080#" + fragment
	}
	hostPort := net.JoinHostPort(infoNodeAddress, strconv.Itoa(infoNodePort))
	return "vless://" + infoNodeUUID + "@" + hostPort + "?encryption=none&security=none&type=tcp#" + fragment
}

// infoNodeOutbound is the JSON-subscription info outbound, in the flat VLESS form genVless emits.
func infoNodeOutbound(linkType string) map[string]any {
	if linkType == service.SubInfoNodeLinkSocks {
		return map[string]any{
			"protocol": "socks",
			"tag":      "proxy",
			"settings": map[string]any{
				"servers": []any{map[string]any{"address": "127.0.0.1", "port": 1080}},
			},
		}
	}
	return map[string]any{
		"protocol": "vless",
		"tag":      "proxy",
		"settings": map[string]any{
			"address":    infoNodeAddress,
			"port":       infoNodePort,
			"id":         infoNodeUUID,
			"encryption": "none",
		},
		"streamSettings": map[string]any{"network": "tcp", "security": "none"},
	}
}
