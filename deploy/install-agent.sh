#!/bin/sh
#
# install-agent.sh installs the Gotham node agent on a Linux host.
#
# It downloads the latest release binary for the host architecture, installs it
# to the service StateDirectory (/var/lib/gotham-agent/bin/gotham-agent), writes
# /etc/gotham/agent.env from the current environment, installs the privileged
# update wrapper + sudoers rule, creates the gotham-agent system user and
# enables the systemd unit.
#
# Re-running it is a reinstall: keys left unset keep the value the previous run
# wrote (so the control plane address and node id survive), operator-added keys
# are preserved, and a unit that is already running is restarted onto the newly
# installed binary.
#
# The release binary is downloaded with the same signed-manifest verification as
# the control-plane installer (deploy/release-verify.sh): the Ed25519 signature
# of gotham-agent-manifest-<arch>.txt is checked against the embedded release
# public key, then the artifact digest against the signed sha256. A failed
# verification aborts; nothing is trusted on first use.
#
# Override the release for testing with GOTHAM_VERSION, GOTHAM_RELEASES_URL or
# GOTHAM_BASE_URL.
#
# mTLS: the agent verifies the control plane with the CA certificate. This
# installer fails closed unless a CA is supplied, so a fresh install never
# dials the agent channel in plaintext. Supply it with --ca <path> (or
# GOTHAM_AGENT_CA_FILE=<path>); the file is installed as /etc/gotham/ca.crt and
# GOTHAM_AGENT_CA is written to agent.env. Copy it from the control plane:
#   scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
#   sudo ./install-agent.sh --ca ./ca.crt
# --insecure is a local-development-only escape hatch: it skips the CA check and
# leaves GOTHAM_AGENT_CA unset, so the agent connects without TLS. Never use it
# on a real node.
#
# Enrollment token: the bootstrap credential a node presents on its first
# registration. Prefer --enrollment-token-file <path> with a root-owned file
# (mode 0600, no symlink, not a directory) — the installer reads the value and
# never echoes it. The environment variable GOTHAM_AGENT_ENROLLMENT_TOKEN also
# works, but never as `sudo GOTHAM_AGENT_ENROLLMENT_TOKEN=<token> ...`: that puts
# the secret in sudo's argv (/proc/<pid>/cmdline, `ps`) and in shell history.
# Read it without echo and export it for `sudo -E`, or use the file. The value is
# written to agent.env as GOTHAM_AGENT_ENROLLMENT_TOKEN (a secret: keep that file
# at its 0640 root-owned mode). The agent sends it only until it holds a
# certificate, over a CA-pinned TLS channel only, after which the operator may
# delete it from agent.env.
#
# Docker Engine and the compose plugin (which the agent shells out to) are
# required at runtime but are NOT installed by default: pass --full (or set
# GOTHAM_AGENT_FULL=1) to install them from the official Docker apt repository
# (Ubuntu/Debian only; other distros fail with a message naming the manual
# step). The repository key is fingerprint-pinned before use and every package
# is apt-verified; nothing is piped to a shell. Re-running --full is a no-op
# when Docker already works. The control-plane installer enables --full for its
# localhost agent automatically.
#
# Usage:
#   sudo ./install-agent.sh [--ca <path>] [--enrollment-token-file <path>]
#                           [--insecure] [--full] [--dry-run]
#                           [--file-manager | --no-file-manager]
#
# Environment variables written to /etc/gotham/agent.env. A key left unset keeps
# the value a previous install wrote (or the agent's built-in default on a fresh
# install):
#   GOTHAM_AGENT_CP_ADDR
#   GOTHAM_AGENT_NODE_ID
#   GOTHAM_AGENT_LISTEN_ADDR
#   GOTHAM_AGENT_CA            (set to /etc/gotham/ca.crt when --ca is used)
#   GOTHAM_AGENT_INSECURE      (set to true only with --insecure)
#   GOTHAM_AGENT_CERT_DIR
#   GOTHAM_AGENT_KEY
#   GOTHAM_AGENT_DOCKER_SOCK
#   GOTHAM_AGENT_LOG_LEVEL
#   GOTHAM_AGENT_AUTO_UPDATE
#   GOTHAM_AGENT_UPDATE_INTERVAL
#   GOTHAM_AGENT_UPDATE_CHANNEL
#   GOTHAM_AGENT_ENROLLMENT_TOKEN  (from the environment or --enrollment-token-file; a secret)
#   GOTHAM_AGENT_FILES         (true/false; written from the file-manager answer)
#
# File manager (remote file browser/editor in the web UI): a direct run asks
# whether you really want it (default: no; the control-plane installer enables
# it for its local node without asking). Answering yes lets the control plane's
# owner browse, read, edit, upload and delete ANY file on this node, because the
# agent then runs as root (systemd drop-in file-manager.conf). Answer without a
# prompt with --file-manager / --no-file-manager or
# GOTHAM_AGENT_FILE_MANAGER=yes|no; without a terminal the answer is no. A re-run
# keeps the previous answer.
#
# Read once at startup (never written to agent.env):
#   GOTHAM_AGENT_FULL          (set to 1 for the --full Docker setup)

