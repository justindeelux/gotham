#!/bin/sh
#
# test-release-install.sh: local end-to-end dry run of the release install
# chain without GitHub or any host change.
#
# It builds the signer and a snapshot control-plane + agent binary, signs the
# per-arch manifests, serves the artifacts from a loopback fake releases server,
# and runs deploy/install.sh against it with GOTHAM_INSTALL_ROOT so nothing
# outside the scratch directory is touched. It covers:
#   - the pinned GOTHAM_BASE_URL + GOTHAM_VERSION path (happy path);
#   - the default no-GOTHAM_VERSION path, resolving the tag from the first
#     redirect of <releases>/latest (H2 regression guard);
#   - the gotham-agent family branch of verify_release;
#   - re-install preserving operator settings (M2);
#   - fail-closed: tampered artifact, tampered manifest, pinned-key mismatch;
#   - control characters in GOTHAM_DATABASE_DSN / GOTHAM_REDIS_ADDR fail
#     closed before gotham.env is written (C2);
#   - a real run (no --dry-run, no GOTHAM_INSTALL_ROOT) ignores
#     GOTHAM_INSTALL_TEST_AGENT_SCRIPT (R4).
#
# The installer's public key is the provisioned release key; the tests inject an
# ephemeral key through GOTHAM_INSTALL_TEST_PUBLIC_KEY (test mode only), and the
# pinned-key case runs without it.
#
# Usage:
#   sh deploy/test-release-install.sh
#
# Requirements: go, openssl 3, curl, python3 (all present on the CI runner).

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_DIR}"
# INSTALL_SH_OVERRIDE points the suite at a scratch copy of the installer for
# negative controls (mutate a copy, rerun, the new tests must catch it).
INSTALL_SH="${INSTALL_SH_OVERRIDE:-${SCRIPT_DIR}/install.sh}"

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

# Exercise the actual CLI container wiring over mTLS before the shell tail's
# command-binary seams can hide missing agent dialers.
echo "==> localhost agent wiring (mTLS)"
go test ./cmd/gotham -run '^TestInstanceContainerAgentWiringMTLS$' -count=1
echo "PASS: the domain CLI container service reaches the mTLS agent"

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

# Bind the standalone payload to the same signed manifest as the binary.
BUNDLE="gotham-deploy-${VERSION}.tar.gz"
tar -czf "${SERVE}/${BUNDLE}" -C "${SCRIPT_DIR}" \
    install.sh release-verify.sh install-agent-lib.sh gotham-update.sh \
    gotham-updater.conf install-sudoers.sh gotham.service \
    gotham-hostbootstrap.sh gotham-hostbootstrap-fetch.sh gotham-hostctl.sh \
    gotham-toolctl.sh gotham-signing-key.pub install-agent.sh \
    gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service \
    install-toolctl-sudoers.sh
printf 'deploy_file=%s\ndeploy_sha256=%s\n' "${BUNDLE}" "$(sha "${SERVE}/${BUNDLE}")" \
    >>"${SERVE}/gotham-manifest-${ARCH}.txt"
"${SCRATCH}/signer" sign -key "${SCRATCH}/signing.key" \
    -in "${SERVE}/gotham-manifest-${ARCH}.txt" >/dev/null

# Fake releases server: a static file server plus GitHub-style routes so the
# default "latest" resolution (a 302 from <releases>/latest) can be exercised.
cat >"${SCRATCH}/server.py" <<'PY'
import http.server, os, sys
root, port, version = sys.argv[1], int(sys.argv[2]), sys.argv[3]

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/releases/latest":
            self.send_response(302)
            self.send_header("Location", "/releases/tag/" + version)
            self.end_headers()
            return
        if path.startswith("/releases/download/"):
            path = "/" + path.split("/", 4)[4]
        elif path.startswith("/releases/tag/"):
            self.send_response(200)
            self.end_headers()
            return
        fp = os.path.join(root, path.lstrip("/"))
        try:
            with open(fp, "rb") as fh:
                data = fh.read()
        except OSError:
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY

echo "==> serving ${SERVE} on 127.0.0.1"
PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
python3 "${SCRATCH}/server.py" "${SERVE}" "${PORT}" "${VERSION}" >/dev/null 2>&1 &
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
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="${ROOT}" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${INSTALL_SH}" "$@"
}

# run_install_pinned uses the key embedded in install.sh (the provisioned release
# public key), with no override. The test manifests are signed by an ephemeral
# test key, so this must fail closed — proving the pinned anchor is what is
# actually used.
run_install_pinned() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_ROOT="${ROOT}" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${INSTALL_SH}" "$@"
}

# run_install_at installs into an arbitrary GOTHAM_INSTALL_ROOT (test mode).
run_install_at() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="$1" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${INSTALL_SH}"
}

# bake_installer builds a scratch copy of the installer with sandbox-only
# values baked in ($1 output file, $2 os-release fixture, $3 agent.env path).
# Production reads fixed paths on any real run, so the suite drives the agent
# step's platform/env inputs through a file copy, never through the
# environment. The copy is made from ${INSTALL_SH} so INSTALL_SH_OVERRIDE
# mutations compose with it.
bake_installer() {
    sed -e "s|^    _la_os_release=\"/etc/os-release\"|    _la_os_release=\"$2\"|" \
        -e "s|^    _la_agent_env=\"/etc/gotham/agent.env\"|    _la_agent_env=\"$3\"|" \
        "${INSTALL_SH}" >"$1"
    grep -qxF "    _la_os_release=\"$2\"" "$1" \
        || { echo "FAIL: bake_installer did not bake the os-release path" >&2; exit 1; }
    grep -qxF "    _la_agent_env=\"$3\"" "$1" \
        || { echo "FAIL: bake_installer did not bake the agent.env path" >&2; exit 1; }
    # The copy runs from its own directory (SCRIPT_DIR), so link the real
    # siblings next to it; the copy itself stays the file under test.
    for _sib in release-verify.sh gotham-update.sh gotham-updater.conf \
        install-sudoers.sh gotham.service install-agent.sh install-agent-lib.sh \
        gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service \
        gotham-toolctl.sh install-toolctl-sudoers.sh gotham-hostbootstrap.sh \
        gotham-hostbootstrap-fetch.sh gotham-signing-key.pub gotham-hostctl.sh; do
        ln -sf "${SCRIPT_DIR}/${_sib}" "$(dirname "$1")/${_sib}"
    done
}

echo "==> standalone install without a checkout"
mkdir "${SCRATCH}/standalone"
cp "${INSTALL_SH}" "${SCRATCH}/standalone/install.sh"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" GOTHAM_INSTALL_ROOT="${SCRATCH}/standalone-root" \
    sh "${SCRATCH}/standalone/install.sh" >"${SCRATCH}/standalone.log" 2>&1 \
    || { cat "${SCRATCH}/standalone.log" >&2; exit 1; }
[ -x "${SCRATCH}/standalone-root/var/lib/gotham/bin/gotham" ] \
    || { echo "FAIL: standalone binary not installed" >&2; exit 1; }
# Piped/stdin installs must never source planted siblings from the cwd.
mkdir "${SCRATCH}/planted"
printf '%s\n' 'echo UNVERIFIED-CWD-CODE >&2; exit 97' >"${SCRATCH}/planted/release-verify.sh"
cp "${SCRATCH}/planted/release-verify.sh" "${SCRATCH}/planted/install-agent-lib.sh"
for input_mode in redirect pipe; do
    if [ "${input_mode}" = redirect ]; then
        (
            cd "${SCRATCH}/planted"
            GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
            GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" GOTHAM_INSTALL_ROOT="${SCRATCH}/stdin-root" \
                sh <"${INSTALL_SH}"
        ) >"${SCRATCH}/stdin.log" 2>&1 \
            || { cat "${SCRATCH}/stdin.log" >&2; exit 1; }
    else
        cat "${INSTALL_SH}" | (
            cd "${SCRATCH}/planted"
            GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
            GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" GOTHAM_INSTALL_ROOT="${SCRATCH}/pipe-root" \
                sh -s -- --domain ' HTTPS://PANEL.EXAMPLE.COM/ '
        ) >"${SCRATCH}/pipe.log" 2>&1 \
            || { cat "${SCRATCH}/pipe.log" >&2; exit 1; }
    fi
done
[ -x "${SCRATCH}/stdin-root/var/lib/gotham/bin/gotham" ] && \
[ -x "${SCRATCH}/pipe-root/var/lib/gotham/bin/gotham" ] \
    || { echo "FAIL: piped/stdin install did not bootstrap" >&2; exit 1; }
if grep -q UNVERIFIED-CWD-CODE "${SCRATCH}/stdin.log" "${SCRATCH}/pipe.log"; then
    echo "FAIL: stdin install sourced unverified cwd code" >&2; exit 1
fi
echo "PASS: piped and redirected installs ignore planted cwd siblings"

# A well-formed signature from another key must also fail before extraction.
cp "${SERVE}/gotham-manifest-${ARCH}.txt.sig" "${SCRATCH}/good-signature"
"${SCRATCH}/signer" keygen -out "${SCRATCH}/wrong.key" >/dev/null
"${SCRATCH}/signer" sign -key "${SCRATCH}/wrong.key" \
    -in "${SERVE}/gotham-manifest-${ARCH}.txt" >/dev/null
if GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" GOTHAM_INSTALL_ROOT="${SCRATCH}/wrong-signature-root" \
    sh "${SCRATCH}/standalone/install.sh" >"${SCRATCH}/wrong-signature.log" 2>&1; then
    echo "FAIL: wrong signature accepted" >&2; exit 1
fi
grep -q 'manifest signature verification FAILED' "${SCRATCH}/wrong-signature.log"
[ ! -e "${SCRATCH}/wrong-signature-root" ] || { echo "FAIL: wrong signature mutated host" >&2; exit 1; }
cp "${SCRATCH}/good-signature" "${SERVE}/gotham-manifest-${ARCH}.txt.sig"
echo "PASS: standalone bootstrap rejects a valid-looking wrong signature"

cp "${SERVE}/${BUNDLE}" "${SCRATCH}/good-bundle"
printf tampered >>"${SERVE}/${BUNDLE}"
if GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" GOTHAM_INSTALL_ROOT="${SCRATCH}/tampered-root" \
    sh "${SCRATCH}/standalone/install.sh" >"${SCRATCH}/tampered.log" 2>&1; then
    echo "FAIL: tampered standalone bundle accepted" >&2; exit 1
fi
grep -q 'deploy bundle digest mismatch' "${SCRATCH}/tampered.log"
[ ! -e "${SCRATCH}/tampered-root" ] || { echo "FAIL: tampered bundle mutated host" >&2; exit 1; }
cp "${SCRATCH}/good-bundle" "${SERVE}/${BUNDLE}"
mv "${SERVE}/gotham-manifest-${ARCH}.txt.sig" "${SCRATCH}/bundle.sig"
if GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" GOTHAM_INSTALL_ROOT="${SCRATCH}/unsigned-root" \
    sh "${SCRATCH}/standalone/install.sh" >"${SCRATCH}/unsigned.log" 2>&1; then
    echo "FAIL: unsigned standalone bundle accepted" >&2; exit 1
fi
[ ! -e "${SCRATCH}/unsigned-root" ] || { echo "FAIL: unsigned bundle mutated host" >&2; exit 1; }
mv "${SCRATCH}/bundle.sig" "${SERVE}/gotham-manifest-${ARCH}.txt.sig"
echo "PASS: standalone install verifies the bundle before execution"

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

# N1 regression guard: shared directories must be world-traversable and the
# service user must be able to read the env/JWT files and stat/exec the wrapper.
# A global restrictive umask would make the directories 0700 and fail here.
mode_of() {
    if stat -c '%a' "$1" >/dev/null 2>&1; then
        stat -c '%a' "$1"
    else
        stat -f '%Lp' "$1"
    fi
}
for dir in "${ROOT}/etc/gotham" "${ROOT}/usr/libexec/gotham" "${ROOT}/var/lib/gotham" "${ROOT}/var/lib/gotham-updater"; do
    [ "$(mode_of "${dir}")" = "755" ] \
        || { echo "FAIL: ${dir} mode is $(mode_of "${dir}"), want 755" >&2; exit 1; }
done
for file in "${ROOT}/etc/gotham/gotham.env" "${ROOT}/etc/gotham/jwt_ed25519.key" "${ROOT}/etc/gotham/jwt_ed25519.pub"; do
    [ -r "${file}" ] || { echo "FAIL: ${file} is not readable" >&2; exit 1; }
done
[ -x "${WRAPPER}" ] && [ -r "${WRAPPER}" ] \
    || { echo "FAIL: wrapper ${WRAPPER} is not executable/readable" >&2; exit 1; }
[ "$(mode_of "${ROOT}/var/lib/gotham/bin/gotham")" = "755" ] \
    || { echo "FAIL: installed binary mode is $(mode_of "${ROOT}/var/lib/gotham/bin/gotham")" >&2; exit 1; }

# install-agent.sh needs root + systemd, so it is not executed here; guard its
# N1 fix statically: permissive base umask + explicit 0755 shared directories.
AGENT_INSTALLER="${SCRIPT_DIR}/install-agent.sh"
grep -q '^umask 022' "${AGENT_INSTALLER}" \
    || { echo "FAIL: install-agent.sh does not use a permissive base umask" >&2; exit 1; }
grep -q 'chmod 0755 /usr/libexec/gotham' "${AGENT_INSTALLER}" \
    || { echo "FAIL: install-agent.sh does not 0755 /usr/libexec/gotham" >&2; exit 1; }
grep -q 'chmod 0755 "${ENV_DIR}"' "${AGENT_INSTALLER}" \
    || { echo "FAIL: install-agent.sh does not 0755 /etc/gotham" >&2; exit 1; }

# B2 static guard: the sudoers installers create /etc/sudoers.d and validate the
# drop-in with visudo, and the installers check for sudo/visudo up front.
for installer in "${SCRIPT_DIR}/install-sudoers.sh" "${SCRIPT_DIR}/install-agent-sudoers.sh"; do
    grep -q 'install -d -m 0750 /etc/sudoers.d' "${installer}" \
        || { echo "FAIL: ${installer} does not create /etc/sudoers.d" >&2; exit 1; }
    grep -q 'mktemp /etc/sudoers.d' "${installer}" \
        || { echo "FAIL: ${installer} does not validate a temp copy first" >&2; exit 1; }
    grep -q 'mv -f "${SUDOERS_TMP}" "${SUDOERS_FILE}"' "${installer}" \
        || { echo "FAIL: ${installer} does not move the validated drop-in into place" >&2; exit 1; }
    grep -q '^visudo -cf' "${installer}" \
        || { echo "FAIL: ${installer} does not validate the drop-in with visudo" >&2; exit 1; }
done
for installer in "${INSTALL_SH}" "${AGENT_INSTALLER}"; do
    grep -q 'require_cmd visudo' "${installer}" \
        || { echo "FAIL: ${installer} does not require visudo" >&2; exit 1; }
done
echo "PASS: sudoers hardening present (B2)"
echo "PASS: happy-path install verified and rendered"

# ---- A1: control-plane CA provisioned + agent installer fails closed ---------
# The CP installer must provision the gRPC certificate authority so a fresh
# install serves TLS. In test mode `gotham ca init` runs the binary directly.
CA_DIR_TEST="${ROOT}/var/lib/gotham/ca"
for f in "${CA_DIR_TEST}/ca.crt" "${CA_DIR_TEST}/ca.key"; do
    [ -f "${f}" ] || { echo "FAIL: installer did not create ${f} (gRPC would be plaintext)" >&2; exit 1; }
    [ "$(mode_of "${f}")" = "600" ] \
        || { echo "FAIL: ${f} mode is $(mode_of "${f}"), want 600" >&2; exit 1; }
done
grep -q "^GOTHAM_CA_DIR=${CA_DIR_TEST}$" "${ROOT}/etc/gotham/gotham.env" \
    || { echo "FAIL: gotham.env GOTHAM_CA_DIR does not point at the provisioned CA" >&2; exit 1; }
echo "PASS: control-plane CA provisioned 0600 and wired via GOTHAM_CA_DIR (A1)"

# F1: --cp-host seeds the listener SAN hosts persisted next to the CA, so a
# remote agent dialing the control plane by name/IP verifies.
[ -s "${CA_DIR_TEST}/hosts" ] \
    || { echo "FAIL: installer did not seed a default SAN host list" >&2; exit 1; }
CP_ROOT="${SCRATCH}/root-cp-host"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${CP_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
    sh "${INSTALL_SH}" --cp-host cp.example.com --cp-host 192.0.2.10 >/dev/null
grep -qx 'cp.example.com' "${CP_ROOT}/var/lib/gotham/ca/hosts" \
    || { echo "FAIL: --cp-host cp.example.com was not persisted" >&2; cat "${CP_ROOT}/var/lib/gotham/ca/hosts" >&2; exit 1; }
grep -qx '192.0.2.10' "${CP_ROOT}/var/lib/gotham/ca/hosts" \
    || { echo "FAIL: --cp-host 192.0.2.10 was not persisted" >&2; exit 1; }
echo "PASS: --cp-host seeds the gRPC listener SAN hosts (F1)"

# The agent installer must refuse to run without a CA (fail closed), accept
# --insecure as the documented dev override, and accept --ca. --dry-run keeps
# all three side-effect free; GOTHAM_BASE_URL/GOTHAM_VERSION avoid the network.
agent_dry_run() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
        sh "${AGENT_INSTALLER}" "$@"
}
if agent_dry_run --dry-run >"${SCRATCH}/agent-no-ca.log" 2>&1; then
    echo "FAIL: install-agent.sh ran without a CA (agent channel would be plaintext)" >&2
    exit 1
