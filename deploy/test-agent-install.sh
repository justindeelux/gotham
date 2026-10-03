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
#   R3    re-running --full after a partial run succeeds: dearmoring into an
#         existing docker.gpg works (temp file in the same directory + atomic
#         mv) and a stale docker.list is rewritten.
#   R4    an engine without the compose plugin gets only the plugin
#         (docker-compose-plugin, then docker-compose-v2); no engine package
#         is ever named, so apt cannot replace or remove the working engine.
#   R5    a Docker repository the operator already defined (docker.sources, a
#         docker.asc Signed-By line, another .list) is reused: no conflicting
#         docker.list is added and the key is not re-downloaded. Commented-out
#         entries and Enabled:no stanzas do not count as a repository.
#   R6    a broken dpkg fails fast with a clear message (no amd64 fallback);
#         the codename is probed against the Docker repository
#         (.../dists/<codename>/Release): an HTTP 404 for Debian
#         testing/sid falls back to the newest stable suite the repo serves,
#         and an unserved codename with no fallback refuses instead of
#         writing a dead docker.list.
#   R7    the key download is HTTPS-pinned, TLS 1.2+, retried, and its temp
#         file is removed on every path without installing an EXIT trap.
#   H1/M1 the GOTHAM_OS_RELEASE_FILE / GOTHAM_APT_ROOT seams are honoured
#         only in the hard test mode (a GOTHAM_INSTALL_ROOT sandbox, or
#         GOTHAM_INSTALL_TEST=1 with DRY_RUN=1): a stray export never
#         redirects a root install.
#   L1    the codename probe times out, surfaces curl errors, and refuses on
#         a network failure (never silently falling back) while an HTTP 404
#         keeps the documented fallback/refusal.
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

# Hard test mode for the lib seams: GOTHAM_OS_RELEASE_FILE / GOTHAM_APT_ROOT
# are honoured only with a GOTHAM_INSTALL_ROOT sandbox (or
# GOTHAM_INSTALL_TEST=1 with DRY_RUN=1), so a stray export never redirects a
# root install. Export a scratch sandbox so the seam-driven F2/R cases below
# keep exercising them; the H1/M1 gating cases unset it in subshells.
GOTHAM_INSTALL_ROOT="${SCRATCH}/sandbox-root"
export GOTHAM_INSTALL_ROOT

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
# Nothing works until the "install" (the curl shim) drops the marker, so the
# full engine-install path is exercised (an engine that already exists takes
# the plugin-only branch covered by R4 instead).
[ -f "${DOCKER_MARKER:?}/installed" ] && exit 0
exit 1
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
# Minimal -o parser: writes a dummy key and drops the installed marker. A
# .../dists/<suite>/Release probe is answered with the HTTP code on stdout
# (200 served, 404 not served); only jammy is served by this fake repo.
case "$*" in
    */dists/*/Release*)
        case "$*" in
            */dists/jammy/Release*) printf '200' ;;
            *) printf '404' ;;
        esac
        exit 0
        ;;
esac
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
cat >"${F2_SHIM}/apt-get" <<'SHIM'
#!/bin/sh
echo "apt-get $*" >>"${APT_LOG:?}"
# A successful engine install provides docker, like the real one: write a
# working docker shim into the minimal PATH and drop the marker the skip
# check looks for.
case "$*" in
    *docker-ce*)
        cat >"${F2_MINPATH:?}/docker" <<'DOCKERSHIM'
#!/bin/sh
[ -f "${DOCKER_MARKER:?}/installed" ] && exit 0
exit 1
DOCKERSHIM
        chmod +x "${F2_MINPATH}/docker"
        mkdir -p "${DOCKER_MARKER:?}"
        touch "${DOCKER_MARKER}/installed"
        ;;
esac
exit 0
SHIM
chmod +x "${F2_SHIM}/curl" "${F2_SHIM}/gpg" "${F2_SHIM}/dpkg" "${F2_SHIM}/apt-get"
APT_LOG="${F2_DIR}/apt.log"
F2_MINPATH="${F2_DIR}/minpath"
export APT_LOG F2_MINPATH
# From nothing: a minimal PATH holding symlinks to the system tools plus the
# shims, and nothing else. A real docker elsewhere on PATH (a dev Mac, a CI
# runner) must not leak into the presence check, and the fake install above
# materializes docker only on success.
mkdir -p "${F2_MINPATH}"
for _tool in sed tr head mktemp awk grep mkdir chmod dirname mv rm cp touch; do
    _tool_path="$(command -v "${_tool}")" && ln -sf "${_tool_path}" "${F2_MINPATH}/${_tool}"
