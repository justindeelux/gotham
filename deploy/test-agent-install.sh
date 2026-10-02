#!/bin/sh
#
# test-agent-install.sh exercises the two reinstall behaviours of install-agent.sh
# without root or a systemd host:
#
#   A3-7  agent_env_write preserves values a prior install wrote for keys this
#         invocation does not set (CP address / node id), lets ambient values
#         win, keeps operator-added keys, and leaves the file mode 0640.
#   A3-8  agent_service_restart restarts an active unit onto the new binary and
#         starts an inactive one, failing when the unit's ExecStart does not
#         reference the installed binary.
#
# The systemd calls are answered by a PATH shim, so nothing outside the scratch
# directory (and the process environment) is touched.
#
# Usage:
#   sh deploy/test-agent-install.sh
#
# Requirements: sh, grep, sed, stat, mktemp (present on the CI runner).

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
AGENT_INSTALLER="${SCRIPT_DIR}/install-agent.sh"

# shellcheck source=deploy/install-agent-lib.sh
. "${SCRIPT_DIR}/install-agent-lib.sh"

FAILURES=0
pass() { echo "PASS: $*"; }
fail() {
    echo "FAIL: $*" >&2
    FAILURES=$((FAILURES + 1))
}

SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/gotham-agent-install-test.XXXXXX")"
cleanup() { rm -rf "${SCRATCH}"; }
trap cleanup EXIT INT TERM

mode_of() {
    if stat -c '%a' "$1" >/dev/null 2>&1; then
        stat -c '%a' "$1"
    else
        stat -f '%Lp' "$1"
    fi
}

# The managed keys are cleared so the tests do not depend on the caller's
# environment; each case sets only what it means to test.
unset_agent_env() {
    unset GOTHAM_AGENT_CP_ADDR GOTHAM_AGENT_NODE_ID GOTHAM_AGENT_LISTEN_ADDR \
        GOTHAM_AGENT_CERT_DIR GOTHAM_AGENT_KEY GOTHAM_AGENT_DOCKER_SOCK \
        GOTHAM_AGENT_LOG_LEVEL GOTHAM_AGENT_AUTO_UPDATE \
        GOTHAM_AGENT_UPDATE_INTERVAL GOTHAM_AGENT_UPDATE_CHANNEL \
        GOTHAM_AGENT_CA GOTHAM_AGENT_INSECURE
}
unset_agent_env

# --- A3-7: reinstall preserves values it does not override --------------------
echo "==> A3-7 reinstall preserves prior CP address and node id"
T1_DIR="${SCRATCH}/t1"
mkdir -p "${T1_DIR}"
T1_ENV="${T1_DIR}/agent.env"
cat >"${T1_ENV}" <<'EOF'
GOTHAM_AGENT_CP_ADDR=cp.example.com:9443
GOTHAM_AGENT_NODE_ID=node-preserved
OPERATOR_EXTRA=keepme
EOF
chmod 0600 "${T1_ENV}" # a prior restrictive mode must be normalized to 0640

if agent_env_write "${T1_ENV}" "/etc/gotham/ca.crt" 0; then
    grep -qx 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443' "${T1_ENV}" \
        || fail "reinstall dropped the prior GOTHAM_AGENT_CP_ADDR"
    grep -qx 'GOTHAM_AGENT_NODE_ID=node-preserved' "${T1_ENV}" \
        || fail "reinstall dropped the prior GOTHAM_AGENT_NODE_ID"
    grep -qx 'OPERATOR_EXTRA=keepme' "${T1_ENV}" \
        || fail "reinstall dropped an operator-added key"
    grep -qx 'GOTHAM_AGENT_CA=/etc/gotham/ca.crt' "${T1_ENV}" \
        || fail "reinstall did not write the resolved CA path"
    [ "$(grep -c '^GOTHAM_AGENT_CP_ADDR=' "${T1_ENV}")" -eq 1 ] \
        || fail "reinstall duplicated GOTHAM_AGENT_CP_ADDR"
    [ "$(mode_of "${T1_ENV}")" = "640" ] \
        || fail "agent.env mode is $(mode_of "${T1_ENV}"), want 640"
    pass "prior CP address, node id and operator key preserved (mode 640)"
else
    fail "agent_env_write failed on the preserve path"
fi

# --- A3-7: ambient values supplied this run win ------------------------------
echo "==> A3-7 ambient values override the prior file"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_CP_ADDR=override.example.com:9443
GOTHAM_AGENT_NODE_ID=node-override
agent_env_write "${T1_ENV}" "/etc/gotham/ca.crt" 0
unset_agent_env
grep -qx 'GOTHAM_AGENT_CP_ADDR=override.example.com:9443' "${T1_ENV}" \
    || fail "ambient GOTHAM_AGENT_CP_ADDR did not override the prior value"
grep -qx 'GOTHAM_AGENT_NODE_ID=node-override' "${T1_ENV}" \
    || fail "ambient GOTHAM_AGENT_NODE_ID did not override the prior value"
[ "$(grep -c '^GOTHAM_AGENT_CP_ADDR=' "${T1_ENV}")" -eq 1 ] \
    || fail "override left a duplicate GOTHAM_AGENT_CP_ADDR"
grep -qx 'OPERATOR_EXTRA=keepme' "${T1_ENV}" \
    || fail "override dropped the operator key"
pass "ambient values win and remain unique"

# --- A3-7: --insecure defaults the listener without duplicating it -----------
echo "==> A3-7 --insecure default listener"
T3_DIR="${SCRATCH}/t3"
mkdir -p "${T3_DIR}"
T3_ENV="${T3_DIR}/agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\n' >"${T3_ENV}"
agent_env_write "${T3_ENV}" "" 1
grep -qx 'GOTHAM_AGENT_INSECURE=true' "${T3_ENV}" \
    || fail "--insecure did not write GOTHAM_AGENT_INSECURE=true"
