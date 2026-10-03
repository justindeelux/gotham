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
#   H1    the production paths (_OS_RELEASE_FILE / _APT_ROOT) are fixed:
#         exporting seam-like variables (old GOTHAM_* names or the internal
#         names) never redirects them; the suite overrides the paths by plain
#         assignment after sourcing the lib. The read-path proof is
#         fixture-driven (host-distro independent): hostile exports lose to
#         the internal variables on both the Ubuntu and the refusal path.
#   C1    every value written to agent.env is rejected on control
#         characters (any byte < 0x20 or 0x7f, including newline); the dial
#         address additionally refuses spaces, quotes and = and must be
#         host:port, and the remaining keys get a cheap shape check matching
#         what the agent accepts (log level, boolean, Go duration, channel
#         name, listen host:port). Printable metacharacters in the node id
#         still pass through verbatim.
#   V1    install-agent.sh validates every agent.env value before its first
#         mutation (user, binary, CA): a direct run with a bad value fails
#         with nothing created.
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

# Stray seam-like variables from the caller's environment must not influence
# the suite: the lib assigns its path variables unconditionally on sourcing,
# and every case below assigns them explicitly by plain shell assignment
# (never exported, never read from the environment).
unset GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
_OS_RELEASE_FILE=/etc/os-release
_APT_ROOT=

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
if _OS_RELEASE_FILE=/nonexistent PATH="${FULL_PATH}" ensure_docker_full >"${F2_DIR}/skip.log" 2>&1; then
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
if _OS_RELEASE_FILE="${F2_DIR}/os/arch-release" _APT_ROOT="${F2_DIR}/apt" \
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
_OS_RELEASE_FILE="${F2_DIR}/os/os-release"
_APT_ROOT="${F2_DIR}/apt"
if PATH="${F2_MINPATH}" ensure_docker_full >"${F2_DIR}/flow.log" 2>&1; then
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
_OS_RELEASE_FILE="${F2_DIR}/os/os-release"
_APT_ROOT="${F2_DIR}/apt2"
if PATH="${FULL_PATH}" ensure_docker_full >"${F2_DIR}/fp.log" 2>&1; then
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
    # The marker/log paths are exported so the shim subprocesses see them;
    # the lib paths are plain shell variables assigned after sourcing (never
    # exported, never read from the environment).
    DOCKER_MARKER="$3"
    APT_LOG="$4-apt.log"
    CURL_LOG="$4-curl.log"
    export DOCKER_MARKER APT_LOG CURL_LOG
    _OS_RELEASE_FILE="$1"
    _APT_ROOT="$2"
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
_OS_RELEASE_FILE="${R_DIR}/os/ubuntu-release"
_APT_ROOT="${R6_APT}"
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

# --- H1: the production paths are fixed, never read from the environment ----
echo "==> H1 production paths ignore the environment"
# Re-sourcing with hostile seam variables exported still yields the fixed
# defaults: the assignments are unconditional, so an inherited environment
# cannot survive them (this is exactly what a fresh `sh install-agent.sh` does).
(
    GOTHAM_OS_RELEASE_FILE=/hostile-os-release
    GOTHAM_APT_ROOT=/hostile-apt-root
    _OS_RELEASE_FILE=/hostile-internal-os-release
    _APT_ROOT=/hostile-internal-apt-root
    export GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT _OS_RELEASE_FILE _APT_ROOT
    # shellcheck source=deploy/install-agent-lib.sh
    . "${SCRIPT_DIR}/install-agent-lib.sh"
    [ "${_OS_RELEASE_FILE}" = "/etc/os-release" ] || exit 1
    [ "${_APT_ROOT}" = "" ] || exit 1
) || fail "H1: re-sourcing with hostile seam variables did not yield the fixed defaults"
pass "H1: sourcing overwrites hostile seam variables with the fixed defaults"
# Differential on the skip path (no side effects anywhere): hostile old-name
# exports behave exactly like the clean run.
SKIP_SHIM="${SCRATCH}/skip-shim"
mkdir -p "${SKIP_SHIM}"
cat >"${SKIP_SHIM}/docker" <<'SHIM'
#!/bin/sh
# Always works (engine + compose present), so the skip path is taken.
exit 0
SHIM
chmod +x "${SKIP_SHIM}/docker"
(
    unset GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
    PATH="${SKIP_SHIM}:${PATH}" ensure_docker_full >"${SCRATCH}/skip-clean.log" 2>&1
    echo $? >"${SCRATCH}/skip-clean.rc"
)
(
    GOTHAM_OS_RELEASE_FILE=/nonexistent-hostile-os-release
    GOTHAM_APT_ROOT=/nonexistent-hostile-apt-root
    export GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
    PATH="${SKIP_SHIM}:${PATH}" ensure_docker_full >"${SCRATCH}/skip-hostile.log" 2>&1
    echo $? >"${SCRATCH}/skip-hostile.rc"
)
[ "$(cat "${SCRATCH}/skip-clean.rc")" = "$(cat "${SCRATCH}/skip-hostile.rc")" ] \
    || fail "H1: hostile exports changed the exit status"
