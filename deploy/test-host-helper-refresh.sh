#!/bin/sh
#
# test-host-helper-refresh.sh: exercises the JUS-188 host-helper refresh chain on
# a fake root and a fake release directory. It never touches the real
# /usr/libexec or /etc and never reaches the network: the release is served from
# a file:// directory and the root-owned key is a scratch Ed25519 public key.
#
# Covered:
#   - the one-time bridge (`gotham-hostbootstrap create`) installs the root key,
#     the fetch helper and the wrapper on an otherwise bare host;
#   - a signed payload is fetched, verified and applied (helper content upgrades
#     through the refresh, not just through install.sh);
#   - a tampered payload (digest mismatch) and a tampered manifest (signature
#     failure) are both refused, and nothing is installed;
#   - a wrong version is refused;
#   - TOCTOU guards: a symlinked or non-0700 staging directory is refused, and a
#     payload with an unsafe path or a symlink entry is refused.
#
# Usage: sh deploy/test-host-helper-refresh.sh
# Requirements: go, openssl, curl, tar (present on the CI runner and a dev box).
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
FETCH="${SCRIPT_DIR}/gotham-hostbootstrap-fetch.sh"
BOOTSTRAP="${SCRIPT_DIR}/gotham-hostbootstrap.sh"
[ -f "${FETCH}" ] || { echo "FAIL: ${FETCH} is missing" >&2; exit 1; }

for tool in go openssl curl tar base64; do
    command -v "${tool}" >/dev/null 2>&1 || { echo "test-host-helper-refresh: '${tool}' is required" >&2; exit 1; }
done

SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/test-host-helper-refresh.XXXXXX")"
trap 'rm -rf "${SCRATCH}"' EXIT

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) echo "unsupported test arch ${ARCH}" >&2; exit 1 ;;
esac
VERSION="v9.9.9-test"

# Fake visudo so `create` (which validates the drop-in) is inert.
FAKE_BIN="${SCRATCH}/bin"
mkdir -p "${FAKE_BIN}"
cat >"${FAKE_BIN}/visudo" <<'SHIM'
#!/bin/sh
exit 0
SHIM
chmod 0755 "${FAKE_BIN}/visudo"
PATH="${FAKE_BIN}:${PATH}"
export PATH

echo "==> building the signer and an ephemeral keypair"
( cd "${REPO_DIR}" && go build -o "${SCRATCH}/signer" ./cmd/signer )
"${SCRATCH}/signer" keygen -out "${SCRATCH}/signing.key" >/dev/null
KEY="${SCRATCH}/signing.key.pub"

# make_payload builds a helper payload archive whose toolctl carries <marker>, so
# a refresh is visible as a content change.
make_payload() { # $1 dir, $2 marker
    dir="$1"; marker="$2"
    mkdir -p "${dir}"
    cat >"${dir}/gotham-hostctl" <<EOF
#!/bin/sh
echo hostctl-${marker}
EOF
    cat >"${dir}/gotham-toolctl" <<EOF
#!/bin/sh
echo toolctl-${marker}
EOF
    # The real installer/fetch/wrapper: the wrapper runs the payload's own
    # hostbootstrap, so this payload must be able to apply itself.
    cp "${SCRIPT_DIR}/gotham-hostbootstrap.sh" "${dir}/gotham-hostbootstrap.sh"
    cp "${SCRIPT_DIR}/gotham-hostbootstrap-fetch.sh" "${dir}/gotham-hostbootstrap-fetch.sh"
    cp "${SCRIPT_DIR}/gotham-update.sh" "${dir}/gotham-update.sh"
    tar -czf "${dir}.tar.gz" -C "${dir}" \
        gotham-hostctl gotham-toolctl gotham-hostbootstrap.sh \
        gotham-hostbootstrap-fetch.sh gotham-update.sh
}

# publish signs a manifest that binds the payload archive for ${VERSION}.
publish() { # $1 release dir, $2 tarball
    dir="$1"; tarball="$2"
    mkdir -p "${dir}"
    cp "${tarball}" "${dir}/gotham-hosthelpers.tar.gz"
    "${SCRATCH}/signer" manifest -key "${SCRATCH}/signing.key" -in "${dir}/gotham-hosthelpers.tar.gz" \
        -version "${VERSION}" -arch "${ARCH}" -channel stable \
        -helper "${dir}/gotham-hosthelpers.tar.gz" \
        -out "${dir}/gotham-manifest-${ARCH}.txt" >/dev/null
}

