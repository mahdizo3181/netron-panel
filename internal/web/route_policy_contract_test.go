package web

import (
	"sort"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/controller"
)

// Deny-by-default only stays safe while someone decides each new route: an
// unclassified route fails here, and so does a policy entry for a removed one.
func TestRoutePolicyContract(t *testing.T) {
	registered := registeredContractRoutes(t)

	var unclassified, stale []string
	for route := range registered {
		if _, ok := controller.RoutePolicies[route]; !ok {
			unclassified = append(unclassified, route)
		}
	}
	for route := range controller.RoutePolicies {
		if !registered[route] {
			stale = append(stale, route)
		}
	}
	sort.Strings(unclassified)
	sort.Strings(stale)
	for _, r := range unclassified {
		t.Errorf("route %s has no entry in controller.RoutePolicies: classify it (admin-only, public or reseller)", r)
	}
	for _, r := range stale {
		t.Errorf("controller.RoutePolicies classifies %s but the server does not register it: remove the entry", r)
	}
	for route, p := range controller.RoutePolicies {
		if p == controller.PolicyReseller && !strings.HasPrefix(strings.Fields(route)[1], "/panel/") {
			t.Errorf("%s is reachable by resellers outside /panel/: resellers only get the SPA shell and /panel/api routes", route)
		}
	}
}
