# netron-panel: upstream files touched

Rebase checklist for moving `feat/usage-multiplier` onto a new MHSanaei/3x-ui tag.
Feature code lives in new files; every upstream file below carries only a small
hook. After a rebase, re-apply or verify each hook, then run `make verify`.

Base: upstream tag `v3.11.0` (commit `c621ba90`).

## Upstream files modified

| File | Hook | Why |
| ---- | ---- | --- |
| `internal/database/model/model.go` | `Inbound.UsageMultiplier *float64` field (column `usage_multiplier`, default 1) | per-inbound multiplier; nil/NULL = x1 |
| `internal/database/db.go` | `&model.ClientInboundTraffic{}` in `allModels()` | AutoMigrate creates `client_inbound_traffics` |
| `internal/database/migrate_data.go` | `&model.ClientInboundTraffic{}` in `migrationModels()` | `migrate-db` copies the table across engines |
| `internal/web/service/setting.go` | `"usageMultiplierEnable": "false"` in `defaultValueMap` | kill switch, default OFF |
| `internal/web/entity/entity.go` | `AllSetting.UsageMultiplierEnable` | kill switch in the settings API |
| `internal/web/service/xray.go` | `RestartXray`: `s.latchUsageMultiplierMode(isForce)`; `GetXrayConfig`: client `entry` email via `usagemult.XrayEmail`, `RouterConfig = s.expandRoutingUserStatEmails(...)` after the inbound loop | stat emails in the generated config; template `user` rules keep matching |
| `internal/web/service/inbound_traffic.go` | `addClientTraffic`: `resolveStatEmailTraffic(tx, traffics)` just before `canonicalizeClientTraffic` | stat-email deltas become the client's weighted delta; quota/disable/expiry run unchanged |
| `internal/web/service/xray.go` (2) | `GetOnlineUsers` returns `foldStatEmailOnlineUsers(users)` | onlines, last-online bump and IP-limit see client emails; IPs merged across inbounds |
| `internal/web/job/xray_traffic_job.go` | `clientTraffics = j.inboundService.FoldStatEmailTraffic(clientTraffics)` right after `AddTraffic` | websocket speed, onlines and external traffic inform get client emails with real bytes |
| `internal/web/job/check_client_ip_job.go` | `disconnectClientTemporarily`: `xrayEmail := usagemult.XrayEmail(...)`, used for `RemoveUser` and the re-added user | the over-limit disconnect targets the identity the core holds |
| `internal/web/service/inbound_traffic_global.go` | `clearGlobalTraffic` ends with `return dropClientInboundTraffic(tx, emails...)` | the reset/delete chokepoint (single reset, bulk/periodic reset, auto-renew, client delete, node-side reset) also clears the per-inbound breakdown |
| `internal/web/service/inbound_traffic.go` (2) | `delClientStatsByEmails` and both branches of `DelDepletedClients` call `dropClientInboundTraffic` | delete paths that bypass `clearGlobalTraffic` |
| `internal/web/service/client_traffic.go` | `ResetAllTraffics`: delete all `ClientInboundTraffic` rows | reset-all bypasses the chokepoint |
| `internal/web/service/client_bulk.go` | `BulkDelete` (`!keepTraffic`): `dropClientInboundTraffic(tx, successEmails...)` | bulk delete bypasses the chokepoint |
| `internal/web/service/client_sync_orphan.go` | `ReapSyncOrphans`: `dropClientInboundTraffic(tx, batch...)` | orphan reaper bypasses the chokepoint |
| `internal/web/service/inbound.go` | `AddInbound`: `validateUsageMultiplier(...)` after the finalmask checks; `UpdateInbound`: `applyUsageMultiplierUpdate(oldInbound, inbound)` after `oldInbound.DisableFlow = ...` | 0..10 and protocol support on every write (REST, UI, import, node push); an update without the field keeps the stored value |
| `internal/web/service/inbound_node.go` | new central inbound: `UsageMultiplier: adoptedUsageMultiplier(snapIb)`; adopt `updates["usage_multiplier"]` only when non-nil | an old node's snapshot (no field) never zeroes or resets the master's multiplier |
| `internal/web/runtime/remote.go` | `wireInbound`: `setWireUsageMultiplier(v, ib)` before `return v` | master sends the multiplier with the other inbound fields |
| `internal/web/runtime/remote.go` (2) | `Remote` field `appliesUsageMult`; `recordCaps`: `r.appliesUsageMult = slices.Contains(caps, usagemult.NodeCap)` | master learns whether a node bills per inbound |
| `internal/web/middleware/config_envelope.go` | caps header value `usagemult.NodeCaps(wirecodec.NodeCaps)` | a node advertises `usage-mult` only while its running mode is ON |
| `internal/web/job/node_traffic_sync_job.go` | `syncOne`: `j.inboundService.WarnUsageMultiplierNode(n, rt)` after a successful snapshot | log warning (once per change) for a node counting weighted inbounds at x1 |
| `internal/web/controller/setting.go` | `g.POST("/usageMultiplierStatus", a.usageMultiplierStatus)` | status endpoint for the UI |
| `internal/web/node_contract_test.go` | `"UsageMultiplierApplied"` in `remoteMethodsOutsideContract` | accessor reads caps already received, no node call |
| `tools/openapigen/main.go` | `UsageMultiplierStatus`, `UsageMultiplierNodeStatus` in the service `StructAllow` | schemas/examples for the new endpoint |
| `frontend/src/pages/api-docs/endpoints.ts` | entry for `POST /panel/api/setting/usageMultiplierStatus` | route registry (pinned by `TestRouteRegistryContract`) |
| `README.md` | fork note crediting MHSanaei/3x-ui with the install command and stock-installer warning; Quick Start, platforms and Docker sections point at this fork | attribution (LICENSE unchanged), safe install |
| `internal/web/runtime/local.go` | `AddInbound` marshals `xrayInbound(ib)`; `AddUser` passes `xrayUser(ib, userMap)`; `RemoveUser` passes `xrayUserEmail(ib, email)` | hot add/remove/update reach Xray under the stat email |
| `internal/config/version` | `3.11.0-netron.1` | panel version = release tag without `v`; `release.yml` refuses a mismatching tag |
| `internal/util/version/version.go` | `parse` returns 4 parts; base/revision via `splitNetronRevision` (new `netron.go`) | `-netron.N` tags compare correctly in the update notifier and node version gates |
| `internal/web/service/panel/panel.go` | updater script and release API URLs built from `panelRepo` (new `netron_repo.go`) | the panel's update check and Update button install this fork, never stock 3x-ui |
| `install.sh`, `update.sh`, `x-ui.sh` | `xui_repo="mahdizo3181/netron-panel"` after the colour block; every `MHSanaei/3x-ui` URL uses `${xui_repo}`; amd64/arm64 guard in `install_x-ui` / `update_x-ui`; `x-ui.sh`: version regex accepts `-netron.N`, legacy-version prompt, uninstall hint uses `main` | one-command install/update from this fork |
| `deploy/cloud-init/cloud-init.yaml` | installer URL | cloud-init installs this fork |
| `.github/workflows/release.yml` | tags `v*.*.*-netron.*` + dispatch only; matrix amd64/arm64; tag-vs-version check; `npm run gen:zod` before the frontend build; per-platform upload replaced by one `publish` job (`gh release`, latest vs pre-release); Windows and dev-channel jobs guarded upstream-only | fork releases |
| `.github/workflows/docker.yml`, `docs-deploy.yml`, `mutation.yml`, `cleanup_caches.yml`, `claude-issue-analyst.yml`, `claude-pr-review.yml` | job `if: github.repository == 'MHSanaei/3x-ui'` | upstream-only jobs (image push, Pages deploy, nightly jobs, Claude bots) never run on the fork |

