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
#   GOTHAM_VERSION              release tag to install (default: resolve latest)
#   GOTHAM_RELEASES_URL         GitHub-style releases root (mirror); default
#                               https://github.com/<repo>/releases
#   GOTHAM_BASE_URL             full asset base URL (static mirror); requires
#                               GOTHAM_VERSION
#   GOTHAM_REPO                 owner/name (default justindeelux/gotham)
#   GOTHAM_INSTALL_ROOT         install everything under this prefix (non-/);
#                               enables test mode. "/" is refused: it is the
#                               real root, not a sandbox.
#   GOTHAM_INSTALL_TEST_PUBLIC_KEY  test-only: replace the pinned trust anchor
#                               (PEM or base64); honoured ONLY with
#                               GOTHAM_INSTALL_ROOT, otherwise warned and ignored
#   GOTHAM_INSTALL_TEST=1       test-only: honour the GOTHAM_OS_RELEASE_FILE /
#                               GOTHAM_TEST_UNAME_M / GOTHAM_AGENT_ENV_FILE
#                               seams in a run without GOTHAM_INSTALL_ROOT, but
#                               ONLY together with --dry-run (a dry run changes
#                               nothing, so the flag alone is safe there). With
#                               a real run it changes nothing, so a stray
#                               export cannot redirect an install. Never set it
#                               in production.
#   GOTHAM_INSTALL_TEST_RUN_AGENT=1 + GOTHAM_INSTALL_TEST_AGENT_SCRIPT=<fake>
#                               test-only: with GOTHAM_INSTALL_ROOT (a non-/
#                               sandbox), run past service activation
#                               (systemctl must be a logging shim on PATH) and
#                               execute <fake> instead of install-agent.sh, so
#                               the agent-failure path runs. RUN_AGENT=1
#                               without the script seam is refused outright (it
#                               would run the real install-agent.sh).
#
# Runtime configuration overrides:
#   GOTHAM_DATABASE_DSN       managed PostgreSQL DSN; skips local provisioning
#   GOTHAM_REDIS_ADDR         Redis host:port (default localhost:6379)
#   GOTHAM_SKIP_DEPS=1        do not install/configure PostgreSQL + Redis
#
# Re-running the installer preserves /etc/gotham/gotham.env: managed keys are
# refreshed (a DSN given on the command line wins; otherwise the existing value
# is kept) and any operator-added keys (AUTO_UPDATE, PLATFORM_ADMINS, ...) are
# left intact.
#
# The installer provisions the mTLS certificate authority (`gotham ca init`) at
# GOTHAM_CA_DIR and the gRPC gateway then runs TLS. Copy ca.crt from there to
# each node and pass it to install-agent.sh --ca.
#
# Remote agents must dial a name/IP present in the gRPC listener certificate
# SANs. Pass --cp-host <name-or-ip> (repeatable) or GOTHAM_GRPC_HOSTS=<a,b> for
# the control plane's hostname(s)/IP(s); the loopback names and the machine
# hostname are always included. On a reinstall, omitting them keeps the list
# already persisted next to the CA.
#
# Localhost agent (on by default). The installer also installs and starts the
# node agent on this host and registers it as the control plane's first node,
# over the mTLS channel above (127.0.0.1 is always a listener SAN, so no
# --cp-host is needed for it). It calls install-agent.sh --full --ca with the
# provisioned CA, so Docker Engine and the compose plugin are set up too, and
# pins the agent to the same release tag the control plane just installed. The
# node id defaults to "<hostname>-agent" (a default, not a reserved id: only
# the control plane's own listener identities are refused) and may be
# overridden with GOTHAM_AGENT_NODE_ID; the dial address defaults to
# 127.0.0.1:9442 and may be overridden with GOTHAM_AGENT_CP_ADDR. Both are
# passed only when agent.env does not already define them, so re-running the
# installer keeps a remote-agent setup or operator edits and never repoints or
# duplicates the node (a hostname change is safe). Registration is automatic:
# the agent registers on first contact and its heartbeats flip the server row
# the UI lists to ready. The localhost agent needs Ubuntu/Debian + systemd +
# a supported architecture: anywhere else it is skipped with a notice and the
# control plane install still succeeds. If the agent step fails on a supported
# platform the control plane is left installed and running (nothing is rolled
# back) and the installer exits nonzero with the exact retry command.
# Re-running the installer re-runs the agent install, which is idempotent (the
# unit is restarted onto the new binary). Pass --no-local-agent (or
# GOTHAM_NO_LOCAL_AGENT=1) to skip it for a remote-only control plane.
#
# Usage:
#   sudo ./install.sh [--cp-host <name-or-ip>]... [--no-local-agent] [--dry-run]

set -eu
# A permissive base umask: shared directories (/etc/gotham, /usr/libexec/gotham,
# /var/lib/gotham*) must be world-traversable and the service user must be able
# to read the env/JWT files. A restrictive umask is applied only around the
# secret writes below, so package installs and directory creation are normal.
umask 022

BINARY_NAME="gotham"
FAMILY=""
SERVICE_USER="gotham"
DEFAULT_REPO="justindeelux/gotham"

