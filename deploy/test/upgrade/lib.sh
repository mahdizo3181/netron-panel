#!/bin/bash
# Shared helpers for the upgrade-from-stock tests. Sourced, not executed.
# Everything happens in a temp directory under XUI_ROOT: no root, no real systemd,
# no network. systemctl/curl/journalctl/docker are stubs on PATH.

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
HERE="${REPO}/deploy/test/upgrade"
SCRIPT="${REPO}/upgrade-from-stock.sh"
FIXTURE_DIR="${HERE}/fixtures"

pass=0
failn=0
CASE=""

fail() {
    echo "  FAIL [${CASE}]: $*" >&2
    failn=$((failn + 1))
    return 0
}

check() { # check "description" command...
    local desc="$1"
    shift
    if "$@" > /dev/null 2>&1; then pass=$((pass + 1)); else fail "${desc}"; fi
}

assert_eq() { if [[ "$1" == "$2" ]]; then pass=$((pass + 1)); else fail "$3 (got '$1', want '$2')"; fi; }

assert_contains() { # haystack needle description
    if grep -qF -- "$2" <<< "$1"; then pass=$((pass + 1)); else fail "$3 (missing '$2')"; fi
}

assert_not_contains() {
    if grep -qF -- "$2" <<< "$1"; then fail "$3 (found '$2')"; else pass=$((pass + 1)); fi
}

sha() { sha256sum "$1" | awk '{print $1}'; }

free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

# write_fake_xui PATH VERSION BEHAVIOUR
#   ok            serves HTTP; a netron build also adds the multiplier column (a "migration")
#   crash         migrates, logs "Error auto migrating model" and exits
#   loud          serves HTTP but logs a migration error
#   silent-crash  migrates and exits without a word
#   switch-on     serves HTTP but leaves usageMultiplierEnable=true
write_fake_xui() {
    cat > "$1" << EOF
#!/bin/bash
VER='$2'
BEHAVIOUR='$3'
case "\${1:-}" in
    -v) echo "\${VER}"; exit 0 ;;
    run) ;;
    *) exit 0 ;;
esac
db="\${XUI_DB_FOLDER}/x-ui.db"
port="\$(sqlite3 "\${db}" "SELECT value FROM settings WHERE key = 'webPort'")"
log() { echo "\$(date +%s) \$*" >> "\${STUB_STATE}/journal"; }
log "Starting x-ui \${VER}"
migrate() {
    case "\${VER}" in
        *-netron.*) sqlite3 "\${db}" "ALTER TABLE inbounds ADD COLUMN usage_multiplier REAL DEFAULT 1;" 2> /dev/null || true ;;
    esac
}
migrate
case "\${BEHAVIOUR}" in
    crash) log "Error auto migrating model: boom"; exit 1 ;;
    silent-crash) exit 1 ;;
    loud) log "Error initializing database: boom" ;;
    switch-on) sqlite3 "\${db}" "DELETE FROM settings WHERE key = 'usageMultiplierEnable'; INSERT INTO settings (key, value) VALUES ('usageMultiplierEnable', 'true');" ;;
    *) ;;
esac
mkdir -p "\${STUB_STATE}/www"
listen="\$(sqlite3 "\${db}" "SELECT value FROM settings WHERE key = 'webListen'")"
exec python3 -m http.server "\${port}" --bind "\${listen:-127.0.0.1}" --directory "\${STUB_STATE}/www" > /dev/null 2>&1
EOF
    chmod +x "$1"
}

