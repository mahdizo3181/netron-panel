#!/bin/bash
# upgrade-from-stock.sh: upgrade a stock MHSanaei/3x-ui panel to netron-panel in place.
#
#   upgrade-from-stock.sh detect                    read-only report, never changes anything
#   upgrade-from-stock.sh convert [--yes] [--force-old] [--version vX.Y.Z-netron.N]
#   upgrade-from-stock.sh rollback [backup-dir] [--yes]
#
# It replaces the panel binary and helper scripts only. The database, settings,
# credentials, port, base path, certificates, Xray config, bin/ and the systemd
# unit stay exactly as they are. A full backup is taken first and the upgrade is
# rolled back automatically when the panel does not come up healthy.
#
# Test/automation overrides (all optional):
#   XUI_ROOT          prefix for every absolute path (default empty), no root needed
#   XUI_MAIN_FOLDER   install dir        XUI_SERVICE  systemd unit dir
#   XUI_BACKUP_ROOT   where xui-backup-* goes (default /root)
#   XUI_HEALTH_WAIT   seconds to watch the new panel (default 30)
#   XUI_HEALTH_STABLE seconds it must stay healthy (default 8)
#   XUI_DOCKER        docker client command (default docker)
set -euo pipefail

xui_repo="mahdizo3181/netron-panel"
# Oldest stock panel whose database this build has been migrated from and checked
# (deploy/test/upgrade/fixtures). Older panels need --force-old.
MIN_VERIFIED_VERSION="3.10.2"

EX_FRESH=10
EX_NETRON=12
EX_REFUSED=20
EX_OLD=21
EX_DECLINED=30
EX_FAILED_ROLLED_BACK=40
EX_FAILED_NO_ROLLBACK=41

if [[ -t 1 ]]; then
    red='\033[0;31m' green='\033[0;32m' yellow='\033[0;33m' blue='\033[0;34m' plain='\033[0m'
else
    red='' green='' yellow='' blue='' plain=''
fi

say() { echo -e "$*"; }
warn() { echo -e "${yellow}$*${plain}" >&2; }
err() { echo -e "${red}$*${plain}" >&2; }
ok() { echo -e "${green}$*${plain}"; }

ROOT="${XUI_ROOT:-}"
p() { printf '%s%s' "${ROOT}" "$1"; }

ASSUME_YES=0
FORCE_OLD=0
WANT_VERSION=""
CMD=""
ROLLBACK_DIR=""

# ---------------------------------------------------------------- paths

SERVICE_DIR="${XUI_SERVICE:-$(p /etc/systemd/system)}"
UNIT_FILE="${SERVICE_DIR}/x-ui.service"
MAIN="${XUI_MAIN_FOLDER:-$(p /usr/local/x-ui)}"
DB_DIR="$(p /etc/x-ui)"
USRBIN_XUI="$(p /usr/bin/x-ui)"
BACKUP_ROOT="${XUI_BACKUP_ROOT:-$(p /root)}"
HEALTH_WAIT="${XUI_HEALTH_WAIT:-30}"
HEALTH_STABLE="${XUI_HEALTH_STABLE:-8}"
DOCKER="${XUI_DOCKER:-docker}"

# ---------------------------------------------------------------- facts (filled by gather_facts)

ENV_FILE=""
ENGINE="sqlite"
DB_FILE=""
VERSION=""
KIND="unknown"
RUNNING="no"
HAVE_INSTALL="no"
HAVE_UNIT="no"
DOCKER_NOTE=""
REFUSE_REASON=""
PORT=""
PROBE_HOST="127.0.0.1"
SCHEME="http"
BASE_PATH="/"
NODES="?"
NODES_ENABLED="?"
INBOUNDS="?"
CLIENTS="?"
TG="?"
DISCORD="?"
SMTP="?"
INFORM="?"
LDAP="?"
CERT_PATHS=()
NONDEFAULT=""
HAVE_SQLITE="no"

command -v sqlite3 > /dev/null 2>&1 && HAVE_SQLITE="yes"

sql() {
    [[ "${HAVE_SQLITE}" == "yes" && -f "${DB_FILE}" ]] || return 1
    sqlite3 -readonly "${DB_FILE}" "$1" 2> /dev/null
}

# setting KEY DEFAULT: the panel only stores non-default keys.
setting() {
    local v
    v="$(sql "SELECT value FROM settings WHERE key = '$1' LIMIT 1;")" || v=""
    if [[ -z "${v}" ]]; then echo "$2"; else echo "${v}"; fi
}

