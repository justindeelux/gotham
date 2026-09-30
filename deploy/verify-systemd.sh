#!/bin/sh
#
# verify-systemd.sh proves the BE-9.1 self-update deployment on a Linux host
# with systemd. It is safe beside a real Gotham service: it uses scratch units,
# a scratch service user and scratch paths under a temp directory, and removes
# everything on exit.
#
#   sudo deploy/verify-systemd.sh
#
# It proves:
#   1. the wrapper's status is written into a root-owned directory/file,
#   2. a failed start rolls back to the previous binary and exits nonzero,
#   3. the restart wrapper survives the service restart cgroup
#      (KillMode=process),
#   4. the staged gate, wrapper-failed rollback, resume and manifest tests (via
#      the Go toolchain),
#   5. the repo/installed unit wiring, the service-user binary ownership (M1)
#      and a planted update.lock symlink leaving its victim unchanged (N1),
#   6. the real launch chain: a non-root unit whose main process runs
#      `setsid sudo -n <wrapper>`, driven once healthy (result=ok) and once with
#      a broken binary (result=rolled_back, restored, pending cleared),
#      recording the wrapper's cgroup.
#
# Environment:
#   GOTHAM_GO=/path/to/go   Go binary for section 4 (default: go)
#
# It does NOT provision a real network release; the signed-manifest chain is
# covered by `go test ./internal/updates`.
set -u

if [ "$(id -u)" -ne 0 ]; then
    echo "verify-systemd.sh must run as root (try sudo)" >&2
    exit 1
fi
for tool in systemctl python3 mktemp flock; do
    if ! command -v "${tool}" >/dev/null 2>&1; then
        echo "verify-systemd.sh: ${tool} is required" >&2
        exit 1
    fi
done
GOTHAM_GO="${GOTHAM_GO:-go}"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)
SCRATCH=$(mktemp -d /tmp/gotham-verify.XXXXXX)
chmod 0755 "${SCRATCH}"
PORT=$(( 20000 + ($$ % 10000) ))
CHAIN_PORT=$(( PORT + 1 ))
UNIT="gotham-verify"
UNIT_FILE="/etc/systemd/system/${UNIT}.service"
CHAIN_UNIT="gotham-verify-chain"
CHAIN_UNIT_FILE="/etc/systemd/system/${CHAIN_UNIT}.service"
CHAIN_USER="gv$$"
CHAIN_SUDOERS="/etc/sudoers.d/gotham-verify-chain"
INSTALLED_UNIT="/etc/systemd/system/gotham.service"
CHAIN_AVAILABLE=0
FAILURES=0

cleanup() {
    systemctl stop "${UNIT}" >/dev/null 2>&1 || true
    systemctl stop "${CHAIN_UNIT}" >/dev/null 2>&1 || true
    rm -f "${UNIT_FILE}" "${CHAIN_UNIT_FILE}" "${CHAIN_SUDOERS}"
    if [ "${CHAIN_AVAILABLE}" -eq 1 ]; then
        userdel "${CHAIN_USER}" >/dev/null 2>&1 || true
    fi
    systemctl daemon-reload >/dev/null 2>&1 || true
    rm -rf "${SCRATCH}"
}
trap cleanup EXIT INT TERM

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; FAILURES=$((FAILURES + 1)); }
skip() { echo "SKIP: $*"; }

# run_wrapper runs the wrapper as root WITHOUT sudo, stripping sudo's
# environment so the non-sudo test seam (GOTHAM_UPDATER_CONF) is honoured.
run_wrapper() {
    env -u SUDO_USER -u SUDO_UID -u SUDO_GID \
        GOTHAM_UPDATER_CONF="${CONF}" \
        sh "${SCRIPT_DIR}/gotham-update.sh"
}

# wait_status polls path until it records a terminal result (up to ~30s).
wait_status() {
    _i=0
    while [ "${_i}" -lt 300 ]; do
        if [ -f "$1" ] && grep -qE '^result=(ok|rolled_back|rollback_failed|no_backup|wrapper_failed)' "$1" 2>/dev/null; then
            return 0
        fi
        _i=$((_i + 1))
        sleep 0.1
    done
    return 1
}