grep -qx 'GOTHAM_AGENT_LISTEN_ADDR=127.0.0.1:9443' "${T3_ENV}" \
    || fail "--insecure did not default the loopback listener"
[ "$(grep -c '^GOTHAM_AGENT_LISTEN_ADDR=' "${T3_ENV}")" -eq 1 ] \
    || fail "--insecure wrote a duplicate listener address"
# A preserved listener must not gain a second default entry.
T3B_ENV="${T3_DIR}/agent-preserved.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443\n' >"${T3B_ENV}"
agent_env_write "${T3B_ENV}" "" 1
grep -qx 'GOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443' "${T3B_ENV}" \
    || fail "preserved listener address was lost"
[ "$(grep -c '^GOTHAM_AGENT_LISTEN_ADDR=' "${T3B_ENV}")" -eq 1 ] \
    || fail "preserved listener gained a duplicate entry"
pass "--insecure writes one listener address, preserved or default"

# --- A3-8: restart an active unit, start an inactive one ---------------------
echo "==> A3-8 restart/start decision"
SHIM_DIR="${SCRATCH}/shim"
mkdir -p "${SHIM_DIR}"
cat >"${SHIM_DIR}/systemctl" <<'SHIM'
#!/bin/sh
log="${SYSTEMCTL_LOG:?}"
case "$1" in
    is-active)
        printf 'is-active %s\n' "$2" >>"${log}"
        if [ "${SYSTEMCTL_ACTIVE:-0}" -eq 1 ]; then exit 0; else exit 3; fi
        ;;
    restart)
        printf 'restart %s\n' "$2" >>"${log}"
        if [ "${SYSTEMCTL_FAIL_RESTART:-0}" -eq 1 ]; then exit 1; fi
        ;;
    start)
        printf 'start %s\n' "$2" >>"${log}"
        ;;
    show)
        printf 'show %s\n' "$*" >>"${log}"
        printf 'ExecStart={ path=%s ; argv[]=%s ; ignore_errors=no }\n' \
            "${SYSTEMCTL_EXECSTART:-}" "${SYSTEMCTL_EXECSTART:-}"
        ;;
    *)
        printf 'other %s\n' "$*" >>"${log}"
        ;;
esac
exit 0
SHIM
chmod +x "${SHIM_DIR}/systemctl"
PATH="${SHIM_DIR}:${PATH}"
export PATH

BIN="/var/lib/gotham-agent/bin/gotham-agent"
SYSTEMCTL_LOG="${SCRATCH}/systemctl.log"
SYSTEMCTL_ACTIVE=1
SYSTEMCTL_EXECSTART="${BIN}"
SYSTEMCTL_FAIL_RESTART=0
export SYSTEMCTL_LOG SYSTEMCTL_ACTIVE SYSTEMCTL_EXECSTART SYSTEMCTL_FAIL_RESTART

: >"${SYSTEMCTL_LOG}"
if agent_service_restart gotham-agent.service "${BIN}"; then
    grep -q '^restart gotham-agent.service$' "${SYSTEMCTL_LOG}" \
        || fail "an active unit was not restarted"
    if grep -q '^start gotham-agent.service$' "${SYSTEMCTL_LOG}"; then
        fail "an active unit was started instead of restarted"
    fi
    pass "active unit restarted onto the new binary"
else
    fail "agent_service_restart failed for an active unit"
fi

: >"${SYSTEMCTL_LOG}"
SYSTEMCTL_ACTIVE=0
if agent_service_restart gotham-agent.service "${BIN}"; then
    grep -q '^start gotham-agent.service$' "${SYSTEMCTL_LOG}" \
        || fail "an inactive unit was not started"
    if grep -q '^restart ' "${SYSTEMCTL_LOG}"; then
        fail "an inactive unit was restarted instead of started"
    fi
    pass "inactive unit started"
else
    fail "agent_service_restart failed for an inactive unit"
fi

# The verification must fail when the unit still execs a different binary.
SYSTEMCTL_ACTIVE=1
SYSTEMCTL_EXECSTART="/opt/old/gotham-agent"
if agent_service_restart gotham-agent.service "${BIN}" 2>/dev/null; then
    fail "agent_service_restart accepted a unit pointing at the old binary"
else
    pass "unit still on the old binary is rejected"
fi

# A failing restart must propagate, not be reported as success.
SYSTEMCTL_EXECSTART="${BIN}"
SYSTEMCTL_FAIL_RESTART=1
if agent_service_restart gotham-agent.service "${BIN}" 2>/dev/null; then
    fail "a failed systemctl restart was reported as success"
else
    pass "a failed restart propagates"
fi
SYSTEMCTL_FAIL_RESTART=0

# --- static wiring: the installer actually uses these helpers -----------------
echo "==> installer wiring"
grep -q 'agent_env_write "${ENV_FILE}"' "${AGENT_INSTALLER}" \
    || fail "install-agent.sh does not call agent_env_write for agent.env"
grep -q 'agent_service_restart gotham-agent.service' "${AGENT_INSTALLER}" \
    || fail "install-agent.sh does not call agent_service_restart"
if grep -q 'systemctl enable --now' "${AGENT_INSTALLER}"; then
    fail "install-agent.sh still uses a blind 'enable --now' (reinstall would not restart)"
fi
pass "install-agent.sh wires both helpers and no longer blind-enables"

if [ "${FAILURES}" -eq 0 ]; then
    echo "ALL AGENT-INSTALL TESTS PASSED"
    exit 0
fi
echo "test-agent-install: ${FAILURES} check(s) failed" >&2
exit 1