# env_lookup NAME: value from the service env file, then unit/drop-in Environment=.
env_lookup() {
    local name="$1" f line v=""
    if [[ -n "${ENV_FILE}" && -r "${ENV_FILE}" ]]; then
        line="$(grep -E "^[[:space:]]*(export[[:space:]]+)?${name}=" "${ENV_FILE}" | tail -n1 || true)"
        v="${line#*=}"
    fi
    if [[ -z "${v}" ]]; then
        for f in "${UNIT_FILE}" "${UNIT_FILE}.d"/*.conf; do
            [[ -r "${f}" ]] || continue
            line="$(grep -E "^Environment=\"?${name}=" "${f}" | tail -n1 || true)"
            [[ -n "${line}" ]] && v="${line#*=}" && v="${v#*=}"
        done
    fi
    v="${v%\"}"
    v="${v#\"}"
    v="${v%\'}"
    v="${v#\'}"
    echo "${v}"
}

pick_env_file() {
    local f
    for f in "$(p /etc/default/x-ui)" "$(p /etc/conf.d/x-ui)" "$(p /etc/sysconfig/x-ui)"; do
        if [[ -r "${f}" ]]; then
            ENV_FILE="${f}"
            return 0
        fi
    done
    return 0
}

installed_version() {
    [[ -x "${MAIN}/x-ui" ]] || return 1
    timeout 15 "${MAIN}/x-ui" -v 2> /dev/null | head -n1 | tr -d '[:space:]'
}

version_base() { local v="${1#v}"; echo "${v%%-netron.*}"; }

# version_lt A B: true when A sorts before B.
version_lt() { [[ "$1" != "$2" && "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" == "$1" ]]; }

gather_facts() {
    pick_env_file
    [[ -f "${UNIT_FILE}" ]] && HAVE_UNIT="yes"
    # An existing unit tells where the panel really lives.
    if [[ "${HAVE_UNIT}" == "yes" && -z "${XUI_MAIN_FOLDER:-}" ]]; then
        local exec_start
        exec_start="$(grep -E '^ExecStart=' "${UNIT_FILE}" | head -n1 | sed -E 's/^ExecStart=//; s/^[-@+!:]+//' | awk '{print $1}')"
        if [[ -n "${exec_start}" && -f "${exec_start}" ]]; then MAIN="$(dirname "${exec_start}")"; fi
    fi
    [[ -e "${MAIN}/x-ui" ]] && HAVE_INSTALL="yes"

    local dbf
    dbf="$(env_lookup XUI_DB_FOLDER)"
    [[ -n "${dbf}" ]] && DB_DIR="${dbf}"
    DB_FILE="${DB_DIR}/x-ui.db"

    local dbtype
    dbtype="$(env_lookup XUI_DB_TYPE | tr '[:upper:]' '[:lower:]')"
    case "${dbtype}" in
        "" | sqlite | sqlite3) ENGINE="sqlite" ;;
        postgres | postgresql | pg) ENGINE="postgres" ;;
        *) ENGINE="${dbtype}" ;;
    esac

    [[ "${MAIN}" != "$(p /usr/local/x-ui)" || "${DB_DIR}" != "$(p /etc/x-ui)" ]] && NONDEFAULT="install dir ${MAIN}, database folder ${DB_DIR}"

    if [[ "${HAVE_UNIT}" == "yes" ]] && systemctl is-active --quiet x-ui 2> /dev/null; then RUNNING="yes"; fi

    if VERSION="$(installed_version)" && [[ -n "${VERSION}" ]]; then
        case "${VERSION}" in
            *-netron.*) KIND="netron" ;;
            [0-9]*.[0-9]*.[0-9]*) KIND="stock" ;;
            *) KIND="stock-dev" ;;
        esac
    else
        VERSION=""
    fi

    docker_check

    if [[ "${ENGINE}" == "sqlite" && "${HAVE_SQLITE}" == "yes" && -f "${DB_FILE}" ]]; then
        PORT="$(setting webPort 2053)"
        BASE_PATH="$(setting webBasePath /)"
        local listen
        listen="$(setting webListen '')"
        case "${listen}" in
            "" | 0.0.0.0 | "::") ;;
            *:*) PROBE_HOST="[${listen}]" ;;
            *) PROBE_HOST="${listen}" ;;
        esac
        local cert
        cert="$(setting webCertFile '')"
        [[ -n "${cert}" ]] && SCHEME="https"
        NODES="$(sql 'SELECT count(*) FROM nodes;' || echo '?')"
        NODES_ENABLED="$(sql 'SELECT count(*) FROM nodes WHERE enable = 1;' || echo '?')"
        INBOUNDS="$(sql 'SELECT count(*) FROM inbounds;' || echo '?')"
        CLIENTS="$(sql 'SELECT count(*) FROM clients;' || echo '?')"
        TG="$(setting tgBotEnable false)"
        DISCORD="$(setting discordBotEnable false)"
        SMTP="$(setting smtpEnable false)"
        INFORM="$(setting externalTrafficInformEnable false)"
        LDAP="$(setting ldapEnable false)"
        local key path
        for key in webCertFile webKeyFile subCertFile subKeyFile; do
            path="$(setting "${key}" '')"
            [[ -n "${path}" ]] && CERT_PATHS+=("${path}")
        done
    fi
}

docker_check() {
    if [[ -z "${ROOT}" && -f /.dockerenv ]]; then
        DOCKER_NOTE="this script is running inside a container"
        return 0
    fi
    command -v "${DOCKER}" > /dev/null 2>&1 || return 0
    local found
    found="$("${DOCKER}" ps -a --format '{{.Names}} {{.Image}}' 2> /dev/null | grep -Ei '3x-ui|x-ui' || true)"
    if [[ -n "${found}" ]]; then
        DOCKER_NOTE="a Docker container looks like a 3x-ui panel ($(echo "${found}" | head -n1 | awk '{print $1}'))"
    fi
    return 0
}

# decide sets STATE (and REFUSE_REASON): fresh | stock | netron | refuse | old
decide() {
    STATE="fresh"
    if [[ -n "${DOCKER_NOTE}" ]]; then
        STATE="refuse"
        REFUSE_REASON="Docker-based install detected: ${DOCKER_NOTE}. In-place conversion is only verified for the native systemd install; upgrade the image instead (see docs/UPGRADE_FROM_STOCK.md)."
        return 0
    fi
    if [[ "${HAVE_INSTALL}" == "no" && "${HAVE_UNIT}" == "no" ]]; then
        return 0
    fi
    if [[ "${HAVE_INSTALL}" == "no" || "${HAVE_UNIT}" == "no" ]]; then
        STATE="refuse"
        REFUSE_REASON="Found only part of an install (binary: ${HAVE_INSTALL}, systemd unit: ${HAVE_UNIT}). Not guessing; fix or remove the leftovers first."
        return 0
    fi
    if [[ "${ENGINE}" != "sqlite" ]]; then
        STATE="refuse"
        REFUSE_REASON="Database engine '${ENGINE}' is not verified for in-place conversion (only SQLite is). Nothing was changed."
        return 0
    fi
    if [[ ! -f "${DB_FILE}" ]]; then
        STATE="refuse"
        REFUSE_REASON="No database at ${DB_FILE}. Not guessing where the data lives."
        return 0
    fi
    if [[ -z "${VERSION}" ]]; then
        STATE="refuse"
        REFUSE_REASON="Could not read the installed panel version from ${MAIN}/x-ui -v."
        return 0
    fi
    if [[ "${KIND}" == "netron" ]]; then
        STATE="netron"
        return 0
    fi
    STATE="stock"
    if [[ "${KIND}" == "stock-dev" ]] || version_lt "$(version_base "${VERSION}")" "${MIN_VERIFIED_VERSION}"; then
        STATE="old"
    fi
    return 0
}

print_report() {
    local base_path_note="(hidden)"
    [[ "${BASE_PATH}" == "/" ]] && base_path_note="/"
    say "${blue}Existing panel report${plain} (read-only)"
    say "  install dir      : ${MAIN} ($([[ "${HAVE_INSTALL}" == yes ]] && echo found || echo missing))"
    say "  database         : ${DB_FILE} (engine: ${ENGINE})"
    say "  systemd unit     : ${UNIT_FILE} ($([[ "${HAVE_UNIT}" == yes ]] && echo found || echo missing))"
    say "  running          : ${RUNNING}"
    say "  panel version    : ${VERSION:-unknown} (${KIND})"
    say "  non-default paths: ${NONDEFAULT:-none}"
    say "  docker           : ${DOCKER_NOTE:-none found}"
    if [[ "${ENGINE}" == "sqlite" && "${HAVE_SQLITE}" == "yes" && -f "${DB_FILE}" ]]; then
        say "  panel port       : ${PORT} (${SCHEME}), base path ${base_path_note}"
        say "  inbounds/clients : ${INBOUNDS} / ${CLIENTS}"
        say "  nodes            : ${NODES} (${NODES_ENABLED} enabled)"
        say "  telegram bot     : $([[ "${TG}" == true ]] && echo ENABLED || echo disabled)"
        say "  other notifiers  : discord ${DISCORD}, email ${SMTP}, traffic-inform URL ${INFORM}, ldap ${LDAP}"
    else
        say "  database facts   : skipped (sqlite3 not installed or database unreadable)"
    fi
}

state_line() { echo "STATE=${STATE}"; }

# ---------------------------------------------------------------- helpers

die() {
    err "$*"
    exit "${2:-1}"
}

lock_dir=""
acquire_lock() {
    lock_dir="${MAIN}.upgrade.lock"
    if ! mkdir "${lock_dir}" 2> /dev/null; then
        lock_dir=""
        die "Another upgrade seems to be running (${MAIN}.upgrade.lock exists). If none is, remove that directory and retry." "${EX_REFUSED}"
    fi
}

release_lock() {
    if [[ -n "${lock_dir}" ]]; then rmdir "${lock_dir}" 2> /dev/null || true; fi
    lock_dir=""
}

cleanup() {
    release_lock
    [[ -n "${WORK}" ]] && safe_rm_rf "${WORK}"
    return 0
}

# safe_rm_rf DIR: only ever removes an absolute path with at least three parts.
safe_rm_rf() {
    local d="$1"
    [[ "${d}" == /*/*/* && -d "${d}" && ! -L "${d}" ]] || return 0
    rm -rf -- "${d}"
}

confirm() {
    [[ "${ASSUME_YES}" == "1" ]] && return 0
    if [[ ! -t 0 && -z "${XUI_PROMPT_STDIN:-}" ]]; then
        die "Not interactive: nothing was changed. Re-run with --yes to confirm." "${EX_DECLINED}"
    fi
    local ans=""
    read -r -p "$1 [y/N] " ans || true
    [[ "${ans}" =~ ^[Yy]([Ee][Ss])?$ ]]
}

arch_name() {
    case "$(uname -m)" in
        x86_64 | x64 | amd64) echo amd64 ;;
        armv8* | arm64 | aarch64) echo arm64 ;;
        *) echo unsupported ;;
    esac
}

# Both are best effort: a missing path yields an empty answer, never a failed pipeline under set -e.
file_size_kb() { { du -sk "$1" 2> /dev/null || true; } | awk '{print $1}'; }
free_kb() { { df -Pk "$1" 2> /dev/null || true; } | awk 'NR == 2 {print $4}'; }

install_atomic() {
    local src="$1" dst="$2" mode="$3" tmp
    tmp="${dst}.netron-new.$$"
    cp "${src}" "${tmp}"
    chmod "${mode}" "${tmp}"
    mv -f "${tmp}" "${dst}"
}

systemd_present() {
    [[ -n "${ROOT}" ]] && return 0
    [[ -d /run/systemd/system ]] && command -v systemctl > /dev/null 2>&1
}

# ---------------------------------------------------------------- release download

resolve_latest_tag() {
    local url tag
    url="$(curl -sSLI -o /dev/null -w '%{url_effective}' --retry 5 --retry-delay 3 --connect-timeout 15 --max-time 60 "https://github.com/${xui_repo}/releases/latest" 2> /dev/null || true)"
    tag="${url##*/tag/}"
    if [[ "${tag}" != "${url}" && -n "${tag}" && "${tag}" != "latest" ]]; then
        echo "${tag}"
        return 0
    fi
    curl -Ls --retry 5 --retry-delay 3 --connect-timeout 15 --max-time 60 "https://api.github.com/repos/${xui_repo}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
}