good_binary() {
    cat >"$1" <<EOF
#!/bin/sh
exec python3 -m http.server ${PORT} --bind 127.0.0.1 --directory "${SCRATCH}/docroot"
EOF
    chmod 0755 "$1"
}

mkdir -p "${SCRATCH}/bin" "${SCRATCH}/docroot" "${SCRATCH}/statusdir"
chmod 0755 "${SCRATCH}/statusdir"
: >"${SCRATCH}/docroot/healthz"
good_binary "${SCRATCH}/bin/gotham"
: >"${SCRATCH}/update.lock"

cat >"${UNIT_FILE}" <<EOF
[Unit]
Description=Gotham self-update verification (scratch)
[Service]
Type=simple
KillMode=process
ExecStart=${SCRATCH}/bin/gotham
Restart=no
EOF

CONF="${SCRATCH}/updater.conf"
cat >"${CONF}" <<EOF
GOTHAM_BINARY=${SCRATCH}/bin/gotham
GOTHAM_SERVICE=${UNIT}
GOTHAM_HEALTH=http://127.0.0.1:${PORT}/healthz
GOTHAM_TIMEOUT=10
GOTHAM_STATUS=${SCRATCH}/statusdir/update.status
GOTHAM_PENDING=${SCRATCH}/update.pending
GOTHAM_LOCK=${SCRATCH}/update.lock
GOTHAM_GRACE=0
EOF
chmod 0644 "${CONF}"

systemctl daemon-reload
systemctl start "${UNIT}" || true
sleep 1

echo "== 1. healthy update writes a root-owned status =="
printf 'result=staged\nversion=v1.2.0-verify\n' >"${SCRATCH}/update.pending"
if run_wrapper; then
    pass "wrapper reported the new binary healthy"
else
    fail "wrapper did not report a healthy binary"
fi
if [ -f "${SCRATCH}/statusdir/update.status" ] && grep -q '^result=ok' "${SCRATCH}/statusdir/update.status"; then
    pass "status result=ok recorded"
else
    fail "status not recorded as ok: $(cat "${SCRATCH}/statusdir/update.status" 2>/dev/null)"
fi
if [ "$(stat -c '%U' "${SCRATCH}/statusdir/update.status" 2>/dev/null)" = "root" ]; then
    pass "status file is root-owned"
else
    fail "status file is not root-owned"
fi
if [ ! -e "${SCRATCH}/update.pending" ]; then
    pass "pending marker released"
else
    fail "pending marker not released"
fi

echo "== 2. failed start rolls back =="
cp "${SCRATCH}/bin/gotham" "${SCRATCH}/bin/gotham.pristine"
cp "${SCRATCH}/bin/gotham" "${SCRATCH}/bin/gotham.old"
printf '#!/bin/sh\nexit 1\n' >"${SCRATCH}/bin/gotham"
chmod 0755 "${SCRATCH}/bin/gotham"
printf 'result=staged\nversion=v9.9.9-verify\n' >"${SCRATCH}/update.pending"
if run_wrapper; then
    fail "wrapper returned success for a broken binary"
else
    pass "wrapper returned nonzero for a broken binary"
fi
if grep -q '^result=rolled_back' "${SCRATCH}/statusdir/update.status"; then
    pass "status result=rolled_back recorded"
else
    fail "status not rolled_back: $(cat "${SCRATCH}/statusdir/update.status" 2>/dev/null)"
fi
# The restore consumes .old, so compare the restored binary against the
# pristine copy and require it to be executable.
if cmp -s "${SCRATCH}/bin/gotham" "${SCRATCH}/bin/gotham.pristine"; then
    pass "previous binary restored (matches the pristine copy)"
else
    fail "previous binary was not restored"
fi
if [ -x "${SCRATCH}/bin/gotham" ]; then
    pass "restored binary is executable"
else
    fail "restored binary is not executable"
fi

