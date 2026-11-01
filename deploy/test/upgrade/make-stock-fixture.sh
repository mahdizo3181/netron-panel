#!/bin/bash
# Builds a synthetic "stock panel" database with a STOCK x-ui binary's own
# migrations and API, then saves a vacuumed copy. Usage:
#   make-stock-fixture.sh <stock x-ui binary> <out.db> [port]
# Every value is made up; nothing here comes from a real server.
set -euo pipefail

bin="${1:?stock x-ui binary}"
out="${2:?output .db}"
[[ ! -e "$out" ]] || { echo "refusing to overwrite $out; remove it first" >&2; exit 1; }
port="${3:-29053}"
work="$(mktemp -d)"
pid=""
trap '[[ -n "${pid}" ]] && kill "${pid}" 2>/dev/null; rm -rf "$work"' EXIT
mkdir -p "$work/db" "$work/log" "$work/bin"
export XUI_DB_FOLDER="$work/db" XUI_LOG_FOLDER="$work/log" XUI_BIN_FOLDER="$work/bin"

"$bin" setting -username fixture -password fixture-not-a-secret -port "$port" -webBasePath /fixture/ > /dev/null
token="$("$bin" setting -getApiToken | awk '/apiToken:/ {print $2}')"
"$bin" run > "$work/run.log" 2>&1 &
pid=$!
for _ in $(seq 1 30); do
    curl -s -o /dev/null "http://127.0.0.1:${port}/fixture/" && break
    sleep 1
done

api() { curl -sf -H "Authorization: Bearer ${token}" -X POST "http://127.0.0.1:${port}/fixture/panel/api/$1" "${@:2}" > /dev/null; }
client() { # email subId totalGB expiryMs uuid
    printf '{"id":"%s","email":"%s","subId":"%s","totalGB":%s,"expiryTime":%s,"enable":true,"limitIp":0,"flow":"","tgId":0,"reset":0,"password":"fixture-pass-%s"}' "$5" "$1" "$2" "$3" "$4" "$2"
}
inbound() { # remark protocol port clients-json
    api inbounds/add --data-urlencode "remark=$1" --data-urlencode "protocol=$2" --data-urlencode "port=$3" \
        --data-urlencode "enable=true" --data-urlencode "settings={\"clients\":[$4],\"decryption\":\"none\"}" \
        --data-urlencode 'streamSettings={"network":"tcp","security":"none","tcpSettings":{"header":{"type":"none"}}}' \
        --data-urlencode 'sniffing={}'
}
gb=1073741824
inbound fixture-vless vless 31001 "$(client alice@fixture sub-alice $((50 * gb)) 0 00000000-0000-4000-8000-000000000001),$(client bob@fixture sub-bob $((20 * gb)) 4102444800000 00000000-0000-4000-8000-000000000002)"
inbound fixture-vmess vmess 31002 "$(client alice@fixture sub-alice $((50 * gb)) 0 00000000-0000-4000-8000-000000000001),$(client carol@fixture sub-carol 0 0 00000000-0000-4000-8000-000000000003)"
inbound fixture-trojan trojan 31003 "$(client dave@fixture sub-dave $((5 * gb)) 0 00000000-0000-4000-8000-000000000004)"

kill "$pid"; wait "$pid" 2>/dev/null || true
pid=""

db="$work/db/x-ui.db"
sqlite3 "$db" "PRAGMA wal_checkpoint(TRUNCATE);" > /dev/null
sqlite3 "$db" "DELETE FROM api_tokens;"
# One node row so conversions of a multi-node master are exercised; all fields are fake.
sqlite3 "$db" "INSERT INTO nodes (name, scheme, address, port, enable, status, tls_verify_mode, guid) VALUES ('fixture-node', 'https', 'node.invalid', 2053, 1, 'offline', 'verify', 'fixture-node-guid');"
sqlite3 "$db" "UPDATE settings SET value = 'fixture-session-secret-not-real00' WHERE key = 'secret';"
sqlite3 "$db" "VACUUM INTO '$out'"
echo "wrote $out ($(stat -c %s "$out") bytes) from $("$bin" -v)"
