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
        gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service; do
        ln -sf "${SCRIPT_DIR}/${_sib}" "$(dirname "$1")/${_sib}"
    done
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
sh "${INSTALL_SH}" --help 2>&1 | grep -q -- '--no-local-agent' \
    || { echo "FAIL: install.sh --help does not document --no-local-agent" >&2; exit 1; }
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
    test-release-install.sh verify-systemd.sh verify-agent-update.sh; do
    [ -x "${SCRIPT_DIR}/${script}" ] \
        || { echo "FAIL: ${script} is not executable (chmod +x it)" >&2; exit 1; }
done
if git -C "${REPO_DIR}" rev-parse --git-dir >/dev/null 2>&1; then
    for script in install.sh install-agent.sh install-sudoers.sh install-agent-sudoers.sh; do
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
dry_run --no-local-agent >"${SCRATCH}/dry-no-agent.log" 2>&1
grep -q 'skipping the localhost agent install (--no-local-agent)' "${SCRATCH}/dry-no-agent.log" \
    || { echo "FAIL: --no-local-agent --dry-run does not show the skip" >&2; exit 1; }
if grep -q 'install-agent.sh --full' "${SCRATCH}/dry-no-agent.log"; then
    echo "FAIL: --no-local-agent still schedules the agent install" >&2
    exit 1
fi
echo "PASS: --dry-run shows the localhost agent install and its --no-local-agent skip (JUS-5)"

# ---- R1: unsupported platform skips the agent; a failed agent step keeps CP -
# An unsupported platform (non-Ubuntu/Debian here) skips the localhost agent
# with a notice instead of failing the install, and schedules no agent step.
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/dry-skip-arch.log" 2>&1
grep -q 'skipping the localhost agent install' "${SCRATCH}/dry-skip-arch.log" \
    || { echo "FAIL: unsupported platform did not skip the localhost agent (R1)" >&2; cat "${SCRATCH}/dry-skip-arch.log" >&2; exit 1; }
grep -q 'supports Ubuntu/Debian only' "${SCRATCH}/dry-skip-arch.log" \
    || { echo "FAIL: the skip notice does not name the supported distros (R1)" >&2; exit 1; }
if grep -q 'install-agent.sh --full' "${SCRATCH}/dry-skip-arch.log"; then
    echo "FAIL: unsupported platform still schedules the agent install (R1)" >&2
    exit 1
fi
echo "PASS: unsupported platform skips the localhost agent with a notice (R1)"
# An unsupported architecture skips the same way (seam: GOTHAM_TEST_UNAME_M).
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
GOTHAM_TEST_UNAME_M=sparc64 \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/dry-skip-cpu.log" 2>&1
grep -q 'skipping the localhost agent install: unsupported architecture' "${SCRATCH}/dry-skip-cpu.log" \
    || { echo "FAIL: unsupported architecture did not skip the localhost agent (R1)" >&2; exit 1; }
if grep -q 'install-agent.sh --full' "${SCRATCH}/dry-skip-cpu.log"; then
    echo "FAIL: unsupported architecture still schedules the agent install (R1)" >&2
    exit 1
fi
echo "PASS: unsupported architecture skips the localhost agent (R1)"
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

# ---- R2: re-run with a pre-existing agent.env never repoints the agent ------
# GOTHAM_AGENT_NODE_ID / GOTHAM_AGENT_CP_ADDR are passed only when agent.env
# does not already define them, so the dry-run line omits them when the file
# has them (kept), shows the defaults when it does not, and honours an
# explicit override. GOTHAM_AGENT_ENV_FILE points the lookup at scratch
# (test seam, honoured with GOTHAM_INSTALL_TEST=1 here).
R2_ENV="${SCRATCH}/prior-agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442\nGOTHAM_AGENT_NODE_ID=kept-node\n' >"${R2_ENV}"
dry_run_agent_line() {
    GOTHAM_AGENT_ENV_FILE="$1" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
    PATH="${SYS_SHIM}:${PATH}" \
        sh "${INSTALL_SH}" --dry-run 2>/dev/null | grep 'install-agent.sh --full' || true
}
PRIOR_LINE="$(dry_run_agent_line "${R2_ENV}")"
[ -n "${PRIOR_LINE}" ] || { echo "FAIL: no agent step scheduled with a prior agent.env (R2)" >&2; exit 1; }
case "${PRIOR_LINE}" in
    *GOTHAM_AGENT_NODE_ID* | *GOTHAM_AGENT_CP_ADDR*)
        echo "FAIL: re-run passes NODE_ID/CP_ADDR although agent.env defines them (R2): ${PRIOR_LINE}" >&2
        exit 1
        ;;
