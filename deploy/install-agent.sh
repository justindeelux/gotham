#!/bin/sh
#
# install-agent.sh installs the Gotham node agent on a Linux host.
#
# It downloads the latest release binary for the host architecture, installs it
# to /usr/local/bin, writes /etc/gotham/agent.env from the current environment,
# creates the gotham-agent system user and enables the systemd unit.
#
# This script is wired for the Phase 9 release pipeline: the download URL and
# binary names must match the GoReleaser configuration produced in that phase.
# Override the release host with GOTHAM_RELEASE_URL when testing.
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

set -eu

BINARY_NAME="gotham-agent"
INSTALL_PATH="/usr/local/bin/gotham-agent"
ENV_DIR="/etc/gotham"
ENV_FILE="${ENV_DIR}/agent.env"
SERVICE_FILE="/etc/systemd/system/gotham-agent.service"
STATE_DIR="/var/lib/gotham-agent"
SERVICE_USER="gotham-agent"
RELEASE_URL="${GOTHAM_RELEASE_URL:-https://github.com/justindeelux/gotham/releases/latest/download}"
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
DOWNLOAD_URL="${RELEASE_URL}/${BINARY_NAME}-linux-${ARCH}"
TMP_BINARY="${TMPDIR:-/tmp}/${BINARY_NAME}.$$"

log "installing ${BINARY_NAME} for linux/${ARCH}"
log "downloading ${DOWNLOAD_URL}"

if [ "${DRY_RUN}" -eq 1 ]; then
    run curl -fsSL -o "${TMP_BINARY}" "${DOWNLOAD_URL}"
    run install -m 0755 "${TMP_BINARY}" "${INSTALL_PATH}"
else
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "${TMP_BINARY}" "${DOWNLOAD_URL}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${TMP_BINARY}" "${DOWNLOAD_URL}"
    else
        echo "curl or wget is required to download the binary" >&2
        exit 1
    fi
    install -m 0755 "${TMP_BINARY}" "${INSTALL_PATH}"
    rm -f "${TMP_BINARY}"
fi

log "creating system user ${SERVICE_USER}"
if [ "${DRY_RUN}" -eq 1 ] || ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
    run useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi
if command -v getent >/dev/null 2>&1 && getent group docker >/dev/null 2>&1; then
    run usermod -aG docker "${SERVICE_USER}"
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
        GOTHAM_AGENT_LOG_LEVEL; do
        eval "value=\${${key}:-}"
        if [ -n "${value}" ]; then
            printf '%s=%s\n' "${key}" "${value}" >>"${ENV_FILE}"
        fi
    done
fi

log "installing systemd unit ${SERVICE_FILE}"
run mkdir -p "${STATE_DIR}"
run chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${SERVICE_FILE}"
else
    SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
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
EnvironmentFile=-/etc/gotham/agent.env
ExecStart=/usr/local/bin/gotham-agent serve
Restart=always
RestartSec=5
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