done
for _tool in curl gpg dpkg apt-get; do
    ln -sf "${F2_SHIM}/${_tool}" "${F2_MINPATH}/${_tool}"
done
: >"${APT_LOG}"
hash -r 2>/dev/null || true
# NOTE: an inline PATH= assignment before a function call persists after it
# returns, so the minimal PATH is saved and restored around the call.
SAVED_MINPATH="${PATH}"
PATH="${F2_MINPATH}"
if GOTHAM_OS_RELEASE_FILE="${F2_DIR}/os/os-release" GOTHAM_APT_ROOT="${F2_DIR}/apt" \
    ensure_docker_full >"${F2_DIR}/flow.log" 2>&1; then
    PATH="${SAVED_MINPATH}"
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
    PATH="${SAVED_MINPATH}"
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

# --- R3-R7: review round 1 (partial re-run, engine-only, existing repo,
#             arch/codename, curl hardening) -----------------------------------
echo "==> R3-R7 review round 1 cases against fresh shims"
R_DIR="${SCRATCH}/round1"
R_SHIM="${R_DIR}/shim"
mkdir -p "${R_SHIM}" "${R_DIR}/os" "${R_DIR}/apt"
printf 'ID=ubuntu\nVERSION_CODENAME=jammy\n' >"${R_DIR}/os/ubuntu-release"
printf 'ID=debian\nVERSION_CODENAME=sid\n' >"${R_DIR}/os/debian-sid-release"
printf 'ID=debian\nVERSION_CODENAME=testing\n' >"${R_DIR}/os/debian-testing-release"
# The docker on PATH delegates to the variant the current case selects
# (docker-absent = from nothing, docker-"" = engine present). R_DOCKER_RUN is
# exported so the delegating shim sees it.
cat >"${R_SHIM}/docker" <<'SHIM'
#!/bin/sh
exec "${R_DOCKER_RUN:?}" "$@"
SHIM
# Engine-only docker: plain docker works, compose works only after the
# "install" (the apt-get shim) drops the marker.
cat >"${R_SHIM}/docker-" <<'SHIM'
#!/bin/sh
if [ "$1" = "compose" ]; then
    [ -f "${DOCKER_MARKER:?}/installed" ] && exit 0
    exit 1
fi
exit 0
SHIM
# No-docker-at-all variant for the from-nothing path (R3): nothing works
# until the apt-get shim drops the marker.
cat >"${R_SHIM}/docker-absent" <<'SHIM'
#!/bin/sh
[ -f "${DOCKER_MARKER:?}/installed" ] && exit 0
exit 1
SHIM
cat >"${R_SHIM}/curl" <<'SHIM'
#!/bin/sh
# Logs its arguments (R7). A .../dists/<suite>/Release probe is answered with
# the HTTP code on stdout: 200 when the suite is in DOCKER_SERVED_SUITES,
# 404 otherwise. DOCKER_CURL_FAIL=1 makes every probe fail like a dead
# network (exit 6 with the real error on stderr). The key download writes a
# dummy key to the -o target.
echo "curl $*" >>"${CURL_LOG:?}"
case "$*" in
    */dists/*/Release*)
        if [ "${DOCKER_CURL_FAIL:-0}" = "1" ]; then
            echo "curl: (6) Could not resolve host: download.docker.com" >&2
            exit 6
        fi
        for _suite in ${DOCKER_SERVED_SUITES:-}; do
            case "$*" in
                */dists/"${_suite}"/Release*) printf '200'; exit 0 ;;
            esac
        done
        printf '404'
        exit 0
        ;;
esac
out=""
prev=""
for arg in "$@"; do
    if [ "${prev}" = "-o" ]; then out="${arg}"; fi
    prev="${arg}"
