#!/bin/sh
#
# test-agent-install.sh exercises the reinstall behaviours of install-agent.sh
# without root or a systemd host:
#
#   A3-7  agent_env_write preserves values a prior install wrote for keys this
#         invocation does not set (CP address / node id), lets ambient values
#         win, keeps operator-added keys, and leaves the file mode 0640.
#   U2    a duplicate managed key resolves to the last occurrence (systemd
#         EnvironmentFile semantics).
#   U3    a plaintext -> TLS reinstall drops the loopback listener and INSECURE;
#         --insecure overrides a preserved non-loopback listener and rejects an
#         ambient one.
#   U4    a symlinked agent.env keeps its link (the target is rewritten).
#   U5    an indented managed key keeps its value.
#   U6    --help documents every managed key.
#   A3-8  agent_service_restart restarts an active unit onto the new binary and
#         starts an inactive one, failing when the unit's ExecStart does not
#         reference the installed binary.
#   F1    install-agent.sh --full is opt-in (flag or GOTHAM_AGENT_FULL=1),
#         stays fail-closed without a CA, and wires ensure_docker_full.
#   F2    ensure_docker_full skips when Docker works, refuses non-Ubuntu/
#         Debian with the manual step, installs via the pinned Docker apt
#         repository, and fails closed on a fingerprint mismatch.
#
# The systemd calls are answered by a PATH shim, so nothing outside the scratch
# directory (and the process environment) is touched.
#
# Usage:
#   sh deploy/test-agent-install.sh
#
# Requirements: sh, grep, sed, awk, stat, readlink, mktemp (present on CI).

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
# shellcheck disable=SC2034  # read by agent_env_write through eval
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
# --insecure requires a loopback listener: a preserved non-loopback address is
# overridden (the agent refuses to start with a non-loopback plaintext listener).
T3B_ENV="${T3_DIR}/agent-preserved.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443\n' >"${T3B_ENV}"
agent_env_write "${T3B_ENV}" "" 1
grep -qx 'GOTHAM_AGENT_LISTEN_ADDR=127.0.0.1:9443' "${T3B_ENV}" \
    || fail "a preserved non-loopback listener was not overridden for --insecure"
[ "$(grep -c '^GOTHAM_AGENT_LISTEN_ADDR=' "${T3B_ENV}")" -eq 1 ] \
    || fail "preserved listener gained a duplicate entry"
pass "--insecure overrides a preserved non-loopback listener to loopback"

# --- U2: a duplicate key takes the last value (systemd EnvironmentFile) -------
echo "==> U2 duplicate key takes the last value"
T2_DIR="${SCRATCH}/u2"
mkdir -p "${T2_DIR}"
T2_ENV="${T2_DIR}/agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=stale:9443\nGOTHAM_AGENT_CP_ADDR=latest:9443\n' >"${T2_ENV}"
agent_env_write "${T2_ENV}" "/etc/gotham/ca.crt" 0
grep -qx 'GOTHAM_AGENT_CP_ADDR=latest:9443' "${T2_ENV}" \
    || fail "duplicate key did not take the last value"
[ "$(grep -c '^GOTHAM_AGENT_CP_ADDR=' "${T2_ENV}")" -eq 1 ] \
    || fail "duplicate key was not collapsed to one line"
pass "duplicate managed key resolves to the last occurrence"

# --- U3: plaintext -> TLS reinstall drops the loopback listener ---------------
echo "==> U3 plaintext->TLS reinstall drops the loopback listener"
T4_DIR="${SCRATCH}/u3"
mkdir -p "${T4_DIR}"
T4_ENV="${T4_DIR}/agent.env"
printf 'GOTHAM_AGENT_INSECURE=true\nGOTHAM_AGENT_LISTEN_ADDR=127.0.0.1:9443\n' >"${T4_ENV}"
agent_env_write "${T4_ENV}" "/etc/gotham/ca.crt" 0
grep -qx 'GOTHAM_AGENT_CA=/etc/gotham/ca.crt' "${T4_ENV}" \
    || fail "TLS reinstall did not write the CA"
if grep -q '^GOTHAM_AGENT_LISTEN_ADDR=' "${T4_ENV}"; then
    fail "TLS reinstall kept the plaintext loopback listener"
