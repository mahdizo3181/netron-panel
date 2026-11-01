#!/bin/bash
# Tests for upgrade-from-stock.sh. Usage: run.sh [case-name-filter]
# Needs: bash, sqlite3, python3, curl, tar. No root, no network, no real systemd.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SECRET_SESSION="fixture-session-secret-not-real00"
SECRET_BASEPATH="/fixture/"

# ---------------------------------------------------------------- cases

case_fresh_server_is_left_alone() {
    setup
    rm -rf -- "${MAIN}" "${R}/etc/systemd/system/x-ui.service" "${R}/etc/systemd/system/x-ui.service.d" "${DBDIR}"
    run_convert --yes
    assert_eq "${RC}" 10 "fresh server: exit code says 'no existing install'"
    assert_contains "${OUT}" "STATE=fresh" "fresh server reported as fresh"
    check "fresh: no backup directory created" test -z "$(backup_dir)"
    check "fresh: no install dir created" test ! -e "${MAIN}"
    teardown
}

case_stock_converts_in_place() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    local unit_sha env_sha cert_sha before_counts geo_sha custom_sha port_row secret_row
    unit_sha="$(sha "${R}/etc/systemd/system/x-ui.service")"
    env_sha="$(sha "${R}/etc/default/x-ui")"
    cert_sha="$(sha "${R}/root/cert/privkey.pem")"
    geo_sha="$(sha "${MAIN}/bin/geoip.dat")"
    custom_sha="$(sha "${MAIN}/bin/custom-geosite.dat")"
    before_counts="$(db_counts)"
    port_row="$(sqlite3 "${DBDIR}/x-ui.db" "select value from settings where key='webPort'")"
    secret_row="$(sqlite3 "${DBDIR}/x-ui.db" "select value from settings where key='secret'")"
    run_convert --yes
    assert_eq "${RC}" 0 "stock -> netron conversion succeeds (output: ${OUT: -400})"
    assert_eq "$(panel_version)" "3.11.0-netron.9" "binary is now the netron build"
    assert_eq "$(sha "${R}/etc/systemd/system/x-ui.service")" "${unit_sha}" "systemd unit untouched"
    assert_eq "$(sha "${R}/etc/default/x-ui")" "${env_sha}" "service env file untouched"
    assert_eq "$(sha "${R}/root/cert/privkey.pem")" "${cert_sha}" "certificate untouched"
    assert_eq "$(sha "${MAIN}/bin/geoip.dat")" "${geo_sha}" "bin/ geo file kept (not replaced by the release copy)"
    assert_eq "$(sha "${MAIN}/bin/custom-geosite.dat")" "${custom_sha}" "admin-placed file in bin/ kept"
    check "bin/: a file the stock install lacked was added" test -f "${MAIN}/bin/mtg-linux-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')"
    assert_eq "$(db_counts)" "${before_counts}" "inbounds/clients/nodes/sub ids unchanged"
    assert_eq "$(sqlite3 "${DBDIR}/x-ui.db" "select value from settings where key='webPort'")" "${port_row}" "panel port unchanged"
    assert_eq "$(sqlite3 "${DBDIR}/x-ui.db" "select value from settings where key='secret'")" "${secret_row}" "session secret unchanged"
    assert_eq "$(sqlite3 "${DBDIR}/x-ui.db" "select count(*) from settings where key='usageMultiplierEnable' and value='true'")" 0 "multiplier switch is not ON"
    assert_contains "$(cat "${R}/usr/bin/x-ui")" "rollback" "menu script was replaced by the release's x-ui.sh"
    check "service is running after conversion" "${STUBS}/systemctl" is-active --quiet x-ui
    local b
    b="$(backup_dir)"
    check "backup dir exists" test -d "${b}"
    for f in manifest.env install-dir.tar.gz x-ui.service service-env usr-bin-x-ui rollback.sh upgrade-from-stock.sh SHA256SUMS db/x-ui.db db/x-ui.db.sha256 certs/MAP; do
        check "backup contains ${f}" test -e "${b}/${f}"
    done
    assert_eq "$(stat -c %a "${b}")" 700 "backup dir is private (700)"
    assert_eq "$(find "${b}" -perm /077 | wc -l)" 0 "no file or dir in the backup is accessible to group/other"
    assert_not_contains "${OUT}" "${SECRET_SESSION}" "report never prints the session secret"
    assert_not_contains "${OUT}" "${SECRET_BASEPATH}" "report never prints the base path"
    assert_contains "${OUT}" "Node rollout order" "node rollout reminder printed"
    assert_contains "${OUT}" "Rolling back restores the database" "rollback data-loss warning printed"
    assert_contains "${OUT}" "rollback.sh" "exact rollback command printed"
    local reqs
    reqs="$(cat "${STATE}/curl.log")"
    assert_not_contains "${reqs}" "MHSanaei" "no request to the upstream repository"
    assert_contains "${reqs}" "mahdizo3181/netron-panel" "downloads came from this repository"
    teardown
}

