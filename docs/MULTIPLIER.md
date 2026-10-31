# Per-inbound usage multiplier

netron-panel can bill a client's traffic differently depending on which inbound
carried it. One client (one email, one subscription link) can sit on several
inbounds; each inbound has its own multiplier (for example `direct x1`,
`tunnel x1.2`). Real bytes on an inbound times that inbound's multiplier are
added to the client's **single** quota counter. Quota cut-off, expiry, resets,
the panel numbers, the Telegram/Discord reports and the `Subscription-Userinfo`
header all keep working through the stock code paths and show the weighted
number.

Example: 10 GB on a x1 inbound + 5 GB on a x1.2 inbound = 10 + 6 = **16 GB**
used. A client with a 16 GB quota is disabled at exactly that point.

## How it works

Xray keeps one traffic counter per user email, whatever the inbound. To weight
per inbound, the panel gives each (client, inbound) pair its own Xray identity,
a *stat email*:

```
nxm_<inboundId>_<16 lowercase base32 chars of sha256("<inboundId>:<lowercase email>")>
```

- **Config and hot changes.** While the mode is active, the generated Xray config and
  every live add/remove/update of a user hand Xray the stat email instead of
  the client's email, on supported protocols only.
- **Traffic.** Every 5 s the panel reads Xray's counters. A stat-email delta is
  checked against the clients really attached to the inbound it names. It is then
  multiplied by that inbound's multiplier (`math.Round`, per direction, per poll),
  recorded in `client_inbound_traffics` (real and billed bytes per inbound) and
  added to the client's normal counter. Everything after that is stock code.
- **Read-back.** Online clients, last-online, the IP limit, the dashboard speed
  column and the external traffic inform all map stat emails back to the
  client's email, with **real** bytes (they display or forward traffic; they
  do not bill it). The IP limit merges a client's addresses across its inbounds.
- **Routing rules.** A template routing rule that matches `"user": ["alice@x"]`
  automatically gains alice's stat emails, so it keeps matching.
- **Reset and delete.** A client reset (single, bulk, periodic, auto-renew,
  reset-all) or delete also clears that client's per-inbound rows.

Emails that are not stat emails take the stock path at x1. That covers counters
left over from before the switch, AmneziaWG/TUIC relays, and any unresolvable
string.

## Multiplier values

- Range `0` to `10`, any fraction (`0.5`, `1.2`, ...). Default `1`.
- `0` means **free**: real bytes are recorded per inbound, nothing is billed,
  and the client still shows as online.
- A multiplier change affects **future traffic only**. Counters already billed
  are never rewritten.
- An API call, import or node snapshot that does not carry the field keeps the
  stored value (on create: x1). It never becomes 0.

## Supported and unsupported protocols

| Protocol | Multiplier | Why |
| --- | --- | --- |
| VLESS, VMess, Trojan, Shadowsocks (multi-user), Hysteria, MASQUE | supported | clients are regular Xray users with an email |
| WireGuard | must stay 1 | not enabled in step 1 |
| TUIC | must stay 1 | traffic is metered by the panel's own TUIC relay, not Xray user stats |
| MTProto | must stay 1 | served by the `mtg-multi` sidecar, which also enforces quota on real bytes |
| AmneziaWG | must stay 1 | relayed through a loopback SOCKS inbound under the client email; kernel mode never reaches Xray |
| tunnel, http, mixed, tun | must stay 1 | no per-client users |

The API and UI reject any other value on these inbounds.

## The kill switch

Settings → **Enable per-inbound multiplier** (`usageMultiplierEnable`, default
**OFF**).

- **OFF** = stock 3x-ui behaviour: the same Xray config and the same emails.
  Multipliers can be stored but have no effect.
- Saving the switch does **not** apply it. It goes live on the next **forced
  Xray restart**: the "Restart Xray" button, the bot's restart command or a
  panel start. **All connections drop once** at that restart. A hot apply
  never switches emails under a running core.
- The new Xray process re-baselines its counters, so nothing is counted twice
  across the switch. Like any stock Xray restart, bytes moved between the last
  5 s poll and the restart are not counted.

## Nodes

Each node is a full panel that builds its own Xray config, so **each node does
its own per-inbound accounting** and the master only receives the node's
already-weighted totals.

Flow for a client on node N:

1. N's Xray reports stat-email deltas; N's panel bills them into N's
   `client_traffics[email]` (weighted). Inbound totals stay real bytes.
2. The master's traffic sync pulls N's inbound list. The node's email-keyed row
   appears once per inbound, so the master takes the **MAX** over them (never
   the sum). It subtracts the `node_client_traffics` baseline, adds the delta to
   its own counter and moves the baseline. **The master never multiplies node
   traffic again.**