# The release trust anchor. This is the base64 raw Ed25519 public key printed by
# `cmd/signer keygen`; the matching PEM is committed at
# deploy/gotham-signing-key.pub and published with every release. A release
# embeds the same key in the binaries. Never fetch this from the download
# channel.
GOTHAM_RELEASE_PUBLIC_KEY_B64="Yt6nz1gGQWF7Bfc9MCt/gQXbPMzhN9OygrUkOEFYdwQ="

DRY_RUN=0
NO_LOCAL_AGENT=0
CP_HOSTS_OPT=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --dry-run) DRY_RUN=1 ;;
        --no-local-agent) NO_LOCAL_AGENT=1 ;;
        --cp-host)
            [ "$#" -ge 2 ] || { echo "--cp-host requires a hostname or IP" >&2; exit 2; }
            CP_HOSTS_OPT="${CP_HOSTS_OPT} $2"
            shift
            ;;
        --cp-host=*) CP_HOSTS_OPT="${CP_HOSTS_OPT} ${1#--cp-host=}" ;;
        -h | --help)
            # Print every leading comment line (the header), not a fixed range,
            # so the documented flags cannot drift out of --help.
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
# GOTHAM_NO_LOCAL_AGENT=1 is the environment form of --no-local-agent.
if [ "${GOTHAM_NO_LOCAL_AGENT:-0}" = "1" ]; then
    NO_LOCAL_AGENT=1
fi

# gRPC listener SAN hosts: --cp-host values plus GOTHAM_GRPC_HOSTS
# (comma-separated). Empty is fine; serve always adds the loopback names and the
# machine hostname.
CP_HOSTS="${GOTHAM_GRPC_HOSTS:-}"
CP_HOSTS="${CP_HOSTS}${CP_HOSTS_OPT}"
CP_HOSTS="$(printf '%s' "${CP_HOSTS}" | tr ',' ' ')"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
for sibling in release-verify.sh install-agent-lib.sh gotham-update.sh gotham-updater.conf install-sudoers.sh gotham.service; do
    if [ ! -f "${SCRIPT_DIR}/${sibling}" ]; then
        echo "install.sh: ${sibling} must be next to this script (run it from the repository checkout)" >&2
        exit 2
    fi
done
# shellcheck source=deploy/release-verify.sh
. "${SCRIPT_DIR}/release-verify.sh"
# shellcheck source=deploy/install-agent-lib.sh
. "${SCRIPT_DIR}/install-agent-lib.sh"

log() { echo "==> $*"; }

# run executes a command, or prints it in dry-run mode.
run() {
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] $*"
        return 0
    fi
    "$@"
}

# _is_loopback_cp_addr reports whether a GOTHAM_AGENT_CP_ADDR value points at
# this host's loopback: a numeric 127.0.0.0/8 IPv4 literal, "localhost" (any
# case), or ::1 (bare or bracketed, with or without :port). Anything else,
# including a hostname that merely starts with 127. (127.evil.example.com),
# is remote: re-running the localhost step over it would overwrite another
# control plane's agent.
_is_loopback_cp_addr() {
    _lc_addr=$1
    case "${_lc_addr}" in
        '['*']'*)
            _lc_host="${_lc_addr%%\]*}"
            _lc_host="${_lc_host#\[}"
            ;;
        *)
            _lc_rest="${_lc_addr#*:}"
            case "${_lc_rest}" in
                *:*) _lc_host="${_lc_addr}" ;; # unbracketed IPv6: no port strip
                *) _lc_host="${_lc_addr%:*}" ;;
            esac
            ;;
    esac
    _lc_lower="$(printf '%s' "${_lc_host}" | tr '[:upper:]' '[:lower:]')"
    if [ "${_lc_lower}" = "localhost" ] || [ "${_lc_host}" = "::1" ]; then
        return 0
    fi
    # Numeric 127.0.0.0/8 only: four dot-separated parts, first 127, the rest
    # 0-255. A non-numeric 127.* name falls through to remote.
    _lc_rest="${_lc_host}"
    _lc_n=0
    _lc_ok=1
    while [ -n "${_lc_rest}" ]; do
        _lc_part="${_lc_rest%%.*}"
        case "${_lc_rest}" in
            *.*) _lc_rest="${_lc_rest#*.}" ;;
            *) _lc_rest="" ;;
        esac
        _lc_n=$((_lc_n + 1))
        case "${_lc_n}" in
            1) [ "${_lc_part}" = "127" ] || _lc_ok=0 ;;
            *)
                case "${_lc_part}" in
                    '' | *[!0-9]*) _lc_ok=0 ;;
                    *)
                        [ "${#_lc_part}" -le 3 ] || _lc_ok=0
                        [ "${_lc_part}" -le 255 ] 2>/dev/null || _lc_ok=0
                        ;;
                esac
                ;;
        esac
    done
    [ "${_lc_ok}" -eq 1 ] && [ "${_lc_n}" -eq 4 ]
}

