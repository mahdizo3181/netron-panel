# Resellers ("نمایندگی"): design

Status: **approved; stages A to D implemented** (see §11). File and function
names below are the ones in this repository at `main` = `c97d9f41` (v3.11.0-netron.7); verify
them again before each stage, because upstream rebases move code.

A reseller is a second kind of login. It manages only its own clients, on the inbounds the admin
allowed, inside a volume pool the admin set. The existing admin keeps every power it has today.

Reference model: the PasarGuard v5 RBAC ideas listed in the requirements (own/all scopes, quotas
enforced in bulk creation too, limited admins cannot create unlimited users, an account cannot
edit its own limits). Only the ideas are used; no code is copied.

---

## 0. Decisions in one page

| Topic               | Decision                                                                                                                         |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Account storage     | New table `resellers` with its own credentials. **Not** rows in `users`.                                                         |
| Why not `users`     | A stock 3x-ui binary (rollback) logs any `users` row in as full admin. An unknown table is ignored.                              |
| Ownership           | `clients.reseller_id` (nullable). NULL = the admin's client.                                                                     |
| Allowed inbounds    | Join table `reseller_inbounds`.                                                                                                  |
| Pool usage          | `resellers.used_traffic`, a running counter fed by the two places that add to `client_traffics`. Never derived from client rows. |
| Cut-off marker      | `clients.pool_disabled` (bool). Only this flag decides who is re-enabled after a top-up.                                         |
| Authorization       | One route policy table (deny by default for resellers) + one `Scope` type in the service layer.                                  |
| Resellers exist on  | The master panel only. Nodes never learn about resellers.                                                                        |
| Subscription server | Unchanged.                                                                                                                       |

---

## 1. Data model

### 1.1 New table `resellers` (model `model.Reseller`, new file `internal/database/model/reseller.go`)