fi
grep -q "no control-plane CA certificate configured" "${SCRATCH}/agent-no-ca.log" \
    || { echo "FAIL: install-agent.sh refused a missing CA for an unexpected reason" >&2; cat "${SCRATCH}/agent-no-ca.log" >&2; exit 1; }
agent_dry_run --insecure --dry-run >"${SCRATCH}/agent-insecure.log" 2>&1 \
    || { echo "FAIL: install-agent.sh --insecure --dry-run failed" >&2; cat "${SCRATCH}/agent-insecure.log" >&2; exit 1; }
printf 'dummy CA for the dry-run path\n' >"${SCRATCH}/ca-src.pem"
agent_dry_run --ca "${SCRATCH}/ca-src.pem" --dry-run >"${SCRATCH}/agent-ca.log" 2>&1 \
    || { echo "FAIL: install-agent.sh --ca --dry-run failed" >&2; cat "${SCRATCH}/agent-ca.log" >&2; exit 1; }
echo "PASS: agent installer fails closed without a CA and accepts --ca/--insecure (A1)"

# ---- JUS-5: localhost agent by default, --full opt-in -----------------------
# install.sh installs and starts the agent on the same host unless
# --no-local-agent is given; install-agent.sh only sets up Docker with --full.
if sh "${INSTALL_SH}" --help 2>&1 | grep -q -- '--no-local-agent'; then
    echo "FAIL: obsolete --no-local-agent still documented" >&2; exit 1
fi
sh "${AGENT_INSTALLER}" --help 2>&1 | grep -q -- '--full' \
    || { echo "FAIL: install-agent.sh --help does not document --full" >&2; exit 1; }
grep -q 'LOCAL_AGENT_RETRY=.*--full --ca' "${INSTALL_SH}" \
    || { echo "FAIL: install.sh does not build a --full --ca retry command for the localhost agent" >&2; exit 1; }
grep -q 'ensure_docker_full' "${AGENT_INSTALLER}" \
    || { echo "FAIL: install-agent.sh does not wire ensure_docker_full" >&2; exit 1; }
# Test mode must not touch the agent paths: there is no systemd/Docker there.
if [ -e "${ROOT}/var/lib/gotham-agent" ] || [ -e "${ROOT}/etc/gotham/agent.env" ]; then
    echo "FAIL: test-mode install.sh installed the localhost agent" >&2
    exit 1
fi
echo "PASS: localhost agent wiring present and test mode skips it (JUS-5)"

# ---- N1: installer scripts must be executable ---------------------------------
# The installers invoke the sudoers helpers (via `sh`, but the committed mode
# must still be +x) and the operator runs the installers and verification
# scripts directly. A missing bit makes the install die at its last step with
# "bad interpreter: Permission denied".
for script in install.sh install-agent.sh install-sudoers.sh install-agent-sudoers.sh \
    gotham-toolctl.sh install-toolctl-sudoers.sh gotham-hostbootstrap.sh \
    gotham-hostbootstrap-fetch.sh test-release-install.sh verify-systemd.sh verify-agent-update.sh; do
    [ -x "${SCRIPT_DIR}/${script}" ] \
        || { echo "FAIL: ${script} is not executable (chmod +x it)" >&2; exit 1; }
done
if git -C "${REPO_DIR}" rev-parse --git-dir >/dev/null 2>&1; then
    for script in install.sh install-agent.sh install-sudoers.sh install-agent-sudoers.sh \
        gotham-toolctl.sh install-toolctl-sudoers.sh gotham-hostbootstrap.sh \
        gotham-hostbootstrap-fetch.sh; do
        mode="$(git -C "${REPO_DIR}" ls-files -s -- "deploy/${script}" | awk '{print $1}')"
        [ "${mode}" = "100755" ] \
            || { echo "FAIL: deploy/${script} is committed mode ${mode}, want 100755" >&2; exit 1; }
    done
fi
echo "PASS: installer scripts are executable (N1)"

# ---- I4: no cleanup-only INT/TERM trap (a signal must abort) -----------------
# A `trap '…' EXIT INT TERM` cleans up but lets the shell carry on after a
# signal; both scripts must clean up on EXIT and exit on INT/TERM.
for installer in "${INSTALL_SH}" "${AGENT_INSTALLER}"; do
    if grep -qE "trap '.*' EXIT INT TERM" "${installer}"; then
        echo "FAIL: ${installer} has a cleanup-only EXIT/INT/TERM trap" >&2
        exit 1
    fi
    grep -q "trap 'exit 1' INT TERM" "${installer}" \
        || { echo "FAIL: ${installer} does not abort on INT/TERM" >&2; exit 1; }
done
echo "PASS: no cleanup-only signal trap (I4)"

# ---- B1: re-install with no operator additions succeeds ---------------------
# Regression: the preservation pipeline filtered every managed key and the
# gotham.env header, so on a host that never added operator settings the final
# `grep -v` matched nothing and exited 1. Under `set -e` that aborted the second
# install right after "writing /etc/gotham/gotham.env", with no error message.
echo "==> re-install with no operator additions (B1)"
B1_ROOT="${SCRATCH}/root-b1"
B1_ENV="${B1_ROOT}/etc/gotham/gotham.env"
run_install_at "${B1_ROOT}" >"${SCRATCH}/b1-first.log" 2>&1 \
    || { echo "FAIL: first install failed" >&2; cat "${SCRATCH}/b1-first.log" >&2; exit 1; }
run_install_at "${B1_ROOT}" >"${SCRATCH}/b1-second.log" 2>&1 \
    || { echo "FAIL: re-install with no operator additions failed (B1 regression)" >&2; cat "${SCRATCH}/b1-second.log" >&2; exit 1; }
grep -qx 'GOTHAM_DATABASE_DSN=postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable' "${B1_ENV}" \
    || { echo "FAIL: re-install did not refresh the managed DSN" >&2; exit 1; }
[ "$(grep -c '^# Gotham control-plane environment\. Read by gotham.service' "${B1_ENV}")" -eq 1 ] \
    || { echo "FAIL: re-install duplicated or dropped the gotham.env header" >&2; exit 1; }
[ "$(grep -c '^GOTHAM_DATABASE_DSN=' "${B1_ENV}")" -eq 1 ] \
    || { echo "FAIL: re-install duplicated a managed key" >&2; exit 1; }
echo "PASS: re-install with no operator additions succeeded (B1)"

# ---- I1: an indented managed key must not override the managed value --------
# systemd strips leading whitespace in EnvironmentFile, so a hand-indented
# managed key used to survive the filter and win. It must be filtered.
printf '  GOTHAM_REDIS_ADDR=attacker:6379\nKEEP_ME=1\n' >>"${B1_ENV}"
run_install_at "${B1_ROOT}" >"${SCRATCH}/i1.log" 2>&1 \
    || { echo "FAIL: re-install with an indented managed key failed" >&2; cat "${SCRATCH}/i1.log" >&2; exit 1; }
grep -qx 'GOTHAM_REDIS_ADDR=localhost:6379' "${B1_ENV}" \
    || { echo "FAIL: managed GOTHAM_REDIS_ADDR was not refreshed" >&2; exit 1; }
if grep -qE '^[[:space:]]+GOTHAM_REDIS_ADDR=' "${B1_ENV}"; then
    echo "FAIL: an indented managed key survived the filter (I1)" >&2
    exit 1
fi
[ "$(grep -cE '^[[:space:]]*GOTHAM_REDIS_ADDR=' "${B1_ENV}")" -eq 1 ] \
    || { echo "FAIL: managed GOTHAM_REDIS_ADDR is not unique" >&2; exit 1; }
grep -qx 'KEEP_ME=1' "${B1_ENV}" \
    || { echo "FAIL: operator key dropped alongside the indented managed key" >&2; exit 1; }
echo "PASS: indented managed keys filtered, operator keys kept (I1)"

# ---- L1/N2: a real filter failure aborts; empty match does not ---------------
# Only grep's "no lines matched" status (1) is tolerated. A genuine failure must
# abort loudly instead of silently dropping operator settings. The shim fails
# the **managed-key** pattern (the first stage of the old two-grep pipeline): a
# single grep makes the exact status visible, closing N2.
echo "==> preservation filter failure aborts (L1/N2)"
REAL_GREP="$(command -v grep)"
mkdir -p "${SCRATCH}/shim"
cat >"${SCRATCH}/shim/grep" <<GREP
#!/bin/sh
case "\$*" in
    *'GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH)='*) exit 2 ;;
esac
exec "${REAL_GREP}" "\$@"
GREP
chmod +x "${SCRATCH}/shim/grep"
SAVED_PATH="${PATH}"
PATH="${SCRATCH}/shim:${PATH}"
if run_install_at "${B1_ROOT}" >"${SCRATCH}/l1.log" 2>&1; then
    PATH="${SAVED_PATH}"
    echo "FAIL: a failing preservation filter did not abort the install (L1/N2)" >&2
    exit 1
fi
PATH="${SAVED_PATH}"
grep -q 'could not filter' "${SCRATCH}/l1.log" \
    || { echo "FAIL: filter failure aborted for an unexpected reason" >&2; cat "${SCRATCH}/l1.log" >&2; exit 1; }
# The abort must not have dropped the operator line from the existing file.
grep -qx 'KEEP_ME=1' "${B1_ENV}" \
    || { echo "FAIL: operator settings were dropped on abort (L1/N2)" >&2; exit 1; }
echo "PASS: a failing preservation filter aborts the install (L1/N2)"

# ---- I3: no secret-bearing temp env copy survives an abort -------------------
# The abort above happened after the root-only gotham.env.tmp.<pid> was written;
# the trap must have removed it.
if ls "${B1_ROOT}"/etc/gotham/gotham.env.tmp.* >/dev/null 2>&1; then
    echo "FAIL: a temp gotham.env survived the aborted install (I3)" >&2
    exit 1
fi
echo "PASS: no temp gotham.env survives an abort (I3)"

# ---- F1: a signal must abort, not rewrite gotham.env -------------------------
# A cleanup-only INT/TERM trap would resume the env subshell and `mv` a temp
# holding only the operator lines over gotham.env, dropping the managed keys and
# the secret (regression from the first I3 trap). The shim TERMs the env
# subshell during the filter and then runs the real grep, so the signal lands
# exactly in that window.
echo "==> a signal during the env write aborts without wiping gotham.env (F1)"
cp "${B1_ENV}" "${SCRATCH}/f1-env-before"
cat >"${SCRATCH}/shim/grep" <<GREP
#!/bin/sh
case "\$*" in
    *'GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH)='*)
        # TERM the env subshell (the grandparent of this grep, skipping the
        # command-substitution subshell), then run the real grep.
        gp="\$(ps -o ppid= -p "\$PPID" 2>/dev/null | tr -d ' ')"
        [ -n "\${gp}" ] && kill -TERM "\${gp}" 2>/dev/null
        ;;
esac
exec "${REAL_GREP}" "\$@"
GREP
chmod +x "${SCRATCH}/shim/grep"
SAVED_PATH="${PATH}"
PATH="${SCRATCH}/shim:${PATH}"
if run_install_at "${B1_ROOT}" >"${SCRATCH}/f1.log" 2>&1; then
    PATH="${SAVED_PATH}"
    echo "FAIL: a TERM during the env write did not abort the install (F1)" >&2
    exit 1
fi
PATH="${SAVED_PATH}"
cmp -s "${SCRATCH}/f1-env-before" "${B1_ENV}" \
    || { echo "FAIL: the aborted install rewrote gotham.env (F1)" >&2; exit 1; }
if ls "${B1_ROOT}"/etc/gotham/gotham.env.tmp.* >/dev/null 2>&1; then
    echo "FAIL: a temp gotham.env survived the TERM abort (F1)" >&2
    exit 1
fi
echo "PASS: a signal during the env write aborts without wiping gotham.env (F1)"

# ---- M2: re-install preserves operator settings (b) and managed DSN (c) ------
ENV_FILE="${ROOT}/etc/gotham/gotham.env"
SECRET_BEFORE="$(sed -n 's/^GOTHAM_SECRET_KEY=//p' "${ENV_FILE}" | head -n1)"
sed 's#^GOTHAM_DATABASE_DSN=.*#GOTHAM_DATABASE_DSN=postgres://managed/db#' "${ENV_FILE}" >"${ENV_FILE}.edit"
printf '\nAUTO_UPDATE=true\nPLATFORM_ADMINS=ops@example.com\n' >>"${ENV_FILE}.edit"
mv "${ENV_FILE}.edit" "${ENV_FILE}"
run_install >/dev/null
grep -qx 'GOTHAM_DATABASE_DSN=postgres://managed/db' "${ENV_FILE}" \
    || { echo "FAIL: re-install overwrote the managed DSN with the local default" >&2; exit 1; }
grep -qx 'AUTO_UPDATE=true' "${ENV_FILE}" \
    || { echo "FAIL: re-install dropped the operator AUTO_UPDATE key" >&2; exit 1; }
grep -qx 'PLATFORM_ADMINS=ops@example.com' "${ENV_FILE}" \
    || { echo "FAIL: re-install dropped the operator PLATFORM_ADMINS key" >&2; exit 1; }
SECRET_AFTER="$(sed -n 's/^GOTHAM_SECRET_KEY=//p' "${ENV_FILE}" | head -n1)"
[ "${SECRET_BEFORE}" = "${SECRET_AFTER}" ] \
    || { echo "FAIL: re-install rotated GOTHAM_SECRET_KEY" >&2; exit 1; }
echo "PASS: re-install preserved the DSN, the secret and operator keys"
# restore the default DSN for the following cases
sed 's#^GOTHAM_DATABASE_DSN=.*#GOTHAM_DATABASE_DSN=postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable#' \
    "${ENV_FILE}" >"${ENV_FILE}.edit"
mv "${ENV_FILE}.edit" "${ENV_FILE}"