set -eu
# Permissive base umask so shared directories stay world-traversable and the
# agent user can read its env; a restrictive umask is applied only around the
# env/secret write below.
umask 022

BINARY_NAME="gotham-agent"
INSTALL_PATH="/var/lib/gotham-agent/bin/gotham-agent"
ENV_DIR="/etc/gotham"
ENV_FILE="${ENV_DIR}/agent.env"
SERVICE_FILE="/etc/systemd/system/gotham-agent.service"
STATE_DIR="/var/lib/gotham-agent"
DOCKER_CONFIG_DIR="${STATE_DIR}/.docker"
STATUS_DIR="/var/lib/gotham-agent-updater"
WRAPPER_PATH="/usr/libexec/gotham/gotham-agent-update"
WRAPPER_CONF="/etc/gotham/agent-updater.conf"
TOOLCTL_PATH="/usr/libexec/gotham/gotham-toolctl"
SERVICE_USER="gotham-agent"
DEFAULT_REPO="justindeelux/gotham"
# Release trust anchor: the base64 raw Ed25519 public key (same as install.sh
# and deploy/gotham-signing-key.pub). Never fetched from the download channel.
GOTHAM_RELEASE_PUBLIC_KEY_B64="Yt6nz1gGQWF7Bfc9MCt/gQXbPMzhN9OygrUkOEFYdwQ="
DRY_RUN=0
INSECURE=0
FULL=0
CA_SOURCE=""
ENROLLMENT_TOKEN_FILE=""
# File manager answer: yes | no | "" (ask). The environment form is the default
# for a non-interactive run.
FILE_MANAGER="${GOTHAM_AGENT_FILE_MANAGER:-}"
FILE_MANAGER_DROPIN_DIR="/etc/systemd/system/gotham-agent.service.d"
FILE_MANAGER_DROPIN="${FILE_MANAGER_DROPIN_DIR}/file-manager.conf"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --dry-run)
            DRY_RUN=1
            ;;
        --insecure)
            INSECURE=1
            ;;
        --full)
            FULL=1
            ;;
        --file-manager)
            FILE_MANAGER=yes
            ;;
        --no-file-manager)
            FILE_MANAGER=no
            ;;
        --ca)
            [ "$#" -ge 2 ] || { echo "--ca requires a path" >&2; exit 2; }
            CA_SOURCE="$2"
            shift
            ;;
        --ca=*)
            CA_SOURCE="${1#--ca=}"
            ;;
        --enrollment-token-file)
            [ "$#" -ge 2 ] || { echo "--enrollment-token-file requires a path" >&2; exit 2; }
            ENROLLMENT_TOKEN_FILE="$2"
            shift
            ;;
        --enrollment-token-file=*)
            ENROLLMENT_TOKEN_FILE="${1#--enrollment-token-file=}"
            ;;
        -h | --help)
            # Print every leading comment line (the header), not a fixed range,
            # so the documented env list cannot drift out of --help.
            awk 'NR == 1 { next } /^[^#]/ { exit } { print }' "$0"
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

