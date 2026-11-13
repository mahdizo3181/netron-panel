package sub

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// Ownership is panel bookkeeping: what an end user's app downloads must be the
// same byte for byte whoever owns the client, and a reseller's client is one link.
func TestSubscriptionOutputIgnoresClientOwnership(t *testing.T) {
	initSubDB(t)
	var admin model.User
	database.GetDB().First(&admin)
	ib := &model.Inbound{
		UserId: admin.Id, Tag: "own-vless", Enable: true, Port: 27101, Protocol: model.VLESS,
		Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	r, err := (&service.ResellerService{}).Create(service.ResellerPayload{Username: "shop-sub", Password: "correct-horse-battery", InboundIds: []int{ib.Id}})
	if err != nil {
		t.Fatal(err)
	}
	clients := &service.ClientService{}
	create := func(scope service.Scope, email, uuid, subID string) {
		t.Helper()
		if _, err := clients.CreateFor(scope, &service.InboundService{}, &service.ClientCreatePayload{
			Client: model.Client{Email: email, ID: uuid, SubID: subID, Enable: true}, InboundIds: []int{ib.Id},
		}); err != nil {
			t.Fatal(err)
		}
	}
	render := func(subID string) string {
		t.Helper()
		links, headers, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(links, "\n") + "|" + strings.Join(headers, "\n")
	}
	create(service.AdminScope(), "alice@sub", "aaaaaaaa-1111-4222-8333-444444444444", "sub-alice")
	before := render("sub-alice")
	if !strings.Contains(before, "aaaaaaaa-1111-4222-8333-444444444444") {
		t.Fatalf("fixture: subscription has no link for the client: %q", before)
	}
	if _, err := clients.SetOwner("alice@sub", &r.Id); err != nil {
		t.Fatal(err)
	}
	if after := render("sub-alice"); after != before {
		t.Fatalf("reassigning to a reseller changed the subscription:\nbefore %q\nafter  %q", before, after)
	}
	if _, err := clients.SetOwner("alice@sub", nil); err != nil {
		t.Fatal(err)
	}
	if after := render("sub-alice"); after != before {
		t.Fatalf("handing back to the admin changed the subscription:\nbefore %q\nafter  %q", before, after)
	}

	create(service.ResellerScope(r.Id), "bob@sub", "bbbbbbbb-1111-4222-8333-444444444444", "sub-bob")
	links, _, _, _, err := NewSubService("").GetSubs("sub-bob", "sub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	same := strings.NewReplacer("bbbbbbbb-1111-4222-8333-444444444444", "aaaaaaaa-1111-4222-8333-444444444444", "bob@sub", "alice@sub", "bob%40sub", "alice%40sub")
	alice, _, _, _, _ := NewSubService("").GetSubs("sub-alice", "sub.example.com")
	if len(links) != 1 || same.Replace(links[0]) != alice[0] {
		t.Fatalf("a reseller-created client's subscription = %q, want one link shaped like the admin's %q", links, alice)
	}
}