# ---- H2: default path resolves the tag from <releases>/latest ---------------
echo "==> install (default latest resolution, no GOTHAM_VERSION)"
LATEST_ROOT="${SCRATCH}/root-latest"
GOTHAM_RELEASES_URL="http://127.0.0.1:${PORT}/releases" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${LATEST_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
    sh "${INSTALL_SH}" >/dev/null
LATEST_BIN="${LATEST_ROOT}/var/lib/gotham/bin/gotham"
[ -x "${LATEST_BIN}" ] || { echo "FAIL: default latest-path install did not install the binary" >&2; exit 1; }
[ "$(sha "${LATEST_BIN}")" = "$(sha "${SERVE}/gotham-linux-${ARCH}")" ] \
    || { echo "FAIL: latest-path binary differs from the served artifact" >&2; exit 1; }
echo "PASS: default latest-tag resolution installs the verified binary"

# ---- L6: the agent family branch of verify_release -------------------------
echo "==> verify_release (gotham-agent family)"
# shellcheck source=deploy/release-verify.sh
. "${SCRIPT_DIR}/release-verify.sh"
verify_release "http://127.0.0.1:${PORT}" "${VERSION}" "${ARCH}" gotham-agent \
    "${SCRATCH}/signing.key.pub" "${SCRATCH}/agent-verified" >/dev/null
[ "$(sha "${SCRATCH}/agent-verified")" = "$(sha "${SERVE}/gotham-agent-linux-${ARCH}")" ] \
    || { echo "FAIL: agent-family verification produced the wrong binary" >&2; exit 1; }
echo "PASS: agent-family verify_release downloaded and verified the agent binary"

# ---- I5: verifier cleanup on a signal, caller traps chained/restored --------
# release-verify.sh installs EXIT/INT/TERM/HUP handlers only for the duration
# of verify_release: it captures and chains the caller's handlers, removes the
# scratch dir on a signal, aborts nonzero and restores the caller's handlers on
# success. A slow curl shim lets the signal land mid-download.
echo "==> verify_release cleans up and chains the caller's traps on a signal (I5)"
I5_DIR="${SCRATCH}/i5"
I5_TMP="${I5_DIR}/tmp"
I5_MARKS="${I5_DIR}/marks"
mkdir -p "${I5_TMP}" "${I5_MARKS}" "${I5_DIR}/shim"
REAL_CURL="$(command -v curl)"
cat >"${I5_DIR}/shim/curl" <<SHIM
#!/bin/sh
sleep 1
exec "${REAL_CURL}" "\$@"
SHIM
chmod +x "${I5_DIR}/shim/curl"
cat >"${I5_DIR}/run.sh" <<RUN
#!/bin/sh
set -eu
. "${SCRIPT_DIR}/release-verify.sh"
trap 'echo prior-exit >>"${I5_MARKS}/exit"' EXIT
trap 'echo prior-term >>"${I5_MARKS}/term"; exit 19' TERM
verify_release "http://127.0.0.1:${PORT}" "${VERSION}" "${ARCH}" "" \\
    "${SCRATCH}/signing.key.pub" "${I5_DIR}/binary"
echo "verify_release unexpectedly completed" >&2
exit 1
RUN
PATH="${I5_DIR}/shim:${PATH}" TMPDIR="${I5_TMP}" sh "${I5_DIR}/run.sh" \
    >"${I5_DIR}/run.log" 2>&1 &
I5_PID=$!
# Wait until the verifier has installed its traps (the lists writes .traps),
# then let it settle into the slow first download.
i=0
while [ "${i}" -lt 50 ]; do
    set -- "${I5_TMP}"/gotham-verify.*/.traps
    [ -f "$1" ] && break
    i=$((i + 1))
    sleep 0.1
done
sleep 0.1
kill -TERM "${I5_PID}" 2>/dev/null || true
I5_RC=0
wait "${I5_PID}" || I5_RC=$?
[ "${I5_RC}" -eq 19 ] \
    || { echo "FAIL: a signal during verify_release exited ${I5_RC}, want the chained handler's 19 (I5)" >&2; cat "${I5_DIR}/run.log" >&2; exit 1; }
[ ! -e "${I5_DIR}/binary" ] \
    || { echo "FAIL: a signal still installed the verified binary (I5)" >&2; exit 1; }
[ "$(cat "${I5_MARKS}/term" 2>/dev/null)" = "prior-term" ] \
    || { echo "FAIL: the caller's TERM handler was not chained (I5)" >&2; exit 1; }
[ "$(cat "${I5_MARKS}/exit" 2>/dev/null)" = "prior-exit" ] \
    || { echo "FAIL: the caller's EXIT handler was not chained (I5)" >&2; exit 1; }
if ls "${I5_TMP}"/gotham-verify.* >/dev/null 2>&1; then
    echo "FAIL: the verifier scratch dir survived the signal (I5)" >&2
    exit 1
fi
echo "PASS: signal cleanup chained the caller's traps and removed the scratch dir (I5)"

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

# ---- Fail-closed: pinned-key mismatch --------------------------------------
# A validly test-signed manifest verified against the provisioned key embedded
# in install.sh must be rejected: the pinned anchor is authoritative.
if run_install_pinned >"${SCRATCH}/pinned-mismatch.log" 2>&1; then
    echo "FAIL: a manifest signed by a different key was accepted under the pinned key" >&2
    exit 1
fi
grep -qi "signature verification FAILED" "${SCRATCH}/pinned-mismatch.log" \
    || { echo "FAIL: pinned-key mismatch failed for an unexpected reason" >&2; cat "${SCRATCH}/pinned-mismatch.log" >&2; exit 1; }
echo "PASS: pinned-key mismatch rejected (signature failure)"

# ---- Dry-run does not touch the filesystem ---------------------------------
DRY_ROOT="${SCRATCH}/dry-root"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${DRY_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
    sh "${INSTALL_SH}" --dry-run >/dev/null
[ ! -e "${DRY_ROOT}" ] || { echo "FAIL: --dry-run created files" >&2; exit 1; }
echo "PASS: --dry-run made no changes"

# ---- JUS-5: dry-run shows the localhost agent step and its opt-out ----------
# Without GOTHAM_INSTALL_ROOT (real paths, but --dry-run creates nothing) the
# run reaches the localhost agent step past service activation. The platform
# pre-check needs a supported fixture plus a systemctl on PATH (this suite
# also runs on macOS), so both are shimmed here and in the R1/R2 cases below.
FIXTURES="${SCRATCH}/fixtures"
SYS_SHIM="${SCRATCH}/sys-shim"
mkdir -p "${FIXTURES}" "${SYS_SHIM}"
printf 'ID=ubuntu\nVERSION_CODENAME=jammy\n' >"${FIXTURES}/ubuntu-release"
printf 'ID=arch\nVERSION_CODENAME=n/a\n' >"${FIXTURES}/arch-release"
printf '#!/bin/sh\nexit 0\n' >"${SYS_SHIM}/systemctl"
chmod +x "${SYS_SHIM}/systemctl"
dry_run() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
    PATH="${SYS_SHIM}:${PATH}" \
        sh "${INSTALL_SH}" --dry-run "$@"
}
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/dry-agent.log" 2>&1
grep -q 'install-agent.sh --full --ca' "${SCRATCH}/dry-agent.log" \
    || { echo "FAIL: --dry-run does not show the localhost agent install" >&2; cat "${SCRATCH}/dry-agent.log" >&2; exit 1; }
if dry_run --no-local-agent >"${SCRATCH}/dry-no-agent.log" 2>&1; then
    echo "FAIL: --no-local-agent was accepted" >&2; exit 1
fi
GOTHAM_NO_LOCAL_AGENT=1 dry_run >"${SCRATCH}/env-no-agent.log" 2>&1
grep -q 'GOTHAM_AGENT_NODE_ID=localhost' "${SCRATCH}/env-no-agent.log" \
    || { echo "FAIL: legacy env skipped mandatory localhost agent" >&2; exit 1; }
echo "PASS: localhost agent is mandatory"

# ---- R1: unsupported platform skips the agent; a failed agent step keeps CP -
# An unsupported platform (non-Ubuntu/Debian here) skips the localhost agent
# with a notice instead of failing the install, and schedules no agent step.
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/dry-skip-arch.log" 2>&1 && { echo "FAIL: unsupported platform succeeded" >&2; exit 1; }
grep -q 'mandatory localhost agent' "${SCRATCH}/dry-skip-arch.log" \
    || { echo "FAIL: unsupported platform did not skip the localhost agent (R1)" >&2; cat "${SCRATCH}/dry-skip-arch.log" >&2; exit 1; }
grep -q 'supports Ubuntu/Debian only' "${SCRATCH}/dry-skip-arch.log" \
    || { echo "FAIL: the skip notice does not name the supported distros (R1)" >&2; exit 1; }
if grep -q 'install-agent.sh --full' "${SCRATCH}/dry-skip-arch.log"; then
    echo "FAIL: unsupported platform still schedules the agent install (R1)" >&2
    exit 1
fi
echo "PASS: unsupported platform refuses before mutation (R1)"
# An unsupported architecture skips the same way (seam: GOTHAM_TEST_UNAME_M).
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
GOTHAM_TEST_UNAME_M=sparc64 \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/dry-skip-cpu.log" 2>&1 && { echo "FAIL: unsupported architecture succeeded" >&2; exit 1; }
grep -q 'mandatory localhost agent: unsupported architecture' "${SCRATCH}/dry-skip-cpu.log" \
    || { echo "FAIL: unsupported architecture did not skip the localhost agent (R1)" >&2; exit 1; }
if grep -q 'install-agent.sh --full' "${SCRATCH}/dry-skip-cpu.log"; then
    echo "FAIL: unsupported architecture still schedules the agent install (R1)" >&2
    exit 1
fi
echo "PASS: unsupported architecture refuses before mutation (R1)"
# A failed agent step must never roll the control plane back: the old
# abort-the-install message is gone, replaced by a warning that leaves the
# control plane installed and running plus the exact retry command.
if grep -q 'localhost agent install failed (re-run with --no-local-agent' "${INSTALL_SH}"; then
    echo "FAIL: install.sh still aborts the install when the localhost agent fails (R1)" >&2
    exit 1
fi
grep -q 'Retry only the agent step' "${INSTALL_SH}" \
    || { echo "FAIL: install.sh has no agent retry message (R1)" >&2; exit 1; }
grep -q 'LOCAL_AGENT_FAILED' "${INSTALL_SH}" \
    || { echo "FAIL: install.sh does not record the agent failure for a late exit (R1)" >&2; exit 1; }
grep -q 'LOCAL_AGENT_RETRY=.*--full --ca' "${INSTALL_SH}" \
    || { echo "FAIL: install.sh retry does not cover the --full --ca agent step (R1)" >&2; exit 1; }
echo "PASS: a failed localhost agent step keeps the control plane and prints the retry (R1)"

# ---- Mandatory localhost defaults and safe refusal of remote agent config ---
R2_ENV="${SCRATCH}/prior-agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442\nGOTHAM_AGENT_NODE_ID=old-host-agent\n' >"${R2_ENV}"
GOTHAM_AGENT_ENV_FILE="${R2_ENV}" dry_run >"${SCRATCH}/localhost-upgrade.log" 2>&1
grep -q 'GOTHAM_AGENT_NODE_ID=localhost GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442' "${SCRATCH}/localhost-upgrade.log" \
    || { echo "FAIL: upgrade does not install mandatory identity/address" >&2; exit 1; }
printf 'GOTHAM_AGENT_CP_ADDR=remote.example.com:9443\nGOTHAM_AGENT_NODE_ID=remote-node\n' >"${R2_ENV}"
if GOTHAM_AGENT_ENV_FILE="${R2_ENV}" dry_run >"${SCRATCH}/remote-refusal.log" 2>&1; then
    echo "FAIL: remote agent setup silently succeeded" >&2; exit 1
fi
grep -q 'leaving it untouched' "${SCRATCH}/remote-refusal.log" \
    || { echo "FAIL: remote refusal lacks explanation" >&2; exit 1; }
for override in GOTHAM_AGENT_NODE_ID=other GOTHAM_AGENT_CP_ADDR=remote:9442; do
    if env "${override}" GOTHAM_INSTALL_TEST=1 GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
        PATH="${SYS_SHIM}:${PATH}" sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/override.log" 2>&1; then
        echo "FAIL: mandatory local defaults could be overridden" >&2; exit 1
    fi
done
echo "PASS: mandatory defaults on upgrade and remote configuration refusal"

# ---- agent.env default parity: install.sh reads what install-agent.sh writes
# The lookup default must stay the path install-agent.sh actually writes
# (its ENV_FILE), or kept values silently stop matching.
grep -q 'ENV_FILE="${ENV_DIR}/agent.env"' "${AGENT_INSTALLER}" \
    || { echo "FAIL: install-agent.sh no longer writes \${ENV_DIR}/agent.env" >&2; exit 1; }
grep -q '_la_agent_env="/etc/gotham/agent.env"' "${INSTALL_SH}" \
    || { echo "FAIL: install.sh lookup default is not /etc/gotham/agent.env" >&2; exit 1; }
echo "PASS: the agent.env lookup default matches the path install-agent.sh writes"

# ---- H1/M1/L3: production dry-run ignores every test seam (hermetic) ------
# Differential: the same production-mode dry-run (no GOTHAM_INSTALL_TEST, no
# GOTHAM_INSTALL_ROOT, so test mode is OFF) with every test-only seam set
# hostile must print byte-identical output to the clean run. Both runs read
# the same host files, so the comparison is hermetic: it cannot fail
# spuriously on a developer box (L3), and it runs on macOS (no Linux-only
# host state needed). Covers GOTHAM_OS_RELEASE_FILE, GOTHAM_TEST_UNAME_M,
# GOTHAM_AGENT_ENV_FILE, GOTHAM_APT_ROOT, GOTHAM_INSTALL_TEST_AGENT_SCRIPT
# and GOTHAM_INSTALL_TEST_RUN_AGENT on the install.sh path, plus the
# install-agent.sh --full dry path.
unset GOTHAM_INSTALL_TEST GOTHAM_INSTALL_ROOT GOTHAM_OS_RELEASE_FILE \
    GOTHAM_TEST_UNAME_M GOTHAM_AGENT_ENV_FILE GOTHAM_APT_ROOT \
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT GOTHAM_INSTALL_TEST_RUN_AGENT
prod_dry() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    PATH="${SYS_SHIM}:${PATH}" \
        sh "${INSTALL_SH}" --dry-run
}
prod_dry >"${SCRATCH}/prod-clean.log" 2>&1
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
GOTHAM_TEST_UNAME_M=sparc64 \
GOTHAM_AGENT_ENV_FILE="${R2_ENV}" \
GOTHAM_APT_ROOT="${SCRATCH}/hostile-apt-root" \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/hostile-agent.sh" \
GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
GOTHAM_INSTALL_TEST_ADMIN_BIN="${SCRATCH}/hostile-admin.sh" \
GOTHAM_INSTALL_TEST_ADMIN_TTY="${SCRATCH}/hostile-tty" \
prod_dry >"${SCRATCH}/prod-hostile.log" 2>&1
# Normalize the random mktemp suffix (gotham-install.XXXXXX differs per run;
# it is not seam input) before comparing.
sed 's/gotham-install\.[A-Za-z0-9][A-Za-z0-9]*/gotham-install.RANDOM/g' \
    "${SCRATCH}/prod-clean.log" >"${SCRATCH}/prod-clean.norm"
sed 's/gotham-install\.[A-Za-z0-9][A-Za-z0-9]*/gotham-install.RANDOM/g' \
    "${SCRATCH}/prod-hostile.log" >"${SCRATCH}/prod-hostile.norm"
cmp -s "${SCRATCH}/prod-clean.norm" "${SCRATCH}/prod-hostile.norm" \
    || { echo "FAIL: test seams changed a production run (H1/M1)" >&2; diff "${SCRATCH}/prod-clean.norm" "${SCRATCH}/prod-hostile.norm" >&2; exit 1; }
if grep -q 'hostile-agent' "${SCRATCH}/prod-hostile.log"; then
    echo "FAIL: GOTHAM_INSTALL_TEST_AGENT_SCRIPT leaked into a production run (M1)" >&2
    exit 1
fi
echo "PASS: a production run ignores every test seam (H1/M1, hermetic)"
unset GOTHAM_OS_RELEASE_FILE GOTHAM_TEST_UNAME_M GOTHAM_AGENT_ENV_FILE \
    GOTHAM_APT_ROOT GOTHAM_INSTALL_TEST_AGENT_SCRIPT GOTHAM_INSTALL_TEST_RUN_AGENT \
    GOTHAM_INSTALL_TEST_ADMIN_BIN GOTHAM_INSTALL_TEST_ADMIN_TTY
# Gating, proved behaviourally rather than by grepping the source: the
# hermetic differential above shows every seam ignored outside test mode, and
# the refusal / no-RUN_AGENT / scrub-dump runs show the script seam needs a
# sandbox + RUN_AGENT + an explicit fake, the child runs scrubbed, and the
# path seams are dry-run-only.
echo "PASS: seam gating proved behaviourally (differential + refusal + scrub dump, M1)"
# install-agent.sh --full dry path: hostile seams change nothing either.
printf 'dummy CA for the dry-run path\n' >"${SCRATCH}/prod-ca.pem"
agent_prod_dry() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
        sh "${AGENT_INSTALLER}" --full --ca "${SCRATCH}/prod-ca.pem" --dry-run
}
agent_prod_dry >"${SCRATCH}/aprod-clean.log" 2>&1
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
GOTHAM_APT_ROOT="${SCRATCH}/hostile-apt-root" \
_OS_RELEASE_FILE="${SCRATCH}/hostile-apt-root" \
_APT_ROOT="${SCRATCH}/hostile-apt-root" \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/hostile-agent.sh" \
agent_prod_dry >"${SCRATCH}/aprod-hostile.log" 2>&1
unset GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT _OS_RELEASE_FILE _APT_ROOT \
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT
sed 's/gotham-agent-install\.[A-Za-z0-9][A-Za-z0-9]*/gotham-agent-install.RANDOM/g' \
    "${SCRATCH}/aprod-clean.log" >"${SCRATCH}/aprod-clean.norm"
sed 's/gotham-agent-install\.[A-Za-z0-9][A-Za-z0-9]*/gotham-agent-install.RANDOM/g' \
    "${SCRATCH}/aprod-hostile.log" >"${SCRATCH}/aprod-hostile.norm"
cmp -s "${SCRATCH}/aprod-clean.norm" "${SCRATCH}/aprod-hostile.norm" \
    || { echo "FAIL: test seams changed install-agent.sh --full --dry-run (H1)" >&2; exit 1; }
grep -q 'ensure Docker Engine and the compose plugin (--full)' "${SCRATCH}/aprod-hostile.log" \
    || { echo "FAIL: install-agent.sh --full --dry-run lost its Docker step (H1)" >&2; exit 1; }
echo "PASS: install-agent.sh --full --dry-run ignores hostile seams (H1)"
# GOTHAM_INSTALL_ROOT=/ is the real root, not a sandbox: refuse before any
# network or filesystem change (safe: dies during prefix validation).
if GOTHAM_INSTALL_ROOT=/ \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/root-slash.log" 2>&1; then
    echo "FAIL: GOTHAM_INSTALL_ROOT=/ was accepted as a sandbox (M1)" >&2
    exit 1
fi
grep -q 'non-/' "${SCRATCH}/root-slash.log" \
    || { echo "FAIL: GOTHAM_INSTALL_ROOT=/ refused for an unexpected reason (M1)" >&2; cat "${SCRATCH}/root-slash.log" >&2; exit 1; }
echo "PASS: GOTHAM_INSTALL_ROOT=/ is refused, never treated as test mode (M1)"

# ---- R3: a failing localhost agent install keeps CP, warns, exits nonzero --
# Test mode runs past service activation with a logging systemctl shim and
# executes a failing fake agent installer through the test-only seam, so the
# real failure path runs: the retry command lands on stderr, the exit is
# nonzero, the control plane is fully installed and its service was enabled
# (never disabled/stopped after the failure).
echo "==> failing localhost agent install keeps the control plane (R3)"
R3_ROOT="${SCRATCH}/root-r3"
R3_SHIM="${SCRATCH}/r3-shim"
R3_AGENT_ENV="${SCRATCH}/r3-agent.env"
mkdir -p "${R3_SHIM}"
cat >"${R3_SHIM}/systemctl" <<'SHIM'
#!/bin/sh
echo "systemctl $*" >>"${SYSTEMCTL_LOG:?}"
if [ "$1" = restart ] && [ "$2" = gotham.service ]; then
    token="${GOTHAM_INSTALL_ROOT}/var/lib/gotham/ca/enrollment-token"
    mkdir -p "$(dirname "${token}")"
    if [ ! -s "${token}" ]; then
        if [ "${ENROLLMENT_TEST_DELAY:-0}" = 1 ]; then
            (sleep 1; umask 077; printf '%s\n' enrollment-fixture-secret >"${token}") &
        else
            (umask 077; printf '%s\n' enrollment-fixture-secret >"${token}")
        fi
    fi