case_already_netron_runs_normal_update() {
    setup 3.11.0-netron.4
    start_stock_panel
    local before
    before="$(tree_hash "${MAIN}")"
    run_convert --yes
    assert_eq "$(update_ran_prefix)" "stub-update.sh" "netron panel is handed to the normal updater"
    assert_eq "$(tree_hash "${MAIN}")" "${before}" "already-netron: install dir not touched by the converter"
    check "already-netron: no backup/convert performed" test -z "$(backup_dir)"
    assert_contains "$(cat "${STATE}/curl.log")" "mahdizo3181/netron-panel" "update.sh fetched from this repo"
    teardown
}

case_docker_is_refused() {
    setup
    export STUB_DOCKER_PS="3x-ui ghcr.io/mhsanaei/3x-ui:latest"
    local before
    before="$(tree_hash "${R}")"
    run_convert --yes
    assert_eq "${RC}" 20 "docker install refused"
    assert_contains "${OUT}" "Docker" "refusal explains Docker"
    assert_eq "$(tree_hash "${R}")" "${before}" "docker: nothing changed"
    teardown
}

case_postgres_is_refused() {
    setup
    echo 'XUI_DB_TYPE=postgres' >> "${R}/etc/default/x-ui"
    echo 'XUI_DB_DSN=postgres://user:SUPERSECRETPW@db/x' >> "${R}/etc/default/x-ui"
    local before
    before="$(tree_hash "${R}")"
    run_convert --yes
    assert_eq "${RC}" 20 "postgres install refused"
    assert_contains "${OUT}" "not verified" "refusal explains the engine is unverified"
    assert_not_contains "${OUT}" "SUPERSECRETPW" "DSN password never printed"
    assert_eq "$(tree_hash "${R}")" "${before}" "postgres: nothing changed"
    teardown
}

case_backup_failure_aborts_without_touching_the_panel() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    # tar stub that fails when it must create the install-dir backup
    cat > "${STUBS}/tar" << 'EOF'
#!/bin/bash
for a in "$@"; do [[ "$a" == -czf ]] && exit 2; done
exec /usr/bin/tar "$@"
EOF
    chmod +x "${STUBS}/tar"
    local before db_before
    before="$(tree_hash "${MAIN}")"
    db_before="$(sha "${DBDIR}/x-ui.db")"
    run_convert --yes
    assert_eq "${RC}" 20 "backup failure aborts the conversion"
    assert_contains "${OUT}" "Backup failed" "failure is reported"
    assert_eq "$(panel_version)" "3.11.0" "binary is still stock"
    assert_eq "$(tree_hash "${MAIN}")" "${before}" "install dir untouched"
    assert_eq "$(sha "${DBDIR}/x-ui.db")" "${db_before}" "database untouched"
    check "stock panel still running" "${STUBS}/systemctl" is-active --quiet x-ui
    teardown
}

case_backup_dir_unwritable_aborts() {
    setup
    make_release v3.11.0-netron.9 ok
    : > "${T}/afile"
    export XUI_BACKUP_ROOT="${T}/afile/nested"
    run_convert --yes
    assert_eq "${RC}" 20 "cannot create backup dir -> abort"
    assert_eq "$(panel_version)" "3.11.0" "binary still stock"
    teardown
}

