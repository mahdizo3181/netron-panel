package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

func TestClientListHTTPScopesByOwnerAdmin(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
	db := database.GetDB()

	var ownerRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOwner).First(&ownerRole).Error; err != nil {
		t.Fatalf("owner role: %v", err)
	}
	var opRole model.AdminRole
	if err := db.Where("slug = ?", model.AdminRoleSlugOperator).First(&opRole).Error; err != nil {
		t.Fatalf("operator role: %v", err)
	}

	owner := model.User{Username: "panel-owner", Status: model.AdminStatusActive, RoleId: ownerRole.Id}
	op := model.User{Username: "reseller-a", Status: model.AdminStatusActive, RoleId: opRole.Id}
	opB := model.User{Username: "reseller-b", Status: model.AdminStatusActive, RoleId: opRole.Id}
	for _, u := range []*model.User{&owner, &op, &opB} {
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := db.Create(&model.ClientRecord{Email: "a-client@e2e", OwnerAdminId: op.Id}).Error; err != nil {
		t.Fatalf("client a: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "r2-good@e2e", OwnerAdminId: opB.Id}).Error; err != nil {
		t.Fatalf("client b: %v", err)
	}

	engine := gin.New()
	store := cookie.NewStore([]byte("client-list-scope-test"))
	engine.Use(sessions.Sessions("3x-ui", store))
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	engine.GET("/test/login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		var u model.User
		if err := db.Where("id = ?", id).First(&u).Error; err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		if err := session.SetLoginUser(c, &u); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})
	NewClientController(engine.Group("/panel/api/clients"))

	emailsFor := func(userID int) []string {
		loginW := httptest.NewRecorder()
		engine.ServeHTTP(loginW, httptest.NewRequest(http.MethodGet, "/test/login/"+strconv.Itoa(userID), nil))
		if loginW.Code != http.StatusOK {
			t.Fatalf("login user %d status=%d", userID, loginW.Code)
		}
		listW := httptest.NewRecorder()
		listReq := httptest.NewRequest(http.MethodGet, "/panel/api/clients/list", nil)
		for _, c := range loginW.Result().Cookies() {
			listReq.AddCookie(c)
		}
		engine.ServeHTTP(listW, listReq)
		if listW.Code != http.StatusOK {
			t.Fatalf("user %d list status=%d body=%s", userID, listW.Code, listW.Body.String())
		}
		var envelope struct {
			Success bool `json:"success"`
			Obj     []struct {
				Email string `json:"email"`
			} `json:"obj"`
		}
		if err := json.Unmarshal(listW.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out := make([]string, 0, len(envelope.Obj))
		for _, row := range envelope.Obj {
			out = append(out, row.Email)
		}
		return out
	}

	opEmails := emailsFor(op.Id)
	for _, em := range opEmails {
		if em == "r2-good@e2e" {
			t.Fatalf("operator list must not contain r2-good@e2e, got %v", opEmails)
		}
	}

	ownerEmails := emailsFor(owner.Id)
	hasA, hasB := false, false
	for _, em := range ownerEmails {
		if em == "a-client@e2e" {
			hasA = true
		}
		if em == "r2-good@e2e" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Fatalf("owner list = %v, want both reseller clients", ownerEmails)
	}
}