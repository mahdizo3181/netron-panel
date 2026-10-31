package runtime

import (
	"context"
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
	_ "github.com/xtls/xray-core/proxy/vless/inbound"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startCore runs a real in-process Xray with the handler API and one empty vless inbound.
func startCore(t *testing.T, vlessTag string) (apiPort int) {
	t.Helper()
	apiPort = freePort(t)
	raw := map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"api": map[string]any{"tag": "api", "services": []string{"HandlerService"}},
		"inbounds": []any{
			map[string]any{"tag": "api", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}},
			map[string]any{"tag": vlessTag, "listen": "127.0.0.1", "port": freePort(t), "protocol": "vless", "settings": map[string]any{"clients": []any{}, "decryption": "none"}},
		},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
		"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}},
	}
	b, _ := json.Marshal(raw)
	var c conf.Config
	if err := json.Unmarshal(b, &c); err != nil {
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

func coreUsers(t *testing.T, apiPort int, tag string) []string {
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

// Hot user ops must reach the core under the per-inbound stat email while the
// mode is active, and under the plain email otherwise (stock behaviour).
func TestLocalUserOpsUseStatEmail(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[active], func(t *testing.T) {
			usagemult.SetActive(active)
			t.Cleanup(func() { usagemult.SetActive(false) })
			ib := &model.Inbound{Id: 17, Tag: "in-vless", Protocol: model.VLESS, Enable: true}
			port := startCore(t, ib.Tag)
			local := NewLocal(LocalDeps{APIPort: func() int { return port }})

			want := "alice@x"
			if active {
				want = usagemult.StatEmail(ib.Id, "alice@x")
			}
			client := model.Client{Email: "alice@x", ID: uuid.NewString(), Enable: true}
			if err := local.AddClient(context.Background(), ib, client); err != nil {
				t.Fatalf("AddClient: %v", err)
			}
			if got := coreUsers(t, port, ib.Tag); !slices.Equal(got, []string{want}) {
				t.Fatalf("core users after AddClient = %v, want [%s]", got, want)
			}
			if err := local.UpdateUser(context.Background(), ib, "alice@x", client); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
			if got := coreUsers(t, port, ib.Tag); !slices.Equal(got, []string{want}) {
				t.Fatalf("core users after UpdateUser = %v, want exactly [%s] (old identity not removed?)", got, want)
			}
			if err := local.RemoveUser(context.Background(), ib, "alice@x"); err != nil {
				t.Fatalf("RemoveUser by logical email: %v", err)
			}
			if got := coreUsers(t, port, ib.Tag); len(got) != 0 {
				t.Fatalf("core users after RemoveUser = %v, want none", got)
			}
		})
	}
}

// A hot-added inbound carries its clients in raw settings; they must get the stat email too.
func TestLocalAddInboundUsesStatEmail(t *testing.T) {
	usagemult.SetActive(true)
	t.Cleanup(func() { usagemult.SetActive(false) })
	port := startCore(t, "placeholder")
	local := NewLocal(LocalDeps{APIPort: func() int { return port }})
	ib := &model.Inbound{
		Id: 23, Tag: "in-hot", Protocol: model.VLESS, Enable: true, Listen: "127.0.0.1", Port: freePort(t),
		Settings: `{"clients":[{"email":"bob@x","id":"` + uuid.NewString() + `"}],"decryption":"none"}`,
	}
	if err := local.AddInbound(context.Background(), ib); err != nil {
		t.Fatalf("AddInbound: %v", err)
	}
	want := []string{usagemult.StatEmail(23, "bob@x")}
	if got := coreUsers(t, port, ib.Tag); !slices.Equal(got, want) {
		t.Fatalf("hot-added inbound users = %v, want %v", got, want)
	}
	if !json.Valid([]byte(ib.Settings)) || !strings.Contains(ib.Settings, `"bob@x"`) {
		t.Fatalf("AddInbound mutated the caller's stored settings: %s", ib.Settings)
	}
}