# One health-failure scenario: the new panel misbehaves, the script must restore stock exactly.
health_failure_scenario() { # behaviour
    setup
    make_release v3.11.0-netron.9 "$1"
    start_stock_panel
    local db_before custom_before xui_sh_before
    "${STUBS}/systemctl" stop x-ui   # settle the stock DB (WAL) so "original bytes" is well defined
    db_before="$(sha "${DBDIR}/x-ui.db")"
    start_stock_panel
    "${STUBS}/systemctl" stop x-ui
    db_before="$(sha "${DBDIR}/x-ui.db")"
    custom_before="$(sha "${MAIN}/bin/custom-geosite.dat")"
    xui_sh_before="$(cat "${R}/usr/bin/x-ui")"
    "${STUBS}/systemctl" start x-ui
    run_convert --yes
    assert_eq "${RC}" 40 "[$1] failed health check -> automatic rollback (output: ${OUT: -500})"
    assert_contains "${OUT}" "Rolling back automatically" "[$1] rollback announced"
    assert_eq "$(panel_version)" "3.11.0" "[$1] stock binary restored"
    assert_eq "$(sha "${DBDIR}/x-ui.db")" "${db_before}" "[$1] database restored to the EXACT original bytes"
    assert_eq "$(sha "${MAIN}/bin/custom-geosite.dat")" "${custom_before}" "[$1] install dir restored"
    assert_eq "$(cat "${R}/usr/bin/x-ui")" "${xui_sh_before}" "[$1] stock menu script restored"
    check "[$1] stock panel running again" "${STUBS}/systemctl" is-active --quiet x-ui
    check "[$1] migrated column is gone with the restored DB" test "$(sqlite3 -readonly "${DBDIR}/x-ui.db" "select count(*) from pragma_table_info('inbounds') where name='usage_multiplier'")" = 0
    check "[$1] backup kept for inspection" test -d "$(backup_dir)"
    teardown
}

case_health_failure_crash_rolls_back() { health_failure_scenario crash; }
case_health_failure_silent_crash_rolls_back() { health_failure_scenario silent-crash; }
case_health_failure_logged_error_rolls_back() { health_failure_scenario loud; }
case_multiplier_switch_on_rolls_back() { health_failure_scenario switch-on; }

case_running_twice_is_safe() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    run_convert --yes
    assert_eq "${RC}" 0 "first run converts"
    local after_first backups_first
    after_first="$(tree_hash "${MAIN}")"
    backups_first="$(find "${R}/root" -maxdepth 1 -name 'xui-backup-*' | wc -l)"
    run_convert --yes
    assert_eq "$(update_ran_prefix)" "stub-update.sh" "second run goes to the normal updater"
    assert_eq "$(tree_hash "${MAIN}")" "${after_first}" "second run did not touch the install"
    assert_eq "$(find "${R}/root" -maxdepth 1 -name 'xui-backup-*' | wc -l)" "${backups_first}" "second run made no second backup"
    teardown
}

case_not_interactive_without_yes_changes_nothing() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    local before
    before="$(tree_hash "${MAIN}")"
    run_convert < /dev/null
    assert_eq "${RC}" 30 "no TTY and no --yes -> declined"
    assert_contains "${OUT}" "--yes" "tells the operator to pass --yes"
    assert_eq "$(tree_hash "${MAIN}")" "${before}" "nothing changed"
    check "no backup dir" test -z "$(backup_dir)"
    teardown
}

case_interactive_decline_and_accept() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    export XUI_PROMPT_STDIN=1
    set +e
    OUT="$(printf 'n\n' | bash "${SCRIPT}" convert 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 30 "answering n cancels"
    assert_eq "$(panel_version)" "3.11.0" "declined: still stock"
    set +e
    OUT="$(printf 'y\ny\n' | bash "${SCRIPT}" convert 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 0 "answering y to both prompts (master with nodes) converts"
    assert_eq "$(panel_version)" "3.11.0-netron.9" "converted"
    teardown
}

case_too_old_needs_force_old() {
    setup 3.9.0
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    run_convert --yes
    assert_eq "${RC}" 21 "3.9.0 is older than the oldest verified version"
    assert_eq "$(panel_version)" "3.9.0" "old: nothing changed"
    run_convert --yes --force-old
    assert_eq "${RC}" 0 "--force-old converts anyway (output: ${OUT: -300})"
    assert_eq "$(panel_version)" "3.11.0-netron.9" "old+force: converted"
    teardown
}

case_oldest_verified_version_converts() {
    setup 3.10.2 "${FIXTURE_DIR}/stock-3.10.2.db"
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    run_convert --yes
    assert_eq "${RC}" 0 "3.10.2 (oldest verified) converts without --force-old"
    teardown
}