done
[ -n "${out}" ] || exit 1
printf 'dummy-docker-key\n' >"${out}"
exit 0
SHIM
cat >"${R_SHIM}/gpg" <<'SHIM'
#!/bin/sh
# Faithful dearmor: like the real gpg, it refuses to overwrite an existing
# -o file, so only a temp-file + mv caller survives a re-run (R3).
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
        if [ -e "${out}" ]; then
            echo "gpg: file '${out}' exists" >&2
            exit 1
        fi
        cp "${src}" "${out}"
        exit 0
        ;;
esac
exit 1
SHIM
cat >"${R_SHIM}/dpkg" <<'SHIM'
#!/bin/sh
echo amd64
SHIM
cat >"${R_SHIM}/dpkg-fail" <<'SHIM'
#!/bin/sh
echo "dpkg: error" >&2
exit 1
SHIM
cat >"${R_SHIM}/apt-get" <<'SHIM'
#!/bin/sh
echo "apt-get $*" >>"${APT_LOG:?}"
# A successful package install makes docker (compose) work, like the real one.
case "$*" in
    *docker-ce* | *docker-compose-plugin* | *docker-compose-v2* | *containerd.io*)
        mkdir -p "${DOCKER_MARKER:?}"
        touch "${DOCKER_MARKER}/installed"
        ;;
esac
exit 0
SHIM
chmod +x "${R_SHIM}/docker" "${R_SHIM}/docker-" "${R_SHIM}/docker-absent" "${R_SHIM}/curl" \
    "${R_SHIM}/gpg" "${R_SHIM}/dpkg" "${R_SHIM}/dpkg-fail" "${R_SHIM}/apt-get"
R_PATH="${R_SHIM}:${PATH}"
export R_PATH
run_full() {
    # $1 os-release fixture, $2 apt root, $3 docker marker dir, $4 log prefix.
    # The marker/log paths are exported so the shim subprocesses see them.
    DOCKER_MARKER="$3"
    APT_LOG="$4-apt.log"
    CURL_LOG="$4-curl.log"
    export DOCKER_MARKER APT_LOG CURL_LOG
    GOTHAM_OS_RELEASE_FILE="$1" GOTHAM_APT_ROOT="$2" \
    PATH="${R_PATH}" ensure_docker_full >"$4-out.log" 2>&1
}
set_docker_variant() {
    ln -sf "${R_SHIM}/docker-$1" "${R_SHIM}/docker-run"
}
R_DOCKER_RUN="${R_SHIM}/docker-run"
export R_DOCKER_RUN
# Suites the fake Docker repository serves (the codename probe answers from
# this list); cases that need a different repo override it per call.
# DOCKER_CURL_FAIL=1 makes every probe fail like a dead network (L1).
DOCKER_SERVED_SUITES="jammy trixie"
DOCKER_CURL_FAIL=0
export DOCKER_SERVED_SUITES DOCKER_CURL_FAIL
# The docker on PATH delegates to the selected variant.
cat >"${R_SHIM}/docker" <<'SHIM'
#!/bin/sh
exec "${R_DOCKER_RUN:?}" "$@"
SHIM
chmod +x "${R_SHIM}/docker"

# --- R3: re-run after a partial run (stale keyring + stale own source) -------
echo "==> R3 re-run after a partial run succeeds"
set_docker_variant absent
R3_APT="${R_DIR}/apt-r3"
R3_MARKER="${R_DIR}/marker-r3"
mkdir -p "${R3_MARKER}" "${R3_APT}/etc/apt/keyrings" "${R3_APT}/etc/apt/sources.list.d"
printf 'stale-keyring\n' >"${R3_APT}/etc/apt/keyrings/docker.gpg"
printf 'deb [arch=amd64] https://stale.example.com/ubuntu jammy stable\n' >"${R3_APT}/etc/apt/sources.list.d/docker.list"
: >"${R_DIR}/r3-curl.log"
if run_full "${R_DIR}/os/ubuntu-release" "${R3_APT}" "${R3_MARKER}" "${R_DIR}/r3"; then
    grep -qx 'dummy-docker-key' "${R3_APT}/etc/apt/keyrings/docker.gpg" \
        || fail "R3: stale docker.gpg was not replaced (dearmor must use temp+mv)"
    grep -qx 'deb \[arch=amd64 signed-by='"${R3_APT}"'/etc/apt/keyrings/docker.gpg\] https://download.docker.com/linux/ubuntu jammy stable' \
        "${R3_APT}/etc/apt/sources.list.d/docker.list" \
        || fail "R3: stale docker.list was not rewritten"
    pass "R3: re-run after a partial run replaces the keyring and the source"
