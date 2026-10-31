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

## New files (no rebase conflict expected)

- `internal/database/model/usage_multiplier.go` (+ test)
- `internal/database/usage_multiplier_migration_test.go`
