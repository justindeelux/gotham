#!/bin/sh
#
# gotham-update: privileged restart + healthcheck + rollback wrapper.
#
# The control plane swaps the verified binary at $GOTHAM_BINARY (hardlinking the
# previous one to $GOTHAM_BINARY.old), then runs this script as root through
# sudoers. It takes NO arguments and, when invoked through sudo, ignores every
# GOTHAM_* environment override: the binary, service, health URL, status,
# pending marker and lock come from the root-owned configuration file. sudoers
# grants only the bare command, so a compromised control plane cannot make root
# restart another unit, move another file or probe another URL.
#
# This script always attempts to restore the previous binary when the restart or
# the health check fails, releases the pending marker, and records the durable
# outcome in a root-owned status file the control plane reads.
#
# Configuration (root-owned, mode 0644): /etc/gotham/updater.conf
#   GOTHAM_BINARY=/var/lib/gotham/bin/gotham
#   GOTHAM_SERVICE=gotham
#   GOTHAM_HEALTH=http://127.0.0.1:8000/healthz
#   GOTHAM_TIMEOUT=30
#   GOTHAM_STATUS=/var/lib/gotham-updater/update.status   (root-owned dir)
#   GOTHAM_PENDING=/var/lib/gotham/update.pending
#   GOTHAM_LOCK=/var/lib/gotham/update.lock
#
# GOTHAM_UPDATER_CONF may point at another configuration file for local
# testing. It is honoured only when NOT running under sudo (the production
# sudoers rule passes no arguments, sudo resets the environment, and this
# script additionally refuses overrides when SUDO_USER/SUDO_UID is set).
#
# Exit codes: 0 = new binary healthy, 1 = rolled back (or rollback failed),
# 2 = invalid configuration.
set -u

# Refuse environment overrides when invoked through sudo. sudo(8) resets the
# environment by default and SETENV must stay disabled; this is defense in depth.
if [ -n "${SUDO_USER:-}" ] || [ -n "${SUDO_UID:-}" ]; then
    for variable in GOTHAM_UPDATER_CONF GOTHAM_BINARY GOTHAM_SERVICE GOTHAM_HEALTH \
        GOTHAM_TIMEOUT GOTHAM_STATUS GOTHAM_PENDING GOTHAM_LOCK GOTHAM_GRACE; do
        unset "${variable}" 2>/dev/null || true
    done
fi

if [ "$#" -ne 0 ]; then
    echo "gotham-update: this wrapper takes no arguments" >&2
    exit 2
fi

CONF="${GOTHAM_UPDATER_CONF:-/etc/gotham/updater.conf}"
if [ -r "${CONF}" ]; then
    # shellcheck source=/dev/null
    . "${CONF}"
fi

BINARY="${GOTHAM_BINARY:-/var/lib/gotham/bin/gotham}"
BACKUP="${BINARY}.old"
SERVICE="${GOTHAM_SERVICE:-gotham}"
HEALTH="${GOTHAM_HEALTH:-http://127.0.0.1:8000/healthz}"
TIMEOUT="${GOTHAM_TIMEOUT:-30}"
STATUS="${GOTHAM_STATUS:-/var/lib/gotham-updater/update.status}"
STATUS_DIR=$(dirname "${STATUS}")
PENDING="${GOTHAM_PENDING:-/var/lib/gotham/update.pending}"
LOCK="${GOTHAM_LOCK:-/var/lib/gotham/update.lock}"
GRACE="${GOTHAM_GRACE:-1}"

log() {
    echo "gotham-update: $*" >&2
}