3. Every 30 s the master pushes its own (weighted) totals to N
   (`client_global_traffics`); N shows and enforces MAX(local, master). Both are
   weighted, and N never folds them into what it reports, so there is no loop.
4. A reset on the master clears its row and the baseline and queues the reset
   for N; N's reset also clears N's per-inbound rows.

The multiplier is pushed to the node with the other inbound fields. A node
reports whether it applies it (`usage-mult` in its `X-3x-Node-Caps` header,
sent only while its running mode is ON). For an inbound with a multiplier ≠ 1
on a node that is an **old version or has the switch OFF**, the master:

- shows a warning on that inbound and in Settings (`/panel/api/setting/usageMultiplierStatus`);
- logs `usage multiplier: node <name> counts inbounds [...] at x1 (...)` once per change.

Traffic on such a node is billed at x1 until the node is upgraded and switched on.

## Limits

- `regexp:` entries in a routing rule's `user` list are left as written. They
  match stat emails only if the pattern happens to; rewrite such rules with
  explicit emails.
- Xray's access log, the Xray config viewer and the raw stats show stat emails
  (`nxm_...`), not client emails.
- The IP-limit temporary disconnect acts on the client's lowest-id inbound only
  (stock behaviour).
- MTProto's sidecar enforces quota on its own real bytes; a client shared
  between MTProto and a weighted inbound is cut by whichever limit trips first.
- The external traffic inform sends client emails with real bytes, as stock does.

## Before the first start: backup

The upgrade only adds a column (`inbounds.usage_multiplier`, default 1) and a
table (`client_inbound_traffics`). It never changes existing columns. Back up
anyway:

1. Stop the panel: `systemctl stop x-ui`.
2. SQLite: `cp /etc/x-ui/x-ui.db /etc/x-ui/x-ui.db.bak-$(date +%F)`.
   PostgreSQL: `pg_dump -Fc -f x-ui-$(date +%F).dump <dbname>`.
   Or use Panel → Backup & Restore → Export before stopping.
3. Install the new binary, start the panel and check the log for migration errors.

## Rollout order

1. Upgrade **the nodes first**, one at a time (back up each node DB as above).
   Leave their switch OFF for now.
2. Upgrade **the master**.
3. On each node: Settings → enable the switch → Restart Xray (connections drop once).
4. On the master: enable the switch → Restart Xray.
5. Only then set multipliers ≠ 1. The master warns for any node that does not
   apply them yet.

## Upgrade and rollback

- **Upgrade** is additive; existing counters are untouched.
- **Turn it off**: switch OFF → Restart Xray. Xray gets the plain emails again;
  multipliers stay stored but unused.
- **Roll back to stock 3x-ui**: switch OFF and restart first, then install the
  stock binary. The extra column and table are harmless to stock 3x-ui, which
  ignores them. Totals billed while the mode was on stay weighted; nothing is
  recalculated.

## Manual test plan

Use a test panel, not production.

1. Create client `mt-user` with a **16 GB** quota.
2. Create inbound **A** (VLESS, multiplier 1) and inbound **B** (VLESS,
   multiplier 1.2); attach `mt-user` to both.
3. Settings → enable the per-inbound multiplier → Restart Xray (confirm the
   warning that connections drop).
4. From a client app, download about **1 GB** through A, then about **1 GB** through B.
5. Panel → Clients → `mt-user`: usage ≈ 1 GB + 1.2 GB = **2.2 GB**.
6. Fetch the subscription with headers: `curl -sI https://<sub-host>/<subPath>/<subId>`.
   In `Subscription-Userinfo: upload=...; download=...; total=...; expire=...`,
   upload + download ≈ 2.2 GB and `total` = 16 GB.
7. Optional: set B to `0`, move 1 GB through B, and usage stays the same.
8. Optional: lower the quota to just above the current usage, move a little
   traffic through B, and the client is disabled when the weighted total crosses it.
9. Switch OFF → Restart Xray: new traffic on B now counts x1.

## Automated tests

```
make test-go                                   # unit + service + API tests (SQLite)
XUI_TEST_PG_DSN="host=... dbname=..." go test ./internal/database -run Postgres
XUI_DB_TYPE=postgres XUI_DB_DSN="host=... dbname=..." \
  go test ./internal/web/service -run '^TestUsageMultiplierPostgres$'
make node-e2e                                  # real master + node processes
XUI_NODE_E2E_OLD_BINARY=/path/to/stock-v3.11.0/x-ui make node-e2e   # + old-node case
```

The list of upstream files this feature touches is in
[NETRON_UPSTREAM_CHANGES.md](NETRON_UPSTREAM_CHANGES.md).