# run executes a command, or prints it when in dry-run mode.
run() {
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] $*"
        return 0
    fi
    "$@"
}

# log prints a progress step.
log() {
    echo "==> $*"
}

# The installer needs its sibling files (the shared wrapper, the agent wrapper
# config and the sudoers installer). Fail early with a clear message rather than
# aborting after the user has been created and the binary installed.
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
for sibling in release-verify.sh gotham-update.sh gotham-agent-updater.conf install-agent-sudoers.sh install-agent-lib.sh gotham-toolctl.sh install-toolctl-sudoers.sh; do
    if [ ! -f "${SCRIPT_DIR}/${sibling}" ]; then
        echo "install-agent.sh: ${sibling} must be next to this script (run it from the repository checkout)" >&2
        exit 2
    fi
done
# shellcheck source=deploy/release-verify.sh
. "${SCRIPT_DIR}/release-verify.sh"
# shellcheck source=deploy/install-agent-lib.sh
. "${SCRIPT_DIR}/install-agent-lib.sh"

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64)
            echo "amd64"
            ;;
        aarch64 | arm64)
            echo "arm64"
            ;;
        *)
            echo "unsupported architecture: $(uname -m)" >&2
            exit 1
            ;;
    esac
}

if [ "$(id -u)" -ne 0 ] && [ "${DRY_RUN}" -eq 0 ]; then
    echo "install-agent.sh must run as root (try sudo)" >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1 && [ "${DRY_RUN}" -eq 0 ]; then
    echo "systemctl not found; this installer targets systemd hosts" >&2
    exit 1
fi

# ---- mTLS: resolve the control-plane CA before doing anything else ----------
# The agent verifies the control plane against this CA. Without one the agent
# would connect in plaintext, so fail closed here unless --insecure was given.
# CA_SRC is a file to install; AGENT_CA_PATH is the path the agent reads.
CA_SRC="${CA_SOURCE:-${GOTHAM_AGENT_CA_FILE:-}}"
AGENT_CA_PATH=""
if [ -n "${CA_SRC}" ]; then
    [ -f "${CA_SRC}" ] && [ -r "${CA_SRC}" ] \
        || { echo "install-agent.sh: CA certificate ${CA_SRC} is not a readable file" >&2; exit 1; }
    AGENT_CA_PATH="${ENV_DIR}/ca.crt"
elif [ -f "${ENV_FILE}" ]; then
    # Reinstall: keep the CA a previous run configured.
    existing_ca="$(sed -n 's/^GOTHAM_AGENT_CA=//p' "${ENV_FILE}" | head -n1)"
    [ -n "${existing_ca}" ] && AGENT_CA_PATH="${existing_ca}"
fi
if [ -z "${AGENT_CA_PATH}" ] && [ "${INSECURE}" -ne 1 ]; then
    echo "install-agent.sh: no control-plane CA certificate configured." >&2
    echo "  Copy /var/lib/gotham/ca/ca.crt from the control plane and pass --ca <path>," >&2
    echo "  or set GOTHAM_AGENT_CA_FILE=<path>." >&2
    echo "  For local development only, pass --insecure to connect without TLS." >&2
    exit 1
fi
if [ "${INSECURE}" -eq 1 ]; then
    echo "==> WARNING: --insecure: the agent will connect to the control plane WITHOUT TLS (development only)" >&2
fi

# ---- Enrollment token: read it off the command line -------------------------
# The bootstrap credential is a secret; accept it from the environment or a
# root-only file, never as an argv value (which /proc/<pid>/cmdline, `ps` and
# shell history expose). The value is only written to agent.env and never logged
# or echoed. read_enrollment_token_file enforces the root-only file rules.
if [ -n "${ENROLLMENT_TOKEN_FILE}" ]; then
    _as_root=0
    if [ "$(id -u 2>/dev/null)" = "0" ]; then
        _as_root=1
    fi
    GOTHAM_AGENT_ENROLLMENT_TOKEN="$(read_enrollment_token_file "${ENROLLMENT_TOKEN_FILE}" "${_as_root}")" || exit 1
    export GOTHAM_AGENT_ENROLLMENT_TOKEN
    unset _as_root