| Column                     | Type                   | Meaning                                                                                                                                                                                        |
| -------------------------- | ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                       | int PK                 |                                                                                                                                                                                                |
| `username`                 | text, unique, not null | Login name. Must also not collide with any `users.username` (checked in the service, both directions).                                                                                         |
| `password`                 | text                   | bcrypt, via `crypto.HashPasswordAsBcrypt` (same helper as `UserService.UpdateUser`). `json:"-"`.                                                                                               |
| `login_epoch`              | int64 default 0        | Same role as `User.LoginEpoch`: bumping it invalidates the reseller's sessions.                                                                                                                |
| `totp_secret`              | text default ''        | Optional per-reseller 2FA secret. `json:"-"`.                                                                                                                                                  |
| `name`                     | text                   | Display name (the owner badge).                                                                                                                                                                |
| `enable`                   | bool default true      | Admin switch. OFF = cannot log in; its clients are cut off (§4.5).                                                                                                                             |
| `pool_bytes`               | int64 default 0        | Total volume pool. 0 = no pool limit.                                                                                                                                                          |
| `used_traffic`             | int64 default 0        | Weighted bytes billed to this reseller's clients since the last admin reset.                                                                                                                   |
| `max_clients`              | int default 0          | 0 = unlimited.                                                                                                                                                                                 |
| `expiry_time`              | int64 default 0        | Unix ms. 0 = never.                                                                                                                                                                            |
| `require_client_quota`     | bool default false     | When true the reseller must give every client a `totalGB` > 0 (PasarGuard's "limited admins cannot create unlimited users"). Default false, per the requirement that `totalGB = 0` is allowed. |
| `cutoff_reason`            | text default ''        | `''`, `pool`, `expired` or `disabled`. Set by the cut-off, cleared by the re-enable. Drives the UI state and makes the cut-off idempotent.                                                     |
| `notified_level`           | int default 0          | 0, 80 or 100. Last Telegram notice sent; reset when usage drops below the level.                                                                                                               |
| `created_at`, `updated_at` | int64 ms               |                                                                                                                                                                                                |

Plain `int64` byte counters, like `client_traffics.up/down`, incremented with
`database.ClampedAddExpr` so they cannot overflow.

### 1.2 New table `reseller_inbounds` (`model.ResellerInbound`)

`reseller_id`, `inbound_id`, composite primary key, index on `inbound_id`. Deleting an inbound
deletes its rows (in `InboundService.DelInbound`'s transaction; SQLite has no enforced FK here, so
it is explicit code, like `client_inbounds`).

### 1.3 New table `reseller_audit_logs` (`model.ResellerAuditLog`) — see §8.3

### 1.4 Changes to `clients` (`model.ClientRecord`, `internal/database/model/model.go:981`)

| Column          | Type                                       | Meaning                                                                          |
| --------------- | ------------------------------------------ | -------------------------------------------------------------------------------- |
| `reseller_id`   | nullable int, index `idx_clients_reseller` | Owner. NULL = admin. Go field `ResellerID *int` with `json:"-"`.                 |
| `pool_disabled` | bool default false                         | True only while this client is disabled **by the reseller cut-off**. `json:"-"`. |

`json:"-"` on both is the first half of the mass-assignment guard: no existing endpoint can
receive or leak them. The owner is exposed through a separate read-only field (§5.3).

### 1.5 `api_tokens` (stage C, optional)

`reseller_id` nullable int. Only meaningful with the new scope `reseller` (§5.4).

### 1.6 Migration

- Add `&model.Reseller{}`, `&model.ResellerInbound{}`, `&model.ResellerAuditLog{}` to
  **both** `allModels()` (`internal/database/db.go`) and `migrationModels()`
  (`internal/database/migrate_data.go`). `TestMigrationModelsMatchPanelModels` fails if they
  drift. In `migrationModels()` they go after `Inbound` and `ClientRecord` (FK-aware order).
- The new `clients` columns are added by AutoMigrate. Existing clients get `reseller_id = NULL`
  (admin's) and `pool_disabled = false`. No data migration, no seeder.
- SQLite vs PostgreSQL: only portable types (int, bigint, text, bool). Every raw SQL in this
  design uses helpers from `internal/database/dialect.go` (`ClampedAddExpr`). The Postgres CI job
  must run the new tests (`XUI_TEST_PG_DSN`), because a skipped Postgres test counts as a failure
  there.
- Rollback to an older netron build or to stock 3x-ui: the extra tables and columns are ignored.
  Reseller clients then look like ordinary admin clients; nothing is lost and nobody gains access.
  Clients that were cut off stay disabled (`enable = false`) and must be re-enabled by hand.

---

## 2. Authentication and sessions

### 2.1 Today

- One admin row in `users` (`model.User`: `Id`, `Username`, `Password`, `LoginEpoch`).
- `POST /login` (`controller/index.go` `login`) → `defaultLoginLimiter` (per IP + username) →
  `UserService.CheckUser` (`service/panel/user.go:52`): bcrypt, optional LDAP fallback, then the
  **global** 2FA setting (`twoFactorEnable` / `twoFactorToken`).
- `session.SetLoginUser` stores `LOGIN_USER` (id) and `LOGIN_EPOCH` in the `3x-ui` cookie session.
  `session.GetLoginUser` reloads the user and drops the session when the epoch differs.
- API tokens and mTLS: `APIController.checkAPIAuth` sets the first user as the acting user and a
  token scope; `enforceTokenScope` applies the `monitor` / `node-sync` / `node-admin` allowlists.
- Several places assume "the first user is the admin": `UserService.GetFirstUser`,
  `HasDefaultCredential`, `UpdateFirstUser` (CLI), `inbound_node.go:636`.
  `BumpLoginEpoch` bumps every row. Keeping resellers out of `users` leaves all of these correct.

### 2.2 Reseller login

- Same login page and the same `POST /login`. Order: admin first (unchanged code path); only if
  the username is not a `users` row, try `ResellerAuthService.Check(username, password, code)`.
- A username can never exist in both tables, so the order cannot change who logs in.
- Reseller check: bcrypt only. **No LDAP fallback** (LDAP would let a directory account that
  happens to share a reseller's username in). `enable = false` or `expiry_time` passed → the same
  generic "wrong username or password" message (no account enumeration); the real reason goes to
  the log and the audit log.
- 2FA: the global 2FA setting stays the admin's. A reseller with a non-empty `totp_secret` must
  send a valid code (same `totp.VerifyWithSkew`). `getTwoFactorEnable` is public and today returns
  the global flag; the login page keeps showing the code field when the global flag is on, and a
  reseller whose own 2FA is on but who sent no code gets a specific "2FA code required" answer
  only **after** the password was correct.
- Login limiter: the existing `defaultLoginLimiter` runs before the lookup, so resellers are
  covered with no change. Login notifications (`tgbot.UserLoginNotify`) fire as today.
- Session: new keys `LOGIN_RESELLER` (id) and `RESELLER_EPOCH`, set by a new
  `session.SetLoginReseller`. A session holds an admin **or** a reseller, never both: setting one
  deletes the other. `session.IsLogin` stays "admin logged in" so every existing caller keeps its
  meaning; a new `session.GetActor(c)` returns the acting identity (§3.1).
- Sessions die when: the admin disables/deletes the reseller or changes its password (epoch bump),
  or the reseller expires (checked on every request in the actor middleware, not only at login).

### 2.3 Password policy

There is no password policy in the code today beyond "not empty". For resellers: minimum 10
characters, not equal to the username, set by the admin on create, changeable by the reseller for
itself (`POST /panel/api/reseller/me/password`, requires the current password, bumps the epoch).
Admin login and admin password rules are unchanged.

---

## 3. Authorization

### 3.1 One actor, resolved once

New middleware `resolveActor` in `APIController.initRouter` (`controller/api.go`), right after
`checkAPIAuth`. It puts one value in the Gin context:

```
Actor{ Kind: admin | reseller | token, ResellerID int, TokenScope string }
```

- Session admin, `admin` token, mTLS → `admin` / `token` exactly as today.
- Session reseller or `reseller` token → `reseller` with its id. The middleware reloads the
  reseller row and rejects (401) when it is disabled or expired.
- `checkAPIAuth` and `BaseController.checkLogin` (SPA pages) learn to accept a reseller session.

### 3.2 Layer 1: the route policy table (deny by default)

New file `internal/web/controller/route_policy.go`: one map from `METHOD /panel/api/...pattern`
to a policy. Three values:

- `AdminOnly` — a reseller gets 403. This is also the answer for any route **missing** from the
  table at runtime.
- `ResellerScoped` — allowed; the handler must pass the request's `Scope` (§3.3) to the service.
- `ResellerSelf` — the new `/reseller/me*` routes.

A new middleware `enforceActorPolicy` (next to `enforceTokenScope`) applies it. Handlers contain
no role checks.

**Contract test (stage B):** extend `internal/web/routes_contract_test.go`
(`TestRouteRegistryContract` already walks every registered route). The new
`TestRoutePolicyContract` fails when a registered `/panel/api/*` route has no policy entry or a
policy entry names a route that no longer exists. A new route therefore cannot ship unclassified.

### 3.3 Layer 2: one `Scope` in the service layer

`service.Scope{ ResellerID *int }` built from the actor (`nil` = admin = no filter). It is the
only place that knows how ownership is checked:

- `scope.Clients(db)` — a GORM scope adding `clients.reseller_id = ?`. Used by every list/search.
- `scope.OwnEmails(tx, emails)` / `OwnClientIDs` / `OwnSubID` — returns "not found" for anything
  not owned. **Not found, never 403**, so a reseller cannot probe which emails exist.
- `scope.AllowedInbounds(tx, ids)` — every inbound id must be in `reseller_inbounds`.
- `scope.StripForReseller(payload)` — removes fields a reseller may not set or see (§8.2).

Path parameters (`:email`, `:subId`, `:tgId`, `:id` on `happLink`) are checked by a small generic
guard driven by the policy entry (the entry names which parameter is a client identifier), so
those handlers need no code at all. Identifiers inside request bodies (`emails`, `inboundIds`,
bulk payloads) are checked by the `ClientService` entry point through the same `Scope` methods.

**IDOR matrix test (stage C):** for every `ResellerScoped` route, call it as reseller A against a
client of reseller B and against an admin client; expect not-found and no state change.

### 3.4 Route classification

`F` = forbidden for resellers (AdminOnly). `S` = reseller-allowed, with the filter shown.
Everything is unchanged for the admin.

**`/panel/api/clients`** (`controller/client.go`)

| Route                                                                                          | Reseller | Filter / note                                                                        |
| ---------------------------------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------ |
| `GET /list`, `GET /list/paged`                                                                 | S        | own clients only; summary counts computed over own clients; no owner field           |
| `GET /get/:email`, `/get/subId/:subId`, `/traffic/:email`, `/links/:email`, `/subLinks/:subId` | S        | own client, else not found                                                           |
| `GET /get/tgId/:tgId`                                                                          | S        | results filtered to own clients                                                      |
| `POST /happLink/:id`                                                                           | S        | own client id                                                                        |
| `POST /add`, `/bulkCreate`                                                                     | S        | owner forced; allowed inbounds; limit gate (§4)                                      |
| `POST /renewalPreview`                                                                         | S        | own emails                                                                           |
| `POST /update/:email`                                                                          | S        | own client; cannot move to non-allowed inbounds; enable blocked while cut off (§4.4) |
| `POST /del/:email`, `/bulkDel`                                                                 | S        | own clients                                                                          |
| `POST /:email/attach`, `/:email/detach`, `/bulkAttach`, `/bulkDetach`                          | S        | own clients **and** allowed inbounds                                                 |
| `POST /:email/externalLinks`                                                                   | S        | own client                                                                           |
| `POST /bulkAdjust`, `/bulkEnable`, `/bulkDisable`, `/bulkResetTraffic`, `/resetTraffic/:email` | S        | own clients; enable blocked while cut off; reset never lowers the pool counter       |
| `POST /ips/:email`, `/clearIps/:email`, `/hwids/:email`, `DELETE /hwids/...`                   | S        | own client                                                                           |
| `POST /onlines`, `/lastOnline`                                                                 | S        | intersected with own emails                                                          |
| `GET /export`                                                                                  | S        | own clients only (still contains their secrets, which the reseller already sees)     |
| `POST /import`                                                                                 | F in v1  | see §10 Q5                                                                           |
| `POST /updateTraffic/:email`                                                                   | F        | manual counter edit is an admin tool                                                 |
| `POST /delOrphans`, `/resetAllTraffics`, `/delDepleted`                                        | F        | global operations                                                                    |
| `POST /onlinesByGuid`, `/clientIpsByGuid`, `/activeInbounds`                                   | F        | node-sync / cross-panel data                                                         |
| `GET /groups`, `GET /groups/:name/emails`, `POST /groups/*` (`controller/group.go`)            | F in v1  | groups are global rows; see §10 Q4                                                   |

**`/panel/api/inbounds`** (`controller/inbound.go`)

| Route                                                                                                                                                                                                                                                                                                                            | Reseller | Filter / note                                                 |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- | ------------------------------------------------------------- |
| `GET /options`                                                                                                                                                                                                                                                                                                                   | S        | only allowed inbounds; see the leak note on `AwgServer` below |
| every other route (`/list`, `/list/slim`, `/get/:id`, `/allLinks`, `/:id/fallbacks`, `/amneziawgKernelReadiness`, `/add`, `/del/:id`, `/bulkDel`, `/update/:id`, `/setEnable/:id`, `/:id/subSortIndex`, `/:id/resetTraffic`, `/:id/delAllClients`, `/resetAllTraffics`, `/import`, `POST /:id/fallbacks`, `/pushClientTraffics`) | F        |                                                               |

**`/panel/api/server`** (`controller/server.go`)

| Route                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | Reseller                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------- |
| `GET /getNewUUID`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | S (pure generator, the client form uses it) |
| everything else: `status`, all history/metrics/observatory routes, `getXrayVersion`, `getPanelUpdateInfo`, `getUpdateStatus`, `getConfigJson`, `getDb`, `getMigration`, `getWebCertFiles`, `descendants`, key generators, `clientIps` (GET/POST), `fail2banStatus`, `stopXrayService`, `restartXrayService`, `installXray`, `updatePanel`, `setUpdateChannel`, `updateGeofile`, `logs`, `xraylogs`, `amneziawglogs`, `importDB`, `getNewEchCert`, `getCertHash`, `getRemoteCertHash`, `scanRealityTarget(s)` | F                                           |

**`/panel/api/setting`** (`controller/setting.go`)

| Route                                                                                                                                                                                                                          | Reseller | Filter / note                                                                                                                                                                                                                                                                                                |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `POST /defaultSettings`                                                                                                                                                                                                        | S        | whitelist only: `expireDiff`, `trafficDiff`, `pageSize`, `datepicker`, `ipLimitEnable`, `happLinkEnable`, `subEnable`, `subTitle`, `subURI`, `subJsonURI`, `subJsonEnable`, `subClashURI`, `subClashEnable`. Not `usageMultiplierEnable`, `tgBotEnable`, `webDomain`, `accessLogEnable`, `devChannelEnable`. |
| everything else: `all`, `factoryDefaults`, `update`, `validateRegex`, `updateUser`, `restartPanel`, `getDefaultJsonConfig`, `apiTokens*`, `testSmtp`, `testTgBot`, `tgBotCapabilities`, `usageMultiplierStatus`, `testDiscord` | F        |                                                                                                                                                                                                                                                                                                              |

**All F:** `/panel/api/nodes/*` (`node.go`), `/panel/api/hosts/*` (`host.go`), `/panel/api/xray/*`
(`xray_setting.go`), `/panel/api/sub-balancers*` (`sub_balancer.go`),
`POST /panel/api/backuptotgbot`, `GET /panel/api/openapi.json`.

**Not under `/panel/api`:**

| Surface                                                                                            | Reseller                                                                      |
| -------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `GET /`, `/csrf-token`, `/sponsors*`, `POST /login`, `/logout`, `/getTwoFactorEnable` (`index.go`) | same as admin (public / login flow)                                           |
| SPA pages `GET /panel/*` (`spa.go`)                                                                | served (it is one bundle); the UI hides admin pages, the API is the real gate |
| PWA assets (`web.go`)                                                                              | public, as today                                                              |
| `GET /ws` (`controller/websocket.go`)                                                              | allowed, but filtered (below)                                                 |

**New routes:** §5.1.

### 3.5 Data-leak spots (each needs a test)

1. **Inbound payloads embed every client.** `GET /inbounds/list`, `/list/slim`, `/get/:id` carry
   `settings.clients` and `clientStats` for all owners, plus `usageMultiplier`. All are F. The
   reseller UI must work from `/inbounds/options` and the clients endpoints only.
2. **`/inbounds/options`** (`service.InboundOption`) has no multiplier, but it carries `AwgServer`
   and `TuicServer` (server blocks so the clients page can build `.conf` files) and the hosting
   node. For resellers: only allowed inbounds, and the node field is dropped. The server blocks are
   needed to render a client config; before stage C confirm they contain no server private key,
   otherwise AmneziaWG/TUIC inbounds cannot be offered to resellers (§10 Q6).
3. **Online / last-online / IP endpoints** return every email (`GetOnlineClients`,
   `GetClientsLastOnline`, `/server/clientIps`). Scoped ones are intersected with own emails; the
   rest are F.
4. **Backup and config**: `getDb`, `getMigration`, `getConfigJson`, `importDB`, `backuptotgbot` —
   F. A reseller must never get a file containing other tenants.
5. **Logs**: `logs`, `xraylogs`, `amneziawglogs` contain other clients' emails and IPs — F.
6. **WebSocket** (`websocket/hub.go`): `Hub.fanout` sends every message to every connection:
   `status`, `traffic`, `inbounds`, `client_stats`, `clients`, `nodes`, `outbounds`, `xray_state`,
   `notification`. Change: `websocket.Client` gets an audience (admin or reseller id);
   `HandleWebSocket` sets it from the actor; `fanout` sends reseller connections **only**
   `invalidate` messages for the `clients` type and a new `reseller` type. Payload-carrying
   messages never reach a reseller. The reseller UI refetches on invalidate and on a timer.
7. **Summary counts** in `ClientService.ListPaged` (`clientQuery.summary`) and the group list must
   be computed inside the scope, or totals leak the size of other tenants.
8. **Error messages**: a create with an email that exists under another owner must say "email
   already in use" and nothing more. Emails stay globally unique (`clients.email` unique index,
   and Xray needs it).
9. **Client export** is scoped; **Telegram/Discord backups** stay admin-only.
10. **`window.X_UI_*` injected by `controller/dist.go`** (version, DB type): harmless, unchanged.

### 3.6 Bots, tokens, subscription server

- **Telegram bot** (`service/tgbot/`): chat ids in `tgBotChatId` are full admins; a client's
  `tgId` gives that end user a view of their own usage. Neither changes. Resellers get **no** bot
  role in v1. The bot's admin actions act as the admin (no scope).
- **Discord bot** (`service/discord/gateway.go`, `GetDiscordAdminIds`): admin-only, unchanged.
- **API tokens**: existing scopes unchanged. A reseller cannot list or create tokens
  (`apiTokens*` is F). Optional `reseller` scope: §5.4.
- **Subscription server** (`internal/sub`): public, keyed by `subId`, no ownership concept. No
  change (§7).

---

## 4. Limit enforcement

### 4.1 One gate

New `service.ResellerGate` (file `internal/web/service/reseller_gate.go`). Every path that adds
clients, enables clients or changes a client's quota calls it, inside the same DB transaction as
the write:

```
gate.CheckAdd(tx, scope, payloads)      // create, bulkCreate, (import)
gate.CheckEnable(tx, scope, emails)     // update enable false→true, bulkEnable
gate.CheckQuota(tx, scope, old, new)    // update, bulkAdjust
```

For the admin (`scope.ResellerID == nil`) every call is a no-op. Rules for a reseller:

| Rule             | Check                                                                                                                                     |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Account usable   | `enable`, not expired, `cutoff_reason == ''`. Otherwise: no create, no enable. Editing and deleting stay allowed.                         |
| Client count     | `max_clients == 0` or `count(own clients) + new <= max_clients`. Disabled clients count. Bulk create is all-or-nothing against the limit. |
| Allowed inbounds | every target inbound id is in `reseller_inbounds`.                                                                                        |
| Required quota   | when `require_client_quota`: `totalGB > 0` on create and on update.                                                                       |
| Pool             | creating or enabling is blocked once `used_traffic >= pool_bytes` (pool > 0).                                                             |

There is **no** "sum of client quotas ≤ pool" rule. The pool is consumed by real weighted traffic
only, as required; a reseller may give out more quota than it has, and the pool cut-off is what
stops it.

### 4.2 Where each path meets the gate

| Path                 | Entry point                                                          | Gate call                                                                                                                                                                                                            |
| -------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Create               | `ClientService.Create` (`client_crud.go:177`)                        | `CheckAdd`; owner written in the same tx                                                                                                                                                                             |
| Bulk create          | `ClientService.bulkCreate` (`client_bulk.go:1280`)                   | `CheckAdd` once for the whole batch                                                                                                                                                                                  |
| Import               | `ClientService.ImportClients`                                        | F for resellers in v1; if opened later it must call `CheckAdd` and force the owner                                                                                                                                   |
| "Clone"              | there is no server-side clone; the UI's duplicate is a normal create | covered by Create                                                                                                                                                                                                    |
| Update               | `ClientService.Update` (`client_crud.go:655`)                        | `CheckEnable` when enabling, `CheckQuota`, allowed inbounds for the inbound filter                                                                                                                                   |
| Attach / bulk attach | `ClientService.Attach`, `BulkAttach`                                 | allowed inbounds                                                                                                                                                                                                     |
| Bulk adjust          | `ClientService.BulkAdjust` (`client_bulk.go:335`)                    | `CheckQuota`                                                                                                                                                                                                         |
| Enable / bulk enable | `SetClientEnableByEmail`, `BulkSetEnable`                            | `CheckEnable`                                                                                                                                                                                                        |
| Reset traffic        | `ResetClientTrafficByEmail`, bulk reset                              | allowed; the pool counter is untouched (§4.3)                                                                                                                                                                        |
| Auto-renew           | `InboundService.autoRenewClients` (`inbound_traffic.go:383`)         | runs in the traffic writer, no actor; it resets the client's own counters and re-enables it. It must **skip clients with `pool_disabled = true`** so a renewal cannot undo a cut-off. The pool counter is untouched. |

**Raising a client's quota later** is allowed at any time and does not touch the pool. The quota
is only a per-client ceiling; the reseller pays in real traffic. If the pool is exhausted the
client stays cut off whatever its quota says.

**Lowering or removing a quota** is allowed unless `require_client_quota` forbids zero.

### 4.3 The pool counter: exact hooks

Weighted client traffic is added to `client_traffics` in exactly two places. The pool counter is
incremented in the same two places, in the same transaction, with the same delta.

1. **Local traffic** — `InboundService.addClientTraffic` (`inbound_traffic.go:131`).
   After `resolveStatEmailTraffic` (applies the per-inbound multiplier) and
   `canonicalizeClientTraffic`, the loop runs
   `UPDATE client_traffics SET up = up + ?, down = down + ?`. The hook goes right after that loop
   and uses the same `traffics` slice (already weighted, one entry per email), restricted to the
   emails that actually had a `client_traffics` row.
   This one function serves the Xray poll, MTProto, TUIC and AmneziaWG jobs (all call
   `InboundService.AddTraffic`).
2. **Node traffic** — `InboundService.mergeNodeClientTraffic` (`inbound_node.go:230`), called from
   `setRemoteTrafficLocked`. The node reports totals that are already weighted; the master
   subtracts its `node_client_traffics` baseline and adds `deltaUp` / `deltaDown`. The hook is
   inside `mergeNodeClientTraffic`, using those same two numbers, so the pool moves exactly when
   and as often as the client's row does. The master never multiplies a node delta again, and
   neither does the hook.

The hook is one helper, `addResellerUsage(tx, deltasByEmail)`
(`internal/web/service/reseller_usage.go`):

- one `SELECT email, reseller_id FROM clients WHERE email IN ? AND reseller_id IS NOT NULL`
  (chunked by `sqlInChunk`), summed per reseller in memory;
- one `UPDATE resellers SET used_traffic = <ClampedAddExpr> WHERE id = ?` per reseller, in
  ascending id order (stable lock order on PostgreSQL).

On a panel with no resellers the SELECT returns nothing; the cost is one indexed query per poll.

What does **not** move the counter:

- client reset, auto-renew, delete, detach, bulk reset, group reset, "reset all traffics";
- `UpdateClientTrafficByEmail` (admin's manual edit of a client's counters);
- `AcceptGlobalTraffic` / `pushClientTraffics` (cross-master totals; display and quota only);
- a node adopting a client it already had usage for (the "row does not exist yet" branch in
  `setRemoteTrafficLocked`): that usage predates the ownership and is not billed.

Only two admin actions lower it: **reset usage** (`used_traffic = 0`) and — indirectly — **add
volume** (raises `pool_bytes`; the counter itself stays).

**Transaction boundaries.** Both hooks run inside the transaction that updates
`client_traffics`: the first `db.Transaction` in `addTrafficLocked`, and the node merge
transaction in `setRemoteTrafficLocked`. Usage and pool commit or roll back together. Both callers
go through `submitTrafficWrite` (`traffic_writer.go`), the single serial traffic writer, so two
polls never update the counter concurrently, and the UPDATE is an atomic increment anyway.

**Node-sync counting.** The reseller lives on the master. A node only sees ordinary clients and
bills them into its own `client_traffics`; the master learns that usage only through the baseline
delta above, once. If a client is on the master's own inbound **and** on a node, each byte arrives
through exactly one of the two hooks. A node that is itself a master of other nodes forwards
totals upward the same way; the counter exists only on the panel where the reseller is defined.

### 4.4 Cut-off

New step `enforceResellerCutoffs(tx, batch)` in the lifecycle transaction of `addTrafficLocked`
(the second `db.Transaction`, next to `disableInvalidClients`). The node sync already calls
`AddTraffic(nil, nil)` after its merges (`job/node_traffic_sync_job.go:137`), so node-driven usage
triggers the same step.

A reseller is **due** when `cutoff_reason == ''` and any of:

- `pool_bytes > 0 AND used_traffic >= pool_bytes` → reason `pool`;
- `expiry_time > 0 AND expiry_time <= now` → reason `expired`;
- `enable = false` → reason `disabled`.

For each due reseller, in that transaction:

1. select its clients with `enable = true`;
2. disable them the same way `disableInvalidClients` (`inbound_disable.go:78`) does: flip
   `enable` in each inbound's settings JSON (`markClientsDisabledInSettings`), set
   `client_traffics.enable = false` and `clients.enable = false`, and queue the runtime work in
   the `trafficMutationBatch` (local `trafficRemoveUser` plans, remote inbound-update plans,
   `addNode`). The batch is applied after commit through `runtime.Runtime`, so nodes follow. No
   direct call to `internal/xray/api.go`.
3. set `clients.pool_disabled = true` on exactly those clients;
4. set `resellers.cutoff_reason`.

Clients that were already disabled (manually, expired, or out of their own quota) are **not**
flagged.

**Threshold and race.** The counter update and the check are in the same serialized writer run:
the usage transaction commits, then the lifecycle transaction reads the committed counter. A
threshold cannot be missed. What is accepted: traffic that flows between the poll that crosses the
pool and the moment Xray drops the users, i.e. **an overshoot of at most one poll interval (5 s)
of traffic**, plus one node sync interval (5 s) for node-hosted traffic. The counter keeps
counting that overshoot, so `used_traffic` may end slightly above `pool_bytes`.

**While cut off:**

- creating and enabling clients is blocked by the gate (§4.1);
- a reseller may still disable, edit or delete its clients;
- **manual disable during the cut-off** (reseller or admin): the client is already off, so
  nothing changes at runtime, but `pool_disabled` is **cleared**. The intent "this client should
  be off" is now manual, and a later top-up will not re-enable it;
- a client that expires or exhausts its own quota while cut off keeps `pool_disabled = true`; the
  re-enable step skips it (below) and clears the flag.

### 4.5 Re-enable

Triggered by the admin: **add volume**, **reset usage**, **extend expiry**, **enable reseller**.
The service (`ResellerService`) updates the row, then calls `releaseResellerCutoff(resellerID)` if
no cut-off condition holds any more:

1. select clients with `pool_disabled = true`;
2. keep only those still valid for their own reasons: not expired, and not over their own quota
   (same predicate as `depletedCond`);
3. enable the kept ones through the existing admin path `ClientService.BulkSetEnable`
   (`client_bulk.go:1570`), which already dispatches through `runtime.Runtime` and marks offline
   nodes dirty;
4. clear `pool_disabled` on **all** flagged clients (kept or not) and clear `cutoff_reason`.

Clients disabled manually (`pool_disabled = false`) are never touched. This is the
"manual-disabled client not re-enabled after top-up" test.

A partial failure (a node offline) leaves the client enabled in the DB with the node marked dirty;
the existing anti-entropy reconcile delivers it later, as for any admin enable.

**Admin "disable all / enable all clients of a reseller"** are plain bulk operations over
`scope.Clients` using `BulkSetEnable`. "Disable all" clears `pool_disabled` (manual intent).
"Enable all" while the reseller is cut off is refused with a clear message.

### 4.6 Concurrency of creates

Two creates at once must not both pass `max_clients`. `CheckAdd` starts by locking the reseller
row inside the write transaction: `SELECT ... FOR UPDATE` on PostgreSQL; on SQLite a no-op
`UPDATE resellers SET updated_at = updated_at WHERE id = ?`, which takes the write lock
(`dialect.go` gets a small helper for this). The count is read after the lock. Test: N parallel
creates with one slot left → exactly one succeeds.

### 4.7 Notifications

After the counter update, when usage crosses 80% or 100% and `notified_level` is lower, set
`notified_level` and publish a new event `reseller.pool` on `internal/eventbus`
(`ResellerPoolData{Name, UsedBytes, PoolBytes, Level}`). The Telegram bot subscribes and messages
the admin chat ids; a new setting `resellerPoolNotify` (default off) gates it. Producers do not
import the bot (layering rule 9). Admin reset / top-up lowers `notified_level` again.

---

## 5. API

### 5.1 New endpoints

All under `/panel/api`. Each needs an entry in `frontend/src/pages/api-docs/endpoints.ts`
(`TestRouteRegistryContract`), a policy entry (§3.2), `make gen`, then the OpenAPI copy to
`docs/public/openapi.json` and `cd docs && pnpm gen:api`. New response structs go into
`StructAllow` in `tools/openapigen/main.go`.

**Admin-only (`/resellers`, new `controller/reseller.go`)**

| Route                                                  | Purpose                                                                                   |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| `GET /resellers`                                       | list with pool total, used, remaining, client count, limit, expiry, state                 |
| `GET /resellers/:id`                                   | detail, including allowed inbound ids                                                     |
| `POST /resellers`                                      | create (username, password, name, limits, allowed inbounds)                               |
| `POST /resellers/:id`                                  | edit name, limits, expiry, `require_client_quota`, allowed inbounds, enable               |
| `POST /resellers/:id/password`                         | set a new password (bumps epoch)                                                          |
| `POST /resellers/:id/del`                              | delete; body says what happens to its clients: `transfer` (to admin, default) or `delete` |
| `POST /resellers/:id/addVolume`                        | `{bytes}`; raises `pool_bytes`, then re-enable check                                      |
| `POST /resellers/:id/resetUsage`                       | `used_traffic = 0`, then re-enable check                                                  |
| `POST /resellers/:id/disableClients`, `/enableClients` | bulk over that reseller's clients                                                         |
| `POST /resellers/:id/resetTotp`                        | clear the reseller's 2FA secret                                                           |
| `GET /resellers/:id/audit`                             | paged audit log                                                                           |
| `POST /clients/:email/owner`                           | reassign a client: `{resellerId: number \| null}`                                         |

Removing an inbound from a reseller's allowed list does not detach existing clients; it only
stops new attachments. The UI says so.

**Reseller self (`/reseller`)**

| Route                                                 | Purpose                                                                                                                                                                                                                  |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET /reseller/me`                                    | name, pool total/used/remaining, client count and limit, expiry, state (`ok` / `pool` / `expired` / `disabled`), `requireClientQuota`. Also the admin's answer is `{role: "admin"}`, so the SPA has one "who am I" call. |
| `POST /reseller/me/password`                          | change own password                                                                                                                                                                                                      |
| `POST /reseller/me/totp`, `/reseller/me/totp/disable` | set up / remove own 2FA                                                                                                                                                                                                  |

A reseller has no endpoint that edits its own limits, expiry, allowed inbounds or usage.

### 5.2 Owner reassignment (`POST /clients/:email/owner`)

Admin only. Sets `clients.reseller_id`.

- `used_traffic` is **not** rewritten for either side. Past traffic stays billed to the old
  owner; only traffic after the change counts for the new owner.
- The new owner's `max_clients` is checked (the admin sees a clear error; no silent override).
- The client's inbounds are not changed, even if some are outside the new owner's allowed list
  (the admin decided). The response lists such inbounds as a warning.
- `pool_disabled` is cleared. If the new owner is currently cut off, the client is disabled and
  flagged under the new owner; if the old owner was cut off and the new one is not, the client is
  re-enabled when still valid for its own reasons.
- Audit log entry on both resellers.

### 5.3 The read-only owner field

Client list and detail responses for the **admin** gain:

```
"owner": { "id": 3, "name": "Shop A" }   // or null for the admin's own clients
```

- Added to `service.ClientSlim` (`client_paging.go:21`) and the detail payload
  (`ClientController.buildClientPayload`). Filled by one extra lookup per page (ids → names), not
  a per-row query.
- For a reseller the field is **absent** (not null): `scope.StripForReseller` removes it.
- Never accepted on input: `ClientCreatePayload` and `model.Client` get no owner field, and the
  DB columns are `json:"-"`. Sending `owner`, `resellerId`, `reseller_id` or `poolDisabled` in a
  create/update body changes nothing. There is a test for exactly this.
- Filter: `ClientPageParams` gains `owner` = empty (all) / `admin` / a reseller id. Ignored for a
  reseller actor (its scope already applies).

### 5.4 API tokens for resellers (optional, stage C)

New scope `reseller` in `model.ApiScope*`, with `api_tokens.reseller_id`. Created by the admin
only. `checkAPIAuth` maps it to a reseller actor, so it passes through the same policy table and
`Scope` as a session — no separate allowlist to maintain. Tokens of a disabled, expired or
deleted reseller stop working. Not required for the first release; see §10 Q3.

---

## 6. UI

Ant Design 6, React Hook Form, Zod schemas in `src/schemas/`, strings in all 14 locale files.
One `useMe()` query (`GET /reseller/me`) tells the SPA the role.

### 6.1 Admin: reseller management

- New route `/resellers` in `frontend/src/routes/panelRoutes.tsx` (router, sidebar, titles and
  command palette read that list) and in `controller/spa.go`.
- List: name, state tag, pool bar (used / total, %), clients (count / limit), expiry, actions.
- Detail drawer: the same numbers, allowed inbounds (multi-select from `/inbounds/options`),
  limits form, and the actions: add volume, reset usage, disable all, enable all, set password,
  reset 2FA, delete (with the transfer/delete choice), audit log tab.
- Destructive actions use the existing confirm pattern.

### 6.2 Admin: owner badge and filter in the client list

- `frontend/src/pages/clients/RowCells.tsx`: a small `Tag` with the reseller's name next to the
  email; a neutral "admin" tag for own clients only while a filter or at least one reseller
  exists (no noise on panels without resellers).
- `FilterDrawer.tsx` / `filters.ts`: owner filter — All / Admin's own / each reseller — sent as
  the `owner` query parameter (server-side paging, so the filter must be server-side).
- Must be checked in the table layout, the mobile card layout, light and dark theme, and RTL
  (fa-IR): the tag sits on the inline-end side and names may be Persian.

### 6.3 Reseller view

- **Navigation:** resellers see only Home and Clients. `PANEL_ROUTES` entries get a
  `roles` field; the sidebar, router and command palette filter by it. Typing an admin URL shows
  the SPA's not-found page; the API would refuse anyway.
- **Home** (the `/` route renders a `ResellerHome` component instead of `IndexPage`): at the top,
  always — pool total, used, remaining with a progress bar and percentage; client count and the
  limit if any; expiry date if any (Jalali when the locale/datepicker says so, as elsewhere). A
  full-width alert when the pool is exhausted ("clients are cut off") or the account expired.
  With no pool limit it shows usage and "unlimited".
  Numbers come from `/reseller/me` (the counter), never from summing clients. Refresh: every
  15 s, on the `invalidate` WebSocket message, and after any client mutation (query
  invalidation).
- **Clients page:** the existing `ClientsPage` with the scope applied by the API. Hidden for
  resellers: owner badge and filter, groups, import, "delete depleted", "reset all", node
  columns, the per-inbound multiplier. The inbound picker lists only allowed inbounds. While cut
  off, "add" and "enable" are disabled with the reason.
- **Byte formatting:** the existing `SizeFormatter` (`frontend/src/utils/index.ts`) everywhere;
  no new formatter.

### 6.4 i18n

New keys under `pages.resellers.*`, `pages.resellerHome.*`, `menu.resellers`, plus toasts. Every
key in all 14 files in `internal/web/translation/` and referenced in the same commit
(`i18n-dead-keys.test.ts`). fa-IR translated, en-US English, English fallback elsewhere.
Interpolation uses single braces.

---

## 7. Multi-node and subscriptions

- **Nodes know nothing about resellers.** The master pushes ordinary clients; ownership, pool and
  flags stay in the master's DB. No change to `runtime.Remote`, the wire format or
  `node_contract_test.go` unless a new `Remote` method is added (none is planned).
- **Every state change goes through `runtime.Runtime`:** the cut-off reuses
  `trafficMutationBatch`; re-enable and the bulk admin actions reuse `BulkSetEnable`. No new path
  to Xray.
- **Offline node during a cut-off:** the client is disabled in the master's DB and the node is
  marked dirty; reconcile applies it on reconnect. Until then the node keeps serving that client,
  and its traffic keeps being billed to the pool when the node returns. This is the existing
  behaviour for any disable and is accepted.
- **A node's own verdicts** (it disabled a client for its local quota) keep flowing through
  `setRemoteTrafficLocked` as today. They never set `pool_disabled`.
- **Old / stock nodes:** unaffected; they never see reseller data.
- **Subscriptions:** `internal/sub` is not touched. Links, `Subscription-Userinfo`, JSON and
  Clash output are identical for reseller and admin clients. A cut-off client looks exactly like
  any disabled client today. No double clients: ownership is a column on the one `clients` row.
- **End users** get no new restriction and no new information.
- **Docs site:** `docs/lib/xray/` (the third link implementation) is not affected, because link
  output does not change.

---

## 8. Security review

### 8.1 Privilege-escalation paths

| Path                                                       | Defence                                                                                                                                                                 |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IDOR on `:email`, `:subId`, `:tgId`, client `:id`          | path-param guard from the policy table → `Scope.Own*`; foreign = not found. IDOR matrix test.                                                                           |
| Emails / ids in bodies (bulk ops, attach, renewal preview) | `Scope.OwnEmails` at the `ClientService` entry point; a batch with one foreign email is rejected whole.                                                                 |
| Inbound ids in bodies                                      | `Scope.AllowedInbounds`; applies to create, update's inbound filter, attach, bulk attach.                                                                               |
| Mass assignment of owner / flags / limits                  | no JSON path to `reseller_id`, `pool_disabled`; reseller limits editable only on admin routes. Test sends them and asserts no change.                                   |
| Fields a reseller must not set on a client                 | `StripForReseller` on input: `tgId` (see §10 Q7), `clientHostRuleIds` (references admin-owned host rules), `reverse`, and anything else found unsafe in stage C review. |
| Reaching admin data through an allowed route               | allowed routes are few and each has a filter test (§3.5).                                                                                                               |
| New route added later                                      | unclassified route fails `TestRoutePolicyContract`; at runtime it is AdminOnly.                                                                                         |
| Session confusion (admin + reseller in one cookie)         | setting one identity deletes the other.                                                                                                                                 |
| Reseller editing itself                                    | no self-edit endpoint for limits; password change needs the current password.                                                                                           |
| Username squatting an admin name                           | uniqueness across both tables, both directions.                                                                                                                         |
| Export / import                                            | export scoped; import F in v1; DB export/import admin-only.                                                                                                             |
| WebSocket                                                  | audience filter in `fanout`; test that a reseller connection receives no payload message.                                                                               |
| CSRF                                                       | new routes sit under the existing `/panel/api` group, so `CSRFMiddleware` already applies.                                                                              |
| Enumeration via errors                                     | not found instead of 403 for foreign objects; generic login failure.                                                                                                    |
| Email namespace collisions                                 | emails are global; a reseller can discover that an email is taken (unavoidable). Optional prefix: §10 Q8.                                                               |
| Bypassing the multiplier                                   | impossible by construction: the pool is fed from the already-weighted delta, and a reseller can neither see nor change `usageMultiplier`.                               |
| Lowering the pool counter                                  | no reseller-reachable code path writes `used_traffic` except the increment. Test: reset/delete/renew do not lower it.                                                   |

### 8.2 What a reseller can see of a client

Its own clients in full (credentials, links, usage, IPs, HWIDs) — it needs them to serve its
customers. Never: owner, other tenants, inbound settings beyond `/inbounds/options`, multipliers,
node names.

### 8.3 Audit log

Table `reseller_audit_logs`: `id`, `at` (ms), `actor_kind` (`admin` / `reseller` / `system`),
`actor_id`, `reseller_id` (the affected tenant, indexed), `action`, `target` (client email or
empty), `detail` (small JSON: changed fields, counts, byte amounts — **no secrets**), `ip`.

Written by one helper called from the service layer, in the same transaction as the change.

- Reseller actions: login, failed login (post-lookup), client create / update (changed field
  names) / delete / enable / disable / reset traffic / attach / detach, bulk ops (count + first
  N emails), password and 2FA changes.
- Admin actions on a reseller: create, edit, add volume, reset usage, bulk enable/disable,
  owner reassignment, delete.
- System: cut-off and release, with the reason and the number of clients.

Retention: a daily prune keeps 90 days (constant; setting later if needed). Read by the admin
only (`GET /resellers/:id/audit`).

---

## 9. Staged plan

Each stage is a separate branch and release, with `make verify` green, and can be shipped without
the next one. TDD per `CLAUDE.md`: every test below is written first and must fail for the stated
reason. Nothing reseller-visible exists until stage B is in; until stage C a reseller can log in
but can do nothing useful.

### Stage A — model, migration, admin CRUD

- Scope: the three tables, the two `clients` columns, `ResellerService` CRUD, the admin
  `/resellers` endpoints (list, get, create, edit, password, delete with transfer/delete),
  `endpoints.ts`, gen, docs OpenAPI copy. No login, no UI.
- Tests:
  - migration from a hand-built pre-reseller DB keeps every client with `reseller_id` NULL
    (SQLite and Postgres);
  - `TestMigrationModelsMatchPanelModels` stays green;
  - username uniqueness across `users` and `resellers`, both directions;
  - password stored as bcrypt and never present in any response;
  - delete with `transfer` leaves clients owned by admin; with `delete` removes them through the
    normal delete path.
- Risks: a new unique index on a busy DB (new table, so none); upstream touching `allModels()`.
- Rollback: older binary ignores the tables/columns.

### Stage B — actor, route policy, scope skeleton, reseller login

- Scope: `resolveActor`, `enforceActorPolicy`, the full policy table from §3.4,
  `session.SetLoginReseller` / `GetActor`, reseller login with limiter and optional 2FA,
  WebSocket audience filter, `GET /reseller/me`, `Scope` type with the path-param guard. Every
  `ResellerScoped` route is still **AdminOnly** in this stage except `/reseller/me`.
- Tests:
  - `TestRoutePolicyContract`: **fails when a registered route is not classified** (prove by
    adding a throwaway route) and when a policy entry is stale;
  - a reseller session gets 403 on every AdminOnly route (table-driven over the policy table);
  - admin behaviour unchanged: the existing controller suites stay green untouched;
  - disabled / expired reseller cannot log in, and an existing session dies on the next request;
  - admin and reseller identities cannot coexist in one session;
  - LDAP enabled does not let a reseller username in with an LDAP password;
  - a reseller WebSocket receives no `traffic` / `inbounds` / `client_stats` payload.
- Risks: `checkAPIAuth` is shared with node sync and tokens — the node contract and node e2e
  suites (`make node-e2e`) must run. Upstream hooks in `api.go`, `index.go`, `session.go`.
- Rollback: delete all reseller rows (or disable them); the middleware is inert without them.

### Stage C — client operations with limits, owner field

- Scope: `ResellerGate`, scoped client routes opened one by one, owner stamping, the read-only
  `owner` field and `owner` filter, `POST /clients/:email/owner`, `/inbounds/options` and
  `/setting/defaultSettings` reseller filters, optional `reseller` token scope.
- Tests:
  - **owner field present for the admin, absent for a reseller** (list, paged list, detail);
  - **owner / resellerId / poolDisabled sent on create and update are ignored**;
  - **owner filter returns only the chosen reseller's clients**; `admin` returns only unowned;
    summary counts match the filter;
  - IDOR matrix over every scoped route (foreign client → not found, state unchanged);
  - attach / create on a non-allowed inbound is rejected, including inside a bulk payload;
  - `max_clients`: the N+1th create fails; bulk create over the limit creates nothing;
    N parallel creates with one slot → exactly one succeeds (SQLite and Postgres);
  - `require_client_quota` rejects `totalGB = 0` on create and update;
  - `totalGB = 0` accepted when the flag is off;
  - reassignment: new owner's count limit enforced, `used_traffic` of both unchanged;
  - `/inbounds/options` for a reseller lists only allowed inbounds and no node field;
    `defaultSettings` returns only the whitelist;
  - a node-backed inbound: reseller create reaches the node (cell in
    `internal/web/node_contract_test.go` only if a `Remote` method is added; otherwise an
    `internal/nodee2e` flow).
- Risks: the client service is the largest upstream-churn area (`client_crud.go`,
  `client_bulk.go`); every hook must be a one-line call into `reseller_*.go` files and listed in
  `docs/NETRON_UPSTREAM_CHANGES.md`.
- Rollback: set the scoped routes back to AdminOnly in the policy table (one-file change);
  ownership data stays.

### Stage D — UI

- Scope: `/resellers` admin page, owner badge + filter, reseller navigation, `ResellerHome`,
  reseller clients view, i18n in 14 locales.
- Tests (Vitest + Testing Library; Storybook stories for new reusable components):
  - owner badge renders the reseller name for owned rows and not at all for a reseller viewer;
  - choosing an owner in the filter sends `owner=<id>` and the table shows only those rows;
  - reseller navigation shows only Home and Clients (`navigation-contract.test.tsx` extended);
  - home renders total / used / remaining / percentage from `/reseller/me`, and the exhausted
    and expired states;
  - **dashboard numbers do not drop after a client reset**: mock `/reseller/me` unchanged while
    the clients list shows zero usage → the home still shows the counter value;
  - add / enable controls disabled with the reason while cut off;
  - locale key parity and dead-key test green.
- Not testable by unit tests: RTL, mobile and dark-theme appearance. Checked manually in the
  panel (fa-IR, phone width, both themes) and stated as such in the PR.
- Risks: `ClientsPage` is large; role-conditional rendering must not change the admin view
  (existing client page tests are the guard).
- Rollback: UI-only; hide the menu entry.

### Stage E — pool counter, cut-off / re-enable, audit log, admin actions

- Scope: `addResellerUsage` hooks, `enforceResellerCutoffs`, `releaseResellerCutoff`,
  `autoRenewClients` skip, add volume / reset usage / disable-all / enable-all, expiry cut-off,
  80/100% event and Telegram notice, audit log table writes and the admin audit view.
- Tests (real DB via `dbtest.InitDB`, real `InboundService.AddTraffic`):
  - a weighted delta (multiplier 1.5) raises `used_traffic` by the billed bytes, not the real
    bytes;
  - a node delta through `SetRemoteTraffic` raises it once; a client on a local **and** a node
    inbound is counted once per byte;
  - **client reset, delete, auto-renew and bulk reset do not lower `used_traffic`**;
  - crossing the pool disables the reseller's enabled clients, sets `pool_disabled`, and leaves
    another reseller's and the admin's clients untouched;
  - the cut-off happens in the same `AddTraffic` call that crosses the pool (no missed
    threshold);
  - **a manually disabled client is not re-enabled after a top-up**;
  - a client disabled manually _during_ the cut-off is not re-enabled;
  - a client that expired during the cut-off is not re-enabled and its flag is cleared;
  - reset usage and add volume re-enable exactly the flagged, still-valid clients;
  - auto-renew does not re-enable a `pool_disabled` client;
  - create and enable are refused while cut off;
  - reseller expiry and admin disable use the same cut-off and release;
  - the cut-off reaches a node: `internal/nodee2e` flow (master + node, reseller client on a node
    inbound, exhaust pool, node stops serving; top up, node serves again);
  - 80% and 100% each publish one event, not one per poll;
  - every listed action writes one audit row with no secret in `detail`;
  - Postgres variants of the counter and cut-off tests.
- Risks: this stage hooks the hottest path (`addClientTraffic`, every 5 s). The hook must add at
  most one indexed SELECT per poll on a panel without resellers; run the scale tests
  (`XUI_SCALE_TEST`). A bug here can cut off paying clients, so the stage ships behind a setting
  `resellerPoolEnforce` (default off = count only, never cut off) for one release.
- Rollback: turn `resellerPoolEnforce` off; counters keep running, nobody is cut off. Clients
  already cut off are released by the same switch (release runs when enforcement is turned off).

Order: A → B → C → D → E. D can start after C's API is merged; E depends only on A–C and can be
built in parallel with D.

---

## 10. Decisions (approved)

The open questions of the first draft, as answered by the owner:

1. **Account storage:** separate `resellers` table.
2. **Reseller 2FA:** yes, optional and self-service; the global 2FA setting stays the admin's.
3. **Reseller API tokens:** deferred until after stage E (§5.4 stays a design slot).
4. **Client groups:** hidden from resellers (routes F, `group` stripped from their input).
5. **Client import:** admin-only.
6. **Inbound protocols for resellers:** only VLESS, VMess, Trojan, Shadowsocks and Hysteria may be
   in an allowed list. Enforced since stage A in `replaceResellerInbounds`
   (`internal/web/service/reseller.go`).
7. **`tgId`:** a reseller may set it on its clients.
8. **Email prefix per reseller:** optional, off by default. The column arrives with stage C, where
   it is enforced.
9. **Deleting a reseller:** `transfer` (clients go to the admin) is the default; `delete` is an
   explicit choice.
10. **Pool counter:** one total, `used_traffic`.
11. **Non-multiplier protocols** are billed at x1 into the pool.
12. **`resellerPoolEnforce`:** kept for one release after stage E, then removed.
13. **Overshoot:** accepted as stated in §4.4.

## 11. Implementation status

| Stage                                          | State       | Notes                                                                                                                                                              |
| ---------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| A — model, migration, admin CRUD               | implemented | Tables, the two `clients` columns, `ResellerService`, `GET/POST /panel/api/resellers*`. The reseller cannot log in yet and no client can be owned through the API. |
| B — actor, route policy, scope, reseller login | implemented | Resellers log in and reach only `/reseller/me` and their own 2FA routes; every other route is refused.                                                             |
| C — client operations with limits, owner field | implemented | Resellers manage their own clients on allowed inbounds; the admin sees and reassigns owners.                                                                       |
| D — UI                                         | implemented | Admin reseller page, owner badge and filter, reseller home and clients view, self password and 2FA.                                                                |
| E                                              | not started |                                                                                                                                                                    |

Stage A details that refine the text above:

- `POST /resellers/:id` is a full-row edit of name, limits, expiry, `requireClientQuota` and
  `inboundIds`; an omitted `enable` keeps its value. The username cannot be changed after create.
- Usernames are compared case-insensitively across `users` and `resellers`. The admin side is
  guarded in `UserService.UpdateUser` and `UpdateFirstUser` (`refuseResellerUsername`).
- Allowed-inbound rows are removed in `ClientService.DetachInbound`, which all three inbound
  delete paths call (`delInbound`, the depleted-client purge, the node sync sweep).
- Editing limits or `enable` only stores the values; cut-off and release arrive with stage E.
- `reseller_audit_logs` and `clients.pool_disabled` exist but nothing writes them before stage E.

Stage B details that refine the text above:

- The policy table is `controller.RoutePolicies` (`internal/web/controller/route_policy.go`). It
  lists every route on the panel engine, including the non-API ones, with one of three classes:
  `PolicyPublic`, `PolicyReseller`, `PolicyAdminOnly`. `TestRoutePolicyContract`
  (`internal/web/route_policy_contract_test.go`) fails on an unclassified route and on an entry
  for a removed route. The subscription server is classified separately in
  `internal/sub/route_policy_contract_test.go` (all public, keyed by subId).
- The gate is one middleware, `enforceActorPolicy`, on the `/panel/api` group after
  `enforceTokenScope`. A reseller gets 403 on any API route not classified `PolicyReseller`.
  Non-API routes keep their own checks, which only know the admin session, so a reseller gets
  exactly the stranger's answer there. The one exception is the SPA shell (`checkLogin`), which
  serves resellers the page markup; all data behind it is still refused.
- Reseller-reachable API routes in this stage: `GET /reseller/me`, `POST /reseller/me/totp`,
  `POST /reseller/me/totp/disable`. `/reseller/me` answers `{"role":"admin"}` for an admin.
  The admin also gets `POST /resellers/:id/resetTotp`.
- **WebSocket:** refused to resellers in stage B (`HandleWebSocket` checks the admin session). The
  audience filter of §3.5 item 6 moves to stage D, when the reseller UI needs invalidations.
- **Self password change** (`/reseller/me/password`, §2.3) moves to stage D with the UI. Until then
  the admin sets reseller passwords.
- `Scope` (`internal/web/service/scope.go`) has `ClientFilter`, `RequireOwnEmails` (case
  insensitive), `RequireOwnClientIDs`, `RequireOwnSubID` and `RequireAllowedInbounds`. Foreign and
  missing objects both give `ErrScopeNotFound`. `StripForReseller` moves to stage C, which
  introduces the fields it strips. No client or inbound operation uses `Scope` yet.
- Sessions: `session.SetLoginReseller` / `GetLoginReseller` use their own keys
  (`LOGIN_RESELLER`, `RESELLER_EPOCH`). Setting either identity drops the other. The reseller row
  is reloaded on every request, so a disable, expiry, delete or password change ends the session
  on its next request.
- Login: the admin check runs first and is unchanged. A refused login reads the same for an
  admin, a reseller, an unknown name, and a disabled or expired reseller with the right password.
  The limiter is the existing per-IP-and-username one. A reseller with 2FA on whose password
  matched but who sent no code gets `{"success":false,"obj":{"twoFactorRequired":true}}`, and the
  login page then shows the code field; a wrong code reads like a wrong password.

Stage C details and corrections found while re-checking the references:

- **Corrections.** `ImportClients` is in `service/client_portable.go`, not `client_crud.go`.
  `GetInboundOptions` filters by the admin's user id, so a reseller's options are read with the
  first admin's id (as token logins already do) and then filtered to its allowed list.
- **A hijack the design did not name.** `ClientService.Create` treats an existing email with the
  same subId as "re-add this identity", reusing its UUID and attaching it to the new inbounds. A
  reseller who knew another tenant's email and subId (both appear in subscription links) could
  have attached that client to its own inbound. The reseller path refuses any email it does not
  own, case-insensitively, with Create's own "email already in use" message.
- **Reseller routes** (`controller.RoutePolicies`, `PolicyReseller`): `GET /clients/list/paged`,
  `GET /clients/get/:email`, `POST /clients/add`, `/clients/bulkCreate`, `/clients/update/:email`,
  `/clients/del/:email`, `/clients/bulkEnable`, `/clients/bulkDisable`,
  `/clients/resetTraffic/:email`, and `GET /inbounds/options` (needed to create a client). Expiry
  is set through update. Everything else from the §3.4 "S" list (links, IPs, HWIDs, onlines,
  export, renewal preview, attach/detach, bulk adjust, `defaultSettings`) stays admin-only until
  stage D needs it, each with its own scope check then.
- **One service entry per operation**: the `*For(scope, …)` methods in
  `service/client_scoped.go`. The admin passes straight to the existing method; a reseller is
  checked by `Scope` and the gate in `service/reseller_gate.go`, then the existing method runs, so
  every write still goes through `runtime.Runtime`. Handlers pass `actorScope(c)` and answer with
  `jsonObjFor`, which runs `StripForReseller` for a reseller.
- **The gate** (`gateNewClients`, `resellerBlocked`): blocked = disabled, expired, `cutoff_reason`
  set, or `used_traffic >= pool_bytes`. Blocked refuses create, bulk create, enabling (bulk enable,
  update false→true) and reset traffic (a reset re-enables a depleted client); edit, disable and
  delete stay allowed. Client limit and the quota rule apply to single and bulk create; a bulk
  create over the limit creates nothing.
- **Concurrency**: one in-process mutex per reseller around count-and-create. A second panel
  process writing the same database is not a supported setup; it would need a row lock.
- **Auto-renew** (`autoRenewClients`) skips clients with `pool_disabled` and clients of a
  blocked reseller. Renewals done by a node itself for node-hosted clients are not covered; that
  is part of stage E's node cut-off.
- **Owner field**: `owner` (`{id, name}` or `null`) on `/clients/list/paged` rows and on
  `/clients/get/:email` for the admin; removed by `StripForReseller` for resellers. The legacy
  full `GET /clients/list` is unchanged. Filter: `owner=admin|<reseller id>` on the paged list.
- **Reassignment** `POST /clients/:email/owner` (admin only): checks the new owner's client
  limit, clears `pool_disabled`, lists attached inbounds outside the new owner's allowed list,
  and never touches `used_traffic`. Disabling a client moved to a cut-off reseller is stage E.
- **Input stripping**: on create a reseller's `group`, `clientHostRuleIds` and `reverse` are
  dropped; on update they keep their stored values. Restored usage (`traffic`) is only read by
  the admin-only import, so it needs no stripping.
- **`StripForReseller`** removes, at any depth: `owner`, `group`, `groups`, `clientHostRuleIds`,
  `reverse`, `adTag`, `nodeId`, `nodeAddress`, `listen`, `shareAddr`, `shareAddrStrategy`,
  `awgServer`, `tuicServer`, `wgPublicKey`, `wgMtu`, `wgDns`, `mtprotoDomain`, `usageMultiplier`.
  Link building for resellers (stage D) will need a deliberate look at `shareAddr`.

Stage D details and corrections:

- **Navigation** is one list: `PANEL_ROUTES` (`frontend/src/routes/panelRoutes.tsx`) feeds the
  router, the sidebar, page titles and the command palette. `panelRoutesFor(reseller)` returns
  `RESELLER_ROUTES` (Home and Clients) for a reseller, and `buildRoutes(true)` adds a catch-all
  that sends any other path to Home. The doc's `roles` field on every route was not needed.
- **How the SPA knows the role**: the server adds `window.X_UI_ROLE="reseller"` to a reseller's
  page (`controller/dist.go`), so no admin request fires before the UI knows. It is a UI hint
  only; the API decides.
- **Separate reseller pages, not the admin `ClientsPage`**: `ResellerHome` and
  `ResellerClientsPage` are their own components. The admin page calls many admin-only routes
  (groups, onlines, links, HWIDs, bulk tools), so reusing it would mean gating dozens of calls
  inside it; separate pages leave the admin page as it was.
- **WebSocket**: the reseller layout does not open it and has no command palette; reseller pages
  poll `/reseller/me` and the paged list every 15 s and refetch after each change. The §3.5
  audience filter is therefore not built; the WebSocket stays admin-only.
- **Links and QR**: a reseller gets its clients' subscription link (`subURI + subId`) and its QR.
  `POST /setting/defaultSettings` is open to resellers with only subEnable, subURI, subJsonEnable,
  subJsonURI, subClashEnable, subClashURI, subTitle, datepicker, pageSize, expireDiff and
  trafficDiff. Per-inbound share links stay admin-only, so `StripForReseller` is unchanged and
  `shareAddr` stays hidden: a reseller sees no server address beyond what the subscription serves.
- **Self password change**: `POST /reseller/me/password` (current password required) ends the
  reseller's other sessions and keeps the current one by re-stamping its epoch.
- **Owner badge and filter** on the admin client list appear only once a reseller exists, so a
  panel without resellers looks exactly as before.
- **Not unit-testable**: RTL, phone width and dark theme; see the stage D report for what was
  looked at in a real browser.