# validate_conf refuses anything but the fixed, root-owned values.
validate_conf() {
    case "${BINARY}" in
        /*) ;;
        *) log "binary must be an absolute path: ${BINARY}"; exit 2 ;;
    esac
    case "${BINARY}" in
        *[[:space:]]*) log "binary path contains whitespace"; exit 2 ;;
    esac
    case "${SERVICE}" in
        "" | *[!A-Za-z0-9_.@-]*) log "invalid service name: ${SERVICE}"; exit 2 ;;
    esac
    case "${HEALTH}" in
        *"@"* | *"?"* | *"#"*) log "health URL must not contain @ ? #: ${HEALTH}"; exit 2 ;;
    esac
    case "${HEALTH}" in
        http://127.0.0.1:[0-9]* | http://localhost:[0-9]* | "http://[::1]:"[0-9]*) ;;
        *) log "health URL must be a loopback http URL: ${HEALTH}"; exit 2 ;;
    esac
}

# write_status writes the authoritative status into the root-owned status
# directory using an exclusive temp file. The directory is not writable by the
# service user, so a pre-planted symlink cannot redirect the root write.
write_status() {
    # $1 result, $2 version, $3 detail
    if [ -L "${STATUS}" ]; then
        log "refusing symlinked status file ${STATUS}"
        return 0
    fi
    mkdir -p "${STATUS_DIR}" 2>/dev/null || true
    tmp=$(mktemp "${STATUS_DIR}/.status.XXXXXX" 2>/dev/null) || {
        log "could not create a status temp file in ${STATUS_DIR}"
        return 0
    }
    if {
        printf 'result=%s\n' "$1"
        printf 'version=%s\n' "$2"
        printf 'detail=%s\n' "$3"
        printf 'at=%s\n' "$(date +%s)"
    } >"${tmp}" 2>/dev/null; then
        chmod 0644 "${tmp}" 2>/dev/null || true
        # mv -T treats the destination as a normal file; fall back where
        # unsupported (the root-owned directory already prevents redirection).
        mv -T "${tmp}" "${STATUS}" 2>/dev/null || mv -f "${tmp}" "${STATUS}" 2>/dev/null || true
    fi
    rm -f "${tmp}" 2>/dev/null || true
}

# finish records the outcome, releases the pending marker and exits.
finish() {
    # $1 result, $2 detail, $3 exit code
    write_status "$1" "${VERSION}" "$2"
    rm -f "${PENDING}" 2>/dev/null || true
    exit "$3"
}

# acquire_lock serializes with the control plane's Apply/Rollback.
acquire_lock() {
    if ! command -v flock >/dev/null 2>&1; then
        log "flock not available; proceeding without the update lock"
        return 0
    fi
    if ! exec 9>"${LOCK}" 2>/dev/null; then
        log "could not open lock ${LOCK}; proceeding without it"
        return 0
    fi
    flock -w 60 9 || log "could not acquire lock ${LOCK} within 60s"
}

probe() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsS -o /dev/null --max-time 5 "${HEALTH}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO /dev/null --timeout=5 "${HEALTH}"
    else
        log "curl or wget is required"
        return 1
    fi
}

wait_healthy() {
    deadline=$(( $(date +%s) + TIMEOUT ))
    while [ "$(date +%s)" -lt "${deadline}" ]; do
        if probe; then
            return 0
        fi
        sleep 1
    done
    return 1
}

restart_service() {
    if systemctl restart "${SERVICE}"; then
        return 0
    fi
    log "systemctl restart ${SERVICE} failed"
    return 1
}

# restore moves the backup over the target atomically. It refuses a symlinked
# backup so a planted link cannot redirect the root move.
restore() {
    if [ -L "${BACKUP}" ]; then
        log "refusing symlinked backup ${BACKUP}"
        return 1
    fi
    if [ -L "${BINARY}" ]; then
        log "refusing symlinked target ${BINARY}"
        return 1
    fi
    if [ ! -f "${BACKUP}" ]; then
        return 1
    fi
    if mv -T "${BACKUP}" "${BINARY}" 2>/dev/null; then
        return 0
    fi
    mv -f "${BACKUP}" "${BINARY}" 2>/dev/null
}

# The control plane recorded the staged version in the pending marker; keep it
# as a label for the final status.
VERSION=""
if [ -r "${PENDING}" ]; then
    VERSION=$(sed -n 's/^version=//p' "${PENDING}" 2>/dev/null | head -n 1 | tr -d '\r\n')
fi

validate_conf
acquire_lock

# Recover from an interrupted swap: a missing target with a valid backup is
# restored before anything else, so the unit's ExecStart always finds a binary.
if [ ! -e "${BINARY}" ] && [ -f "${BACKUP}" ]; then
    log "target ${BINARY} is missing; restoring ${BACKUP}"
    if restore; then
        restart_service || log "restart on the recovered binary failed"
        if wait_healthy; then
            finish rolled_back "recovered the previous binary after an interrupted update" 1
        fi
        finish rollback_failed "recovered the previous binary but it is unhealthy" 1
    fi
    finish no_backup "target is missing and the backup could not be restored" 1
fi

if [ ! -f "${BINARY}" ]; then
    finish no_backup "binary ${BINARY} is missing and no backup exists" 1
fi

log "waiting ${GRACE}s before restarting ${SERVICE}"
sleep "${GRACE}"

# A failed restart must still attempt a rollback; never exit before that.
if ! restart_service; then
    if restore; then
        log "restored ${BACKUP} after a failed restart"
        restart_service || log "restart on the previous binary failed"
        finish rolled_back "restart failed; previous binary restored" 1
    fi
    finish no_backup "restart failed and no backup could be restored" 1
fi

if wait_healthy; then
    write_status ok "${VERSION}" "new binary healthy"
    rm -f "${PENDING}" 2>/dev/null || true
    log "new binary healthy"
    exit 0
fi

log "health check failed; restoring ${BACKUP}"
if ! restore; then
    finish no_backup "health check failed and no backup could be restored" 1
fi
restart_service || log "restart on the previous binary failed"
if wait_healthy; then
    finish rolled_back "health check failed; previous binary restored" 1
fi
finish rollback_failed "previous binary is also unhealthy" 1