WORK=""
NEW_TAG=""
fetch_release() {
    local arch asset url expected actual
    arch="$(arch_name)"
    [[ "${arch}" != "unsupported" ]] || die "netron-panel publishes linux amd64 and arm64 builds only; this CPU is not supported. Nothing was changed." "${EX_REFUSED}"
    NEW_TAG="${WANT_VERSION:-$(resolve_latest_tag)}"
    [[ -n "${NEW_TAG}" ]] || die "Could not determine the latest netron-panel release. Nothing was changed." "${EX_REFUSED}"
    WORK="$(mktemp -d "$(dirname "${MAIN}")/x-ui-upgrade.XXXXXX")"
    asset="x-ui-linux-${arch}.tar.gz"
    url="https://github.com/${xui_repo}/releases/download/${NEW_TAG}/${asset}"
    say "Downloading ${NEW_TAG} (${asset})..."
    curl -fLR --retry 5 --retry-delay 3 --connect-timeout 15 --speed-limit 1 --speed-time 300 -o "${WORK}/${asset}" "${url}" ||
        die "Download failed. Nothing was changed." "${EX_REFUSED}"
    [[ -s "${WORK}/${asset}" ]] || die "Downloaded archive is empty. Nothing was changed." "${EX_REFUSED}"
    local code
    code="$(curl -sL --retry 3 --retry-delay 3 --connect-timeout 15 --max-time 60 -o "${WORK}/${asset}.sha256" -w '%{http_code}' "${url}.sha256" || true)"
    [[ "${code}" == "200" ]] || die "Could not download the checksum (HTTP ${code}); refusing to install an unverified binary. Nothing was changed." "${EX_REFUSED}"
    expected="$(awk 'NR == 1 {print $1}' "${WORK}/${asset}.sha256")"
    actual="$(sha256sum "${WORK}/${asset}" | awk '{print $1}')"
    if [[ ! "${expected}" =~ ^[0-9a-f]{64}$ || "${expected}" != "${actual}" ]]; then
        die "Checksum mismatch (expected ${expected:-none}, got ${actual}). Nothing was changed." "${EX_REFUSED}"
    fi
    ok "Checksum verified: ${actual}"
    tar --no-same-owner -xzf "${WORK}/${asset}" -C "${WORK}" || die "Could not extract the release archive. Nothing was changed." "${EX_REFUSED}"
    [[ -s "${WORK}/x-ui/x-ui" && -s "${WORK}/x-ui/x-ui.sh" ]] || die "The release archive lacks the panel binary or x-ui.sh. Nothing was changed." "${EX_REFUSED}"
    chmod +x "${WORK}/x-ui/x-ui"
    local shipped
    shipped="$(timeout 15 "${WORK}/x-ui/x-ui" -v 2> /dev/null | head -n1 | tr -d '[:space:]' || true)"
    [[ "${shipped}" == *-netron.* ]] || die "The downloaded binary reports version '${shipped:-unknown}', not a netron build. Refusing. Nothing was changed." "${EX_REFUSED}"
    NEW_VERSION="${shipped}"
}
NEW_VERSION=""

