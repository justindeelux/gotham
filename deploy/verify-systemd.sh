#!/bin/sh
#
# verify-systemd.sh proves the BE-9.1 self-update deployment on a Linux host
# with systemd. It is safe to run beside a real Gotham service: it uses a
# scratch unit (gotham-verify.service) and scratch paths under a temp directory,
# and removes them on exit.
#
#   sudo deploy/verify-systemd.sh
#
# It proves:
#   1. the wrapper's status is written into a root-owned directory/file,
#   2. a failed start rolls back to the previous binary and exits nonzero,
#   3. the restart wrapper survives the service restart cgroup
#      (KillMode=process),
#   4. the staged gate refuses a second apply while one is pending
#      (via the Go tests, which need the repository), and
#   5. the installed unit declares the required self-update wiring.
#
# It does NOT provision a real network release; the signed-manifest chain is
# covered by `go test ./internal/updates`.
set -u

if [ "$(id -u)" -ne 0 ]; then
    echo "verify-systemd.sh must run as root (try sudo)" >&2
    exit 1
fi
for tool in systemctl python3 mktemp; do
    if ! command -v "${tool}" >/dev/null 2>&1; then
        echo "verify-systemd.sh: ${tool} is required" >&2
        exit 1
    fi
done

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)
UNIT="gotham-verify"
UNIT_FILE="/etc/systemd/system/${UNIT}.service"
SCRATCH=$(mktemp -d /tmp/gotham-verify.XXXXXX)
PORT=$(( 20000 + ($$ % 20000) ))
FAILURES=0

cleanup() {
    systemctl stop "${UNIT}" >/dev/null 2>&1 || true
    rm -f "${UNIT_FILE}"
    systemctl daemon-reload >/dev/null 2>&1 || true
    rm -rf "${SCRATCH}"
}
trap cleanup EXIT INT TERM

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; FAILURES=$((FAILURES + 1)); }

# good_binary writes the healthy "server" script.
good_binary() {
    cat >"$1" <<EOF
#!/bin/sh
exec python3 -m http.server ${PORT} --bind 127.0.0.1 --directory "${SCRATCH}/docroot"
EOF
    chmod 0755 "$1"
}

mkdir -p "${SCRATCH}/bin" "${SCRATCH}/docroot" "${SCRATCH}/statusdir"
: >"${SCRATCH}/docroot/healthz"
good_binary "${SCRATCH}/bin/gotham"

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

# Run the wrapper as root without sudo, so the test seam is honoured.
run_wrapper() {
    GOTHAM_UPDATER_CONF="${CONF}" sh "${SCRIPT_DIR}/gotham-update.sh"
}

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
if cmp -s "${SCRATCH}/bin/gotham" "${SCRATCH}/bin/gotham.old"; then
    pass "previous binary restored"
else
    fail "previous binary was not restored"
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
        echo "SKIP: could not join the service cgroup (non-systemd cgroup layout)"
    fi
    kill "${SLEEPER}" 2>/dev/null || true
else
    echo "SKIP: ${CGROUP_PROCS} is not writable; inspect KillMode=process manually"
fi

echo "== 4. staged gate and signed-manifest tests =="
if command -v go >/dev/null 2>&1 && [ -f "${REPO_DIR}/go.mod" ]; then
    if (cd "${REPO_DIR}" && go test -count=1 ./internal/updates \
        -run 'TestApplierRefusesSecondApplyWhileStaged|TestApplierConcurrentApplySerialized|TestApplierRejectsDigestMismatch|TestLoadPublicKeyPrecedence'); then
        pass "staged gate, serialization, manifest and key-precedence tests pass"
    else
        fail "self-update Go tests failed"
    fi
    if (cd "${REPO_DIR}" && go test -count=1 ./internal/server -run TestUpdateRoutesGating); then
        pass "route gating test passes"
    else
        fail "route gating test failed"
    fi
else
    echo "SKIP: go toolchain not available; run the Go tests separately"
fi

echo "== 5. installed unit wiring =="
INSTALLED_UNIT="/etc/systemd/system/gotham.service"
if [ -f "${INSTALLED_UNIT}" ]; then
    for setting in "KillMode=process" "GOTHAM_UPDATE_BINARY=" "GOTHAM_UPDATE_PENDING=" "GOTHAM_UPDATE_LOCK="; do
        if grep -q "${setting}" "${INSTALLED_UNIT}"; then
            pass "installed unit contains ${setting}"
        else
            fail "installed unit is missing ${setting}"
        fi
    done
else
    echo "SKIP: ${INSTALLED_UNIT} is not installed"
fi

echo
if [ "${FAILURES}" -eq 0 ]; then
    echo "verify-systemd: all checks passed"
    exit 0
fi
echo "verify-systemd: ${FAILURES} check(s) failed" >&2
exit 1