echo "== 3. wrapper survives the restart cgroup (KillMode=process) =="
systemctl restart "${UNIT}" >/dev/null 2>&1 || true
sleep 1
CGROUP=$(systemctl show -p ControlGroup --value "${UNIT}" 2>/dev/null)
CGROUP_PROCS="/sys/fs/cgroup${CGROUP}/cgroup.procs"
if [ -w "${CGROUP_PROCS}" ]; then
    sleep 60 &
    SLEEPER=$!
    if echo "${SLEEPER}" >"${CGROUP_PROCS}" 2>/dev/null; then
        systemctl restart "${UNIT}" >/dev/null 2>&1 || true
        if kill -0 "${SLEEPER}" 2>/dev/null; then
            pass "a helper in the service cgroup survived the restart"
        else
            fail "a helper in the service cgroup was killed by the restart"
        fi
    else
        skip "could not join the service cgroup (non-systemd cgroup layout)"
    fi
    kill "${SLEEPER}" 2>/dev/null || true
else
    skip "${CGROUP_PROCS} is not writable; inspect KillMode=process manually"
fi

echo "== 4. staged gate and self-update regression tests =="
if command -v "${GOTHAM_GO}" >/dev/null 2>&1 && [ -f "${REPO_DIR}/go.mod" ]; then
    if (cd "${REPO_DIR}" && "${GOTHAM_GO}" test -count=1 ./internal/updates \
        -run 'TestApplierRefusesSecondApplyWhileStaged|TestApplierConcurrentApplySerialized|TestWrapperFailedPreservesKnownGood|TestMonitorRestartRollsBack|TestCrashBeforeCommitReopensGate|TestResumeStagedRelaunches|TestApplierRejectsDigestMismatch|TestLoadPublicKeyPrecedence|TestWrapperRefusesSymlinkedLock'); then
        pass "gate, wrapper-failed rollback, resume, manifest and lock tests pass"
    else
        fail "self-update Go tests failed"
    fi
    if (cd "${REPO_DIR}" && "${GOTHAM_GO}" test -count=1 ./internal/server -run TestUpdateRoutesGating); then
        pass "route gating test passes"
    else
        fail "route gating test failed"
    fi
else
    skip "${GOTHAM_GO} toolchain or repository not available; run the Go tests separately"
fi

echo "== 5. unit wiring, binary ownership and the lock symlink negative =="
for setting in "KillMode=process" "GOTHAM_UPDATE_BINARY=" "GOTHAM_UPDATE_PENDING=" "GOTHAM_UPDATE_LOCK="; do
    if grep -q "${setting}" "${SCRIPT_DIR}/gotham.service"; then
        pass "repo unit contains ${setting}"
    else
        fail "repo unit is missing ${setting}"
    fi
done
if [ -f "${INSTALLED_UNIT}" ]; then
    if grep -q 'GOTHAM_UPDATE_BINARY=' "${INSTALLED_UNIT}"; then
        for setting in "KillMode=process" "GOTHAM_UPDATE_PENDING=" "GOTHAM_UPDATE_LOCK="; do
            if grep -q "${setting}" "${INSTALLED_UNIT}"; then
                pass "installed unit contains ${setting}"
            else
                fail "installed unit is missing ${setting}"
            fi
        done
    else
        skip "${INSTALLED_UNIT} is a foreign/older unit (no GOTHAM_UPDATE_BINARY=)"
    fi
else
    skip "${INSTALLED_UNIT} is not installed"
fi
INSTALLED_BINARY="/var/lib/gotham/bin/gotham"
if [ -f "${INSTALLED_BINARY}" ]; then
    if [ "$(stat -c '%U' "${INSTALLED_BINARY}" 2>/dev/null)" = "gotham" ]; then
        pass "installed binary is owned by the service user"
    else
        fail "installed binary must be owned by gotham (protected_hardlinks makes os.Link of a root-owned file fail)"
    fi
else
    skip "${INSTALLED_BINARY} is not installed"
fi

# N1: a Gotham-planted update.lock symlink must not make root modify the target.
LOCKVICTIM="${SCRATCH}/lock-victim"
printf 'root-only victim\n' >"${LOCKVICTIM}"
chmod 0600 "${LOCKVICTIM}"
rm -f "${SCRATCH}/update.lock"
ln -s "${LOCKVICTIM}" "${SCRATCH}/update.lock"
printf 'result=staged\nversion=v1.2.0-verify\n' >"${SCRATCH}/update.pending"
run_wrapper >/dev/null 2>&1 || true
if [ "$(cat "${LOCKVICTIM}" 2>/dev/null)" = "root-only victim" ]; then
    pass "planted update.lock symlink left its victim unchanged"
