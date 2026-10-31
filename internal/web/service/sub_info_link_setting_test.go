package service

import "testing"

// Existing installs never stored the key, so the default must be vless (Happ rejects socks).
func TestSubInfoNodeLinkTypeDefaultsToVlessAndRejectsInvalid(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	settings, err := s.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SubInfoNodeLinkType != "vless" {
		t.Fatalf("default link type = %q, want vless", settings.SubInfoNodeLinkType)
	}

	settings.SubInfoNodeLinkType = "socks"
	if err := s.UpdateAllSetting(settings, SecretClears{}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetSubInfoNodeLinkType(); err != nil || got != "socks" {
		t.Fatalf("stored link type = %q, %v; want socks", got, err)
	}

	settings.SubInfoNodeLinkType = "http"
	if err := s.UpdateAllSetting(settings, SecretClears{}); err == nil || err.Error() != "info config link type must be vless or socks" {
		t.Fatalf("invalid link type error = %v", err)
	}
	if got, _ := s.GetSubInfoNodeLinkType(); got != "socks" {
		t.Fatalf("rejected save changed link type to %q", got)
	}

	settings.SubInfoNodeLinkType = ""
	if err := s.UpdateAllSetting(settings, SecretClears{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSubInfoNodeLinkType(); got != "vless" {
		t.Fatalf("empty link type saved as %q, want vless", got)
	}
}