esac
echo "PASS: re-run with a pre-existing agent.env passes neither NODE_ID nor CP_ADDR (R2)"
FRESH_LINE="$(dry_run_agent_line "${SCRATCH}/no-such-agent.env")"
case "${FRESH_LINE}" in
    *GOTHAM_AGENT_NODE_ID=*-agent*GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442*)
        echo "PASS: fresh install still passes the default node id and loopback address (R2)"
        ;;
    *)
        echo "FAIL: fresh install does not pass the defaults (R2): ${FRESH_LINE}" >&2
        exit 1
        ;;
esac
EXPLICIT_LINE="$(GOTHAM_AGENT_NODE_ID=explicit-override dry_run_agent_line "${R2_ENV}")"
case "${EXPLICIT_LINE}" in
    *GOTHAM_AGENT_NODE_ID=explicit-override*)
        case "${EXPLICIT_LINE}" in
            *GOTHAM_AGENT_CP_ADDR*)
                echo "FAIL: explicit NODE_ID leaked in CP_ADDR too (R2): ${EXPLICIT_LINE}" >&2
                exit 1
                ;;
        esac
        echo "PASS: an explicit GOTHAM_AGENT_NODE_ID still wins over agent.env (R2)"
        ;;
    *)
        echo "FAIL: explicit GOTHAM_AGENT_NODE_ID did not win (R2): ${EXPLICIT_LINE}" >&2
        exit 1
        ;;
esac

# ---- Remote agent.env: a re-run leaves another control plane's agent alone -
# When the prior agent.env points at a remote control plane, the localhost
# step is skipped entirely: --ca must not overwrite that host's ca.crt and
# its agent config must not be repointed. An explicit address this run still
# repoints deliberately.
R2R_ENV="${SCRATCH}/remote-agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=remote.example.com:9443\nGOTHAM_AGENT_NODE_ID=remote-node\n' >"${R2R_ENV}"
GOTHAM_AGENT_ENV_FILE="${R2R_ENV}" \
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/dry-remote.log" 2>&1
grep -q 'leaving it untouched' "${SCRATCH}/dry-remote.log" \
    || { echo "FAIL: a remote agent.env was not left untouched" >&2; cat "${SCRATCH}/dry-remote.log" >&2; exit 1; }
if grep -q 'install-agent.sh --full' "${SCRATCH}/dry-remote.log"; then
    echo "FAIL: a remote agent.env still schedules the agent install" >&2
    exit 1
fi
echo "PASS: a remote agent.env skips the localhost step untouched"
REMOTE_EXPLICIT="$(GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442 dry_run_agent_line "${R2R_ENV}")"
[ -n "${REMOTE_EXPLICIT}" ] \
    || { echo "FAIL: an explicit CP_ADDR did not repoint a remote agent.env" >&2; exit 1; }
echo "PASS: an explicit GOTHAM_AGENT_CP_ADDR still repoints deliberately"

