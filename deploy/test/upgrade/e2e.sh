#!/bin/bash
# End-to-end conversion with REAL binaries: a stock x-ui running on a stock database
# is converted to the netron build, checked through its HTTP API, then rolled back.
#
#   XUI_E2E_NETRON_BINARY=/path/x-ui-netron \
#   XUI_E2E_STOCK_3_11_0=/path/x-ui-stock-v3.11.0 XUI_E2E_STOCK_3_10_2=/path/x-ui-stock-v3.10.2 \
#   deploy/test/upgrade/e2e.sh
#
# Stock binaries: `git worktree add /tmp/s v3.11.0 && mkdir -p /tmp/s/internal/web/dist && touch /tmp/s/internal/web/dist/.gitkeep && (cd /tmp/s && go build -o x-ui-stock .)`.
# Skips when the binaries are not given. Uses throwaway directories only.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

netron="${XUI_E2E_NETRON_BINARY:-}"
if [[ -z "${netron}" || ! -x "${netron}" ]]; then
    echo "skipped: set XUI_E2E_NETRON_BINARY (and XUI_E2E_STOCK_3_11_0 / XUI_E2E_STOCK_3_10_2)"
    exit 0
fi

api_token() { # prints a throwaway token created by the panel's own CLI in the sandbox DB
    XUI_DB_FOLDER="${DBDIR}" XUI_LOG_FOLDER="${R}/var/log/x-ui" "${MAIN}/x-ui" setting -getApiToken | awk '/apiToken:/ {print $2}'
}

e2e_case() { # stock-binary fixture label
    local stock="$1" fixture="$2" label="$3"
    CASE="e2e ${label}"
    echo "== ${CASE}"
    [[ -x "${stock}" ]] || { echo "   skipped (no stock binary for ${label})"; return 0; }
    local before_failn=${failn}
    setup "${label}" "${fixture}"
    export XUI_HEALTH_WAIT=60 XUI_HEALTH_STABLE=6
    cp "${stock}" "${MAIN}/x-ui"
    # release tarball built from the real netron binary and the repo's own x-ui.sh
    local arch d
    case "$(uname -m)" in aarch64 | arm64) arch=arm64 ;; *) arch=amd64 ;; esac
    d="${T}/release/build/x-ui"
    mkdir -p "${d}/bin"
    cp "${netron}" "${d}/x-ui"
    cp "${REPO}/x-ui.sh" "${d}/x-ui.sh"
    local ver
    ver="$("${netron}" -v)"
    tar -C "${T}/release/build" -czf "${T}/release/x-ui-linux-${arch}.tar.gz" x-ui
    (cd "${T}/release" && sha256sum "x-ui-linux-${arch}.tar.gz" > "x-ui-linux-${arch}.tar.gz.sha256")
    export STUB_LATEST_TAG="v${ver}"

    start_stock_panel
    assert_eq "$(panel_version)" "${label}" "real stock panel ${label} is the running version"
    "${STUBS}/systemctl" stop x-ui
    local db_before counts_before
    counts_before="$(db_counts)"
    start_stock_panel

    run_convert --yes
    assert_eq "${RC}" 0 "real conversion ${label} -> ${ver} succeeds (output: ${OUT: -600})"
    assert_eq "$(panel_version)" "${ver}" "real netron binary installed"
    assert_eq "$(db_counts)" "${counts_before}" "inbounds/clients/nodes/sub ids unchanged after the REAL migration"
    check "real migration added inbounds.usage_multiplier" test "$(sqlite3 -readonly "${DBDIR}/x-ui.db" "select count(*) from pragma_table_info('inbounds') where name='usage_multiplier'")" = 1
    local token body
    token="$(api_token)"
    body="$(curl -s -X POST -H "Authorization: Bearer ${token}" "http://127.0.0.1:${PORT}/fixture/panel/api/setting/usageMultiplierStatus")"
    assert_contains "${body}" '"configured":false' "running panel reports the multiplier switch OFF (saved)"
    assert_contains "${body}" '"active":false' "running panel reports the multiplier mode inactive"
    assert_eq "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/fixture/")" 200 "panel answers on the SAME port and base path"

    local b
    b="$(backup_dir)"
    db_before="$(cat "${b}/db/x-ui.db.sha256")"
    set +e
    OUT="$("${b}/rollback.sh" --yes 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 0 "rollback.sh succeeds (output: ${OUT: -300})"
    assert_eq "$(panel_version)" "${label}" "stock binary back"
    assert_eq "$(sha "${DBDIR}/x-ui.db")" "${db_before}" "database restored to the backup's exact bytes"
    local up=""
    for _ in $(seq 1 30); do
        # any HTTP answer: test stock binaries are built without the embedded UI, so they answer 500 on pages
        [[ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/fixture/")" != 000 ]] && up=1 && break
        sleep 1
    done
    assert_eq "${up}" 1 "restored stock panel answers again"
    teardown
    [[ ${failn} -eq ${before_failn} ]] && echo "   ok"
    return 0
}

e2e_case "${XUI_E2E_STOCK_3_11_0:-}" "${FIXTURE_DIR}/stock-3.11.0.db" 3.11.0
e2e_case "${XUI_E2E_STOCK_3_10_2:-}" "${FIXTURE_DIR}/stock-3.10.2.db" 3.10.2

echo
echo "passed checks: ${pass}, failed: ${failn}"
[[ ${failn} -eq 0 ]]
