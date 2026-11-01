package service

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/usagemult"

	_ "github.com/mattn/go-sqlite3"
)

// Real databases written by stock panels (made with the stock binaries'
// own migrations, see deploy/test/upgrade/make-stock-fixture.sh).
var stockFixtures = []string{"stock-3.10.2.db", "stock-3.11.0.db"}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("SELECT name FROM pragma_table_info('%s')", table))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols
}

// dumpColumns renders every row of table, limited to cols, in rowid order.
func dumpColumns(t *testing.T, db *sql.DB, table string, cols []string) []string {
	t.Helper()
	q := fmt.Sprintf("SELECT %s FROM %s ORDER BY rowid", "quote("+strings.Join(cols, "), quote(")+")", table)
	rows, err := db.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join(vals, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func userTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// An in-place conversion opens a stock database with this build. Everything the
// stock panel stored must still be there and unchanged, and the multiplier
// switch must come up OFF so behaviour equals stock until the operator turns it on.
func TestStockDatabaseMigratesLosslesslyAndMultiplierStaysOff(t *testing.T) {
	for _, name := range stockFixtures {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XUI_DB_FOLDER", dir)
			stock := filepath.Join("..", "..", "..", "deploy", "test", "upgrade", "fixtures", name)
			orig := filepath.Join(dir, "x-ui.db.orig")
			copyFile(t, stock, orig)
			path := filepath.Join(dir, "x-ui.db")
			copyFile(t, stock, path)

			before := openRaw(t, orig)
			if got := tableColumns(t, before, "inbounds"); contains(got, "usage_multiplier") {
				t.Fatalf("fixture %s is not a stock schema: already has usage_multiplier", name)
			}

			if err := database.InitDB(path); err != nil {
				t.Fatalf("this build cannot open a %s database: %v", name, err)
			}
			t.Cleanup(func() { _ = database.CloseDB(); usagemult.SetActive(false) })
			// Close our handle's WAL so the raw reader below sees committed pages.
			if err := database.GetDB().Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
				t.Fatal(err)
			}
			after := openRaw(t, path)

			for _, table := range userTables(t, before) {
				cols := withoutUpdatedAt(tableColumns(t, before, table))
				want := dumpColumns(t, before, table, cols)
				got := dumpColumns(t, after, table, cols)
				if table == "settings" || table == "history_of_seeders" {
					// Startup may add default keys and newer seeder records; every stock row must stay.
					have := map[string]bool{}
					for _, r := range got {
						have[r] = true
					}
					for _, r := range want {
						if !have[r] {
							t.Errorf("settings row lost or changed: %s", maskSecret(r))
						}
					}
					continue
				}
				if table == "inbounds" || table == "clients" || table == "client_traffics" {
					// Startup may heal JSON blobs and normalise timestamps; identity
					// and ownership columns must not move.
					cols = identityColumns(table)
					want = dumpColumns(t, before, table, cols)
					got = dumpColumns(t, after, table, cols)
				}
				if strings.Join(want, "\n") != strings.Join(got, "\n") {
					t.Errorf("table %s changed by the migration\nbefore:\n%s\nafter:\n%s", table, strings.Join(want, "\n"), strings.Join(got, "\n"))
				}
			}

			// The new objects exist, are empty, and every stock inbound reads x1.
			if !contains(tableColumns(t, after, "inbounds"), "usage_multiplier") {
				t.Fatal("inbounds.usage_multiplier was not added")
			}
			var perInbound, weighted int
			if err := after.QueryRow("SELECT count(*) FROM client_inbound_traffics").Scan(&perInbound); err != nil || perInbound != 0 {
				t.Fatalf("client_inbound_traffics rows = %d (err %v), want 0", perInbound, err)
			}
			if err := after.QueryRow("SELECT count(*) FROM inbounds WHERE usage_multiplier IS NOT NULL AND usage_multiplier <> 1").Scan(&weighted); err != nil || weighted != 0 {
				t.Fatalf("%d stock inbounds got a multiplier other than x1 (err %v)", weighted, err)
			}

			on, err := (&SettingService{}).GetUsageMultiplierEnable()
			if err != nil {
				t.Fatal(err)
			}
			if on {
				t.Fatal("the per-inbound multiplier switch is ON after migrating a stock database; it must be OFF")
			}
			if usagemult.Active() {
				t.Fatal("the running multiplier mode is active after migration")
			}
		})
	}
}

func identityColumns(table string) []string {
	switch table {
	case "inbounds":
		return []string{"id", "user_id", "remark", "enable", "port", "protocol", "tag", "node_id", "up", "down", "total", "expiry_time"}
	case "clients":
		return []string{"id", "email", "sub_id", "uuid", "total_gb", "expiry_time", "enable"}
	default: // client_traffics
		return []string{"id", "inbound_id", "email", "up", "down", "total", "expiry_time", "enable"}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func maskSecret(row string) string {
	if strings.Contains(row, "secret") {
		return "<secret row>"
	}
	return row
}

// Upstream seeders stamp updated_at when they touch a row; that is bookkeeping, not data.
func withoutUpdatedAt(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c != "updated_at" {
			out = append(out, c)
		}
	}
	return out
}
