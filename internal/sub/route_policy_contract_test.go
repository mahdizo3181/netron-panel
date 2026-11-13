package sub

import (
	"sort"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// subscriptionRoutes classifies the subscription server: all public and keyed by
// subId only, with no panel session, so no reseller rule can apply (docs/RESELLERS.md §7).
var subscriptionRoutes = map[string]string{
	"GET /sub-c/:subid":              "public: raw subscription",
	"HEAD /sub-c/:subid":             "public: raw subscription",
	"GET /sub-c/:subid/hwid-status":  "public: HWID slot status",
	"HEAD /sub-c/:subid/hwid-status": "public: HWID slot status",
	"GET /json-c/:subid":             "public: JSON subscription",
	"HEAD /json-c/:subid":            "public: JSON subscription",
	"GET /clash-c/:subid":            "public: Clash subscription",
	"HEAD /clash-c/:subid":           "public: Clash subscription",
	"GET /mihomo/:subid":             "public: Mihomo alias",
	"HEAD /mihomo/:subid":            "public: Mihomo alias",
	"GET /clash-legacy/:subid":       "public: legacy Clash alias",
	"HEAD /clash-legacy/:subid":      "public: legacy Clash alias",
	"GET /assets/*filepath":          "public: subscription page assets",
	"HEAD /assets/*filepath":         "public: subscription page assets",
	"GET /sub-c/assets/*filepath":    "public: subscription page assets",
	"HEAD /sub-c/assets/*filepath":   "public: subscription page assets",
}

// Every route, with every optional format switched on, must be classified, and
// no classification may outlive its route.
func TestSubscriptionRouteContract(t *testing.T) {
	initSubDB(t)
	for key, value := range map[string]string{
		"subPath": "/sub-c/", "subJsonPath": "/json-c/", "subClashPath": "/clash-c/",
		"subJsonEnable": "true", "subClashEnable": "true",
	} {
		database.GetDB().Where("key = ?", key).Delete(&model.Setting{})
		if err := database.GetDB().Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	router, err := (&Server{}).initRouter()
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, r := range router.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	var unclassified, stale []string
	for r := range registered {
		if _, ok := subscriptionRoutes[r]; !ok {
			unclassified = append(unclassified, r)
		}
	}
	for r := range subscriptionRoutes {
		if !registered[r] {
			stale = append(stale, r)
		}
	}
	sort.Strings(unclassified)
	sort.Strings(stale)
	for _, r := range unclassified {
		t.Errorf("subscription route %s is not classified in subscriptionRoutes", r)
	}
	for _, r := range stale {
		t.Errorf("subscriptionRoutes classifies %s but the subscription server does not register it", r)
	}
}