fi

# --full defaults off; GOTHAM_AGENT_FULL=1 is the environment form (the
# control-plane installer passes --full explicitly for its localhost agent).
if [ "${GOTHAM_AGENT_FULL:-0}" = "1" ]; then
    FULL=1
fi

# ---- File manager: ask before enabling it ----------------------------------
# The file manager gives the owner read/write
# access to every file on this node, which needs the agent to run as root.
# It is opt-in: the default answer is no, and so is any run without a terminal.
resolve_file_manager() {
    case "$(printf '%s' "${FILE_MANAGER}" | tr '[:upper:]' '[:lower:]')" in
        yes | y | true | 1) FILE_MANAGER=yes; return 0 ;;
        no | n | false | 0) FILE_MANAGER=no; return 0 ;;
        "") ;;
        *)
            echo "install-agent.sh: GOTHAM_AGENT_FILE_MANAGER must be yes or no (got '${FILE_MANAGER}')" >&2
            exit 2
            ;;
    esac
    # A re-run keeps the previous answer: the drop-in marks yes, an explicit
    # GOTHAM_AGENT_FILES=false in agent.env marks no.
    if [ -f "${FILE_MANAGER_DROPIN}" ]; then
        FILE_MANAGER=yes
        return 0
    fi
    if [ -f "${ENV_FILE}" ]; then
        case "$(sed -n 's/^[[:space:]]*GOTHAM_AGENT_FILES=//p' "${ENV_FILE}" | tail -n1 | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')" in
            false)
                FILE_MANAGER=no
                return 0
                ;;
            true)
                # Enabled by hand earlier: keep it as is (no prompt, and no
                # root drop-in the operator never asked for).
                FILE_MANAGER=keep
                return 0
                ;;
        esac
    fi
    FILE_MANAGER=no
    _fm_tty="${GOTHAM_AGENT_FILE_MANAGER_TTY:-/dev/tty}"
    # Only a character device qualifies, so the test seam can never make the
    # prompt truncate a regular file as root.
    if [ "${DRY_RUN}" -eq 1 ] || [ ! -c "${_fm_tty}" ] \
        || ! { ( : <"${_fm_tty}" ) 2>/dev/null && ( : >>"${_fm_tty}" ) 2>/dev/null; }; then
        return 0
    fi
    {
        echo ""
        echo "File manager"
        echo "  Lets the owner browse, read, edit, upload and"
        echo "  delete ANY file on this node. To do that the agent runs as root (instead of"
        echo "  the unprivileged gotham-agent user) without systemd sandboxing."
        printf '  Enable the file manager on this node? [y/N] '
    } >"${_fm_tty}"
    _fm_answer=""
    IFS= read -r _fm_answer <"${_fm_tty}" || _fm_answer=""
    case "$(printf '%s' "${_fm_answer}" | tr '[:upper:]' '[:lower:]')" in
        y | yes) FILE_MANAGER=yes ;;
    esac
}
resolve_file_manager
if [ "${FILE_MANAGER}" = "no" ]; then
    GOTHAM_AGENT_FILES=false
else
    GOTHAM_AGENT_FILES=true
fi
export GOTHAM_AGENT_FILES
log "file manager: ${FILE_MANAGER}"

# Validate every value that would land in agent.env before the first
# mutation (system user, binary, CA, Docker): a bad value fails here, never
# in a half-applied install.
agent_env_validate "${ENV_FILE}" "${AGENT_CA_PATH}" "${INSECURE}" || exit 1

