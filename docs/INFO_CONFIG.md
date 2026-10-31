# Subscription info config link type

When **Settings → Subscription → Separate Info Config / Dummy Node** is on, the
subscription starts with a fake entry whose name shows the client's remaining
data and days. When the client is expired or out of data, that entry is the
**only** one returned, and its name comes from the *Expired Template* or the
*Traffic Depleted Template*.

The entry exists only to show text. It can never carry traffic: it points at
`127.0.0.1` port 1 (or 1080 for SOCKS) with the all-zero UUID. It is not a
client: it never appears in client lists, traffic or stats, and the
`Subscription-Userinfo` header is computed from the real clients only.

## The option

**Info Config Link Type** sits right below the info config switch.

| Value | Raw / Base64 subscription | JSON subscription |
| ----- | ------------------------- | ----------------- |
| `vless` (default) | `vless://00000000-0000-0000-0000-000000000000@127.0.0.1:1?encryption=none&security=none&type=tcp#<remark>` | VLESS outbound to `127.0.0.1:1`, id `00000000-…`, `encryption: none`, tcp, no security |
| `socks` (legacy) | `socks://127.0.0.1:1080#<remark>` | SOCKS outbound to `127.0.0.1:1080` |

The `socks` value produces exactly the output older netron-panel versions sent.

**Why VLESS is the default:** newer Happ versions reject a subscription that
contains a SOCKS entry and fail the whole import. They accept a VLESS entry.

**Clash / Mihomo** subscriptions always use the SOCKS5 dummy, whatever this
option says. Every Clash core reads `socks5`, but the original Clash core
(served by the legacy Clash mode) has no VLESS. The dummy is left out of the
proxy groups whenever real proxies exist, so it is never auto-selected.

Existing installs switch to `vless` on upgrade, because the setting was never
stored before and its default is `vless`.

## Switching back to SOCKS

Only needed for a client that cannot read a VLESS entry.

- Panel: **Settings → Subscription → Info Config Link Type → SOCKS (legacy)**,
  then save.
- API: send `subInfoNodeLinkType: "socks"` with the rest of the settings to
  `POST /panel/api/setting/update`. Any value other than `vless`, `socks` or
  empty (= `vless`) is rejected.

Subscriptions pick the new value up on their next fetch; no restart is needed.
