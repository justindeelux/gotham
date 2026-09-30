#!/bin/sh
#
# install.sh installs the Gotham control plane on a systemd Linux host.
#
# It is the signed one-line installer for a clean Ubuntu 22.04 VPS: it detects
# the architecture, downloads the release binary, verifies the Ed25519-signed
# manifest and the artifact digest (see deploy/release-verify.sh), installs the
# binary into the BE-9.1 layout, installs the root-owned update wrapper, its
# config, the sudoers rule and the systemd unit, and prints the web UI URL and
# the first-login steps.
#
# Run it from a repository checkout (it uses its sibling files: the wrapper,
# the updater config, the sudoers installer and the unit). Example:
#
#   git clone https://github.com/justindeelux/gotham.git
#   sudo gotham/deploy/install.sh
#
# Verification environment overrides (testing only; see
# deploy/test-release-install.sh):
#   GOTHAM_VERSION            release tag to install (default: latest)
#   GOTHAM_BASE_URL           full asset base URL (mirror); requires GOTHAM_VERSION
#   GOTHAM_REPO               owner/name (default justindeelux/gotham)
#   GOTHAM_UPDATE_PUBLIC_KEY  override the embedded public key (PEM or base64)
#   GOTHAM_INSTALL_ROOT       install everything under this prefix (non-root)
#
# Runtime configuration overrides:
#   GOTHAM_DATABASE_DSN       managed PostgreSQL DSN; skips local provisioning
#   GOTHAM_REDIS_ADDR         Redis host:port (default localhost:6379)
#   GOTHAM_SKIP_DEPS=1        do not install/configure PostgreSQL + Redis
#
# Usage:
#   sudo ./install.sh [--dry-run]

set -eu

BINARY_NAME="gotham"
FAMILY=""
SERVICE_USER="gotham"
MANIFEST_PREFIX="gotham-manifest-"
DEFAULT_REPO="justindeelux/gotham"

# The release trust anchor. This is the base64 raw Ed25519 public key printed by
# `cmd/signer keygen`; the matching PEM is committed at
# deploy/gotham-signing-key.pub and published with every release. A release
# embeds the same key in the binaries. Never fetch this from the download
# channel.
GOTHAM_RELEASE_PUBLIC_KEY_B64="Yt6nz1gGQWF7Bfc9MCt/gQXbPMzhN9OygrUkOEFYdwQ="

DRY_RUN=0
for argument in "$@"; do
    case "${argument}" in
        --dry-run) DRY_RUN=1 ;;
        -h | --help)
            sed -n '2,34p' "$0"
            exit 0
            ;;
        *)
            echo "unknown argument: ${argument}" >&2
            exit 2
            ;;
    esac
done

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# shellcheck source=deploy/release-verify.sh
. "${SCRIPT_DIR}/release-verify.sh"

log() { echo "==> $*"; }

# run executes a command, or prints it in dry-run mode.
run() {
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] $*"
        return 0
    fi
    "$@"
}

