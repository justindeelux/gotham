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

# The reserved sudoers word ALL is rejected, never written into a drop-in.
if GOTHAM_HOST_BOOTSTRAP_ROOT="$(root_for "${SCRATCH}/root-all")" sh "${BOOTSTRAP}" create ALL >"${SCRATCH}/all.log" 2>&1; then
    fail "the reserved user ALL was accepted"
fi
grep -q "invalid control-plane user" "${SCRATCH}/all.log" || fail "ALL rejection was not explained"
pass "rejects the reserved user ALL"

# ---- idempotency -----------------------------------------------------------
before="$(cksum "${ROOT1}/usr/libexec/gotham/gotham-hostctl" "${ROOT1}/etc/sudoers.d/gotham-hostctl")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT1}" sh "${BOOTSTRAP}" apply >/dev/null
after="$(cksum "${ROOT1}/usr/libexec/gotham/gotham-hostctl" "${ROOT1}/etc/sudoers.d/gotham-hostctl")"
[ "${before}" = "${after}" ] || fail "a second run changed the installed files"
pass "apply is idempotent"

# ---- apply restores a deleted helper + drop-in -----------------------------
# This is the root-owned re-apply the self-update wrapper runs (M1): it restores
# the live artifacts from the payload, so a deleted helper or drop-in is fixed.
rm -f "${ROOT1}/usr/libexec/gotham/gotham-hostctl" "${ROOT1}/etc/sudoers.d/gotham-hostctl"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT1}" sh "${BOOTSTRAP}" apply >/dev/null
[ -f "${ROOT1}/usr/libexec/gotham/gotham-hostctl" ] || fail "apply did not restore the helper"
[ -f "${ROOT1}/etc/sudoers.d/gotham-hostctl" ] || fail "apply did not restore the drop-in"
pass "apply restores a deleted helper and drop-in from the payload"

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

# ---- create installs the one-time bridge artifacts (JUS-188) ---------------
# The root-owned release key, the fetch helper and the canonical wrapper, so the
# update wrapper can verify and apply released payloads on later self-updates.
for f in usr/libexec/gotham/gotham-signing-key.pub \
    usr/libexec/gotham/gotham-hostbootstrap-fetch.sh \
    usr/libexec/gotham/gotham-update \
    usr/libexec/gotham/hostbootstrap/gotham-hostbootstrap-fetch.sh; do
    [ -f "${ROOT1}/${f}" ] || fail "create did not install the bridge artifact ${f}"
done
pass "create installs the root key, fetch helper and wrapper (one-time bridge)"

# ---- create records the control-plane user and seeds the high-water mark ----
[ -f "${ROOT1}/usr/libexec/gotham/hostbootstrap/cp-user" ] || fail "create did not record the control-plane user"
grep -qx "alice" "${ROOT1}/usr/libexec/gotham/hostbootstrap/cp-user" || fail "the recorded control-plane user is wrong"
pass "create records the control-plane user for later drop-in regeneration"

ROOT7="$(root_for "${SCRATCH}/root7")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT7}" sh "${BOOTSTRAP}" create carol v9.9.9 >/dev/null
[ "$(cat "${ROOT7}/usr/libexec/gotham/hostbootstrap/version")" = "v9.9.9" ] \
    || fail "create did not seed the high-water mark from the installer version"
pass "create seeds the high-water mark from the installer version"

# ---- version-cmp orders release tags (high-water-mark helper) --------------
vcase() { # a b want
    got="$(sh "${BOOTSTRAP}" version-cmp "$1" "$2")"
    [ "${got}" = "$3" ] || fail "version-cmp $1 $2 = ${got}, want $3"
}
vcase v1.2.3 v1.2.3 0
vcase v1.2.3 v1.2.4 -1
vcase v1.2.4 v1.2.3 1
vcase v1.9.0 v1.10.0 -1
vcase v1.10.0 v1.9.0 1
vcase v0.0.9 v0.1.0 -1
vcase v2.0.0 v2.0.0-rc.1 1
vcase v2.0.0-rc.1 v2.0.0 -1
vcase v2.0.0-rc.1 v2.0.0-rc.2 -1
vcase v1 v1.0.0 0
vcase v1.2 v1.2.0 0
pass "version-cmp orders release tags"

