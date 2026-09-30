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
# The release binary is downloaded with the same signed-manifest verification as
# the control-plane installer (deploy/release-verify.sh): the Ed25519 signature
# of gotham-agent-manifest-<arch>.txt is checked against the embedded release
# public key, then the artifact digest against the signed sha256. A failed
# verification aborts; nothing is trusted on first use.
#
# Override the release for testing with GOTHAM_VERSION and GOTHAM_BASE_URL.
#
# Usage:
#   sudo ./install-agent.sh [--dry-run]
#
# Environment variables written to /etc/gotham/agent.env (unset values are
# omitted so the agent keeps its built-in default):
#   GOTHAM_AGENT_CP_ADDR
#   GOTHAM_AGENT_NODE_ID
#   GOTHAM_AGENT_LISTEN_ADDR
#   GOTHAM_AGENT_CA
#   GOTHAM_AGENT_CERT_DIR
#   GOTHAM_AGENT_KEY
#   GOTHAM_AGENT_DOCKER_SOCK
#   GOTHAM_AGENT_LOG_LEVEL
#   GOTHAM_AGENT_AUTO_UPDATE
#   GOTHAM_AGENT_UPDATE_INTERVAL

set -eu

BINARY_NAME="gotham-agent"
INSTALL_PATH="/var/lib/gotham-agent/bin/gotham-agent"
ENV_DIR="/etc/gotham"
ENV_FILE="${ENV_DIR}/agent.env"
SERVICE_FILE="/etc/systemd/system/gotham-agent.service"
STATE_DIR="/var/lib/gotham-agent"
STATUS_DIR="/var/lib/gotham-agent-updater"
WRAPPER_PATH="/usr/libexec/gotham/gotham-agent-update"
WRAPPER_CONF="/etc/gotham/agent-updater.conf"
SERVICE_USER="gotham-agent"
DEFAULT_REPO="justindeelux/gotham"
# Release trust anchor: the base64 raw Ed25519 public key (same as install.sh
# and deploy/gotham-signing-key.pub). Never fetched from the download channel.
GOTHAM_RELEASE_PUBLIC_KEY_B64="y21W8J0G82S/N+ws/2KwoO7RTDuOvvbKzLQOKvSe97E="
DRY_RUN=0

for argument in "$@"; do
    case "${argument}" in
        --dry-run)
            DRY_RUN=1
            ;;
        -h | --help)
            sed -n '2,30p' "$0"
            exit 0
            ;;
        *)
            echo "unknown argument: ${argument}" >&2
            exit 2
            ;;
    esac
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
# shellcheck source=deploy/release-verify.sh
. "${SCRIPT_DIR}/release-verify.sh"
for sibling in release-verify.sh gotham-update.sh gotham-agent-updater.conf install-agent-sudoers.sh; do
    if [ ! -f "${SCRIPT_DIR}/${sibling}" ]; then
        echo "install-agent.sh: ${sibling} must be next to this script (run it from the repository checkout)" >&2
        exit 2
    fi
done

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

ARCH="$(detect_arch)"
REPO="${GOTHAM_REPO:-${DEFAULT_REPO}}"
if [ -n "${GOTHAM_BASE_URL:-}" ]; then
    RELEASE_BASE="${GOTHAM_BASE_URL%/}"
    [ -n "${GOTHAM_VERSION:-}" ] || { echo "GOTHAM_VERSION is required with GOTHAM_BASE_URL" >&2; exit 1; }
    VERSION="${GOTHAM_VERSION}"
else
    RELEASES_BASE="https://github.com/${REPO}/releases"
    if [ -n "${GOTHAM_VERSION:-}" ]; then
        RELEASE_BASE="${RELEASES_BASE}/download/${GOTHAM_VERSION}"
        VERSION="${GOTHAM_VERSION}"
    else
        RELEASE_BASE="${RELEASES_BASE}/latest/download"
        VERSION="$(
            curl -fsSL -o /dev/null -w '%{url_effective}' \
                "${RELEASE_BASE}/gotham-agent-manifest-${ARCH}.txt" \
                | sed -n 's#.*/download/\([^/]*\)/.*#\1#p'
        )"
        [ -n "${VERSION}" ] || { echo "could not resolve the latest release tag" >&2; exit 1; }
    fi