fi
exit 0
SHIM
chmod +x "${R3_SHIM}/systemctl"
cat >"${SCRATCH}/fake-agent.sh" <<'SHIM'
#!/bin/sh
echo "fake-agent-installer: refusing on purpose" >&2
touch "${AGENT_MARKER:?}"
exit 1
SHIM
chmod +x "${SCRATCH}/fake-agent.sh"
rm -f "${R3_AGENT_ENV}"
# The platform/env inputs for this real sandbox run are baked into a scratch
# installer copy: production reads fixed paths outside --dry-run, so the
# suite drives them through a file copy, never through the environment.
R3_INSTALLER="${SCRATCH}/install-r3.sh"
bake_installer "${R3_INSTALLER}" "${FIXTURES}/ubuntu-release" "${R3_AGENT_ENV}"
: >"${SCRATCH}/r3-systemctl.log"
R3_RC=0
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${R3_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/fake-agent.sh" \
SYSTEMCTL_LOG="${SCRATCH}/r3-systemctl.log" \
AGENT_MARKER="${SCRATCH}/r3-agent-ran" \
PATH="${R3_SHIM}:${PATH}" \
    sh "${R3_INSTALLER}" >"${SCRATCH}/r3-out.log" 2>"${SCRATCH}/r3-err.log" || R3_RC=$?
[ "${R3_RC}" -ne 0 ] \
    || { echo "FAIL: the install exited 0 although the agent step failed (R3)" >&2; exit 1; }
[ -f "${SCRATCH}/r3-agent-ran" ] \
    || { echo "FAIL: the failing agent installer never ran (R3)" >&2; exit 1; }
grep -q 'Retry only the agent step' "${SCRATCH}/r3-err.log" \
    || { echo "FAIL: the retry block is not on stderr (R3)" >&2; cat "${SCRATCH}/r3-err.log" >&2; exit 1; }
grep -q 'install-agent.sh --full --ca' "${SCRATCH}/r3-err.log" \
    || { echo "FAIL: stderr names no --full --ca retry command (R3)" >&2; exit 1; }
grep -q 'Nothing was rolled back' "${SCRATCH}/r3-err.log" \
    || { echo "FAIL: stderr does not say the control plane was kept (R3)" >&2; exit 1; }
[ -x "${R3_ROOT}/var/lib/gotham/bin/gotham" ] \
    || { echo "FAIL: the control plane binary is missing after the agent failure (R3)" >&2; exit 1; }
[ -f "${R3_ROOT}/etc/gotham/gotham.env" ] \
    || { echo "FAIL: gotham.env is missing after the agent failure (R3)" >&2; exit 1; }
[ -f "${R3_ROOT}/var/lib/gotham/ca/ca.crt" ] \
    || { echo "FAIL: the CA is missing after the agent failure (R3)" >&2; exit 1; }
[ -f "${R3_ROOT}/etc/systemd/system/gotham.service" ] \
    || { echo "FAIL: the unit is missing after the agent failure (R3)" >&2; exit 1; }
grep -q 'systemctl enable gotham.service' "${SCRATCH}/r3-systemctl.log" \
    || { echo "FAIL: the control plane service was not enabled before the agent failure (R3)" >&2; exit 1; }
# JUS-38: a reinstall must restart the unit onto the new binary (enable --now
# alone leaves a pre-upgrade process running the old version).
grep -q 'systemctl restart gotham.service' "${SCRATCH}/r3-systemctl.log" \
    || { echo "FAIL: the control plane service was not restarted onto the new binary (R3/JUS-38)" >&2; exit 1; }
if grep -qE 'systemctl (disable|stop) ' "${SCRATCH}/r3-systemctl.log"; then
    echo "FAIL: the control plane service was touched after the agent failure (R3)" >&2
    exit 1
fi
echo "PASS: a failing agent install keeps the control plane, warns on stderr and exits nonzero (R3)"

# ---- Sandbox RUN_AGENT without the script seam is refused -----------------
# Without an explicit fake the sandbox run would execute the real
# install-agent.sh, so the installer dies before the agent step (after the
# control plane itself is installed). Behavioural proof of the gate the old
# static greps stood in for.
echo "==> sandbox RUN_AGENT without a script seam is refused"
REF_ROOT="${SCRATCH}/root-refuse"
REF_INSTALLER="${SCRATCH}/install-refuse.sh"
bake_installer "${REF_INSTALLER}" "${FIXTURES}/ubuntu-release" "${SCRATCH}/refuse-agent.env"
: >"${SCRATCH}/refuse-systemctl.log"
REF_RC=0
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${REF_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
SYSTEMCTL_LOG="${SCRATCH}/refuse-systemctl.log" \
PATH="${R3_SHIM}:${PATH}" \
    sh "${REF_INSTALLER}" >"${SCRATCH}/refuse-out.log" 2>"${SCRATCH}/refuse-err.log" || REF_RC=$?
[ "${REF_RC}" -ne 0 ] \
    || { echo "FAIL: sandbox RUN_AGENT without a script seam was not refused" >&2; exit 1; }
grep -q 'requires GOTHAM_INSTALL_TEST_AGENT_SCRIPT' "${SCRATCH}/refuse-err.log" \
    || { echo "FAIL: the refusal does not name the missing script seam" >&2; cat "${SCRATCH}/refuse-err.log" >&2; exit 1; }
if grep -q 'installing the localhost agent node' "${SCRATCH}/refuse-out.log"; then
    echo "FAIL: the refused run reached the agent step" >&2
    exit 1
fi
[ -x "${REF_ROOT}/var/lib/gotham/bin/gotham" ] \
    || { echo "FAIL: the refused run did not install the control plane first" >&2; exit 1; }
echo "PASS: sandbox RUN_AGENT without a script seam is refused before the agent step"

# ---- Sandbox script seam without RUN_AGENT never runs ----------------------
# The fake runs only past the test-mode gate: with RUN_AGENT unset the
# install exits after the service files and the fake never executes.
echo "==> sandbox script seam without RUN_AGENT never runs"
NORUN_ROOT="${SCRATCH}/root-norun"
NORUN_MARKER="${SCRATCH}/norun-agent-ran"
cat >"${SCRATCH}/norun-agent.sh" <<'SHIM'
#!/bin/sh
touch "${AGENT_MARKER:?}"
exit 1
SHIM
chmod +x "${SCRATCH}/norun-agent.sh"
rm -f "${NORUN_MARKER}"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${NORUN_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/norun-agent.sh" \
AGENT_MARKER="${NORUN_MARKER}" \
PATH="${R3_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" >"${SCRATCH}/norun-out.log" 2>&1 \
    || { echo "FAIL: the no-RUN_AGENT sandbox run failed" >&2; exit 1; }
grep -q 'test mode: skipping service activation' "${SCRATCH}/norun-out.log" \
    || { echo "FAIL: the no-RUN_AGENT sandbox run did not exit at the test-mode gate" >&2; exit 1; }
[ ! -e "${NORUN_MARKER}" ] \
    || { echo "FAIL: the fake agent ran without RUN_AGENT" >&2; exit 1; }
echo "PASS: sandbox script seam without RUN_AGENT never runs"

# ---- The agent child runs scrubbed; path seams stay ignored on real runs ---
# The fake dumps its environment: none of the scrubbed test seams may reach
# it. The run also exports hostile path seams (os-release, arch, agent.env)
# plus the flag without --dry-run; the baked ubuntu fixture still wins and
# the step runs, proving those seams are dry-run-only.
echo "==> the agent child sees no test seam (scrubbed)"
DUMP_ROOT="${SCRATCH}/root-dump"
DUMP_INSTALLER="${SCRATCH}/install-dump.sh"
DUMP_ENV="${SCRATCH}/dump-agent.env"
DUMP_CAPTURE="${SCRATCH}/dump-child-env"
bake_installer "${DUMP_INSTALLER}" "${FIXTURES}/ubuntu-release" "${DUMP_ENV}"
cat >"${SCRATCH}/dump-agent.sh" <<'SHIM'
#!/bin/sh
env | sort >"${ENV_DUMP:?}"
exit 0
SHIM
chmod +x "${SCRATCH}/dump-agent.sh"
rm -f "${DUMP_CAPTURE}"
: >"${SCRATCH}/dump-systemctl.log"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${DUMP_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/dump-agent.sh" \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
GOTHAM_TEST_UNAME_M=sparc64 \
GOTHAM_AGENT_ENV_FILE="${R2_ENV}" \
GOTHAM_APT_ROOT="${SCRATCH}/hostile-apt-root" \
GOTHAM_INSTALL_TEST=1 \
SYSTEMCTL_LOG="${SCRATCH}/dump-systemctl.log" \
ENV_DUMP="${DUMP_CAPTURE}" \
PATH="${R3_SHIM}:${PATH}" \
    sh "${DUMP_INSTALLER}" >"${SCRATCH}/dump-out.log" 2>"${SCRATCH}/dump-err.log" \
    || { echo "FAIL: the scrub-probe install failed although the fixtures are baked" >&2; cat "${SCRATCH}/dump-err.log" >&2; exit 1; }
[ -f "${DUMP_CAPTURE}" ] \
    || { echo "FAIL: the dumping agent step never ran" >&2; exit 1; }
grep -qx "GOTHAM_VERSION=${VERSION}" "${DUMP_CAPTURE}" \
    || { echo "FAIL: the dump missed the positive control (GOTHAM_VERSION)" >&2; exit 1; }
for _seam in GOTHAM_OS_RELEASE_FILE GOTHAM_APT_ROOT GOTHAM_AGENT_ENV_FILE \
    GOTHAM_TEST_UNAME_M GOTHAM_INSTALL_TEST GOTHAM_INSTALL_TEST_RUN_AGENT \
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT GOTHAM_INSTALL_TEST_PUBLIC_KEY \
    GOTHAM_INSTALL_TEST_ADMIN_BIN GOTHAM_INSTALL_TEST_ADMIN_TTY; do
    if grep -q "^${_seam}=" "${DUMP_CAPTURE}"; then
        echo "FAIL: the agent child saw ${_seam} (scrub bypassed)" >&2
        exit 1
    fi
done
if grep -q 'hostile-apt-root\|sparc64\|arch-release' "${DUMP_CAPTURE}"; then
    echo "FAIL: a hostile seam value leaked into the agent child" >&2
    exit 1
fi
echo "PASS: the agent child runs scrubbed and hostile path seams change nothing on a real run"

# ---- C2: bad input fails before the first mutation, nothing created --------
# The DSN and the Redis address are validated unconditionally, before the
# service user, directories, binary or any env file is created. A newline
# smuggling a second line into root-owned gotham.env fails closed the same
# way. Sandbox real runs (no --dry-run) prove the write path itself: a fresh
# prefix must hold no etc/, no var/ (hence no binary) and no env files.
# (No user is created in a sandbox; the container re-proof asserts that.)
# Agent values are validated only when the agent step will run, so their
# cases below run as --dry-run installs against the Ubuntu fixture (plus a
# systemctl shim for macOS): the step would run there, and a bad value must
# fail before anything is printed for creation. A skipped agent step is never
# blocked by agent values — proven by the skip cases after this block.
echo "==> bad input fails before the first mutation with nothing created (C2)"
_c2_must_reject() { # $1 case name, $2 key, $3 value
    C2_ROOT="${SCRATCH}/root-c2-$1"
    rm -rf "${C2_ROOT}"
    C2_RC=0
    env "$2=$3" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="${C2_ROOT}" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${INSTALL_SH}" >"${SCRATCH}/c2-$1-out.log" 2>"${SCRATCH}/c2-$1-err.log" || C2_RC=$?
    [ "${C2_RC}" -ne 0 ] \
        || { echo "FAIL: $2='$3' was accepted (C2/$1)" >&2; exit 1; }
    grep -q "$2" "${SCRATCH}/c2-$1-err.log" \
        || { echo "FAIL: the $2 rejection does not name the key (C2/$1)" >&2; exit 1; }
    for _c2path in etc var; do
        [ ! -e "${C2_ROOT}/${_c2path}" ] \
            || { echo "FAIL: ${C2_ROOT}/${_c2path} was created although $2 was rejected (C2/$1)" >&2; exit 1; }
    done
    [ ! -f "${C2_ROOT}/etc/gotham/gotham.env" ] \
        || { echo "FAIL: gotham.env was written although $2 was rejected (C2/$1)" >&2; exit 1; }
}
_c2_must_reject dsn GOTHAM_DATABASE_DSN "$(printf 'postgres://gotham:gotham@localhost:5432/gotham\nGOTHAM_EVIL=true')"
_c2_must_reject redis GOTHAM_REDIS_ADDR "$(printf 'localhost:6379\nGOTHAM_EVIL=true')"
# A trailing backslash would join the gotham.env line with the next one under
# systemd continuation, and quotes would not survive the service's read of the
# file: both fail closed with nothing created, naming the key.
_c2_must_reject dsn-quote GOTHAM_DATABASE_DSN 'postgres://gotham:gotha"m@localhost:5432/gotham?sslmode=disable'
_c2_must_reject dsn-backslash GOTHAM_DATABASE_DSN "$(printf "postgres://gotham@localhost:5432/gotham\\")"
_c2_must_reject redis-quote GOTHAM_REDIS_ADDR 'rediss://redis.internal:6380"'
_c2_must_reject redis-backslash GOTHAM_REDIS_ADDR "$(printf "localhost:6379\\")"
_c2_agent_must_reject() { # $1 case name, $2 key, $3 value
    C2_ROOT="${SCRATCH}/root-c2-$1"
    rm -rf "${C2_ROOT}"
    C2_RC=0
    env "$2=$3" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="${C2_ROOT}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
    PATH="${SYS_SHIM}:${PATH}" \
        sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/c2-$1-out.log" 2>"${SCRATCH}/c2-$1-err.log" || C2_RC=$?
    [ "${C2_RC}" -ne 0 ] \
        || { echo "FAIL: $2='$3' was accepted although the agent step would run (C2/$1)" >&2; exit 1; }
    grep -q "$2" "${SCRATCH}/c2-$1-err.log" \
        || { echo "FAIL: the $2 rejection does not name the key (C2/$1)" >&2; exit 1; }
    for _c2path in etc var; do
        [ ! -e "${C2_ROOT}/${_c2path}" ] \
            || { echo "FAIL: ${C2_ROOT}/${_c2path} was created although $2 was rejected (C2/$1)" >&2; exit 1; }
    done
}
_c2_agent_must_reject node-space GOTHAM_AGENT_NODE_ID 'bad id'
_c2_agent_must_reject node-star GOTHAM_AGENT_NODE_ID 'a*b'
_c2_agent_must_reject node-slash GOTHAM_AGENT_NODE_ID 'a/b'
_c2_agent_must_reject node-squote GOTHAM_AGENT_NODE_ID "a'b"
_c2_agent_must_reject certdir-rel GOTHAM_AGENT_CERT_DIR 'relative/certs'
_c2_agent_must_reject certdir-space GOTHAM_AGENT_CERT_DIR '/opt/my dir/certs'
_c2_agent_must_reject key-rel GOTHAM_AGENT_KEY 'relative/key.pem'
_c2_agent_must_reject sock-rel GOTHAM_AGENT_DOCKER_SOCK 'unix://relative.sock'
_c2_agent_must_reject sock-noport GOTHAM_AGENT_DOCKER_SOCK 'tcp://docker'
_c2_agent_must_reject level GOTHAM_AGENT_LOG_LEVEL 'verbose'
_c2_agent_must_reject autoupdate GOTHAM_AGENT_AUTO_UPDATE 'yes'
_c2_agent_must_reject interval GOTHAM_AGENT_UPDATE_INTERVAL '5'
_c2_agent_must_reject interval-day GOTHAM_AGENT_UPDATE_INTERVAL '1d'
_c2_agent_must_reject interval-huge GOTHAM_AGENT_UPDATE_INTERVAL '99999999999h'
_c2_agent_must_reject channel GOTHAM_AGENT_UPDATE_CHANNEL 'a/b'
_c2_agent_must_reject cpaddr GOTHAM_AGENT_CP_ADDR 'not-a-host-port'
_c2_agent_must_reject cpaddr-noport GOTHAM_AGENT_CP_ADDR '127.0.0.1'
_c2_agent_must_reject listen GOTHAM_AGENT_LISTEN_ADDR '9443'
_c2_agent_must_reject listen-space GOTHAM_AGENT_LISTEN_ADDR '0.0.0.0: 9443'
_c2_agent_must_reject listen-dquote GOTHAM_AGENT_LISTEN_ADDR ':94"43'
_c2_agent_must_reject listen-eq GOTHAM_AGENT_LISTEN_ADDR ':94=43'
echo "PASS: bad DSN / Redis / agent values fail closed with nothing created (C2)"

# ---- Unsupported platforms fail before any mutation ------------------------
if GOTHAM_TEST_UNAME_M=riscv64 dry_run >"${SCRATCH}/arch-refuse.log" 2>&1; then
    echo "FAIL: unsupported architecture succeeded without local agent" >&2; exit 1
fi
grep -q 'unsupported architecture' "${SCRATCH}/arch-refuse.log" \
    || { echo "FAIL: missing unsupported architecture reason" >&2; exit 1; }

# ---- C2c: DSN/Redis shapes the installer passes through --------------------
# The DSN/Redis gate is control characters only by design: the installer
# passes managed values through untouched (a managed DSN skips local
# provisioning; shape validation belongs to the server, which already runs).
# These cases pin that an @-bearing, a %-escaped and a rediss:// value are
# accepted by name, written verbatim, and (rediss aside, which only the
# server interprets) reach a green sandbox install.
echo "==> DSN/Redis pass-through shapes are accepted by name (C2c)"
_c2c_must_accept() { # $1 case name, $2 key, $3 value
    C2C_ROOT="${SCRATCH}/root-c2c-$1"
    rm -rf "${C2C_ROOT}"
    env "$2=$3" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="${C2C_ROOT}" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${INSTALL_SH}" >"${SCRATCH}/c2c-$1-out.log" 2>"${SCRATCH}/c2c-$1-err.log" \
        || { echo "FAIL: $2='$3' was rejected (C2c/$1)" >&2; cat "${SCRATCH}/c2c-$1-err.log" >&2; exit 1; }
    grep -qxF "$2=$3" "${C2C_ROOT}/etc/gotham/gotham.env" \
        || { echo "FAIL: $2='$3' was not written verbatim (C2c/$1)" >&2; exit 1; }
}
_c2c_must_accept dsn-at GOTHAM_DATABASE_DSN 'postgres://gotham@db.internal:5432/gotham?sslmode=require'
_c2c_must_accept dsn-userpass GOTHAM_DATABASE_DSN 'postgres://gotham:s3cret@db.internal:5432/gotham?sslmode=require'
_c2c_must_accept dsn-pct GOTHAM_DATABASE_DSN 'postgres://gotham:p%40ss%3Aword@localhost:5432/gotham?sslmode=disable'
_c2c_must_accept redis-url GOTHAM_REDIS_ADDR 'rediss://redis.internal:6380'
echo "PASS: @, user:password@, %-escaped and rediss:// DSN/Redis values are accepted by name (C2c)"

# ---- R4: a real run ignores GOTHAM_INSTALL_TEST_AGENT_SCRIPT ---------------
# No --dry-run, no GOTHAM_INSTALL_ROOT: the installer runs for real with
# every test seam set hostile, from a scratch copy whose five writable
# roots are rewritten onto a sandbox and whose platform/agent.env lookups
# are baked to fixtures (the suite's bake_installer pattern: production
# reads fixed paths on a real run, so the copy drives them through the
# file). The PREFIX/TEST_MODE derivation, the gate and everything else are
# byte-identical, asserted below. PATH shims fake root (id), the service
# activations (systemctl/runuser), the key verification this run cannot
# pass honestly (openssl) and the sudoers sub-installer (sh); file
# operations run for real inside the sandbox. The run must still reach the
# agent step, pick the REAL install-agent.sh, and fail only on its missing
# CA — the fake script must never execute. A scratch copy whose gate
# honours the seam on a real run lets the fake execute, so this test
# catches that regression (run it with INSTALL_SH_OVERRIDE=<mutant> to
# see it fail).
echo "==> a real run ignores the agent-script seam (R4)"
RR_SHIM="${SCRATCH}/rr-shim"
RR_BOX="${SCRATCH}/realrun"
mkdir -p "${RR_SHIM}" "${RR_BOX}/tmp"
RR_AGENT_ENV="${SCRATCH}/rr-agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442\nGOTHAM_AGENT_NODE_ID=old-installed-host-agent\n' >"${RR_AGENT_ENV}"
RR_INSTALLER="${SCRATCH}/install-rr.sh"
# Sandbox the five writable roots (the rest derive from them) and bake the
# platform/agent.env lookups to fixtures (the one early block feeds both the
# validate-before-mutate gate and the late localhost-step lookup); the
# TEST_MODE derivation, the gate and everything else stay byte-identical so
# this is a genuine TEST_MODE=0 real run, only relocated.
sed -e "s|^ETC_DIR=\"\${PREFIX}/etc/gotham\"\$|ETC_DIR=\"${RR_BOX}/etc/gotham\"|" \
    -e "s|^STATE_DIR=\"\${PREFIX}/var/lib/gotham\"\$|STATE_DIR=\"${RR_BOX}/var/lib/gotham\"|" \
    -e "s|^STATUS_DIR=\"\${PREFIX}/var/lib/gotham-updater\"\$|STATUS_DIR=\"${RR_BOX}/var/lib/gotham-updater\"|" \
    -e "s|^WRAPPER_PATH=\"\${PREFIX}/usr/libexec/gotham/gotham-update\"\$|WRAPPER_PATH=\"${RR_BOX}/usr/libexec/gotham/gotham-update\"|" \
    -e "s|^SERVICE_FILE=\"\${PREFIX}/etc/systemd/system/gotham.service\"\$|SERVICE_FILE=\"${RR_BOX}/etc/systemd/system/gotham.service\"|" \
    -e "s|^    _la_os_release=\"/etc/os-release\"\$|    _la_os_release=\"${FIXTURES}/ubuntu-release\"|" \
    -e "s|^    _la_agent_env=\"/etc/gotham/agent.env\"\$|    _la_agent_env=\"${RR_AGENT_ENV}\"|" \
    "${INSTALL_SH}" >"${RR_INSTALLER}"
for _rrvar in ETC_DIR STATE_DIR STATUS_DIR WRAPPER_PATH SERVICE_FILE; do
    grep -q "^${_rrvar}=\"${RR_BOX}/" "${RR_INSTALLER}" \
        || { echo "FAIL: the R4 copy did not rewrite ${_rrvar}" >&2; exit 1; }
done
grep -qxF "    _la_os_release=\"${FIXTURES}/ubuntu-release\"" "${RR_INSTALLER}" \
    || { echo "FAIL: the R4 copy did not bake the os-release path" >&2; exit 1; }
grep -qxF "    _la_agent_env=\"${RR_AGENT_ENV}\"" "${RR_INSTALLER}" \
    || { echo "FAIL: the R4 copy did not bake the agent.env path" >&2; exit 1; }
_rr_extra="$(diff "${INSTALL_SH}" "${RR_INSTALLER}" | grep '^>' | grep -vcE '^> ((ETC_DIR|STATE_DIR|STATUS_DIR|WRAPPER_PATH|SERVICE_FILE)=|    _la_os_release=|    _la_agent_env=)' || true)"
[ "${_rr_extra}" = "0" ] \
    || { echo "FAIL: the R4 copy differs by more than the relocated lines" >&2; diff "${INSTALL_SH}" "${RR_INSTALLER}" >&2; exit 1; }
    # The copy runs from its own directory (SCRIPT_DIR), so link the real
    # siblings next to it; the copy itself stays the file under test.
    for _sib in release-verify.sh gotham-update.sh gotham-updater.conf \
        install-sudoers.sh gotham.service install-agent.sh install-agent-lib.sh \
        gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service \
        gotham-toolctl.sh install-toolctl-sudoers.sh gotham-hostbootstrap.sh \
        gotham-hostbootstrap-fetch.sh gotham-signing-key.pub gotham-hostctl.sh; do
        ln -sf "${SCRIPT_DIR}/${_sib}" "$(dirname "${RR_INSTALLER}")/${_sib}"
    done
    REAL_OPENSSL="$(command -v openssl)"
    export REAL_OPENSSL
    cat >"${RR_SHIM}/id" <<'SHIM'
#!/bin/sh
printf '0\n'
SHIM
    cat >"${RR_SHIM}/systemctl" <<'SHIM'
#!/bin/sh
echo "systemctl $*" >>"${SYSTEMCTL_LOG:-/dev/null}"
exit 0
SHIM
    cat >"${RR_SHIM}/runuser" <<'SHIM'
#!/bin/sh
echo "runuser $*" >>"${SYSTEMCTL_LOG:-/dev/null}"
exit 0
SHIM
    for _dumb in sudo visudo; do
        printf '#!/bin/sh\nexit 0\n' >"${RR_SHIM}/${_dumb}"
    done
    # install runs for real when it can (owner flags stripped: the fake
    # root owns nothing); chown can never succeed here and is faked.
    cat >"${RR_SHIM}/install" <<'SHIM'
#!/bin/sh
if "${0}-real" "$@" 2>/dev/null; then exit 0; fi
stripped=""
skip=0
for a in "$@"; do
    if [ "${skip}" = "1" ]; then skip=0; continue; fi
    case "${a}" in
        -o | -g) skip=1 ;;
        *) stripped="${stripped} ${a}" ;;
    esac
done
# shellcheck disable=SC2086
"${0}-real" ${stripped} 2>/dev/null || exit 0
SHIM
    ln -sf "$(command -v install)" "${RR_SHIM}/install-real"
    printf '#!/bin/sh\nexit 0\n' >"${RR_SHIM}/chown"
    cat >"${RR_SHIM}/openssl" <<'SHIM'
#!/bin/sh
# The capability probe and the manifest verification are faked (the run has
# no key it could verify with); every other openssl runs for real.
case "$*" in
    *pkeyutl*-help*) printf 'options: -rawin -verify\n'; exit 0 ;;
    *pkeyutl*-verify*) exit 0 ;;
esac
exec "${REAL_OPENSSL}" "$@"
SHIM
    cat >"${RR_SHIM}/sh" <<'SHIM'
#!/bin/sh
# The sudoers sub-installer would write the host /etc/sudoers.d; it is
# proven elsewhere, so the fake-root run fakes its success. The agent step
# (the point of this test) runs for real.
case "$*" in
    *install-sudoers.sh*) exit 0 ;;
