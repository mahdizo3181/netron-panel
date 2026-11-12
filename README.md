<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./media/netron-logo-dark.svg">
    <img alt="netron panel" src="./media/netron-logo-light.svg" width="360">
  </picture>
</p>

<p align="center">
  <b>A 3x-ui fork with per-inbound traffic multipliers and one shared quota per client.</b>
</p>

<p align="center">
  <a href="https://github.com/mahdizo3181/netron-panel/releases"><img src="https://img.shields.io/github/v/release/mahdizo3181/netron-panel" alt="Release"></a>
  <a href="https://github.com/mahdizo3181/netron-panel/actions"><img src="https://img.shields.io/github/actions/workflow/status/mahdizo3181/netron-panel/release.yml.svg" alt="Build"></a>
  <a href="#"><img src="https://img.shields.io/github/go-mod/go-version/mahdizo3181/netron-panel.svg" alt="Go version"></a>
  <a href="https://github.com/mahdizo3181/netron-panel/releases/latest"><img src="https://img.shields.io/github/downloads/mahdizo3181/netron-panel/total.svg" alt="Downloads"></a>
  <a href="https://www.gnu.org/licenses/gpl-3.0.en.html"><img src="https://img.shields.io/badge/license-GPL--3.0-green.svg" alt="License"></a>
</p>

<p align="center">
  English · <a href="README.fa_IR.md">فارسی</a>
</p>

