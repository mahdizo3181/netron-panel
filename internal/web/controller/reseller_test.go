package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func setupResellerRouter(t *testing.T) *gin.Engine {
	t.Helper()
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewResellerController(router.Group("/panel/api"))
	return router
}

func resellerCall(t *testing.T, router *gin.Engine, method, path, body string) (hostEnvelope, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("%s %s: status %d, body=%s", method, path, resp.Code, resp.Body.String())
	}
	var env hostEnvelope
	if err := json.Unmarshal(resp.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: decode %q: %v", method, path, resp.Body.String(), err)
	}
	return env, resp.Body.String()
}

type resellerWire struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Enable      bool   `json:"enable"`
	PoolBytes   int64  `json:"poolBytes"`
	MaxClients  int    `json:"maxClients"`
	ClientCount int64  `json:"clientCount"`
}

// The admin drives the whole reseller lifecycle over HTTP; no response may carry
// the password, and a refused request must say why.
func TestResellerAdminCRUDOverHTTP(t *testing.T) {
	router := setupResellerRouter(t)
	const secret = "correct-horse-battery"

	env, raw := resellerCall(t, router, http.MethodPost, "/panel/api/resellers",
		`{"username":"shop-a","password":"`+secret+`","name":"Shop A","poolBytes":1000,"maxClients":5}`)
	var created resellerWire
	if !env.Success || json.Unmarshal(env.Obj, &created) != nil || created.Id == 0 ||
		created.Username != "shop-a" || !created.Enable || created.PoolBytes != 1000 || created.MaxClients != 5 {
		t.Fatalf("create: %s", raw)
	}
	if strings.Contains(raw, secret) || strings.Contains(strings.ToLower(raw), "password") {
		t.Fatalf("create response leaks the password: %s", raw)
	}
	id := "/panel/api/resellers/" + jsonInt(created.Id)

	env, raw = resellerCall(t, router, http.MethodPost, "/panel/api/resellers", `{"username":"SHOP-A","password":"`+secret+`"}`)
	if env.Success || !strings.Contains(env.Msg, "username is already in use") {
		t.Fatalf("duplicate username must be refused with its reason: %s", raw)
	}

	env, raw = resellerCall(t, router, http.MethodPost, id, `{"name":"Renamed","enable":false,"poolBytes":2000,"maxClients":9}`)
	var updated resellerWire
	if !env.Success || json.Unmarshal(env.Obj, &updated) != nil || updated.Name != "Renamed" || updated.Enable || updated.PoolBytes != 2000 {
		t.Fatalf("update: %s", raw)
	}

	env, raw = resellerCall(t, router, http.MethodGet, "/panel/api/resellers", "")
	var list []resellerWire
	if !env.Success || json.Unmarshal(env.Obj, &list) != nil || len(list) != 1 || list[0].Name != "Renamed" {
		t.Fatalf("list: %s", raw)
	}
	env, raw = resellerCall(t, router, http.MethodGet, id, "")
	var one resellerWire
	if !env.Success || json.Unmarshal(env.Obj, &one) != nil || one.Id != created.Id {
		t.Fatalf("get: %s", raw)
	}
	if env, raw = resellerCall(t, router, http.MethodGet, "/panel/api/resellers/99999", ""); env.Success || !strings.Contains(env.Msg, "reseller not found") {
		t.Fatalf("get of an unknown id: %s", raw)
	}

	if env, raw = resellerCall(t, router, http.MethodPost, id+"/password", `{"password":"short"}`); env.Success || !strings.Contains(env.Msg, "at least 10 characters") {
		t.Fatalf("weak password must be refused with its reason: %s", raw)
	}
	if env, raw = resellerCall(t, router, http.MethodPost, id+"/password", `{"password":"another-long-secret"}`); !env.Success || strings.Contains(raw, "another-long-secret") {
		t.Fatalf("set password: %s", raw)
	}

	if env, raw = resellerCall(t, router, http.MethodPost, id+"/del", `{"clients":"purge"}`); env.Success || !strings.Contains(env.Msg, "transfer or delete") {
		t.Fatalf("unknown delete mode must be refused: %s", raw)
	}
	if env, raw = resellerCall(t, router, http.MethodPost, id+"/del", ""); !env.Success {
		t.Fatalf("delete with no body (default transfer): %s", raw)
	}
	var left int64
	database.GetDB().Model(&model.Reseller{}).Count(&left)
	if left != 0 {
		t.Fatalf("resellers left after delete = %d", left)
	}
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