esac
exec /bin/sh "$@"
SHIM
    chmod +x "${RR_SHIM}/id" "${RR_SHIM}/systemctl" "${RR_SHIM}/runuser" \
        "${RR_SHIM}/sudo" "${RR_SHIM}/visudo" "${RR_SHIM}/install" \
        "${RR_SHIM}/chown" "${RR_SHIM}/openssl" "${RR_SHIM}/sh"
    cat >"${SCRATCH}/rr-agent.sh" <<'SHIM'
#!/bin/sh
touch "${AGENT_MARKER:?}"
echo "fake-agent: ran" >&2
exit 1
SHIM
    chmod +x "${SCRATCH}/rr-agent.sh"
    cat >"${SCRATCH}/rr-admin.sh" <<'SHIM'
#!/bin/sh
touch "${ADMIN_MARKER:?}"
echo "fake-admin: ran" >&2
exit 1
SHIM
    chmod +x "${SCRATCH}/rr-admin.sh"
    rm -f "${SCRATCH}/rr-agent-ran" "${SCRATCH}/rr-admin-ran"
    : >"${SCRATCH}/rr-systemctl.log"
    mkdir -p "${RR_BOX}/var/lib/gotham/ca"
    (umask 077; printf '%s\n' enrollment-fixture-secret >"${RR_BOX}/var/lib/gotham/ca/enrollment-token")
    RR_RC=0
    # GOTHAM_ADMIN_EMAIL="" pins the phase-1 collection to the non-interactive
    # no-email path: this is a genuine TEST_MODE=0 run, so on a developer
    # terminal it would otherwise stop at the tty prompt.
    TMPDIR="${RR_BOX}/tmp" \
    SYSTEMCTL_LOG="${SCRATCH}/rr-systemctl.log" \
    AGENT_MARKER="${SCRATCH}/rr-agent-ran" \
    ADMIN_MARKER="${SCRATCH}/rr-admin-ran" \
    GOTHAM_ADMIN_EMAIL="" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/rr-agent.sh" \
    GOTHAM_INSTALL_TEST_ADMIN_BIN="${SCRATCH}/rr-admin.sh" \
    GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
    GOTHAM_TEST_UNAME_M=sparc64 \
    GOTHAM_AGENT_ENV_FILE="${SCRATCH}/rr-agent.env" \
    GOTHAM_APT_ROOT="${SCRATCH}/rr-hostile-apt" \
    GOTHAM_HOST_BOOTSTRAP_ROOT="${RR_BOX}" \
    PATH="${RR_SHIM}:${PATH}" \
        sh "${RR_INSTALLER}" >"${SCRATCH}/rr-out.log" 2>"${SCRATCH}/rr-err.log" || RR_RC=$?
    [ "${RR_RC}" -ne 0 ] \
        || { echo "FAIL: the real run exited 0 although the agent step failed (R4)" >&2; exit 1; }
    grep -q 'installing the localhost agent node' "${SCRATCH}/rr-out.log" \
        || { echo "FAIL: the real run never reached the agent step (R4)" >&2; cat "${SCRATCH}/rr-err.log" >&2; exit 1; }
    grep -q 'Retry only the agent step' "${SCRATCH}/rr-err.log" \
        || { echo "FAIL: the real run failed before the agent step (R4)" >&2; cat "${SCRATCH}/rr-err.log" >&2; exit 1; }
    [ ! -e "${SCRATCH}/rr-agent-ran" ] \
        || { echo "FAIL: the fake agent script ran on a real run (R4)" >&2; exit 1; }
    [ ! -e "${SCRATCH}/rr-admin-ran" ] \
        || { echo "FAIL: the admin-bin seam fake ran on a real run (R4/JUS-22)" >&2; exit 1; }
    if grep -q 'hostile-apt-root\|sparc64\|arch-release' "${SCRATCH}/rr-out.log" "${SCRATCH}/rr-err.log"; then
        echo "FAIL: a hostile seam value leaked into the real run (R4)" >&2
        exit 1
    fi
    grep -q 'GOTHAM_LEGACY_LOCAL_NODE_ID=.*gotham-migrate.*old-installed-host-agent' "${SCRATCH}/rr-systemctl.log" \
        || { echo "FAIL: migration did not receive the saved local agent identity" >&2; exit 1; }
    echo "PASS: a real run reaches the agent step on the real script and never runs the seam fake (R4)"

# ---- JUS-22: the installer creates the first admin account ---------------
# The admin step runs after the control plane is up, through the installed
# binary (a fake here, via the test-only GOTHAM_INSTALL_TEST_ADMIN_BIN
# seam): non-interactive env vars, generated passwords, rerun idempotency,
# invalid/missing emails, and the interactive tty prompt under a pty. The
# password must only travel on stdin: it is absent from argv, the
# environment, the env file and the logs.
echo "==> first admin account creation (JUS-22)"
cat >"${SCRATCH}/fake-admin.sh" <<'SHIM'
#!/bin/sh
# Fake `gotham admin` for the JUS-22 tests. State lives in $ADMIN_STATE_DIR:
# a `users` marker file means an account exists. Every `admin create` call
# records its argv, environment and stdin password for the secrecy asserts.
state="${ADMIN_STATE_DIR:?}"
# The fake stands in for the `gotham` binary, so it receives the full
# `admin <subcommand> ...` line like the real one.
if [ "$1" = instance ]; then
    if [ "$2" = wait-localhost ]; then exit "${DOMAIN_WAIT_RC:-0}"; fi
    [ "$2" = set-url ] || exit 2
    printf '%s\n' "$@" >"${DOMAIN_TEST_LOG}"
    exit "${DOMAIN_TEST_RC:-0}"
fi
cmd="$1"
shift || true
if [ "${cmd}" = "admin" ]; then
    cmd="$1"
    shift || true
fi
case "${cmd}" in
    exists)
        if [ "${ADMIN_NOADMIN:-0}" = "1" ]; then
            # Stand-in for a release with no `admin` command at all (v0.1.0):
            # it can run neither the probe nor the manual create command.
            echo 'unknown command "admin"' >&2
            exit 2
        fi
        if [ "${ADMIN_LEGACY:-0}" = "1" ]; then
            # Stand-in for a release with `admin create` but no `admin exists`
            # (v0.2.0): it cannot run the probe.
            echo 'unknown admin command "exists"' >&2
            exit 2
        fi
        if [ "${ADMIN_EXISTS_FAIL:-0}" = "1" ]; then
            echo "fake-admin: database is down" >&2
            exit 1
        fi
        if [ "${ADMIN_DB_COLLISION:-0}" = "1" ]; then
            # Genuine database error whose text contains the legacy phrase
            # without being the exact usage line: must stay a failure.
            echo 'fake-admin: connect to database: FATAL: database "unknown admin command" does not exist' >&2
            exit 1
        fi
        if [ -f "${state}/users" ]; then
            echo "admin-exists: true"
        else
            echo "admin-exists: false"
        fi
        exit 0
        ;;
    create)
        if [ "${ADMIN_NOADMIN:-0}" = "1" ]; then
            echo 'unknown command "admin"' >&2
            exit 2
        fi
        if [ "${ADMIN_LEGACY:-0}" = "1" ]; then
            # Same-era `admin create` has no --password-stdin either: the
            # installer must never get this far on such a release.
            echo "flag provided but not defined: -password-stdin" >&2
            exit 2
        fi
        email=""
        stdin_mode=0
        prev=""
        for a in "$@"; do
            if [ "${prev}" = "--email" ]; then email="$a"; fi
            if [ "$a" = "--password-stdin" ]; then stdin_mode=1; fi
            prev="$a"
        done
        printf '%s\n' "$@" >"${state}/argv"
        env | sort >"${state}/env"
        pw=""
        IFS= read -r pw || true
        printf '%s' "${pw}" >"${state}/password"
        if [ "${ADMIN_CREATE_FAIL:-0}" = "1" ]; then
            echo "fake-admin: create refused (ADMIN_CREATE_FAIL)" >&2
            exit 1
        fi
        if [ "${ADMIN_CREATE_FAIL_ONCE:-0}" = "1" ] && [ ! -f "${state}/failed-once" ]; then
            touch "${state}/failed-once"
            echo "fake-admin: first create refused (ADMIN_CREATE_FAIL_ONCE)" >&2
            exit 1
        fi
        if [ "${ADMIN_FLIP_FLOP:-0}" = "1" ]; then
            # Race stand-in: the create loses to a concurrent registration
            # (the account now exists) and reports the conflict.
            touch "${state}/users"
            echo "fake-admin: email already registered" >&2
            exit 1
        fi
        if [ "${stdin_mode}" != "1" ]; then
            echo "fake-admin: password did not arrive on stdin" >&2
            exit 1
        fi
        case "${email}" in
            ?*@?*.?*) ;;
            *) echo "fake-admin: invalid email ${email}" >&2; exit 1 ;;
        esac
        case "${pw}" in
            ????????*) ;;
            *) echo "fake-admin: password too short" >&2; exit 1 ;;
        esac
        touch "${state}/users"
        echo "created account ${email}"
        exit 0
        ;;
    *)
        echo "fake-admin: unknown command ${cmd}" >&2
        exit 2
        ;;