fi
if grep -q '^GOTHAM_AGENT_INSECURE=' "${T4_ENV}"; then
    fail "TLS reinstall kept GOTHAM_AGENT_INSECURE"
fi
pass "plaintext->TLS reinstall drops the loopback listener and INSECURE"

# An ambient non-loopback listener under --insecure must fail closed.
T5_ENV="${T4_DIR}/insecure-ambient.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp:9443\n' >"${T5_ENV}"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443
if agent_env_write "${T5_ENV}" "" 1 2>/dev/null; then
    fail "--insecure accepted a non-loopback ambient listener"
else
    pass "--insecure rejects a non-loopback ambient listener"
fi
unset_agent_env

# --- U1/U3: --insecure with a CA must not force loopback -----------------------
echo "==> U1 --insecure alongside --ca keeps the TLS listener"
T8_DIR="${SCRATCH}/u1"
mkdir -p "${T8_DIR}"
T8_ENV="${T8_DIR}/agent.env"
printf 'GOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443\n' >"${T8_ENV}"
agent_env_write "${T8_ENV}" "/etc/gotham/ca.crt" 1
grep -qxF 'GOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443' "${T8_ENV}" \
    || fail "--ca --insecure rewrote the listener to loopback"
grep -qxF 'GOTHAM_AGENT_CA=/etc/gotham/ca.crt' "${T8_ENV}" \
    || fail "--ca --insecure did not write the CA"
if grep -q '^GOTHAM_AGENT_INSECURE=' "${T8_ENV}"; then
    fail "--ca --insecure wrote GOTHAM_AGENT_INSECURE"
fi
pass "--ca --insecure keeps the listener and writes the CA"

# Plain TLS -> TLS reinstall keeps a non-loopback listener (remote CP reachable).
T9_ENV="${T8_DIR}/tls-reinstall.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp:9443\nGOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443\n' >"${T9_ENV}"
agent_env_write "${T9_ENV}" "/etc/gotham/ca.crt" 0
grep -qxF 'GOTHAM_AGENT_LISTEN_ADDR=0.0.0.0:9443' "${T9_ENV}" \
    || fail "TLS->TLS reinstall dropped a non-loopback listener"
pass "TLS->TLS reinstall keeps a non-loopback listener"

# A case-variant prior INSECURE=True behaves like true: drop the loopback
# listener when switching to TLS.
T10_ENV="${T8_DIR}/insecure-case.env"
printf 'GOTHAM_AGENT_INSECURE=True\nGOTHAM_AGENT_LISTEN_ADDR=127.0.0.1:9443\n' >"${T10_ENV}"
agent_env_write "${T10_ENV}" "/etc/gotham/ca.crt" 0
if grep -q '^GOTHAM_AGENT_LISTEN_ADDR=' "${T10_ENV}"; then
    fail "case-variant INSECURE=True kept the loopback listener through --ca"
fi
pass "case-variant INSECURE=True is treated as true"

# --- U2: installer loopback validation matches the agent ----------------------
echo "==> U2 loopback validation parity"
for _bad in '127.example.com:9443' '::1:9443' '0.0.0.0:9443' '127.0.0.300:9443'; do
    _f="${T8_DIR}/bad.env"
    printf 'GOTHAM_AGENT_CP_ADDR=cp:9443\n' >"${_f}"
    # shellcheck disable=SC2034  # read by agent_env_write through eval
    GOTHAM_AGENT_LISTEN_ADDR="${_bad}"
    if agent_env_write "${_f}" "" 1 2>/dev/null; then
        fail "--insecure accepted non-loopback ${_bad}"
    fi
    unset_agent_env
done
for _good in 'localhost:9443' 'LOCALHOST:9443' '127.0.0.1:9443' '[::1]:9443'; do
    _f="${T8_DIR}/good.env"
    printf 'GOTHAM_AGENT_CP_ADDR=cp:9443\n' >"${_f}"
    # shellcheck disable=SC2034  # read by agent_env_write through eval
    GOTHAM_AGENT_LISTEN_ADDR="${_good}"
    agent_env_write "${_f}" "" 1
    grep -qxF "GOTHAM_AGENT_LISTEN_ADDR=${_good}" "${_f}" \
        || fail "--insecure rejected loopback ${_good}"
    unset_agent_env
done
pass "loopback parity: rejects non-loopback, accepts localhost/127.0.0.1/[::1]"

