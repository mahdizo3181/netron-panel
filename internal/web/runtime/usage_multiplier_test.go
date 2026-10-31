package runtime

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
	"github.com/mhsanaei/3x-ui/v3/internal/xray/xraytest"
)

// Hot user ops must reach the core under the per-inbound stat email while the
// mode is active, and under the plain email otherwise (stock behaviour).
func TestLocalUserOpsUseStatEmail(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[active], func(t *testing.T) {
			usagemult.SetActive(active)
			t.Cleanup(func() { usagemult.SetActive(false) })
			ib := &model.Inbound{Id: 17, Tag: "in-vless", Protocol: model.VLESS, Enable: true}
			port := xraytest.StartCore(t, xraytest.VlessInbound(t, ib.Tag))
			local := NewLocal(LocalDeps{APIPort: func() int { return port }})

			want := "alice@x"
			if active {
				want = usagemult.StatEmail(ib.Id, "alice@x")
			}
			client := model.Client{Email: "alice@x", ID: uuid.NewString(), Enable: true}
			if err := local.AddClient(context.Background(), ib, client); err != nil {
				t.Fatalf("AddClient: %v", err)
			}
			if got := xraytest.InboundUsers(t, port, ib.Tag); !slices.Equal(got, []string{want}) {
				t.Fatalf("core users after AddClient = %v, want [%s]", got, want)
			}
			if err := local.UpdateUser(context.Background(), ib, "alice@x", client); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
			if got := xraytest.InboundUsers(t, port, ib.Tag); !slices.Equal(got, []string{want}) {
				t.Fatalf("core users after UpdateUser = %v, want exactly [%s] (old identity not removed?)", got, want)
			}
			if err := local.RemoveUser(context.Background(), ib, "alice@x"); err != nil {
				t.Fatalf("RemoveUser by logical email: %v", err)
			}
			if got := xraytest.InboundUsers(t, port, ib.Tag); len(got) != 0 {
				t.Fatalf("core users after RemoveUser = %v, want none", got)
			}
		})
	}
}

// A hot-added inbound carries its clients in raw settings; they must get the stat email too.
func TestLocalAddInboundUsesStatEmail(t *testing.T) {
	usagemult.SetActive(true)
	t.Cleanup(func() { usagemult.SetActive(false) })
	port := xraytest.StartCore(t)
	local := NewLocal(LocalDeps{APIPort: func() int { return port }})
	ib := &model.Inbound{
		Id: 23, Tag: "in-hot", Protocol: model.VLESS, Enable: true, Listen: "127.0.0.1", Port: xraytest.FreePort(t),
		Settings: `{"clients":[{"email":"bob@x","id":"` + uuid.NewString() + `"}],"decryption":"none"}`,
	}
	if err := local.AddInbound(context.Background(), ib); err != nil {
		t.Fatalf("AddInbound: %v", err)
	}
	want := []string{usagemult.StatEmail(23, "bob@x")}
	if got := xraytest.InboundUsers(t, port, ib.Tag); !slices.Equal(got, want) {
		t.Fatalf("hot-added inbound users = %v, want %v", got, want)
	}
	if !json.Valid([]byte(ib.Settings)) || !strings.Contains(ib.Settings, `"bob@x"`) {
		t.Fatalf("AddInbound mutated the caller's stored settings: %s", ib.Settings)
	}
}