# ---------------------------------------------------------------- backup

BACKUP=""
WAS_RUNNING="no"

backup_prepare_dir() {
    local ts n=0
    ts="$(date +%Y%m%d-%H%M%S)"
    BACKUP="${BACKUP_ROOT}/xui-backup-${ts}"
    while [[ -e "${BACKUP}" ]]; do
        n=$((n + 1))
        BACKUP="${BACKUP_ROOT}/xui-backup-${ts}-${n}"
    done
    (umask 077 && mkdir -p "${BACKUP}/db" "${BACKUP}/certs") || die "Cannot create ${BACKUP}. Nothing was changed." "${EX_REFUSED}"
}

write_manifest() {
    {
        printf "%s='%s'\n" MAIN "${MAIN}" DB_FILE "${DB_FILE}" DB_DIR "${DB_DIR}" UNIT_FILE "${UNIT_FILE}" \
            SERVICE_DIR "${SERVICE_DIR}" ENV_FILE "${ENV_FILE}" USRBIN_XUI "${USRBIN_XUI}" PREV_VERSION "${VERSION}" \
            NEW_VERSION "${NEW_VERSION}" WAS_RUNNING "${WAS_RUNNING}" PORT "${PORT}" SCHEME "${SCHEME}" BASE_PATH "${BASE_PATH}" \
            TAKEN_AT "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    } > "${BACKUP}/manifest.env"
}

backup_phase_one() { # while the panel still runs: nothing here touches it
    backup_prepare_dir
    WAS_RUNNING="${RUNNING}"
    say "Backing up to ${BACKUP} ..."
    local parent base
    parent="$(dirname "${MAIN}")"
    base="$(basename "${MAIN}")"
    tar -C "${parent}" -czf "${BACKUP}/install-dir.tar.gz" "${base}" || return 1
    tar -tzf "${BACKUP}/install-dir.tar.gz" > /dev/null || return 1
    cp -p "${UNIT_FILE}" "${BACKUP}/x-ui.service" || return 1
    if [[ -d "${UNIT_FILE}.d" ]]; then cp -a "${UNIT_FILE}.d" "${BACKUP}/x-ui.service.d" || return 1; fi
    if [[ -n "${ENV_FILE}" ]]; then cp -p "${ENV_FILE}" "${BACKUP}/service-env" || return 1; fi
    if [[ -f "${USRBIN_XUI}" ]]; then cp -p "${USRBIN_XUI}" "${BACKUP}/usr-bin-x-ui" || return 1; fi
    local i=0 c
    for c in "${CERT_PATHS[@]+"${CERT_PATHS[@]}"}"; do
        if [[ -f "${c}" ]]; then
            i=$((i + 1))
            cp -pL "${c}" "${BACKUP}/certs/${i}.file" || return 1
            printf '%s\t%s\n' "${i}.file" "${c}" >> "${BACKUP}/certs/MAP"
        fi
    done
    if [[ "${HAVE_SQLITE}" == "yes" && "${RUNNING}" == "yes" ]]; then
        # A consistent snapshot while the panel writes; the exact-bytes copy follows the stop.
        sqlite3 -readonly "${DB_FILE}" ".backup '${BACKUP}/db/x-ui.db.live-snapshot'" || return 1
    fi
    return 0
}

backup_phase_two() { # after the panel stopped: the exact bytes rollback restores
    local f
    for f in "${DB_FILE}" "${DB_FILE}-wal" "${DB_FILE}-shm"; do
        if [[ -e "${f}" ]]; then cp -p "${f}" "${BACKUP}/db/$(basename "${f}")" || return 1; fi
    done
    local want got
    want="$(sha256sum "${DB_FILE}" | awk '{print $1}')"
    got="$(sha256sum "${BACKUP}/db/x-ui.db" | awk '{print $1}')"
    [[ "${want}" == "${got}" ]] || return 1
    echo "${want}" > "${BACKUP}/db/x-ui.db.sha256"
    if [[ "${HAVE_SQLITE}" == "yes" ]]; then
        # Checked on a scratch copy so the backup itself stays byte-identical.
        local scratch="${BACKUP}/db/.verify.db"
        cp "${BACKUP}/db/x-ui.db" "${scratch}"
        [[ -e "${BACKUP}/db/x-ui.db-wal" ]] && cp "${BACKUP}/db/x-ui.db-wal" "${scratch}-wal"
        local res
        res="$(sqlite3 "${scratch}" 'PRAGMA integrity_check;' 2> /dev/null || true)"
        rm -f "${scratch}" "${scratch}-wal" "${scratch}-shm"
        [[ "${res}" == "ok" ]] || return 1
    fi
    write_manifest
    install_rollback_helper || return 1
    (cd "${BACKUP}" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > "${BACKUP}.sums.tmp") || return 1
    mv "${BACKUP}.sums.tmp" "${BACKUP}/SHA256SUMS"
    return 0
}

install_rollback_helper() {
    if [[ -f "$0" ]]; then
        cp "$0" "${BACKUP}/upgrade-from-stock.sh" || return 1
    else
        curl -fsSL "https://raw.githubusercontent.com/${xui_repo}/${NEW_TAG}/upgrade-from-stock.sh" -o "${BACKUP}/upgrade-from-stock.sh" || return 1
    fi
    chmod 700 "${BACKUP}/upgrade-from-stock.sh"
    cat > "${BACKUP}/rollback.sh" << 'EOF'
#!/bin/bash
# Restores the panel exactly as it was before the netron-panel upgrade.
exec bash "$(dirname "$(readlink -f "$0")")/upgrade-from-stock.sh" rollback "$(dirname "$(readlink -f "$0")")" "$@"
EOF
    chmod 700 "${BACKUP}/rollback.sh"
}

# ---------------------------------------------------------------- stop / swap / start / health

stop_panel() {
    systemctl stop x-ui
    kill_sidecars
}

# The panel's own MTProto/TUIC sidecars can outlive a stop and hold ports (same cleanup the installers do).
kill_sidecars() {
    [[ -n "${ROOT}" ]] && return 0
    pkill -f 'mtg-linux-[^ ]* run ' > /dev/null 2>&1 || true
    pkill -f 'tuic-server.*-c .*bin/tuic/tuic_[0-9]+\.json' > /dev/null 2>&1 || true
    return 0
}

swap_files() {
    local new="${WORK}/x-ui" added=() u f rel
    install_atomic "${new}/x-ui" "${MAIN}/x-ui" 0755
    install_atomic "${new}/x-ui.sh" "${MAIN}/x-ui.sh" 0755
    install_atomic "${new}/x-ui.sh" "${USRBIN_XUI}" 0755
    for u in x-ui.service.debian x-ui.service.arch x-ui.service.rhel; do
        [[ -f "${new}/${u}" ]] && install_atomic "${new}/${u}" "${MAIN}/${u}" 0644
    done
    # bin/ stays as the operator has it; only files the stock install lacked are added.
    if [[ -d "${new}/bin" ]]; then
        while IFS= read -r -d '' f; do
            rel="${f#"${new}"/bin/}"
            if [[ ! -e "${MAIN}/bin/${rel}" ]]; then
                mkdir -p "${MAIN}/bin/$(dirname "${rel}")"
                cp -a "${f}" "${MAIN}/bin/${rel}"
                added+=("${rel}")
            fi
        done < <(find "${new}/bin" -type f -print0)
    fi
    if [[ ${#added[@]} -gt 0 ]]; then say "Added to bin/ (missing before): ${added[*]}"; fi
    local shipped_xray kept_xray
    shipped_xray="$(find "${new}/bin" -maxdepth 1 -name 'xray-linux-*' -type f 2> /dev/null | head -n1)"
    if [[ -n "${shipped_xray}" && -f "${MAIN}/bin/$(basename "${shipped_xray}")" ]]; then
        kept_xray="${MAIN}/bin/$(basename "${shipped_xray}")"
        if ! cmp -s "${shipped_xray}" "${kept_xray}"; then
            say "${yellow}Note: your bin/$(basename "${kept_xray}") differs from the one this release ships; it was kept as is.${plain}"
        fi
    fi
}

log_scan_regex='Error initializing database|Error auto migrating|Error creating the first panel user|Error starting web server|Error starting sub server|Error initializing node-token|panic:|fatal error:'

journal_since() { # journal_since EPOCH
    if command -v journalctl > /dev/null 2>&1; then
        journalctl -u x-ui --since "@$1" --no-pager -o cat 2> /dev/null || true
    fi
}

panel_log_file() {
    local d
    d="$(env_lookup XUI_LOG_FOLDER)"
    [[ -n "${d}" ]] || d="$(p /var/log/x-ui)"
    echo "${d}/3xui.log"
}

http_status() {
    curl -sk --max-time 5 -o /dev/null -w '%{http_code}' "${SCHEME}://${PROBE_HOST}:${PORT}${BASE_PATH}" 2> /dev/null || echo 000
}

port_listening() {
    local out
    if [[ -n "${PORT}" ]]; then
        out="$(ss -ltn 2> /dev/null || true)"
        awk -v p=":${PORT}\$" '$4 ~ p {found=1} END {exit !found}' <<< "${out}"
    else
        out="$(ss -ltnp 2> /dev/null || true)"
        grep -q '"x-ui"' <<< "${out}"
    fi
}

HEALTH_REASON=""
health_check() {
    local start_epoch="$1" log_size_before="$2" restarts_before="$3"
    local deadline=$((SECONDS + HEALTH_WAIT)) ok_since=-1 pid_first="" pid now status logfile journal newlog starts
    logfile="$(panel_log_file)"
    while ((SECONDS < deadline)); do
        now=$SECONDS
        # Captured into variables first: grep -q on a pipe can SIGPIPE the producer and
        # make pipefail report a failure after a successful match.
        journal="$(journal_since "${start_epoch}")"
        if grep -Eq "${log_scan_regex}" <<< "${journal}"; then
            HEALTH_REASON="the panel logged a startup/migration error (journal)"
            return 1
        fi
        newlog=""
        [[ -f "${logfile}" ]] && newlog="$(tail -c "+$((log_size_before + 1))" "${logfile}" 2> /dev/null || true)"
        if grep -Eq "${log_scan_regex}" <<< "${newlog}"; then
            HEALTH_REASON="the panel logged a startup/migration error (${logfile})"
            return 1
        fi
        starts="$(grep -c "Starting x-ui ${NEW_VERSION}" <<< "${journal}" || true)"
        if [[ "${starts:-0}" -gt 1 ]]; then
            HEALTH_REASON="the panel restarted by itself (crash loop)"
            return 1
        fi
        pid="$(systemctl show -p MainPID --value x-ui 2> /dev/null || true)"
        if systemctl is-active --quiet x-ui 2> /dev/null && port_listening; then
            status=200
            [[ -n "${PORT}" ]] && status="$(http_status)"
            if [[ "${status}" =~ ^[234][0-9][0-9]$ ]]; then
                [[ -z "${pid_first}" ]] && pid_first="${pid}"
                if [[ "${pid}" != "${pid_first}" ]]; then
                    HEALTH_REASON="the panel process changed while being watched (restarting)"
                    return 1
                fi
                ((ok_since < 0)) && ok_since=${now}
                if ((now - ok_since >= HEALTH_STABLE)); then
                    local restarts_after
                    restarts_after="$(systemctl show -p NRestarts --value x-ui 2> /dev/null || true)"
                    if [[ "${restarts_after}" != "${restarts_before}" ]]; then
                        HEALTH_REASON="systemd restarted the service during the check"
                        return 1
                    fi
                    return 0
                fi
            else
                ok_since=-1
            fi
        else
            ok_since=-1
        fi
        sleep 1
    done
    HEALTH_REASON="the panel did not become healthy (service active + port ${PORT:-?} listening + HTTP answer) within ${HEALTH_WAIT}s"
    return 1
}

multiplier_switch_off() {
    local v
    v="$(sql "SELECT value FROM settings WHERE key = 'usageMultiplierEnable' LIMIT 1;" || true)"
    [[ "${v}" != "true" ]]
}

# ---------------------------------------------------------------- rollback

load_manifest() {
    local dir="$1" k v
    [[ -f "${dir}/manifest.env" ]] || return 1
    while IFS='=' read -r k v; do
        v="${v#\'}"
        v="${v%\'}"
        case "${k}" in
            MAIN | DB_FILE | DB_DIR | UNIT_FILE | SERVICE_DIR | ENV_FILE | USRBIN_XUI | PREV_VERSION | NEW_VERSION | WAS_RUNNING | PORT | SCHEME | BASE_PATH | TAKEN_AT)
                printf -v "${k}" '%s' "${v}"
                ;;
            *) ;;
        esac
    done < "${dir}/manifest.env"
    return 0
}

do_rollback() {
    local dir="$1"
    load_manifest "${dir}" || { err "No manifest in ${dir}; cannot roll back from it."; return 1; }
    [[ -f "${dir}/db/x-ui.db" && -f "${dir}/install-dir.tar.gz" ]] || { err "${dir} is incomplete; cannot roll back from it."; return 1; }
    say "Rolling back from ${dir} ..."
    systemctl stop x-ui 2> /dev/null || true
    kill_sidecars

    local parent base restore
    parent="$(dirname "${MAIN}")"
    base="$(basename "${MAIN}")"
    restore="$(mktemp -d "${parent}/x-ui-restore.XXXXXX")"
    tar -xzf "${dir}/install-dir.tar.gz" -C "${restore}" || { err "Could not extract the install-dir backup."; safe_rm_rf "${restore}"; return 1; }
    [[ -s "${restore}/${base}/x-ui" ]] || { err "The install-dir backup has no panel binary."; safe_rm_rf "${restore}"; return 1; }
    local failed="${parent}/x-ui-netron-failed.$$"
    mv "${MAIN}" "${failed}"
    mv "${restore}/${base}" "${MAIN}"
    safe_rm_rf "${restore}"
    safe_rm_rf "${failed}"

    local f
    for f in "${DB_FILE}-wal" "${DB_FILE}-shm"; do rm -f "${f}"; done
    for f in x-ui.db x-ui.db-wal x-ui.db-shm; do
        [[ -e "${dir}/db/${f}" ]] && cp -p "${dir}/db/${f}" "$(dirname "${DB_FILE}")/${f}"
    done
    local want got
    want="$(cat "${dir}/db/x-ui.db.sha256")"
    got="$(sha256sum "${DB_FILE}" | awk '{print $1}')"
    [[ "${want}" == "${got}" ]] || { err "Restored database does not match the backup checksum."; return 1; }

    local reload=0
    if [[ -f "${dir}/x-ui.service" ]] && ! cmp -s "${dir}/x-ui.service" "${UNIT_FILE}"; then
        cp -p "${dir}/x-ui.service" "${UNIT_FILE}"
        reload=1
    fi
    if [[ -n "${ENV_FILE}" && -f "${dir}/service-env" ]] && ! cmp -s "${dir}/service-env" "${ENV_FILE}"; then
        cp -p "${dir}/service-env" "${ENV_FILE}"
    fi
    [[ -f "${dir}/usr-bin-x-ui" ]] && cp -p "${dir}/usr-bin-x-ui" "${USRBIN_XUI}"
    ((reload == 1)) && systemctl daemon-reload
    if [[ "${WAS_RUNNING}" == "yes" ]]; then
        systemctl start x-ui || { err "Restored everything but the service did not start; check 'systemctl status x-ui'."; return 1; }
    fi
    ok "Restored panel ${PREV_VERSION:-?} (binary, database, scripts) from ${dir}"
    return 0
}

# ---------------------------------------------------------------- commands

nodes_reminder() {
    say ""
    say "${blue}Node rollout order${plain}"
    say "  1. Upgrade the NODES first (run this script on each node), then the master."
    say "  2. A stock node ignores the multiplier and counts everything at x1. The panel"
    say "     warns about it in Settings and the log once a multiplier is set above 1."
    say "  3. Only then turn on 'Enable per-inbound multiplier' and restart Xray."
}

cmd_detect() {
    gather_facts
    decide
    print_report
    if [[ "${STATE}" == "refuse" ]]; then err "REFUSED: ${REFUSE_REASON}"; fi
    state_line
    case "${STATE}" in
        fresh) return "${EX_FRESH}" ;;
        netron) return "${EX_NETRON}" ;;
        refuse) return "${EX_REFUSED}" ;;
        old) return "${EX_OLD}" ;;
        *) return 0 ;;
    esac
}