write_stubs() {
    cat > "${STUBS}/systemctl" << 'EOF'
#!/bin/bash
# Minimal systemctl: one service "x-ui" whose process is $STUB_MAIN/x-ui run.
state="${STUB_STATE}"
pidfile="${state}/pid"
alive() { [[ -f "${pidfile}" ]] && kill -0 "$(cat "${pidfile}")" 2> /dev/null; }
case "${1:-}" in
    is-active)
        alive && exit 0
        exit 3
        ;;
    start)
        alive && exit 0
        echo "$*" >> "${state}/systemctl.log"
        # exec so the recorded PID is the panel process itself, not a wrapper
        nohup bash -c 'cd "${STUB_MAIN}" && exec env XUI_DB_FOLDER="${STUB_DBDIR}" XUI_LOG_FOLDER="${STUB_LOGDIR}" XUI_BIN_FOLDER="${STUB_MAIN}/bin" "${STUB_MAIN}/x-ui" run' >> "${state}/panel.out" 2>&1 &
        echo $! > "${pidfile}"
        sleep 0.3
        exit 0
        ;;
    stop)
        echo "$*" >> "${state}/systemctl.log"
        if alive; then
            pid="$(cat "${pidfile}")"
            # the fake panel execs python; stop the whole process
            kill "${pid}" 2> /dev/null
            # like systemd: wait for a clean exit, then SIGKILL
            for _ in $(seq 1 400); do kill -0 "${pid}" 2> /dev/null || break; sleep 0.1; done
            kill -9 "${pid}" 2> /dev/null || true
        fi
        rm -f "${pidfile}"
        exit 0
        ;;
    show)
        if [[ "$*" == *MainPID* ]]; then alive && cat "${pidfile}" || echo 0; exit 0; fi
        if [[ "$*" == *NRestarts* ]]; then echo 0; exit 0; fi
        exit 0
        ;;
    daemon-reload | enable | disable | reset-failed)
        echo "$*" >> "${state}/systemctl.log"
        exit 0
        ;;
    *) exit 0 ;;
esac
EOF
    cat > "${STUBS}/journalctl" << 'EOF'
#!/bin/bash
since=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --since) shift; since="${1#@}" ;;
    esac
    shift
done
[[ -f "${STUB_STATE}/journal" ]] || exit 0
while read -r ts line; do
    [[ "${ts}" -ge "${since}" ]] && echo "${line}"
done < "${STUB_STATE}/journal"
exit 0
EOF
    cat > "${STUBS}/curl" << 'EOF'
#!/bin/bash
# Serves GitHub URLs from local files; passes everything else (the local panel) to real curl.
url=""
out=""
want_code=0
want_effective=0
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
    a="${args[$i]}"
    case "${a}" in
        -o) out="${args[$((i + 1))]}" ;;
        -w) [[ "${args[$((i + 1))]}" == *http_code* ]] && want_code=1
            [[ "${args[$((i + 1))]}" == *url_effective* ]] && want_effective=1 ;;
        http*://*) url="${a}" ;;
    esac
done
if [[ "${url}" != *github.com* && "${url}" != *githubusercontent.com* ]]; then
    exec /usr/bin/curl "$@"
fi
echo "${url}" >> "${STUB_STATE}/curl.log"
emit() { # file-or-stdout
    if [[ -n "${out}" && "${out}" != /dev/null ]]; then cp "$1" "${out}"; else cat "$1"; fi
}
case "${url}" in
    */releases/latest)
        [[ "${want_effective}" == 1 ]] && printf '%s' "https://github.com/mahdizo3181/netron-panel/releases/tag/${STUB_LATEST_TAG}"
        exit 0
        ;;
    */releases/download/*/x-ui-linux-*)
        asset="${url##*/}"
        if [[ -f "${STUB_RELEASE_DIR}/${asset}" ]]; then
            emit "${STUB_RELEASE_DIR}/${asset}"
            [[ "${want_code}" == 1 ]] && printf '200'
            exit 0
        fi
        [[ "${want_code}" == 1 ]] && printf '404'
        exit 22
        ;;
    */upgrade-from-stock.sh)
        emit "${STUB_HELPER}"
        exit 0
        ;;
    */update.sh)
        printf '#!/bin/bash\necho "stub-update.sh ran XUI_UPDATE_TAG=${XUI_UPDATE_TAG:-}" > "%s/update-ran"\n' "${STUB_STATE}" > "${STUB_STATE}/update.sh"
        emit "${STUB_STATE}/update.sh"
        exit 0
        ;;
    *)
        [[ "${want_code}" == 1 ]] && printf '404'
        exit 22
        ;;