# ---- L2: only loopback literals count as local ------------------------------
# 127.evil.example.com is NOT local (it merely starts with 127.); only
# numeric 127.0.0.0/8, localhost and ::1 keep the localhost step, everything
# else is left untouched like a remote control plane. All values here are
# host:port: a bare 127.0.0.1 never reaches this classifier — the agent
# itself refuses a port-less dial address, so the installer rejects it up
# front (covered in C2).
L2_ENV="${SCRATCH}/l2-agent.env"
l2_case() {
    # $1 addr, $2 want (local/remote)
    printf 'GOTHAM_AGENT_CP_ADDR=%s\nGOTHAM_AGENT_NODE_ID=l2-node\n' "$1" >"${L2_ENV}"
    GOTHAM_AGENT_ENV_FILE="${L2_ENV}" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
    PATH="${SYS_SHIM}:${PATH}" \
        sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/l2.log" 2>&1
    if [ "$2" = "local" ]; then
        grep -q 'install-agent.sh --full' "${SCRATCH}/l2.log" \
            || { echo "FAIL: loopback ${1} skipped the localhost step (L2)" >&2; exit 1; }
        if grep -q 'leaving it untouched' "${SCRATCH}/l2.log"; then
            echo "FAIL: loopback ${1} treated as a remote control plane (L2)" >&2
            exit 1
        fi
    else
        grep -q 'leaving it untouched' "${SCRATCH}/l2.log" \
            || { echo "FAIL: non-loopback ${1} was not left untouched (L2)" >&2; cat "${SCRATCH}/l2.log" >&2; exit 1; }
        if grep -q 'install-agent.sh --full' "${SCRATCH}/l2.log"; then
            echo "FAIL: non-loopback ${1} still schedules the agent install (L2)" >&2
            exit 1
        fi
    fi
}
for _local in 127.0.0.1:9442 127.1.2.3:9442 localhost:9443 \
    LOCALHOST:9443 '[::1]:9443'; do
    l2_case "${_local}" local
done
for _remote in 127.evil.example.com:9443 remote.example.com:9443 \
    128.0.0.1:9442 192.168.1.1:9442 127.0.0.300:9442 10.0.0.1:9442; do
    l2_case "${_remote}" remote
done
echo "PASS: only loopback literals count as local (127.evil.example.com is remote) (L2)"

# ---- Retry quoting: values with spaces/specials stay one re-runnable word --
# (A node id can no longer hold a space — the installer mirrors the agent's
# own gate — so this schedules an agent-valid but still quoting-hostile id.)
QUOTED_LINE="$(GOTHAM_AGENT_NODE_ID="odd;\$(id>xq);a=b" dry_run_agent_line "${SCRATCH}/no-such-agent.env")"
case "${QUOTED_LINE}" in
    *"GOTHAM_AGENT_NODE_ID='odd;\$(id>xq);a=b'"*)
        echo "PASS: the retry command shell-quotes values with spaces/specials"
        ;;
    *)
        echo "FAIL: the retry command is not safely quoted: ${QUOTED_LINE}" >&2
        exit 1
        ;;
esac

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
GOTHAM_AGENT_ENV_FILE="${R2R_ENV}" \
GOTHAM_APT_ROOT="${SCRATCH}/hostile-apt-root" \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/hostile-agent.sh" \
GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
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
    GOTHAM_APT_ROOT GOTHAM_INSTALL_TEST_AGENT_SCRIPT GOTHAM_INSTALL_TEST_RUN_AGENT
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
grep -q 'systemctl enable --now gotham.service' "${SCRATCH}/r3-systemctl.log" \
    || { echo "FAIL: the control plane service was not enabled before the agent failure (R3)" >&2; exit 1; }
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
GOTHAM_AGENT_ENV_FILE="${R2R_ENV}" \
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
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT GOTHAM_INSTALL_TEST_PUBLIC_KEY; do
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

# ---- M2: a metachar node id reaches the agent step verbatim -----------------
# The agent step must not word-split its assignments: a node id holding
# $(), ;, = and > (spaces, quotes, * and / are refused up front now — the
# installer mirrors the agent's own gate, and quotes are refused because
# systemd's EnvironmentFile parser would strip or regroup them, proven on
# Ubuntu 22.04 systemd 249) is passed as one env value, no command in it
# runs, and no glob expands. The fake captures what it receives. The
# execution probe stays slash- and space-free so the id itself is valid: if
# it were ever expanded, ./m2-pwned-jus12 would appear in the suite's
# working directory.
echo "==> metachar node id passes through the agent step verbatim (M2)"
M2_ROOT="${SCRATCH}/root-m2"
M2_PWNED="m2-pwned-jus12"
M2_CAPTURE="${SCRATCH}/m2-captured-node-id"
M2_NODE="odd;\$(id>${M2_PWNED});a=b"
cat >"${SCRATCH}/capture-agent.sh" <<'SHIM'
#!/bin/sh
printf '%s' "${GOTHAM_AGENT_NODE_ID}" >"${CAPTURE:?}"
exit 0
SHIM
chmod +x "${SCRATCH}/capture-agent.sh"
rm -f "${M2_PWNED}" "${M2_CAPTURE}"
: >"${SCRATCH}/m2-systemctl.log"
# Same baked-copy mechanism as R3: fixed paths on a real run.
M2_INSTALLER="${SCRATCH}/install-m2.sh"
bake_installer "${M2_INSTALLER}" "${FIXTURES}/ubuntu-release" "${SCRATCH}/m2-agent.env"
M2_RC=0
GOTHAM_AGENT_NODE_ID="${M2_NODE}" \
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
GOTHAM_INSTALL_ROOT="${M2_ROOT}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/capture-agent.sh" \
SYSTEMCTL_LOG="${SCRATCH}/m2-systemctl.log" \
CAPTURE="${M2_CAPTURE}" \
PATH="${R3_SHIM}:${PATH}" \
    sh "${M2_INSTALLER}" >"${SCRATCH}/m2-out.log" 2>"${SCRATCH}/m2-err.log" || M2_RC=$?