# Install prefix. Empty = the production layout from deploy/README.md. A
# non-empty prefix is the test-only redirect used by the dry run so no host
# path is touched.
PREFIX="${GOTHAM_INSTALL_ROOT:-}"
if [ -n "${PREFIX}" ]; then
    case "${PREFIX}" in
        /*) ;;
        *) die "GOTHAM_INSTALL_ROOT must be an absolute path" ;;
    esac
    # A trailing slash changes nothing about where the prefix points, so strip
    # it before the root check: "/" (and "///") is the real root, not a
    # sandbox, and must never enable test mode.
    while [ "${PREFIX}" != "/" ] && [ "${PREFIX%/}" != "${PREFIX}" ]; do
        PREFIX="${PREFIX%/}"
    done
    if [ "${PREFIX}" = "/" ]; then
        die "GOTHAM_INSTALL_ROOT must be a non-/ sandbox path, not the real root"
    fi
    TEST_MODE=1
else
    TEST_MODE=0
fi

# Explicit test-harness flag. The GOTHAM_OS_RELEASE_FILE /
# GOTHAM_TEST_UNAME_M / GOTHAM_AGENT_ENV_FILE path seams below are honoured
# only when IN_TEST is 1 AND the run is a dry run (a dry run changes nothing,
# so the flag alone is safe there): never on a run that can execute the agent
# step. IN_TEST is 1 inside a GOTHAM_INSTALL_ROOT sandbox, or with
# GOTHAM_INSTALL_TEST=1 together with --dry-run. A bare GOTHAM_INSTALL_TEST=1
# on a real run changes nothing, so a stray export cannot redirect the
# install. The agent-script seam (GOTHAM_INSTALL_TEST_AGENT_SCRIPT, sandbox +
# RUN_AGENT only) is the one exception: it names the fake the sandbox run
# executes, and a sandbox RUN_AGENT run without it is refused outright.
IN_TEST=0
if [ "${TEST_MODE}" -eq 1 ]; then
    IN_TEST=1
elif [ "${GOTHAM_INSTALL_TEST:-0}" = "1" ] && [ "${DRY_RUN}" -eq 1 ]; then
    IN_TEST=1
fi

# sh_quote prints one argument for safe re-execution: bare when it holds
# only shell-safe characters, single-quoted otherwise.
sh_quote() {
    case "$1" in
        '' | *[!A-Za-z0-9_@%+=:,./-]*) printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")" ;;
        *) printf '%s' "$1" ;;
    esac
}

ETC_DIR="${PREFIX}/etc/gotham"
STATE_DIR="${PREFIX}/var/lib/gotham"
BIN_DIR="${STATE_DIR}/bin"
INSTALL_PATH="${BIN_DIR}/${BINARY_NAME}"
CA_DIR="${STATE_DIR}/ca"
STATUS_DIR="${PREFIX}/var/lib/gotham-updater"
WRAPPER_PATH="${PREFIX}/usr/libexec/gotham/gotham-update"
WRAPPER_CONF="${ETC_DIR}/updater.conf"
ENV_FILE="${ETC_DIR}/gotham.env"
SERVICE_FILE="${PREFIX}/etc/systemd/system/gotham.service"

# ---- Validate every input before the first mutation -------------------------
# The DSN/Redis values are checked here, before the service user,
# directories, binary or any env file is created, so a bad value fails with
# nothing created. Whether the localhost agent step will run is decided here
# too: every skip reason (--no-local-agent, distro, systemd, architecture, a
# remote prior agent.env) only reads files, so the decision moves above the
# first mutation as well. The agent values go through the same validator
# functions install-agent.sh runs (agent_env_validate also covers values a
# previous agent.env would preserve for keys this run leaves unset), but only
# when the agent step will actually run: a value the skipped step would never
# use must not abort the control-plane install. A failing agent step itself
# still never rolls the control plane back (see the localhost-agent section
# below).
DEFAULT_DSN="postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
DEFAULT_REDIS="localhost:6379"

# Values a previous install already wrote, so a repair/upgrade keeps a managed
# DSN and operator edits instead of falling back to local defaults. A DSN or
# Redis address given on the command line still wins.
ENV_PREV=""
if [ -f "${ENV_FILE}" ]; then
    ENV_PREV="$(cat "${ENV_FILE}")"
fi
env_prev() {
    printf '%s\n' "${ENV_PREV}" | sed -n "s/^$1=//p" | head -n1
}
DSN="${GOTHAM_DATABASE_DSN:-$(env_prev GOTHAM_DATABASE_DSN)}"
DSN="${DSN:-${DEFAULT_DSN}}"
REDIS_ADDR="${GOTHAM_REDIS_ADDR:-$(env_prev GOTHAM_REDIS_ADDR)}"
REDIS_ADDR="${REDIS_ADDR:-${DEFAULT_REDIS}}"

# A control character (notably a newline smuggling a second line) in either
# value would corrupt root-owned gotham.env, so both are rejected before
# anything is written.
_stripped_dsn="$(printf '%s' "${DSN}" | tr -d '\000-\037\177')"
[ "${_stripped_dsn}" = "${DSN}" ] \
    || die "GOTHAM_DATABASE_DSN contains a control character (rejected)"
_stripped_redis="$(printf '%s' "${REDIS_ADDR}" | tr -d '\000-\037\177')"
[ "${_stripped_redis}" = "${REDIS_ADDR}" ] \
    || die "GOTHAM_REDIS_ADDR contains a control character (rejected)"

if [ "${NO_LOCAL_AGENT}" -eq 0 ]; then
    # The skip decision lives here, before the first mutation, and the
    # localhost-agent section below reuses it: the platform/env lookups only
    # read files. _la_agent_env is the path install-agent.sh actually
    # writes; the GOTHAM_AGENT_ENV_FILE / GOTHAM_OS_RELEASE_FILE /
    # GOTHAM_TEST_UNAME_M seams redirect it only on a dry run in test mode
    # (a sandbox real run uses a scratch installer copy with the paths baked
    # in — see deploy/test-release-install.sh).
    LOCAL_AGENT_SKIP=""
    _la_os_release="/etc/os-release"
    _la_agent_env="/etc/gotham/agent.env"
    if [ "${IN_TEST}" -eq 1 ] && [ "${DRY_RUN}" -eq 1 ]; then
        if [ -n "${GOTHAM_OS_RELEASE_FILE:-}" ]; then
            _la_os_release="${GOTHAM_OS_RELEASE_FILE}"
        fi
        if [ -n "${GOTHAM_AGENT_ENV_FILE:-}" ]; then
            _la_agent_env="${GOTHAM_AGENT_ENV_FILE}"
        fi
    fi
    _la_distro=""
    if [ -f "${_la_os_release}" ]; then
        _la_distro="$(sed -n 's/^ID=//p' "${_la_os_release}" | head -n1 | tr -d '"')"
    fi
    case "${_la_distro}" in
        ubuntu | debian) ;;
        *) LOCAL_AGENT_SKIP="automatic localhost agent setup supports Ubuntu/Debian only (found '${_la_distro:-unknown}')" ;;
    esac
    if [ -z "${LOCAL_AGENT_SKIP}" ] && ! command -v systemctl >/dev/null 2>&1; then
        LOCAL_AGENT_SKIP="systemctl not found; the localhost agent needs a systemd host"
    fi
    if [ -z "${LOCAL_AGENT_SKIP}" ]; then
        _la_uname_m="$(uname -m)"
        if [ "${IN_TEST}" -eq 1 ] && [ "${DRY_RUN}" -eq 1 ] && [ -n "${GOTHAM_TEST_UNAME_M:-}" ]; then
            _la_uname_m="${GOTHAM_TEST_UNAME_M}"
        fi
        case "${_la_uname_m}" in
            x86_64 | amd64 | aarch64 | arm64) ;;
            *) LOCAL_AGENT_SKIP="unsupported architecture: ${_la_uname_m}" ;;
        esac
    fi
    if [ -z "${LOCAL_AGENT_SKIP}" ] && [ -z "${GOTHAM_AGENT_CP_ADDR:-}" ] && [ -f "${_la_agent_env}" ]; then
        # Never touch another control plane's agent: when the prior agent.env
        # points at a remote control plane (an explicit address this run
        # still wins and repoints deliberately), the whole step is skipped.
        _la_prior_cp_addr="$(sed -n 's/^[[:space:]]*GOTHAM_AGENT_CP_ADDR=//p' "${_la_agent_env}" | tail -n1)"
        if [ -n "${_la_prior_cp_addr}" ] && ! _is_loopback_cp_addr "${_la_prior_cp_addr}"; then
            LOCAL_AGENT_SKIP="agent.env points at a remote control plane (${_la_prior_cp_addr}); leaving it untouched"
        fi
    fi
    if [ -z "${LOCAL_AGENT_SKIP}" ]; then
        agent_env_validate "${_la_agent_env}" "${CA_DIR}/ca.crt" 0 \
            || die "invalid agent configuration (see above); refusing to install"
    fi
fi

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
# sudo/visudo are only needed when the sudoers drop-in is installed; --dry-run
# and test mode skip that step, so do not require them there (a non-root
# dry-run may not have /usr/sbin on PATH, where visudo lives).
if [ "${TEST_MODE}" -eq 0 ] && [ "${DRY_RUN}" -eq 0 ]; then
    require_cmd sudo "apt-get install -y sudo"
    require_cmd visudo "apt-get install -y sudo"
fi
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
DEFAULT_RELEASES_URL="https://github.com/${REPO}/releases"
if [ -n "${GOTHAM_BASE_URL:-}" ]; then
    # Full asset base URL (e.g. a static mirror or .../releases/download/<tag>).
    RELEASE_BASE="${GOTHAM_BASE_URL%/}"
    [ -n "${GOTHAM_VERSION:-}" ] || die "GOTHAM_VERSION is required with GOTHAM_BASE_URL"
    VERSION="${GOTHAM_VERSION}"
else
    RELEASES_BASE="${GOTHAM_RELEASES_URL:-${DEFAULT_RELEASES_URL}}"
    RELEASES_BASE="${RELEASES_BASE%/}"
    if [ -n "${GOTHAM_VERSION:-}" ]; then
        VERSION="${GOTHAM_VERSION}"
    else
        # Resolve the current tag from the FIRST redirect of /releases/latest.
        # Following it with -L lands on a CDN URL that carries no tag, so only
        # the first hop is used; all downloads then pin to that one tag.
        redirect="$(curl -fsS -o /dev/null -w '%{redirect_url}' "${RELEASES_BASE}/latest" || true)"
        VERSION="${redirect%/}"
        VERSION="${VERSION##*/}"
        printf '%s' "${VERSION}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' \
            || die "could not resolve the latest release tag from ${RELEASES_BASE}/latest"
    fi
    RELEASE_BASE="${RELEASES_BASE}/download/${VERSION}"