## Upstream files modified: info config link type

See `docs/INFO_CONFIG.md`. Builders live in `internal/sub/info_link.go`; the
upstream files only pick the link type and call them.

| File | Hook | Why |
| ---- | ---- | --- |
| `internal/sub/service.go` | `SubService.subInfoNodeLinkType string` field; `loadRemarkSettings`: `s.subInfoNodeLinkType, _ = s.settingService.GetSubInfoNodeLinkType()`; `getSubs`: `dummyLink := infoNodeLink(s.subInfoNodeLinkType, remark)` (was the inline `socks://127.0.0.1:1080#…` `Sprintf`) | raw/base64 info entry is VLESS by default; Happ rejects SOCKS |
| `internal/sub/json_service.go` | `genDummySocksConfig(remark)` renamed `genDummyInfoConfig(outbound, remark)` with the SOCKS outbound literal moved to `infoNodeOutbound`; call site `s.genDummyInfoConfig(infoNodeOutbound(subReq.subInfoNodeLinkType), remark)` | JSON info document uses the same link type |
| `internal/sub/clash_service.go` | none (dummy stays `socks5`, `isDummyProxy` unchanged) | every Clash core reads socks5; the original Clash core has no VLESS |
| `internal/web/service/setting.go` | `"subInfoNodeLinkType": SubInfoNodeLinkVless` in `defaultValueMap`; `UpdateAllSetting`: `normalizeSubInfoNodeLinkType(...)` right after the host-sort-mode switch | default `vless` for new and existing installs; reject anything but vless/socks |
| `internal/web/entity/entity.go` | `AllSetting.SubInfoNodeLinkType` (`example:"vless"`) after `SubInfoNodeEnable` | setting in the settings API and OpenAPI |
| `frontend/src/schemas/setting.ts` | `SubInfoNodeLinkTypeSchema` (`vless`/`socks`); `subInfoNodeLinkType` in `AllSettingSchema` | settings form validation |
| `frontend/src/models/setting.ts` | `subInfoNodeLinkType: SubInfoNodeLinkType = 'vless'` | form default |
| `frontend/src/pages/settings/SubscriptionGeneralTab.tsx` | `Select` `sub-info-node-link-type` after the info config switch | the setting in the UI |
| `internal/web/translation/*.json` (14) | `subInfoNodeLinkType`, `…Desc`, `subInfoNodeLinkVless`, `subInfoNodeLinkSocks` after `subInfoNodeEnableDesc` (fa-IR translated, others English); `subInfoNodeEnableDesc` no longer says SOCKS in any locale | i18n key parity test |
| `internal/sub/service_info_node_test.go`, `json_info_node_test.go`, `host_order_test.go`, `frontend/src/test/setting-sub-info-node.test.ts` | info-entry assertions expect the VLESS link (`assertInfoVlessLink`) | default changed |
| `frontend/src/generated/*`, `frontend/public/openapi.json`, `docs/public/openapi.json` | regenerated: `subInfoNodeLinkType` | `make gen` / `docs-openapi-check` |

