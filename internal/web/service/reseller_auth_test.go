package service

import (
	"errors"
	"testing"
	"time"

	"github.com/xlzd/gotp"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const resellerTotpSecret = "JBSWY3DPEHPK3PXPJBSWY3DP"

func totpCode(secret string, at time.Time) string { return gotp.NewDefaultTOTP(secret).AtTime(at) }

// Every refusal before a matching password must read the same, whatever the reason.
func TestResellerCheckLoginRefusalsLookAlike(t *testing.T) {
	setupResellerDB(t)
	now := time.Now()
	view := mustCreateReseller(t, resellerPayload("shop-a"))
	svc := &ResellerService{}
	db := database.GetDB()

	got, err := svc.CheckLogin("SHOP-A", resellerTestPassword, "", now)
	if err != nil || got == nil || got.Id != view.Id {
		t.Fatalf("correct login (other case): %v, %v", got, err)
	}

	cases := map[string]func(){
		"unknown username": func() {},
		"wrong password":   func() {},
		"disabled":         func() { db.Model(&model.Reseller{}).Where("id = ?", view.Id).Update("enable", false) },
		"expired": func() {
			db.Model(&model.Reseller{}).Where("id = ?", view.Id).Updates(map[string]any{"enable": true, "expiry_time": now.UnixMilli() - 1})
		},
	}
	for _, name := range []string{"unknown username", "wrong password", "disabled", "expired"} {
		cases[name]()
		user, pass := "shop-a", resellerTestPassword
		if name == "unknown username" {
			user = "nobody"
		}
		if name == "wrong password" {
			pass = "not-the-password"
		}
		r, err := svc.CheckLogin(user, pass, "", now)
		if r != nil || err == nil || err.Error() != "invalid credentials" {
			t.Errorf("%s: got %v, %v; want nil and \"invalid credentials\"", name, r, err)
		}
	}
}

func TestResellerCheckLoginTwoFactor(t *testing.T) {
	setupResellerDB(t)
	now := time.Now()
	view := mustCreateReseller(t, resellerPayload("shop-a"))
	svc := &ResellerService{}
	if err := svc.SetTotp(view.Id, resellerTotpSecret, totpCode(resellerTotpSecret, now), now); err != nil {
		t.Fatalf("SetTotp: %v", err)
	}
	if _, err := svc.CheckLogin("shop-a", resellerTestPassword, "", now); !errors.Is(err, ErrResellerTwoFactorRequired) {
		t.Fatalf("no code: err = %v, want ErrResellerTwoFactorRequired", err)
	}
	if _, err := svc.CheckLogin("shop-a", "not-the-password", "", now); err == nil || err.Error() != "invalid credentials" {
		t.Fatalf("wrong password with 2FA on must not hint at 2FA: %v", err)
	}
	if _, err := svc.CheckLogin("shop-a", resellerTestPassword, "000000", now); !errors.Is(err, ErrResellerTotpCode) {
		t.Fatalf("wrong code: err = %v, want ErrResellerTotpCode", err)
	}
	if r, err := svc.CheckLogin("shop-a", resellerTestPassword, totpCode(resellerTotpSecret, now), now); err != nil || r == nil {
		t.Fatalf("right code: %v, %v", r, err)
	}
}

func TestResellerTotpSetupAndRemoval(t *testing.T) {
	setupResellerDB(t)
	now := time.Now()
	view := mustCreateReseller(t, resellerPayload("shop-a"))
	svc := &ResellerService{}

	if err := svc.SetTotp(view.Id, "short", "123456", now); !errors.Is(err, ErrResellerTotpSecret) {
		t.Fatalf("bad secret: err = %v, want ErrResellerTotpSecret", err)
	}
	if err := svc.SetTotp(view.Id, resellerTotpSecret, "000000", now); !errors.Is(err, ErrResellerTotpCode) {
		t.Fatalf("code not matching the secret: err = %v, want ErrResellerTotpCode", err)
	}
	if err := svc.DisableTotp(view.Id, "000000", now); !errors.Is(err, ErrResellerTotpInactive) {
		t.Fatalf("disable while off: err = %v, want ErrResellerTotpInactive", err)
	}
	if err := svc.SetTotp(view.Id, resellerTotpSecret, totpCode(resellerTotpSecret, now), now); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTotp(view.Id, "KRSXG5CTMVRXEZLUKRSXG5CT", "123456", now); !errors.Is(err, ErrResellerTotpActive) {
		t.Fatalf("replacing an active secret: err = %v, want ErrResellerTotpActive", err)
	}
	if err := svc.DisableTotp(view.Id, "000000", now); !errors.Is(err, ErrResellerTotpCode) {
		t.Fatalf("disable with a wrong code: err = %v, want ErrResellerTotpCode", err)
	}
	if err := svc.DisableTotp(view.Id, totpCode(resellerTotpSecret, now), now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckLogin("shop-a", resellerTestPassword, "", now); err != nil {
		t.Fatalf("login after disabling 2FA: %v", err)
	}

	if err := svc.SetTotp(view.Id, resellerTotpSecret, totpCode(resellerTotpSecret, now), now); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetTotp(view.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckLogin("shop-a", resellerTestPassword, "", now); err != nil {
		t.Fatalf("login after the admin reset 2FA: %v", err)
	}
	if err := svc.ResetTotp(99999); !errors.Is(err, ErrResellerNotFound) {
		t.Fatalf("reset unknown: err = %v, want ErrResellerNotFound", err)
	}
}

func TestResellerMeShowsOwnNumbersAndState(t *testing.T) {
	setupResellerDB(t)
	now := time.Now()
	p := resellerPayload("shop-a")
	p.PoolBytes, p.MaxClients, p.ExpiryTime = 1000, 5, now.Add(time.Hour).UnixMilli()
	view := mustCreateReseller(t, p)
	emails := seedEnabledClientsForReset(t, &ClientService{}, 25003, 2, "me")
	ownClients(t, view.Id, emails[:1])
	db := database.GetDB()
	db.Model(&model.Reseller{}).Where("id = ?", view.Id).Update("used_traffic", 400)
	svc := &ResellerService{}

	me, err := svc.Me(view.Id, now)
	if err != nil {
		t.Fatal(err)
	}
	want := ResellerSelfView{
		Username: "shop-a", Name: "Shop shop-a", PoolBytes: 1000, UsedTraffic: 400, RemainingBytes: 600,
		MaxClients: 5, ClientCount: 1, ExpiryTime: p.ExpiryTime, State: "ok",
	}
	if me.Role != "reseller" || me.Reseller == nil || *me.Reseller != want {
		t.Fatalf("Me = %+v / %+v, want %+v", me, me.Reseller, want)
	}
	db.Model(&model.Reseller{}).Where("id = ?", view.Id).Update("used_traffic", 1000)
	if me, _ = svc.Me(view.Id, now); me.Reseller.State != "pool" || me.Reseller.RemainingBytes != 0 {
		t.Fatalf("exhausted pool: state %q remaining %d, want pool and 0", me.Reseller.State, me.Reseller.RemainingBytes)
	}
	if _, err := svc.Me(99999, now); !errors.Is(err, ErrResellerNotFound) {
		t.Fatalf("unknown: err = %v", err)
	}
}