else
    fail "R3: ensure_docker_full failed on a re-run after a partial run"
fi

# --- R4: engine present, compose missing -> plugin only ----------------------
echo "==> R4 engine without compose gets only the plugin"
set_docker_variant ""
R4_APT="${R_DIR}/apt-r4"
R4_MARKER="${R_DIR}/marker-r4"
mkdir -p "${R4_MARKER}"
if run_full "${R_DIR}/os/ubuntu-release" "${R4_APT}" "${R4_MARKER}" "${R_DIR}/r4"; then
    grep -q 'install -y docker-compose-plugin' "${R_DIR}/r4-apt.log" \
        || fail "R4: apt was not asked for only the compose plugin"
    if grep -q 'docker-ce' "${R_DIR}/r4-apt.log"; then
        fail "R4: an engine package was named while a working engine exists"
    fi
    if grep -q 'containerd.io' "${R_DIR}/r4-apt.log"; then
        fail "R4: containerd.io was named while a working engine exists"
    fi
    grep -q 'keeping the existing engine' "${R_DIR}/r4-out.log" \
        || fail "R4: the engine-preserving path did not log itself"
    pass "R4: engine kept, only the compose plugin installed"
else
    fail "R4: ensure_docker_full failed with an engine but no compose plugin"
fi

# --- R5: pre-existing Docker repository is reused, not duplicated ------------
echo "==> R5 pre-existing Docker repository is reused"
set_docker_variant ""
R5_APT="${R_DIR}/apt-r5"
R5_MARKER="${R_DIR}/marker-r5"
mkdir -p "${R5_MARKER}" "${R5_APT}/etc/apt/sources.list.d"
cat >"${R5_APT}/etc/apt/sources.list.d/operator-docker.sources" <<'SOURCES'
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: jammy
Components: stable
Signed-By: /etc/apt/keyrings/docker.asc
SOURCES
: >"${R_DIR}/r5-curl.log"
if run_full "${R_DIR}/os/ubuntu-release" "${R5_APT}" "${R5_MARKER}" "${R_DIR}/r5"; then
    [ ! -e "${R5_APT}/etc/apt/sources.list.d/docker.list" ] \
        || fail "R5: a conflicting docker.list was added next to the existing repository"
    [ ! -s "${R_DIR}/r5-curl.log" ] \
        || fail "R5: the signing key was re-downloaded although a repository is already configured"
    grep -q 'reusing it instead of adding a conflicting source' "${R_DIR}/r5-out.log" \
        || fail "R5: the reuse path did not log itself"
    grep -q 'apt-get update' "${R_DIR}/r5-apt.log" \
        || fail "R5: apt-get update did not run against the reused repository"
    pass "R5: existing Docker repository reused, no conflicting source added"
else
    fail "R5: ensure_docker_full failed with a pre-existing Docker repository"
fi
# A commented-out download.docker.com line is not a repository: it must be
# ignored and our own docker.list written.
set_docker_variant ""
R5B_APT="${R_DIR}/apt-r5b"
R5B_MARKER="${R_DIR}/marker-r5b"
mkdir -p "${R5B_MARKER}" "${R5B_APT}/etc/apt/sources.list.d"
printf '# deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu jammy stable\n' \
    >"${R5B_APT}/etc/apt/sources.list.d/operator-docker.list"
if run_full "${R_DIR}/os/ubuntu-release" "${R5B_APT}" "${R5B_MARKER}" "${R_DIR}/r5b"; then
    [ -f "${R5B_APT}/etc/apt/sources.list.d/docker.list" ] \
        || fail "R5: a commented-out entry was mistaken for an existing repository"
else
    fail "R5: ensure_docker_full failed with only a commented-out entry present"
fi
pass "R5: commented-out entries do not count as an existing repository"
# A DEB822 stanza with Enabled: no is disabled: it must be ignored too, while
# an enabled stanza still counts.
R5C_APT="${R_DIR}/apt-r5c"
R5C_MARKER="${R_DIR}/marker-r5c"
mkdir -p "${R5C_MARKER}" "${R5C_APT}/etc/apt/sources.list.d"
cat >"${R5C_APT}/etc/apt/sources.list.d/operator-docker.sources" <<'SOURCES'
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: jammy
Components: stable
Enabled: no
SOURCES
if run_full "${R_DIR}/os/ubuntu-release" "${R5C_APT}" "${R5C_MARKER}" "${R_DIR}/r5c"; then
    [ -f "${R5C_APT}/etc/apt/sources.list.d/docker.list" ] \
        || fail "R5: an Enabled:no stanza was mistaken for an existing repository"
