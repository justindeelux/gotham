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
#   - the manifest is bound to the staged binary (its signed sha256 must match);
#   - a tampered payload (digest mismatch), a tampered manifest, a manifest whose
#     helper fields were added without re-signing, and a manifest without helper
#     fields (honest no-op) are handled without applying unverified bytes;
#   - anti-rollback: a lower signed version is refused *by the already-installed
#     wrapper before the fetched payload runs*, and the high-water mark holds; a
#     lower payload whose own hostbootstrap carries no check at all (a replayable
#     older release) is refused too; a tampered payload never blocks the update;
#   - TOCTOU guards: a symlinked or non-0700 staging directory is refused, and a
#     payload with an unsafe path, a symlink, a non-regular entry or a duplicate
#     entry is refused.
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
OLDER="v9.9.8-test"

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

# The release's binary artefact. The manifest binds its sha256, and the fetch
# requires the manifest to describe the staged binary before applying a payload.
BIN="${SCRATCH}/gotham-bin"
printf 'gotham-control-plane-%s\n' "${VERSION}" >"${BIN}"
OTHER_BIN="${SCRATCH}/other-bin"
printf 'a different binary\n' >"${OTHER_BIN}"

# make_payload builds a helper payload archive whose toolctl carries <marker>, so
# a refresh is visible as a content change. The optional third argument replaces
# the payload's own hostbootstrap (the replayable-payload replay test).
make_payload() { # $1 dir, $2 marker, [$3 hostbootstrap]
    dir="$1"; marker="$2"; boot="${3:-${SCRIPT_DIR}/gotham-hostbootstrap.sh}"
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
    cp "${boot}" "${dir}/gotham-hostbootstrap.sh"
    cp "${SCRIPT_DIR}/gotham-hostbootstrap-fetch.sh" "${dir}/gotham-hostbootstrap-fetch.sh"
    cp "${SCRIPT_DIR}/gotham-update.sh" "${dir}/gotham-update.sh"
    tar -czf "${dir}.tar.gz" -C "${dir}" \
        gotham-hostctl gotham-toolctl gotham-hostbootstrap.sh \
        gotham-hostbootstrap-fetch.sh gotham-update.sh
}

# publish signs a manifest (binding the binary digest and the payload archive)
# for <version> and places it in <release dir>.
publish() { # $1 release dir, $2 tarball, $3 version, [$4 in-file]
    dir="$1"; tarball="$2"; ver="$3"; in="${4:-${BIN}}"
    mkdir -p "${dir}"
    cp "${tarball}" "${dir}/gotham-hosthelpers.tar.gz"
    "${SCRATCH}/signer" manifest -key "${SCRATCH}/signing.key" -in "${in}" \
        -version "${ver}" -arch "${ARCH}" -channel stable \
        -helper "${dir}/gotham-hosthelpers.tar.gz" \
        -out "${dir}/gotham-manifest-${ARCH}.txt" >/dev/null
}

# publish_no_helper signs a manifest with no helper binding at all.
publish_no_helper() { # $1 release dir, $2 version
    dir="$1"; ver="$2"
    mkdir -p "${dir}"
    "${SCRATCH}/signer" manifest -key "${SCRATCH}/signing.key" -in "${BIN}" \
        -version "${ver}" -arch "${ARCH}" -channel stable \
        -out "${dir}/gotham-manifest-${ARCH}.txt" >/dev/null
}

# fetch_into runs the fetch script against a release base and staging dir, with
# the staged binary the manifest must describe.
fetch_into() { # $1 base, $2 staging, $3 version ; prints the payload dir
    sh "${FETCH}" "$1" "$3" "${ARCH}" "${KEY}" "$2" "${BIN}"
}

REL="${SCRATCH}/release"
GOOD_BASE="file://${REL}"