cmp -s "${SCRATCH}/skip-clean.log" "${SCRATCH}/skip-hostile.log" \
    || fail "H1: hostile exports changed the output"
grep -q 'already installed; skipping' "${SCRATCH}/skip-hostile.log" \
    || fail "H1: the differential skip run did not take the skip path"
pass "H1: hostile old-name exports change nothing (differential)"
# Read-path: hostile old-name exports are ignored, on any host. The os-release
# and apt paths come from the internal variables (plain assignment after
# sourcing, the only seam the tests get), so no host file is read: with the
# internal ubuntu fixture the install must succeed into the sandbox apt root
# (a re-opened environment seam would read the hostile arch fixture and
# refuse), and with the internal arch fixture it must refuse (a re-opened
# seam would proceed from the hostile ubuntu fixture). All writes stay under
# SCRATCH.
H_APT="${SCRATCH}/hostile-apt"
H_OTHER="${SCRATCH}/hostile-apt-other"
H_MARKER="${SCRATCH}/hostile-marker"
mkdir -p "${H_APT}" "${H_OTHER}" "${H_MARKER}"
printf 'ID=arch\nVERSION_CODENAME=n/a\n' >"${SCRATCH}/hostile-arch-release"
printf 'ID=ubuntu\nVERSION_CODENAME=jammy\n' >"${SCRATCH}/hostile-ubuntu-release"
set_docker_variant absent
(
    GOTHAM_OS_RELEASE_FILE="${SCRATCH}/hostile-arch-release"
    GOTHAM_APT_ROOT="${H_OTHER}"
    export GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
    run_full "${R_DIR}/os/ubuntu-release" "${H_APT}" "${H_MARKER}" "${SCRATCH}/hostile"
) || fail "H1: the hostile arch fixture was honored instead of the internal ubuntu one"
grep -qx 'deb \[arch=amd64 signed-by='"${H_APT}"'/etc/apt/keyrings/docker.gpg\] https://download.docker.com/linux/ubuntu jammy stable' \
    "${H_APT}/etc/apt/sources.list.d/docker.list" \
    || fail "H1: hostile exports redirected the install away from the internal paths"
if [ -e "${H_OTHER}/etc/apt/sources.list.d/docker.list" ]; then
    fail "H1: a repo was written under the hostile apt root"