else
    fail "R5: ensure_docker_full failed with only a disabled stanza present"
fi
pass "R5: Enabled:no stanzas do not count as an existing repository"

# --- R6: broken dpkg fails fast; testing/sid maps to stable ------------------
echo "==> R6 arch detection fails fast, testing/sid maps to stable"
mkdir -p "${R_SHIM}/nodpkg"
ln -sf "${R_SHIM}/dpkg-fail" "${R_SHIM}/nodpkg/dpkg"
set_docker_variant ""
R6_APT="${R_DIR}/apt-r6"
R6_MARKER="${R_DIR}/marker-r6"
mkdir -p "${R6_MARKER}"
DOCKER_MARKER="${R6_MARKER}"
APT_LOG="${R_DIR}/r6-apt.log"
CURL_LOG="${R_DIR}/r6-curl.log"
export DOCKER_MARKER APT_LOG CURL_LOG
GOTHAM_OS_RELEASE_FILE="${R_DIR}/os/ubuntu-release" GOTHAM_APT_ROOT="${R6_APT}" \
PATH="${R_SHIM}/nodpkg:${R_PATH}" ensure_docker_full >"${R_DIR}/r6-out.log" 2>&1 || R6_RC=$?
if [ "${R6_RC:-0}" -eq 0 ]; then
    fail "R6: a broken dpkg was silently treated as amd64"
else
    grep -q 'dpkg --print-architecture' "${R_DIR}/r6-out.log" \
        || fail "R6: broken dpkg failed without naming dpkg --print-architecture"
    [ ! -s "${R_DIR}/r6-curl.log" ] \
        || fail "R6: the key download ran although the architecture is unknown"
    pass "R6: broken dpkg fails fast with a clear message (no amd64 fallback)"
fi
for _r6case in sid testing; do
    R6B_APT="${R_DIR}/apt-r6b-${_r6case}"
    R6B_MARKER="${R_DIR}/marker-r6b-${_r6case}"
    mkdir -p "${R6B_MARKER}"
    if run_full "${R_DIR}/os/debian-${_r6case}-release" "${R6B_APT}" "${R6B_MARKER}" "${R_DIR}/r6b-${_r6case}"; then
        grep -qx 'deb \[arch=amd64 signed-by='"${R6B_APT}"'/etc/apt/keyrings/docker.gpg\] https://download.docker.com/linux/debian trixie stable' \
            "${R6B_APT}/etc/apt/sources.list.d/docker.list" \
            || fail "R6: Debian ${_r6case} did not map to the trixie repository"
        grep -q "does not serve '${_r6case}'; falling back to 'trixie'" "${R_DIR}/r6b-${_r6case}-out.log" \
            || fail "R6: Debian ${_r6case} fell back silently (the fallback must be logged)"
    else
        fail "R6: Debian ${_r6case} was refused instead of mapping to stable"
    fi
done
pass "R6: Debian testing/sid installs from the newest stable codename"
# A codename the repo serves is used as-is (no fallback): jammy stays jammy.
R6C_APT="${R_DIR}/apt-r6c"
R6C_MARKER="${R_DIR}/marker-r6c"
mkdir -p "${R6C_MARKER}"
if run_full "${R_DIR}/os/ubuntu-release" "${R6C_APT}" "${R6C_MARKER}" "${R_DIR}/r6c"; then
    grep -qx 'deb \[arch=amd64 signed-by='"${R6C_APT}"'/etc/apt/keyrings/docker.gpg\] https://download.docker.com/linux/ubuntu jammy stable' \
        "${R6C_APT}/etc/apt/sources.list.d/docker.list" \
        || fail "R6: a served codename was not used as-is"
    if grep -q 'falling back' "${R_DIR}/r6c-out.log"; then
        fail "R6: a served codename triggered the fallback"
    fi
else
    fail "R6: a served codename was refused"