fi

log "installing ${BINARY_NAME} ${VERSION} for linux/${ARCH}"

# Materialize the pinned public key. The test-only override is honoured only in
# test mode (GOTHAM_INSTALL_ROOT set); elsewhere it is ignored with a warning so
# an exported GOTHAM_INSTALL_TEST_PUBLIC_KEY cannot silently replace the anchor.
PUBKEY_SOURCE="${GOTHAM_RELEASE_PUBLIC_KEY_B64}"
if [ -n "${GOTHAM_INSTALL_TEST_PUBLIC_KEY:-}" ]; then
    if [ "${TEST_MODE}" -eq 1 ]; then
        log "WARNING: using the GOTHAM_INSTALL_TEST_PUBLIC_KEY override (test mode)"
        PUBKEY_SOURCE="${GOTHAM_INSTALL_TEST_PUBLIC_KEY}"
    else
        echo "gotham-install: WARNING: ignoring GOTHAM_INSTALL_TEST_PUBLIC_KEY outside test mode" >&2
    fi
fi

# Private scratch dir (0700): the pinned key, the verified binary and the
# generated secrets live here and are removed on exit.
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gotham-install.XXXXXX")"
PUBKEY_FILE="${WORK_DIR}/release.pub"
TMP_BINARY="${WORK_DIR}/${BINARY_NAME}"
# Clean up the private scratch dir on normal exit, and abort on a signal (a
# cleanup-only INT/TERM trap would let the install carry on).
trap 'rm -rf "${WORK_DIR}"' EXIT
trap 'exit 1' INT TERM
materialize_public_key "${PUBKEY_SOURCE}" "${PUBKEY_FILE}"

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${PUBKEY_FILE}"
    echo "[dry-run] verify_release ${RELEASE_BASE} ${VERSION} ${ARCH} ${FAMILY} <pubkey> ${INSTALL_PATH}"