else
    fail "planted update.lock symlink modified its victim"
fi
rm -f "${SCRATCH}/update.lock"
: >"${SCRATCH}/update.lock"

echo "== 6. real launch chain (non-root unit + sudo) =="
if ! command -v useradd >/dev/null 2>&1 || ! command -v sudo >/dev/null 2>&1 || ! command -v setsid >/dev/null 2>&1; then
    skip "useradd/sudo/setsid not available; the real chain was not exercised"
else
    if useradd --system --no-create-home "${CHAIN_USER}" 2>/dev/null; then
        CHAIN_AVAILABLE=1
        mkdir -p "${SCRATCH}/chain/bin" "${SCRATCH}/chain/docroot"
        chmod 0755 "${SCRATCH}/chain" "${SCRATCH}/chain/bin" "${SCRATCH}/chain/docroot"
        : >"${SCRATCH}/chain/docroot/healthz"
        : >"${SCRATCH}/chain/update.lock"

        # A wrapper copy whose default conf is the scratch chain conf. The sudo
        # guard ignores GOTHAM_* overrides, so the default path must be scratch;
        # no host file is touched.
        sed "s#/etc/gotham/updater.conf#${SCRATCH}/chain.conf#g" \
            "${SCRIPT_DIR}/gotham-update.sh" >"${SCRATCH}/gotham-update"
        chmod 0755 "${SCRATCH}/gotham-update"
        chown root:root "${SCRATCH}/gotham-update"

        cat >"${SCRATCH}/chain.conf" <<EOF
GOTHAM_BINARY=${SCRATCH}/chain/bin/gotham
GOTHAM_SERVICE=${CHAIN_UNIT}
GOTHAM_HEALTH=http://127.0.0.1:${CHAIN_PORT}/healthz
GOTHAM_TIMEOUT=10
GOTHAM_STATUS=${SCRATCH}/statusdir/chain.status
GOTHAM_PENDING=${SCRATCH}/chain/update.pending
GOTHAM_LOCK=${SCRATCH}/chain/update.lock
GOTHAM_GRACE=0
EOF
        chmod 0644 "${SCRATCH}/chain.conf"
        # The service user owns its StateDirectory, as in production; the
        # wrapper (root) still writes the status into the root-owned statusdir.
        chown -R "${CHAIN_USER}:${CHAIN_USER}" "${SCRATCH}/chain"

        cat >"${CHAIN_SUDOERS}" <<EOF
Defaults:${CHAIN_USER} !requiretty
${CHAIN_USER} ALL=(root) NOPASSWD: ${SCRATCH}/gotham-update ""
EOF
        chmod 0440 "${CHAIN_SUDOERS}"
        if command -v visudo >/dev/null 2>&1 && ! visudo -cf "${CHAIN_SUDOERS}" >/dev/null 2>&1; then
            fail "scratch sudoers drop-in is invalid"
        fi

        cat >"${CHAIN_UNIT_FILE}" <<EOF