fi
pass "R6: a served codename is used as-is"
# An unserved codename with no served fallback refuses loudly and writes no
# repo line at all (never a dead docker.list).
printf 'ID=ubuntu\nVERSION_CODENAME=zzznosuch\n' >"${R_DIR}/os/ubuntu-unknown-release"
R6D_APT="${R_DIR}/apt-r6d"
R6D_MARKER="${R_DIR}/marker-r6d"
mkdir -p "${R6D_MARKER}"
DOCKER_SERVED_SUITES=""
export DOCKER_SERVED_SUITES
if run_full "${R_DIR}/os/ubuntu-unknown-release" "${R6D_APT}" "${R6D_MARKER}" "${R_DIR}/r6d"; then
    fail "R6: an unserved codename with no fallback was accepted"
else
    grep -q "does not serve 'zzznosuch'" "${R_DIR}/r6d-out.log" \
        || fail "R6: the refusal does not name the unserved codename"
    grep -q 'manually' "${R_DIR}/r6d-out.log" \
        || fail "R6: the refusal does not name the manual step"
fi
[ ! -e "${R6D_APT}/etc/apt/sources.list.d/docker.list" ] \
    || fail "R6: an unserved codename still wrote a docker.list"
[ ! -e "${R6D_APT}/etc/apt/keyrings/docker.gpg" ] \
    || fail "R6: the key was downloaded although no suite is served"
DOCKER_SERVED_SUITES="jammy trixie"
export DOCKER_SERVED_SUITES
pass "R6: an unserved codename with no fallback refuses without writing a repo"

# --- L1: a dead network refuses with the real error, never falls back -------
echo "==> L1 network failure refuses instead of falling back"
L1_APT="${R_DIR}/apt-l1"
L1_MARKER="${R_DIR}/marker-l1"
mkdir -p "${L1_MARKER}"
DOCKER_CURL_FAIL=1
if run_full "${R_DIR}/os/debian-sid-release" "${L1_APT}" "${L1_MARKER}" "${R_DIR}/l1"; then
    DOCKER_CURL_FAIL=0
    fail "L1: a network failure fell back to another suite (or succeeded)"
else
    DOCKER_CURL_FAIL=0
    grep -q 'Could not resolve host' "${R_DIR}/l1-out.log" \
        || fail "L1: the real curl error is hidden (it must reach stderr)"
    grep -q 'could not reach the Docker apt repository' "${R_DIR}/l1-out.log" \
        || fail "L1: the refusal does not say the repository could not be reached"
    if grep -q "; falling back to '" "${R_DIR}/l1-out.log"; then
        fail "L1: a network failure fell back to an unverified suite"
    fi
    grep -q 'manually' "${R_DIR}/l1-out.log" \
        || fail "L1: the refusal does not name the manual step"
fi
[ ! -e "${L1_APT}/etc/apt/sources.list.d/docker.list" ] \
    || fail "L1: a network failure still wrote a docker.list"
[ ! -e "${L1_APT}/etc/apt/keyrings/docker.gpg" ] \
    || fail "L1: the key was downloaded although the repository is unreachable"
# A served codename with a dead network refuses the same way (no silent pass).
L1B_APT="${R_DIR}/apt-l1b"
L1B_MARKER="${R_DIR}/marker-l1b"
mkdir -p "${L1B_MARKER}"
DOCKER_CURL_FAIL=1
if run_full "${R_DIR}/os/ubuntu-release" "${L1B_APT}" "${L1B_MARKER}" "${R_DIR}/l1b"; then
    DOCKER_CURL_FAIL=0
    fail "L1: a served codename was accepted although the network is dead"
else
    DOCKER_CURL_FAIL=0
    grep -q 'could not reach the Docker apt repository' "${R_DIR}/l1b-out.log" \
        || fail "L1: a served codename with a dead network refused for an unexpected reason"
    if grep -q "; falling back to '" "${R_DIR}/l1b-out.log"; then
        fail "L1: a dead network fell back even for a served codename"
    fi
fi
[ ! -e "${L1B_APT}/etc/apt/sources.list.d/docker.list" ] \
    || fail "L1: a dead network wrote a docker.list for a served codename"
pass "L1: network failure refuses with the real error and never falls back"

