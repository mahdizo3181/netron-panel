package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
)

func initUsageMultTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dir)
	dbtest.InitDB(t, filepath.Join(dir, "x-ui.db"))
	t.Cleanup(func() { usagemult.SetActive(false) })
}

func TestUsageMultiplierSettingDefaultsOff(t *testing.T) {
	initUsageMultTestDB(t)
	s := SettingService{}
	on, err := s.GetUsageMultiplierEnable()
	if err != nil {
		t.Fatalf("read setting: %v", err)
	}
	if on {
		t.Fatal("kill switch must default OFF so a fresh install is stock 3x-ui")
	}
	all, err := s.GetAllSetting()
	if err != nil {
		t.Fatalf("GetAllSetting: %v", err)
	}
	if all.UsageMultiplierEnable {
		t.Fatal("AllSetting.UsageMultiplierEnable must default false")
	}
}

// A saved toggle must not reach a running core through a hot apply: only a
// forced (full) restart may flip the latch.
func TestUsageMultiplierLatchFlipsOnlyOnForcedRestart(t *testing.T) {
	initUsageMultTestDB(t)
	s := &XrayService{}
	if err := s.settingService.setBool(usageMultiplierEnableKey, true); err != nil {
		t.Fatalf("enable setting: %v", err)
	}

	s.latchUsageMultiplierMode(false)
	if usagemult.Active() {
		t.Fatal("non-forced restart (hot apply path) flipped the latch")
	}
	s.latchUsageMultiplierMode(true)
	if !usagemult.Active() {
		t.Fatal("forced restart did not latch the saved mode")
	}

	if err := s.settingService.setBool(usageMultiplierEnableKey, false); err != nil {
		t.Fatalf("disable setting: %v", err)
	}
	s.latchUsageMultiplierMode(false)
	if !usagemult.Active() {
		t.Fatal("turning the setting off reached the running core without a restart")
	}
	s.latchUsageMultiplierMode(true)
	if usagemult.Active() {
		t.Fatal("forced restart did not latch OFF")
	}
}