# --- U4: a symlinked agent.env keeps its link ---------------------------------
echo "==> U4 symlinked agent.env keeps its link"
T6_DIR="${SCRATCH}/u4"
mkdir -p "${T6_DIR}"
T6_REAL="${T6_DIR}/real-agent.env"
T6_LINK="${T6_DIR}/agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=linked:9443\nOPERATOR_EXTRA=keepme\n' >"${T6_REAL}"
ln -s "${T6_REAL}" "${T6_LINK}"
agent_env_write "${T6_LINK}" "/etc/gotham/ca.crt" 0
[ -L "${T6_LINK}" ] || fail "agent_env_write replaced a symlinked agent.env"
grep -qx 'GOTHAM_AGENT_CP_ADDR=linked:9443' "${T6_REAL}" \
    || fail "symlink target was not rewritten"
grep -qx 'GOTHAM_AGENT_CA=/etc/gotham/ca.crt' "${T6_REAL}" \
    || fail "symlink target did not receive the CA"
pass "symlinked agent.env is rewritten through the link"

# --- U5: an indented managed key keeps its value -------------------------------
echo "==> U5 indented managed key keeps its value"
T7_DIR="${SCRATCH}/u5"
mkdir -p "${T7_DIR}"
T7_ENV="${T7_DIR}/agent.env"
printf '  GOTHAM_AGENT_CP_ADDR=indented:9443\n' >"${T7_ENV}"
agent_env_write "${T7_ENV}" "/etc/gotham/ca.crt" 0
grep -qx 'GOTHAM_AGENT_CP_ADDR=indented:9443' "${T7_ENV}" \
    || fail "an indented managed key was silently dropped"
[ "$(grep -c '^GOTHAM_AGENT_CP_ADDR=' "${T7_ENV}")" -eq 1 ] \
    || fail "indented managed key produced a duplicate or stray line"
pass "indented managed key keeps its value"

# --- U6: --help documents every managed key -----------------------------------
echo "==> U6 --help lists every managed key"
HELP_OUT="$(sh "${AGENT_INSTALLER}" --help 2>&1 || true)"
for _key in \
    GOTHAM_AGENT_CP_ADDR \
    GOTHAM_AGENT_NODE_ID \
    GOTHAM_AGENT_LISTEN_ADDR \
    GOTHAM_AGENT_CA \
    GOTHAM_AGENT_INSECURE \
    GOTHAM_AGENT_CERT_DIR \
    GOTHAM_AGENT_KEY \
    GOTHAM_AGENT_DOCKER_SOCK \
    GOTHAM_AGENT_LOG_LEVEL \
    GOTHAM_AGENT_AUTO_UPDATE \
    GOTHAM_AGENT_UPDATE_INTERVAL \
    GOTHAM_AGENT_UPDATE_CHANNEL; do
    printf '%s' "${HELP_OUT}" | grep -q "${_key}" \
        || fail "--help does not document ${_key}"
done
pass "--help documents every managed key"

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

# --- F1: --full Docker setup -------------------------------------------------
echo "==> F1 --full installs Docker Engine + the compose plugin (Ubuntu/Debian only)"
printf 'dummy CA for the --full dry-run path\n' >"${SCRATCH}/full-ca.pem"
# --help documents --full.
env GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
    sh "${AGENT_INSTALLER}" --help 2>&1 | grep -q -- '--full' \
    || fail "--help does not document --full"
# The CA gate stays first: --full without a CA still fails closed.
if env GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
    sh "${AGENT_INSTALLER}" --full --dry-run >"${SCRATCH}/full-no-ca.log" 2>&1; then
    fail "--full ran without a CA (agent channel would be plaintext)"
fi
grep -q "no control-plane CA certificate configured" "${SCRATCH}/full-no-ca.log" \
    || fail "--full without a CA failed for an unexpected reason"
# With a CA, --full announces the Docker setup in dry-run mode.
env GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
    sh "${AGENT_INSTALLER}" --full --ca "${SCRATCH}/full-ca.pem" --dry-run \
    >"${SCRATCH}/full-dry.log" 2>&1 \
    || fail "--full --ca --dry-run failed"
grep -q 'ensure Docker Engine and the compose plugin (--full)' "${SCRATCH}/full-dry.log" \
    || fail "--full --dry-run did not announce the Docker setup"
