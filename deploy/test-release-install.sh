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
#   - fail-closed: tampered artifact, tampered manifest, pinned-key mismatch.
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
        sh "${SCRIPT_DIR}/install.sh" "$@"
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
        sh "${SCRIPT_DIR}/install.sh" "$@"
}

# run_install_at installs into an arbitrary GOTHAM_INSTALL_ROOT (test mode).
run_install_at() {
    GOTHAM_BASE_URL="http://127.0.0.1:${PORT}" \
    GOTHAM_VERSION="${VERSION}" \
    GOTHAM_INSTALL_TEST_PUBLIC_KEY="${PUB_B64}" \
    GOTHAM_INSTALL_ROOT="$1" \
    GOTHAM_SKIP_DEPS=1 \
        sh "${SCRIPT_DIR}/install.sh"
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
for installer in "${SCRIPT_DIR}/install.sh" "${AGENT_INSTALLER}"; do
    grep -q 'require_cmd visudo' "${installer}" \
        || { echo "FAIL: ${installer} does not require visudo" >&2; exit 1; }
done
echo "PASS: sudoers hardening present (B2)"
echo "PASS: happy-path install verified and rendered"

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
for installer in "${SCRIPT_DIR}/install.sh" "${AGENT_INSTALLER}"; do
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
    sh "${SCRIPT_DIR}/install.sh" >/dev/null
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
    sh "${SCRIPT_DIR}/install.sh" --dry-run >/dev/null
[ ! -e "${DRY_ROOT}" ] || { echo "FAIL: --dry-run created files" >&2; exit 1; }
echo "PASS: --dry-run made no changes"

echo "ALL RELEASE-INSTALL TESTS PASSED"