# --- R7: curl hardening + trap-safe temp cleanup ------------------------------
echo "==> R7 curl hardening and trap-safe cleanup"
grep -q -- '--proto' "${R_DIR}/r3-curl.log" \
    || fail "R7: the key download is not protocol-pinned (--proto)"
grep -q 'tlsv1.2' "${R_DIR}/r3-curl.log" \
    || fail "R7: the key download does not require TLS 1.2+"
grep -q -- '--retry' "${R_DIR}/r3-curl.log" \
    || fail "R7: the key download is not retried"
grep -q -- '--silent' "${R_DIR}/r3-curl.log" \
    || fail "R7: the key download is not silent (secrets-safe logging)"
grep -q -- '--connect-timeout' "${R_DIR}/r3-curl.log" \
    || fail "R7: the probe has no connect timeout"
grep -q -- '--max-time' "${R_DIR}/r3-curl.log" \
    || fail "R7: the probe has no total timeout"
if awk '/^ensure_docker_full\(\)/,/^}/' "${SCRIPT_DIR}/install-agent-lib.sh" | grep -qE '^[[:space:]]*trap[[:space:]]'; then
    fail "R7: ensure_docker_full installs a trap (it would clobber the caller's EXIT trap)"
fi
if ls "${TMPDIR:-/tmp}"/docker-key.* >/dev/null 2>&1; then
    fail "R7: a docker-key temp file survived (success and failure paths must remove it)"
fi
pass "R7: hardened curl flags, no EXIT-trap clobbering, no temp file left"

# --- H1/M1: the lib seams are gated behind the hard test mode ---------------
echo "==> H1/M1 lib seams ignored outside the hard test mode"
# The gate itself, all six combinations (hermetic: no host file is read).
GATE_CASE=0
check_gate() {
    GATE_CASE=$((GATE_CASE + 1))
    # $1 want (0/1), $2 shell snippet setting the subshell environment
    if (
        unset GOTHAM_INSTALL_ROOT GOTHAM_INSTALL_TEST DRY_RUN
        eval "$2"
        _gotham_seams_allowed
    ); then
        _got=1
    else
        _got=0
    fi
    [ "${_got}" = "$1" ] \
        || fail "H1/M1 gate case ${GATE_CASE}: got ${_got}, want $1 ($2)"
}
check_gate 0 true
check_gate 0 'GOTHAM_INSTALL_TEST=1; export GOTHAM_INSTALL_TEST'
check_gate 1 'GOTHAM_INSTALL_TEST=1; DRY_RUN=1; export GOTHAM_INSTALL_TEST DRY_RUN'
check_gate 1 'GOTHAM_INSTALL_ROOT=/tmp/sb-test-root; export GOTHAM_INSTALL_ROOT'
check_gate 0 'GOTHAM_INSTALL_ROOT=/; export GOTHAM_INSTALL_ROOT'
check_gate 0 'GOTHAM_INSTALL_ROOT=/; GOTHAM_INSTALL_TEST=1; export GOTHAM_INSTALL_ROOT GOTHAM_INSTALL_TEST'
pass "H1/M1 _gotham_seams_allowed honors sandbox / flag+dry-run only"
# No ungated seam read may remain: the old one-line default expansions are
# gone (both seams now resolve inside a _gotham_seams_allowed branch).
if grep -q '_os_release="${GOTHAM_OS_RELEASE_FILE:-/etc/os-release}"' "${SCRIPT_DIR}/install-agent-lib.sh"; then
    fail "H1: install-agent-lib.sh still reads GOTHAM_OS_RELEASE_FILE ungated"
fi
if grep -q '^    _apt_root="${GOTHAM_APT_ROOT:-}"' "${SCRIPT_DIR}/install-agent-lib.sh"; then
    fail "H1: install-agent-lib.sh still reads GOTHAM_APT_ROOT ungated"
fi
grep -q '_gotham_seams_allowed' "${SCRIPT_DIR}/install-agent-lib.sh" \
    || fail "H1: install-agent-lib.sh has no seam gate at all"