fi
unset GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT
pass "H1: hostile exports ignored on the install path (internal ubuntu wins)"
(
    GOTHAM_OS_RELEASE_FILE="${SCRATCH}/hostile-ubuntu-release"
    GOTHAM_APT_ROOT="${H_OTHER}"
    DOCKER_MARKER="${SCRATCH}/hostile-marker-refuse"
    APT_LOG="${SCRATCH}/hostile-apt.log"
    CURL_LOG="${SCRATCH}/hostile-curl.log"
    export GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT DOCKER_MARKER APT_LOG CURL_LOG
    mkdir -p "${DOCKER_MARKER}"
    _OS_RELEASE_FILE="${SCRATCH}/hostile-arch-release"
    _APT_ROOT="${H_APT}"
    PATH="${R_PATH}" ensure_docker_full >"${SCRATCH}/hostile-out.log" 2>&1
) && H_RC=0 || H_RC=$?
[ "${H_RC}" -ne 0 ] \
    || fail "H1: the hostile ubuntu fixture was honored instead of the internal arch one"
grep -q 'supports Ubuntu/Debian only' "${SCRATCH}/hostile-out.log" \
    || fail "H1: refusal did not use the internal arch os-release"
if grep -q "found 'ubuntu'" "${SCRATCH}/hostile-out.log"; then
    fail "H1: production run read the hostile fixture instead of the internal paths"
fi
[ ! -e "${H_OTHER}/etc/apt/sources.list.d/docker.list" ] \
    || fail "H1: a repo was written under the hostile apt root"
pass "H1: hostile exports ignored on the refusal path (internal arch wins)"
# Fresh-process proof through install-agent.sh itself: exporting the old and
# the new names before --full --dry-run changes nothing, because sourcing the
# lib overwrites them before any path is used.
printf 'dummy CA for the hostile dry-run path\n' >"${SCRATCH}/hostile-ca.pem"
agent_full_dry() {
    GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
        sh "${AGENT_INSTALLER}" --full --ca "${SCRATCH}/hostile-ca.pem" --dry-run
}
agent_full_dry >"${SCRATCH}/if-clean.log" 2>&1
GOTHAM_OS_RELEASE_FILE=/nonexistent-hostile \
GOTHAM_APT_ROOT=/nonexistent-hostile \
_OS_RELEASE_FILE=/nonexistent-hostile \
_APT_ROOT=/nonexistent-hostile \
    agent_full_dry >"${SCRATCH}/if-hostile.log" 2>&1
unset GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT _OS_RELEASE_FILE _APT_ROOT
_OS_RELEASE_FILE=/etc/os-release
_APT_ROOT=
sed 's/gotham-agent-install\.[A-Za-z0-9][A-Za-z0-9]*/gotham-agent-install.RANDOM/g' \
    "${SCRATCH}/if-clean.log" >"${SCRATCH}/if-clean.norm"
sed 's/gotham-agent-install\.[A-Za-z0-9][A-Za-z0-9]*/gotham-agent-install.RANDOM/g' \
    "${SCRATCH}/if-hostile.log" >"${SCRATCH}/if-hostile.norm"
cmp -s "${SCRATCH}/if-clean.norm" "${SCRATCH}/if-hostile.norm" \
    || fail "H1: hostile seam exports changed install-agent.sh --full --dry-run"
grep -q 'ensure Docker Engine and the compose plugin (--full)' "${SCRATCH}/if-hostile.log" \
    || fail "H1: install-agent.sh --full --dry-run lost its Docker step"
pass "H1: install-agent.sh --full --dry-run ignores hostile old- and new-name exports"

# --- C1: control characters in the node id / dial address are rejected -----
echo "==> C1 control characters in GOTHAM_AGENT_NODE_ID / GOTHAM_AGENT_CP_ADDR are rejected"
CC_DIR="${SCRATCH}/ctrl"
mkdir -p "${CC_DIR}"
# A newline smuggling a second line (the classic agent.env injection) fails
# closed with a clear error and writes nothing.
CC_ENV="${CC_DIR}/agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\n' >"${CC_ENV}"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_NODE_ID="$(printf 'a\nGOTHAM_AGENT_INSECURE=true')"
if agent_env_write "${CC_ENV}" "/etc/gotham/ca.crt" 0 2>"${CC_DIR}/node-err.log"; then
    fail "a newline in GOTHAM_AGENT_NODE_ID was written to agent.env"
