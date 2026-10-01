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

# The default configuration is chosen by the installed name so one hardened
# wrapper serves both the control plane (gotham-update) and the node agent
# (gotham-agent-update). The file is root-owned and is the only source of the
# paths it acts on.
case "$(basename "$0")" in
    gotham-agent-update)
        DEFAULT_CONF="/etc/gotham/agent-updater.conf"
        # The agent wrapper fails closed on a missing config: it must never
        # fall back to the control-plane defaults and restart/swap the wrong
        # service.
        if [ ! -r "${GOTHAM_UPDATER_CONF:-${DEFAULT_CONF}}" ]; then
            echo "gotham-update: configuration ${GOTHAM_UPDATER_CONF:-${DEFAULT_CONF}} is missing or unreadable" >&2
            exit 2
        fi
        ;;
    *)
        DEFAULT_CONF="/etc/gotham/updater.conf"
        ;;
esac
CONF="${GOTHAM_UPDATER_CONF:-${DEFAULT_CONF}}"
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
# LOCK_PIN is the hardlink that pins the lock inode in the root-owned status
# directory for the duration of a run (acquire_lock); the trap removes it, and
# stale pins from hard-killed runs are swept when the next run starts.
LOCK_PIN=""

log() {
    echo "gotham-update: $*" >&2
}

# remove_nonregular_pending deletes the pending marker when it is not a regular
# file (a planted FIFO, device, directory or symlink). Such a marker can hang a
# reader, and it never carries a staged update, so removing it is safe; a
# regular staged marker is left in place so the control plane gate stays closed.
remove_nonregular_pending() {
    if [ -L "${PENDING}" ] || { [ -e "${PENDING}" ] && [ ! -f "${PENDING}" ]; }; then
        rm -f "${PENDING}" 2>/dev/null || true
    fi
}

# cleanup runs on every exit: drop a non-regular pending marker so it cannot
# hang a reader, and remove a lock pin this run created. A regular staged
# marker is preserved for the control plane.
cleanup() {
    remove_nonregular_pending
    if [ -n "${LOCK_PIN:-}" ]; then
        rm -f "${LOCK_PIN}" 2>/dev/null || true
    fi
}
trap 'cleanup' EXIT
# Route catchable termination signals through the EXIT trap too, so the pin is
# removed on TERM/INT/HUP and not only on normal exits; SIGKILL (and OOM) cannot
# be trapped, which is what the start-time sweep in acquire_lock covers.
trap 'exit 1' HUP INT TERM