esac
EOF
    cat > "${STUBS}/docker" << 'EOF'
#!/bin/bash
[[ -n "${STUB_DOCKER_PS:-}" ]] && echo "${STUB_DOCKER_PS}"
exit 0
EOF
    local pm
    for pm in apt-get apt dnf yum pacman zypper apk; do
        # shellcheck disable=SC2016
        printf '#!/bin/bash\necho "%s $*" >> "${STUB_STATE}/pkg.log"\n' "${pm}" > "${STUBS}/${pm}"
    done
    chmod +x "${STUBS}"/*
}

# put_setting KEY VALUE: settings.key has no unique index, so replace by delete+insert.
put_setting() {
    sqlite3 "${DBDIR}/x-ui.db" "DELETE FROM settings WHERE key = '$1'; INSERT INTO settings (key, value) VALUES ('$1', '$2');"
}

# setup STOCK_VERSION [fixture-file]   builds a stock server in $T, exports the env
setup() {
    local ver="${1:-3.11.0}" fixture="${2:-${FIXTURE_DIR}/stock-3.11.0.db}"
    T="$(mktemp -d)"
    R="${T}/root"
    STATE="${T}/state"
    STUBS="${T}/stubs"
    MAIN="${R}/usr/local/x-ui"
    DBDIR="${R}/etc/x-ui"
    mkdir -p "${MAIN}/bin" "${DBDIR}" "${R}/etc/systemd/system/x-ui.service.d" "${R}/usr/bin" "${R}/var/log/x-ui" "${R}/root" "${R}/etc/default" "${R}/root/cert" "${STATE}" "${STUBS}" "${T}/release"
    cp "${fixture}" "${DBDIR}/x-ui.db"
    PORT="$(free_port)"
    sqlite3 "${DBDIR}/x-ui.db" "UPDATE settings SET value = '${PORT}' WHERE key = 'webPort';"
    put_setting subPort "$(free_port)"
    echo "fake-cert" > "${R}/root/cert/fullchain.pem"
    echo "fake-key" > "${R}/root/cert/privkey.pem"
    put_setting subCertFile "${R}/root/cert/fullchain.pem"
    put_setting subKeyFile "${R}/root/cert/privkey.pem"
    write_fake_xui "${MAIN}/x-ui" "${ver}" ok
    echo "stock-xui.sh ${ver}" > "${MAIN}/x-ui.sh"
    echo "stock-menu ${ver}" > "${R}/usr/bin/x-ui"
    chmod +x "${MAIN}/x-ui.sh" "${R}/usr/bin/x-ui"
    echo "geo-original" > "${MAIN}/bin/geoip.dat"
    echo "admin-custom-geo" > "${MAIN}/bin/custom-geosite.dat"
    echo "xray-original" > "${MAIN}/bin/xray-linux-amd64"
    cp "${MAIN}/bin/xray-linux-amd64" "${MAIN}/bin/xray-linux-arm64"
    cat > "${R}/etc/systemd/system/x-ui.service" << EOF
[Unit]
Description=x-ui Service
[Service]
EnvironmentFile=-${R}/etc/default/x-ui
WorkingDirectory=${MAIN}/
ExecStart=${MAIN}/x-ui
[Install]
WantedBy=multi-user.target
EOF
    echo '[Service]' > "${R}/etc/systemd/system/x-ui.service.d/10-xui-sandbox.conf"
    echo 'XUI_ENABLE_FAIL2BAN=false' > "${R}/etc/default/x-ui"

    export STUB_STATE="${STATE}" STUB_MAIN="${MAIN}" STUB_DBDIR="${DBDIR}" STUB_LOGDIR="${R}/var/log/x-ui"
    export STUB_HELPER="${SCRIPT}" STUB_RELEASE_DIR="${T}/release" STUB_LATEST_TAG="v3.11.0-netron.9" STUB_DOCKER_PS=""
    export XUI_ROOT="${R}" XUI_BACKUP_ROOT="${R}/root" XUI_HEALTH_WAIT=14 XUI_HEALTH_STABLE=2 XUI_DOCKER="${STUBS}/docker"
    unset XUI_MAIN_FOLDER XUI_SERVICE XUI_PROMPT_STDIN
    write_stubs
    export PATH="${STUBS}:${PATH}"
}

# make_release TAG BEHAVIOUR   -> release assets for this machine's arch
make_release() {
    local tag="$1" behaviour="${2:-ok}" arch asset d
    case "$(uname -m)" in aarch64 | arm64) arch=arm64 ;; *) arch=amd64 ;; esac
    asset="x-ui-linux-${arch}.tar.gz"
    d="${T}/release/build"
    mkdir -p "${d}/x-ui/bin"
    write_fake_xui "${d}/x-ui/x-ui" "${tag#v}" "${behaviour}"
    cp "${REPO}/x-ui.sh" "${d}/x-ui/x-ui.sh"
    echo "unit" > "${d}/x-ui/x-ui.service.debian"
    echo "xray-NEW" > "${d}/x-ui/bin/xray-linux-${arch}"
    echo "geo-NEW" > "${d}/x-ui/bin/geoip.dat"
    echo "mtg-new" > "${d}/x-ui/bin/mtg-linux-${arch}"
    tar -C "${d}" -czf "${T}/release/${asset}" x-ui
    (cd "${T}/release" && sha256sum "${asset}" > "${asset}.sha256")
}

have_userns() { command -v unshare > /dev/null && unshare -r true 2> /dev/null; }

# first 14 chars of the marker the stub updater leaves (empty if it never ran)
update_ran_prefix() { cut -c1-14 "${STATE}/update-ran" 2> /dev/null || true; }

# run_convert ARGS...  -> sets OUT and RC (read by the test cases that source this file)
# shellcheck disable=SC2034
run_convert() {
    set +e
    OUT="$(bash "${SCRIPT}" convert "$@" 2>&1)"
    RC=$?
    set -e
}

stop_stub_panel() {
    "${STUBS}/systemctl" stop x-ui > /dev/null 2>&1 || true
}

teardown() {
    stop_stub_panel
    [[ -n "${T:-}" && "${T}" == /tmp/* ]] && rm -rf -- "${T}"
    return 0
}

start_stock_panel() {
    "${STUBS}/systemctl" start x-ui
    for _ in $(seq 1 30); do
        curl -s -o /dev/null "http://127.0.0.1:${PORT}/" && return 0
        sleep 0.3
    done
    fail "stock fake panel did not start"
}

start_stock_panel_on() { # host
    "${STUBS}/systemctl" start x-ui
    local _
    for _ in $(seq 1 30); do
        curl -s -o /dev/null "http://$1:${PORT}/" && return 0
        sleep 0.3
    done
    fail "stock fake panel did not start on $1"
}

backup_dir() { find "${R}/root" -maxdepth 1 -type d -name 'xui-backup-*' | sort | tail -n1; }

panel_version() { "${MAIN}/x-ui" -v; }

db_counts() { sqlite3 -readonly "${DBDIR}/x-ui.db" "SELECT (SELECT count(*) FROM inbounds)||'/'||(SELECT count(*) FROM clients)||'/'||(SELECT count(*) FROM nodes)||'/'||(SELECT group_concat(sub_id) FROM clients);"; }

tree_hash() { # hash of a directory's file names + contents
    (cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}')
}