else
    grep -q 'control character' "${CC_DIR}/node-err.log" \
        || fail "the newline rejection names no control character"
    if grep -q '^GOTHAM_AGENT_INSECURE=' "${CC_ENV}"; then
        fail "the smuggled GOTHAM_AGENT_INSECURE line reached agent.env"
    fi
fi
unset_agent_env
# Same for the dial address (DEL byte, generated so no raw 0x7f sits in source).
CC_ENV2="${CC_DIR}/cp.env"
printf 'GOTHAM_AGENT_NODE_ID=node-ctrl\n' >"${CC_ENV2}"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_CP_ADDR="$(printf 'cp.example.com:9443\177')"
if agent_env_write "${CC_ENV2}" "/etc/gotham/ca.crt" 0 2>"${CC_DIR}/cp-err.log"; then
    fail "a DEL byte in GOTHAM_AGENT_CP_ADDR was written to agent.env"
else
    grep -q 'control character' "${CC_DIR}/cp-err.log" \
        || fail "the DEL rejection names no control character"
fi
unset_agent_env
# A poisoned prior file is rejected the same way: the check runs on the value
# actually written, not just the ambient environment.
CC_ENV3="${CC_DIR}/prior.env"
printf 'GOTHAM_AGENT_NODE_ID=a\177b\n' >"${CC_ENV3}"
if agent_env_write "${CC_ENV3}" "/etc/gotham/ca.crt" 0 2>/dev/null; then
    fail "a DEL byte from a prior agent.env was kept"
fi
# Printable metacharacters the agent itself accepts (quotes, ;, $(), =)
# still pass through verbatim (no over-rejection and no execution): the
# quoting class must survive this gate. Shapes the agent refuses (spaces,
# *, /, backslashes) are rejected below instead of passed through.
CC_ENV4="${CC_DIR}/meta.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\n' >"${CC_ENV4}"
# The execution probe stays slash- and space-free (a node id can hold
# neither anymore): if the value were ever expanded, ./c1-pwned-jus12 would
# appear in the suite's working directory.
rm -f ./c1-pwned-jus12
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_NODE_ID="o'dd;\$(id>c1-pwned-jus12);a=b"
agent_env_write "${CC_ENV4}" "/etc/gotham/ca.crt" 0
unset_agent_env
grep -qxF "GOTHAM_AGENT_NODE_ID=o'dd;\$(id>c1-pwned-jus12);a=b" "${CC_ENV4}" \
    || fail "agent-valid metacharacters in the node id were rejected or mangled"
if [ -e ./c1-pwned-jus12 ]; then
    rm -f ./c1-pwned-jus12
    fail "the metachar node id executed during the write"
fi
# Every other managed key gets the same control-character gate: a newline
# smuggling a second line fails closed, names the key, and writes nothing.
# shellcheck disable=SC2034  # CC_BAD is consumed through eval below
CC_BAD="$(printf 'ok\nGOTHAM_AGENT_INSECURE=true')"
for _cckey in GOTHAM_AGENT_CERT_DIR GOTHAM_AGENT_KEY GOTHAM_AGENT_DOCKER_SOCK \
    GOTHAM_AGENT_LOG_LEVEL GOTHAM_AGENT_AUTO_UPDATE \
    GOTHAM_AGENT_UPDATE_INTERVAL GOTHAM_AGENT_UPDATE_CHANNEL \
    GOTHAM_AGENT_LISTEN_ADDR; do
    CC_KENV="${CC_DIR}/k-${_cckey}.env"
    printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-ok\n' >"${CC_KENV}"
    # shellcheck disable=SC2034  # read by agent_env_write through eval
    eval "${_cckey}=\"\${CC_BAD}\""
    if agent_env_write "${CC_KENV}" "/etc/gotham/ca.crt" 0 2>"${CC_DIR}/k-err.log"; then
        fail "a newline in ${_cckey} was written to agent.env"
    else
        grep -q "${_cckey}.*control character" "${CC_DIR}/k-err.log" \
            || fail "the ${_cckey} rejection does not name the key and the cause"
        if grep -q '^GOTHAM_AGENT_INSECURE=' "${CC_KENV}"; then
            fail "the smuggled line via ${_cckey} reached agent.env"
        fi
    fi
    unset_agent_env