# fetch_into runs the fetch script against a release base and staging dir.
fetch_into() { # $1 base, $2 staging ; prints the payload dir
    sh "${FETCH}" "$1" "${VERSION}" "${ARCH}" "${KEY}" "$2"
}

REL="${SCRATCH}/release"
GOOD_BASE="file://${REL}"

# ---- happy path: fetch + verify + extract ----------------------------------
make_payload "${SCRATCH}/p1" one
publish "${REL}/${VERSION}" "${SCRATCH}/p1.tar.gz"
STAGE="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
PAYLOAD="$(fetch_into "${GOOD_BASE}" "${STAGE}")"
[ -f "${PAYLOAD}/gotham-hostctl" ] || fail "the verified payload was not extracted"
grep -q "hostctl-one" "${PAYLOAD}/gotham-hostctl" || fail "the extracted payload has the wrong content"
pass "a signed payload is fetched, verified and extracted into the staging dir"

# ---- the one-time bridge installs the trust anchor + fetch helper + wrapper --
ROOT="$(mktemp -d "${SCRATCH}/root.XXXXXX")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT}" sh "${BOOTSTRAP}" create alice >/dev/null
for f in usr/libexec/gotham/gotham-signing-key.pub \
    usr/libexec/gotham/gotham-hostbootstrap-fetch.sh \
    usr/libexec/gotham/gotham-update; do
    [ -f "${ROOT}/${f}" ] || fail "create did not install the bridge artifact ${f}"
done
# The installed fetch helper is the one under test, run with the scratch root.
INSTALLED_FETCH="${ROOT}/usr/libexec/gotham/gotham-hostbootstrap-fetch.sh"
cmp -s "${FETCH}" "${INSTALLED_FETCH}" || fail "the installed fetch helper differs from the source"
# Simulate the host's provisioned anchor: the ephemeral test key replaces the
# committed release key the checkout shipped.
INSTALLED_KEY="${ROOT}/usr/libexec/gotham/gotham-signing-key.pub"
cp "${KEY}" "${INSTALLED_KEY}"
pass "create installs the root key, the fetch helper and the wrapper (bridge)"