case_checksum_mismatch_aborts_early() {
    setup
    make_release v3.11.0-netron.9 ok
    echo "0000000000000000000000000000000000000000000000000000000000000000  x" > "${T}"/release/x-ui-linux-*.tar.gz.sha256
    start_stock_panel
    local before
    before="$(tree_hash "${MAIN}")"
    run_convert --yes
    assert_eq "${RC}" 20 "bad checksum aborts"
    assert_contains "${OUT}" "Checksum mismatch" "mismatch reported"
    assert_eq "$(tree_hash "${MAIN}")" "${before}" "nothing changed"
    check "no backup made for a rejected download" test -z "$(backup_dir)"
    teardown
}

case_release_that_is_not_netron_is_refused() {
    setup
    make_release v3.11.1 ok     # no -netron suffix: upstream binary
    start_stock_panel
    run_convert --yes
    assert_eq "${RC}" 20 "a non-netron binary is refused"
    assert_eq "$(panel_version)" "3.11.0" "still stock"
    teardown
}

case_rollback_command_restores_stock() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    "${STUBS}/systemctl" stop x-ui
    local db_before
    db_before="$(sha "${DBDIR}/x-ui.db")"
    "${STUBS}/systemctl" start x-ui
    run_convert --yes
    assert_eq "${RC}" 0 "converted"
    check "post-upgrade DB differs (migration happened)" test "$(sha "${DBDIR}/x-ui.db")" != "${db_before}"
    local b
    b="$(backup_dir)"
    set +e
    OUT="$("${b}/rollback.sh" --yes 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 0 "rollback.sh succeeds (output: ${OUT: -300})"
    assert_eq "$(panel_version)" "3.11.0" "stock binary restored by rollback.sh"
    assert_eq "$(sha "${DBDIR}/x-ui.db")" "${db_before}" "stock DB bytes restored by rollback.sh"
    assert_contains "${OUT}" "one way" "one-way migration warning shown"
    assert_contains "${OUT}" "lost" "data-loss-since-backup warning shown"
    check "stock panel running again" "${STUBS}/systemctl" is-active --quiet x-ui
    # and a later conversion works again
    run_convert --yes
    assert_eq "${RC}" 0 "converting again after a rollback works"
    teardown
}

case_non_default_db_folder_is_honoured() {
    setup
    make_release v3.11.0-netron.9 ok
    local custom="${R}/data/xui-db"
    mkdir -p "${custom}"
    mv "${DBDIR}/x-ui.db" "${custom}/x-ui.db"
    echo "XUI_DB_FOLDER=${custom}" >> "${R}/etc/default/x-ui"
    export STUB_DBDIR="${custom}"
    DBDIR="${custom}"
    start_stock_panel
    set +e
    OUT="$(bash "${SCRIPT}" detect 2>&1)"
    set -e
    assert_contains "${OUT}" "non-default paths: install dir" "non-default path reported"
    run_convert --yes
    assert_eq "${RC}" 0 "conversion with a relocated DB works (output: ${OUT: -300})"
    check "relocated DB was backed up" test -f "$(backup_dir)/db/x-ui.db"
    teardown
}

# A panel bound to one address (webListen) does not answer on 127.0.0.1; probing the
# wrong address would roll back a healthy panel.
case_panel_bound_to_a_specific_address_is_probed_there() {
    setup
    make_release v3.11.0-netron.9 ok
    put_setting webListen 127.0.0.2
    start_stock_panel_on 127.0.0.2
    run_convert --yes
    assert_eq "${RC}" 0 "healthy panel bound to 127.0.0.2 is not rolled back (output: ${OUT: -300})"
    assert_eq "$(panel_version)" "3.11.0-netron.9" "converted"
    teardown
}