# ---- happy path: fetch + verify + extract ----------------------------------
make_payload "${SCRATCH}/p1" one
publish "${REL}/${VERSION}" "${SCRATCH}/p1.tar.gz" "${VERSION}"
STAGE="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
PAYLOAD="$(fetch_into "${GOOD_BASE}" "${STAGE}" "${VERSION}")"
[ -f "${PAYLOAD}/gotham-hostctl" ] || fail "the verified payload was not extracted"
grep -q "hostctl-one" "${PAYLOAD}/gotham-hostctl" || fail "the extracted payload has the wrong content"
[ "$(cat "${STAGE}/version")" = "${VERSION}" ] || fail "the signed version was not recorded"
pass "a signed payload is fetched, verified and extracted with its signed version"

# ---- the one-time bridge installs the trust anchor + fetch helper + wrapper --
ROOT="$(mktemp -d "${SCRATCH}/root.XXXXXX")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT}" sh "${BOOTSTRAP}" create alice "${VERSION}" >/dev/null
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
publish "${REL}/${VERSION}" "${SCRATCH}/p2.tar.gz" "${VERSION}"
STAGE2="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
# Use the installed fetch helper and key under the scratch root, as the wrapper
# does in production.
PAYLOAD2="$(sh "${INSTALLED_FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${INSTALLED_KEY}" "${STAGE2}" "${BIN}")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${ROOT}" sh "${BOOTSTRAP}" apply-release "${PAYLOAD2}" "${VERSION}" >/dev/null
grep -q "toolctl-two" "${ROOT}/usr/libexec/gotham/gotham-toolctl" || fail "the refresh did not upgrade the live toolctl"
grep -q "alice ALL=(root)" "${ROOT}/etc/sudoers.d/gotham-hostctl" || fail "the recorded drop-in user was lost by the refresh"
[ "$(cat "${ROOT}/usr/libexec/gotham/hostbootstrap/version")" = "${VERSION}" ] \
    || fail "the refresh did not record the high-water mark"
pass "the refresh upgrades the helper content and keeps the recorded drop-in"

# ---- tamper: payload digest mismatch is refused ----------------------------
make_payload "${SCRATCH}/p3" three
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz" "${VERSION}"
printf 'x' >>"${REL}/${VERSION}/gotham-hosthelpers.tar.gz"
STAGE3="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE3}" "${VERSION}" >"${SCRATCH}/tamper-payload.log" 2>&1; then
    fail "a tampered payload was accepted"
fi
grep -qi "digest mismatch" "${SCRATCH}/tamper-payload.log" || fail "the digest mismatch was not reported"
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz" "${VERSION}"
pass "a tampered payload is refused (digest mismatch)"

# ---- tamper: manifest signature failure is refused -------------------------
printf '\n# forged\n' >>"${REL}/${VERSION}/gotham-manifest-${ARCH}.txt"
STAGE4="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE4}" "${VERSION}" >"${SCRATCH}/tamper-manifest.log" 2>&1; then
    fail "a tampered manifest was accepted"
fi
grep -qi "signature verification FAILED" "${SCRATCH}/tamper-manifest.log" \
    || fail "the signature failure was not reported"
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz" "${VERSION}"
pass "a tampered manifest is refused (signature failure)"

# ---- helper fields added without re-signing invalidate the signature -------
publish_no_helper "${REL}/${VERSION}" "${VERSION}"
if command -v sha256sum >/dev/null 2>&1; then
    helper_sha="$(sha256sum "${SCRATCH}/p3.tar.gz" | awk '{print $1}')"
else
    helper_sha="$(shasum -a 256 "${SCRATCH}/p3.tar.gz" | awk '{print $1}')"
fi
{
    printf 'helper_file=gotham-hosthelpers.tar.gz\n'
    printf 'helper_sha256=%s\n' "${helper_sha}"
} >>"${REL}/${VERSION}/gotham-manifest-${ARCH}.txt"
STAGE9="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE9}" "${VERSION}" >"${SCRATCH}/added.log" 2>&1; then
    fail "a manifest with helper fields added without re-signing was accepted"
fi
grep -qi "signature verification FAILED" "${SCRATCH}/added.log" || fail "the added-field tamper was not reported"
pass "adding helper fields without re-signing invalidates the signature"

# ---- helper fields stripped: a valid manifest with no binding is a no-op ---
publish_no_helper "${REL}/${VERSION}" "${VERSION}"
STAGE10="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE10}" "${VERSION}" >"${SCRATCH}/stripped.log" 2>&1; then
    fail "a manifest with no helper binding was accepted as an apply"
fi
grep -qi "no host-helper payload binding" "${SCRATCH}/stripped.log" || fail "the missing binding was not reported"
pass "a manifest without a helper binding is an honest no-op (no unverified install)"

# ---- the manifest must describe the staged binary --------------------------
make_payload "${SCRATCH}/p5" five
publish "${REL}/${VERSION}" "${SCRATCH}/p5.tar.gz" "${VERSION}" "${OTHER_BIN}"
STAGE11="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE11}" "${VERSION}" >"${SCRATCH}/otherbin.log" 2>&1; then
    fail "a manifest for a different binary was accepted"
fi
grep -qi "does not describe the staged binary" "${SCRATCH}/otherbin.log" \
    || fail "the staged-binary mismatch was not reported"
publish "${REL}/${VERSION}" "${SCRATCH}/p3.tar.gz" "${VERSION}"
pass "a manifest that does not describe the staged binary is refused"

# ---- a wrong version is refused (no such manifest) -------------------------
STAGE5="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE5}" "v0.0.1" >"${SCRATCH}/wrongver.log" 2>&1; then
    fail "a missing version was accepted"
fi
grep -qi "download failed" "${SCRATCH}/wrongver.log" || fail "the missing manifest was not reported"
pass "a version with no manifest is refused"

# ---- TOCTOU: staging dir must be root-owned 0700 and not a symlink ----------
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${KEY}" "/nonexistent-staging" "${BIN}" \
    >"${SCRATCH}/nostage.log" 2>&1; then
    fail "a missing staging directory was accepted"
fi
grep -qi "does not exist" "${SCRATCH}/nostage.log" || fail "the missing staging dir was not reported"

WIDE="$(mktemp -d "${SCRATCH}/wide.XXXXXX")"
chmod 0755 "${WIDE}"
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${KEY}" "${WIDE}" "${BIN}" >"${SCRATCH}/wide.log" 2>&1; then
    fail "a 0755 staging directory was accepted"
fi
grep -qi "must be mode 0700" "${SCRATCH}/wide.log" || fail "the staging mode was not enforced"

LINK="$(mktemp -d "${SCRATCH}/linkdir.XXXXXX")"
ln -s "${LINK}" "${SCRATCH}/stage-link"
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${KEY}" "${SCRATCH}/stage-link" "${BIN}" \
    >"${SCRATCH}/link.log" 2>&1; then
    fail "a symlinked staging directory was accepted"
fi
grep -qi "symlinked staging" "${SCRATCH}/link.log" || fail "the symlinked staging dir was not refused"
pass "the staging directory must be a real, root-owned 0700 directory"

# ---- a missing key is refused ----------------------------------------------
STAGE6="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if sh "${FETCH}" "${GOOD_BASE}" "${VERSION}" "${ARCH}" "${SCRATCH}/no-such-key.pub" "${STAGE6}" "${BIN}" \
    >"${SCRATCH}/nokey.log" 2>&1; then
    fail "a fetch without a key was accepted"
fi
grep -qi "public key" "${SCRATCH}/nokey.log" || fail "the missing key was not reported"
pass "a missing release public key is refused"

# ---- archive hardening: unsafe path, symlink, non-regular, duplicate -------
# unsafe path: -P keeps the "../escape" member name tar would otherwise strip.
mkdir -p "${SCRATCH}/evil/inner"
printf 'x' >"${SCRATCH}/evil/escape"
printf '../escape\n' >"${SCRATCH}/evil/list"
tar -P -czf "${SCRATCH}/evil.tar.gz" -C "${SCRATCH}/evil/inner" -T "${SCRATCH}/evil/list"
publish "${REL}/${VERSION}" "${SCRATCH}/evil.tar.gz" "${VERSION}"
STAGE7="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE7}" "${VERSION}" >"${SCRATCH}/evil.log" 2>&1; then
    fail "a payload with an unsafe path was accepted"
fi
grep -qi "unsafe path" "${SCRATCH}/evil.log" || fail "the unsafe path was not reported"
pass "a payload with an unsafe path is refused"

# symlink entry
mkdir -p "${SCRATCH}/lnk"
printf '#!/bin/sh\n' >"${SCRATCH}/lnk/gotham-hostctl"
printf '#!/bin/sh\n' >"${SCRATCH}/lnk/gotham-toolctl"
printf '#!/bin/sh\n' >"${SCRATCH}/lnk/gotham-hostbootstrap.sh"
ln -s /etc/passwd "${SCRATCH}/lnk/gotham-hostbootstrap-fetch.sh"
tar -czf "${SCRATCH}/lnk.tar.gz" -C "${SCRATCH}/lnk" \
    gotham-hostctl gotham-toolctl gotham-hostbootstrap.sh gotham-hostbootstrap-fetch.sh
publish "${REL}/${VERSION}" "${SCRATCH}/lnk.tar.gz" "${VERSION}"
STAGE8="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE8}" "${VERSION}" >"${SCRATCH}/lnk.log" 2>&1; then
    fail "a payload containing a symlink was accepted"
fi
grep -qi "non-regular entry" "${SCRATCH}/lnk.log" || fail "the symlink entry was not reported"
pass "a payload with a symlink entry is refused"

# non-regular (directory) entry
mkdir -p "${SCRATCH}/dirp/sub"
printf '#!/bin/sh\n' >"${SCRATCH}/dirp/gotham-hostctl"
printf '#!/bin/sh\n' >"${SCRATCH}/dirp/gotham-toolctl"
printf '#!/bin/sh\n' >"${SCRATCH}/dirp/gotham-hostbootstrap.sh"
tar -czf "${SCRATCH}/dirp.tar.gz" -C "${SCRATCH}/dirp" \
    gotham-hostctl gotham-toolctl gotham-hostbootstrap.sh sub
publish "${REL}/${VERSION}" "${SCRATCH}/dirp.tar.gz" "${VERSION}"
STAGE12="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE12}" "${VERSION}" >"${SCRATCH}/dirp.log" 2>&1; then
    fail "a payload with a directory entry was accepted"
fi
grep -qi "non-regular entry" "${SCRATCH}/dirp.log" || fail "the directory entry was not reported"
pass "a payload with a non-regular entry is refused"

# duplicate entry
printf '#!/bin/sh\n' >"${SCRATCH}/dupsrc"
printf 'gotham-hostctl\ngotham-hostctl\ngotham-toolctl\ngotham-hostbootstrap.sh\n' >"${SCRATCH}/duplist"
mkdir -p "${SCRATCH}/dup"
printf '#!/bin/sh\n' >"${SCRATCH}/dup/gotham-hostctl"
printf '#!/bin/sh\n' >"${SCRATCH}/dup/gotham-toolctl"
printf '#!/bin/sh\n' >"${SCRATCH}/dup/gotham-hostbootstrap.sh"
tar -czf "${SCRATCH}/dup.tar.gz" -C "${SCRATCH}/dup" -T "${SCRATCH}/duplist"
publish "${REL}/${VERSION}" "${SCRATCH}/dup.tar.gz" "${VERSION}"
STAGE13="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
if fetch_into "${GOOD_BASE}" "${STAGE13}" "${VERSION}" >"${SCRATCH}/dup.log" 2>&1; then
    fail "a payload with a duplicate entry was accepted"
fi
grep -qi "duplicate entry" "${SCRATCH}/dup.log" || fail "the duplicate entry was not reported"
pass "a payload with a duplicate entry is refused"

# ---- end-to-end through the wrapper ----------------------------------------
# Run the real wrapper against a scratch root and a file:// release (with a stub
# service and health endpoint) to prove it wires fetch -> verify -> apply before
# the restart, that a tampered payload never blocks the update, and that a lower
# signed version is refused (anti-rollback).
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
GOTHAM_HOST_BOOTSTRAP_ROOT="${WROOT}" sh "${BOOTSTRAP}" create erin "${VERSION}" >/dev/null
cp "${KEY}" "${WROOT}/usr/libexec/gotham/gotham-signing-key.pub"
mkdir -p "${WROOT}/status"
: >"${WROOT}/update.lock"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
WCONF="${SCRATCH}/wrapper.conf"
cat >"${WCONF}" <<EOF
GOTHAM_BINARY=${BIN}
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
publish "${REL}/${VERSION}" "${SCRATCH}/p4.tar.gz" "${VERSION}"
run_wrapper >"${SCRATCH}/wrapper-ok.log" 2>&1 || fail "the wrapper exited nonzero: $(cat "${SCRATCH}/wrapper-ok.log")"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || { cat "${SCRATCH}/wrapper-ok.log" >&2; fail "the wrapper did not refresh the helper from the release"; }
grep -q "refreshed the host-helper payload" "${SCRATCH}/wrapper-ok.log" \
    || fail "the wrapper did not report the refresh"
grep -q '^result=ok' "${WROOT}/status/update.status" || fail "the wrapper did not record a healthy update"
pass "the wrapper fetches, verifies and applies the helper, then records ok"

# A lower signed version must be refused by the already-installed wrapper (which
# compares before it runs the payload) and must not change the helper.
make_payload "${SCRATCH}/p6" rolled-back
publish "${REL}/${OLDER}" "${SCRATCH}/p6.tar.gz" "${OLDER}"
printf 'version=%s\n' "${OLDER}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-rollback.log" 2>&1 || fail "the wrapper exited nonzero on a lower version"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "a lower signed version changed the helper (rollback)"
# The refusal is logged by the wrapper's own gate: the payload's stderr is
# discarded, so this line can only come from installed code.
grep -q "refusing to downgrade the host helper" "${SCRATCH}/wrapper-rollback.log" \
    || fail "the installed wrapper did not refuse the lower signed version"
grep -q '^result=ok' "${WROOT}/status/update.status" \
    || fail "a lower signed version blocked the update"
pass "the wrapper refuses a lower signed version (anti-rollback) without blocking the update"

# A tampered payload must not refresh the helper and must not block the update.
printf 'x' >>"${REL}/${VERSION}/gotham-hosthelpers.tar.gz"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-tamper.log" 2>&1 || fail "the wrapper exited nonzero on a tampered payload"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "a tampered payload still changed the helper"
grep -qi "digest mismatch" "${SCRATCH}/wrapper-tamper.log" \
    || fail "the tampered payload was not reported"
grep -q '^result=ok' "${WROOT}/status/update.status" \
    || fail "a tampered payload blocked the update"
pass "a tampered payload is refused without blocking the update"

# ---- replay of an older payload whose own script carries no check -----------
# The payload is the code the release ships, and an older release can lack the
# anti-rollback check entirely. The already-installed wrapper makes the decision
# before it runs the payload, so such a replay is still refused. The fixture is
# proved to be genuinely checkless first, so the refusal is not vacuous.
CHECKLESS_BOOT="${SCRATCH}/checkless-boot.sh"
sed '/# Anti-rollback: refuse a version below/,/^    esac$/d' \
    "${SCRIPT_DIR}/gotham-hostbootstrap.sh" >"${CHECKLESS_BOOT}"
grep -q "refusing to downgrade the host helper" "${CHECKLESS_BOOT}" \
    && fail "the checkless hostbootstrap fixture still contains the check"
sh -n "${CHECKLESS_BOOT}" || fail "the checkless hostbootstrap fixture is not valid shell"
make_payload "${SCRATCH}/p7" checkless "${CHECKLESS_BOOT}"
publish "${REL}/${OLDER}" "${SCRATCH}/p7.tar.gz" "${OLDER}"

# Non-vacuous: on a fresh host whose mark is below OLDER the checkless payload
# applies, proving it has no anti-rollback check of its own.
CHROOT="$(mktemp -d "${SCRATCH}/checkless-root.XXXXXX")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${CHROOT}" sh "${BOOTSTRAP}" create frank v9.9.0-test >/dev/null
CHSTAGE="$(mktemp -d "${SCRATCH}/stage.XXXXXX")"
CHPAYLOAD="$(sh "${CHROOT}/usr/libexec/gotham/gotham-hostbootstrap-fetch.sh" \
    "${GOOD_BASE}" "${OLDER}" "${ARCH}" "${KEY}" "${CHSTAGE}" "${BIN}")"
GOTHAM_HOST_BOOTSTRAP_ROOT="${CHROOT}" sh "${CHECKLESS_BOOT}" apply-release "${CHPAYLOAD}" "${OLDER}" >/dev/null \
    || fail "the checkless fixture did not apply on a lower-marked host (fixture is not checkless)"
grep -q "toolctl-checkless" "${CHROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "the checkless fixture did not install its payload"

# ... yet the installed wrapper refuses the same payload on the installed host.
printf 'version=%s\n' "${OLDER}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-replay.log" 2>&1 || fail "the wrapper exited nonzero on the checkless older payload"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "the checkless older payload changed the helper"
grep -q "refusing to downgrade the host helper" "${SCRATCH}/wrapper-replay.log" \
    || fail "the installed wrapper did not refuse the checkless older payload"
grep -q '^result=ok' "${WROOT}/status/update.status" \
    || fail "the replay refusal blocked the update"
pass "the installed wrapper refuses an older payload that carries no check of its own"

# ---- a missing high-water mark refuses the refresh -------------------------
# The wrapper's gate must fail closed when the mark is gone, not read it as
# "version 0" and apply whatever signed payload it fetched.
make_payload "${SCRATCH}/p8" nomark
publish "${REL}/${VERSION}" "${SCRATCH}/p8.tar.gz" "${VERSION}"
rm -f "${WROOT}/usr/libexec/gotham/hostbootstrap/version"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-nomark.log" 2>&1 || fail "the wrapper exited nonzero on a missing mark"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "a missing mark still applied the payload"
grep -q "no root-owned host-helper version mark" "${SCRATCH}/wrapper-nomark.log" \
    || fail "the installed wrapper did not refuse a missing mark"
grep -q '^result=ok' "${WROOT}/status/update.status" \
    || fail "the missing mark blocked the update"
pass "the installed wrapper refuses a missing high-water mark"

# ---- a malformed or empty mark refuses the refresh (not 0.0.0) --------------
# A garbage mark must fail closed: comparing it as 0.0.0 would accept any
# rollback, i.e. the missing-mark hole reopened by a malformed file.
printf 'dev\n' >"${WROOT}/usr/libexec/gotham/hostbootstrap/version"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-junkmark.log" 2>&1 || fail "the wrapper exited nonzero on a garbage mark"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "a garbage mark still applied the payload"
grep -q "not a valid release version" "${SCRATCH}/wrapper-junkmark.log" \
    || fail "the installed wrapper did not refuse a garbage mark with a clear reason"
grep -q '^result=ok' "${WROOT}/status/update.status" || fail "a garbage mark blocked the update"

: >"${WROOT}/usr/libexec/gotham/hostbootstrap/version"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-emptymark.log" 2>&1 || fail "the wrapper exited nonzero on an empty mark"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "an empty mark still applied the payload"
grep -q "empty host-helper version mark" "${SCRATCH}/wrapper-emptymark.log" \
    || fail "the installed wrapper did not refuse an empty mark"
grep -q '^result=ok' "${WROOT}/status/update.status" || fail "an empty mark blocked the update"

rm -f "${WROOT}/usr/libexec/gotham/hostbootstrap/version"
ln -s /etc/passwd "${WROOT}/usr/libexec/gotham/hostbootstrap/version"
printf 'version=%s\n' "${VERSION}" >"${WROOT}/update.pending"
run_wrapper >"${SCRATCH}/wrapper-linkmark.log" 2>&1 || fail "the wrapper exited nonzero on a symlinked mark"
grep -q "toolctl-wrapped" "${WROOT}/usr/libexec/gotham/gotham-toolctl" \
    || fail "a symlinked mark still applied the payload"
grep -q "no root-owned host-helper version mark" "${SCRATCH}/wrapper-linkmark.log" \
    || fail "the installed wrapper did not refuse a symlinked mark"
grep -q '^result=ok' "${WROOT}/status/update.status" || fail "a symlinked mark blocked the update"
pass "the installed wrapper refuses a malformed, empty or symlinked high-water mark"

echo "ALL HOST-HELPER REFRESH TESTS PASSED"