# ---- apply-release installs a verified payload, keeps the pinned drop-in ---
ROOT5="$(root_for "${SCRATCH}/root5")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" create bob >/dev/null
SRC="${SCRATCH}/payload-src"
mkdir -p "${SRC}"
printf '#!/bin/sh\necho refreshed-hostctl\n' >"${SRC}/gotham-hostctl"
printf '#!/bin/sh\necho refreshed-toolctl\n' >"${SRC}/gotham-toolctl"
printf '#!/bin/sh\necho refreshed-bootstrap\n' >"${SRC}/gotham-hostbootstrap.sh"
printf '#!/bin/sh\necho refreshed-fetch\n' >"${SRC}/gotham-hostbootstrap-fetch.sh"
printf '#!/bin/sh\necho refreshed-wrapper\n' >"${SRC}/gotham-update.sh"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" apply-release "${SRC}" v1.2.0 >/dev/null
grep -q refreshed-hostctl "${ROOT5}/usr/libexec/gotham/gotham-hostctl" || fail "apply-release did not refresh the live hostctl"
grep -q refreshed-toolctl "${ROOT5}/usr/libexec/gotham/gotham-toolctl" || fail "apply-release did not refresh the live toolctl"
grep -q refreshed-fetch "${ROOT5}/usr/libexec/gotham/gotham-hostbootstrap-fetch.sh" || fail "apply-release did not refresh the live fetch helper"
grep -q refreshed-wrapper "${ROOT5}/usr/libexec/gotham/gotham-update" || fail "apply-release did not refresh the wrapper"
grep -q refreshed-toolctl "${ROOT5}/usr/libexec/gotham/hostbootstrap/gotham-toolctl" || fail "apply-release did not refresh the payload"
grep -q "bob ALL=(root)" "${ROOT5}/etc/sudoers.d/gotham-hostctl" || fail "the recorded drop-in user was lost"
grep -q "gotham ALL=(root)" "${ROOT5}/etc/sudoers.d/gotham-hostctl" && fail "the drop-in user changed"
[ "$(cat "${ROOT5}/usr/libexec/gotham/hostbootstrap/version")" = "v1.2.0" ] || fail "apply-release did not record the version"
pass "apply-release installs a verified payload, keeps the recorded user and records the version"

# ---- anti-rollback: older refused, equal idempotent, newer applied ---------
if GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" apply-release "${SRC}" v1.1.0 >"${SCRATCH}/rollback.log" 2>&1; then
    fail "an older signed payload was applied"
fi
grep -q "refusing to downgrade" "${SCRATCH}/rollback.log" || fail "the downgrade refusal was not explained"
[ "$(cat "${ROOT5}/usr/libexec/gotham/hostbootstrap/version")" = "v1.2.0" ] \
    || fail "a refused downgrade changed the high-water mark"
pass "an older signed payload is refused (anti-rollback)"

GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" apply-release "${SRC}" v1.2.0 >/dev/null \
    || fail "an equal-version re-apply failed"
pass "an equal signed version re-applies idempotently"

GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" apply-release "${SRC}" v1.3.0 >/dev/null \
    || fail "a newer signed payload failed"
[ "$(cat "${ROOT5}/usr/libexec/gotham/hostbootstrap/version")" = "v1.3.0" ] \
    || fail "the high-water mark did not advance"
pass "a newer signed payload is applied and advances the high-water mark"