case_detect_is_read_only_and_hides_secrets() {
    setup
    start_stock_panel
    "${STUBS}/systemctl" stop x-ui
    put_setting tgBotEnable true
    put_setting tgBotToken 123:SECRET-BOT-TOKEN
    "${STUBS}/systemctl" start x-ui
    local before
    before="$(tree_hash "${R}")"
    set +e
    OUT="$(bash "${SCRIPT}" detect 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 0 "detect on a stock panel exits 0"
    assert_contains "${OUT}" "STATE=stock" "stock detected"
    assert_contains "${OUT}" "panel version    : 3.11.0 (stock)" "version and kind shown"
    assert_contains "${OUT}" "telegram bot     : ENABLED" "telegram bot flag shown"
    assert_contains "${OUT}" "nodes            : 1" "node count shown"
    assert_not_contains "${OUT}" "SECRET-BOT-TOKEN" "bot token never printed"
    assert_not_contains "${OUT}" "${SECRET_SESSION}" "session secret never printed"
    assert_not_contains "${OUT}" "${SECRET_BASEPATH}" "base path never printed"
    assert_eq "$(tree_hash "${R}")" "${before}" "detect changed nothing"
    teardown
}

case_install_sh_hands_an_existing_panel_to_the_converter() {
    have_userns || { echo "   skipped (no user namespaces)"; return 0; }
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    set +e
    OUT="$(XUI_MAIN_FOLDER="${MAIN}" XUI_SERVICE="${R}/etc/systemd/system" unshare -r bash "${REPO}/install.sh" --yes v3.11.0-netron.9 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 0 "install.sh on an existing stock panel converts it (output: ${OUT: -300})"
    assert_contains "${OUT}" "in-place upgrade flow" "install.sh announced the hand-off"
    assert_eq "$(panel_version)" "3.11.0-netron.9" "panel converted via install.sh"
    check "install.sh did not run the package manager before/instead" test ! -s "${STATE}/pkg.log"
    assert_not_contains "$(cat "${STATE}/curl.log")" "MHSanaei" "install.sh made no upstream request"
    teardown
}

case_update_sh_refuses_a_stock_panel_before_any_side_effect() {
    have_userns || { echo "   skipped (no user namespaces)"; return 0; }
    setup
    start_stock_panel
    local before
    before="$(tree_hash "${MAIN}")"
    set +e
    OUT="$(XUI_MAIN_FOLDER="${MAIN}" XUI_SERVICE="${R}/etc/systemd/system" XUI_UPDATE_STATUS_FILE="${T}/status.json" unshare -r bash "${REPO}/update.sh" 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 2 "update.sh refuses a stock panel"
    assert_contains "${OUT}" "Convert it with" "refusal tells how to convert"
    assert_contains "${OUT}" "mahdizo3181/netron-panel" "points at this repository"
    check "no package manager call" test ! -s "${STATE}/pkg.log"
    assert_eq "$(tree_hash "${MAIN}")" "${before}" "install dir untouched"
    teardown
}

# ---------------------------------------------------------------- interruption safety

assert_converted_healthy() { # label
    assert_eq "$(panel_version)" "3.11.0-netron.9" "[$1] the panel is the converted build"
    check "[$1] service active" "${STUBS}/systemctl" is-active --quiet x-ui
    check "[$1] panel answers HTTP" test "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/fixture/")" != 000
    check "[$1] decided: no IN_PROGRESS marker left" test -z "$(find "${R}/root" -name IN_PROGRESS)"
    check "[$1] migration present in the DB" test "$(sqlite3 -readonly "${DBDIR}/x-ui.db" "select count(*) from pragma_table_info('inbounds') where name='usage_multiplier'")" = 1
}

assert_original_state() { # label db_sha
    assert_eq "$(panel_version)" "3.11.0" "[$1] stock binary"
    assert_eq "$(sha "${DBDIR}/x-ui.db")" "$2" "[$1] database is the EXACT original bytes"
    assert_eq "$(cat "${MAIN}/x-ui.sh")" "stock-xui.sh 3.11.0" "[$1] stock x-ui.sh in the install dir"
    assert_eq "$(cat "${R}/usr/bin/x-ui")" "stock-menu 3.11.0" "[$1] stock menu script"
    check "[$1] stock panel is running (not stopped)" "${STUBS}/systemctl" is-active --quiet x-ui
    check "[$1] panel answers HTTP" test "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/fixture/")" != 000
    check "[$1] no IN_PROGRESS marker left" test -z "$(find "${R}/root" -name IN_PROGRESS)"
}

