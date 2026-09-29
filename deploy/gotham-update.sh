#!/bin/sh
#
# gotham-update.sh restarts the control plane after an in-place binary swap and
# rolls back to <binary>.old when the new binary fails its health check.
#
# The in-process updater (internal/updates.Applier) writes the verified binary
# over <binary> and leaves the previous one at <binary>.old; it then runs this
# script as root (see deploy/gotham.service and deploy/install-sudoers.sh). The
# script is the only thing that may restart the unit, so a crash it cannot
# observe (a binary that panics on start) still ends with the old binary active.
#
# Usage:
#   gotham-update.sh [--binary /usr/local/bin/gotham] \
#                    [--health http://127.0.0.1:8000/healthz] \
#                    [--service gotham] [--timeout 30]
#
# Exit codes: 0 = new binary healthy, 1 = rolled back (or rollback failed).
set -eu

BINARY="/usr/local/bin/gotham"
OLD="${BINARY}.old"
HEALTH="http://127.0.0.1:8000/healthz"
SERVICE="gotham"
TIMEOUT=30
# Give the HTTP response of the request that triggered the update time to flush
# before the restart kills the process.
GRACE=1

while [ "$#" -gt 0 ]; do
    case "$1" in
        --binary) BINARY="$2"; OLD="${BINARY}.old"; shift 2 ;;
        --health) HEALTH="$2"; shift 2 ;;
        --service) SERVICE="$2"; shift 2 ;;
        --timeout) TIMEOUT="$2"; shift 2 ;;
        -h | --help) sed -n '2,20p' "$0"; exit 0 ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
done

log() {
    echo "gotham-update: $*"
}

# probe returns 0 when the health endpoint answers.
probe() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsS -o /dev/null --max-time 5 "${HEALTH}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO /dev/null --timeout=5 "${HEALTH}"
    else
        echo "gotham-update: curl or wget is required" >&2
        return 1
    fi
}

# wait_healthy polls the health endpoint until it succeeds or TIMEOUT elapses.
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

if [ ! -f "${BINARY}" ]; then
    log "binary ${BINARY} is missing; nothing to restart"
    exit 1
fi
if [ ! -f "${OLD}" ]; then
    log "no ${OLD} to roll back to; refusing to restart unattended"
    exit 1
fi

log "waiting ${GRACE}s before restarting ${SERVICE}"
sleep "${GRACE}"

log "restarting ${SERVICE}"
systemctl restart "${SERVICE}"

if wait_healthy; then
    log "new binary healthy"
    exit 0
fi

log "health check failed; restoring ${OLD}" >&2
if ! mv -f "${OLD}" "${BINARY}"; then
    log "rollback failed: could not restore ${OLD}" >&2
    exit 1
fi

log "restarting ${SERVICE} on the previous binary"
systemctl restart "${SERVICE}"

if wait_healthy; then
    log "rolled back to the previous binary"
    exit 1
fi

log "previous binary is also unhealthy; manual intervention required" >&2
exit 1