pass "H1: both lib seams are gated (no ungated default expansion)"
# Differential: with docker present (skip path, no side effects anywhere) a
# production-mode run with hostile seams behaves exactly like the clean run.
SKIP_SHIM="${SCRATCH}/skip-shim"
mkdir -p "${SKIP_SHIM}"
cat >"${SKIP_SHIM}/docker" <<'SHIM'
#!/bin/sh
# Always works (engine + compose present), so the skip path is taken.
exit 0
SHIM
chmod +x "${SKIP_SHIM}/docker"
(
    unset GOTHAM_INSTALL_ROOT GOTHAM_INSTALL_TEST DRY_RUN
    unset GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
    PATH="${SKIP_SHIM}:${PATH}" ensure_docker_full >"${SCRATCH}/skip-clean.log" 2>&1
    echo $? >"${SCRATCH}/skip-clean.rc"
)
(
    unset GOTHAM_INSTALL_ROOT GOTHAM_INSTALL_TEST DRY_RUN
    GOTHAM_OS_RELEASE_FILE=/nonexistent-hostile-os-release
    GOTHAM_APT_ROOT=/nonexistent-hostile-apt-root
    export GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
    PATH="${SKIP_SHIM}:${PATH}" ensure_docker_full >"${SCRATCH}/skip-hostile.log" 2>&1
    echo $? >"${SCRATCH}/skip-hostile.rc"
)
[ "$(cat "${SCRATCH}/skip-clean.rc")" = "$(cat "${SCRATCH}/skip-hostile.rc")" ] \
    || fail "H1: hostile seams changed the exit status outside test mode"
cmp -s "${SCRATCH}/skip-clean.log" "${SCRATCH}/skip-hostile.log" \
    || fail "H1: hostile seams changed the output outside test mode"
grep -q 'already installed; skipping' "${SCRATCH}/skip-hostile.log" \
    || fail "H1: the differential skip run did not take the skip path"
pass "H1: hostile seams change nothing outside test mode (differential)"
# Read-path negative where the real host refuses on its own (no Ubuntu/Debian
# os-release): the hostile ubuntu fixture must still be ignored in production
# mode, while the sandbox honors it. On Ubuntu/Debian hosts this is SKIP-noted
# (a production run there would proceed into real apt paths); the live
# hostile-env install in the container proof covers it instead.
if [ ! -f /etc/os-release ] || ! grep -qE '^ID=(ubuntu|debian)' /etc/os-release; then
    NEG_APT="${SCRATCH}/neg-apt"
    NEG_MARKER="${SCRATCH}/neg-marker"
    mkdir -p "${NEG_MARKER}" "${NEG_APT}/etc/apt/sources.list.d"
    printf 'ID=ubuntu\nVERSION_CODENAME=jammy\n' >"${SCRATCH}/neg-ubuntu-release"
    set_docker_variant absent
    (
        unset GOTHAM_INSTALL_ROOT GOTHAM_INSTALL_TEST DRY_RUN
        GOTHAM_OS_RELEASE_FILE="${SCRATCH}/neg-ubuntu-release"
        GOTHAM_APT_ROOT="${NEG_APT}"
        DOCKER_MARKER="${NEG_MARKER}"
        APT_LOG="${SCRATCH}/neg-apt.log"
        CURL_LOG="${SCRATCH}/neg-curl.log"
        export GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT DOCKER_MARKER APT_LOG CURL_LOG
        PATH="${R_PATH}" ensure_docker_full >"${SCRATCH}/neg-out.log" 2>&1
    ) && NEG_RC=0 || NEG_RC=$?
    [ "${NEG_RC}" -ne 0 ] \
        || fail "H1: the hostile ubuntu fixture was honored outside test mode"
    grep -q 'supports Ubuntu/Debian only' "${SCRATCH}/neg-out.log" \
        || fail "H1: production refusal did not use the real host os-release"
    if grep -q "found 'ubuntu'" "${SCRATCH}/neg-out.log"; then
        fail "H1: production run read the hostile fixture instead of the real os-release"
    fi
    [ ! -e "${NEG_APT}/etc/apt/sources.list.d/docker.list" ] \
        || fail "H1: a production run wrote an apt source from a hostile seam"
    pass "H1: hostile fixture ignored outside test mode, real host file used"
else
    echo "SKIP: hostile-fixture read-path needs a non-Ubuntu/Debian host (container live proof covers it)"
fi

if [ "${FAILURES}" -eq 0 ]; then
    echo "ALL AGENT-INSTALL TESTS PASSED"
    exit 0
fi
echo "test-agent-install: ${FAILURES} check(s) failed" >&2
exit 1
