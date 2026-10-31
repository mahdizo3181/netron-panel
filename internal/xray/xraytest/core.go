// Package xraytest runs a real in-process Xray core for tests that must see what
// the core actually accepted. Import it from _test.go files only.
package xraytest

import (
	"context"
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	_ "github.com/xtls/xray-core/app/commander"
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/dokodemo"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/trojan"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
)

// FreePort returns a loopback TCP port that was free a moment ago.
func FreePort(t testing.TB) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// StartCore runs a core with the handler API (inbound tag "api") plus inbounds,
// and returns the API port.
func StartCore(t testing.TB, inbounds ...map[string]any) int {
	t.Helper()
	apiPort := FreePort(t)
	all := []any{map[string]any{"tag": "api", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}}
	for _, ib := range inbounds {
		all = append(all, ib)
	}
	raw, _ := json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"api":       map[string]any{"tag": "api", "services": []string{"HandlerService"}},
		"inbounds":  all,
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
		"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}},
	})
	var c conf.Config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	built, err := c.Build()
	if err != nil {
		t.Fatal(err)
	}
	x, err := core.New(built)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return apiPort
}

// VlessInbound is a loopback vless inbound with the given client emails.
func VlessInbound(t testing.TB, tag string, emails ...string) map[string]any {
	t.Helper()
	clients := []any{}
	for i, e := range emails {
		clients = append(clients, map[string]any{"email": e, "id": "00000000-0000-0000-0000-" + strconv.Itoa(100000000000+i)})
	}
	return map[string]any{"tag": tag, "listen": "127.0.0.1", "port": FreePort(t), "protocol": "vless", "settings": map[string]any{"clients": clients, "decryption": "none"}}
}

// InboundUsers lists the emails the core holds on an inbound, sorted.
func InboundUsers(t testing.TB, apiPort int, tag string) []string {
	t.Helper()
	conn, err := grpc.NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := command.NewHandlerServiceClient(conn).GetInboundUsers(ctx, &command.GetInboundUserRequest{Tag: tag})
	if err != nil {
		t.Fatalf("GetInboundUsers(%s): %v", tag, err)
	}
	var emails []string
	for _, u := range resp.GetUsers() {
		emails = append(emails, u.GetEmail())
	}
	slices.Sort(emails)
	return emails
}