esac
SHIM
chmod +x "${SCRATCH}/fake-admin.sh"
cat >"${SCRATCH}/ok-agent.sh" <<'SHIM'
#!/bin/sh
exit 0
SHIM
chmod +x "${SCRATCH}/ok-agent.sh"
: >"${SCRATCH}/admin-systemctl.log"
# run_admin_install runs a full sandbox install past service activation
# (RUN_AGENT + succeeding fake agent + systemctl shim, the R3 pattern) with
# the fake admin binary. $1 root, $2 admin state dir, $3 stdout log,
# $4 stderr log. ADMIN_EMAIL/ADMIN_PWFILE/ADMIN_TTY shell vars steer the run
# (empty means unset-equivalent).
run_admin_install() {
    _ar_root="$1"
    _ar_state="$2"
    _ar_out="$3"
    _ar_err="$4"
    # Extra argv for the installer itself (empty, or -x for the xtrace case);
    # "$@" below keeps it word-split-free.
    set -- "${INSTALL_SH}"
    if [ "${ADMIN_XTRACE:-0}" = "1" ]; then
        set -- -x "$@"
    fi
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="${_ar_root}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/ok-agent.sh" \
    GOTHAM_INSTALL_TEST_ADMIN_BIN="${SCRATCH}/fake-admin.sh" \
    GOTHAM_ADMIN_EMAIL="${ADMIN_EMAIL:-}" \
    GOTHAM_ADMIN_PASSWORD_FILE="${ADMIN_PWFILE:-}" \
    GOTHAM_INSTALL_TEST_ADMIN_TTY="${ADMIN_TTY:-}" \
    ADMIN_EXISTS_FAIL="${ADMIN_EXISTS_FAIL:-0}" \
    ADMIN_CREATE_FAIL="${ADMIN_CREATE_FAIL:-0}" \
    ADMIN_CREATE_FAIL_ONCE="${ADMIN_CREATE_FAIL_ONCE:-0}" \
    ADMIN_FLIP_FLOP="${ADMIN_FLIP_FLOP:-0}" \
    ADMIN_LEGACY="${ADMIN_LEGACY:-0}" \
    ADMIN_NOADMIN="${ADMIN_NOADMIN:-0}" \
    ADMIN_DB_COLLISION="${ADMIN_DB_COLLISION:-0}" \
    ADMIN_STATE_DIR="${_ar_state}" \
    SYSTEMCTL_LOG="${SCRATCH}/admin-systemctl.log" \
    PATH="${R3_SHIM}:${PATH}" \
        sh "$@" >"${_ar_out}" 2>"${_ar_err}"
    _ar_rc=$?
    # One-shot fake controls: never leak them into later runs.
    unset ADMIN_EXISTS_FAIL ADMIN_CREATE_FAIL ADMIN_CREATE_FAIL_ONCE
    unset ADMIN_FLIP_FLOP ADMIN_XTRACE ADMIN_LEGACY ADMIN_NOADMIN ADMIN_DB_COLLISION
    return "${_ar_rc}"
}
# The password must never be passed as a command-line argument: only
# --password-stdin (no value) may appear in the installer.
if grep -q -- '--password ' "${INSTALL_SH}"; then
    echo "FAIL: install.sh passes --password <value> on a command line (JUS-22)" >&2
    exit 1
fi
grep -q -- '--password-stdin' "${INSTALL_SH}" \
    || { echo "FAIL: install.sh does not use --password-stdin (JUS-22)" >&2; exit 1; }
echo "PASS: the installer passes the password on stdin only, never as argv (JUS-22)"

# JUS-22/a: non-interactive with GOTHAM_ADMIN_EMAIL + password file.
A_ROOT="${SCRATCH}/root-admin-env"
A_STATE="${SCRATCH}/admin-state-env"
mkdir -p "${A_STATE}"
printf '%s' "Sup3r \$ecret!" >"${SCRATCH}/admin-pw.txt"
chmod 600 "${SCRATCH}/admin-pw.txt"
ADMIN_EMAIL="ops@example.com" ADMIN_PWFILE="${SCRATCH}/admin-pw.txt" ADMIN_TTY="" \
    run_admin_install "${A_ROOT}" "${A_STATE}" "${SCRATCH}/a-out.log" "${SCRATCH}/a-err.log" \
    || { echo "FAIL: non-interactive admin creation failed (JUS-22/a)" >&2; cat "${SCRATCH}/a-err.log" >&2; exit 1; }
[ -f "${A_STATE}/users" ] \
    || { echo "FAIL: admin create never ran (JUS-22/a)" >&2; exit 1; }
[ "$(cat "${A_STATE}/password")" = "Sup3r \$ecret!" ] \
    || { echo "FAIL: the password file did not arrive verbatim on stdin (JUS-22/a)" >&2; exit 1; }
grep -q 'Account:  ops@example.com' "${SCRATCH}/a-out.log" \
    || { echo "FAIL: the summary does not show the admin email (JUS-22/a)" >&2; exit 1; }
if grep -qF -- "Sup3r \$ecret!" "${SCRATCH}/a-out.log" "${SCRATCH}/a-err.log"; then
    echo "FAIL: the password leaked into the install logs (JUS-22/a)" >&2
    exit 1
fi
if grep -qF -- "Sup3r \$ecret!" "${A_ROOT}/etc/gotham/gotham.env" "${A_STATE}/env" "${A_STATE}/argv"; then
    echo "FAIL: the password leaked into the env file, child env or argv (JUS-22/a)" >&2
    exit 1
fi
echo "PASS: non-interactive GOTHAM_ADMIN_EMAIL + password file creates the account secretly (JUS-22/a)"

# JUS-22/b: generated password is shown once and nowhere else.
B_STATE="${SCRATCH}/admin-state-gen"
mkdir -p "${B_STATE}"
ADMIN_EMAIL="gen@example.com" ADMIN_PWFILE="" ADMIN_TTY="" \
    run_admin_install "${A_ROOT}" "${B_STATE}" "${SCRATCH}/b-out.log" "${SCRATCH}/b-err.log" \
    || { echo "FAIL: generated-password install failed (JUS-22/b)" >&2; cat "${SCRATCH}/b-err.log" >&2; exit 1; }
B_GEN="$(sed -n 's/^  Password: //p' "${SCRATCH}/b-out.log" | head -n1)"
[ -n "${B_GEN}" ] \
    || { echo "FAIL: the summary shows no generated password (JUS-22/b)" >&2; cat "${SCRATCH}/b-out.log" >&2; exit 1; }
[ "$(printf '%s' "${B_GEN}" | wc -c)" -ge 24 ] \
    || { echo "FAIL: the generated password is shorter than 24 chars (JUS-22/b)" >&2; exit 1; }
grep -q 'Store the password now, it is not shown again' "${SCRATCH}/b-out.log" \
    || { echo "FAIL: the store-it-now line is missing (JUS-22/b)" >&2; exit 1; }
grep -q 'Account:  gen@example.com' "${SCRATCH}/b-out.log" \
    || { echo "FAIL: the summary does not pair the email with the password (JUS-22/b)" >&2; exit 1; }
[ "$(grep -oF -e "${B_GEN}" -- "${SCRATCH}/b-out.log" | wc -l)" -eq 1 ] \
    || { echo "FAIL: the generated password is shown more than once (JUS-22/b)" >&2; exit 1; }
if grep -qF -e "${B_GEN}" -- "${SCRATCH}/b-err.log"; then
    echo "FAIL: the generated password leaked onto stderr (JUS-22/b)" >&2
    exit 1
fi
if grep -qF -e "${B_GEN}" -- "${A_ROOT}/etc/gotham/gotham.env" "${B_STATE}/env" "${B_STATE}/argv"; then
    echo "FAIL: the generated password leaked into the env file, child env or argv (JUS-22/b)" >&2
    exit 1
fi
echo "PASS: the generated password is shown once, never logged or stored (JUS-22/b)"

# ---- JUS-40: a leading-hyphen password is matched literally, never as options
# Without -e/-- a password starting with `-` is parsed as grep options:
# grep exits 2, and `if grep` misreads the error as "no match", so a
# secrecy assert passes while blind. The static pin fails on any
# password-controlled grep lacking -e/--, and the live run through the
# existing password-file seam checks exit statuses explicitly (0 = found,
# 1 = absent; anything else fails loudly instead of passing silently).
HY_BAD="$(grep -E 'grep.*\$\{(B_GEN|H_GEN)\}' "${SCRIPT_DIR}/test-release-install.sh" | grep -v ' -- ' || true)"
[ -z "${HY_BAD}" ] \
    || { echo "FAIL: password-controlled grep without -e/-- (JUS-40): ${HY_BAD}" >&2; exit 1; }
HY_ROOT="${SCRATCH}/root-admin-hyphen"
HY_STATE="${SCRATCH}/admin-state-hyphen"
mkdir -p "${HY_STATE}"
printf '%s' '-lead-Hyphen-S3cret!' >"${SCRATCH}/admin-hyphen-pw.txt"
chmod 600 "${SCRATCH}/admin-hyphen-pw.txt"
HY_PW='-lead-Hyphen-S3cret!'
ADMIN_EMAIL="hyphen@example.com" ADMIN_PWFILE="${SCRATCH}/admin-hyphen-pw.txt" ADMIN_TTY="" \
    run_admin_install "${HY_ROOT}" "${HY_STATE}" "${SCRATCH}/hy-out.log" "${SCRATCH}/hy-err.log" \
    || { echo "FAIL: leading-hyphen admin creation failed (JUS-40)" >&2; cat "${SCRATCH}/hy-err.log" >&2; exit 1; }
[ "$(cat "${HY_STATE}/password")" = "${HY_PW}" ] \
    || { echo "FAIL: the leading-hyphen password did not arrive verbatim (JUS-40)" >&2; exit 1; }
HY_RC=0
grep -qF -e "${HY_PW}" -- "${SCRATCH}/admin-hyphen-pw.txt" 2>/dev/null || HY_RC=$?
[ "${HY_RC}" -eq 0 ] \
    || { echo "FAIL: literal match missed a leading-hyphen password, rc=${HY_RC} (JUS-40)" >&2; exit 1; }
HY_RC=0
grep -qF -e "${HY_PW}" -- "${SCRATCH}/hy-out.log" "${SCRATCH}/hy-err.log" 2>/dev/null || HY_RC=$?
[ "${HY_RC}" -eq 1 ] \
    || { echo "FAIL: log secrecy check inconclusive, rc=${HY_RC} (JUS-40)" >&2; exit 1; }
HY_RC=0
grep -qF -e "${HY_PW}" -- "${HY_ROOT}/etc/gotham/gotham.env" "${HY_STATE}/env" "${HY_STATE}/argv" 2>/dev/null || HY_RC=$?
[ "${HY_RC}" -eq 1 ] \
    || { echo "FAIL: env/argv secrecy check inconclusive, rc=${HY_RC} (JUS-40)" >&2; exit 1; }
echo "PASS: leading-hyphen passwords match literally and stay secret (JUS-40)"

# JUS-22/c: rerun idempotency via `admin exists` (no prompt, no failure).
rm -f "${A_STATE}/argv"
ADMIN_EMAIL="someone-else@example.com" ADMIN_PWFILE="" ADMIN_TTY="" \
    run_admin_install "${A_ROOT}" "${A_STATE}" "${SCRATCH}/c-out.log" "${SCRATCH}/c-err.log" \
    || { echo "FAIL: rerun with an existing account failed (JUS-22/c)" >&2; cat "${SCRATCH}/c-err.log" >&2; exit 1; }
grep -q 'admin already exists, skipping' "${SCRATCH}/c-out.log" \
    || { echo "FAIL: the rerun did not skip with the exists message (JUS-22/c)" >&2; exit 1; }
[ ! -e "${A_STATE}/argv" ] \
    || { echo "FAIL: the rerun called admin create although an account exists (JUS-22/c)" >&2; exit 1; }
echo "PASS: a rerun detects the existing account and skips (JUS-22/c)"

# JUS-22/d: invalid email fails closed naming the variable.
D_STATE="${SCRATCH}/admin-state-bad"
mkdir -p "${D_STATE}"
D_RC=0
ADMIN_EMAIL="not-an-email" ADMIN_PWFILE="" ADMIN_TTY="" \
    run_admin_install "${SCRATCH}/root-admin-bad" "${D_STATE}" "${SCRATCH}/d-out.log" "${SCRATCH}/d-err.log" || D_RC=$?
[ "${D_RC}" -ne 0 ] \
    || { echo "FAIL: an invalid GOTHAM_ADMIN_EMAIL was accepted (JUS-22/d)" >&2; exit 1; }
grep -q 'GOTHAM_ADMIN_EMAIL' "${SCRATCH}/d-err.log" \
    || { echo "FAIL: the invalid-email rejection does not name GOTHAM_ADMIN_EMAIL (JUS-22/d)" >&2; cat "${SCRATCH}/d-err.log" >&2; exit 1; }
[ ! -e "${D_STATE}/users" ] \
    || { echo "FAIL: an account was created for an invalid email (JUS-22/d)" >&2; exit 1; }
echo "PASS: an invalid GOTHAM_ADMIN_EMAIL fails closed (JUS-22/d)"

# JUS-22/e: no email and no tty skips with the exact manual command.
E_STATE="${SCRATCH}/admin-state-skip"
mkdir -p "${E_STATE}"
ADMIN_EMAIL="" ADMIN_PWFILE="" ADMIN_TTY="" \
    run_admin_install "${SCRATCH}/root-admin-skip" "${E_STATE}" "${SCRATCH}/e-out.log" "${SCRATCH}/e-err.log" \
    || { echo "FAIL: the missing-email run failed instead of skipping (JUS-22/e)" >&2; cat "${SCRATCH}/e-err.log" >&2; exit 1; }
grep -q 'admin create --email ops@example.com' "${SCRATCH}/e-out.log" \
    || { echo "FAIL: the skip does not print the exact manual command (JUS-22/e)" >&2; exit 1; }
[ ! -e "${E_STATE}/users" ] \
    || { echo "FAIL: an account was created without an email (JUS-22/e)" >&2; exit 1; }
echo "PASS: a missing email skips with the manual command (JUS-22/e)"

# JUS-22/i: an exists-check failure warns and exits nonzero (nothing rolled back).
I_STATE="${SCRATCH}/admin-state-fail"
mkdir -p "${I_STATE}"
I_RC=0
ADMIN_EMAIL="ops@example.com" ADMIN_PWFILE="" ADMIN_TTY="" ADMIN_EXISTS_FAIL=1 \
    run_admin_install "${SCRATCH}/root-admin-fail" "${I_STATE}" "${SCRATCH}/i-out.log" "${SCRATCH}/i-err.log" || I_RC=$?
[ "${I_RC}" -ne 0 ] \
    || { echo "FAIL: an exists-check failure exited 0 (JUS-22/i)" >&2; exit 1; }
grep -q 'could not check for an existing admin account' "${SCRATCH}/i-err.log" \
    || { echo "FAIL: the exists-check failure names no reason (JUS-22/i)" >&2; cat "${SCRATCH}/i-err.log" >&2; exit 1; }
grep -q 'Nothing was rolled back' "${SCRATCH}/i-err.log" \
    || { echo "FAIL: the admin warning does not say the control plane was kept (JUS-22/i)" >&2; exit 1; }
grep -q 'The admin account could not be created automatically' "${SCRATCH}/i-out.log" \
    || { echo "FAIL: the summary shows no manual fallback (JUS-22/i)" >&2; exit 1; }
grep -q 'admin create --email ops@example.com' "${SCRATCH}/i-out.log" \
    || { echo "FAIL: the fallback names no manual command (JUS-22/i)" >&2; exit 1; }
[ ! -e "${I_STATE}/users" ] \
    || { echo "FAIL: an account was created although the check failed (JUS-22/i)" >&2; exit 1; }
