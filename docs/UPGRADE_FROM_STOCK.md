# Upgrade a stock 3x-ui to netron-panel (in place)

For a server that already runs the stock MHSanaei/3x-ui panel with real users.
One command converts it to netron-panel on the same server. Users, inbounds,
subscription links, credentials, port and base path stay as they are.

## The one command

Run as root on the server:

```
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh)
```

The installer sees the existing panel and switches to the upgrade flow by
itself. It never reinstalls over it. Add `--yes` for no prompts (automation),
`--force-old` to accept a panel older than 3.10.2, or a tag to pin the version:

```
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh) v3.11.0-netron.5 --yes
```

To look first without changing anything:

```
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/upgrade-from-stock.sh) detect
```

## What it does, in order

1. **Detects** (read-only) and prints a report: install dir, database and its
   engine, systemd unit, running or not, panel version, stock or already
   netron, node count, whether the Telegram bot is on. It never prints
   passwords, tokens, the database DSN or your base path.
2. **Decides.**
   - no panel found: normal fresh install;
   - stock panel: the conversion below;
   - already netron-panel: the normal updater, nothing converted;
   - Docker, PostgreSQL, a partial install, no systemd, or an unreadable
     version: **refuses and changes nothing** (see "What it refuses");
   - stock older than 3.10.2: stops and asks for `--force-old`.
3. **Pre-flight**: root, SQLite `PRAGMA integrity_check` (a damaged database is
   never converted), disk space, release download with SHA-256 check. All of this
   happens before anything is touched.
4. **Shows the plan** and asks for confirmation. If the panel has nodes, it asks
   once more that you understand the node order below.
5. **Backs up** to `/root/xui-backup-<time>/` (see below) and refuses to
   continue if any part fails.
6. **Stops** the service, **swaps** the panel binary and menu script, **starts**.
7. **Health check** for up to 30 s: service active and stable, panel port
   listening, the panel answers HTTP, no migration/startup error in the journal
   or panel log, the multiplier switch is OFF. If any check fails it **rolls
   back automatically** and says why.

Downtime is the stop-to-healthy window, normally well under a minute. Running
connections on Xray are interrupted during it.

## If your connection drops or you press Ctrl-C

Run the upgrade **inside `tmux` or `screen`** (or with `nohup`) when you are on
SSH; the script prints a one-line tip when it sees you are not. It never blocks on it.

Independently of that, the destructive part protects itself:

- From just before the panel is stopped until the upgrade is decided
  (converted, or rolled back), the script **ignores** SIGHUP (SSH drop), SIGINT
  (Ctrl-C), SIGTERM and a dead terminal. It prints this before it starts. Do not
  power off the server during it; the window is normally under a minute.
  Ignoring is inherited by `systemctl`, `tar` and `cp`, so a hangup does not kill
  those mid-operation either.
- It records how far it got (stopped, swapped, started). If it nevertheless
  exits abnormally after stopping the panel (a failing command, disk full),
  its exit handler **undoes what was done** and prints what it did: before the
  swap it restarts the old panel; after, it rolls back to the backup.
- Every `systemctl` call it makes has a deadline (120 s for stop/start, 10 s for
  status checks; `XUI_SYSTEMCTL_TIMEOUT` and `XUI_SYSTEMCTL_CHECK_TIMEOUT` override).
  A wedged systemd therefore cannot freeze the upgrade forever: a timeout counts as
  a failure and takes the same recovery path (restart the old panel, or roll back).
- Before the window (download, backup, prompts), a signal just exits cleanly:
  nothing has been changed.
- `kill -9` and power loss cannot be caught by any program. For those, a
  marker file `IN_PROGRESS` stays in the backup directory. `detect` and the next
  `convert` report it and refuse to proceed until you restore with
  `/root/xui-backup-<time>/rollback.sh` (a stale lock from the dead run does not
  block that). If it died before the final backup step nothing was replaced, and
  the message says to just start the panel again.

## Kept and replaced

| Kept exactly as is | Replaced |
| --- | --- |
| database and every setting (users, inbounds, clients, subscription ids and settings, nodes, credentials, port, base path) | the panel binary `/usr/local/x-ui/x-ui` |
| TLS certificates and their paths | `/usr/local/x-ui/x-ui.sh` and the `x-ui` menu command |
| Xray config template, `bin/` (Xray binary, geo files, anything you added) | |
| the systemd unit, its drop-ins and the service env file (`/etc/default/x-ui`) | |

Files the stock install lacked in `bin/` (for example the MTProto binary) are
added; nothing in `bin/` is overwritten. Other services, firewall rules,
tunnels and ports are never touched. No `apt upgrade` is run.

The per-inbound multiplier switch stays **OFF**, so the panel behaves like stock
until you turn it on yourself.

## Backup and rollback

