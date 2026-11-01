package service

import "errors"

// Protocol of the subscription's non-functional info config; Happ rejects socks entries.
const (
	SubInfoNodeLinkVless = "vless"
	SubInfoNodeLinkSocks = "socks"
)

func (s *SettingService) GetSubInfoNodeLinkType() (string, error) {
	return s.getString("subInfoNodeLinkType")
}

func normalizeSubInfoNodeLinkType(value string) (string, error) {
	switch value {
	case "":
		return SubInfoNodeLinkVless, nil
	case SubInfoNodeLinkVless, SubInfoNodeLinkSocks:
		return value, nil
	}
	return "", errors.New("info config link type must be vless or socks")
}