done
# The dial address additionally refuses spaces, quotes and = (systemd would
# parse them differently from what the operator typed) and must be host:port.
for _cpbad in 'cp.example.com: 9443' "cp.exa'mple.com:9443" 'cp.exa"mple.com:9443' \
    'cp.exa=mple.com:9443' 'cp.example.com'; do
    CC_CPBAD="${CC_DIR}/cp-bad.env"
    printf 'GOTHAM_AGENT_NODE_ID=node-ok\nGOTHAM_AGENT_CP_ADDR=%s\n' "${_cpbad}" >"${CC_CPBAD}"
    if agent_env_write "${CC_CPBAD}" "/etc/gotham/ca.crt" 0 2>/dev/null; then
        fail "GOTHAM_AGENT_CP_ADDR='${_cpbad}' was written to agent.env"
    fi
done
unset_agent_env
# Bracketed IPv6 and :port forms are host:port and still pass.
printf 'GOTHAM_AGENT_NODE_ID=node-ok\nGOTHAM_AGENT_CP_ADDR=[::1]:9443\n' >"${CC_DIR}/cp-v6.env"
agent_env_write "${CC_DIR}/cp-v6.env" "/etc/gotham/ca.crt" 0 \
    || fail "a bracketed IPv6 dial address was rejected"
grep -qxF 'GOTHAM_AGENT_CP_ADDR=[::1]:9443' "${CC_DIR}/cp-v6.env" \
    || fail "a bracketed IPv6 dial address was mangled"
# Shape checks accept everything the agent accepts: log level (any case),
# true/false booleans (any case, trimmed), Go durations, channel names,
# :port listeners, absolute paths and unix/tcp/docker socket forms.
CC_SHAPE="${CC_DIR}/shape.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-shape\n' >"${CC_SHAPE}"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_LOG_LEVEL=Warning
# shellcheck disable=SC2034
GOTHAM_AGENT_AUTO_UPDATE=true
# shellcheck disable=SC2034
GOTHAM_AGENT_UPDATE_INTERVAL=1h30m
# shellcheck disable=SC2034
GOTHAM_AGENT_UPDATE_CHANNEL=beta
# shellcheck disable=SC2034
GOTHAM_AGENT_LISTEN_ADDR=:9443
# shellcheck disable=SC2034
GOTHAM_AGENT_CERT_DIR="/opt/gotham-agent/certs"
# shellcheck disable=SC2034
GOTHAM_AGENT_KEY=/etc/gotham/agent.key
# shellcheck disable=SC2034
GOTHAM_AGENT_DOCKER_SOCK=tcp://docker:2375
agent_env_write "${CC_SHAPE}" "/etc/gotham/ca.crt" 0 \
    || fail "valid shaped values were rejected"
for _shapeline in 'GOTHAM_AGENT_LOG_LEVEL=Warning' 'GOTHAM_AGENT_AUTO_UPDATE=true' \
    'GOTHAM_AGENT_UPDATE_INTERVAL=1h30m' 'GOTHAM_AGENT_UPDATE_CHANNEL=beta' \
    'GOTHAM_AGENT_LISTEN_ADDR=:9443' 'GOTHAM_AGENT_CERT_DIR=/opt/gotham-agent/certs' \
    'GOTHAM_AGENT_KEY=/etc/gotham/agent.key' 'GOTHAM_AGENT_DOCKER_SOCK=tcp://docker:2375'; do
    grep -qxF "${_shapeline}" "${CC_SHAPE}" \
        || fail "valid shaped value not written verbatim: ${_shapeline}"
