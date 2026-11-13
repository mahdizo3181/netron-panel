package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func (b *browser) json(method, path string, body any) (int, envelope, string) {
	b.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, b.h.url+path, rd)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-CSRF-Token", b.csrf)
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var e envelope
	_ = json.Unmarshal(raw, &e)
	return resp.StatusCode, e, string(raw)
}

func (h *panelHarness) inbound(port int) int {
	h.t.Helper()
	var admin model.User
	database.GetDB().First(&admin)
	ib := &model.Inbound{
		UserId: admin.Id, Tag: fmt.Sprintf("vless-%d", port), Enable: true, Port: port, Protocol: model.VLESS,
		Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		h.t.Fatal(err)
	}
	return ib.Id
}

func (h *panelHarness) resellerOn(username string, maxClients int, inboundIds ...int) *model.Reseller {
	h.t.Helper()
	v, err := (&service.ResellerService{}).Create(service.ResellerPayload{
		Username: username, Password: e2eResellerPassword, Name: "Shop " + username, MaxClients: maxClients, InboundIds: inboundIds,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	var r model.Reseller
	database.GetDB().First(&r, v.Id)
	return &r
}

func (h *panelHarness) client(scope service.Scope, email, subID string, inboundIds ...int) {
	h.t.Helper()
	if _, err := (&service.ClientService{}).CreateFor(scope, &service.InboundService{}, &service.ClientCreatePayload{
		Client: model.Client{Email: email, ID: uuid.NewString(), SubID: subID, Enable: true}, InboundIds: inboundIds,
	}); err != nil {
		h.t.Fatal(err)
	}
}

func dbClient(t *testing.T, email string) *model.ClientRecord {
	t.Helper()
	var r model.ClientRecord
	if err := database.GetDB().Where("email = ?", email).First(&r).Error; err != nil {
		return nil
	}
	return &r
}

func createBody(email string, inboundIds ...int) map[string]any {
	return map[string]any{"client": map[string]any{"email": email, "id": uuid.NewString(), "enable": true}, "inboundIds": inboundIds}
}

// The list of reseller-reachable API routes is deliberate: a new entry has to be
// added here on purpose, together with a scope check behind it.
func TestResellerReachableRoutesAreExactlyTheListedRoutes(t *testing.T) {
	var reachable []string
	for route, p := range controllerRoutePolicies() {
		if p && strings.HasPrefix(strings.Fields(route)[1], "/panel/api/") {
			reachable = append(reachable, route)
		}
	}
	want := []string{
		"GET /panel/api/clients/get/:email",
		"GET /panel/api/clients/list/paged",
		"GET /panel/api/inbounds/options",
		"GET /panel/api/reseller/me",
		"POST /panel/api/clients/add",
		"POST /panel/api/clients/bulkCreate",
		"POST /panel/api/clients/bulkDisable",
		"POST /panel/api/clients/bulkEnable",
		"POST /panel/api/clients/del/:email",
		"POST /panel/api/clients/resetTraffic/:email",
		"POST /panel/api/clients/update/:email",
		"POST /panel/api/reseller/me/password",
		"POST /panel/api/reseller/me/totp",
		"POST /panel/api/reseller/me/totp/disable",
		"POST /panel/api/setting/defaultSettings",
	}
	if got := sortedJoin(reachable); got != sortedJoin(want) {
		t.Fatalf("API routes a reseller may call:\n%s\nwant:\n%s", got, sortedJoin(want))
	}
}

type idorFixture struct {
	h                    *panelHarness
	me                   *browser
	admin                *browser
	mine, theirs         int
	r, other             *model.Reseller
	ownEmail, bossEmail  string
	otherEmail, bossSub  string
	bossID               int
	bossUUID, bossInbIDs string
}

func newIDORFixture(t *testing.T, name string) *idorFixture {
	t.Helper()
	h := newPanelHarness(t, "admin-"+name)
	f := &idorFixture{h: h, ownEmail: "own@" + name, bossEmail: "boss@" + name, otherEmail: "other@" + name, bossSub: "sub-boss-" + name}
	f.mine, f.theirs = h.inbound(27001), h.inbound(27002)
	f.r = h.resellerOn("shop-"+name, 0, f.mine)
	f.other = h.resellerOn("rival-"+name, 0, f.theirs)
	h.client(service.ResellerScope(f.r.Id), f.ownEmail, "", f.mine)
	h.client(service.ResellerScope(f.other.Id), f.otherEmail, "", f.theirs)
	h.client(service.AdminScope(), f.bossEmail, f.bossSub, f.theirs)
	boss := dbClient(t, f.bossEmail)
	f.bossID, f.bossUUID = boss.Id, boss.UUID
	ids, _ := (&service.ClientService{}).GetInboundIdsForRecord(boss.Id)
	f.bossInbIDs = fmt.Sprint(ids)
	f.me = h.browser()
	f.me.mustLogin("shop-"+name, e2eResellerPassword)
	f.admin = h.browser()
	f.admin.mustLogin("admin-"+name, e2eAdminPassword)
	return f
}

func (f *idorFixture) foreignUntouched(t *testing.T) {
	t.Helper()
	for _, email := range []string{f.bossEmail, f.otherEmail} {
		row := dbClient(t, email)
		if row == nil || !row.Enable || row.Comment != "" {
			t.Fatalf("foreign client %s changed or vanished: %+v", email, row)
		}
	}
	boss := dbClient(t, f.bossEmail)
	ids, _ := (&service.ClientService{}).GetInboundIdsForRecord(boss.Id)
	if boss.UUID != f.bossUUID || fmt.Sprint(ids) != f.bossInbIDs || boss.ResellerID != nil {
		t.Fatalf("the admin's client was rewired: uuid %s inbounds %v owner %v", boss.UUID, ids, boss.ResellerID)
	}
}

// Every way a reseller can name a client or an inbound: a foreign one must look
// exactly like a missing one, and must come out unchanged.
func TestResellerIDORMatrix(t *testing.T) {
	f := newIDORFixture(t, "idor")
	_, missing, _ := f.me.json(http.MethodGet, "/panel/api/clients/get/nobody@x", nil)
	_, ownDup, _ := f.me.json(http.MethodPost, "/panel/api/clients/add",
		map[string]any{"client": map[string]any{"email": f.ownEmail, "subId": "fresh", "enable": true}, "inboundIds": []int{f.mine}})

	for _, foreign := range []string{f.bossEmail, f.otherEmail} {
		cases := []struct {
			name, method, path string
			body               any
		}{
			{"get by email", http.MethodGet, "/panel/api/clients/get/" + foreign, nil},
			{"update", http.MethodPost, "/panel/api/clients/update/" + foreign, map[string]any{"email": foreign, "comment": "hijacked", "enable": false}},
			{"delete", http.MethodPost, "/panel/api/clients/del/" + foreign, nil},
			{"reset traffic", http.MethodPost, "/panel/api/clients/resetTraffic/" + foreign, nil},
			{"bulk disable, own plus foreign", http.MethodPost, "/panel/api/clients/bulkDisable", map[string]any{"emails": []string{f.ownEmail, foreign}}},
			{"bulk enable, foreign", http.MethodPost, "/panel/api/clients/bulkEnable", map[string]any{"emails": []string{foreign}}},
		}
		for _, tc := range cases {
			code, e, raw := f.me.json(tc.method, tc.path, tc.body)
			if code != http.StatusOK || e.Success {
				t.Errorf("%s %s: %d %s, want a refusal", tc.name, foreign, code, raw)
			}
			if tc.name == "get by email" && e.Msg != missing.Msg {
				t.Errorf("a foreign email answers %q, a missing one %q", e.Msg, missing.Msg)
			}
		}
	}
	if row := dbClient(t, f.ownEmail); !row.Enable {
		t.Fatal("a refused bulk disable still disabled the reseller's own client")
	}

	// Taken emails, including the same-subId re-add path, answer like a duplicate of one's own.
	for _, body := range []map[string]any{
		{"client": map[string]any{"email": f.bossEmail, "subId": "fresh", "enable": true}, "inboundIds": []int{f.mine}},
		{"client": map[string]any{"email": f.bossEmail, "subId": f.bossSub, "enable": true}, "inboundIds": []int{f.mine}},
		{"client": map[string]any{"email": strings.ToUpper(f.bossEmail), "subId": f.bossSub, "enable": true}, "inboundIds": []int{f.mine}},
	} {
		_, e, raw := f.me.json(http.MethodPost, "/panel/api/clients/add", body)
		email := body["client"].(map[string]any)["email"].(string)
		if want := strings.Replace(ownDup.Msg, f.ownEmail, email, 1); e.Success || e.Msg != want {
			t.Errorf("create %s: %s, want %q", email, raw, want)
		}
	}

	// Inbound ids inside bodies and query strings.
	for name, call := range map[string]func() envelope{
		"create on a foreign inbound": func() envelope {
			_, e, _ := f.me.json(http.MethodPost, "/panel/api/clients/add", createBody("x1@idor", f.theirs))
			return e
		},
		"create on own and foreign inbounds": func() envelope {
			_, e, _ := f.me.json(http.MethodPost, "/panel/api/clients/add", createBody("x2@idor", f.mine, f.theirs))
			return e
		},
		"bulk create with one foreign inbound": func() envelope {
			_, e, _ := f.me.json(http.MethodPost, "/panel/api/clients/bulkCreate", []any{createBody("x3@idor", f.mine), createBody("x4@idor", f.theirs)})
			return e
		},
		"update filtered to a foreign inbound": func() envelope {
			_, e, _ := f.me.json(http.MethodPost, fmt.Sprintf("/panel/api/clients/update/%s?inboundIds=%d", f.ownEmail, f.theirs),
				map[string]any{"email": f.ownEmail, "enable": true})
			return e
		},
	} {
		if e := call(); e.Success {
			t.Errorf("%s was accepted", name)
		}
	}
	for _, email := range []string{"x1@idor", "x2@idor", "x3@idor", "x4@idor"} {
		if dbClient(t, email) != nil {
			t.Errorf("refused create left %s behind", email)
		}
	}

	// Routes that look clients up by subId, numeric id, or that list or report on
	// other clients stay admin-only.
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/panel/api/clients/get/subId/" + f.bossSub},
		{http.MethodPost, fmt.Sprintf("/panel/api/clients/happLink/%d", f.bossID)},
		{http.MethodGet, "/panel/api/clients/subLinks/" + f.bossSub},
		{http.MethodGet, "/panel/api/clients/links/" + f.bossEmail},
		{http.MethodGet, "/panel/api/clients/traffic/" + f.bossEmail},
		{http.MethodPost, "/panel/api/clients/ips/" + f.bossEmail},
		{http.MethodPost, "/panel/api/clients/onlines"},
		{http.MethodPost, "/panel/api/clients/lastOnline"},
		{http.MethodGet, "/panel/api/clients/list"},
		{http.MethodGet, "/panel/api/clients/export"},
		{http.MethodGet, "/panel/api/inbounds/list"},
		{http.MethodGet, "/panel/api/inbounds/list/slim"},
		{http.MethodGet, fmt.Sprintf("/panel/api/inbounds/get/%d", f.theirs)},
		{http.MethodGet, "/panel/api/server/clientIps"},
		{http.MethodPost, "/panel/api/clients/" + f.bossEmail + "/owner"},
	} {
		if code, _, raw := f.me.json(route.method, route.path, map[string]any{"resellerId": f.r.Id}); code != http.StatusForbidden {
			t.Errorf("%s %s with a reseller session = %d %s, want 403", route.method, route.path, code, raw)
		}
	}

	_, e, raw := f.me.json(http.MethodGet, "/panel/api/clients/list/paged?search=boss", nil)
	var page struct {
		Items    []map[string]any `json:"items"`
		Filtered int              `json:"filtered"`
		Total    int              `json:"total"`
		Summary  struct {
			Total int `json:"total"`
		} `json:"summary"`
	}
	if json.Unmarshal(e.Obj, &page) != nil || len(page.Items) != 0 || page.Filtered != 0 || page.Total != 1 || page.Summary.Total != 1 {
		t.Fatalf("searching for the admin's client: %s", raw)
	}
	f.foreignUntouched(t)
}

// Ownership and pool fields can never be written through a client body, by a
// reseller or by the admin; only the owner route reassigns.
func TestClientBodiesCannotAssignOwnerOrPoolFields(t *testing.T) {
	f := newIDORFixture(t, "mass")
	smuggled := map[string]any{
		"owner": map[string]any{"id": f.other.Id, "name": "x"}, "resellerId": f.other.Id, "reseller_id": f.other.Id,
		"ResellerID": f.other.Id, "poolDisabled": true, "pool_disabled": true, "usedTraffic": 999, "used_traffic": 999,
	}
	client := map[string]any{"email": "smuggle@mass", "id": uuid.NewString(), "enable": true, "group": "vip"}
	body := map[string]any{"client": client, "inboundIds": []int{f.mine}}
	for k, v := range smuggled {
		client[k] = v
		body[k] = v
	}
	if _, e, raw := f.me.json(http.MethodPost, "/panel/api/clients/add", body); !e.Success {
		t.Fatalf("create: %s", raw)
	}
	update := map[string]any{"email": f.ownEmail, "enable": true, "comment": "edited"}
	for k, v := range smuggled {
		update[k] = v
	}
	if _, e, raw := f.me.json(http.MethodPost, "/panel/api/clients/update/"+f.ownEmail, update); !e.Success {
		t.Fatalf("update: %s", raw)
	}
	adminBody := createBody("adminmade@mass", f.mine)
	for k, v := range smuggled {
		adminBody[k] = v
		adminBody["client"].(map[string]any)[k] = v
	}
	if _, e, raw := f.admin.json(http.MethodPost, "/panel/api/clients/add", adminBody); !e.Success {
		t.Fatalf("admin create: %s", raw)
	}

	for email, wantOwner := range map[string]*int{"smuggle@mass": &f.r.Id, f.ownEmail: &f.r.Id, "adminmade@mass": nil} {
		row := dbClient(t, email)
		if row == nil || fmt.Sprint(row.ResellerID != nil && wantOwner != nil && *row.ResellerID == *wantOwner) != fmt.Sprint(wantOwner != nil) ||
			(wantOwner == nil && row.ResellerID != nil) || row.PoolDisabled {
			t.Errorf("%s: owner %v poolDisabled %v, want owner %v and no flag", email, row.ResellerID, row.PoolDisabled, wantOwner)
		}
	}
	if row := dbClient(t, "smuggle@mass"); row.Group != "" {
		t.Errorf("a reseller set a group: %q", row.Group)
	}
	var used []int64
	database.GetDB().Model(&model.Reseller{}).Order("id").Pluck("used_traffic", &used)
	if fmt.Sprint(used) != "[0 0]" {
		t.Fatalf("pool usage written through a client body: %v", used)
	}
}

func TestOwnerFieldIsTheAdminsAndReadOnly(t *testing.T) {
	f := newIDORFixture(t, "owner")
	_, _, adminList := f.admin.json(http.MethodGet, "/panel/api/clients/list/paged?sort=email&order=asc", nil)
	for _, want := range []string{
		fmt.Sprintf(`"email":"%s"`, f.ownEmail), fmt.Sprintf(`"owner":{"id":%d,"name":"Shop shop-owner"}`, f.r.Id), `"owner":null`,
	} {
		if !strings.Contains(adminList, want) {
			t.Errorf("admin list lacks %s: %s", want, adminList)
		}
	}
	_, _, adminGet := f.admin.json(http.MethodGet, "/panel/api/clients/get/"+f.ownEmail, nil)
	if !strings.Contains(adminGet, fmt.Sprintf(`"owner":{"id":%d,`, f.r.Id)) {
		t.Errorf("admin get lacks the owner: %s", adminGet)
	}
	_, _, myList := f.me.json(http.MethodGet, "/panel/api/clients/list/paged", nil)
	_, _, myGet := f.me.json(http.MethodGet, "/panel/api/clients/get/"+f.ownEmail, nil)
	for name, raw := range map[string]string{"list": myList, "get": myGet} {
		if !strings.Contains(raw, f.ownEmail) || strings.Contains(raw, `"owner"`) || strings.Contains(raw, f.r.Name) {
			t.Errorf("reseller %s carries owner data or lost its client: %s", name, raw)
		}
	}

	_, e, raw := f.admin.json(http.MethodPost, "/panel/api/clients/"+f.bossEmail+"/owner", map[string]any{"resellerId": f.r.Id})
	var change struct {
		Owner *struct {
			Id int `json:"id"`
		} `json:"owner"`
		OutsideInbounds []int `json:"outsideInbounds"`
	}
	if !e.Success || json.Unmarshal(e.Obj, &change) != nil || change.Owner == nil || change.Owner.Id != f.r.Id ||
		fmt.Sprint(change.OutsideInbounds) != fmt.Sprint([]int{f.theirs}) {
		t.Fatalf("reassign: %s", raw)
	}
	if row := dbClient(t, f.bossEmail); row.ResellerID == nil || *row.ResellerID != f.r.Id {
		t.Fatalf("reassigned owner = %v", row.ResellerID)
	}
	if _, e, raw := f.admin.json(http.MethodPost, "/panel/api/clients/"+f.bossEmail+"/owner", map[string]any{"resellerId": nil}); !e.Success {
		t.Fatalf("back to admin: %s", raw)
	}
}

func TestResellerInboundOptionsAreItsOwnAndStripped(t *testing.T) {
	f := newIDORFixture(t, "opts")
	database.GetDB().Model(&model.Inbound{}).Where("id = ?", f.mine).Updates(map[string]any{"listen": "10.0.0.1", "share_addr": "edge.example"})
	_, e, raw := f.me.json(http.MethodGet, "/panel/api/inbounds/options", nil)
	var opts []map[string]any
	if !e.Success || json.Unmarshal(e.Obj, &opts) != nil || len(opts) != 1 || opts[0]["id"] != float64(f.mine) {
		t.Fatalf("reseller options = %s, want only inbound %d", raw, f.mine)
	}
	for _, key := range []string{"listen", "shareAddr", "nodeId", "nodeAddress"} {
		if _, ok := opts[0][key]; ok {
			t.Errorf("reseller options carry %s: %s", key, raw)
		}
	}
	_, e, raw = f.admin.json(http.MethodGet, "/panel/api/inbounds/options", nil)
	if json.Unmarshal(e.Obj, &opts) != nil || len(opts) != 2 || !strings.Contains(raw, `"listen":"10.0.0.1"`) {
		t.Fatalf("admin options = %s, want both inbounds with their addresses", raw)
	}
}

// The reseller's own client lifecycle over HTTP, end to end.
func TestResellerManagesItsOwnClientsOverHTTP(t *testing.T) {
	f := newIDORFixture(t, "life")
	if _, e, raw := f.me.json(http.MethodPost, "/panel/api/clients/add", createBody("new@life", f.mine)); !e.Success {
		t.Fatalf("create: %s", raw)
	}
	if _, e, raw := f.me.json(http.MethodPost, "/panel/api/clients/bulkCreate", []any{createBody("bulk1@life", f.mine), createBody("bulk2@life", f.mine)}); !e.Success {
		t.Fatalf("bulk create: %s", raw)
	}
	steps := []struct {
		name, method, path string
		body               any
	}{
		{"update expiry", http.MethodPost, "/panel/api/clients/update/new@life", map[string]any{"email": "new@life", "enable": true, "expiryTime": 1893456000000, "totalGB": 5 << 30}},
		{"disable", http.MethodPost, "/panel/api/clients/bulkDisable", map[string]any{"emails": []string{"new@life"}}},
		{"enable", http.MethodPost, "/panel/api/clients/bulkEnable", map[string]any{"emails": []string{"new@life"}}},
		{"reset traffic", http.MethodPost, "/panel/api/clients/resetTraffic/new@life", nil},
		{"delete", http.MethodPost, "/panel/api/clients/del/bulk2@life", nil},
	}
	for _, s := range steps {
		if code, e, raw := f.me.json(s.method, s.path, s.body); code != http.StatusOK || !e.Success {
			t.Fatalf("%s: %d %s", s.name, code, raw)
		}
	}
	row := dbClient(t, "new@life")
	if row == nil || !row.Enable || row.ExpiryTime != 1893456000000 || row.TotalGB != 5<<30 || row.ResellerID == nil || *row.ResellerID != f.r.Id {
		t.Fatalf("after the lifecycle: %+v", row)
	}
	if dbClient(t, "bulk2@life") != nil || dbClient(t, "bulk1@life") == nil {
		t.Fatal("bulk create or delete did not land")
	}
	f.foreignUntouched(t)
}