[ "${M2_RC}" -eq 0 ] \
    || { echo "FAIL: the install failed although the agent step succeeded (M2)" >&2; cat "${SCRATCH}/m2-err.log" >&2; exit 1; }
[ -f "${M2_CAPTURE}" ] \
    || { echo "FAIL: the agent step never ran (M2)" >&2; exit 1; }
printf '%s' "${M2_NODE}" >"${SCRATCH}/m2-expected"
cmp -s "${SCRATCH}/m2-expected" "${M2_CAPTURE}" \
    || { echo "FAIL: the node id did not arrive verbatim (M2)" >&2; echo "want: ${M2_NODE}" >&2; echo "got:  $(cat "${M2_CAPTURE}")" >&2; exit 1; }
[ ! -e "${M2_PWNED}" ] \
    || { echo "FAIL: a command inside the node id was executed (M2)" >&2; exit 1; }
rm -f "${M2_PWNED}"
echo "PASS: a metachar node id arrives verbatim and nothing in it runs (M2)"

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

# ---- C2b: a skipped agent step is never blocked by agent values ------------
# The skip decisions (distro, arch, systemd, a remote prior agent.env,
# --no-local-agent) are computed before the first mutation, and the agent
# validation runs only when the agent step will run. Each case below carries
# a would-be-invalid agent value (AUTO_UPDATE=yes) that aborted the install
# before this fix; now the install succeeds and the agent step stays
# skipped. --no-local-agent behaviour is unchanged (still skipped, still
# green).
echo "==> a skipped agent step is never blocked by agent values (C2b)"
_c2b_must_skip() { # $1 case name, $2 os-release fixture, $3 extra agent setup
    C2B_RC=0
    # shellcheck disable=SC2086  # $3 is an intentional extra env prefix
    env $3 \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_OS_RELEASE_FILE="$2" \
    PATH="${SYS_SHIM}:${PATH}" \
        sh "${INSTALL_SH}" --dry-run >"${SCRATCH}/c2b-$1.log" 2>&1 || C2B_RC=$?
    [ "${C2B_RC}" -eq 0 ] \
        || { echo "FAIL: a skipped agent step was blocked by agent values (C2b/$1)" >&2; cat "${SCRATCH}/c2b-$1.log" >&2; exit 1; }
    if grep -q 'install-agent.sh --full' "${SCRATCH}/c2b-$1.log"; then
        echo "FAIL: the agent step ran although it must stay skipped (C2b/$1)" >&2
        exit 1
    fi
}
# Remote agent.env holding a would-be-invalid value: untouched, install green.
C2B_REMOTE="${SCRATCH}/c2b-remote-agent.env"
printf 'GOTHAM_AGENT_CP_ADDR=remote.example.com:9443\nGOTHAM_AGENT_NODE_ID=remote-node\nGOTHAM_AGENT_AUTO_UPDATE=yes\n' >"${C2B_REMOTE}"
C2B_SUM_BEFORE="$(sha "${C2B_REMOTE}")"
_c2b_must_skip remote-invalid "${FIXTURES}/ubuntu-release" "GOTHAM_AGENT_ENV_FILE=${C2B_REMOTE} GOTHAM_AGENT_AUTO_UPDATE=yes"
grep -q 'leaving it untouched' "${SCRATCH}/c2b-remote-invalid.log" \
    || { echo "FAIL: a remote agent.env with a bad value was not left untouched (C2b)" >&2; exit 1; }