# HUP (ssh drop), INT (Ctrl-C) and TERM arriving at each point of the destructive window
# must not interrupt it: the end state is the fully converted, healthy panel.
case_signals_in_the_critical_window_cannot_interrupt() {
    local spec sig at
    for spec in HUP:stop TERM:stop INT:stop HUP:start-before TERM:start-before HUP:start-after TERM:start-after; do
        sig="${spec%%:*}"
        at="${spec##*:}"
        setup
        make_release v3.11.0-netron.9 ok
        start_stock_panel
        export STUB_SIGNAL="${sig}" STUB_SIGNAL_AT="${at}"
        run_convert --yes
        assert_eq "${RC}" 0 "[${spec}] conversion completes despite the signal (output: ${OUT: -300})"
        check "[${spec}] the signal was really sent" test -f "${STATE}/signal-fired"
        assert_not_contains "${OUT}" "stopped unexpectedly" "[${spec}] no recovery was needed"
        assert_converted_healthy "${spec}"
        teardown
    done
}

# An abnormal exit (here: systemctl stop fails after stopping) while only stopped: the old panel comes back.
case_abnormal_exit_after_stop_restarts_the_old_panel() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    local db_before
    db_before="$(sha "${DBDIR}/x-ui.db")"
    export STUB_STOP_FAIL_ONCE=1
    run_convert --yes
    assert_eq "${RC}" 40 "abnormal exit after stop is recovered (output: ${OUT: -400})"
    assert_contains "${OUT}" "stopped unexpectedly" "what happened is printed"
    assert_contains "${OUT}" "is running again" "the old panel was restarted"
    assert_original_state "after-stop" "${db_before}"
    teardown
}

# An abnormal exit with the files half replaced (the 2nd file move fails): exact original bytes come back.
case_abnormal_exit_half_swapped_rolls_back_exact_bytes() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    local db_before
    db_before="$(sha "${DBDIR}/x-ui.db")"
    export STUB_MV_FAIL_TARGET="${MAIN}/x-ui.sh"
    run_convert --yes
    assert_eq "${RC}" 40 "half-swapped exit is rolled back (output: ${OUT: -500})"
    assert_contains "${OUT}" "Files may be half replaced" "the half-swapped state is recognised"
    check "the failure was really injected" test -f "${STATE}/mv-failed"
    assert_original_state "half-swapped" "${db_before}"
    teardown
}

# SIGKILL cannot be trapped. It must leave a marker so the next run refuses to guess, a stale lock
# must not block the recovery, and rollback.sh must restore the exact original state.
case_sigkill_is_detected_and_recoverable() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    local db_before b
    db_before="$(sha "${DBDIR}/x-ui.db")"
    export STUB_SIGNAL=KILL STUB_SIGNAL_AT=start-before
    run_convert --yes
    check "script was killed (non-zero exit)" test "${RC}" -ne 0
    check "panel is left stopped (the unrecoverable-by-script state)" test ! "$("${STUBS}/systemctl" is-active x-ui > /dev/null 2>&1 && echo up)" = up
    b="$(backup_dir)"
    check "IN_PROGRESS marker left behind" test -f "${b}/IN_PROGRESS"
    unset STUB_SIGNAL STUB_SIGNAL_AT
    set +e
    OUT="$(bash "${SCRIPT}" detect 2>&1)"
    set -e
    assert_contains "${OUT}" "INTERRUPTED upgrade" "detect reports the interrupted upgrade"
    run_convert --yes
    assert_eq "${RC}" 20 "a new convert refuses while an interrupted one is undecided"
    assert_contains "${OUT}" "rollback.sh" "it points at the rollback"
    set +e
    OUT="$("${b}/rollback.sh" --yes 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 0 "rollback.sh recovers despite the stale lock (output: ${OUT: -300})"
    assert_original_state "after-sigkill-rollback" "${db_before}"
    run_convert --yes
    assert_eq "${RC}" 0 "a fresh conversion works after the recovery"
    teardown
}

# The EXIT trap's recovery when the abnormal exit happens after the new panel was started.
case_abnormal_exit_after_start_rolls_back() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    local db_before b
    db_before="$(sha "${DBDIR}/x-ui.db")"
    export STUB_SIGNAL=KILL STUB_SIGNAL_AT=start-after
    run_convert --yes
    unset STUB_SIGNAL STUB_SIGNAL_AT
    b="$(backup_dir)"
    assert_eq "$(panel_version)" "3.11.0-netron.9" "precondition: killed after the new panel started"
    # what the script's EXIT trap does when it exits abnormally in phase 'started'
    set +e
    OUT="$(bash -c 'source "$1"; PHASE=started; BACKUP="$2"; WAS_RUNNING=yes; VERSION=3.11.0; cleanup' _ "${SCRIPT}" "${b}" 2>&1)"
    RC=$?
    set -e
    assert_eq "${RC}" 40 "cleanup in phase 'started' rolls back (output: ${OUT: -400})"
    assert_original_state "after-start" "${db_before}"
    teardown
}