# Docker Engine + the compose plugin: required at runtime, installed only in
# --full mode (a no-op when they already work; Ubuntu/Debian only).
if [ "${FULL}" -eq 1 ]; then
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] ensure Docker Engine and the compose plugin (--full)"
    else
        log "ensuring Docker Engine and the compose plugin (--full)"
        ensure_docker_full \
            || { echo "install-agent.sh: Docker setup failed (install Docker manually, then re-run without --full)" >&2; exit 1; }
    fi
fi

ARCH="$(detect_arch)"
REPO="${GOTHAM_REPO:-${DEFAULT_REPO}}"
DEFAULT_RELEASES_URL="https://github.com/${REPO}/releases"
if [ -n "${GOTHAM_BASE_URL:-}" ]; then
    RELEASE_BASE="${GOTHAM_BASE_URL%/}"
    [ -n "${GOTHAM_VERSION:-}" ] || { echo "GOTHAM_VERSION is required with GOTHAM_BASE_URL" >&2; exit 1; }
    VERSION="${GOTHAM_VERSION}"
else
    RELEASES_BASE="${GOTHAM_RELEASES_URL:-${DEFAULT_RELEASES_URL}}"
    RELEASES_BASE="${RELEASES_BASE%/}"
    if [ -n "${GOTHAM_VERSION:-}" ]; then
        VERSION="${GOTHAM_VERSION}"
    else
        # First redirect of /releases/latest; -L would land on a tagless CDN URL.
        redirect="$(curl -fsS -o /dev/null -w '%{redirect_url}' "${RELEASES_BASE}/latest" || true)"
        VERSION="${redirect%/}"
        VERSION="${VERSION##*/}"
        printf '%s' "${VERSION}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' \
            || { echo "could not resolve the latest release tag from ${RELEASES_BASE}/latest" >&2; exit 1; }
    fi
    RELEASE_BASE="${RELEASES_BASE}/download/${VERSION}"
fi
# Private scratch dir (0700): the pinned key and the verified binary live
# here and are removed on exit.
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gotham-agent-install.XXXXXX")"
PUBKEY_FILE="${WORK_DIR}/release.pub"
TMP_BINARY="${WORK_DIR}/${BINARY_NAME}"
# Clean up the private scratch dir on normal exit, and abort on a signal (a
# cleanup-only INT/TERM trap would let the install carry on).
trap 'rm -rf "${WORK_DIR}"' EXIT
trap 'exit 1' INT TERM

log "installing ${BINARY_NAME} ${VERSION} for linux/${ARCH}"
log "downloading ${RELEASE_BASE}/gotham-agent-linux-${ARCH}"

require_cmd curl "apt-get install -y curl"
require_cmd openssl "apt-get install -y openssl"
require_cmd base64 "coreutils"
# sudo/visudo are only needed when the sudoers drop-in is installed; --dry-run
# skips that step, so do not require them there.
if [ "${DRY_RUN}" -eq 0 ]; then
    require_cmd sudo "apt-get install -y sudo"
    require_cmd visudo "apt-get install -y sudo"
fi
# The pinned release public key is the anchor; there is no runtime override.
materialize_public_key "${GOTHAM_RELEASE_PUBLIC_KEY_B64}" "${PUBKEY_FILE}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] verify_release ${RELEASE_BASE} ${VERSION} ${ARCH} gotham-agent <pubkey> ${TMP_BINARY}"
else
    verify_release "${RELEASE_BASE}" "${VERSION}" "${ARCH}" "gotham-agent" "${PUBKEY_FILE}" "${TMP_BINARY}"
fi

log "creating system user ${SERVICE_USER}"
if [ "${DRY_RUN}" -eq 1 ] || ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
    run useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi
# The unit joins the docker group (SupplementaryGroups=docker), but Docker is
# installed only with --full or later from the App Store, so a bare node has no
# docker group and systemd fails the unit at start (status 216/GROUP) — the
# agent would then never register, which the "install Docker from the App Store
# after the agent is registered" guidance depends on. Create the system group
# when missing (so both the checked-in unit and the fallback unit start), then
# add the service user; a later Docker install reuses the same group.
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] getent group docker >/dev/null 2>&1 || groupadd --system docker"
else
    getent group docker >/dev/null 2>&1 || groupadd --system docker