[ "$(sha "${C2B_REMOTE}")" = "${C2B_SUM_BEFORE}" ] \
    || { echo "FAIL: the remote agent.env was modified (C2b)" >&2; exit 1; }
echo "PASS: a remote agent.env with a would-be-invalid value stays untouched and green (C2b)"
# Unsupported distro plus a bad ambient agent value: skipped, install green.
_c2b_must_skip distro-invalid "${FIXTURES}/arch-release" "GOTHAM_AGENT_AUTO_UPDATE=yes GOTHAM_AGENT_ENV_FILE=${SCRATCH}/no-such-c2b-agent.env"
grep -q 'supports Ubuntu/Debian only' "${SCRATCH}/c2b-distro-invalid.log" \
    || { echo "FAIL: the distro skip notice is missing (C2b)" >&2; exit 1; }
echo "PASS: an unsupported distro plus a bad ambient agent value stays green (C2b)"
# --no-local-agent with a bad ambient agent value: unchanged skip, green.
C2B_NLAG="--no-local-agent"
C2B_RC=0
env GOTHAM_AGENT_AUTO_UPDATE=yes \
GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
GOTHAM_VERSION="${VERSION}" \
GOTHAM_SKIP_DEPS=1 \
GOTHAM_INSTALL_TEST=1 \
GOTHAM_OS_RELEASE_FILE="${FIXTURES}/ubuntu-release" \
PATH="${SYS_SHIM}:${PATH}" \
    sh "${INSTALL_SH}" --dry-run "${C2B_NLAG}" >"${SCRATCH}/c2b-nolocal.log" 2>&1 || C2B_RC=$?
[ "${C2B_RC}" -eq 0 ] \
    || { echo "FAIL: --no-local-agent was blocked by an agent value (C2b)" >&2; exit 1; }
grep -q 'skipping the localhost agent install (--no-local-agent)' "${SCRATCH}/c2b-nolocal.log" \
    || { echo "FAIL: --no-local-agent skip message changed (C2b)" >&2; exit 1; }
echo "PASS: --no-local-agent with a bad ambient agent value stays green (C2b)"

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
_c2c_must_accept dsn-pct GOTHAM_DATABASE_DSN 'postgres://gotham:p%40ss%3Aword@localhost:5432/gotham?sslmode=disable'
_c2c_must_accept redis-url GOTHAM_REDIS_ADDR 'rediss://redis.internal:6380'
echo "PASS: @, %-escaped and rediss:// DSN/Redis values are accepted by name (C2c)"

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
rm -f "${RR_AGENT_ENV}"
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
        gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service; do
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
    rm -f "${SCRATCH}/rr-agent-ran"
    : >"${SCRATCH}/rr-systemctl.log"
    RR_RC=0
    TMPDIR="${RR_BOX}/tmp" \
    SYSTEMCTL_LOG="${SCRATCH}/rr-systemctl.log" \
    AGENT_MARKER="${SCRATCH}/rr-agent-ran" \
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_SKIP_DEPS=1 \
    GOTHAM_INSTALL_TEST=1 \
    GOTHAM_INSTALL_TEST_RUN_AGENT=1 \
    GOTHAM_INSTALL_TEST_AGENT_SCRIPT="${SCRATCH}/rr-agent.sh" \
    GOTHAM_OS_RELEASE_FILE="${FIXTURES}/arch-release" \
    GOTHAM_TEST_UNAME_M=sparc64 \
    GOTHAM_AGENT_ENV_FILE="${SCRATCH}/rr-agent.env" \
    GOTHAM_APT_ROOT="${SCRATCH}/rr-hostile-apt" \
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
    if grep -q 'hostile-apt-root\|sparc64\|arch-release' "${SCRATCH}/rr-out.log" "${SCRATCH}/rr-err.log"; then
        echo "FAIL: a hostile seam value leaked into the real run (R4)" >&2
        exit 1
    fi
    echo "PASS: a real run reaches the agent step on the real script and never runs the seam fake (R4)"

echo "ALL RELEASE-INSTALL TESTS PASSED"