Each conversion writes `/root/xui-backup-<time>/`. **It is sensitive**: it holds
your panel database (password hashes, API tokens, Telegram/Discord bot tokens,
node tokens, the session secret) and copies of your TLS **private keys**. It is
created with `umask 077` and then locked down (`chmod -R go-rwx`): nothing in it is readable by anyone but root. It is still
a full copy of your secrets on the same disk as the panel. After you have
confirmed the upgrade is good, **move it off the server** (for example
`tar czf - /root/xui-backup-<time> | gpg -c > xui-backup.tgz.gpg`, then copy that
elsewhere) **or delete it** (`rm -r /root/xui-backup-<time>`). Keep it only for
as long as you may still want to roll back. Never put it in a public place.

| File | What |
| --- | --- |
| `db/x-ui.db` (+ `-wal`, `-shm`) | the exact database bytes at the moment of conversion |
| `db/x-ui.db.live-snapshot` | a consistent snapshot taken while the panel still ran |
| `install-dir.tar.gz` | the whole `/usr/local/x-ui` |
| `x-ui.service`, `x-ui.service.d/`, `service-env`, `usr-bin-x-ui` | unit, drop-ins, env file, stock menu script |
| `certs/` | the certificates your settings point to |
| `rollback.sh`, `upgrade-from-stock.sh`, `SHA256SUMS`, `manifest.env` | rollback helper and integrity data |

Roll back at any time:

```
x-ui rollback                                  # newest backup
/root/xui-backup-<time>/rollback.sh            # a specific backup; works offline
```

It restores the stock binary, the stock menu script **and the database**, then
restarts the panel. After a rollback the `x-ui rollback` command no longer
exists (it is part of the netron menu); use the `rollback.sh` path above.

> **Warning: the database migration is one way.** The first start of the new
> binary adds a column and a table to the database. An older stock binary
> will not be happy on that database, so going back to an older stock version
> needs the database **from the backup**, not just the old binary. Rollback does
> exactly that.
>
> **Rolling back loses everything written after the backup**: traffic counted,
> clients added, edits made. Immediately after a failed upgrade that is
> seconds of data. Days later it is days. Decide with that in mind.

## What it refuses (and nothing is changed)

- **Docker installs.** The conversion is verified for the native systemd install
  only. For Docker, replace the image instead.
- **PostgreSQL** (or any engine other than SQLite). Not verified for in-place
  conversion.
- **Partial installs** (binary without unit, or the reverse), an unreadable
  version, or a missing database. It will not guess.
- **No systemd** (for example Alpine/OpenRC), or an architecture other than
  amd64/arm64.
- **A release download that fails its checksum**, or a binary that does not
  identify as netron.
- **Stock older than 3.10.2** unless you pass `--force-old`. 3.10.2 and 3.11.0
  are the stock versions whose databases were migrated and compared row by row
  in the tests.

Non-default install or database locations (read from the unit and
`/etc/default/x-ui`) are honoured and shown in the report.

## Before you start (checklist)

- [ ] Run `detect` (above) and read the report. Is it the panel you mean?
- [ ] Do the dry run below on a throwaway server first. This is not optional for a
      panel with real users.
- [ ] You have a second backup of your own: `x-ui` menu, or copy
      `/etc/x-ui/x-ui.db` (stop the panel first for a clean copy).
- [ ] Free disk space of roughly twice the install dir plus three times the database.
- [ ] You know which of your nodes are panels (see the node order) and will
      upgrade them first.
- [ ] A quiet hour. Connections drop during the swap.

## After the upgrade (checklist)

- [ ] `x-ui status` shows running, and `/usr/local/x-ui/x-ui -v` prints `3.11.0-netron.N`.
- [ ] You can log in with the **same** credentials, port and base path.
- [ ] Inbounds and client counts look the same; open one subscription link.
- [ ] Settings → "Enable per-inbound multiplier" is **off**.
- [ ] Keep `/root/xui-backup-<time>/` only while you may still roll back, then move it
      off the server (encrypted) or delete it: it contains your database and TLS
      private keys.
- [ ] Do not run the stock upstream installer/updater again on this server: it
      would put stock 3x-ui back (the netron `x-ui update` is the right one).

## Node order

Every node is its own full panel and does its own per-inbound accounting; the
master only receives its totals. So:

1. **Upgrade the nodes first**, one by one: run the same command on each node.
2. **Upgrade the master last.**
3. Only when every node is upgraded: in Settings turn on "Enable per-inbound
   multiplier", save, and restart Xray (all connections drop once). Then set
   multipliers on inbounds.

A stock node ignores multipliers and counts everything at x1. The master shows a
warning in Settings and logs it once for every node in that state, so nothing is
silently under-counted. Details: [MULTIPLIER.md](MULTIPLIER.md).

## Dry run on a throwaway server