fi
run usermod -aG docker "${SERVICE_USER}"

# The binary MUST live in the service StateDirectory and be owned by the
# service user: the updater hardlinks it to <binary>.old, and
# fs.protected_hardlinks=1 (the default) makes os.Link of a root-owned file fail
# with EPERM.
log "installing binary to ${INSTALL_PATH}"
run mkdir -p "${STATE_DIR}/bin"
run chmod 0755 "${STATE_DIR}" "${STATE_DIR}/bin"
run chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}" "${STATE_DIR}/bin"
if [ "${DRY_RUN}" -eq 1 ]; then
    run install -m 0755 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${TMP_BINARY}" "${INSTALL_PATH}"
else
    install -m 0755 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${TMP_BINARY}" "${INSTALL_PATH}"
    rm -f "${TMP_BINARY}"
fi

log "creating ${ENV_DIR}"
run mkdir -p "${ENV_DIR}"
run chmod 0755 "${ENV_DIR}"

if [ -n "${CA_SRC}" ]; then
    log "installing the control-plane CA certificate to ${AGENT_CA_PATH}"
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] install -m 0644 ${CA_SRC} ${AGENT_CA_PATH}"
    else
        install -m 0644 -o root -g root "${CA_SRC}" "${AGENT_CA_PATH}"
    fi
fi

log "writing ${ENV_FILE}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${ENV_FILE} from GOTHAM_AGENT_* environment (preserving prior values)"
else
    # Reinstall: keys this invocation does not set keep the value a previous
    # install wrote, so the CP address and node id are not lost.
    agent_env_write "${ENV_FILE}" "${AGENT_CA_PATH}" "${INSECURE}"
    chown root:"${SERVICE_USER}" "${ENV_FILE}"
fi

# Self-update chain: the shared wrapper installed under the agent name, its
# root-owned configuration, the root-owned status directory and the sudoers
# rule. Mirror of the control-plane install in deploy/install.sh.
log "installing the update wrapper ${WRAPPER_PATH}"
run mkdir -p /usr/libexec/gotham
run chmod 0755 /usr/libexec/gotham
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] install ${SCRIPT_DIR}/gotham-update.sh ${WRAPPER_PATH}"
else
    install -m 0755 -o root -g root "${SCRIPT_DIR}/gotham-update.sh" "${WRAPPER_PATH}"
fi

log "installing ${WRAPPER_CONF}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] install ${SCRIPT_DIR}/gotham-agent-updater.conf ${WRAPPER_CONF}"
else
    install -m 0644 -o root -g root "${SCRIPT_DIR}/gotham-agent-updater.conf" "${WRAPPER_CONF}"
fi

log "creating the root-owned status directory ${STATUS_DIR}"
run mkdir -p "${STATUS_DIR}"
run chmod 0755 "${STATUS_DIR}"

log "installing the sudoers rule"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] sh ${SCRIPT_DIR}/install-agent-sudoers.sh ${SERVICE_USER}"
else
    # Invoke via sh so a checkout that lost the exec bit still installs.
    sh "${SCRIPT_DIR}/install-agent-sudoers.sh" "${SERVICE_USER}"
fi

# Host-tool helper: the fixed root-owned installer the agent reaches through
# sudoers for InstallTool/UninstallTool. Without it the tool manager reports a
# refusal instead of running an unbacked sudo.
log "installing the host-tool helper ${TOOLCTL_PATH}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] install -m 0755 ${SCRIPT_DIR}/gotham-toolctl.sh ${TOOLCTL_PATH}"
    echo "[dry-run] sh ${SCRIPT_DIR}/install-toolctl-sudoers.sh ${SERVICE_USER}"
else
    install -m 0755 -o root -g root "${SCRIPT_DIR}/gotham-toolctl.sh" "${TOOLCTL_PATH}"
    # Invoke via sh so a checkout that lost the exec bit still installs.
    sh "${SCRIPT_DIR}/install-toolctl-sudoers.sh" "${SERVICE_USER}"