fi
TMP_BINARY="${TMPDIR:-/tmp}/${BINARY_NAME}.$$"
PUBKEY_FILE="$(mktemp "${TMPDIR:-/tmp}/gotham-pubkey.XXXXXX")"
trap 'rm -f "${PUBKEY_FILE}" "${TMP_BINARY}"' EXIT INT TERM

log "installing ${BINARY_NAME} ${VERSION} for linux/${ARCH}"
log "downloading ${RELEASE_BASE}/gotham-agent-linux-${ARCH}"

require_cmd curl "apt-get install -y curl"
require_cmd openssl "apt-get install -y openssl"
require_cmd base64 "coreutils"
materialize_public_key "${GOTHAM_UPDATE_PUBLIC_KEY:-${GOTHAM_RELEASE_PUBLIC_KEY_B64}}" "${PUBKEY_FILE}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] verify_release ${RELEASE_BASE} ${VERSION} ${ARCH} gotham-agent <pubkey> ${TMP_BINARY}"
else
    verify_release "${RELEASE_BASE}" "${VERSION}" "${ARCH}" "gotham-agent" "${PUBKEY_FILE}" "${TMP_BINARY}"
fi

log "creating system user ${SERVICE_USER}"
if [ "${DRY_RUN}" -eq 1 ] || ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
    run useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi
if command -v getent >/dev/null 2>&1 && getent group docker >/dev/null 2>&1; then
    run usermod -aG docker "${SERVICE_USER}"
fi

# The binary MUST live in the service StateDirectory and be owned by the
# service user: the updater hardlinks it to <binary>.old, and
# fs.protected_hardlinks=1 (the default) makes os.Link of a root-owned file fail
# with EPERM.
log "installing binary to ${INSTALL_PATH}"
run mkdir -p "${STATE_DIR}/bin"
run chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}" "${STATE_DIR}/bin"
if [ "${DRY_RUN}" -eq 1 ]; then
    run install -m 0755 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${TMP_BINARY}" "${INSTALL_PATH}"
else
    install -m 0755 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${TMP_BINARY}" "${INSTALL_PATH}"
    rm -f "${TMP_BINARY}"
fi

log "creating ${ENV_DIR}"
run mkdir -p "${ENV_DIR}"

log "writing ${ENV_FILE}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${ENV_FILE} from GOTHAM_AGENT_* environment"
else
    : >"${ENV_FILE}"
    chmod 0640 "${ENV_FILE}"
    for key in \
        GOTHAM_AGENT_CP_ADDR \
        GOTHAM_AGENT_NODE_ID \
        GOTHAM_AGENT_LISTEN_ADDR \
        GOTHAM_AGENT_CA \
        GOTHAM_AGENT_CERT_DIR \
        GOTHAM_AGENT_KEY \
        GOTHAM_AGENT_DOCKER_SOCK \
        GOTHAM_AGENT_LOG_LEVEL \
        GOTHAM_AGENT_AUTO_UPDATE \
        GOTHAM_AGENT_UPDATE_INTERVAL; do
        eval "value=\${${key}:-}"
        if [ -n "${value}" ]; then
            printf '%s=%s\n' "${key}" "${value}" >>"${ENV_FILE}"
        fi
    done
fi

# Self-update chain: the shared wrapper installed under the agent name, its
# root-owned configuration, the root-owned status directory and the sudoers
# rule. Mirror of the control-plane install in deploy/README.md.
log "installing the update wrapper ${WRAPPER_PATH}"
run mkdir -p /usr/libexec/gotham
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
    echo "[dry-run] ${SCRIPT_DIR}/install-agent-sudoers.sh ${SERVICE_USER}"
else
    "${SCRIPT_DIR}/install-agent-sudoers.sh" "${SERVICE_USER}"
fi

log "installing systemd unit ${SERVICE_FILE}"
run mkdir -p "${STATE_DIR}"
run chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"

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

log "enabling ${BINARY_NAME}"
run systemctl daemon-reload
run systemctl enable --now gotham-agent.service

log "done. Check status with: systemctl status gotham-agent"
