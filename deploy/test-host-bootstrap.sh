#!/bin/sh
#
# test-host-bootstrap.sh: exercises deploy/gotham-hostbootstrap.sh on a fake
# root (JUS-185). It never touches the real /usr/libexec or /etc: the script's
# GOTHAM_HOST_BOOTSTRAP_ROOT seam (honoured only when not under sudo) points
# every write at a temp dir, and a fake visudo stands in for the real one.
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BOOTSTRAP="${SCRIPT_DIR}/gotham-hostbootstrap.sh"
[ -f "${BOOTSTRAP}" ] || { echo "FAIL: ${BOOTSTRAP} is missing" >&2; exit 1; }

SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/test-host-bootstrap.XXXXXX")"
trap 'rm -rf "${SCRATCH}"' EXIT

# Fake visudo: succeeds by default; GOTHAM_TEST_VISUDO_FAIL makes it fail.
FAKE_BIN="${SCRATCH}/bin"
mkdir -p "${FAKE_BIN}"
cat >"${FAKE_BIN}/visudo" <<'SHIM'
#!/bin/sh
[ "${GOTHAM_TEST_VISUDO_FAIL:-0}" = "1" ] && { echo "syntax error" >&2; exit 1; }
exit 0
SHIM
chmod 0755 "${FAKE_BIN}/visudo"
PATH="${FAKE_BIN}:${PATH}"
export PATH

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

root_for() { mkdir -p "$1"; printf '%s' "$1"; }

# ---- non-root refusal without the test seam --------------------------------
if [ "$(id -u)" -ne 0 ]; then
    if (unset GOTHAM_HOST_BOOTSTRAP_ROOT; sh "${BOOTSTRAP}" apply) >"${SCRATCH}/nonroot.log" 2>&1; then
        fail "a non-root run without the test seam was accepted"
    fi
    grep -q "must run as root" "${SCRATCH}/nonroot.log" || fail "non-root run did not explain the refusal"
    pass "refuses a non-root caller and does not trust env for privilege"
fi

# ---- create on a fake root -------------------------------------------------
ROOT1="$(root_for "${SCRATCH}/root1")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT1}" sh "${BOOTSTRAP}" create alice >/dev/null
for f in usr/libexec/gotham/hostbootstrap/gotham-hostctl \
    usr/libexec/gotham/hostbootstrap/gotham-toolctl \
    usr/libexec/gotham/hostbootstrap/gotham-hostctl.sudoers \
    usr/libexec/gotham/hostbootstrap/gotham-hostbootstrap.sh \
    usr/libexec/gotham/gotham-hostctl \
    usr/libexec/gotham/gotham-toolctl \
    etc/sudoers.d/gotham-hostctl; do
    [ -f "${ROOT1}/${f}" ] || fail "create did not install ${f}"
done
grep -q "alice ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-hostctl status" "${ROOT1}/etc/sudoers.d/gotham-hostctl" \
    || fail "the hostctl drop-in is missing the status verb for alice"
grep -q "alice ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-toolctl install-tool gotham-hostctl" "${ROOT1}/etc/sudoers.d/gotham-hostctl" \
    || fail "the hostctl drop-in is missing the pinned repair verb"
[ "$(stat -c '%a' "${ROOT1}/etc/sudoers.d/gotham-hostctl" 2>/dev/null || stat -f '%Lp' "${ROOT1}/etc/sudoers.d/gotham-hostctl")" = "440" ] \
    || fail "the drop-in is not mode 0440"
pass "create installs the payload, live helpers and the pinned drop-in"

# A custom control-plane user is baked in, not the default gotham.
GOTHAM_HOST_BOOTSTRAP_ROOT="$(root_for "${SCRATCH}/root-custom")" sh "${BOOTSTRAP}" create ops >/dev/null
grep -q "ops ALL=(root) NOPASSWD:" "${SCRATCH}/root-custom/etc/sudoers.d/gotham-hostctl" || fail "custom cp user not honoured"
grep -q "gotham ALL=(root)" "${SCRATCH}/root-custom/etc/sudoers.d/gotham-hostctl" && fail "default user leaked into a custom install"
pass "a custom control-plane user is used instead of the default"

# ---- idempotency -----------------------------------------------------------
before="$(cksum "${ROOT1}/usr/libexec/gotham/gotham-hostctl" "${ROOT1}/etc/sudoers.d/gotham-hostctl")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT1}" sh "${BOOTSTRAP}" apply >/dev/null
after="$(cksum "${ROOT1}/usr/libexec/gotham/gotham-hostctl" "${ROOT1}/etc/sudoers.d/gotham-hostctl")"
[ "${before}" = "${after}" ] || fail "a second run changed the installed files"
pass "apply is idempotent"

# ---- symlink refusal -------------------------------------------------------
ROOT2="$(root_for "${SCRATCH}/root2")"
mkdir -p "${ROOT2}/usr/libexec/gotham"
ln -s /etc/passwd "${ROOT2}/usr/libexec/gotham/gotham-hostctl"
if GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT2}" sh "${BOOTSTRAP}" create alice >"${SCRATCH}/symlink.log" 2>&1; then
    fail "a symlinked live helper was accepted"
fi
grep -q "refusing to replace symlink" "${SCRATCH}/symlink.log" || fail "symlink refusal was not explained"
pass "refuses a symlinked destination"

# ---- a bad sudoers abort leaves nothing -----------------------------------
ROOT3="$(root_for "${SCRATCH}/root3")"
if GOTHAM_TEST_VISUDO_FAIL=1 GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT3}" sh "${BOOTSTRAP}" create alice >"${SCRATCH}/badsudo.log" 2>&1; then
    fail "a visudo rejection was accepted"
fi
grep -q "failed visudo" "${SCRATCH}/badsudo.log" || fail "visudo rejection was not explained"
[ ! -e "${ROOT3}/etc/sudoers.d/gotham-hostctl" ] || fail "a rejected drop-in was left in place"
pass "a visudo rejection aborts and leaves no drop-in"

# ---- apply without a payload is a clear refusal ----------------------------
ROOT4="$(root_for "${SCRATCH}/root4")"
if GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT4}" sh "${BOOTSTRAP}" apply >"${SCRATCH}/nopay.log" 2>&1; then
    fail "apply without a payload succeeded"
fi
grep -q "no installed payload" "${SCRATCH}/nopay.log" || fail "missing-payload refusal was not explained"
pass "apply without a payload refuses clearly"

echo "ALL HOST-BOOTSTRAP TESTS PASSED"