fi

log "installing systemd unit ${SERVICE_FILE}"
run mkdir -p "${STATE_DIR}"
run chmod 0755 "${STATE_DIR}"
run chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"

# The unit points DOCKER_CONFIG here because ProtectHome hides ~/.docker. The
# docker CLI skips its compose plugin when it cannot read its config directory,
# so the directory must exist and belong to the service user on every run.
log "preparing the docker CLI config directory ${DOCKER_CONFIG_DIR}"
run mkdir -p "${DOCKER_CONFIG_DIR}"
run chmod 0700 "${DOCKER_CONFIG_DIR}"
run chown -R "${SERVICE_USER}:${SERVICE_USER}" "${DOCKER_CONFIG_DIR}"

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${SERVICE_FILE}"
else
    if [ -f "${SCRIPT_DIR}/gotham-agent.service" ]; then
        install -m 0644 "${SCRIPT_DIR}/gotham-agent.service" "${SERVICE_FILE}"
    else
        # Fall back to a minimal unit when run without the repository checkout.
        cat >"${SERVICE_FILE}" <<'UNIT'
[Unit]
Description=Gotham node agent
After=network-online.target docker.service
Wants=network-online.target docker.service

[Service]
Type=simple
User=gotham-agent
Group=gotham-agent
WorkingDirectory=/var/lib/gotham-agent
Environment=GOTHAM_AGENT_CERT_DIR=/var/lib/gotham-agent
Environment=GOTHAM_AGENT_BINARY=/var/lib/gotham-agent/bin/gotham-agent
Environment=DOCKER_CONFIG=/var/lib/gotham-agent/.docker
EnvironmentFile=-/etc/gotham/agent.env
ExecStart=/var/lib/gotham-agent/bin/gotham-agent serve
Restart=always
RestartSec=5
KillMode=process
LimitNOFILE=65536
SupplementaryGroups=docker

[Install]
WantedBy=multi-user.target
UNIT
        chmod 0644 "${SERVICE_FILE}"
    fi
fi

# File manager: the drop-in runs the agent as root without sandboxing so it can
# reach every file; answering no removes it again (back to the unprivileged unit).
if [ "${FILE_MANAGER}" = "yes" ]; then
    log "installing ${FILE_MANAGER_DROPIN} (agent runs as root)"
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] write ${FILE_MANAGER_DROPIN}"
    else
        mkdir -p "${FILE_MANAGER_DROPIN_DIR}"
        cat >"${FILE_MANAGER_DROPIN}" <<'DROPIN'
# Written by install-agent.sh after you enabled the file manager. Remove this
# file (and set GOTHAM_AGENT_FILES=false in /etc/gotham/agent.env) to go back to
# the unprivileged, sandboxed agent.
[Service]
User=root
Group=root
ProtectSystem=no
ProtectHome=no
PrivateTmp=no
DROPIN
        chmod 0644 "${FILE_MANAGER_DROPIN}"
    fi
elif [ "${FILE_MANAGER}" = "no" ]; then
    # Back to the unprivileged agent: files the root agent created in its state
    # directory (certificate, key) must belong to the service user again.
    if [ -f "${FILE_MANAGER_DROPIN}" ]; then
        run chown -R "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"
    fi
    run rm -f "${FILE_MANAGER_DROPIN}"
fi

log "enabling ${BINARY_NAME}"
run systemctl daemon-reload
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] systemctl enable gotham-agent.service"
    echo "[dry-run] systemctl restart gotham-agent.service (or start when inactive)"
else
    # A reinstall must not leave the previous process running: restart the unit
    # when it is already active (start it otherwise) and confirm it will exec the
    # binary just installed.
    systemctl enable gotham-agent.service
    agent_service_restart gotham-agent.service "${INSTALL_PATH}" \
        || { echo "install-agent.sh: could not bring gotham-agent onto the new binary" >&2; exit 1; }
fi

log "done. Check status with: systemctl status gotham-agent"