delegate_update() {
    say "This server already runs netron-panel ${VERSION}; using the normal updater (nothing converted)."
    local tag_env=()
    [[ -n "${WANT_VERSION}" ]] && tag_env=("XUI_UPDATE_TAG=${WANT_VERSION}")
    exec env "${tag_env[@]+"${tag_env[@]}"}" bash <(curl -Ls "https://raw.githubusercontent.com/${xui_repo}/main/update.sh")
}

cmd_convert() {
    gather_facts
    decide
    print_report
    say ""
    case "${STATE}" in
        fresh)
            say "No existing panel found: nothing to convert (use the normal installer)."
            state_line
            return "${EX_FRESH}"
            ;;
        refuse)
            err "REFUSED: ${REFUSE_REASON}"
            err "Nothing was changed."
            state_line
            return "${EX_REFUSED}"
            ;;
        netron)
            state_line
            delegate_update
            ;;
        old)
            if [[ "${FORCE_OLD}" != "1" ]]; then
                err "Panel version ${VERSION:-unknown} is older than ${MIN_VERIFIED_VERSION}, the oldest stock version whose database this build was verified against."
                err "Nothing was changed. Update the stock panel first, or re-run with --force-old to accept the risk (the backup still protects you)."
                state_line
                return "${EX_OLD}"
            fi
            warn "--force-old given: converting an unverified version (${VERSION})."
            ;;
        *) ;;
    esac

    # ---- pre-flight (read-only)
    if [[ -z "${ROOT}" && "${EUID}" -ne 0 ]]; then die "Run as root. Nothing was changed." "${EX_REFUSED}"; fi
    systemd_present || die "systemd is required (OpenRC/Alpine installs are not verified). Nothing was changed." "${EX_REFUSED}"
    if [[ "${HAVE_SQLITE}" == "yes" ]]; then
        local ic
        ic="$(sqlite3 -readonly "${DB_FILE}" 'PRAGMA integrity_check;' 2> /dev/null || true)"
        [[ "${ic}" == "ok" ]] || die "SQLite integrity_check failed on ${DB_FILE}: ${ic:-no answer}. Not converting a damaged database; nothing was changed." "${EX_REFUSED}"
        ok "Database integrity_check: ok"
    else
        warn "sqlite3 is not installed: integrity check and live snapshot skipped (the database is still copied after a clean stop)."
    fi
    acquire_lock
    trap cleanup EXIT

    fetch_release

    local need_kb have_kb_backup have_kb_main
    need_kb=$(($(file_size_kb "${MAIN}") * 2 + $(file_size_kb "${DB_DIR}") * 3 + 262144))
    have_kb_backup="$(free_kb "${BACKUP_ROOT}")"
    have_kb_main="$(free_kb "$(dirname "${MAIN}")")"
    if [[ -n "${have_kb_backup}" && "${have_kb_backup}" -lt "${need_kb}" ]] || [[ -n "${have_kb_main}" && "${have_kb_main}" -lt 262144 ]]; then
        die "Not enough free disk space (need about $((need_kb / 1024)) MB for the backup). Nothing was changed." "${EX_REFUSED}"
    fi

    say ""
    say "${blue}Plan${plain}: ${VERSION} (stock) -> ${NEW_VERSION}"
    say "  KEPT     : database and all settings, credentials, port, base path, certificates,"
    say "             Xray config template, subscription settings, bin/ (Xray, geo files), systemd unit and drop-ins"
    say "  REPLACED : ${MAIN}/x-ui (panel binary), x-ui.sh and ${USRBIN_XUI} (menu script)"
    say "  BACKUP   : ${BACKUP_ROOT}/xui-backup-<time>/ (database, whole install dir, unit, certificates)"
    say "  The database schema is migrated one way on first start: an older stock binary will not run on it."
    say "  The multiplier switch stays OFF, so behaviour equals stock until you turn it on."
    if [[ "${NODES}" != "?" && "${NODES}" != "0" ]]; then
        warn "  This panel has ${NODES} node(s) configured. See the rollout order printed at the end: nodes first, master last."
    fi
    if [[ "${TG}" == "true" ]]; then say "  Note: the Telegram bot is enabled and will restart with the panel."; fi
    say "  The panel is stopped for the swap (usually well under a minute); running clients on Xray are interrupted."
    confirm "Convert this server to netron-panel ${NEW_VERSION}?" || { say "Cancelled. Nothing was changed."; return "${EX_DECLINED}"; }
    if [[ "${NODES}" != "?" && "${NODES}" != "0" && "${ASSUME_YES}" != "1" ]]; then
        confirm "This master has ${NODES} node(s). Do you understand that stock nodes keep counting at x1 until they are upgraded too?" ||
            { say "Cancelled. Nothing was changed."; return "${EX_DECLINED}"; }
    fi

    # ---- backup phase 1 (panel still running)
    if ! backup_phase_one; then
        err "Backup failed. Nothing was changed on the panel; the partial backup is left in ${BACKUP} for inspection."
        return "${EX_REFUSED}"
    fi
    local log_file log_size
    log_file="$(panel_log_file)"
    log_size=0
    [[ -f "${log_file}" ]] && log_size="$(stat -c %s "${log_file}")"

    # ---- stop, final backup, swap
    local was_running="${RUNNING}"
    stop_panel
    if ! backup_phase_two; then
        err "Final backup step failed; restarting the panel untouched. Nothing was changed."
        [[ "${was_running}" == "yes" ]] && systemctl start x-ui
        return "${EX_REFUSED}"
    fi
    ok "Backup complete: ${BACKUP}"
    say "Rollback any time with:  ${BACKUP}/rollback.sh   (or: x-ui rollback)"

    swap_files
    local start_epoch restarts_before
    start_epoch="$(date +%s)"
    restarts_before="$(systemctl show -p NRestarts --value x-ui 2> /dev/null || true)"
    if [[ "${was_running}" == "yes" || "${HAVE_UNIT}" == "yes" ]]; then
        say "Starting netron-panel ${NEW_VERSION} and checking its health for up to ${HEALTH_WAIT}s ..."
        systemctl start x-ui || true
        # Port/base path are re-read from the migrated DB (they must be unchanged).
        if health_check "${start_epoch}" "${log_size}" "${restarts_before}" && multiplier_switch_off; then
            :
        else
            [[ -z "${HEALTH_REASON}" ]] && HEALTH_REASON="the per-inbound multiplier switch is not OFF after the upgrade"
            err "Health check FAILED: ${HEALTH_REASON}"
            err "Rolling back automatically ..."
            if do_rollback "${BACKUP}"; then
                err "The server is back on stock ${VERSION}: binary, database bytes and scripts restored. Backup kept at ${BACKUP}."
                err "Look at: journalctl -u x-ui   and   $(panel_log_file)"
                return "${EX_FAILED_ROLLED_BACK}"
            fi
            err "AUTOMATIC ROLLBACK FAILED. Your backup is intact at ${BACKUP}; restore by hand with ${BACKUP}/rollback.sh"
            return "${EX_FAILED_NO_ROLLBACK}"
        fi
    fi

    ok "Converted: panel is now netron-panel ${NEW_VERSION} and healthy."
    say ""
    say "  Credentials, port, base path, certificates and settings are unchanged."
    say "  The multiplier switch is OFF (stock behaviour). Turn it on later in Settings, then restart Xray."
    say "  Backup (keep it): ${BACKUP}"
    say "  Roll back:        x-ui rollback    or    ${BACKUP}/rollback.sh"
    say "  Rolling back restores the database from the backup: anything written after the upgrade is lost."
    nodes_reminder
    return 0
}

