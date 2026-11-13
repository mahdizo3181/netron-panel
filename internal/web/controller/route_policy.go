package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// RoutePolicy says who may reach a panel route (docs/RESELLERS.md §3.2).
type RoutePolicy int

const (
	// PolicyAdminOnly is also the answer for a route missing from the table.
	PolicyAdminOnly RoutePolicy = iota
	PolicyPublic
	PolicyReseller
)

// RoutePolicies classifies every route on the panel engine, keyed "METHOD /path"
// relative to the base path; TestRoutePolicyContract keeps it in step with the router.
var RoutePolicies = map[string]RoutePolicy{
	// Public: no login needed, the same answer for everyone.
	"GET /": PolicyPublic,
	"GET /.well-known/appspecific/com.chrome.devtools.json": PolicyPublic,
	"GET /assets/*filepath":                                 PolicyPublic,
	"GET /csrf-token":                                       PolicyPublic,
	"GET /icons/:name":                                      PolicyPublic,
	"GET /manifest.webmanifest":                             PolicyPublic,
	"GET /pwa-register.js":                                  PolicyPublic,
	"GET /service-worker.js":                                PolicyPublic,
	"GET /sponsors":                                         PolicyPublic,
	"GET /sponsors/logo/:name":                              PolicyPublic,
	"HEAD /assets/*filepath":                                PolicyPublic,
	"POST /getTwoFactorEnable":                              PolicyPublic,
	"POST /login":                                           PolicyPublic,
	"POST /logout":                                          PolicyPublic,
	// SPA shell pages (checkLogin): markup only, the data comes from the API below.
	"GET /panel/":           PolicyReseller,
	"GET /panel/api-docs":   PolicyReseller,
	"GET /panel/clients":    PolicyReseller,
	"GET /panel/csrf-token": PolicyReseller,
	"GET /panel/groups":     PolicyReseller,
	"GET /panel/inbounds":   PolicyReseller,
	"GET /panel/nodes":      PolicyReseller,
	"GET /panel/outbound":   PolicyReseller,
	"GET /panel/routing":    PolicyReseller,
	"GET /panel/settings":   PolicyReseller,
	"GET /panel/xray":       PolicyReseller,
	// Reseller self routes.
	"GET /panel/api/reseller/me":               PolicyReseller,
	"POST /panel/api/reseller/me/totp":         PolicyReseller,
	"POST /panel/api/reseller/me/totp/disable": PolicyReseller,
	// Reseller client routes: each handler passes actorScope to a *For service method.
	"GET /panel/api/clients/get/:email":           PolicyReseller,
	"GET /panel/api/clients/list/paged":           PolicyReseller,
	"GET /panel/api/inbounds/options":             PolicyReseller,
	"POST /panel/api/clients/add":                 PolicyReseller,
	"POST /panel/api/clients/bulkCreate":          PolicyReseller,
	"POST /panel/api/clients/bulkDisable":         PolicyReseller,
	"POST /panel/api/clients/bulkEnable":          PolicyReseller,
	"POST /panel/api/clients/del/:email":          PolicyReseller,
	"POST /panel/api/clients/resetTraffic/:email": PolicyReseller,
	"POST /panel/api/clients/update/:email":       PolicyReseller,

	// Admin only: the WebSocket and every other API route (resellers get 403).
	"POST /panel/api/clients/:email/owner":                      PolicyAdminOnly,
	"DELETE /panel/api/clients/hwids/:email":                    PolicyAdminOnly,
	"DELETE /panel/api/clients/hwids/:email/:id":                PolicyAdminOnly,
	"DELETE /panel/api/sub-balancers/:id":                       PolicyAdminOnly,
	"DELETE /panel/api/xray/outbound-subs/:id":                  PolicyAdminOnly,
	"GET /panel/api/clients/export":                             PolicyAdminOnly,
	"GET /panel/api/clients/get/subId/:subId":                   PolicyAdminOnly,
	"GET /panel/api/clients/get/tgId/:tgId":                     PolicyAdminOnly,
	"GET /panel/api/clients/groups":                             PolicyAdminOnly,
	"GET /panel/api/clients/groups/:name/emails":                PolicyAdminOnly,
	"GET /panel/api/clients/links/:email":                       PolicyAdminOnly,
	"GET /panel/api/clients/list":                               PolicyAdminOnly,
	"GET /panel/api/clients/subLinks/:subId":                    PolicyAdminOnly,
	"GET /panel/api/clients/traffic/:email":                     PolicyAdminOnly,
	"GET /panel/api/hosts/byInbound/:inboundId":                 PolicyAdminOnly,
	"GET /panel/api/hosts/get/:groupId":                         PolicyAdminOnly,
	"GET /panel/api/hosts/list":                                 PolicyAdminOnly,
	"GET /panel/api/hosts/raw":                                  PolicyAdminOnly,
	"GET /panel/api/hosts/tags":                                 PolicyAdminOnly,
	"GET /panel/api/inbounds/:id/fallbacks":                     PolicyAdminOnly,
	"GET /panel/api/inbounds/allLinks":                          PolicyAdminOnly,
	"GET /panel/api/inbounds/amneziawgKernelReadiness":          PolicyAdminOnly,
	"GET /panel/api/inbounds/get/:id":                           PolicyAdminOnly,
	"GET /panel/api/inbounds/list":                              PolicyAdminOnly,
	"GET /panel/api/inbounds/list/slim":                         PolicyAdminOnly,
	"GET /panel/api/nodes/get/:id":                              PolicyAdminOnly,
	"GET /panel/api/nodes/history/:id/:metric/:bucket":          PolicyAdminOnly,
	"GET /panel/api/nodes/list":                                 PolicyAdminOnly,
	"GET /panel/api/nodes/webCert/:id":                          PolicyAdminOnly,
	"GET /panel/api/openapi.json":                               PolicyAdminOnly,
	"GET /panel/api/resellers":                                  PolicyAdminOnly,
	"GET /panel/api/resellers/:id":                              PolicyAdminOnly,
	"GET /panel/api/server/clientIps":                           PolicyAdminOnly,
	"GET /panel/api/server/cpuHistory/:bucket":                  PolicyAdminOnly,
	"GET /panel/api/server/descendants":                         PolicyAdminOnly,
	"GET /panel/api/server/fail2banStatus":                      PolicyAdminOnly,
	"GET /panel/api/server/getConfigJson":                       PolicyAdminOnly,
	"GET /panel/api/server/getDb":                               PolicyAdminOnly,
	"GET /panel/api/server/getMigration":                        PolicyAdminOnly,
	"GET /panel/api/server/getNewUUID":                          PolicyAdminOnly,
	"GET /panel/api/server/getNewVlessEnc":                      PolicyAdminOnly,
	"GET /panel/api/server/getNewX25519Cert":                    PolicyAdminOnly,
	"GET /panel/api/server/getNewmldsa65":                       PolicyAdminOnly,
	"GET /panel/api/server/getNewmlkem768":                      PolicyAdminOnly,
	"GET /panel/api/server/getPanelUpdateInfo":                  PolicyAdminOnly,
	"GET /panel/api/server/getUpdateStatus":                     PolicyAdminOnly,
	"GET /panel/api/server/getWebCertFiles":                     PolicyAdminOnly,
	"GET /panel/api/server/getXrayVersion":                      PolicyAdminOnly,
	"GET /panel/api/server/history/:metric/:bucket":             PolicyAdminOnly,
	"GET /panel/api/server/status":                              PolicyAdminOnly,
	"GET /panel/api/server/xrayMetricsHistory/:metric/:bucket":  PolicyAdminOnly,
	"GET /panel/api/server/xrayMetricsState":                    PolicyAdminOnly,
	"GET /panel/api/server/xrayObservatory":                     PolicyAdminOnly,
	"GET /panel/api/server/xrayObservatoryHistory/:tag/:bucket": PolicyAdminOnly,
	"GET /panel/api/setting/apiTokens":                          PolicyAdminOnly,
	"GET /panel/api/setting/getDefaultJsonConfig":               PolicyAdminOnly,
	"GET /panel/api/sub-balancers":                              PolicyAdminOnly,
	"GET /panel/api/xray/geodata/categories":                    PolicyAdminOnly,
	"GET /panel/api/xray/geodata/entries":                       PolicyAdminOnly,
	"GET /panel/api/xray/geodata/files":                         PolicyAdminOnly,
	"GET /panel/api/xray/getDefaultJsonConfig":                  PolicyAdminOnly,
	"GET /panel/api/xray/getOutboundsTraffic":                   PolicyAdminOnly,
	"GET /panel/api/xray/getXrayResult":                         PolicyAdminOnly,
	"GET /panel/api/xray/outbound-subs":                         PolicyAdminOnly,
	"GET /ws":                                                   PolicyAdminOnly,
	"POST /panel/api/backuptotgbot":                             PolicyAdminOnly,
	"POST /panel/api/clients/:email/attach":                     PolicyAdminOnly,
	"POST /panel/api/clients/:email/detach":                     PolicyAdminOnly,
	"POST /panel/api/clients/:email/externalLinks":              PolicyAdminOnly,
	"POST /panel/api/clients/activeInbounds":                    PolicyAdminOnly,
	"POST /panel/api/clients/bulkAdjust":                        PolicyAdminOnly,
	"POST /panel/api/clients/bulkAttach":                        PolicyAdminOnly,
	"POST /panel/api/clients/bulkDel":                           PolicyAdminOnly,
	"POST /panel/api/clients/bulkDetach":                        PolicyAdminOnly,
	"POST /panel/api/clients/bulkResetTraffic":                  PolicyAdminOnly,
	"POST /panel/api/clients/clearIps/:email":                   PolicyAdminOnly,
	"POST /panel/api/clients/clientIpsByGuid":                   PolicyAdminOnly,
	"POST /panel/api/clients/delDepleted":                       PolicyAdminOnly,
	"POST /panel/api/clients/delOrphans":                        PolicyAdminOnly,
	"POST /panel/api/clients/groups/bulkAdd":                    PolicyAdminOnly,
	"POST /panel/api/clients/groups/bulkRemove":                 PolicyAdminOnly,
	"POST /panel/api/clients/groups/create":                     PolicyAdminOnly,
	"POST /panel/api/clients/groups/delete":                     PolicyAdminOnly,
	"POST /panel/api/clients/groups/rename":                     PolicyAdminOnly,
	"POST /panel/api/clients/groups/resetTraffic":               PolicyAdminOnly,
	"POST /panel/api/clients/happLink/:id":                      PolicyAdminOnly,
	"POST /panel/api/clients/hwids/:email":                      PolicyAdminOnly,
	"POST /panel/api/clients/import":                            PolicyAdminOnly,
	"POST /panel/api/clients/ips/:email":                        PolicyAdminOnly,
	"POST /panel/api/clients/lastOnline":                        PolicyAdminOnly,
	"POST /panel/api/clients/onlines":                           PolicyAdminOnly,
	"POST /panel/api/clients/onlinesByGuid":                     PolicyAdminOnly,
	"POST /panel/api/clients/renewalPreview":                    PolicyAdminOnly,
	"POST /panel/api/clients/resetAllTraffics":                  PolicyAdminOnly,
	"POST /panel/api/clients/updateTraffic/:email":              PolicyAdminOnly,
	"POST /panel/api/hosts/add":                                 PolicyAdminOnly,
	"POST /panel/api/hosts/bulk/add":                            PolicyAdminOnly,
	"POST /panel/api/hosts/bulk/del":                            PolicyAdminOnly,
	"POST /panel/api/hosts/bulk/setEnable":                      PolicyAdminOnly,
	"POST /panel/api/hosts/del/:groupId":                        PolicyAdminOnly,
	"POST /panel/api/hosts/reorder":                             PolicyAdminOnly,
	"POST /panel/api/hosts/setEnable/:groupId":                  PolicyAdminOnly,
	"POST /panel/api/hosts/update/:groupId":                     PolicyAdminOnly,
	"POST /panel/api/inbounds/:id/delAllClients":                PolicyAdminOnly,
	"POST /panel/api/inbounds/:id/fallbacks":                    PolicyAdminOnly,
	"POST /panel/api/inbounds/:id/resetTraffic":                 PolicyAdminOnly,
	"POST /panel/api/inbounds/:id/subSortIndex":                 PolicyAdminOnly,
	"POST /panel/api/inbounds/add":                              PolicyAdminOnly,
	"POST /panel/api/inbounds/bulkDel":                          PolicyAdminOnly,
	"POST /panel/api/inbounds/del/:id":                          PolicyAdminOnly,
	"POST /panel/api/inbounds/import":                           PolicyAdminOnly,
	"POST /panel/api/inbounds/pushClientTraffics":               PolicyAdminOnly,
	"POST /panel/api/inbounds/resetAllTraffics":                 PolicyAdminOnly,
	"POST /panel/api/inbounds/setEnable/:id":                    PolicyAdminOnly,
	"POST /panel/api/inbounds/update/:id":                       PolicyAdminOnly,
	"POST /panel/api/nodes/add":                                 PolicyAdminOnly,
	"POST /panel/api/nodes/certFingerprint":                     PolicyAdminOnly,
	"POST /panel/api/nodes/del/:id":                             PolicyAdminOnly,
	"POST /panel/api/nodes/inbounds":                            PolicyAdminOnly,
	"POST /panel/api/nodes/mtls/ca":                             PolicyAdminOnly,
	"POST /panel/api/nodes/mtls/reloadClient":                   PolicyAdminOnly,
	"POST /panel/api/nodes/mtls/trustCA":                        PolicyAdminOnly,
	"POST /panel/api/nodes/probe/:id":                           PolicyAdminOnly,
	"POST /panel/api/nodes/setEnable/:id":                       PolicyAdminOnly,
	"POST /panel/api/nodes/test":                                PolicyAdminOnly,
	"POST /panel/api/nodes/update/:id":                          PolicyAdminOnly,
	"POST /panel/api/nodes/updatePanel":                         PolicyAdminOnly,
	"POST /panel/api/resellers":                                 PolicyAdminOnly,
	"POST /panel/api/resellers/:id":                             PolicyAdminOnly,
	"POST /panel/api/resellers/:id/del":                         PolicyAdminOnly,
	"POST /panel/api/resellers/:id/password":                    PolicyAdminOnly,
	"POST /panel/api/resellers/:id/resetTotp":                   PolicyAdminOnly,
	"POST /panel/api/server/amneziawglogs/:count":               PolicyAdminOnly,
	"POST /panel/api/server/clientIps":                          PolicyAdminOnly,
	"POST /panel/api/server/getCertHash":                        PolicyAdminOnly,
	"POST /panel/api/server/getNewEchCert":                      PolicyAdminOnly,
	"POST /panel/api/server/getRemoteCertHash":                  PolicyAdminOnly,
	"POST /panel/api/server/importDB":                           PolicyAdminOnly,
	"POST /panel/api/server/installXray/:version":               PolicyAdminOnly,
	"POST /panel/api/server/logs/:count":                        PolicyAdminOnly,
	"POST /panel/api/server/restartXrayService":                 PolicyAdminOnly,
	"POST /panel/api/server/scanRealityTarget":                  PolicyAdminOnly,
	"POST /panel/api/server/scanRealityTargets":                 PolicyAdminOnly,
	"POST /panel/api/server/setUpdateChannel":                   PolicyAdminOnly,
	"POST /panel/api/server/stopXrayService":                    PolicyAdminOnly,
	"POST /panel/api/server/updateGeofile":                      PolicyAdminOnly,
	"POST /panel/api/server/updateGeofile/:fileName":            PolicyAdminOnly,
	"POST /panel/api/server/updatePanel":                        PolicyAdminOnly,
	"POST /panel/api/server/xraylogs/:count":                    PolicyAdminOnly,
	"POST /panel/api/setting/all":                               PolicyAdminOnly,
	"POST /panel/api/setting/apiTokens/create":                  PolicyAdminOnly,
	"POST /panel/api/setting/apiTokens/delete/:id":              PolicyAdminOnly,
	"POST /panel/api/setting/apiTokens/setEnabled/:id":          PolicyAdminOnly,
	"POST /panel/api/setting/defaultSettings":                   PolicyAdminOnly,
	"POST /panel/api/setting/factoryDefaults":                   PolicyAdminOnly,
	"POST /panel/api/setting/restartPanel":                      PolicyAdminOnly,
	"POST /panel/api/setting/testDiscord":                       PolicyAdminOnly,
	"POST /panel/api/setting/testSmtp":                          PolicyAdminOnly,
	"POST /panel/api/setting/testTgBot":                         PolicyAdminOnly,
	"POST /panel/api/setting/tgBotCapabilities":                 PolicyAdminOnly,
	"POST /panel/api/setting/update":                            PolicyAdminOnly,
	"POST /panel/api/setting/updateUser":                        PolicyAdminOnly,
	"POST /panel/api/setting/usageMultiplierStatus":             PolicyAdminOnly,
	"POST /panel/api/setting/validateRegex":                     PolicyAdminOnly,
	"POST /panel/api/sub-balancers":                             PolicyAdminOnly,
	"POST /panel/api/sub-balancers/:id":                         PolicyAdminOnly,
	"POST /panel/api/sub-balancers/:id/del":                     PolicyAdminOnly,
	"POST /panel/api/xray/":                                     PolicyAdminOnly,
	"POST /panel/api/xray/balancerOverride":                     PolicyAdminOnly,
	"POST /panel/api/xray/balancerStatus":                       PolicyAdminOnly,
	"POST /panel/api/xray/geodata/validate":                     PolicyAdminOnly,
	"POST /panel/api/xray/nord/:action":                         PolicyAdminOnly,
	"POST /panel/api/xray/outbound-subs":                        PolicyAdminOnly,
	"POST /panel/api/xray/outbound-subs/:id":                    PolicyAdminOnly,
	"POST /panel/api/xray/outbound-subs/:id/del":                PolicyAdminOnly,
	"POST /panel/api/xray/outbound-subs/:id/move":               PolicyAdminOnly,
	"POST /panel/api/xray/outbound-subs/:id/refresh":            PolicyAdminOnly,
	"POST /panel/api/xray/outbound-subs/parse":                  PolicyAdminOnly,
	"POST /panel/api/xray/pia/:action":                          PolicyAdminOnly,
	"POST /panel/api/xray/resetOutboundsTraffic":                PolicyAdminOnly,
	"POST /panel/api/xray/routeTest":                            PolicyAdminOnly,
	"POST /panel/api/xray/testOutbound":                         PolicyAdminOnly,
	"POST /panel/api/xray/testOutbounds":                        PolicyAdminOnly,
	"POST /panel/api/xray/update":                               PolicyAdminOnly,
	"POST /panel/api/xray/warp/:action":                         PolicyAdminOnly,
}