## New files (no rebase conflict expected)

- `docs/MULTIPLIER.md`, `docs/NETRON_UPSTREAM_CHANGES.md`, `docs/INFO_CONFIG.md`
- `internal/sub/info_link.go` (+ `info_link_test.go`): info entry link/outbound builders
- `internal/web/service/sub_info_link_setting.go` (+ test): link type constants, getter, validation
- `internal/util/version/netron.go` (+ test), `internal/web/service/panel/netron_repo.go`, `netron_update_test.go`
- `internal/database/model/usage_multiplier.go` (+ test)
- `internal/database/usage_multiplier_migration_test.go`, `usage_multiplier_postgres_test.go`
- `internal/web/service/usage_multiplier_postgres_test.go` (run: `XUI_DB_TYPE=postgres XUI_DB_DSN=... go test ./internal/web/service -run ^TestUsageMultiplierPostgres$`)
- `internal/usagemult/` (stat email, protocol support, mode latch)
- `internal/web/service/usage_multiplier_setting.go`, `usage_multiplier_config.go`, `usage_multiplier_traffic.go` (+ tests)
- `internal/web/runtime/usage_multiplier.go` (+ in-process Xray test)
- `internal/web/service/usage_multiplier_fold.go` (+ test)
- `internal/web/service/usage_multiplier_reset_test.go`
- `internal/web/service/usage_multiplier_inbound.go`, `usage_multiplier_node_test.go`
- `internal/web/controller/usage_multiplier.go` (+ test)
- `internal/web/service/usage_multiplier_status.go` (+ test)
- `internal/web/runtime/usage_multiplier_caps_test.go`
- `internal/web/job/usage_multiplier_disconnect_test.go`
- `internal/xray/xraytest/` (in-process Xray core for tests)
- `internal/nodee2e/usage_multiplier_test.go` (real master + node; `XUI_NODE_E2E_OLD_BINARY` = a stock v3.11.0 build for the old-node case)

## Deliberately not hooked

- Inbound deletion keeps that inbound's `client_inbound_traffics` rows: the bytes
  are already in the client's total, and inbound ids are never reused
  (SQLite AUTOINCREMENT, Postgres serial). A client reset or delete clears them.
- `inbound_node.go` central `client_traffics` deletes for node inbounds: per-inbound
  rows exist only for local inbounds; the client's own delete path clears them.

## After every rebase

No upstream download URL may come back (the Go module path `.../3x-ui/v3` is excluded). This must print nothing:

```
grep -rniP '(github\.com|githubusercontent\.com|api\.github\.com/repos)/mhsanaei/3x-ui(?!/v3)' \
  install.sh update.sh x-ui.sh deploy/ internal/web/service/panel/ .github/workflows/release.yml
```

Then `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -shellcheck=` and
`shellcheck --severity=error install.sh update.sh x-ui.sh`.