else
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
run mkdir -p "${STATE_DIR}" "${BIN_DIR}"
run chmod 0755 "${STATE_DIR}" "${BIN_DIR}"
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
run chmod 0755 "${ETC_DIR}"
JWT_KEY="${ETC_DIR}/jwt_ed25519.key"
JWT_PUB="${ETC_DIR}/jwt_ed25519.pub"

if [ "${DRY_RUN}" -eq 0 ]; then
    # Restrictive umask only around the secret material, so it is never briefly
    # world-readable; the shared directories stay 0755 (base umask 022).
    (
        umask 077
        # Preserve the secret and JWT keys across reinstalls; only generate missing.
        SECRET_KEY="$(env_prev GOTHAM_SECRET_KEY)"
        [ -n "${SECRET_KEY}" ] || SECRET_KEY="$(openssl rand -base64 32)"
        if [ ! -f "${JWT_KEY}" ]; then
            openssl genpkey -algorithm ed25519 -out "${JWT_KEY}"
        fi
        if [ ! -f "${JWT_PUB}" ]; then
            openssl pkey -in "${JWT_KEY}" -pubout -out "${JWT_PUB}"
        fi
        # Rewrite the managed keys and keep every other (operator) line untouched.
        env_tmp="${ENV_FILE}.tmp.$$"
        # The temp copy is root-only and holds the secret key: remove it if any
        # step below aborts. The success path moves it into place first, so this
        # is a no-op then. INT/TERM must abort, not merely clean up: a trap that
        # only removes the temp would let the subshell resume and `mv` a temp
        # holding just the operator lines over gotham.env, silently dropping the
        # managed keys and the secret.
        trap 'rm -f "${env_tmp}"' EXIT
        trap 'exit 1' INT TERM
        {
            echo "# Gotham control-plane environment. Read by gotham.service (EnvironmentFile)."
            echo "GOTHAM_DATABASE_DSN=${DSN}"
            echo "GOTHAM_REDIS_ADDR=${REDIS_ADDR}"
            echo "GOTHAM_CA_DIR=${CA_DIR}"
            echo "GOTHAM_SECRET_KEY=${SECRET_KEY}"
            echo "GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH=${JWT_KEY}"
            echo "GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH=${JWT_PUB}"
        } >"${env_tmp}"
        if [ -n "${ENV_PREV}" ]; then
            # Keep the operator lines: drop every managed key (tolerating leading
            # whitespace, so a hand-indented key cannot silently override the
            # managed value) and the header comment. A single grep keeps the exit
            # status exact — with a pipeline only the last stage's status is
            # visible, so a failure in an earlier stage would be masked. 1 means
            # "nothing matched" (the normal "no operator settings" case) and is
            # fine; anything else aborts rather than silently dropping operator
            # settings.
            filter_status=0
            preserved=$(printf '%s\n' "${ENV_PREV}" \
                | grep -v -E \
                    -e '^[[:space:]]*(GOTHAM_DATABASE_DSN|GOTHAM_REDIS_ADDR|GOTHAM_CA_DIR|GOTHAM_SECRET_KEY|GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH|GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH)=' \
                    -e '^# Gotham control-plane environment\. Read by gotham\.service') \
                || filter_status=$?
            case "${filter_status}" in
                0) ;;
                1) preserved="" ;;
                *) die "could not filter the existing ${ENV_FILE} (grep exit ${filter_status})" ;;
            esac
            if [ -n "${preserved}" ]; then
                printf '%s\n' "${preserved}" >>"${env_tmp}" \
                    || die "could not preserve operator settings in ${ENV_FILE}"
            fi
        fi
        chmod 0640 "${env_tmp}"
        mv -f "${env_tmp}" "${ENV_FILE}"
        chmod 0640 "${ENV_FILE}" "${JWT_KEY}"
        chmod 0644 "${JWT_PUB}"
    )
    if [ "${TEST_MODE}" -eq 0 ]; then
        chown root:"${SERVICE_USER}" "${ENV_FILE}" "${JWT_KEY}"
        chown root:root "${JWT_PUB}"
    fi