done
unset_agent_env
# ... and reject malformed ones (unknown level, non-true/false booleans —
# 1/yes/on silently mean off to the agent — unit-less or unit-broken
# durations, channel names with spaces or slashes, node ids the agent itself
# refuses, relative or whitespace/quoted paths, scheme-broken docker
# endpoints, a port-less listener), while agent-valid spellings keep passing
# verbatim (booleans any case/trimmed, Go-duration forms, quoted node ids,
# absolute paths, socket forms). Each entry is key|value|want.
for _spec in 'GOTHAM_AGENT_LOG_LEVEL|verbose|reject' 'GOTHAM_AGENT_AUTO_UPDATE|maybe|reject' \
    'GOTHAM_AGENT_AUTO_UPDATE|1|reject' 'GOTHAM_AGENT_AUTO_UPDATE|yes|reject' \
    'GOTHAM_AGENT_AUTO_UPDATE|on|reject' 'GOTHAM_AGENT_AUTO_UPDATE|0|reject' \
    'GOTHAM_AGENT_AUTO_UPDATE|no|reject' 'GOTHAM_AGENT_AUTO_UPDATE|off|reject' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|5|reject' 'GOTHAM_AGENT_UPDATE_INTERVAL|5x|reject' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|5M|reject' 'GOTHAM_AGENT_UPDATE_INTERVAL|5 m|reject' \
    'GOTHAM_AGENT_UPDATE_CHANNEL|a b|reject' \
    'GOTHAM_AGENT_UPDATE_CHANNEL|a/b|reject' 'GOTHAM_AGENT_LISTEN_ADDR|9443|reject' \
    'GOTHAM_AGENT_NODE_ID|a b|reject' 'GOTHAM_AGENT_NODE_ID|a*b|reject' \
    'GOTHAM_AGENT_NODE_ID|a/b|reject' 'GOTHAM_AGENT_CP_ADDR|cp.example.com|reject' \
    'GOTHAM_AGENT_CERT_DIR|relative/certs|reject' \
    'GOTHAM_AGENT_CERT_DIR|/opt/my dir/certs|reject' \
    'GOTHAM_AGENT_KEY|relative/key.pem|reject' \
    'GOTHAM_AGENT_KEY|/etc/a b.key|reject' \
    'GOTHAM_AGENT_DOCKER_SOCK|relative.sock|reject' \
    'GOTHAM_AGENT_DOCKER_SOCK|unix://relative.sock|reject' \
    'GOTHAM_AGENT_DOCKER_SOCK|tcp://|reject' \
    'GOTHAM_AGENT_DOCKER_SOCK|tcp://docker|reject' \
    'GOTHAM_AGENT_DOCKER_SOCK|http://docker:2375/x|reject' \
    'GOTHAM_AGENT_DOCKER_SOCK|/sock dir/docker.sock|reject' \
    "GOTHAM_AGENT_NODE_ID|a'b|pass" \
    'GOTHAM_AGENT_NODE_ID|a=b|pass' \
    'GOTHAM_AGENT_NODE_ID|host-with-dashes|pass' \
    'GOTHAM_AGENT_NODE_ID|a\b|reject' \
    'GOTHAM_AGENT_CERT_DIR|/opt/a\b|reject' \
    'GOTHAM_AGENT_AUTO_UPDATE|FALSE|pass' \
    'GOTHAM_AGENT_AUTO_UPDATE| True |pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|+5m|pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|.5s|pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|5.s|pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|0|pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|500ms|pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|1m30s|pass' \
    'GOTHAM_AGENT_UPDATE_INTERVAL|-5m|pass' \
    'GOTHAM_AGENT_DOCKER_SOCK|unix:///run/docker.sock|pass' \
    'GOTHAM_AGENT_DOCKER_SOCK|/var/run/docker.sock|pass' \
    'GOTHAM_AGENT_DOCKER_SOCK|tcp://127.0.0.1:2375|pass' \
    'GOTHAM_AGENT_LISTEN_ADDR|[::1]:9443|pass'; do
    _skey="${_spec%%|*}"
    _srest="${_spec#*|}"
    _sval="${_srest%|*}"
    _swant="${_srest#*|}"
    CC_SENV="${CC_DIR}/shape-bad.env"
    printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-ok\n' >"${CC_SENV}"
    # shellcheck disable=SC2034  # read by agent_env_write through eval
    eval "${_skey}=\"\${_sval}\""
    if [ "${_swant}" = "reject" ]; then
        if agent_env_write "${CC_SENV}" "/etc/gotham/ca.crt" 0 2>/dev/null; then
            fail "${_skey}='${_sval}' was written to agent.env"
        fi
    else
        agent_env_write "${CC_SENV}" "/etc/gotham/ca.crt" 0 \
            || fail "${_skey}='${_sval}' was rejected although the agent accepts it"
        grep -qxF "${_skey}=${_sval}" "${CC_SENV}" \
            || fail "${_skey}='${_sval}' was not written verbatim"
    fi
    unset_agent_env
