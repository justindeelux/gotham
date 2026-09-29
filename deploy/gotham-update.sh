#!/bin/sh
#
# gotham-update: privileged restart + healthcheck + rollback wrapper.
#
# The control plane swaps the verified binary at $GOTHAM_BINARY (keeping the
# previous one at $GOTHAM_BINARY.old), then runs this script as root through
# sudoers. It takes NO arguments: the binary, service, health URL and status
# file come from the root-owned configuration file, so a compromised control
# plane cannot make root restart another unit, move another file or probe
# another URL.
#
# This script always attempts to restore the previous binary when the restart
# or the health check fails, and it records the durable outcome in the status
# file the control plane reads on its next start.
#
# Configuration (root-owned, mode 0644): /etc/gotham/updater.conf
#   GOTHAM_BINARY=/var/lib/gotham/bin/gotham
#   GOTHAM_SERVICE=gotham
#   GOTHAM_HEALTH=http://127.0.0.1:8000/healthz
#   GOTHAM_TIMEOUT=30
#   GOTHAM_STATUS=/run/gotham/update.status
#
# GOTHAM_UPDATER_CONF may point at another configuration file for local
# testing; the production sudoers rule passes no arguments and sudo resets the
# environment, so a caller cannot set it through sudo.
#
# Exit codes: 0 = new binary healthy, 1 = rolled back (or rollback failed),
# 2 = invalid configuration.
set -u

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
STATUS="${GOTHAM_STATUS:-/run/gotham/update.status}"
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
    case "${SERVICE}" in
        "" | *[!A-Za-z0-9_.@-]*) log "invalid service name: ${SERVICE}"; exit 2 ;;
    esac
    case "${HEALTH}" in
        http://127.0.0.1:* | http://localhost:* | "http://[::1]:"*) ;;
        *) log "health URL must be a loopback http URL: ${HEALTH}"; exit 2 ;;
    esac
}

write_status() {
    # $1 result, $2 version, $3 detail
    status_dir=$(dirname "${STATUS}")
    mkdir -p "${status_dir}" 2>/dev/null || true
    tmp="${STATUS}.tmp.$$"
    if {
        printf 'result=%s\n' "$1"
        printf 'version=%s\n' "$2"
        printf 'detail=%s\n' "$3"
        printf 'at=%s\n' "$(date +%s)"
    } >"${tmp}" 2>/dev/null; then
        mv -f "${tmp}" "${STATUS}" 2>/dev/null || true
    fi
    rm -f "${tmp}" 2>/dev/null || true
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

# The control plane recorded the staged version before launching us; keep it so
# the final status names the release that was attempted.
VERSION=""
if [ -r "${STATUS}" ]; then
    VERSION=$(sed -n 's/^version=//p' "${STATUS}" 2>/dev/null | head -n 1)
fi

validate_conf

if [ ! -f "${BINARY}" ]; then
    write_status no_backup "${VERSION}" "binary ${BINARY} is missing"
    log "binary ${BINARY} is missing; nothing to activate"
    exit 1
fi

log "waiting ${GRACE}s before restarting ${SERVICE}"
sleep "${GRACE}"

# A failed restart must still attempt a rollback; never exit before that.
if ! restart_service; then
    if [ -f "${BACKUP}" ]; then
        log "restoring ${BACKUP} after a failed restart"
        if mv -f "${BACKUP}" "${BINARY}"; then
            restart_service || log "restart on the previous binary failed"
            write_status rolled_back "${VERSION}" "restart failed; previous binary restored"
        else
            write_status rollback_failed "${VERSION}" "restart failed; could not restore ${BACKUP}"
        fi
    else
        write_status no_backup "${VERSION}" "restart failed and no ${BACKUP} exists"
    fi
    exit 1
fi

if wait_healthy; then
    write_status ok "${VERSION}" "new binary healthy"
    log "new binary healthy"
    exit 0
fi

log "health check failed; restoring ${BACKUP}"
if [ ! -f "${BACKUP}" ]; then
    write_status no_backup "${VERSION}" "health check failed and no ${BACKUP} exists"
    exit 1
fi
if ! mv -f "${BACKUP}" "${BINARY}"; then
    write_status rollback_failed "${VERSION}" "health check failed; could not restore ${BACKUP}"
    exit 1
fi

restart_service || log "restart on the previous binary failed"
if wait_healthy; then
    write_status rolled_back "${VERSION}" "health check failed; previous binary restored"
    log "rolled back to the previous binary"
else
    write_status rollback_failed "${VERSION}" "previous binary is also unhealthy"
    log "previous binary is also unhealthy; manual intervention required"
fi
exit 1