# ---- end-to-end: bridge -> refresh applies the released content -------------
make_payload "${SCRATCH}/p2" two
publish "${REL}/${VERSION}" "${SCRATCH}/p2.tar.gz"
STAGE2="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
# Use the installed fetch helper and key under the scratch root, as the wrapper
# does in production.
PAYLOAD2="$(sh "${INSTALLED_FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${INSTALLED_KEY}" "${STAGE2}")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT}" sh "${BOOTSTRAP}" apply-release "${PAYLOAD2}" >/dev/null
grep -q "toolctl-two" "${ROOT}/usr/libexec/gotham/gotham-toolctl" || fail "the refresh did not upgrade the live toolctl"
grep -q "alice ALL=(root)" "${ROOT}/etc/sudoers.d/gotham-hostctl" || fail "the pinned drop-in was lost by the refresh"
pass "the refresh upgrades the helper content and keeps the pinned drop-in"

# ---- tamper: payload digest mismatch is refused ----------------------------
make_payload "${SCRATCH}/p3" three
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz"
printf 'x' >>"${REL}/${VERSION}/gotham-hosthelpers.tar.gz"
STAGE3="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE3}" >"${SCRATCH}/tamper-payload.log" 2>&1; then
    fail "a tampered payload was accepted"
fi
grep -qi "digest mismatch" "${SCRATCH}/tamper-payload.log" || fail "the digest mismatch was not reported"
# restore
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz"
pass "a tampered payload is refused (digest mismatch)"

# ---- tamper: manifest signature failure is refused -------------------------
printf '\n# forged\n' >>"${REL}/${VERSION}/gotham-manifest-${ARCH}.txt"
STAGE4="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE4}" >"${SCRATCH}/tamper-manifest.log" 2>&1; then
    fail "a tampered manifest was accepted"
fi
grep -qi "signature verification FAILED" "${SCRATCH}/tamper-manifest.log" \
    || fail "the signature failure was not reported"
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz"
pass "a tampered manifest is refused (signature failure)"

# ---- a wrong version is refused (no such manifest) -------------------------
STAGE5="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if sh "${FETCH}" "${GOOD_BASE}" "v0.0.1" "${ARCH}" "${KEY}" "${STAGE5}" >"${SCRATCH}/wrongver.log" 2>&1; then
    fail "a missing version was accepted"
fi
grep -qi "download failed" "${SCRATCH}/wrongver.log" || fail "the missing manifest was not reported"
pass "a version with no manifest is refused"

# ---- TOCTOU: staging dir must be root-owned 0700 and not a symlink ----------
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${KEY}" "/nonexistent-staging" >"${SCRATCH}/nostage.log" 2>&1; then
    fail "a missing staging directory was accepted"
fi
grep -qi "does not exist" "${SCRATCH}/nostage.log" || fail "the missing staging dir was not reported"

WIDE="$(mktemp -d "${SCRATCH}/wide.XXXXXX")"
chmod 0755 "${WIDE}"
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${KEY}" "${WIDE}" >"${SCRATCH}/wide.log" 2>&1; then
    fail "a 0755 staging directory was accepted"
fi
grep -qi "must be mode 0700" "${SCRATCH}/wide.log" || fail "the staging mode was not enforced"

LINK="$(mktemp -d "${SCRATCH}/linkdir.XXXXXX")"
ln -s "${LINK}" "${SCRATCH}/stage-link"
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${KEY}" "${SCRATCH}/stage-link" >"${SCRATCH}/link.log" 2>&1; then
    fail "a symlinked staging directory was accepted"
fi
grep -qi "symlinked staging" "${SCRATCH}/link.log" || fail "the symlinked staging dir was not refused"
pass "the staging directory must be a real, root-owned 0700 directory"

# ---- a missing key is refused ----------------------------------------------
STAGE6="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${SCRATCH}/no-such-key.pub" "${STAGE6}" \
    >"${SCRATCH}/nokey.log" 2>&1; then
    fail "a fetch without a key was accepted"
fi
grep -qi "public key" "${SCRATCH}/nokey.log" || fail "the missing key was not reported"
pass "a missing release public key is refused"

# ---- a payload with an unsafe path is refused ------------------------------
# -P keeps the "../escape" member name GNU/BSD tar would otherwise strip.
mkdir -p "${SCRATCH}/evil/inner"
printf 'x' >"${SCRATCH}/evil/escape"
printf '../escape\n' >"${SCRATCH}/evil/list"
tar -P -czf "${SCRATCH}/evil.tar.gz" -C "${SCRATCH}/evil/inner" -T "${SCRATCH}/evil/list"
publish "${REL}/${VERSION}" "${SCRATCH}/evil.tar.gz"
STAGE7="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE7}" >"${SCRATCH}/evil.log" 2>&1; then
    fail "a payload with an unsafe path was accepted"
fi
grep -qi "unsafe path" "${SCRATCH}/evil.log" || fail "the unsafe path was not reported"
pass "a payload with an unsafe path is refused"

# ---- an archive with a symlink entry is refused ----------------------------
mkdir -p "${SCRATCH}/lnk"
printf '#!/bin/sh\n' >"${SCRATCH}/lnk/gotham-hostctl"
printf '#!/bin/sh\n' >"${SCRATCH}/lnk/gotham-toolctl"
printf '#!/bin/sh\n' >"${SCRATCH}/lnk/gotham-hostbootstrap.sh"
ln -s /etc/passwd "${SCRATCH}/lnk/gotham-hostbootstrap-fetch.sh"
tar -czf "${SCRATCH}/lnk.tar.gz" -C "${SCRATCH}/lnk" \
    gotham-hostctl gotham-toolctl gotham-hostbootstrap.sh gotham-hostbootstrap-fetch.sh
publish "${REL}/${VERSION}" "${SCRATCH}/lnk.tar.gz"
STAGE8="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE8}" >"${SCRATCH}/lnk.log" 2>&1; then
    fail "a payload containing a symlink was accepted"
fi
grep -qi "symlink" "${SCRATCH}/lnk.log" || fail "the symlink entry was not reported"
pass "a payload with a symlink entry is refused"

# ---- end-to-end through the wrapper ----------------------------------------
# Run the real wrapper against a scratch root and a file:// release (with a stub
# service and health endpoint) to prove it wires fetch -> verify -> apply before
# the restart, and that a tampered payload never blocks the update.
: >"${FAKE_BIN}/flock"
cat >"${FAKE_BIN}/flock" <<'SHIM'
#!/bin/sh
exit 0
SHIM
cat >"${FAKE_BIN}/systemctl" <<'SHIM'
#!/bin/sh
exit 0
SHIM
chmod 0755 "${FAKE_BIN}/flock" "${FAKE_BIN}/systemctl"

HEALTH_DIR="${SCRATCH}/health"
mkdir -p "${HEALTH_DIR}"
: >"${HEALTH_DIR}/healthz"
HPORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
python3 -m http.server "${HPORT}" --bind 127.0.0.1 --directory "${HEALTH_DIR}" >/dev/null 2>&1 &
HEALTH_PID=$!
trap 'kill "${HEALTH_PID}" 2>/dev/null; wait "${HEALTH_PID}" 2>/dev/null || true; rm -rf "${SCRATCH}"' EXIT
i=0
while ! curl -fsS "http://127.0.0.1:${HPORT}/healthz" -o /dev/null 2>/dev/null; do
    i=$((i + 1))
    [ "${i}" -gt 50 ] && fail "the health server did not start"
    sleep 0.1
done

WROOT="$(mktemp -d "${SCRATCH}/wroot.XXXXXX")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${WROOT}" sh "${BOOTSTRAP}" create erin >/dev/null
cp "${KEY}" "${WROOT}/usr/libexec/gotham/gotham-signing-key.pub"
mkdir -p "${WROOT}/bin" "${WROOT}/status"
: >"${WROOT}/bin/gotham"
: >"${WROOT}/update.lock"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
WCONF="${SCRATCH}/wrapper.conf"
cat >"${WCONF}" <<EOF
GOTHAM_BINARY=${WROOT}/bin/gotham
GOTHAM_SERVICE=gotham-test
GOTHAM_HEALTH=http://127.0.0.1:${HPORT}/healthz
GOTHAM_TIMEOUT=5
GOTHAM_STATUS=${WROOT}/status/update.status
GOTHAM_PENDING=${WROOT}/update.pending
GOTHAM_LOCK=${WROOT}/update.lock
GOTHAM_GRACE=0
GOTHAM_RELEASE_BASE=file://${REL}
GOTHAM_STAGING=${WROOT}
EOF

run_wrapper() {
    env -u SUDO_USER -u SUDO_UID -u SUDO_GID \
        GOTHAM_UPDATER_CONF="${WCONF}" \
        GOTHAM_HOST_BOOTSTRAP_ROOT="${WROOT}" \
        sh "${SCRIPT_DIR}/gotham-update.sh"
}

make_payload "${SCRATCH}/p4" wrapped
publish "${REL}/${VERSION}" "${SCRATCH}/p4.tar.gz"
run_wrapper >"${SCRATCH}/wrapper-ok.log" 2>&1 || fail "the wrapper exited nonzero: $(cat "${SCRATCH}/wrapper-ok.log")"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || { cat "${SCRATCH}/wrapper-ok.log" >&2; fail "the wrapper did not refresh the helper from the release"; }
grep -q "refreshed the host-helper payload" "${SCRATCH}/wrapper-ok.log" \
    || fail "the wrapper did not report the refresh"
grep -q '^result=ok' "${WROOT}/status/update.status" || fail "the wrapper did not record a healthy update"
pass "the wrapper fetches, verifies and applies the helper, then records ok"

# A tampered payload must not refresh the helper and must not block the update.
printf 'x' >>"${REL}/${VERSION}/gotham-hosthelpers.tar.gz"
: >"${WROOT}/update.pending"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-tamper.log" 2>&1 || fail "the wrapper exited nonzero on a tampered payload"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "a tampered payload still changed the helper"
grep -qi "digest mismatch" "${SCRATCH}/wrapper-tamper.log" \
    || fail "the tampered payload was not reported"
grep -q '^result=ok' "${WROOT}/status/update.status" \
    || fail "a tampered payload blocked the update"
pass "a tampered payload is refused without blocking the update"

echo "ALL HOST-HELPER REFRESH TESTS PASSED"