done
# Shapes a double-quoted eval cannot carry (a trailing backslash would escape
# the closing quote, a double quote would end the value early, tabs and
# overlong ids need command substitution) are assigned directly; each
# assignment stands alone on its line so the SC2034 suppression above it
# applies. agent_env_write reads the same variables, so the write path is
# still what is proven.
_cc_direct_reject() { # $1 key: the value in _cc_val must be refused
    CC_SENV="${CC_DIR}/shape-bad.env"
    printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-ok\n' >"${CC_SENV}"
    if agent_env_write "${CC_SENV}" "/etc/gotham/ca.crt" 0 2>"${CC_DIR}/k-shape-err.log"; then
        fail "$1='${_cc_val}' was written to agent.env"
    else
        grep -q "$1" "${CC_DIR}/k-shape-err.log" \
            || fail "the $1 rejection does not name the key"
    fi
    unset_agent_env
}
_cc_val='a"b'
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_NODE_ID="${_cc_val}"
CC_SENV="${CC_DIR}/shape-ok.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-ok\n' >"${CC_SENV}"
agent_env_write "${CC_SENV}" "/etc/gotham/ca.crt" 0 \
    || fail "GOTHAM_AGENT_NODE_ID='a\"b' was rejected although the agent accepts it"
grep -qxF 'GOTHAM_AGENT_NODE_ID=a"b' "${CC_SENV}" \
    || fail "GOTHAM_AGENT_NODE_ID='a\"b' was not written verbatim"
unset_agent_env
_cc_val="$(printf 'a\tb')"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_NODE_ID="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_NODE_ID
_cc_val="abc\\"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_NODE_ID="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_NODE_ID
_cc_val="$(awk 'BEGIN { for (i = 0; i < 254; i++) printf "a" }')"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_NODE_ID="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_NODE_ID
_cc_val='/opt/a"b/certs'
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_CERT_DIR="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_CERT_DIR
_cc_val="/certs\\"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_CERT_DIR="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_CERT_DIR
_cc_val="cp.example.com:9443\\"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_CP_ADDR="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_CP_ADDR
_cc_val="127.0.0.1:9443\\"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_LISTEN_ADDR="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_LISTEN_ADDR
_cc_val="/run/docker.sock\\"
# shellcheck disable=SC2034  # read by agent_env_write through eval
GOTHAM_AGENT_DOCKER_SOCK="${_cc_val}"
_cc_direct_reject GOTHAM_AGENT_DOCKER_SOCK
pass "C1: every agent.env value rejected on control characters, dial-address charset and shapes enforced"

# --- V2: agent_env_validate sees the previous file and the CA ---------------
# The pre-write validator must reject a poisoned prior agent.env (values this
# run leaves unset but would preserve) and a poisoned installer-resolved CA
# path: without those reads a neutered validator still lets the write-time
# check fail later, i.e. after the install already mutated.
echo "==> V2 the pre-write validator rejects poisoned prior values and CA paths"
V2_DIR="${SCRATCH}/v2"
mkdir -p "${V2_DIR}"
V2_ENV="${V2_DIR}/prior.env"
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=bad id\n' >"${V2_ENV}"
if agent_env_validate "${V2_ENV}" "/etc/gotham/ca.crt" 0 2>/dev/null; then
    fail "V2: a prior agent.env node id with a space passed validation"