cmd_rollback() {
    local dir="${ROLLBACK_DIR}"
    if [[ -z "${dir}" ]]; then
        dir="$(find "${BACKUP_ROOT}" -maxdepth 1 -type d -name 'xui-backup-*' 2> /dev/null | sort | tail -n1)"
    fi
    [[ -n "${dir}" && -d "${dir}" ]] || die "No backup found (looked in ${BACKUP_ROOT}/xui-backup-*)." "${EX_REFUSED}"
    load_manifest "${dir}" || die "${dir} has no manifest.env; not a backup from this script." "${EX_REFUSED}"
    if [[ -z "${ROOT}" && "${EUID}" -ne 0 ]]; then die "Run as root." "${EX_REFUSED}"; fi
    say "Backup: ${dir} (taken ${TAKEN_AT:-?}; stock ${PREV_VERSION:-?} -> ${NEW_VERSION:-?})"
    say "Rollback restores the stock binary, the menu script AND the database as they were then."
    warn "Everything written to the panel since the backup (traffic, new clients, edits) is lost."
    say "The database schema was migrated one way: going back to an older stock version needs this DB backup, not just the old binary."
    confirm "Restore this backup now?" || { say "Cancelled."; return "${EX_DECLINED}"; }
    acquire_lock
    trap cleanup EXIT
    if do_rollback "${dir}"; then
        return 0
    fi
    return "${EX_FAILED_NO_ROLLBACK}"
}

TAKEN_AT=""

usage() {
    sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'
}

parse_args() {
    [[ $# -gt 0 ]] || { usage; exit 2; }
    CMD="$1"
    shift
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --yes | -y) ASSUME_YES=1 ;;
            --force-old) FORCE_OLD=1 ;;
            --version)
                shift
                WANT_VERSION="${1:-}"
                ;;
            -*) die "Unknown option: $1" 2 ;;
            *) if [[ "${CMD}" == "rollback" && -z "${ROLLBACK_DIR}" ]]; then ROLLBACK_DIR="$1"; else WANT_VERSION="$1"; fi ;;
        esac
        shift
    done
    [[ -z "${WANT_VERSION}" || "${WANT_VERSION}" == v* ]] || WANT_VERSION="v${WANT_VERSION}"
}

STATE=""
main() {
    parse_args "$@"
    case "${CMD}" in
        detect) cmd_detect ;;
        convert) cmd_convert ;;
        rollback) cmd_rollback ;;
        *) usage; exit 2 ;;
    esac
}

main "$@"