# Install prefix. Empty = the production layout from deploy/README.md. A
# non-empty prefix is the test-only redirect used by the dry run so no host
# path is touched.
PREFIX="${GOTHAM_INSTALL_ROOT:-}"
if [ -n "${PREFIX}" ]; then
    TEST_MODE=1
    case "${PREFIX}" in
        /*) ;;
        *) die "GOTHAM_INSTALL_ROOT must be an absolute path" ;;
    esac
else
    TEST_MODE=0
fi

ETC_DIR="${PREFIX}/etc/gotham"
STATE_DIR="${PREFIX}/var/lib/gotham"
BIN_DIR="${STATE_DIR}/bin"
INSTALL_PATH="${BIN_DIR}/${BINARY_NAME}"
STATUS_DIR="${PREFIX}/var/lib/gotham-updater"
WRAPPER_PATH="${PREFIX}/usr/libexec/gotham/gotham-update"
WRAPPER_CONF="${ETC_DIR}/updater.conf"
ENV_FILE="${ETC_DIR}/gotham.env"
SERVICE_FILE="${PREFIX}/etc/systemd/system/gotham.service"

# render_file installs a root-owned config/unit file, rewriting the production
# absolute roots onto GOTHAM_INSTALL_ROOT when one is set (test mode only).
render_file() {
    src=$1
    dst=$2
    mode=$3
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] install -m ${mode} ${src} ${dst}"
        return 0
    fi
    mkdir -p "$(dirname "${dst}")"
    if [ -n "${PREFIX}" ]; then
        sed \
            -e "s#/usr/libexec/gotham#${PREFIX}/usr/libexec/gotham#g" \
            -e "s#/var/lib/gotham#${PREFIX}/var/lib/gotham#g" \
            -e "s#/etc/gotham#${PREFIX}/etc/gotham#g" \
            "${src}" >"${dst}"
        chmod "${mode}" "${dst}"
    else
        install -m "${mode}" -o root -g root "${src}" "${dst}"
    fi
}

for sibling in release-verify.sh gotham-update.sh gotham-updater.conf install-sudoers.sh gotham.service; do
    [ -f "${SCRIPT_DIR}/${sibling}" ] \
        || die "${sibling} must be next to this script (run it from the repository checkout)"
done

if [ "$(id -u)" -ne 0 ] && [ "${DRY_RUN}" -eq 0 ] && [ "${TEST_MODE}" -eq 0 ]; then
    die "install.sh must run as root (try sudo)"
fi
if [ "${TEST_MODE}" -eq 0 ] && ! command -v systemctl >/dev/null 2>&1 && [ "${DRY_RUN}" -eq 0 ]; then
    die "systemctl not found; this installer targets systemd hosts"
fi

require_cmd curl "apt-get install -y curl"
require_cmd openssl "apt-get install -y openssl"
require_cmd base64 "coreutils"
require_cmd mktemp "coreutils"
require_cmd sed "sed"
require_cmd awk "mawk/gawk"
require_cmd install "coreutils"
if [ "${DRY_RUN}" -eq 0 ]; then
    openssl pkeyutl -help 2>&1 | grep -q rawin \
        || die "openssl 3+ is required (Ed25519 -rawin support)"
fi

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo "amd64" ;;
        aarch64 | arm64) echo "arm64" ;;
        *) die "unsupported architecture: $(uname -m)" ;;
    esac
}

ARCH="$(detect_arch)"
REPO="${GOTHAM_REPO:-${DEFAULT_REPO}}"
if [ -n "${GOTHAM_BASE_URL:-}" ]; then
    # Full asset base URL (e.g. a local mirror or .../releases/download/<tag>).
    RELEASE_BASE="${GOTHAM_BASE_URL%/}"
    [ -n "${GOTHAM_VERSION:-}" ] || die "GOTHAM_VERSION is required with GOTHAM_BASE_URL"
    VERSION="${GOTHAM_VERSION}"
else
    RELEASES_BASE="https://github.com/${REPO}/releases"
    if [ -n "${GOTHAM_VERSION:-}" ]; then
        RELEASE_BASE="${RELEASES_BASE}/download/${GOTHAM_VERSION}"
        VERSION="${GOTHAM_VERSION}"
    else
        RELEASE_BASE="${RELEASES_BASE}/latest/download"
        # The manifest binds the version; discover it from the release redirect.
        VERSION="$(
            curl -fsSL -o /dev/null -w '%{url_effective}' \
                "${RELEASE_BASE}/${MANIFEST_PREFIX}${ARCH}.txt" \
                | sed -n 's#.*/download/\([^/]*\)/.*#\1#p'
        )"
        [ -n "${VERSION}" ] || die "could not resolve the latest release tag"
    fi
fi

log "installing ${BINARY_NAME} ${VERSION} for linux/${ARCH}"