# ssh dropped: stdout is gone for the whole destructive window. Output errors must not abort the
# conversion (which would roll back a healthy panel).
case_dead_terminal_does_not_abort_the_conversion() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    set +e
    bash "${SCRIPT}" convert --yes 2>&1 | python3 -c '
import sys
for line in sys.stdin:
    if "Backup complete" in line:
        break
'
    local rc="${PIPESTATUS[0]}"
    set -e
    assert_eq "${rc}" 0 "converted although nothing could be printed any more"
    assert_converted_healthy "dead-terminal"
    teardown
}

# A wedged systemd must not freeze the conversion: the call times out, counts as a failure and the
# normal recovery runs. (The hung stub inherits the ignored TERM, so this also proves the -k kill.)
case_hung_systemctl_times_out_and_takes_the_recovery_path() {
    local db_before hang
    export XUI_SYSTEMCTL_TIMEOUT=2 XUI_SYSTEMCTL_CHECK_TIMEOUT=2 XUI_HEALTH_WAIT=5
    for hang in start stop; do
        setup
        export XUI_SYSTEMCTL_TIMEOUT=2 XUI_SYSTEMCTL_CHECK_TIMEOUT=2 XUI_HEALTH_WAIT=5
        make_release v3.11.0-netron.9 ok
        start_stock_panel
        db_before="$(sha "${DBDIR}/x-ui.db")"
        export STUB_HANG="${hang}"
        local t0=${SECONDS}
        run_convert --yes
        check "[hang ${hang}] it did not hang (finished in well under the stub's 300s sleep)" test $((SECONDS - t0)) -lt 60
        assert_eq "${RC}" 40 "[hang ${hang}] timeout is a failure -> recovery/rollback (output: ${OUT: -500})"
        assert_contains "${OUT}" "did not answer within 2s" "[hang ${hang}] the timeout is reported"
        check "[hang ${hang}] the hang was really injected" test -f "${STATE}/hung-${hang}"
        assert_original_state "hang-${hang}" "${db_before}"
        unset STUB_HANG
        teardown
    done
    unset XUI_SYSTEMCTL_TIMEOUT XUI_SYSTEMCTL_CHECK_TIMEOUT
    export XUI_HEALTH_WAIT=14
}

case_recommends_tmux_over_ssh_without_blocking() {
    setup
    make_release v3.11.0-netron.9 ok
    start_stock_panel
    export SSH_CONNECTION="198.51.100.1 50000 192.0.2.1 22"
    unset TMUX STY
    set +e
    OUT="$(bash "${SCRIPT}" convert < /dev/null 2>&1)"
    set -e
    assert_contains "${OUT}" "inside tmux or screen" "tip shown over ssh"
    OUT="$(TMUX=/tmp/tmux-0/default,1,0 bash "${SCRIPT}" convert < /dev/null 2>&1 || true)"
    assert_not_contains "${OUT}" "inside tmux or screen" "no tip inside tmux"
    OUT="$(bash -c 'trap "" HUP; exec bash "$1" convert' _ "${SCRIPT}" < /dev/null 2>&1 || true)"
    assert_not_contains "${OUT}" "inside tmux or screen" "no tip under nohup (SIGHUP already ignored)"
    assert_eq "$(panel_version)" "3.11.0" "the tip never changes anything"
    unset SSH_CONNECTION
    teardown
}

# ---------------------------------------------------------------- runner

filter="${1:-}"
for fn in $(declare -F | awk '{print $3}' | grep '^case_' | sort); do
    [[ -z "${filter}" || "${fn}" == *"${filter}"* ]] || continue
    CASE="${fn#case_}"
    echo "== ${CASE}"
    before=${failn}
    "${fn}" || fail "case function returned non-zero"
    [[ ${failn} -eq ${before} ]] && echo "   ok"
done
echo
echo "passed checks: ${pass}, failed: ${failn}"
[[ ${failn} -eq 0 ]]