else
    echo "[dry-run] write ${ENV_FILE}, ${JWT_KEY}, ${JWT_PUB}"
fi

# ---- Certificate authority (mTLS) -------------------------------------------
# Provision the CA the gRPC gateway uses for TLS. `gotham ca init` is
# idempotent and writes ca.crt/ca.key (0600) under the service-owned
# GOTHAM_CA_DIR, so a fresh install never serves the agent channel in plaintext.
# Copy ca.crt to each node for install-agent.sh --ca.
#
# The listener certificate SANs come from --cp-host / GOTHAM_GRPC_HOSTS and are
# persisted next to the CA. With none, and no list from a previous install, seed
# the machine's FQDN so a node can dial the control plane by name.
CP_HOSTS_LIST="${CP_HOSTS}"
if [ -z "${CP_HOSTS_LIST}" ] && [ ! -f "${CA_DIR}/hosts" ]; then
    default_host="$(hostname -f 2>/dev/null || true)"
    [ -n "${default_host}" ] || default_host="$(hostname 2>/dev/null || true)"
    CP_HOSTS_LIST="${default_host}"
fi
ca_init_args=""
for host in ${CP_HOSTS_LIST}; do
    [ -n "${host}" ] && ca_init_args="${ca_init_args} --host ${host}"
done
log "initializing the mTLS certificate authority at ${CA_DIR}"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] GOTHAM_CA_DIR=${CA_DIR} ${INSTALL_PATH} ca init${ca_init_args}"
elif [ "${TEST_MODE}" -eq 1 ]; then
    GOTHAM_CA_DIR="${CA_DIR}" "${INSTALL_PATH}" ca init ${ca_init_args}
else
    runuser -u "${SERVICE_USER}" -- env GOTHAM_CA_DIR="${CA_DIR}" "${INSTALL_PATH}" ca init ${ca_init_args}
fi

# ---- Self-update chain (mirror of deploy/README.md) -------------------------
log "installing the update wrapper ${WRAPPER_PATH}"
run mkdir -p "$(dirname "${WRAPPER_PATH}")"
run chmod 0755 "$(dirname "${WRAPPER_PATH}")"
render_file "${SCRIPT_DIR}/gotham-update.sh" "${WRAPPER_PATH}" 0755

log "installing ${WRAPPER_CONF}"
render_file "${SCRIPT_DIR}/gotham-updater.conf" "${WRAPPER_CONF}" 0644

log "creating the root-owned status directory ${STATUS_DIR}"
run mkdir -p "${STATUS_DIR}"
run chmod 0755 "${STATUS_DIR}"

if [ "${TEST_MODE}" -eq 0 ] && [ "${DRY_RUN}" -eq 0 ]; then
    log "installing the sudoers rule"
    # Invoke via sh so a checkout that lost the exec bit still installs.
    sh "${SCRIPT_DIR}/install-sudoers.sh" "${SERVICE_USER}"
fi

log "installing systemd unit ${SERVICE_FILE}"
render_file "${SCRIPT_DIR}/gotham.service" "${SERVICE_FILE}" 0644

if [ "${TEST_MODE}" -eq 1 ] && [ "${GOTHAM_INSTALL_TEST_RUN_AGENT:-0}" != "1" ]; then
    log "test mode: skipping service activation and the localhost agent install"
    log "done (test install under ${PREFIX})"
    exit 0
fi
# GOTHAM_INSTALL_TEST_RUN_AGENT=1 (sandbox test mode only) runs past
# the exit above with shims for systemctl and the agent installer, so the
# suite can execute the real agent-failure path end to end.