[Unit]
Description=Gotham self-update chain verification (scratch)
[Service]
Type=simple
User=${CHAIN_USER}
Group=${CHAIN_USER}
WorkingDirectory=${SCRATCH}/chain
KillMode=process
ExecStart=${SCRATCH}/chain/bin/gotham
Restart=no
EOF

        systemctl daemon-reload

        # write_chain_binary: launches the sudo wrapper once, then either serves
        # health (healthy) or exits 1 (broken).
        write_chain_binary() {
            # $1 path, $2 "1" for broken
            cat >"$1" <<EOF
#!/bin/sh
if [ ! -e "${SCRATCH}/chain/chain.launched" ]; then
    : > "${SCRATCH}/chain/chain.launched" 2>/dev/null || true
    setsid sudo -n "${SCRATCH}/gotham-update" &
    if command -v pgrep >/dev/null 2>&1; then
        (
            i=0
            while [ "\$i" -lt 50 ]; do
                pid=\$(pgrep -f "${SCRATCH}/gotham-update" 2>/dev/null | head -n 1)
                if [ -n "\$pid" ]; then
                    cat "/proc/\$pid/cgroup" > "${SCRATCH}/chain/wrapper.cgroup" 2>/dev/null || true
                    break
                fi
                i=\$((i + 1))
                sleep 0.1
            done
        ) &
    fi
fi
EOF
            if [ "$2" = "1" ]; then
                printf 'exit 1\n' >>"$1"
            else
                cat >>"$1" <<EOF
exec python3 -m http.server ${CHAIN_PORT} --bind 127.0.0.1 --directory "${SCRATCH}/chain/docroot"
EOF
            fi
            chmod 0755 "$1"
            chown "${CHAIN_USER}:${CHAIN_USER}" "$1"
        }

        # Healthy chain run.
        rm -f "${SCRATCH}/chain/chain.launched" "${SCRATCH}/statusdir/chain.status" "${SCRATCH}/chain/wrapper.cgroup"
        write_chain_binary "${SCRATCH}/chain/bin/gotham" "0"
        printf 'result=staged\nversion=v1.2.0-chain\n' >"${SCRATCH}/chain/update.pending"
        systemctl start "${CHAIN_UNIT}" || true
        if wait_status "${SCRATCH}/statusdir/chain.status"; then
            if grep -q '^result=ok' "${SCRATCH}/statusdir/chain.status"; then
                pass "real chain: healthy update recorded result=ok"
            else
                fail "real chain: healthy status = $(cat "${SCRATCH}/statusdir/chain.status" 2>/dev/null)"
            fi
        else
            fail "real chain: healthy update never recorded a status"
        fi
        if [ ! -e "${SCRATCH}/chain/update.pending" ]; then
            pass "real chain: pending cleared"
        else
            fail "real chain: pending not cleared"
        fi
        if [ -f "${SCRATCH}/chain/wrapper.cgroup" ]; then
            pass "real chain: wrapper cgroup recorded ($(tr '\n' ' ' <"${SCRATCH}/chain/wrapper.cgroup"))"
        else
            skip "real chain: wrapper cgroup was not captured (pgrep race)"
        fi

        # Broken chain run: must roll back to the known good.
        systemctl stop "${CHAIN_UNIT}" >/dev/null 2>&1 || true
        cp "${SCRATCH}/chain/bin/gotham" "${SCRATCH}/chain/bin/gotham.pristine"
        cp "${SCRATCH}/chain/bin/gotham" "${SCRATCH}/chain/bin/gotham.old"
        rm -f "${SCRATCH}/chain/chain.launched" "${SCRATCH}/statusdir/chain.status"
        write_chain_binary "${SCRATCH}/chain/bin/gotham" "1"
        printf 'result=staged\nversion=v9.9.9-chain\n' >"${SCRATCH}/chain/update.pending"
        systemctl start "${CHAIN_UNIT}" || true
        if wait_status "${SCRATCH}/statusdir/chain.status"; then
            if grep -q '^result=rolled_back' "${SCRATCH}/statusdir/chain.status"; then
                pass "real chain: broken binary recorded result=rolled_back"
            else
                fail "real chain: broken status = $(cat "${SCRATCH}/statusdir/chain.status" 2>/dev/null)"
            fi
        else
            fail "real chain: broken binary never recorded a status"
        fi
        if cmp -s "${SCRATCH}/chain/bin/gotham" "${SCRATCH}/chain/bin/gotham.pristine"; then
            pass "real chain: previous binary restored"
        else
            fail "real chain: previous binary was not restored"
        fi
        if [ ! -e "${SCRATCH}/chain/update.pending" ]; then
            pass "real chain: pending cleared after rollback"
        else
            fail "real chain: pending not cleared after rollback"
        fi
    else
        skip "could not create the scratch service user; the real chain was not exercised"
    fi
fi

echo
if [ "${FAILURES}" -eq 0 ]; then
    echo "verify-systemd: all checks passed"
    exit 0
fi
echo "verify-systemd: ${FAILURES} check(s) failed" >&2
exit 1