**netron-panel** is a web control panel for [Xray-core](https://github.com/XTLS/Xray-core) servers. It is a fork of [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) (base: `v3.11.0`) and keeps everything 3x-ui offers: multi-protocol inbounds, per-client quotas, multi-node, subscriptions, bots and an API. On top of that it adds the features below.

> [!NOTE]
> netron-panel is released under the same [GPL-3.0](LICENSE) license as the original. All credit for 3X-UI goes to [MHSanaei](https://github.com/MHSanaei) and its contributors. The upstream files this fork touches are listed in [docs/NETRON_UPSTREAM_CHANGES.md](docs/NETRON_UPSTREAM_CHANGES.md).

## What netron-panel adds

### Per-inbound usage multiplier, one shared quota

One client (one email, one subscription link) can sit on several inbounds, and each inbound gets its own multiplier, for example `direct ×1` and `tunnel ×1.2`. The real bytes moved on an inbound, times that inbound's multiplier, are added to the client's **single** quota counter.

> 10 GB on a ×1 inbound + 5 GB on a ×1.2 inbound = 10 + 6 = **16 GB** used. A client with a 16 GB quota is cut off at exactly that point.

- **No changes for end users.** Subscription links stay the same and there are no duplicate clients. Quota cut-off, expiry, resets, the numbers in the panel, bot reports and the `Subscription-Userinfo` header all use the usual code paths and show the weighted number.
- **Range `0` to `10`, any fraction.** Default `1`. `0` means free: traffic is recorded but not billed.
- **Future traffic only.** Changing a multiplier never rewrites what was already billed.
- **Real and billed bytes per inbound** are stored side by side, so inbound totals stay real bytes.
- **Multi-node aware.** Each node does its own accounting and the master receives already-weighted totals, never multiplying them twice. The master warns about any node that is too old or has the mode off.
- **Supported protocols:** VLESS, VMess, Trojan, Shadowsocks (multi-user), Hysteria, MASQUE. WireGuard, TUIC, MTProto, AmneziaWG and tunnel/http/mixed/tun inbounds must stay at ×1.

**Safe by default.** The mode is behind a switch, **Settings → Enable per-inbound multiplier**, which is **OFF** by default. OFF behaves exactly like stock 3x-ui. Saving the switch does not apply it: it goes live on the next forced Xray restart, and all connections drop once at that moment. The Settings page shows `Saved` and `Running` states so you can see what is actually active.

How it works, the rollout order for multi-node setups, limits and a manual test plan: **[docs/MULTIPLIER.md](docs/MULTIPLIER.md)**.

### Info config that Happ can import

With "Separate Info Config / Dummy Node" on, a subscription starts with a fake entry showing remaining data and days (and is the only entry for expired or depleted clients). Newer Happ versions reject subscriptions with a SOCKS entry, so netron-panel uses a non-functional VLESS entry by default. The old SOCKS entry is one setting away. See **[docs/INFO_CONFIG.md](docs/INFO_CONFIG.md)**.

### In-place conversion of a stock 3x-ui

Already running stock 3x-ui? Run the install command below on that server and it converts the installation in place: sensitive-data-safe backup first, health check afterwards, **automatic rollback** if anything fails, and `x-ui rollback` to go back later. It refuses Docker, PostgreSQL, partial or too-old installs instead of guessing. The database migration itself is one-way, so keep the backup. See **[docs/UPGRADE_FROM_STOCK.md](docs/UPGRADE_FROM_STOCK.md)**.

### Releases come from this repository

`install.sh`, `update.sh`, the `x-ui` menu and the panel's Update button all fetch from `mahdizo3181/netron-panel`. Every release ships linux `amd64` and `arm64` tarballs with `.sha256` sums that the installer verifies.

> [!WARNING]
> **Never run the stock upstream installer or updater on a netron server.** It would replace the binary with stock 3x-ui, which ignores multipliers and bills every inbound at ×1.

## Quick Start

On Ubuntu/Debian (amd64 or arm64), as root:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh)
```

To install a specific release, append its tag (e.g. `v3.11.0-netron.5`):

```bash
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh) v3.11.0-netron.5
```

netron-panel has no rolling dev build. During installation a random username, password and access path are generated. Afterwards run `x-ui` to open the management menu: start/stop the service, view or reset credentials, manage SSL certificates and more.

For general panel usage (inbounds, clients, routing, API) the upstream documentation at [docs.sanaei.dev](https://docs.sanaei.dev) still applies, except where this README or the netron docs say otherwise.

### Unattended install

The installer also runs **non-interactively** for cloud-init. Set `XUI_NONINTERACTIVE=1` (or pipe with no TTY) and it installs end-to-end with zero prompts, generating random credentials and writing them to `/etc/x-ui/install-result.env`. See [`deploy/`](deploy/) for:

- [Cloud-init user-data](deploy/cloud-init/): unattended install on any cloud
- [Hetzner Cloud notes](deploy/marketplace/hetzner/)

## Features inherited from 3x-ui

- **Multi-protocol inbounds:** VLESS, VMess, Trojan, Shadowsocks, WireGuard, AmneziaWG, TUIC v5, Hysteria2, MTProto, HTTP, SOCKS (Mixed), Dokodemo-door / Tunnel and TUN.
- **Modern transports and security:** TCP (Raw), mKCP, WebSocket, gRPC, HTTPUpgrade and XHTTP, secured with TLS, XTLS and REALITY.
- **AmneziaWG built in:** DPI-resistant WireGuard on a userspace network stack, no kernel module or DKMS.
- **Native TUIC v5 server** with Xray routing and per-client accounting.
- **MTProto proxies** with per-client FakeTLS secrets, ad-tags and quotas, applied live.
- **Fallbacks:** several protocols on one port (e.g. VLESS and Trojan on 443).
- **Per-client management:** quotas, expiry, IP limits, HWID device limits, scheduled renewals, online status, share links, QR codes and subscriptions.
- **Traffic statistics** per inbound, client and outbound, with reset controls.
- **Multi-node support** from a single panel, including cloning inbounds onto other nodes.
- **Outbound and routing:** WARP, NordVPN, PIA, custom rules, load balancers and proxy chaining.
- **Built-in subscription server:** raw, JSON and Clash output chosen by User-Agent, plus [custom page templates](docs/custom-subscription-templates.md).
- **Telegram and Discord bots** for remote monitoring and management.
- **RESTful API** with scoped, optionally expiring tokens.
- **Installable panel (PWA)**, **SQLite or PostgreSQL**, **14 UI languages**, dark and light themes, and **Fail2ban** integration for IP limits.

## Screenshots

<details>
<summary>Click to expand</summary>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/01-overview-dark.png">
  <img alt="Overview" src="./media/01-overview-light.png">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/02-add-inbound-dark.png">
  <img alt="Inbounds" src="./media/02-add-inbound-light.png">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/03-add-client-dark.png">
  <img alt="Add client" src="./media/03-add-client-light.png">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/05-add-nodes-dark.png">
  <img alt="Nodes" src="./media/05-add-nodes-light.png">
</picture>

</details>

## Supported Platforms

**Operating systems:** Ubuntu, Debian, Armbian, Fedora, CentOS, RHEL, AlmaLinux, Rocky Linux, Oracle Linux, Amazon Linux, Virtuozzo, Arch, Manjaro, Parch, openSUSE (Tumbleweed / Leap) and Alpine.

**Architectures:** netron-panel releases are published for Linux `amd64` and `arm64` only.

## Database Options

- **SQLite** (default): a single file at `/etc/x-ui/x-ui.db`. Zero setup, ideal for small and medium deployments.
- **PostgreSQL:** for high client counts or multi-node setups. The installer can install it locally or accept a DSN. Note that the in-place conversion from stock 3x-ui does not support PostgreSQL installs.

The installer writes the backend settings to `/etc/default/x-ui`:

```
XUI_DB_TYPE=postgres
XUI_DB_DSN=postgres://xui:password@127.0.0.1:5432/xui?sslmode=disable
```

### Migrating SQLite to PostgreSQL

```bash
x-ui migrate-db --dsn "postgres://xui:password@127.0.0.1:5432/xui?sslmode=disable"
# then set XUI_DB_TYPE and XUI_DB_DSN in /etc/default/x-ui and restart:
systemctl restart x-ui
```

The source SQLite file is left untouched; remove it manually once the new backend is verified.

### Docker

netron-panel publishes **no Docker image** (`ghcr.io/mhsanaei/3x-ui` is stock 3x-ui and ignores multipliers). Build this fork's image locally with `docker compose build`; the bundled `docker-compose.yml` builds from your checkout. To use the bundled PostgreSQL service, uncomment the two `XUI_DB_*` lines and run `docker compose --profile postgres up -d`.

The image bundles Fail2ban to enforce IP limits, which needs `NET_ADMIN`. `docker-compose.yml` already grants it; with `docker run`, add `--cap-add=NET_ADMIN --cap-add=NET_RAW` yourself.

## Environment Variables

| Variable | Description | Default |
| --- | --- | --- |
| `XUI_DB_TYPE` | Database backend: `sqlite` or `postgres` | `sqlite` |
| `XUI_DB_DSN` | PostgreSQL connection string (when `XUI_DB_TYPE=postgres`) | — |
| `XUI_DB_FOLDER` | Directory for the SQLite database file | `/etc/x-ui` |
| `XUI_DB_MAX_OPEN_CONNS` | Maximum open connections (PostgreSQL pool) | — |
| `XUI_DB_MAX_IDLE_CONNS` | Maximum idle connections (PostgreSQL pool) | — |
| `XUI_INIT_WEB_BASE_PATH` | The initial URI path for the web panel | `/` |
| `XUI_ENABLE_FAIL2BAN` | Enable Fail2ban-based IP-limit enforcement | `true` |
| `XUI_LOG_LEVEL` | Log verbosity (`debug`, `info`, `warning`, `error`) | `info` |
| `XUI_DEBUG` | Enable debug mode | `false` |
| `XUI_TUNNEL_HEALTH_MONITOR` | Tunnel health monitor (probes a URL and restarts xray after repeated failures; a restart drops all clients) | `false` |
| `XUI_TUNNEL_HEALTH_PROXY` | Proxy the probe goes through, e.g. `socks5://127.0.0.1:1080`. Empty means the probe only checks host connectivity | — |
| `XUI_TUNNEL_HEALTH_URL` | URL probed for tunnel health | `https://www.cloudflare.com/cdn-cgi/trace` |
| `XUI_TUNNEL_HEALTH_INTERVAL` | Interval between probes | `30s` |
| `XUI_TUNNEL_HEALTH_TIMEOUT` | Per-probe timeout | `10s` |
| `XUI_TUNNEL_HEALTH_FAILURES` | Consecutive failures before a restart | `3` |
| `XUI_TUNNEL_HEALTH_COOLDOWN` | Minimum delay between restarts | `5m` |
| `NODE_TOKEN_ENCRYPTION` | Encryption at rest for node API tokens: `off`, `migration` or `required` (no `XUI_` prefix) | `off` |
| `XUI_NODE_TOKEN_KEY_FILE` | JSON keyring (mode `0600`) with the active key id and base64 32-byte keys | `/etc/x-ui/node_token_key.json` |
| `XUI_NODE_TOKEN_KEY` | A single base64 32-byte key, used only if the key file cannot be loaded | — |