# Default off: without --full (flag or environment) no Docker setup is printed.
env GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
    sh "${AGENT_INSTALLER}" --ca "${SCRATCH}/full-ca.pem" --dry-run \
    >"${SCRATCH}/no-full-dry.log" 2>&1 \
    || fail "--ca --dry-run without --full failed"
if grep -q 'ensure Docker Engine' "${SCRATCH}/no-full-dry.log"; then
    fail "Docker setup ran without --full (default must stay off)"
fi
# The environment form enables it too.
env GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test GOTHAM_AGENT_FULL=1 \
    sh "${AGENT_INSTALLER}" --ca "${SCRATCH}/full-ca.pem" --dry-run \
    >"${SCRATCH}/full-env-dry.log" 2>&1 \
    || fail "GOTHAM_AGENT_FULL=1 --dry-run failed"
grep -q 'ensure Docker Engine and the compose plugin (--full)' "${SCRATCH}/full-env-dry.log" \
    || fail "GOTHAM_AGENT_FULL=1 did not enable the Docker setup"
# The installer wires the helper (static guard; the live path needs root/apt).
grep -q 'ensure_docker_full' "${AGENT_INSTALLER}" \
    || fail "install-agent.sh does not call ensure_docker_full for --full"
pass "--full is opt-in, fail-closed without a CA, and wired to ensure_docker_full"

# --- F2: ensure_docker_full behaviour ----------------------------------------
echo "==> F2 ensure_docker_full skips, refuses, and installs via shims"
F2_DIR="${SCRATCH}/full-flow"
F2_SHIM="${F2_DIR}/shim"
mkdir -p "${F2_SHIM}" "${F2_DIR}/os" "${F2_DIR}/apt"
printf 'ID=ubuntu\nVERSION_CODENAME=jammy\n' >"${F2_DIR}/os/os-release"
printf 'ID=arch\nVERSION_CODENAME=n/a\n' >"${F2_DIR}/os/arch-release"
cat >"${F2_SHIM}/docker" <<'SHIM'
#!/bin/sh
# Succeeds only once the "install" (the curl shim) drops the marker.
if [ "$1" = "compose" ]; then
    [ -f "${DOCKER_MARKER:?}/installed" ] && exit 0
    exit 1
fi
exit 0
SHIM
chmod +x "${F2_SHIM}/docker"
FULL_PATH="${F2_SHIM}:${PATH}"
export FULL_PATH
# Already installed: skipped before the distro is even read.
DOCKER_MARKER="${F2_DIR}/marker-present"
export DOCKER_MARKER
mkdir -p "${DOCKER_MARKER}"
touch "${DOCKER_MARKER}/installed"
if GOTHAM_OS_RELEASE_FILE=/nonexistent PATH="${FULL_PATH}" ensure_docker_full >"${F2_DIR}/skip.log" 2>&1; then
    grep -q 'already installed; skipping' "${F2_DIR}/skip.log" \
        || fail "skip path did not log the skip"
else
    fail "ensure_docker_full did not skip when Docker already works"
fi
rm -rf "${DOCKER_MARKER}"
DOCKER_MARKER="${F2_DIR}/marker-absent"
export DOCKER_MARKER
mkdir -p "${DOCKER_MARKER}"
# Unsupported distro: fails with the manual step, touching nothing.
if GOTHAM_OS_RELEASE_FILE="${F2_DIR}/os/arch-release" GOTHAM_APT_ROOT="${F2_DIR}/apt" \
    PATH="${FULL_PATH}" ensure_docker_full >"${F2_DIR}/arch.log" 2>&1; then
    fail "ensure_docker_full accepted an unsupported distro"
else
    grep -q 'supports Ubuntu/Debian only' "${F2_DIR}/arch.log" \
        || fail "unsupported distro failed without the manual-step message"
fi
[ ! -e "${F2_DIR}/apt/etc/apt/sources.list.d/docker.list" ] \
    || fail "unsupported distro still wrote an apt source"
pass "skip-when-present and refuse-elsewhere hold"
# Full flow against shims: pinned key, atomic repo file, expected packages.
cat >"${F2_SHIM}/curl" <<'SHIM'
#!/bin/sh
# Minimal -o parser: writes a dummy key and drops the installed marker.
out=""
prev=""
for arg in "$@"; do
    if [ "${prev}" = "-o" ]; then out="${arg}"; fi
    prev="${arg}"