# Materialize the pinned public key.
PUBKEY_FILE="$(mktemp "${TMPDIR:-/tmp}/gotham-pubkey.XXXXXX")"
# shellcheck disable=SC2064
trap "rm -f '${PUBKEY_FILE}'" EXIT INT TERM
materialize_public_key "${GOTHAM_UPDATE_PUBLIC_KEY:-${GOTHAM_RELEASE_PUBLIC_KEY_B64}}" "${PUBKEY_FILE}"

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${PUBKEY_FILE}"
    echo "[dry-run] verify_release ${RELEASE_BASE} ${VERSION} ${ARCH} ${FAMILY} <pubkey> ${INSTALL_PATH}"
else
    TMP_BINARY="${TMPDIR:-/tmp}/${BINARY_NAME}.$$"
    verify_release "${RELEASE_BASE}" "${VERSION}" "${ARCH}" "${FAMILY}" "${PUBKEY_FILE}" "${TMP_BINARY}"
fi

# ---- Service user and directories -------------------------------------------
if [ "${TEST_MODE}" -eq 0 ] && [ "${DRY_RUN}" -eq 0 ]; then
    if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
        log "creating system user ${SERVICE_USER}"
        useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
    fi
fi
log "creating ${BIN_DIR}"
run mkdir -p "${BIN_DIR}"
if [ "${TEST_MODE}" -eq 0 ]; then
    run chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}" "${BIN_DIR}"
fi

log "installing binary to ${INSTALL_PATH}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] install -m 0755 ${TMP_BINARY:-<verified>} ${INSTALL_PATH}"
elif [ "${TEST_MODE}" -eq 1 ]; then
    install -m 0755 "${TMP_BINARY}" "${INSTALL_PATH}"
    rm -f "${TMP_BINARY}"
else
    install -m 0755 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${TMP_BINARY}" "${INSTALL_PATH}"
    rm -f "${TMP_BINARY}"
fi

# ---- Control-plane configuration --------------------------------------------
log "writing ${ENV_FILE}"
run mkdir -p "${ETC_DIR}"
JWT_KEY="${ETC_DIR}/jwt_ed25519.key"
JWT_PUB="${ETC_DIR}/jwt_ed25519.pub"
DSN="${GOTHAM_DATABASE_DSN:-postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable}"
REDIS_ADDR="${GOTHAM_REDIS_ADDR:-localhost:6379}"
if [ "${DRY_RUN}" -eq 0 ]; then
    # Persist the secret and JWT keys across reinstalls; only generate missing.
    SECRET_KEY=""
    if [ -f "${ENV_FILE}" ]; then
        SECRET_KEY="$(sed -n 's/^GOTHAM_SECRET_KEY=//p' "${ENV_FILE}" | head -n1)"
    fi
    [ -n "${SECRET_KEY}" ] || SECRET_KEY="$(openssl rand -base64 32)"
    if [ ! -f "${JWT_KEY}" ]; then
        openssl genpkey -algorithm ed25519 -out "${JWT_KEY}"
    fi
    if [ ! -f "${JWT_PUB}" ]; then
        openssl pkey -in "${JWT_KEY}" -pubout -out "${JWT_PUB}"
    fi
    cat >"${ENV_FILE}" <<EOF
# Gotham control-plane environment. Read by gotham.service (EnvironmentFile).
GOTHAM_DATABASE_DSN=${DSN}
GOTHAM_REDIS_ADDR=${REDIS_ADDR}
GOTHAM_CA_DIR=${STATE_DIR}/ca
GOTHAM_SECRET_KEY=${SECRET_KEY}
GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH=${JWT_KEY}
GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH=${JWT_PUB}
EOF
    chmod 0640 "${ENV_FILE}" "${JWT_KEY}"
    chmod 0644 "${JWT_PUB}"
    if [ "${TEST_MODE}" -eq 0 ]; then
        chown root:"${SERVICE_USER}" "${ENV_FILE}" "${JWT_KEY}"
        chown root:root "${JWT_PUB}"
    fi