[ -x "${SCRATCH}/root-admin-fail/var/lib/gotham/bin/gotham" ] \
    || { echo "FAIL: the control plane binary is missing after the admin failure (JUS-22/i)" >&2; exit 1; }
echo "PASS: an exists-check failure warns, keeps the control plane and exits nonzero (JUS-22/i)"

# JUS-38/j: a release with `admin create` but no `admin exists` (v0.2.0)
# skips automatic creation with the manual command instead of the database
# warning, and exits 0: the install itself succeeded.
J_STATE="${SCRATCH}/admin-state-legacy"
mkdir -p "${J_STATE}"
ADMIN_EMAIL="legacy@example.com" ADMIN_PWFILE="" ADMIN_TTY="" ADMIN_LEGACY=1 \
    run_admin_install "${SCRATCH}/root-admin-legacy" "${J_STATE}" "${SCRATCH}/j-out.log" "${SCRATCH}/j-err.log" \
    || { echo "FAIL: the legacy-release install exited nonzero (JUS-38/j)" >&2; cat "${SCRATCH}/j-err.log" >&2; exit 1; }
grep -q 'predates automated admin bootstrap' "${SCRATCH}/j-out.log" \
    || { echo "FAIL: the legacy skip names no reason (JUS-38/j)" >&2; cat "${SCRATCH}/j-out.log" >&2; exit 1; }
grep -q 'admin create --email legacy@example.com' "${SCRATCH}/j-out.log" \
    || { echo "FAIL: the legacy skip shows no manual command (JUS-38/j)" >&2; exit 1; }
if grep -q 'could not check for an existing admin account' "${SCRATCH}/j-out.log" "${SCRATCH}/j-err.log"; then
    echo "FAIL: the legacy skip raised the database warning (JUS-38/j)" >&2
    exit 1
fi
[ ! -e "${J_STATE}/users" ] \
    || { echo "FAIL: an account was created through the legacy binary (JUS-38/j)" >&2; exit 1; }
[ -x "${SCRATCH}/root-admin-legacy/var/lib/gotham/bin/gotham" ] \
    || { echo "FAIL: the control plane binary is missing after the legacy skip (JUS-38/j)" >&2; exit 1; }
echo "PASS: a pre-bootstrap release skips admin creation with the manual command, no database warning (JUS-38/j)"

# JUS-39: every manual admin command the installer prints must run as the
# service user (gotham.env is root:gotham 0640, group-readable), never root.
if grep -q 'sudo ${INSTALL_PATH} admin' "${INSTALL_SH}" \
    || grep -q 'sudo /var/lib/gotham/bin/gotham admin' "${INSTALL_SH}" \
    || grep -q 'sudo /var/lib/gotham/bin/gotham admin' "${SCRIPT_DIR}/../docs/guide/install.md"; then
    echo "FAIL: an active manual admin command still runs as root (JUS-39)" >&2
    exit 1
fi
for _jus39_log in "${SCRATCH}/e-out.log" "${SCRATCH}/i-out.log" "${SCRATCH}/j-out.log"; do
    grep -q 'sudo -u gotham -- .*admin create --email' "${_jus39_log}" \
        || { echo "FAIL: ${_jus39_log} manual create does not run as the service user (JUS-39)" >&2; exit 1; }
done
grep -q 'sudo -u gotham -- .*admin reset-password' "${SCRATCH}/e-out.log" \
    || { echo "FAIL: the summary reset-password guidance does not run as the service user (JUS-39)" >&2; exit 1; }
echo "PASS: manual admin guidance runs as the service user, never root (JUS-39)"

# JUS-38/m: a release with no `admin` command at all (v0.1.0) prints upgrade
# guidance instead of a manual command that binary cannot run, and exits 0.
M_STATE="${SCRATCH}/admin-state-noadmin"
mkdir -p "${M_STATE}"
ADMIN_EMAIL="noadmin@example.com" ADMIN_PWFILE="" ADMIN_TTY="" ADMIN_NOADMIN=1 \
    run_admin_install "${SCRATCH}/root-admin-noadmin" "${M_STATE}" "${SCRATCH}/m-out.log" "${SCRATCH}/m-err.log" \
    || { echo "FAIL: the no-admin install exited nonzero (JUS-38/m)" >&2; cat "${SCRATCH}/m-err.log" >&2; exit 1; }
grep -q 'has no admin commands' "${SCRATCH}/m-out.log" \
    || { echo "FAIL: the no-admin skip names no reason (JUS-38/m)" >&2; cat "${SCRATCH}/m-out.log" >&2; exit 1; }
if grep -q 'admin create --email' "${SCRATCH}/m-out.log"; then
    echo "FAIL: the no-admin skip prints a create command that binary cannot run (JUS-38/m)" >&2
    exit 1
fi
if grep -q 'could not check for an existing admin account' "${SCRATCH}/m-out.log" "${SCRATCH}/m-err.log"; then
    echo "FAIL: the no-admin skip raised the database warning (JUS-38/m)" >&2
    exit 1
fi
[ ! -e "${M_STATE}/users" ] \
    || { echo "FAIL: an account was created through the admin-less binary (JUS-38/m)" >&2; exit 1; }
echo "PASS: a release without admin commands prints upgrade guidance, no create command, no database warning (JUS-38/m)"

# JUS-38/d: a genuine database error containing the legacy phrase is not a
# legacy skip: it warns and exits nonzero (nothing rolled back).
DC_STATE="${SCRATCH}/admin-state-collision"
mkdir -p "${DC_STATE}"
DC_RC=0
ADMIN_EMAIL="ops@example.com" ADMIN_PWFILE="" ADMIN_TTY="" ADMIN_DB_COLLISION=1 \
    run_admin_install "${SCRATCH}/root-admin-collision" "${DC_STATE}" "${SCRATCH}/dc-out.log" "${SCRATCH}/dc-err.log" || DC_RC=$?
[ "${DC_RC}" -ne 0 ] \
    || { echo "FAIL: a colliding database error exited 0 (JUS-38/d)" >&2; exit 1; }
grep -q 'could not check for an existing admin account' "${SCRATCH}/dc-err.log" \
    || { echo "FAIL: the colliding failure names no reason (JUS-38/d)" >&2; cat "${SCRATCH}/dc-err.log" >&2; exit 1; }
if grep -q 'predates automated admin bootstrap\|has no admin commands' "${SCRATCH}/dc-out.log"; then
    echo "FAIL: a genuine database error took a legacy skip (JUS-38/d)" >&2
    exit 1
fi
[ ! -e "${DC_STATE}/users" ] \
    || { echo "FAIL: an account was created although the check failed (JUS-38/d)" >&2; exit 1; }
echo "PASS: a database error containing the legacy phrase still warns and exits nonzero (JUS-38/d)"

# JUS-22/f: --dry-run prompts nothing and changes nothing, even with every
# admin seam set hostile (emails, password files, fake binary, tty).
F_RC=0
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
GOTHAM_ADMIN_EMAIL="dry@example.com" \
GOTHAM_ADMIN_PASSWORD_FILE="${SCRATCH}/admin-pw.txt" \
GOTHAM_INSTALL_TEST_ADMIN_BIN="${SCRATCH}/fake-admin.sh" \
GOTHAM_INSTALL_TEST_ADMIN_TTY=/dev/tty \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/f-out.log" 2>&1 || F_RC=$?
[ "${F_RC}" -eq 0 ] \
    || { echo "FAIL: --dry-run with admin seams failed (JUS-22/f)" >&2; cat "${SCRATCH}/f-out.log" >&2; exit 1; }
grep -q '\[dry-run\] /var/lib/gotham/bin/gotham admin create --email' "${SCRATCH}/f-out.log" \
    || { echo "FAIL: --dry-run does not show the admin step on the real binary (JUS-22/f)" >&2; cat "${SCRATCH}/f-out.log" >&2; exit 1; }
echo "PASS: --dry-run prompts nothing and changes nothing (JUS-22/f)"
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run --domain ' HTTPS://PANEL.EXAMPLE.COM/ ' >"${SCRATCH}/domain-dry.log" 2>&1
grep -q 'instance set-url https://panel.example.com' "${SCRATCH}/domain-dry.log" \
    || { echo "FAIL: --domain did not reach the instance command" >&2; exit 1; }
echo "PASS: --domain uses instance set-url"


# JUS-22/g-h: interactive prompt under a pty, driven by deploy/pty-drive.py
# (stdlib only; python3 is already required above): every SEND waits for its
# EXPECT first, so nothing is typed ahead and the kernel never echoes secrets
# while echo is still on. Assertions below are on what the installer prints
# and the fake records, never on type-ahead.
G_STATE="${SCRATCH}/admin-state-tty"
mkdir -p "${G_STATE}"
G_ROOT="${SCRATCH}/root-admin-tty"
cat >"${SCRATCH}/pty-g.txt" <<'SCRIPT'
EXPECT Admin email
SEND pty@example.com
EXPECT Admin password
SEND PtY-s3cret!
EXPECT Confirm password
SEND PtY-s3cret!
EXPECT Panel domain
SEND
EXPECT-EOF
SCRIPT
cat >"${SCRATCH}/pty-h.txt" <<'SCRIPT'
EXPECT Admin email
SEND bogus
EXPECT Invalid email address
EXPECT Admin email
SEND pty2@example.com
EXPECT Admin password
SEND
EXPECT Confirm password
SEND
EXPECT Panel domain
SEND
EXPECT-EOF
SCRIPT
pty_run() {
    python3 "${SCRIPT_DIR}/pty-drive.py" "$1" -- sh "${INSTALL_SH}"
}
# pty_run is a function, so its environment travels through exports (a
# VAR=... prefix would stop at the function on dash).
export GOTHAM_BASE_URL="http://127.0.0.1:${PORT}"
export GOTHAM_VERSION="${VERSION}"
export GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}"
export GOTHAM_INSTALL_TEST_RUN_AGENT=1
export GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/ok-agent.sh"
export GOTHAM_INSTALL_TEST_ADMIN_BIN="${SCRATCH}/fake-admin.sh"
export GOTHAM_INSTALL_TEST_ADMIN_TTY=/dev/tty
export GOTHAM_SKIP_DEPS=1
export SYSTEMCTL_LOG="${SCRATCH}/admin-systemctl.log"
export PATH="${R3_SHIM}:${PATH}"
export GOTHAM_INSTALL_ROOT="${G_ROOT}"
export ADMIN_STATE_DIR="${G_STATE}"
# One-shot fake controls default off; tests needing them export 1 and
# unset after (a leak would corrupt later cases).
export ADMIN_EXISTS_FAIL=0 ADMIN_CREATE_FAIL=0
export ADMIN_CREATE_FAIL_ONCE=0 ADMIN_FLIP_FLOP=0
pty_run "${SCRATCH}/pty-g.txt" >"${SCRATCH}/g-out.log" 2>"${SCRATCH}/g-err.log" \
    || { echo "FAIL: the pty admin run failed (JUS-22/g)" >&2; cat "${SCRATCH}/g-err.log" >&2; exit 1; }
tr -d '\r' <"${SCRATCH}/g-out.log" >"${SCRATCH}/g-out.clean"
[ -f "${G_STATE}/users" ] \
    || { echo "FAIL: the pty run created no account (JUS-22/g)" >&2; exit 1; }
[ "$(cat "${G_STATE}/password")" = 'PtY-s3cret!' ] \
    || { echo "FAIL: the tty password did not arrive verbatim (JUS-22/g)" >&2; exit 1; }
grep -q 'Account:  pty@example.com' "${SCRATCH}/g-out.clean" \
    || { echo "FAIL: the pty summary misses the email (JUS-22/g)" >&2; exit 1; }
if grep -qF -- 'PtY-s3cret!' "${SCRATCH}/g-out.clean" "${SCRATCH}/g-err.log"; then
    echo "FAIL: the tty password leaked into the logs (JUS-22/g)" >&2
    exit 1
fi
echo "PASS: the interactive tty prompt creates the account secretly (JUS-22/g)"

# JUS-22/h: empty tty password means generate; a bad email reprompts.
H_STATE="${SCRATCH}/admin-state-tty-gen"
mkdir -p "${H_STATE}"
export GOTHAM_INSTALL_ROOT="${SCRATCH}/root-admin-tty-gen"
export ADMIN_STATE_DIR="${H_STATE}"
pty_run "${SCRATCH}/pty-h.txt" >"${SCRATCH}/h-out.log" 2>"${SCRATCH}/h-err.log" \
    || { echo "FAIL: the pty generate run failed (JUS-22/h)" >&2; cat "${SCRATCH}/h-err.log" >&2; exit 1; }
tr -d '\r' <"${SCRATCH}/h-out.log" >"${SCRATCH}/h-out.clean"
grep -q 'Invalid email address, try again' "${SCRATCH}/h-out.clean" \
    || { echo "FAIL: the bad tty email did not reprompt (JUS-22/h)" >&2; exit 1; }
H_GEN="$(cat "${H_STATE}/password")"
[ "$(printf '%s' "${H_GEN}" | wc -c)" -ge 24 ] \
    || { echo "FAIL: the empty tty password did not generate (JUS-22/h)" >&2; exit 1; }
grep -q 'Store the password now, it is not shown again' "${SCRATCH}/h-out.clean" \
    || { echo "FAIL: the pty generate summary misses the store-it-now line (JUS-22/h)" >&2; exit 1; }
if grep -qF -e "${H_GEN}" -- "${SCRATCH}/h-err.log"; then
    echo "FAIL: the pty-generated password leaked onto stderr (JUS-22/h)" >&2
    exit 1
fi
echo "PASS: an empty tty password generates, a bad email reprompts (JUS-22/h)"

# ---- JUS-22 fix round 1 --------------------------------------------------
# Order statics: collection precedes the first mutation, creation precedes
# the service start, so registration can never open before the first
# account exists.
P1_LINE="$(grep -nx 'admin_phase1_collect' "${INSTALL_SH}" | cut -d: -f1 || true)"
P2_LINE="$(grep -nx 'admin_phase2_create' "${INSTALL_SH}" | cut -d: -f1 || true)"
[ -n "${P1_LINE}" ] && [ -n "${P2_LINE}" ] \
    || { echo "FAIL: admin phases are not called (JUS-22/order)" >&2; exit 1; }
MUT_LINE="$(grep -n 'useradd --system' "${INSTALL_SH}" | head -n1 | cut -d: -f1 || true)"
MIG_LINE="$(grep -n 'gotham-migrate' "${INSTALL_SH}" | head -n1 | cut -d: -f1 || true)"
START_LINE="$(grep -n 'BINARY_NAME}.service' "${INSTALL_SH}" | head -n1 | cut -d: -f1 || true)"
[ "${P1_LINE}" -lt "${MUT_LINE}" ] \
    || { echo "FAIL: collection is not before the first mutation (JUS-22/order)" >&2; exit 1; }
[ "${P2_LINE}" -gt "${MIG_LINE}" ] && [ "${P2_LINE}" -lt "${START_LINE}" ] \
    || { echo "FAIL: creation is not between migrate and start (JUS-22/order)" >&2; exit 1; }
echo "PASS: collection precedes mutation, creation precedes start (JUS-22/order)"

# JUS-22/j: tty streams without a controlling terminal fall back to the
# non-interactive branch (never prompt, never die silently). Skipped on
# macOS: its sh (bash) re-acquires a controlling terminal on startup, so
# the state cannot be simulated there; Linux sh (dash) does not.
if [ "$(uname -s)" = "Darwin" ]; then
    echo "SKIP: no-ctty needs a non-acquiring sh (JUS-22/j)"
else
    J_STATE="${SCRATCH}/admin-state-noctty"
    mkdir -p "${J_STATE}"
    export GOTHAM_INSTALL_ROOT="${SCRATCH}/root-admin-noctty"
    export ADMIN_STATE_DIR="${J_STATE}"
    export GOTHAM_ADMIN_EMAIL="noctty@example.com"
    python3 "${SCRIPT_DIR}/pty-drive.py" --no-ctty -- sh "${INSTALL_SH}" \
        >"${SCRATCH}/j-out.log" 2>"${SCRATCH}/j-err.log" \
        || { echo "FAIL: the no-ctty run failed (JUS-22/j)" >&2; cat "${SCRATCH}/j-err.log" >&2; exit 1; }
    unset GOTHAM_ADMIN_EMAIL
    [ -f "${J_STATE}/users" ] \
        || { echo "FAIL: the no-ctty run created no account (JUS-22/j)" >&2; exit 1; }
    if grep -q 'Admin email' "${SCRATCH}/j-out.log"; then
        echo "FAIL: the no-ctty run prompted although /dev/tty is unusable (JUS-22/j)" >&2
        exit 1
    fi
    grep -q 'Account:  noctty@example.com' "${SCRATCH}/j-out.log" \
        || { echo "FAIL: the no-ctty summary misses the account (JUS-22/j)" >&2; exit 1; }
    echo "PASS: tty streams without a controlling terminal use the non-interactive branch (JUS-22/j)"
fi

