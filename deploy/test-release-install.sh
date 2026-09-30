#!/bin/sh
#
# test-release-install.sh: local end-to-end dry run of the release install
# chain without GitHub or any host change.
#
# It builds the signer and a snapshot control-plane + agent binary, signs the
# per-arch manifests, serves the artifacts from a loopback file server, and runs
# deploy/install.sh against it with GOTHAM_INSTALL_ROOT so nothing outside the
# scratch directory is touched. It then proves the verification fails closed:
# a tampered artifact and a tampered manifest must both abort the install.
#
# Usage:
#   sh deploy/test-release-install.sh
#
# Requirements: go, openssl 3, curl, python3 (all present on the CI runner).

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_DIR}"

require() {
    command -v "$1" >/dev/null 2>&1 || { echo "test-release-install: '$1' is required" >&2; exit 1; }
}
require go
require openssl
require curl
require python3
require base64

SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/gotham-release-test.XXXXXX")"
SERVER_PID=""
sha() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}
cleanup() {
    [ -n "${SERVER_PID}" ] && kill "${SERVER_PID}" 2>/dev/null || true
    rm -rf "${SCRATCH}"
}
trap cleanup EXIT INT TERM

VERSION="v9.9.9-test"
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) echo "unsupported test arch ${ARCH}" >&2; exit 1 ;;
esac

SERVE="${SCRATCH}/serve"
ROOT="${SCRATCH}/root"
mkdir -p "${SERVE}"

echo "==> building signer and ${ARCH} snapshot binaries"
go build -o "${SCRATCH}/signer" ./cmd/signer
go build -o "${SERVE}/gotham-linux-${ARCH}" ./cmd/gotham
go build -o "${SERVE}/gotham-agent-linux-${ARCH}" ./cmd/gotham-agent

echo "==> generating an ephemeral test keypair"
"${SCRATCH}/signer" keygen -out "${SCRATCH}/signing.key" >/dev/null
PUB_B64="$(openssl pkey -pubin -in "${SCRATCH}/signing.key.pub" -outform DER | tail -c 32 | base64 | tr -d '\n')"

echo "==> signing manifests"
"${SCRATCH}/signer" manifest -key "${SCRATCH}/signing.key" -in "${SERVE}/gotham-linux-${ARCH}" \
    -version "${VERSION}" -arch "${ARCH}" -channel stable -out "${SERVE}/gotham-manifest-${ARCH}.txt" >/dev/null
"${SCRATCH}/signer" manifest -key "${SCRATCH}/signing.key" -in "${SERVE}/gotham-agent-linux-${ARCH}" \
    -version "${VERSION}" -arch "${ARCH}" -channel stable -out "${SERVE}/gotham-agent-manifest-${ARCH}.txt" >/dev/null

echo "==> serving ${SERVE} on 127.0.0.1"
PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
python3 -m http.server "${PORT}" --bind 127.0.0.1 --directory "${SERVE}" >/dev/null 2>&1 &
SERVER_PID=$!
i=0
while ! curl -fsS "http://127.0.0.1:${PORT}/gotham-linux-${ARCH}" -o /dev/null 2>/dev/null; do
    i=$((i + 1))
    [ "${i}" -gt 50 ] && { echo "file server did not start" >&2; exit 1; }
    sleep 0.1
done

run_install() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_UPDATE_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="${ROOT}" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${SCRIPT_DIR}/install.sh" "$@"
}

echo "==> install (happy path)"
run_install >/dev/null

# ---- Assertions -------------------------------------------------------------
INSTALL_PATH="${ROOT}/var/lib/gotham/bin/gotham"
[ -f "${INSTALL_PATH}" ] || { echo "FAIL: binary not installed" >&2; exit 1; }
[ -x "${INSTALL_PATH}" ] || { echo "FAIL: binary not executable" >&2; exit 1; }
served_sha="$(sha "${SERVE}/gotham-linux-${ARCH}")"
installed_sha="$(sha "${INSTALL_PATH}")"
[ "${served_sha}" = "${installed_sha}" ] || { echo "FAIL: installed binary differs from the verified artifact" >&2; exit 1; }

WRAPPER="${ROOT}/usr/libexec/gotham/gotham-update"
[ -x "${WRAPPER}" ] || { echo "FAIL: update wrapper not installed" >&2; exit 1; }
grep -q "GOTHAM_BINARY=${ROOT}/var/lib/gotham/bin/gotham" "${ROOT}/etc/gotham/updater.conf" \
    || { echo "FAIL: updater.conf not rendered with the install prefix" >&2; exit 1; }

UNIT="${ROOT}/etc/systemd/system/gotham.service"
grep -q "ExecStart=${ROOT}/var/lib/gotham/bin/gotham serve" "${UNIT}" \
    || { echo "FAIL: unit ExecStart not rendered" >&2; exit 1; }
grep -q "GOTHAM_UPDATE_SCRIPT=${ROOT}/usr/libexec/gotham/gotham-update" "${UNIT}" \
    || { echo "FAIL: unit update script path not rendered" >&2; exit 1; }

grep -q "^GOTHAM_DATABASE_DSN=" "${ROOT}/etc/gotham/gotham.env" \
    || { echo "FAIL: gotham.env not written" >&2; exit 1; }
echo "PASS: happy-path install verified and rendered"

# ---- Fail-closed: tampered artifact ----------------------------------------
cp "${SERVE}/gotham-linux-${ARCH}" "${SCRATCH}/good-artifact"
printf 'x' | dd of="${SERVE}/gotham-linux-${ARCH}" bs=1 seek=100 count=1 conv=notrunc 2>/dev/null
if run_install >"${SCRATCH}/tampered-artifact.log" 2>&1; then
    echo "FAIL: tampered artifact was installed" >&2
    exit 1
fi
grep -qi "digest mismatch" "${SCRATCH}/tampered-artifact.log" \
    || { echo "FAIL: tampered artifact failed for an unexpected reason" >&2; cat "${SCRATCH}/tampered-artifact.log" >&2; exit 1; }
cp "${SCRATCH}/good-artifact" "${SERVE}/gotham-linux-${ARCH}"
echo "PASS: tampered artifact rejected (digest mismatch)"

# ---- Fail-closed: tampered manifest (signature no longer valid) -------------
cp "${SERVE}/gotham-manifest-${ARCH}.txt" "${SCRATCH}/good-manifest"
sed 's/^version=.*/version=v0.0.0-forged/' "${SCRATCH}/good-manifest" >"${SERVE}/gotham-manifest-${ARCH}.txt"
if run_install >"${SCRATCH}/tampered-manifest.log" 2>&1; then
    echo "FAIL: tampered manifest was accepted" >&2
    exit 1
fi
grep -qi "signature verification FAILED" "${SCRATCH}/tampered-manifest.log" \
    || { echo "FAIL: tampered manifest failed for an unexpected reason" >&2; cat "${SCRATCH}/tampered-manifest.log" >&2; exit 1; }
cp "${SCRATCH}/good-manifest" "${SERVE}/gotham-manifest-${ARCH}.txt"
echo "PASS: tampered manifest rejected (signature failure)"

# ---- Dry-run does not touch the filesystem ---------------------------------
DRY_ROOT="${SCRATCH}/dry-root"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_UPDATE_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${DRY_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
    sh "${SCRIPT_DIR}/install.sh" --dry-run >/dev/null
[ ! -e "${DRY_ROOT}" ] || { echo "FAIL: --dry-run created files" >&2; exit 1; }
echo "PASS: --dry-run made no changes"

echo "ALL RELEASE-INSTALL TESTS PASSED"