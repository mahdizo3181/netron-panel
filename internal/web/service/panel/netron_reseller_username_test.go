package panel

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// The login form is shared: an admin renamed onto a reseller's name would
// shadow that reseller (the admin lookup runs first).
func TestAdminCannotTakeAResellersUsername(t *testing.T) {
	rename := map[string]func(admin model.User, username string) error{
		"panel settings": func(admin model.User, username string) error {
			return (&UserService{}).UpdateUser(admin.Id, username, "new-admin-password")
		},
		"x-ui setting CLI": func(_ model.User, username string) error {
			return (&UserService{}).UpdateFirstUser(username, "new-admin-password")
		},
	}
	for name, do := range rename {
		t.Run(name, func(t *testing.T) {
			dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
			if _, err := (&service.ResellerService{}).Create(service.ResellerPayload{Username: "shop-a", Password: "correct-horse-battery"}); err != nil {
				t.Fatal(err)
			}
			var before model.User
			if err := database.GetDB().First(&before).Error; err != nil {
				t.Fatal(err)
			}
			if err := do(before, "Shop-A"); !errors.Is(err, service.ErrResellerUsernameTaken) {
				t.Fatalf("rename onto a reseller's name: err = %v, want ErrResellerUsernameTaken", err)
			}
			var after model.User
			database.GetDB().First(&after)
			if after.Username != before.Username || after.Password != before.Password || after.LoginEpoch != before.LoginEpoch {
				t.Fatalf("a refused rename changed the admin row: %+v -> %+v", before, after)
			}
			if err := do(before, "free-name"); err != nil {
				t.Fatalf("rename to a free name: %v", err)
			}
		})
	}
}