# validate_conf refuses anything but the fixed, root-owned values. It returns
# nonzero on invalid config so the caller can clean up before exiting.
validate_conf() {
    case "${BINARY}" in
        /*) ;;
        *) log "binary must be an absolute path: ${BINARY}"; return 1 ;;
    esac
    case "${BINARY}" in
        *[[:space:]]*) log "binary path contains whitespace"; return 1 ;;
    esac
    case "${SERVICE}" in
        "" | *[!A-Za-z0-9_.@-]*) log "invalid service name: ${SERVICE}"; return 1 ;;
    esac
    case "${HEALTH}" in
        *"@"* | *"?"* | *"#"*) log "health URL must not contain @ ? #: ${HEALTH}"; return 1 ;;
    esac
    case "${HEALTH}" in
        http://127.0.0.1:[0-9]* | http://localhost:[0-9]* | "http://[::1]:"[0-9]*) ;;
        *) log "health URL must be a loopback http URL: ${HEALTH}"; return 1 ;;
    esac
    return 0
}

# status_dir_usable reports whether STATUS_DIR is (or can be created as) an
# existing, non-symlink, root-owned directory. That ownership floor is what
# makes the temp-file + mv status write and the lock pin safe: the service user
# cannot create, swap or redirect entries in a directory it does not own.
status_dir_usable() {
    [ ! -L "${STATUS_DIR}" ] || return 1
    if [ ! -d "${STATUS_DIR}" ]; then
        mkdir -p "${STATUS_DIR}" 2>/dev/null || return 1
    fi
    [ -O "${STATUS_DIR}" ]
}

# write_status writes the authoritative status into the root-owned status
# directory using an exclusive temp file. The directory must be an existing,
# non-symlink, root-owned directory (status_dir_usable); when that floor is
# broken the wrapper refuses instead of writing authoritative state into a
# service-writable directory. The update then records no outcome (fail closed):
# the control plane's monitor resolves the staged gate.
write_status() {
    # $1 result, $2 version, $3 detail
    if [ -L "${STATUS}" ]; then
        log "refusing symlinked status file ${STATUS}"
        return 0
    fi
    if ! status_dir_usable; then
        log "refusing to write status in ${STATUS_DIR}: not a root-owned directory"
        return 0
    fi
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

# sweep_stale_lock_pins removes pins left behind by wrappers that died without
# running their trap (SIGKILL, OOM). A pin's name embeds the PID of the wrapper
# that created it, so a pin whose PID is no longer alive — or whose PID is this
# process (PID reuse: a live wrapper cannot share our PID) — cannot belong to a
# live run. Only called for a directory that passed status_dir_usable, so root
# only ever unlinks a root-owned entry in a root-owned directory.
sweep_stale_lock_pins() {
    for pin in "${STATUS_DIR}/.update-lock."*; do
        [ -f "${pin}" ] && [ ! -L "${pin}" ] || continue
        pid="${pin##*.update-lock.}"
        case "${pid}" in
            '' | *[!0-9]*) continue ;;
        esac
        if [ "${pid}" -eq "$$" ] || ! kill -0 "${pid}" 2>/dev/null; then
            rm -f "${pin}" 2>/dev/null || true
        fi
    done
    return 0
}

# acquire_lock serializes with the control plane's Apply/Rollback. The lock
# lives in the service StateDirectory, so the service user owns every path
# component: a FIFO swapped in between the checks and the open would block
# root's open(2) forever (flock(1) opens blocking too, so `flock -w` cannot
# bound it).
#
# Root therefore pins the inode first: ln(2) hardlinks whatever the lock path
# resolves to into the root-owned status directory and never opens the inode,
# so a FIFO cannot block it. Only a pinned, verified regular file is opened;
# the pinned name cannot be swapped out of a root-owned directory, and locking
# the hardlink locks the same inode the control plane locks. When no pin can be
# made (no root-owned status directory, no hardlink support) it falls back to
# the checked read-only open; that residual is recorded in deploy/README.md.
acquire_lock() {
    if ! command -v flock >/dev/null 2>&1; then
        log "flock is unavailable; refusing to update without the lock"
        return 1
    fi
    LOCK_PIN=""
    if status_dir_usable; then
        sweep_stale_lock_pins
        pin="${STATUS_DIR}/.update-lock.$$"
        if ln "${LOCK}" "${pin}" 2>/dev/null; then
            # ln may dereference a symlink; require the pinned name itself to be
            # a regular file, the original path not to be a symlink, and both
            # to resolve to the same inode. A swapped FIFO fails -f; a swapped
            # symlink fails either -L; a swapped regular file fails -ef.
            if [ -f "${pin}" ] && [ ! -L "${pin}" ] && [ ! -L "${LOCK}" ] && [ "${LOCK}" -ef "${pin}" ]; then
                LOCK_PIN="${pin}"
            else
                rm -f "${pin}" 2>/dev/null || true
            fi
        fi
    fi
    if [ -n "${LOCK_PIN}" ]; then
        exec 9<"${LOCK_PIN}"
    else
        if [ -L "${LOCK}" ] || [ ! -f "${LOCK}" ] || [ ! -r "${LOCK}" ]; then
            log "refusing to lock ${LOCK}: not an existing regular file"
            return 1
        fi
        exec 9<"${LOCK}"
    fi
    if ! flock -w 60 9; then
        log "could not acquire lock ${LOCK} within 60s"
        return 1
    fi
    # A swap while waiting would leave root holding an inode the control plane
    # no longer locks; refuse rather than run unserialized.
    if [ -n "${LOCK_PIN}" ] && { [ -L "${LOCK}" ] || ! [ "${LOCK}" -ef "${LOCK_PIN}" ]; }; then
        log "lock ${LOCK} changed while acquiring it"
        return 1
    fi
    return 0
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
    # Always probe at least once. With a short timeout (the tests use
    # GOTHAM_TIMEOUT=1) the deadline can lapse between the `date` calls before
    # the loop body runs, which would record a healthy binary as rolled_back.
    # The do/while shape keeps the same deadline for the later attempts.
    deadline=$(( $(date +%s) + TIMEOUT ))
    while :; do
        if probe; then
            return 0
        fi
        if [ "$(date +%s)" -ge "${deadline}" ]; then
            return 1
        fi
        sleep 1
    done
}

restart_service() {
    # A crash-looping new binary can trip systemd's start rate limit
    # ("Start request repeated too quickly"), after which systemd refuses to
    # start the unit until its failed state is cleared. Without this, a healthy
    # rollback would be recorded as rollback_failed and the service would stay
    # down. Clearing the failed state is best-effort; the restart below is what
    # decides the outcome.
    if ! systemctl reset-failed "${SERVICE}"; then
        log "systemctl reset-failed ${SERVICE} failed; continuing"
    fi
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
# as a label for the final status. The marker is in the Gotham-writable
# StateDirectory, so read it only when it is a regular, non-symlink file and
# sanitize the value: a planted symlink/FIFO must not leak a root-only line or
# hang root.
VERSION=""
if [ -f "${PENDING}" ] && [ ! -L "${PENDING}" ]; then
    VERSION=$(sed -n 's/^version=//p' "${PENDING}" 2>/dev/null | head -n 1 | tr -d '\r\n')
fi
case "${VERSION}" in
    "" | *[!A-Za-z0-9._+-]*) VERSION="" ;;
esac
if [ "${#VERSION}" -gt 64 ]; then
    VERSION=""
fi

if ! validate_conf; then
    remove_nonregular_pending
    exit 2
fi
# Fail closed on a lock failure: record the outcome and keep a regular staged
# marker in place so the gate stays closed (the control plane's monitor will
# roll back, or an operator resets). A non-regular marker is removed so it
# cannot hang a reader.
if ! acquire_lock; then
    write_status wrapper_failed "${VERSION}" "could not acquire the update lock"
    remove_nonregular_pending
    log "refusing to continue without the update lock"
    exit 1
fi

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
        if wait_healthy; then
            finish rolled_back "restart failed; previous binary restored" 1
        fi
        finish rollback_failed "restart failed; previous binary restored but unhealthy" 1
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