# ---- Dependencies (PostgreSQL + Redis) --------------------------------------
# Only provision local services when the resolved DSN is the built-in local
# default; a managed DSN (from the CLI or a previous install) is left alone.
if [ "${DRY_RUN}" -eq 0 ] && [ "${TEST_MODE}" -eq 0 ] && [ "${GOTHAM_SKIP_DEPS:-0}" != "1" ] && [ "${DSN}" = "${DEFAULT_DSN}" ]; then
    if ! command -v psql >/dev/null 2>&1 || ! command -v redis-server >/dev/null 2>&1; then
        if command -v apt-get >/dev/null 2>&1; then
            log "installing PostgreSQL and Redis"
            # Package maintainer scripts must run under a normal umask.
            ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get update )
            ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql redis-server )
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
# Skipped in test mode (no database there; the agent-failure suite covers the
# tail with a systemctl shim instead).
if [ "${DRY_RUN}" -eq 0 ] && [ "${TEST_MODE}" -eq 0 ]; then
    log "applying database migrations"
    # Pass the DSN and binary path positionally: interpolating the DSN into a
    # single-quoted sh -c would let a quote in the DSN run commands as the
    # service user.
    runuser -u "${SERVICE_USER}" -- sh -c \
        'cd / && GOTHAM_DATABASE_DSN="$1" exec "$2" migrate up' gotham-migrate "${DSN}" "${INSTALL_PATH}" \
        || die "database migrations failed (check PostgreSQL and GOTHAM_DATABASE_DSN)"
fi

log "starting ${BINARY_NAME}"
run systemctl daemon-reload
run systemctl enable --now "${BINARY_NAME}.service"

# ---- Localhost agent (the control plane's first node) -----------------------
# On by default: install and start the agent on this host, pointed at the
# loopback gRPC listener over the mTLS channel provisioned above. The dial
# address needs no --cp-host because the loopback names are always listener
# SANs. Idempotent: the agent installer keeps the prior node id and address and
# restarts the unit onto the newly installed binary. Opt out with
# --no-local-agent for a remote-only control plane.
if [ "${NO_LOCAL_AGENT}" -eq 0 ]; then
    # The skip decision was computed before the first mutation (see above)
    # and is reused here; only the sibling check still runs at this point.
    # The production paths below are fixed; a sandbox real run uses a scratch
    # installer copy with them baked in, and the dry-run test seams were
    # already honoured by the early decision (see
    # deploy/test-release-install.sh).
    if [ -n "${LOCAL_AGENT_SKIP}" ]; then
        log "skipping the localhost agent install: ${LOCAL_AGENT_SKIP}"
        log "install the agent manually with deploy/install-agent.sh once the platform supports it"
    else
        for agent_sibling in install-agent.sh install-agent-lib.sh gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service; do
            if [ ! -f "${SCRIPT_DIR}/${agent_sibling}" ]; then
                die "install.sh: ${agent_sibling} must be next to this script for the localhost agent install (--no-local-agent to skip)"
            fi
        done
        # A re-run keeps the operator's agent: GOTHAM_AGENT_NODE_ID /
        # GOTHAM_AGENT_CP_ADDR are passed only when agent.env does not already
        # define them. A value set in this run's environment still wins;
        # otherwise the prior file's values are kept by passing nothing, so a
        # hostname change is never repointed and no duplicate node is created.
        # The prior-env lookup reads the path the early decision used
        # (_la_agent_env, baked on a scratch copy for sandbox real runs).
        agent_env_prev() {
            [ -f "${_la_agent_env}" ] || return 0
            sed -n "s/^[[:space:]]*$1=//p" "${_la_agent_env}" | tail -n1
        }
        # The remote-agent.env skip already fired above when no explicit
        # address repoints deliberately; nothing more to check here.
        LOCAL_AGENT_NODE_ID="${GOTHAM_AGENT_NODE_ID:-}"
        if [ -z "${LOCAL_AGENT_NODE_ID}" ] && [ -z "$(agent_env_prev GOTHAM_AGENT_NODE_ID)" ]; then
            LOCAL_AGENT_NODE_ID="$(hostname 2>/dev/null || true)-agent"
            [ "${LOCAL_AGENT_NODE_ID}" != "-agent" ] || LOCAL_AGENT_NODE_ID="local-agent"
        fi
        LOCAL_AGENT_CP_ADDR="${GOTHAM_AGENT_CP_ADDR:-}"
        if [ -z "${LOCAL_AGENT_CP_ADDR}" ] && [ -z "$(agent_env_prev GOTHAM_AGENT_CP_ADDR)" ]; then
            LOCAL_AGENT_CP_ADDR="127.0.0.1:9442"
        fi
        # The retry line below reuses exactly these assignments, shell-quoted
        # for safe re-execution. Which script runs: the real installer by
        # default; in a GOTHAM_INSTALL_ROOT sandbox together with
        # GOTHAM_INSTALL_TEST_RUN_AGENT=1 the GOTHAM_INSTALL_TEST_AGENT_SCRIPT
        # fake (never from a bare GOTHAM_INSTALL_TEST=1, and never in a real
        # run). RUN_AGENT=1 in a sandbox without the script seam is refused:
        # it would execute the real install-agent.sh. The retry line always
        # shows the canonical command.
        LOCAL_AGENT_ENV="GOTHAM_VERSION=$(sh_quote "${VERSION}")"
        [ -z "${LOCAL_AGENT_CP_ADDR}" ] \
            || LOCAL_AGENT_ENV="GOTHAM_AGENT_CP_ADDR=$(sh_quote "${LOCAL_AGENT_CP_ADDR}") ${LOCAL_AGENT_ENV}"
        [ -z "${LOCAL_AGENT_NODE_ID}" ] \
            || LOCAL_AGENT_ENV="GOTHAM_AGENT_NODE_ID=$(sh_quote "${LOCAL_AGENT_NODE_ID}") ${LOCAL_AGENT_ENV}"
        LOCAL_AGENT_RETRY="${LOCAL_AGENT_ENV} sh $(sh_quote "${SCRIPT_DIR}/install-agent.sh") --full --ca $(sh_quote "${CA_DIR}/ca.crt")"
        LOCAL_AGENT_FAILED=0
        _agent_script="${SCRIPT_DIR}/install-agent.sh"
        if [ "${TEST_MODE}" -eq 1 ] && [ "${GOTHAM_INSTALL_TEST_RUN_AGENT:-0}" = "1" ]; then
            [ -n "${GOTHAM_INSTALL_TEST_AGENT_SCRIPT:-}" ] \
                || die "GOTHAM_INSTALL_TEST_RUN_AGENT=1 requires GOTHAM_INSTALL_TEST_AGENT_SCRIPT=<fake agent script>; refusing to run the real install-agent.sh in a sandbox"
            _agent_script="${GOTHAM_INSTALL_TEST_AGENT_SCRIPT}"
        fi
        if [ "${DRY_RUN}" -eq 1 ]; then
            echo "[dry-run] ${LOCAL_AGENT_RETRY}"
        else
            _agent_label="${LOCAL_AGENT_NODE_ID:-$(agent_env_prev GOTHAM_AGENT_NODE_ID)}"
            [ -n "${_agent_label}" ] || _agent_label="the existing node"
            log "installing the localhost agent node ${_agent_label} (--no-local-agent to skip)"
            # Invoke via sh so a checkout that lost the exec bit still installs.
            # The release location (mirror/base URL, repo) is inherited from the
            # environment; the tag is pinned to the control plane's so both
            # binaries come from one release. A failing agent step must NOT roll
            # the control plane back: it is already migrated and started above,
            # so record the failure and exit nonzero only at the end.
            #
            # The assignments travel as positional parameters through env, one
            # per argument, never through an unquoted string: values holding
            # spaces, quotes, $() or globs arrive verbatim instead of being
            # split (or run). The child always runs scrubbed: every test seam
            # is removed from its environment (defense in depth — the
            # installers ignore them anyway), so a stray export cannot
            # redirect it; the intended values travel as "$@" below.
            (
                set -- "GOTHAM_VERSION=${VERSION}"
                if [ -n "${LOCAL_AGENT_CP_ADDR}" ]; then
                    set -- "$@" "GOTHAM_AGENT_CP_ADDR=${LOCAL_AGENT_CP_ADDR}"
                fi
                if [ -n "${LOCAL_AGENT_NODE_ID}" ]; then
                    set -- "$@" "GOTHAM_AGENT_NODE_ID=${LOCAL_AGENT_NODE_ID}"
                fi
                env -u GOTHAM_OS_RELEASE_FILE -u GOTHAM_APT_ROOT \
                    -u GOTHAM_AGENT_ENV_FILE -u GOTHAM_TEST_UNAME_M \
                    -u GOTHAM_INSTALL_TEST -u GOTHAM_INSTALL_TEST_RUN_AGENT \
                    -u GOTHAM_INSTALL_TEST_AGENT_SCRIPT \
                    -u GOTHAM_INSTALL_TEST_PUBLIC_KEY \
                    "$@" sh "${_agent_script}" --full --ca "${CA_DIR}/ca.crt"
            ) || LOCAL_AGENT_FAILED=1
        fi
    fi