fi
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-ok\n' >"${V2_ENV}"
if agent_env_validate "${V2_ENV}" "$(printf '/etc/gotham/ca.crt\nGOTHAM_AGENT_INSECURE=true')" 0 2>/dev/null; then
    fail "V2: a CA path with a smuggled newline passed validation"
fi
printf 'GOTHAM_AGENT_CP_ADDR=cp.example.com:9443\nGOTHAM_AGENT_NODE_ID=node-ok\n' >"${V2_ENV}"
if agent_env_validate "${V2_ENV}" "relative/ca.crt" 0 2>/dev/null; then
    fail "V2: a relative CA path passed validation"
fi
agent_env_validate "${V2_ENV}" "/etc/gotham/ca.crt" 0 \
    || fail "V2: a clean prior file with a valid CA failed validation"
if agent_env_validate "${V2_DIR}/no-such.env" "/etc/gotham/ca.crt" 0; then
    pass "V2: poisoned prior values and CA paths fail, clean input passes"
else
    fail "V2: a missing prior file with a valid CA failed validation"
fi

# --- V1: install-agent.sh validates before its first mutation ---------------
echo "==> V1 a direct run with a bad value fails with nothing created"
V1_DIR="${SCRATCH}/v1"
mkdir -p "${V1_DIR}/tmp"
printf 'dummy CA for the validate-first path\n' >"${V1_DIR}/ca.pem"
# A poisoned value fails the direct --dry-run before any mutation is even
# planned: the rejection names the key, no [dry-run] line is printed, and
# TMPDIR stays empty (validation precedes the first mktemp). One control-
# character case and two shape cases prove both gates run up front.
_v1_must_fail() { # $1 key=value assignment, $2 key name
    if env "$1" GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
        TMPDIR="${V1_DIR}/tmp" \
        sh "${AGENT_INSTALLER}" --ca "${V1_DIR}/ca.pem" --dry-run >"${V1_DIR}/bad.log" 2>&1; then
        fail "V1: a direct run with a poisoned $2 succeeded"
    else
        grep -q "$2" "${V1_DIR}/bad.log" \
            || fail "V1: the $2 rejection does not name the key"
        if grep -q '\[dry-run\]' "${V1_DIR}/bad.log"; then
            fail "V1: the failed run planned mutations before validating ($2)"
        fi
        [ -z "$(ls -A "${V1_DIR}/tmp")" ] \
            || fail "V1: the failed run created scratch files before validating ($2)"
    fi
}
_v1_must_fail "GOTHAM_AGENT_LOG_LEVEL=$(printf 'info\nGOTHAM_AGENT_INSECURE=true')" GOTHAM_AGENT_LOG_LEVEL
_v1_must_fail "GOTHAM_AGENT_NODE_ID=bad id" GOTHAM_AGENT_NODE_ID
_v1_must_fail "GOTHAM_AGENT_AUTO_UPDATE=yes" GOTHAM_AGENT_AUTO_UPDATE
# Valid input still passes validation (no behaviour change for good values).
GOTHAM_BASE_URL=http://127.0.0.1:9 GOTHAM_VERSION=v9.9.9-test \
    TMPDIR="${V1_DIR}/tmp" \
    sh "${AGENT_INSTALLER}" --ca "${V1_DIR}/ca.pem" --dry-run >"${V1_DIR}/good.log" 2>&1 \
    || fail "V1: valid input no longer passes validation"
pass "V1: bad values fail before the first mutation, good values pass"

if [ "${FAILURES}" -eq 0 ]; then
    echo "ALL AGENT-INSTALL TESTS PASSED"
    exit 0
fi
echo "test-agent-install: ${FAILURES} check(s) failed" >&2
exit 1