# ---- every refresh regenerates the drop-in from the released hostbootstrap -
# The released hostbootstrap (not the installed one) decides the verb set, so a
# release that adds or removes a verb is honoured instead of the stale drop-in.
RELBOOT="${SCRATCH}/relboot"
mkdir -p "${RELBOOT}"
sed 's/^HOSTCTL_VERBS=.*/HOSTCTL_VERBS="status apply-network confirm-network revert-network apply-system extra-verb"/' \
    "${BOOTSTRAP}" >"${RELBOOT}/gotham-hostbootstrap.sh"
cp "${SRC}/gotham-hostctl" "${RELBOOT}/gotham-hostctl"
cp "${SRC}/gotham-toolctl" "${RELBOOT}/gotham-toolctl"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${RELBOOT}/gotham-hostbootstrap.sh" apply-release "${RELBOOT}" v1.4.0 >/dev/null \
    || fail "apply-release from a released hostbootstrap failed"
grep -q "extra-verb" "${ROOT5}/etc/sudoers.d/gotham-hostctl" || fail "the live drop-in was not regenerated"
grep -q "extra-verb" "${ROOT5}/usr/libexec/gotham/hostbootstrap/gotham-hostctl.sudoers" || fail "the payload drop-in was not regenerated"
grep -q "bob ALL=(root)" "${ROOT5}/etc/sudoers.d/gotham-hostctl" || fail "the recorded user was lost on regeneration"
pass "every refresh regenerates the drop-in from the released hostbootstrap"

# ---- a failed refresh leaves the live files and the high-water mark intact --
before_hctl="$(cksum "${ROOT5}/usr/libexec/gotham/gotham-hostctl")"
if GOTHAM_TEST_VISUDO_FAIL=1 GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" \
    sh "${BOOTSTRAP}" apply-release "${SRC}" v1.5.0 >"${SCRATCH}/refresh-visudo.log" 2>&1; then
    fail "a refresh with a bad drop-in succeeded"
fi
after_hctl="$(cksum "${ROOT5}/usr/libexec/gotham/gotham-hostctl")"
[ "${before_hctl}" = "${after_hctl}" ] || fail "a failed refresh still replaced the live helper"
[ "$(cat "${ROOT5}/usr/libexec/gotham/hostbootstrap/version")" = "v1.4.0" ] \
    || fail "a failed refresh advanced the high-water mark"
pass "a failed refresh leaves the live files and the high-water mark intact"

# ---- apply-release without a recorded user refuses -------------------------
ROOT6="$(root_for "${SCRATCH}/root6")"
if GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT6}" sh "${BOOTSTRAP}" apply-release "${SRC}" v1.2.0 >"${SCRATCH}/norelay.log" 2>&1; then
    fail "apply-release without a recorded user succeeded"
fi
grep -q "run create first" "${SCRATCH}/norelay.log" || fail "the missing-user refusal was not explained"
pass "apply-release refuses without a recorded control-plane user"

# ---- apply-release needs the signature-verified version --------------------
if GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" apply-release "${SRC}" "" >"${SCRATCH}/nover.log" 2>&1; then
    fail "apply-release without a version succeeded"
fi
grep -q "needs the signature-verified version" "${SCRATCH}/nover.log" || fail "the missing-version refusal was not explained"
pass "apply-release requires the signature-verified version"

# ---- apply-release refuses a symlinked payload source (TOCTOU) -------------
rm -f "${SRC}/gotham-toolctl"
ln -s /etc/passwd "${SRC}/gotham-toolctl"
if GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT5}" sh "${BOOTSTRAP}" apply-release "${SRC}" v1.6.0 >"${SCRATCH}/srclink.log" 2>&1; then
    fail "apply-release accepted a symlinked payload source"
fi
grep -q "refusing a symlinked payload source" "${SCRATCH}/srclink.log" || fail "the symlinked source was not explained"
rm -f "${SRC}/gotham-toolctl"
pass "apply-release refuses a symlinked payload source"

echo "ALL HOST-BOOTSTRAP TESTS PASSED"
