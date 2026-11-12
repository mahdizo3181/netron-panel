package service

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"
)

const usageMultiplierEnableKey = "usageMultiplierEnable"

// GetUsageMultiplierEnable is the saved kill switch; the running mode is usagemult.Active.
func (s *SettingService) GetUsageMultiplierEnable() (bool, error) {
	return s.getBool(usageMultiplierEnableKey)
}

// latchUsageMultiplierMode adopts the saved mode for the Xray process a forced
// restart is about to start; any other restart keeps the running mode.
func (s *XrayService) latchUsageMultiplierMode(isForce bool) {
	if !isForce {
		return
	}
	on, err := s.settingService.GetUsageMultiplierEnable()
	if err != nil {
		logger.Warning("usage multiplier: read setting failed, keeping current mode:", err)
		return
	}
	if on != usagemult.Active() {
		logger.Infof("usage multiplier: per-inbound accounting %s with this Xray restart", map[bool]string{true: "enabled", false: "disabled"}[on])
	}
	usagemult.SetActive(on)
}

// ApplySavedUsageMultiplierMode force-restarts Xray when the saved mode is not
// live, so a panel-only restart applies it; a manually stopped core stays down.
func (s *XrayService) ApplySavedUsageMultiplierMode() error {
	on, err := s.settingService.GetUsageMultiplierEnable()
	if err != nil || on == usagemult.Active() || isManuallyStopped.Load() {
		return err
	}
	return s.RestartXray(true)
}