else
    echo "[dry-run] write ${ENV_FILE}, ${JWT_KEY}, ${JWT_PUB}"
fi

# ---- Self-update chain (mirror of deploy/README.md) -------------------------
log "installing the update wrapper ${WRAPPER_PATH}"
run mkdir -p "$(dirname "${WRAPPER_PATH}")"
render_file "${SCRIPT_DIR}/gotham-update.sh" "${WRAPPER_PATH}" 0755

log "installing ${WRAPPER_CONF}"
render_file "${SCRIPT_DIR}/gotham-updater.conf" "${WRAPPER_CONF}" 0644

log "creating the root-owned status directory ${STATUS_DIR}"
run mkdir -p "${STATUS_DIR}"
run chmod 0755 "${STATUS_DIR}"

if [ "${TEST_MODE}" -eq 0 ] && [ "${DRY_RUN}" -eq 0 ]; then
    log "installing the sudoers rule"
    "${SCRIPT_DIR}/install-sudoers.sh" "${SERVICE_USER}"
fi

log "installing systemd unit ${SERVICE_FILE}"
render_file "${SCRIPT_DIR}/gotham.service" "${SERVICE_FILE}" 0644

if [ "${TEST_MODE}" -eq 1 ]; then
    log "test mode: skipping service activation"
    log "done (test install under ${PREFIX})"
    exit 0
fi

# ---- Dependencies (PostgreSQL + Redis) --------------------------------------
if [ "${DRY_RUN}" -eq 0 ] && [ "${GOTHAM_SKIP_DEPS:-0}" != "1" ] && [ -z "${GOTHAM_DATABASE_DSN:-}" ]; then
    if ! command -v psql >/dev/null 2>&1 || ! command -v redis-server >/dev/null 2>&1; then
        if command -v apt-get >/dev/null 2>&1; then
            log "installing PostgreSQL and Redis"
            DEBIAN_FRONTEND=noninteractive apt-get update
            DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql redis-server
        else
            die "PostgreSQL and Redis are required; install them or set GOTHAM_DATABASE_DSN and GOTHAM_SKIP_DEPS=1"
        fi
    fi
    log "enabling PostgreSQL and Redis"
    systemctl enable --now postgresql redis-server || die "could not start PostgreSQL/Redis"
    # Idempotent role + database matching the default DSN.
    if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='gotham'" | grep -q 1; then
        runuser -u postgres -- psql -c "CREATE ROLE gotham LOGIN PASSWORD 'gotham'"
    fi
    if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='gotham'" | grep -q 1; then
        runuser -u postgres -- createdb -O gotham gotham
    fi
fi

# ---- Migrate + start --------------------------------------------------------
if [ "${DRY_RUN}" -eq 0 ]; then
    log "applying database migrations"
    runuser -u "${SERVICE_USER}" -- sh -c \
        "cd / && GOTHAM_DATABASE_DSN='${DSN}' '${INSTALL_PATH}' migrate up" \
        || die "database migrations failed (check PostgreSQL and GOTHAM_DATABASE_DSN)"
fi

log "starting ${BINARY_NAME}"
run systemctl daemon-reload
run systemctl enable --now "${BINARY_NAME}.service"

HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "${HOST_IP}" ] || HOST_IP="127.0.0.1"

cat <<EOF

Gotham ${VERSION} is installed.

  Web UI:   http://${HOST_IP}:8000
  Status:   systemctl status gotham
  Logs:     journalctl -u gotham -f

First login:
  1. Open the Web UI and choose "Create account" (the first account is the
     platform owner).
  2. Sign in, then add a node agent with deploy/install-agent.sh.

Self-update is on by default; set AUTO_UPDATE=true in
/etc/gotham/gotham.env to apply new releases unattended.
EOF