// RoutePolicyOf looks a route up; unknown routes are admin-only.
func RoutePolicyOf(method, path string) RoutePolicy {
	if p, ok := RoutePolicies[method+" "+path]; ok {
		return p
	}
	return PolicyAdminOnly
}

const resellerActorKey = "reseller_actor_id"

// actorResellerID is the reseller behind this request, set by checkAPIAuth.
func actorResellerID(c *gin.Context) (int, bool) {
	id, ok := c.Get(resellerActorKey)
	if !ok {
		return 0, false
	}
	n, ok := id.(int)
	return n, ok
}

func routePolicyFor(c *gin.Context) RoutePolicy {
	rel := "/" + strings.TrimPrefix(c.FullPath(), c.GetString("base_path"))
	return RoutePolicyOf(c.Request.Method, rel)
}

// enforceActorPolicy is the single gate for resellers: deny unless the table says reseller.
func enforceActorPolicy(c *gin.Context) {
	if _, isReseller := actorResellerID(c); !isReseller || routePolicyFor(c) == PolicyReseller {
		c.Next()
		return
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"success": false,
		"msg":     I18nWeb(c, "pages.resellers.toasts.forbidden"),
	})
}

// actorScope is the service Scope for this request's actor.
func actorScope(c *gin.Context) service.Scope {
	if id, ok := actorResellerID(c); ok {
		return service.ResellerScope(id)
	}
	return service.AdminScope()
}

// jsonObjFor answers like jsonObj; a reseller's copy goes through StripForReseller.
func jsonObjFor(c *gin.Context, obj any) {
	if _, isReseller := actorResellerID(c); !isReseller {
		jsonObj(c, obj, nil)
		return
	}
	stripped, err := service.StripForReseller(obj)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	jsonObj(c, stripped, nil)
}