The complete upstream list is on the [environment variables reference](https://docs.sanaei.dev/docs/reference/env-vars).

## Documentation

| Document | Contents |
| --- | --- |
| [docs/MULTIPLIER.md](docs/MULTIPLIER.md) | Multiplier design, kill switch, nodes, rollout order, limits, test plan, cutting a release |
| [docs/INFO_CONFIG.md](docs/INFO_CONFIG.md) | Dummy info entry in subscriptions (VLESS or SOCKS) |
| [docs/UPGRADE_FROM_STOCK.md](docs/UPGRADE_FROM_STOCK.md) | In-place conversion, backup, rollback, dry run |
| [docs/NETRON_UPSTREAM_CHANGES.md](docs/NETRON_UPSTREAM_CHANGES.md) | Every upstream file this fork changes (checklist for rebasing) |

## Supported Languages

The panel UI is available in 14 languages: English · فارسی · العربية · 中文（简体） · 中文（繁體） · Español · Русский · Українська · Türkçe · Tiếng Việt · 日本語 · Bahasa Indonesia · Português (Brasil) · Slovenčina

## Contributing

Contributions are welcome. Please read the [Contributing Guide](CONTRIBUTING.md) before opening an issue or pull request. Bugs in stock 3x-ui behaviour may be better reported [upstream](https://github.com/MHSanaei/3x-ui/issues).

## Credits and Acknowledgment

- [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) and its contributors, the project this fork is built on.
- [alireza0](https://github.com/alireza0/), for the original 3x-ui work.
- [Iran v2ray rules](https://github.com/chocolate4u/Iran-v2ray-rules) (GPL-3.0): routing rules with built-in Iranian domains, security and adblocking.
- [Russia v2ray rules](https://github.com/runetfreedom/russia-v2ray-rules-dat) (GPL-3.0): automatically updated routing rules for blocked domains and addresses in Russia.

## Community Tools (for 3x-ui)

- [terraform-provider-3x-ui](https://github.com/batonogov/terraform-provider-threexui) (MIT): manage inbounds, clients and settings as code with Terraform / OpenTofu.
- [3X-UI Manager](https://github.com/yukh975/3X-UI-Manager) (MIT): native Android client for 3x-ui.

> [!IMPORTANT]
> This project is intended for personal use. Do not use it for illegal purposes.

## License

[GPL-3.0](LICENSE). This is a derivative work of [3x-ui](https://github.com/MHSanaei/3x-ui) by MHSanaei.