done
[ -n "${out}" ] || exit 1
printf 'dummy-docker-key\n' >"${out}"
mkdir -p "${DOCKER_MARKER:?}"
touch "${DOCKER_MARKER}/installed"
exit 0
SHIM
cat >"${F2_SHIM}/gpg" <<'SHIM'
#!/bin/sh
case "$*" in
    *--show-keys*)
        echo "fpr:::::::::9DC858229FC7DD38854AE2D88D81803C0EBFCD88:"
        exit 0
        ;;
    *--dearmor*)
        out=""; prev=""; src=""
        for arg in "$@"; do
            if [ "${prev}" = "-o" ]; then out="${arg}"; else src="${arg}"; fi
            prev="${arg}"
        done
        cp "${src}" "${out}"
        exit 0
        ;;
esac
exit 1
SHIM
cat >"${F2_SHIM}/dpkg" <<'SHIM'
#!/bin/sh
echo amd64
SHIM
cat >"${F2_SHIM}/apt-get" <<'SHIM'
#!/bin/sh
echo "apt-get $*" >>"${APT_LOG:?}"
exit 0
SHIM
chmod +x "${F2_SHIM}/curl" "${F2_SHIM}/gpg" "${F2_SHIM}/dpkg" "${F2_SHIM}/apt-get"
APT_LOG="${F2_DIR}/apt.log"
export APT_LOG
: >"${APT_LOG}"
if GOTHAM_OS_RELEASE_FILE="${F2_DIR}/os/os-release" GOTHAM_APT_ROOT="${F2_DIR}/apt" \
    PATH="${FULL_PATH}" ensure_docker_full >"${F2_DIR}/flow.log" 2>&1; then
    grep -qx 'deb \[arch=amd64 signed-by='"${F2_DIR}"'/apt/etc/apt/keyrings/docker.gpg\] https://download.docker.com/linux/ubuntu jammy stable' \
        "${F2_DIR}/apt/etc/apt/sources.list.d/docker.list" \
        || fail "apt source has the wrong content"
    [ -f "${F2_DIR}/apt/etc/apt/keyrings/docker.gpg" ] \
        || fail "keyring file was not written"
    grep -q 'docker-ce-cli' "${APT_LOG}" && grep -q 'docker-compose-plugin' "${APT_LOG}" \
        || fail "apt was not asked for the engine and compose plugin packages"
    grep -q 'containerd.io' "${APT_LOG}" && grep -q 'docker-buildx-plugin' "${APT_LOG}" \
        || fail "apt was not asked for containerd.io and the buildx plugin"
    pass "shimmed install writes the pinned repo and requests the engine + compose plugin"
else
    fail "ensure_docker_full failed against shims"
fi
# A wrong fingerprint aborts before any apt path is written.
cat >"${F2_SHIM}/gpg" <<'SHIM'
#!/bin/sh
case "$*" in
    *--show-keys*)
        echo "fpr:::::::::AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA:"
        exit 0
        ;;
esac
exit 1
SHIM
chmod +x "${F2_SHIM}/gpg"
rm -rf "${F2_DIR}/apt2" "${DOCKER_MARKER}/installed"
if GOTHAM_OS_RELEASE_FILE="${F2_DIR}/os/os-release" GOTHAM_APT_ROOT="${F2_DIR}/apt2" \
    PATH="${FULL_PATH}" ensure_docker_full >"${F2_DIR}/fp.log" 2>&1; then
    fail "ensure_docker_full accepted a key with the wrong fingerprint"
else
    grep -q 'fingerprint mismatch' "${F2_DIR}/fp.log" \
        || fail "wrong fingerprint failed without the mismatch message"
fi
[ ! -e "${F2_DIR}/apt2/etc/apt/sources.list.d/docker.list" ] \
    || fail "a wrong-fingerprint key still wrote an apt source"
pass "a wrong fingerprint fails closed before any apt path is written"

if [ "${FAILURES}" -eq 0 ]; then
    echo "ALL AGENT-INSTALL TESTS PASSED"
    exit 0
fi
echo "test-agent-install: ${FAILURES} check(s) failed" >&2
exit 1