else
    log "skipping the localhost agent install (--no-local-agent)"
fi

HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "${HOST_IP}" ] || HOST_IP="127.0.0.1"

cat <<EOF

Gotham ${VERSION} is installed.

  Web UI:   http://${HOST_IP}:8000
  Status:   systemctl status gotham
  Logs:     journalctl -u gotham -f

First login:
  1. Open the Web UI and create an account through the sign-up form.
  2. Sign in: the Servers page already lists this host's agent node (ready
     once its first heartbeat lands). Add more nodes with deploy/install-agent.sh.
  Platform-global operations (node-wide proxy sync, DNS providers) also require
  the account email in PLATFORM_ADMINS in /etc/gotham/gotham.env.

Self-update checking is enabled by default. To apply new releases unattended,
add AUTO_UPDATE=true to /etc/gotham/gotham.env (operator edits there are kept
across reinstalls).
EOF

# A failed localhost agent step leaves the control plane installed and running
# (nothing was rolled back): report it loudly with the exact retry command and
# exit nonzero only now that the control plane is fully up.
if [ "${LOCAL_AGENT_FAILED:-0}" -eq 1 ]; then
    cat >&2 <<EOF

================================================================
WARNING: the control plane is installed and running, but the
localhost agent install failed. Nothing was rolled back.
Retry only the agent step with:
  ${LOCAL_AGENT_RETRY}
or re-run this installer (an existing agent.env is kept as-is).
================================================================
EOF
    exit 1
fi