Goal: convert a **copy** of your real database on a disposable server, with
everything that talks to the outside world switched off.

1. **Throwaway server**, same OS family. Install the *stock* panel of the **same
   version** your production runs (this is the one place the upstream installer
   is fine, because the server is disposable):
   `bash <(curl -Ls https://raw.githubusercontent.com/MHSanaei/3x-ui/master/install.sh) v3.11.0`
2. **Copy the production database without touching production**, on production:
   `sqlite3 /etc/x-ui/x-ui.db ".backup '/root/rehearsal.db'"` (consistent even
   while the panel runs), then `scp` it to the throwaway server.
3. **Put it in place and neutralise the outside world.** `x-ui stop`, then
   `cp /root/rehearsal.db /etc/x-ui/x-ui.db` and run this (keys are the panel's
   real setting names; `settings.key` is not unique, so delete before inserting):

   ```
   sqlite3 /etc/x-ui/x-ui.db <<'SQL'
   DELETE FROM settings WHERE key IN (
     'tgBotEnable','tgBotBackup','tgBotToken',
     'discordBotEnable','discordBotBackup','discordBotToken',
     'smtpEnable','externalTrafficInformEnable','externalTrafficInformURI',
     'ldapEnable','webCertFile','webKeyFile','subCertFile','subKeyFile');
   INSERT INTO settings (key, value) VALUES
     ('tgBotEnable','false'),('tgBotBackup','false'),('tgBotToken',''),
     ('discordBotEnable','false'),('discordBotBackup','false'),('discordBotToken',''),
     ('smtpEnable','false'),
     ('externalTrafficInformEnable','false'),('externalTrafficInformURI',''),
     ('ldapEnable','false'),
     ('webCertFile',''),('webKeyFile',''),('subCertFile',''),('subKeyFile','');
   UPDATE nodes SET enable = 0;
   SQL
   ```

   This turns off the Telegram bot and its backups, the Discord bot, e-mail,
   the external traffic-inform URL, LDAP sync, and every node (so the rehearsal
   cannot push to or pull from your real nodes). Certificates are cleared
   because the files do not exist on this server.
   Then `x-ui start`, and confirm you can log in with your production credentials.
4. **Rehearse the conversion**: run the one command from the top of this page.
   Read the report, the plan and the final summary. Then check the "after"
   checklist, and try `x-ui rollback` once to see the stock panel come back.
5. **Rehearse the failure path** (optional): rerun with a tag whose binary
   cannot start, or break the database on purpose, and watch the automatic
   rollback restore the original database.

### A pre-release tag for the rehearsal

To rehearse an exact build before it becomes "latest", cut a **pre-release**
tag (the installer and the update check ignore pre-releases):

```
git tag v3.11.0-netron.5-rc.1 && git push origin v3.11.0-netron.5-rc.1
```

Then on the throwaway server pass it explicitly:

```
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh) v3.11.0-netron.5-rc.1 --yes
```

Delete it afterwards with `gh release delete v3.11.0-netron.5-rc.1 --cleanup-tag`.
See [MULTIPLIER.md](MULTIPLIER.md#release-dry-run-do-this-first) for the release dry run.

## Update notifier

The panel's own "check for updates" and Update button use this repository's
releases (and its `update.sh`), never MHSanaei's. The update script also refuses to
run on a stock panel, so a stock server cannot be half-converted by it.

## How this is tested

`deploy/test/upgrade/run.sh` (no root, no network, no real systemd) covers a
fresh server, stock to netron, already netron, Docker and PostgreSQL refusals,
backup failure, bad checksum, health failure in four ways (each rolls back to
the exact original database bytes), running twice, non-interactive and `--yes`,
old versions, the `rollback` command, a relocated database and a panel bound to
a specific address, and interruption safety: HUP, INT and TERM sent at each point
of the destructive window (after the stop, after the swap, after the start) all
end in the fully converted healthy panel; an abnormal exit after the stop, or with
the files half replaced, ends in the original panel with the exact original database
bytes (never stopped, never half swapped); `kill -9` leaves a marker that blocks a
blind retry and `rollback.sh` recovers; a dead terminal does not abort a running
conversion; a `systemctl` that hangs forever (on stop, or on start after the swap)
times out and ends in the original panel, running. `deploy/test/upgrade/e2e.sh` converts the **real** stock
3.10.2 and 3.11.0 binaries on real stock databases to the real netron binary and
checks the running panel's API. The Go test
`TestStockDatabaseMigratesLosslesslyAndMultiplierStaysOff` compares every table of
the stock databases before and after the migration.

Not covered by an automated test (read and reviewed only): PostgreSQL
conversion (refused by design), Alpine/OpenRC, arm64 hardware, and the
`x-ui rollback` menu wrapper itself (the `rollback.sh` it calls is tested).