# JUS-22/k: a policy-rejected interactive password re-prompts (mirrored
# 8..128 bounds) instead of failing the install.
cat >"${SCRATCH}/pty-k.txt" <<'SCRIPT'
EXPECT Admin email
SEND policy@example.com
EXPECT Admin password
SEND short
EXPECT Confirm password
SEND short
EXPECT Password must be between
EXPECT Admin password
SEND long-enough-PW1
EXPECT Confirm password
SEND long-enough-PW1
EXPECT Panel domain
SEND
EXPECT-EOF
SCRIPT
K_STATE="${SCRATCH}/admin-state-policy"
mkdir -p "${K_STATE}"
export GOTHAM_INSTALL_ROOT="${SCRATCH}/root-admin-policy"
export ADMIN_STATE_DIR="${K_STATE}"
pty_run "${SCRATCH}/pty-k.txt" >"${SCRATCH}/k-out.log" 2>"${SCRATCH}/k-err.log" \
    || { echo "FAIL: the policy-reprompt run failed (JUS-22/k)" >&2; cat "${SCRATCH}/k-err.log" >&2; exit 1; }
tr -d '\r' <"${SCRATCH}/k-out.log" >"${SCRATCH}/k-out.clean"
[ -f "${K_STATE}/users" ] \
    || { echo "FAIL: the policy-reprompt run created no account (JUS-22/k)" >&2; exit 1; }
[ "$(cat "${K_STATE}/password")" = 'long-enough-PW1' ] \
    || { echo "FAIL: the reprompted password did not arrive (JUS-22/k)" >&2; exit 1; }
grep -q 'Password must be between 8 and 128 characters' "${SCRATCH}/k-out.clean" \
    || { echo "FAIL: the policy rejection printed no bounds message (JUS-22/k)" >&2; exit 1; }
echo "PASS: a policy-rejected password re-prompts (JUS-22/k)"

# JUS-22/l: a binary rejection retries interactively (bounded): the first
# create fails, the operator retries once, the second succeeds.
cat >"${SCRATCH}/pty-l.txt" <<'SCRIPT'
EXPECT Admin email
SEND retry@example.com
EXPECT Admin password
SEND Retry-pw123
EXPECT Confirm password
SEND Retry-pw123
EXPECT Panel domain
SEND
EXPECT Could not create the account
EXPECT Admin password
SEND Retry-pw123
EXPECT Confirm password
SEND Retry-pw123
EXPECT-EOF
SCRIPT
L_STATE="${SCRATCH}/admin-state-retry"
mkdir -p "${L_STATE}"
export GOTHAM_INSTALL_ROOT="${SCRATCH}/root-admin-retry"
export ADMIN_STATE_DIR="${L_STATE}"
export ADMIN_CREATE_FAIL_ONCE=1
pty_run "${SCRATCH}/pty-l.txt" >"${SCRATCH}/l-out.log" 2>"${SCRATCH}/l-err.log" \
    || { echo "FAIL: the create-retry run failed (JUS-22/l)" >&2; cat "${SCRATCH}/l-err.log" >&2; exit 1; }
unset ADMIN_CREATE_FAIL_ONCE
tr -d '\r' <"${SCRATCH}/l-out.log" >"${SCRATCH}/l-out.clean"
[ -f "${L_STATE}/users" ] \
    || { echo "FAIL: the retried create produced no account (JUS-22/l)" >&2; exit 1; }
grep -q 'Could not create the account' "${SCRATCH}/l-out.clean" \
    || { echo "FAIL: the rejection was not shown before retrying (JUS-22/l)" >&2; exit 1; }
grep -q 'Account:  retry@example.com' "${SCRATCH}/l-out.clean" \
    || { echo "FAIL: the retried summary misses the account (JUS-22/l)" >&2; exit 1; }
echo "PASS: a rejected create retries interactively (JUS-22/l)"

# JUS-22/l2: the retry is bounded at 3 attempts, then warns and exits nonzero.
cat >"${SCRATCH}/pty-l2.txt" <<'SCRIPT'
EXPECT Admin email
SEND bound@example.com
EXPECT Admin password
SEND Bound-pw123
EXPECT Confirm password
SEND Bound-pw123
EXPECT Panel domain
SEND
EXPECT Admin password
SEND Bound-pw123
EXPECT Confirm password
SEND Bound-pw123
EXPECT Admin password
SEND Bound-pw123
EXPECT Confirm password
SEND Bound-pw123
EXPECT-EOF
SCRIPT
L2_STATE="${SCRATCH}/admin-state-bound"
mkdir -p "${L2_STATE}"
export GOTHAM_INSTALL_ROOT="${SCRATCH}/root-admin-bound"
export ADMIN_STATE_DIR="${L2_STATE}"
export ADMIN_CREATE_FAIL=1
L2_RC=0
pty_run "${SCRATCH}/pty-l2.txt" >"${SCRATCH}/l2-out.log" 2>"${SCRATCH}/l2-err.log" || L2_RC=$?
unset ADMIN_CREATE_FAIL
[ "${L2_RC}" -ne 0 ] \
    || { echo "FAIL: triple rejection exited 0 (JUS-22/l2)" >&2; exit 1; }
tr -d '\r' <"${SCRATCH}/l2-out.log" >"${SCRATCH}/l2-out.clean"
[ "$(grep -c 'Admin password' "${SCRATCH}/l2-out.clean")" -eq 3 ] \
    || { echo "FAIL: the retry was not bounded at 3 prompts (JUS-22/l2)" >&2; exit 1; }
[ ! -e "${L2_STATE}/users" ] \
    || { echo "FAIL: an account was created although every create failed (JUS-22/l2)" >&2; exit 1; }
echo "PASS: the interactive retry is bounded at 3 attempts (JUS-22/l2)"

# JUS-22/m: on a terminal, GOTHAM_ADMIN_EMAIL is the prompt default (empty
# keeps it) and GOTHAM_ADMIN_PASSWORD_FILE is used without prompting.
printf '%s' "File \$ecret2!" >"${SCRATCH}/admin-default-pw.txt"
chmod 600 "${SCRATCH}/admin-default-pw.txt"
cat >"${SCRATCH}/pty-m.txt" <<'SCRIPT'
EXPECT Admin email \[default@example.com\]
SEND
EXPECT Panel domain
SEND  HTTPS://PANEL.EXAMPLE.COM/
EXPECT-EOF
SCRIPT
export DOMAIN_TEST_LOG="${SCRATCH}/prompt-domain.log"
M_STATE="${SCRATCH}/admin-state-default"
mkdir -p "${M_STATE}"
export GOTHAM_INSTALL_ROOT="${SCRATCH}/root-admin-default"
export ADMIN_STATE_DIR="${M_STATE}"
export GOTHAM_ADMIN_EMAIL="default@example.com"
export GOTHAM_ADMIN_PASSWORD_FILE="${SCRATCH}/admin-default-pw.txt"
pty_run "${SCRATCH}/pty-m.txt" >"${SCRATCH}/m-out.log" 2>"${SCRATCH}/m-err.log" \
    || { echo "FAIL: the env-default run failed (JUS-22/m)" >&2; cat "${SCRATCH}/m-err.log" >&2; exit 1; }
unset GOTHAM_ADMIN_EMAIL GOTHAM_ADMIN_PASSWORD_FILE
tr -d '\r' <"${SCRATCH}/m-out.log" >"${SCRATCH}/m-out.clean"
[ -f "${M_STATE}/users" ] \
    || { echo "FAIL: the env-default run created no account (JUS-22/m)" >&2; exit 1; }
[ "$(cat "${M_STATE}/password")" = "File \$ecret2!" ] \
    || { echo "FAIL: the default password file was not used (JUS-22/m)" >&2; exit 1; }
grep -q 'Account:  default@example.com' "${SCRATCH}/m-out.clean" \
    || { echo "FAIL: the default email was not kept (JUS-22/m)" >&2; exit 1; }
if grep -q 'Admin password' "${SCRATCH}/m-out.clean"; then
    echo "FAIL: the password file did not suppress the prompt (JUS-22/m)" >&2
    exit 1
fi
[ "$(cat "${DOMAIN_TEST_LOG}")" = "$(printf 'instance\nset-url\nhttps://panel.example.com')" ] \
    || { echo "FAIL: prompted domain was not normalized" >&2; exit 1; }
grep -q 'within 2 minutes' "${SCRATCH}/m-out.log"
unset DOMAIN_TEST_LOG
echo "PASS: env values are prompt defaults on a terminal (JUS-22/m)"

# JUS-22/n: a group/world-readable password file warns but still works.
printf '%s' 'Perm-warn-pw1' >"${SCRATCH}/admin-open-pw.txt"
chmod 644 "${SCRATCH}/admin-open-pw.txt"
N_STATE="${SCRATCH}/admin-state-perms"
mkdir -p "${N_STATE}"
ADMIN_EMAIL="perms@example.com" ADMIN_PWFILE="${SCRATCH}/admin-open-pw.txt" ADMIN_TTY="" \
    ADMIN_CREATE_FAIL=0 ADMIN_CREATE_FAIL_ONCE=0 ADMIN_FLIP_FLOP=0 ADMIN_EXISTS_FAIL=0 \
    run_admin_install "${SCRATCH}/root-admin-perms" "${N_STATE}" "${SCRATCH}/n-out.log" "${SCRATCH}/n-err.log" \
    || { echo "FAIL: the readable-pwfile run failed (JUS-22/n)" >&2; cat "${SCRATCH}/n-err.log" >&2; exit 1; }
[ -f "${N_STATE}/users" ] \
    || { echo "FAIL: the readable-pwfile run created no account (JUS-22/n)" >&2; exit 1; }
grep -q 'readable by group/others' "${SCRATCH}/n-out.log" \
    || { echo "FAIL: no group/world-readable warning (JUS-22/n)" >&2; exit 1; }
echo "PASS: a group/world-readable password file warns but works (JUS-22/n)"

# JUS-22/o: losing the first-account race reports a likely takeover instead
# of the generic failure (the binary guard stays atomic: still one account).
O_STATE="${SCRATCH}/admin-state-flip"
mkdir -p "${O_STATE}"
O_RC=0
ADMIN_EMAIL="race@example.com" ADMIN_PWFILE="" ADMIN_TTY="" ADMIN_FLIP_FLOP=1 \
    run_admin_install "${SCRATCH}/root-admin-flip" "${O_STATE}" "${SCRATCH}/o-out.log" "${SCRATCH}/o-err.log" || O_RC=$?
[ "${O_RC}" -ne 0 ] \
    || { echo "FAIL: the lost race exited 0 (JUS-22/o)" >&2; exit 1; }
grep -q 'created by someone else' "${SCRATCH}/o-err.log" \
    || { echo "FAIL: the lost race names no takeover (JUS-22/o)" >&2; cat "${SCRATCH}/o-err.log" >&2; exit 1; }
echo "PASS: a lost first-account race reports a likely takeover (JUS-22/o)"

# JUS-22/x: sh -x never prints the password (xtrace disabled around secrets).
printf '%s' "Xtrace \$ecret1!" >"${SCRATCH}/admin-xtrace-pw.txt"
chmod 600 "${SCRATCH}/admin-xtrace-pw.txt"
X_STATE="${SCRATCH}/admin-state-xtrace"
mkdir -p "${X_STATE}"
ADMIN_EMAIL="xtrace@example.com" ADMIN_PWFILE="${SCRATCH}/admin-xtrace-pw.txt" ADMIN_TTY="" ADMIN_XTRACE=1 \
    run_admin_install "${SCRATCH}/root-admin-xtrace" "${X_STATE}" "${SCRATCH}/x-out.log" "${SCRATCH}/x-err.log" \
    || { echo "FAIL: the xtrace run failed (JUS-22/x)" >&2; cat "${SCRATCH}/x-err.log" >&2; exit 1; }
[ -f "${X_STATE}/users" ] \
    || { echo "FAIL: the xtrace run created no account (JUS-22/x)" >&2; exit 1; }
if grep -qF -- "Xtrace \$ecret1!" "${SCRATCH}/x-out.log" "${SCRATCH}/x-err.log"; then
    echo "FAIL: sh -x printed the password (JUS-22/x)" >&2
    exit 1
fi
grep -q '^+' "${SCRATCH}/x-err.log" \
    || { echo "FAIL: xtrace produced no trace lines at all (JUS-22/x)" >&2; exit 1; }
echo "PASS: sh -x never prints the password (JUS-22/x)"

# The real agent requires the bootstrap token even though it trusts the CA.
# Validate the root-only file handoff at agent invocation, after CP startup.
cp "${SCRATCH}/ok-agent.sh" "${SCRATCH}/old-ok-agent.sh"
cat >"${SCRATCH}/ok-agent.sh" <<'SHIM'
#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
    case "$1" in
        --enrollment-token-file) token=$2; shift ;;
    esac
    shift
done
[ -n "${token:-}" ] || { echo "missing enrollment token file" >&2; exit 1; }
[ "${token}" != "${GOTHAM_INSTALL_ROOT:?}/var/lib/gotham/ca/enrollment-token" ]
[ "$(stat -c %a "${token}")" = 600 ]
[ "$(stat -c %u "${token}")" = "$(id -u)" ]
cmp -s "${token}" "${GOTHAM_INSTALL_ROOT:?}/var/lib/gotham/ca/enrollment-token"
# The bootstrap token must not be inherited as an environment value.
[ -z "${GOTHAM_AGENT_ENROLLMENT_TOKEN:-}" ]
printf '%s\n' "$token" >"${ENROLLMENT_HANDOFF_LOG:?}"
SHIM
export ENROLLMENT_HANDOFF_LOG="${SCRATCH}/enrollment-handoff.log"
export ENROLLMENT_TEST_DELAY=1
mkdir "${SCRATCH}/enrollment-admin"
ADMIN_EMAIL=enroll@example.com ADMIN_TTY="" ADMIN_PWFILE="" ADMIN_XTRACE=1 \
    run_admin_install "${SCRATCH}/enrollment-root" "${SCRATCH}/enrollment-admin" \
    "${SCRATCH}/enrollment-out.log" "${SCRATCH}/enrollment-err.log" \
    || { cat "${SCRATCH}/enrollment-err.log" >&2; exit 1; }
[ -s "${ENROLLMENT_HANDOFF_LOG}" ]
if grep -qF -- enrollment-fixture-secret "${SCRATCH}/enrollment-out.log" "${SCRATCH}/enrollment-err.log"; then
    echo "FAIL: enrollment token leaked to installer logs" >&2; exit 1
fi
unset ENROLLMENT_HANDOFF_LOG ENROLLMENT_TEST_DELAY
mv "${SCRATCH}/old-ok-agent.sh" "${SCRATCH}/ok-agent.sh"
echo "PASS: localhost enrollment uses a private token file after CP startup without logging the token"

# Run the standalone tail with the existing command-binary seam. Domain
# failures must leave the successful install and its host HTTP login intact.
INSTALL_SH="${SCRATCH}/standalone/install.sh"
export DOMAIN_TEST_LOG="${SCRATCH}/domain-command.log"
export GOTHAM_DOMAIN=' HTTPS://PANEL.EXAMPLE.COM/ '
mkdir "${SCRATCH}/domain-admin"
ADMIN_EMAIL=domain@example.com ADMIN_TTY="" ADMIN_PWFILE="" \
    run_admin_install "${SCRATCH}/domain-root" "${SCRATCH}/domain-admin" \
    "${SCRATCH}/domain-out.log" "${SCRATCH}/domain-err.log"
[ "$(cat "${DOMAIN_TEST_LOG}")" = "$(printf 'instance\nset-url\nhttps://panel.example.com')" ] \
    || { echo "FAIL: standalone domain command arguments differ" >&2; exit 1; }
grep -q 'Web UI:   http://panel.example.com' "${SCRATCH}/domain-out.log"
grep -q 'DNS-only' "${SCRATCH}/domain-out.log"
export DOMAIN_TEST_RC=1
export GOTHAM_DOMAIN=invalid-domain
mkdir "${SCRATCH}/bad-domain-admin"
ADMIN_EMAIL=domain@example.com ADMIN_TTY="" ADMIN_PWFILE="" \
    run_admin_install "${SCRATCH}/bad-domain-root" "${SCRATCH}/bad-domain-admin" \
    "${SCRATCH}/bad-domain-out.log" "${SCRATCH}/bad-domain-err.log"
grep -q 'WARNING: domain setup failed' "${SCRATCH}/bad-domain-out.log"
grep -q 'Web UI:   http://.*:8000' "${SCRATCH}/bad-domain-out.log"
[ -f "${SCRATCH}/bad-domain-root/etc/gotham/gotham.env" ] \
    || { echo "FAIL: failed domain rolled back the installation" >&2; exit 1; }
rm -f "${DOMAIN_TEST_LOG}"
export DOMAIN_WAIT_RC=1
mkdir "${SCRATCH}/not-ready-admin"
ADMIN_EMAIL=domain@example.com ADMIN_TTY="" ADMIN_PWFILE="" \
    run_admin_install "${SCRATCH}/not-ready-root" "${SCRATCH}/not-ready-admin" \
    "${SCRATCH}/not-ready-out.log" "${SCRATCH}/not-ready-err.log"
[ ! -e "${DOMAIN_TEST_LOG}" ] || { echo "FAIL: set-url ran before agent readiness" >&2; exit 1; }
grep -q 'WARNING: domain setup failed' "${SCRATCH}/not-ready-out.log"
grep -q 'Web UI:   http://.*:8000' "${SCRATCH}/not-ready-out.log"
unset DOMAIN_WAIT_RC
echo "PASS: agent readiness failure skips domain setup and preserves HTTP"
unset DOMAIN_TEST_LOG DOMAIN_TEST_RC GOTHAM_DOMAIN
echo "PASS: standalone domain success and failure preserve the install and login summary"

echo "ALL RELEASE-INSTALL TESTS PASSED"