#!/bin/sh
#
# install.sh installs the Gotham control plane on a systemd Linux host.
#
# It is the signed one-line installer for a clean Ubuntu 22.04 VPS: it detects
# the architecture, downloads the release binary, verifies the Ed25519-signed
# manifest and the artifact digest (see deploy/release-verify.sh), installs the
# binary into the BE-9.1 layout, installs the root-owned update wrapper, its
# config, the sudoers rule and the systemd unit, creates the first admin
# account, and prints the web UI URL and the first-login steps.
#
# The first admin account is collected before the first mutation (a tty
# prompt, or GOTHAM_ADMIN_EMAIL non-interactively) and created right after
# the migrations and before the service starts, so nobody can register first
# through the open web form. On a terminal, GOTHAM_ADMIN_EMAIL is the email
# prompt default (empty keeps it) and GOTHAM_ADMIN_PASSWORD_FILE is used
# without prompting; without a terminal they drive creation directly (a
# missing email skips with the manual command). A generated password is
# printed once in the final summary. The password travels on stdin only.
#
# Download this script and run it directly; no checkout is needed:
#
#   curl -fsSL https://raw.githubusercontent.com/justindeelux/gotham/main/deploy/install.sh | sudo sh
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
#   GOTHAM_INSTALL_TEST_ADMIN_BIN=<fake> + GOTHAM_INSTALL_TEST_ADMIN_TTY=<path>
#                               test-only: with GOTHAM_INSTALL_ROOT, use <fake>
#                               instead of the installed binary for the first-
#                               admin/domain steps (`admin` / `instance`),
#                               and drive interactive prompts from <path>
#                               instead of /dev/tty. Without the BIN seam test
#                               mode skips admin creation; /dev/tty is never
#                               opened in test mode unless the TTY seam is set.
#   GOTHAM_INSTALL_TEST_DEPS=1  test-only: with GOTHAM_INSTALL_ROOT and a
#                               baked test apt root, run the PostgreSQL/Redis
#                               dependency step against PATH shims and exit
#                               after writing gotham.env, so the selection,
#                               role and password logic is testable without
#                               root.
#
# Local PostgreSQL and Redis are installed at tested versions: PostgreSQL 16
# (from the PGDG apt repository when the distribution package is older) and
# Redis from the distribution when it ships 6.2 or newer, otherwise 7.2 from
# packages.redis.io. Third-party repositories are used only after every needed
# signing-key fingerprint is verified; a mismatch aborts before any repository
# or package is written or installed. A fresh install creates the local role
# with a random password stored only in gotham.env; an existing role and DSN
# are never changed (the legacy fixed password only produces a warning with the
# manual change command). An existing older major version is never upgraded
# automatically: the installer warns and points at the upgrade guide.
#
# Runtime configuration overrides:
#   GOTHAM_DATABASE_DSN       managed PostgreSQL DSN; skips local provisioning
#   GOTHAM_REDIS_ADDR         Redis host:port (default localhost:6379)
#   GOTHAM_SKIP_DEPS=1        do not install/configure PostgreSQL + Redis
#   GOTHAM_ADMIN_EMAIL        email of the first admin account. Without a
#                             terminal it drives creation non-interactively
#                             (password from GOTHAM_ADMIN_PASSWORD_FILE,
#                             else generated and printed once in the final
#                             summary); without an email creation is skipped
#                             with the manual command. On a terminal it is
#                             the email prompt default (empty keeps it).
#   GOTHAM_ADMIN_PASSWORD_FILE  path to a file holding the first admin
#                             password (single line, root-readable; a warning
#                             is printed when group/world readable). On a
#                             terminal it is used without prompting. Never
#                             passed on a command line or stored anywhere:
#                             it travels to the binary on stdin only. `sh -x`
#                             tracing is disabled around the secret handling.
#
# Re-running the installer preserves /etc/gotham/gotham.env: managed keys are
# refreshed (a DSN given on the command line wins; otherwise the existing value
# is kept) and any operator-added keys (AUTO_UPDATE, ...) are left intact.
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
# The localhost agent is mandatory and uses node id localhost, dialing the
# control plane at 127.0.0.1:9442. Upgrades replace the old local node identity;
# the forward-only migration preserves its UUID and workload references.
# Unsupported platforms or an existing remote agent fail before mutation.
# An agent install failure leaves the control plane running and exits nonzero.
#
# Usage:
#   sudo ./install.sh [--cp-host <name-or-ip>]... [--dry-run]
#                     [--file-manager | --no-file-manager] [--domain <host>]

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

# ---- Pinned third-party apt repositories ------------------------------------
# Used only when the distribution package is too old. Each key is downloaded
# over TLS and its fingerprint compared with the values below before it is
# trusted; a mismatch aborts the install. The fingerprints were verified
# against the official key files (2026-10):
#   PostgreSQL: https://www.postgresql.org/media/keys/ACCC4CF8.asc
#   Redis:      https://packages.redis.io/gpg
PGDG_KEY_URL="https://www.postgresql.org/media/keys/ACCC4CF8.asc"
PGDG_KEY_FINGERPRINT="B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8"
PGDG_REPO_URL="https://apt.postgresql.org/pub/repos/apt"
REDIS_KEY_URL="https://packages.redis.io/gpg"
REDIS_KEY_FINGERPRINT="54318FA4052D1E61A6B6F7BB5F4349D6BF53AA0C"
REDIS_REPO_URL="https://packages.redis.io/deb"
# Tested target and the series pinned on hosts whose distro Redis is older.
MIN_POSTGRES_MAJOR="16"
MIN_REDIS_VERSION="6.2"
REDIS_PINNED_SERIES="7.2"
DB_UPGRADE_DOCS_URL="https://github.com/justindeelux/gotham/blob/main/docs/guide/upgrade-databases.md"

# Fixed production paths. The test suite bakes scratch values into a copy of
# this script (the same pattern as the agent installer's os-release seam).
_DEPS_OS_RELEASE=/etc/os-release
_DEPS_APT_ROOT=
_DEPS_PG_LIB_DIR=/usr/lib/postgresql

DRY_RUN=0
CP_HOSTS_OPT=""
DOMAIN="${GOTHAM_DOMAIN:-}"
DOMAIN_GIVEN=0
[ "${GOTHAM_DOMAIN+x}" != x ] || DOMAIN_GIVEN=1
while [ "$#" -gt 0 ]; do
    case "$1" in
        --dry-run) DRY_RUN=1 ;;
        --file-manager) GOTHAM_AGENT_FILE_MANAGER=yes; export GOTHAM_AGENT_FILE_MANAGER ;;
        --no-file-manager) GOTHAM_AGENT_FILE_MANAGER=no; export GOTHAM_AGENT_FILE_MANAGER ;;
        --domain)
            [ "$#" -ge 2 ] || { echo "--domain requires a hostname" >&2; exit 2; }
            DOMAIN=$2; DOMAIN_GIVEN=1; shift
            ;;
        --domain=*) DOMAIN=${1#--domain=}; DOMAIN_GIVEN=1 ;;
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
# gRPC listener SAN hosts: --cp-host values plus GOTHAM_GRPC_HOSTS
# (comma-separated). Empty is fine; serve always adds the loopback names and the
# machine hostname.
CP_HOSTS="${GOTHAM_GRPC_HOSTS:-}"
CP_HOSTS="${CP_HOSTS}${CP_HOSTS_OPT}"
CP_HOSTS="$(printf '%s' "${CP_HOSTS}" | tr ',' ' ')"

# A piped shell's $0 is sh (or /bin/sh), not the downloaded installer.
# Only an explicit script file can opt into using its own sibling files.
SCRIPT_DIR=""
case "$0" in
    *.sh)
        if [ -f "$0" ]; then
            SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
        fi
        ;;
esac
# Bootstrap when piped or downloaded alone. Never source code from the download
# channel until its archive is bound to the signed control-plane manifest.
if [ -z "${SCRIPT_DIR}" ] || [ ! -f "${SCRIPT_DIR}/release-verify.sh" ]; then
    (
        set -eu
        for tool in curl openssl base64 mktemp tar; do
            command -v "${tool}" >/dev/null 2>&1 || {
                echo "gotham-install: required tool '${tool}' is missing (install curl, openssl 3+, coreutils and tar)" >&2
                exit 1
            }
        done
        openssl pkeyutl -help 2>&1 | grep -q rawin || {
            echo "gotham-install: OpenSSL 3+ is required for Ed25519 verification" >&2
            exit 1
        }
        bootstrap="$(mktemp -d "${TMPDIR:-/tmp}/gotham-bootstrap.XXXXXX")"
        trap 'rm -rf "${bootstrap}"' EXIT
        trap 'exit 1' INT TERM HUP
        fail() { echo "gotham-install: $*" >&2; exit 1; }
        key="${GOTHAM_RELEASE_PUBLIC_KEY_B64}"
        if [ -n "${GOTHAM_INSTALL_TEST_PUBLIC_KEY:-}" ] && [ -n "${GOTHAM_INSTALL_ROOT:-}" ]; then
            case "${GOTHAM_INSTALL_ROOT}" in
                /*) ;;
                *) fail "GOTHAM_INSTALL_ROOT must be an absolute sandbox path" ;;
            esac
            [ -n "$(printf '%s' "${GOTHAM_INSTALL_ROOT}" | tr -d '/')" ] \
                || fail "GOTHAM_INSTALL_ROOT must be a non-/ sandbox path"
            key="${GOTHAM_INSTALL_TEST_PUBLIC_KEY}"
        fi
        case "${key}" in
            *"BEGIN PUBLIC KEY"*) printf '%s\n' "${key}" >"${bootstrap}/key.pub" ;;
            *)
                printf '%s' "${key}" | base64 -d >"${bootstrap}/key.raw"
                [ "$(wc -c <"${bootstrap}/key.raw" | tr -d ' ')" = 32 ] || fail "invalid public key"
                { printf '\060\052\060\005\006\003\053\145\160\003\041\000'; cat "${bootstrap}/key.raw"; } >"${bootstrap}/key.der"
                openssl pkey -pubin -inform DER -in "${bootstrap}/key.der" -out "${bootstrap}/key.pub"
                ;;
        esac
        case "$(uname -m)" in
            x86_64 | amd64) arch=amd64 ;;
            aarch64 | arm64) arch=arm64 ;;
            *) fail "unsupported architecture" ;;
        esac
        releases="${GOTHAM_RELEASES_URL:-https://github.com/${GOTHAM_REPO:-justindeelux/gotham}/releases}"
        version="${GOTHAM_VERSION:-}"
        if [ -z "${version}" ]; then
            [ -z "${GOTHAM_BASE_URL:-}" ] || fail "GOTHAM_VERSION is required with GOTHAM_BASE_URL"
            redirect="$(curl -fsS -o /dev/null -w '%{redirect_url}' "${releases%/}/latest")"
            version="${redirect%/}"; version="${version##*/}"
        fi
        printf '%s' "${version}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' || fail "invalid release version"
        base="${GOTHAM_BASE_URL:-${releases%/}/download/${version}}"
        manifest="gotham-manifest-${arch}.txt"
        curl -fsSL --retry 3 -o "${bootstrap}/manifest" "${base%/}/${manifest}"
        curl -fsSL --retry 3 -o "${bootstrap}/signature" "${base%/}/${manifest}.sig"
        base64 -d <"${bootstrap}/signature" >"${bootstrap}/signature.raw"
        openssl pkeyutl -verify -pubin -inkey "${bootstrap}/key.pub" -rawin \
            -in "${bootstrap}/manifest" -sigfile "${bootstrap}/signature.raw" >/dev/null 2>&1 \
            || fail "manifest signature verification FAILED (refusing to install)"
        field() { sed -n "s/^$1=//p" "${bootstrap}/manifest"; }
        [ "$(field version)" = "${version}" ] && [ "$(field arch)" = "${arch}" ] \
            && [ "$(field file)" = "gotham-linux-${arch}" ] || fail "manifest release binding mismatch"
        bundle="gotham-deploy-${version}.tar.gz"
        [ "$(field deploy_file)" = "${bundle}" ] || fail "manifest has no matching deploy bundle"
        digest="$(field deploy_sha256)"
        printf '%s' "${digest}" | grep -Eq '^[0-9a-f]{64}$' || fail "invalid deploy digest"
        curl -fsSL --retry 3 -o "${bootstrap}/bundle.tar.gz" "${base%/}/${bundle}"
        if command -v sha256sum >/dev/null 2>&1; then
            got="$(sha256sum "${bootstrap}/bundle.tar.gz" | awk '{print $1}')"
        else
            got="$(shasum -a 256 "${bootstrap}/bundle.tar.gz" | awk '{print $1}')"
        fi
        [ "${got}" = "${digest}" ] || fail "deploy bundle digest mismatch"
        mkdir "${bootstrap}/deploy"
        tar -xzf "${bootstrap}/bundle.tar.gz" -C "${bootstrap}/deploy"
        # The verified bundle contains the installer too, so sibling lookups
        # and the existing cleanup/test seams keep their original behavior.
        set --
        [ "${DRY_RUN}" -eq 0 ] || set -- "$@" --dry-run
        [ "${DOMAIN_GIVEN}" -eq 0 ] || set -- "$@" --domain "${DOMAIN}"
        for cp_host in ${CP_HOSTS_OPT}; do set -- "$@" --cp-host "${cp_host}"; done
        GOTHAM_VERSION="${version}" sh "${bootstrap}/deploy/install.sh" "$@" </dev/null
    )
    exit $?
fi
for sibling in release-verify.sh install-agent-lib.sh gotham-update.sh gotham-updater.conf install-sudoers.sh gotham.service gotham-hostbootstrap.sh gotham-hostbootstrap-fetch.sh gotham-hostctl.sh gotham-toolctl.sh gotham-signing-key.pub install-agent.sh gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service install-toolctl-sudoers.sh; do
    [ -f "${SCRIPT_DIR}/${sibling}" ] || {
        echo "gotham-install: missing deploy file ${sibling}; download a fresh standalone install.sh" >&2
        exit 2
    }
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

# Keep xtrace (sh -x) from printing secrets: every command line the shell
# traces is expanded first, so assignments, tests and the stdin printf would
# all leak a password. Regions are never nested; each off has its on. Defined
# before the input validation so the stored DSN is read with xtrace off too.
_admin_xtrace_off() {
    case $- in
        *x*) _ADMIN_XTRACE_WAS_ON=1; set +x ;;
        *) _ADMIN_XTRACE_WAS_ON=0 ;;
    esac
}
_admin_xtrace_on() {
    if [ "${_ADMIN_XTRACE_WAS_ON:-0}" = "1" ]; then
        set -x
    fi
    _ADMIN_XTRACE_WAS_ON=0
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

# Both early validation and the agent step use these mandatory values.
derive_agent_defaults() {
    _derived_node_id="localhost"
    _derived_cp_addr="127.0.0.1:9442"
}

# Install prefix. Empty = the production paths assigned below. A
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
# Validate the control plane and mandatory local agent before changing files.
# Unsupported platforms and remote agent configurations fail without mutation.
# A later agent install failure leaves the already started control plane intact.
DEFAULT_DSN="postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
DEFAULT_REDIS="localhost:6379"

# Values a previous install already wrote, so a repair/upgrade keeps a managed
# DSN and operator edits instead of falling back to local defaults. A DSN or
# Redis address given on the command line still wins. The whole block reads
# and splits the stored DSN (and therefore its password), so xtrace is off.
_admin_xtrace_off
ENV_PREV=""
if [ -f "${ENV_FILE}" ]; then
    ENV_PREV="$(cat "${ENV_FILE}")" \
        || die "cannot read ${ENV_FILE}; fix its permissions or set GOTHAM_DATABASE_DSN"
fi
env_prev() {
    printf '%s\n' "${ENV_PREV}" | sed -n "s/^$1=//p" | head -n1
}
# LOCAL_DSN means the local PostgreSQL/Redis services are this installer's to
# install and keep running. FRESH_LOCAL_DB means no DSN exists yet, so the
# role and database are created with a new random password. A stored DSN is
# never rewritten and its existing role is never touched.
LOCAL_DSN=0
FRESH_LOCAL_DB=0
DB_PASSWORD=""
if [ -n "${GOTHAM_DATABASE_DSN:-}" ]; then
    # An explicit DSN is managed by the operator: no local provisioning.
    DSN="${GOTHAM_DATABASE_DSN}"
else
    DSN="$(env_prev GOTHAM_DATABASE_DSN)"
    if [ -z "${DSN}" ]; then
        LOCAL_DSN=1
        FRESH_LOCAL_DB=1
    else
        case "${DSN}" in
            postgres://gotham:*@localhost:5432/*\?sslmode=disable) LOCAL_DSN=1 ;;
        esac
    fi
fi
# Install and configure the local services only when this installer owns them
# and the operator did not ask to skip dependencies. The dependency test seam
# runs in a GOTHAM_INSTALL_ROOT sandbox only.
PROVISION_LOCAL_DEPS=0
if [ "${LOCAL_DSN}" -eq 1 ] && [ "${GOTHAM_SKIP_DEPS:-0}" != "1" ] && [ "${DRY_RUN}" -eq 0 ]; then
    if [ "${TEST_MODE}" -eq 0 ] || [ "${GOTHAM_INSTALL_TEST_DEPS:-0}" = "1" ]; then
        PROVISION_LOCAL_DEPS=1
    fi
fi
# With provisioning skipped, an install that never wrote a DSN keeps the
# documented default so an existing local database keeps working.
if [ "${LOCAL_DSN}" -eq 1 ] && [ "${PROVISION_LOCAL_DEPS}" -eq 0 ] && [ -z "${DSN}" ]; then
    DSN="${DEFAULT_DSN}"
fi
# The dependency test seam must never reach the real /etc/apt: production
# leaves _DEPS_APT_ROOT empty, and only the test suite bakes a scratch root.
if [ "${PROVISION_LOCAL_DEPS}" -eq 1 ] && [ "${TEST_MODE}" -eq 1 ] && [ -z "${_DEPS_APT_ROOT}" ]; then
    die "GOTHAM_INSTALL_TEST_DEPS requires a baked test apt root; refusing to touch the real /etc/apt"
fi
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
# A trailing backslash would join the gotham.env line with the next one under
# systemd's EnvironmentFile continuation, swallowing a key; quotes are refused
# because the service would see a different value than the file holds. URL
# metacharacters a real DSN needs (@, :, %-escapes, ?query, rediss://) pass
# through untouched (pinned by the C2c cases in test-release-install.sh).
case "${DSN}" in
    *\\) die "GOTHAM_DATABASE_DSN must not end with a backslash (systemd would join it with the next line)" ;;
    *\'* | *\"*) die "GOTHAM_DATABASE_DSN must not contain quotes (got '${DSN}'); keyword DSNs with quoted values (password='sec ret') must use the URL form postgres://user:password@host/db?sslmode=..." ;;
esac
case "${REDIS_ADDR}" in
    *\\) die "GOTHAM_REDIS_ADDR must not end with a backslash (systemd would join it with the next line)" ;;
    *\'* | *\"*) die "GOTHAM_REDIS_ADDR must not contain quotes (got '${REDIS_ADDR}')" ;;
esac
_admin_xtrace_on

{
    # Platform and prior-agent lookups only read files. _la_agent_env is the
    # path install-agent.sh writes; the GOTHAM_AGENT_ENV_FILE / GOTHAM_OS_RELEASE_FILE /
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
        # still wins and repoints deliberately), refuse before mutation.
        _la_prior_cp_addr="$(sed -n 's/^[[:space:]]*GOTHAM_AGENT_CP_ADDR=//p' "${_la_agent_env}" | tail -n1)"
        if [ -n "${_la_prior_cp_addr}" ] && ! _is_loopback_cp_addr "${_la_prior_cp_addr}"; then
            LOCAL_AGENT_SKIP="agent.env points at a remote control plane (${_la_prior_cp_addr}); leaving it untouched"
        fi
    fi
    [ -z "${LOCAL_AGENT_SKIP}" ] || die "mandatory localhost agent: ${LOCAL_AGENT_SKIP}"
    [ -z "${GOTHAM_AGENT_NODE_ID:-}" ] || [ "${GOTHAM_AGENT_NODE_ID}" = "localhost" ] \
        || die "GOTHAM_AGENT_NODE_ID must be localhost for the control-plane installer"
    [ -z "${GOTHAM_AGENT_CP_ADDR:-}" ] || [ "${GOTHAM_AGENT_CP_ADDR}" = "127.0.0.1:9442" ] \
        || die "GOTHAM_AGENT_CP_ADDR must be 127.0.0.1:9442 for the control-plane installer"
    # Override the prior local identity before validating the effective config.
    LEGACY_LOCAL_NODE_ID=""
    if [ -f "${_la_agent_env}" ]; then
        LEGACY_LOCAL_NODE_ID="$(sed -n 's/^[[:space:]]*GOTHAM_AGENT_NODE_ID=//p' "${_la_agent_env}" | tail -n1)"
    fi
    GOTHAM_AGENT_NODE_ID=localhost
    GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442
    export GOTHAM_AGENT_NODE_ID GOTHAM_AGENT_CP_ADDR
    if [ -z "${LOCAL_AGENT_SKIP}" ]; then
        agent_env_validate "${_la_agent_env}" "${CA_DIR}/ca.crt" 0 \
            || die "invalid agent configuration (see above); refusing to install"
        # Validate the same mandatory defaults the agent step will use.
        derive_agent_defaults
        [ -z "${_derived_node_id}" ] \
            || _env_check_value GOTHAM_AGENT_NODE_ID "${_derived_node_id}" \
                || die "invalid derived agent node id (see above); refusing to install"
        [ -z "${_derived_cp_addr}" ] \
            || _env_check_value GOTHAM_AGENT_CP_ADDR "${_derived_cp_addr}" \
                || die "invalid derived agent address (see above); refusing to install"
    fi
}

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
# A FRESH install creates the role and database before gotham.env is written.
# If it aborts in that window, remove only the objects this run created and
# only while the env is not installed yet; a stored-DSN install never reaches
# this (FRESH_LOCAL_DB is 0) and pre-existing objects are never dropped.
_drop_fresh_db_objects() {
    [ "${FRESH_LOCAL_DB:-0}" -eq 1 ] || return 0
    # The temp file is the "env is not live yet" marker: mv consumes it, so a
    # signal after a successful mv leaves ENV_TMP set but nothing to drop.
    [ -n "${ENV_TMP:-}" ] && [ -f "${ENV_TMP}" ] || return 0
    [ "${DB_ROLE_CREATED:-0}" -eq 1 ] || [ "${DB_DATABASE_CREATED:-0}" -eq 1 ] || return 0
    command -v runuser >/dev/null 2>&1 && command -v psql >/dev/null 2>&1 || return 1
    log "removing the partially created gotham role/database from this failed install"
    if [ "${DB_DATABASE_CREATED:-0}" -eq 1 ]; then
        runuser -u postgres -- psql -v ON_ERROR_STOP=0 -q \
            -c 'DROP DATABASE IF EXISTS gotham' >/dev/null 2>&1 || true
    fi
    if [ "${DB_ROLE_CREATED:-0}" -eq 1 ]; then
        runuser -u postgres -- psql -v ON_ERROR_STOP=0 -q \
            -c 'DROP ROLE IF EXISTS gotham' >/dev/null 2>&1 || true
    fi
    return 0
}

# One EXIT path for every abort: drop objects from an interrupted fresh install,
# then remove the private scratch material (the prepared gotham.env temp holds
# the secret key and the database password). INT/TERM/HUP/QUIT abort (exit 1)
# and therefore pass through it too.
_install_cleanup() {
    _drop_fresh_db_objects || true
    rm -rf "${WORK_DIR}"
    [ -z "${ENV_TMP:-}" ] || rm -f "${ENV_TMP}"
    [ -z "${_DEPS_KEY_DIR:-}" ] || rm -rf "${_DEPS_KEY_DIR}"
}
trap '_install_cleanup' EXIT
trap 'exit 1' INT TERM HUP QUIT
materialize_public_key "${PUBKEY_SOURCE}" "${PUBKEY_FILE}"

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] write ${PUBKEY_FILE}"
    echo "[dry-run] verify_release ${RELEASE_BASE} ${VERSION} ${ARCH} ${FAMILY} <pubkey> ${INSTALL_PATH}"
else
    verify_release "${RELEASE_BASE}" "${VERSION}" "${ARCH}" "${FAMILY}" "${PUBKEY_FILE}" "${TMP_BINARY}"
fi

# ---- First admin account ----------------------------------------------------
# Credentials are collected in phase 1 (before the first mutation, while the
# service cannot serve registration yet) and the account is created in phase
# 2 (right after the migrations, before `systemctl start`). The password only
# ever travels on stdin (never argv, environment, disk or logs): interactive
# prompts read from the tty, the non-interactive password file is piped, and
# a generated password is printed once in the final summary below.
# `sh -x` tracing is disabled around the secret handling.
#
# Test seams (honoured ONLY in test mode, ignored in production):
#   GOTHAM_INSTALL_TEST_ADMIN_BIN=<fake>  binary used for `admin exists` /
#                               `admin create` instead of the installed one.
#                               Without it test mode skips this step.
#   GOTHAM_INSTALL_TEST_ADMIN_TTY=<path>   interactive prompts use this file
#                               instead of /dev/tty, so the suite can drive
#                               them without a pty. /dev/tty is never opened
#                               in test mode unless this seam is set.
_admin_tty_path() {
    if [ "${TEST_MODE}" -eq 1 ] && [ -n "${GOTHAM_INSTALL_TEST_ADMIN_TTY:-}" ]; then
        printf '%s' "${GOTHAM_INSTALL_TEST_ADMIN_TTY}"
    else
        printf '/dev/tty'
    fi
}

# Interactive when a terminal is really usable: a tty stream alone is not
# enough (setsid or a CI pty gives tty fds without a controlling terminal),
# so /dev/tty must also open for reading and writing. Otherwise the
# non-interactive branch runs and nothing blocks or dies silently.
# In test mode only the TTY seam counts, so sandbox runs never touch /dev/tty
# unasked (the seam path itself is what gets probed there).
_admin_interactive() {
    if [ "${TEST_MODE}" -eq 1 ]; then
        [ -n "${GOTHAM_INSTALL_TEST_ADMIN_TTY:-}" ] || return 1
    else
        { [ -t 0 ] || [ -t 1 ] || [ -t 2 ]; } || return 1
    fi
    _ai_tty="$(_admin_tty_path)"
    ( : <"${_ai_tty}" ) 2>/dev/null || return 1
    ( : >>"${_ai_tty}" ) 2>/dev/null || return 1
    return 0
}

# Basic email shape check (the binary revalidates strictly): one @, a dot
# after it, no spaces or control characters.
_admin_valid_email() {
    _ave_stripped="$(printf '%s' "$1" | tr -d '\000-\037\177')"
    [ "${_ave_stripped}" = "$1" ] || return 1
    case "$1" in
        *" "* | *@*@* | @* | *@) return 1 ;;
    esac
    case "$1" in
        ?*@?*.?*) return 0 ;;
        *) return 1 ;;
    esac
}

# Mirrors the shared password policy (internal/auth/service.go:
# minPasswordLength/maxPasswordLength): length bounds only, so the shell can
# re-prompt before the binary ever sees a hopeless password.
_admin_pw_min_len=8
_admin_pw_max_len=128
_admin_pw_policy_ok() {
    [ "${#1}" -ge "${_admin_pw_min_len}" ] && [ "${#1}" -le "${_admin_pw_max_len}" ]
}

# Run the admin subcommand as the service user in production (the binary
# reads the root-owned env file through the gotham group), directly in test
# mode like `ca init` above.
_admin_run() {
    if [ "${TEST_MODE}" -eq 1 ]; then
        "${_ADMIN_BIN}" "$@"
    else
        runuser -u "${SERVICE_USER}" -- "${_ADMIN_BIN}" "$@"
    fi
}

# Restore the tty echo after a hidden prompt.
_admin_restore_tty() {
    if [ -c "${_ADMIN_TTY_ACTIVE:-/nonexistent}" ]; then
        stty echo <"${_ADMIN_TTY_ACTIVE}" 2>/dev/null || true
    fi
}

# Uniform prompt traps for a prompt section: EXIT/INT/TERM/HUP/QUIT all
# restore echo first, then chain the installer's own handlers (the WORK_DIR
# cleanup on EXIT, `exit 1` on INT/TERM; HUP/QUIT had none). Restated
# explicitly because POSIX cannot introspect traps without parsing (see
# release-verify.sh for the parsing variant).
_admin_prompt_traps() {
    trap '_admin_restore_tty; rm -rf "${WORK_DIR}"' EXIT
    trap '_admin_restore_tty; exit 1' INT TERM HUP QUIT
}

# Put back exactly what _admin_prompt_traps replaced.
_admin_restore_prompt_traps() {
    trap '_install_cleanup' EXIT
    trap 'exit 1' INT TERM HUP QUIT
}

# Hidden single-line prompt on the tty into _ADMIN_SECRET. Empty input is a
# valid answer (the caller treats it as "generate"). Echo is disabled before
# the prompt prints, so bytes arriving from that point on — including
# type-ahead sent the moment the prompt appears — are never echoed. (Bytes
# typed long before, while echo was still on, were already echoed by the
# terminal itself.) Prompt-section traps (above) cover signals and set -e
# aborts, so echo is always restored.
_admin_prompt_secret() {
    _ADMIN_TTY_ACTIVE="$2"
    if [ -c "${_ADMIN_TTY_ACTIVE}" ]; then
        stty -echo <"${_ADMIN_TTY_ACTIVE}" 2>/dev/null || true
    fi
    printf '%s' "$1" >"${_ADMIN_TTY_ACTIVE}"
    _ADMIN_SECRET=""
    IFS= read -r _ADMIN_SECRET <"${_ADMIN_TTY_ACTIVE}" || true
    _admin_restore_tty
    printf '\n' >"${_ADMIN_TTY_ACTIVE}"
}

# Forget every in-memory secret. Called the moment the binary has the
# password (success or failure) and at the end of collection.
_admin_wipe() {
    _admin_pw=""
    _admin_pw1=""
    _ADMIN_SECRET=""
    _admin_generated=""
}

# Warn (not fail) when the password file is readable beyond its owner.
_admin_warn_pwfile_perms() {
    _awp_mode=""
    if _awp_out="$(stat -c '%a' "$1" 2>/dev/null)"; then
        _awp_mode="${_awp_out}"
    elif _awp_out="$(stat -f '%Lp' "$1" 2>/dev/null)"; then
        _awp_mode="${_awp_out}"
    fi
    [ -n "${_awp_mode}" ] || return 0
    if [ $((_awp_mode / 10 % 10 & 4)) -ne 0 ] || [ $((_awp_mode % 10 & 4)) -ne 0 ]; then
        log "WARNING: $1 is readable by group/others; restrict it to root (chmod 600)"
    fi
}

# Generate a 32-character base64url password (24 random bytes). openssl is
# already a hard requirement above.
_admin_generate_password() {
    openssl rand -base64 24 2>/dev/null | tr -d '\n=' | tr '+/' '-_' | tr -d '\n'
}

# Read the password file into _admin_pw (single line). Dies on unreadable or
# empty files; warns on group/world readability. Call with xtrace off.
_admin_read_pwfile() {
    [ -r "$1" ] \
        || die "GOTHAM_ADMIN_PASSWORD_FILE is not readable (got '$1')"
    _admin_warn_pwfile_perms "$1"
    _admin_pw=""
    IFS= read -r _admin_pw <"$1" || true
    _admin_pw="$(printf '%s' "${_admin_pw}" | tr -d '\r\n')"
    [ -n "${_admin_pw}" ] \
        || die "GOTHAM_ADMIN_PASSWORD_FILE is empty (remove it to generate a password)"
}

# Prompt for the password with confirmation into _admin_pw (hidden input on
# $1; empty input generates into _admin_generated and uses it). Rejects
# mismatches and policy violations with a re-prompt. Call with xtrace off
# and prompt traps installed.
_admin_prompt_password() {
    while true; do
        _admin_prompt_secret 'Admin password (empty to generate): ' "$1"
        _admin_pw1="${_ADMIN_SECRET}"
        _admin_prompt_secret 'Confirm password: ' "$1"
        if [ "${_admin_pw1}" != "${_ADMIN_SECRET}" ]; then
            printf 'Passwords do not match, try again.\n' >"$1"
            continue
        fi
        _admin_pw="${_admin_pw1}"
        if [ -z "${_admin_pw}" ]; then
            _admin_generated="$(_admin_generate_password)" || _admin_generated=""
            [ -n "${_admin_generated}" ] || die "could not generate an admin password"
            _admin_pw="${_admin_generated}"
            break
        fi
        if _admin_pw_policy_ok "${_admin_pw}"; then
            break
        fi
        printf 'Password must be between 8 and 128 characters, try again.\n' >"$1"
    done
}

# Collection (phase 1) runs before the first host mutation, while the service
# cannot serve registration yet: credentials are gathered up front, and
# creation happens in phase 2 right after the migrations and before
# `systemctl start`, so nobody can claim the first account through the open
# web form while the operator ponders the prompt. Both phases always return
# 0; the outcome is in ADMIN_STATUS (runtime failures set ADMIN_FAILED and
# exit nonzero at the end, like the localhost agent step). Operator config
# errors (bad email, bad password file) die here, before anything is created.
admin_phase1_collect() {
    ADMIN_STATUS=""
    ADMIN_EMAIL=""
    ADMIN_GENERATED=""
    ADMIN_FAILED=0
    ADMIN_FAIL_REASON=""
    ADMIN_HAVE_CREDS=0
    _ADMIN_BIN="${INSTALL_PATH}"
    if [ "${TEST_MODE}" -eq 1 ]; then
        if [ -z "${GOTHAM_INSTALL_TEST_ADMIN_BIN:-}" ]; then
            log "test mode: skipping admin creation (no GOTHAM_INSTALL_TEST_ADMIN_BIN seam)"
            ADMIN_STATUS="test-skip"
            return 0
        fi
        _ADMIN_BIN="${GOTHAM_INSTALL_TEST_ADMIN_BIN}"
    fi
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] ${_ADMIN_BIN} admin create --email <tty prompt or \$GOTHAM_ADMIN_EMAIL> (password via stdin only)"
        ADMIN_STATUS="dry-run"
        return 0
    fi

    # Re-run fast path: a previously installed binary plus an existing
    # account means no prompt and no collection. A missing binary (fresh
    # install) or an unreachable database falls through to collection;
    # phase 2 checks again once the database is migrated.
    if [ -x "${_ADMIN_BIN}" ]; then
        if _admin_exists_out="$(_admin_run admin exists 2>/dev/null)"; then
            case "${_admin_exists_out}" in
                *"admin-exists: true"*)
                    log "admin already exists, skipping"
                    ADMIN_STATUS="exists"
                    return 0
                    ;;
            esac
        fi
    fi

    _admin_email=""
    _admin_pw=""
    _admin_generated=""
    if _admin_interactive; then
        _admin_tty="$(_admin_tty_path)"
        _admin_email_default="${GOTHAM_ADMIN_EMAIL:-admin@example.com}"
        _admin_prompt_traps
        while true; do
            if [ -n "${_admin_email_default}" ]; then
                printf 'Admin email [%s] (empty keeps it, - to skip): ' "${_admin_email_default}" >"${_admin_tty}"
            else
                printf 'Admin email (empty to skip): ' >"${_admin_tty}"
            fi
            IFS= read -r _admin_email <"${_admin_tty}" || _admin_email=""
            # The seam file carries scripted input without newlines trimmed
            # by a terminal; strip a possible carriage return.
            _admin_email="$(printf '%s' "${_admin_email}" | tr -d '\r')"
            if [ -z "${_admin_email}" ]; then
                if [ -n "${_admin_email_default}" ]; then
                    _admin_email="${_admin_email_default}"
                    break
                fi
                break
            fi
            if [ "${_admin_email}" = "-" ] && [ -n "${_admin_email_default}" ]; then
                _admin_email=""
                break
            fi
            if _admin_valid_email "${_admin_email}"; then
                break
            fi
            printf 'Invalid email address, try again.\n' >"${_admin_tty}"
        done
        if [ -z "${_admin_email}" ]; then
            _admin_restore_prompt_traps
            ADMIN_STATUS="no-email"
            return 0
        fi
        _admin_xtrace_off
        if [ -n "${GOTHAM_ADMIN_PASSWORD_FILE:-}" ]; then
            _admin_read_pwfile "${GOTHAM_ADMIN_PASSWORD_FILE}"
        else
            _admin_prompt_password "${_admin_tty}"
        fi
        ADMIN_GENERATED="${_admin_generated}"
        _admin_restore_prompt_traps
        ADMIN_EMAIL="${_admin_email}"
        ADMIN_HAVE_CREDS=1
        _admin_pw1=""
        _ADMIN_SECRET=""
        _admin_xtrace_on
        return 0
    else
        _admin_email="${GOTHAM_ADMIN_EMAIL:-}"
        if [ -z "${_admin_email}" ]; then
            ADMIN_STATUS="no-email"
            return 0
        fi
        _admin_valid_email "${_admin_email}" \
            || die "GOTHAM_ADMIN_EMAIL is not a valid email address (got '${_admin_email}')"
        _admin_xtrace_off
        if [ -n "${GOTHAM_ADMIN_PASSWORD_FILE:-}" ]; then
            _admin_read_pwfile "${GOTHAM_ADMIN_PASSWORD_FILE}"
        else
            _admin_generated="$(_admin_generate_password)" || _admin_generated=""
            [ -n "${_admin_generated}" ] || die "could not generate an admin password"
            _admin_pw="${_admin_generated}"
        fi
        ADMIN_GENERATED="${_admin_generated}"
        ADMIN_EMAIL="${_admin_email}"
        ADMIN_HAVE_CREDS=1
        _admin_pw1=""
        _ADMIN_SECRET=""
        _admin_xtrace_on
        return 0
    fi
}

# Creation (phase 2) runs right after the migrations and before the service
# starts, so the first account exists before registration opens. It never
# prompts except for the bounded interactive retry below. The binary carries
# its own atomic first-account guard, so a concurrent registration cannot
# yield a second admin; a failure followed by an existing account is reported
# as a likely takeover ("created by someone else").
admin_phase2_create() {
    case "${ADMIN_STATUS}" in
        test-skip | dry-run | exists | no-email) return 0 ;;
    esac
    if [ "${ADMIN_HAVE_CREDS}" != "1" ]; then
        ADMIN_STATUS="no-email"
        return 0
    fi

    _admin_exists_out=""
    _admin_exists_status=0
    _admin_exists_out="$(_admin_run admin exists 2>"${WORK_DIR}/admin-exists.err")" \
        || _admin_exists_status=$?
    if [ "${_admin_exists_status}" -ne 0 ]; then
        # Releases this installer postdates fail the probe with usage exit
        # status 2 and a single exact diagnostic line, not with a database
        # error: v0.1.0 has no `admin` command at all (so it cannot run the
        # manual command either: point at the upgrade), v0.2.0 has
        # `admin create` (hidden prompt) but no `admin exists` (print the
        # manual command). Both the status and the complete line must match:
        # a genuine database error merely containing the phrase stays on the
        # failure path below.
        if [ "${_admin_exists_status}" -eq 2 ] \
            && grep -qx 'unknown command "admin"' "${WORK_DIR}/admin-exists.err" 2>/dev/null; then
            cat "${WORK_DIR}/admin-exists.err" >&2 || true
            log "the installed release has no admin commands; see the upgrade note below"
            ADMIN_STATUS="legacy-upgrade"
            _admin_wipe
            return 0
        fi
        if [ "${_admin_exists_status}" -eq 2 ] \
            && grep -qx 'unknown admin command "exists"' "${WORK_DIR}/admin-exists.err" 2>/dev/null; then
            cat "${WORK_DIR}/admin-exists.err" >&2 || true
            log "the installed release predates automated admin bootstrap; skipping (see the manual command below)"
            ADMIN_STATUS="legacy-manual"
            _admin_wipe
            return 0
        fi
        cat "${WORK_DIR}/admin-exists.err" >&2 || true
        ADMIN_FAIL_REASON="could not check for an existing admin account (is the database up?)"
        ADMIN_FAILED=1
        ADMIN_STATUS="failed"
        _admin_wipe
        return 0
    fi
    case "${_admin_exists_out}" in
        *"admin-exists: true"*)
            log "admin already exists, skipping"
            ADMIN_STATUS="exists"
            _admin_wipe
            return 0
            ;;
        *"admin-exists: false"*) ;;
        *)
            ADMIN_FAIL_REASON="unexpected output from '${_ADMIN_BIN} admin exists': ${_admin_exists_out}"
            ADMIN_FAILED=1
            ADMIN_STATUS="failed"
            _admin_wipe
            return 0
            ;;
    esac

    # printf is a shell builtin, so the password never appears in a process
    # list; the pipe keeps it off the command line, out of the environment
    # and off disk. It is wiped the moment the binary has it.
    _admin_xtrace_off
    _admin_attempts=0
    _admin_created=0
    while [ "${_admin_attempts}" -lt 3 ]; do
        _admin_attempts=$((_admin_attempts + 1))
        if printf '%s\n' "${_admin_pw}" | _admin_run admin create --email "${_admin_email}" --password-stdin 2>"${WORK_DIR}/admin-create.err"; then
            _admin_created=1
            break
        fi
        _admin_wipe
        if _admin_interactive && [ "${_admin_attempts}" -lt 3 ]; then
            _admin_tty="$(_admin_tty_path)"
            _admin_prompt_traps
            printf 'Could not create the account (%s).\n' "$(cat "${WORK_DIR}/admin-create.err")" >"${_admin_tty}"
            printf 'Check the email and try another password.\n' >"${_admin_tty}"
            _admin_prompt_password "${_admin_tty}"
            ADMIN_GENERATED="${_admin_generated}"
            _admin_restore_prompt_traps
            continue
        fi
        break
    done
    _admin_wipe
    _admin_xtrace_on
    if [ "${_admin_created}" = "1" ]; then
        ADMIN_STATUS="created"
        return 0
    fi
    if _admin_exists_again="$(_admin_run admin exists 2>/dev/null)"; then
        case "${_admin_exists_again}" in
            *"admin-exists: true"*)
                ADMIN_FAIL_REASON="an account already exists (created by someone else?): check the instance"
                ;;
            *)
                ADMIN_FAIL_REASON="could not create the admin account for '${_admin_email}'"
                ;;
        esac
    else
        ADMIN_FAIL_REASON="could not create the admin account for '${_admin_email}'"
    fi
    ADMIN_FAILED=1
    ADMIN_STATUS="failed"
    return 0
}
# ---- First admin account: collection (phase 1) -----------------------------
# After every pre-check and the verified download, before the first mutation
# (and while the service cannot serve registration yet): prompt for (or read)
# the first-admin credentials, so creation in phase 2 runs before
# `systemctl start`. No prompts on --dry-run, in test mode without the BIN
# seam, or when the instance already has an account.
admin_phase1_collect
if [ "${DOMAIN_GIVEN}" -eq 0 ] && [ "${DRY_RUN}" -eq 0 ] && _admin_interactive; then
    _domain_tty="$(_admin_tty_path)"
    printf 'Panel domain (optional, Enter skips): ' >"${_domain_tty}"
    IFS= read -r DOMAIN <"${_domain_tty}" || DOMAIN=""
fi

# Accept pasted URLs as well as bare hostnames.
DOMAIN="$(printf '%s' "${DOMAIN}" | tr '[:upper:]' '[:lower:]' | sed \
    -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    -e 's#^http://##' -e 's#^https://##' -e 's#/*$##')"

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
# A fresh local install gets a random database password here (not inside the
# env-write subshell) so the dependency step below uses the same value. It is
# written only to gotham.env and travels to psql on stdin; xtrace is off around
# generation and the env write so `sh -x` never logs it. A stored DSN is never
# regenerated or rewritten.
_admin_xtrace_off
if [ "${FRESH_LOCAL_DB}" -eq 1 ] && [ "${PROVISION_LOCAL_DEPS}" -eq 1 ] && [ -z "${DB_PASSWORD}" ]; then
    DB_PASSWORD="$(_admin_generate_password)" || DB_PASSWORD=""
    case "${DB_PASSWORD}" in
        '' | *[!A-Za-z0-9_-]*) die "could not generate a database password" ;;
    esac
    DSN="postgres://gotham:${DB_PASSWORD}@localhost:5432/gotham?sslmode=disable"
fi
_admin_xtrace_on

# The legacy fixed password is never changed automatically, but the owner must
# know. One warning here and the same text in the final summary.
LEGACY_DB_PASSWORD_NOTE=""
case "${DSN}" in
    postgres://gotham:gotham@localhost:5432/*)
        LEGACY_DB_PASSWORD_NOTE="  Database: WARNING: ${ENV_FILE} uses the old default password.
    Change it manually (use only letters, digits, _ and -, with no quote):
  sudo -u postgres psql -v ON_ERROR_STOP=1 <<'SQL'
ALTER ROLE gotham WITH LOGIN PASSWORD '<new-strong-password>';
SQL
    Then set GOTHAM_DATABASE_DSN in ${ENV_FILE} to the same password and
    run: sudo systemctl restart gotham
"
        log "WARNING: ${ENV_FILE} uses the old default database password; it was left unchanged."
        log "  Change it with (use only letters, digits, _ and -, with no quote):
  sudo -u postgres psql -v ON_ERROR_STOP=1 <<'SQL'
ALTER ROLE gotham WITH LOGIN PASSWORD '<new-strong-password>';
SQL
  then set GOTHAM_DATABASE_DSN in ${ENV_FILE} to the same password and
  run: sudo systemctl restart gotham"
        ;;
esac

log "writing ${ENV_FILE}"
run mkdir -p "${ETC_DIR}"
run chmod 0755 "${ETC_DIR}"
JWT_KEY="${ETC_DIR}/jwt_ed25519.key"
JWT_PUB="${ETC_DIR}/jwt_ed25519.pub"

# Render the new environment into a root-only temp file now, but do not move it
# over gotham.env yet: a fresh install creates the role and database first.
# install_env_file below performs the atomic rename after the dependency step
# (which never touches a stored DSN). A failure in between leaves the old file
# in place, and the cleanup trap removes a partially created fresh role/database.
ENV_TMP=""
_admin_xtrace_off
if [ "${DRY_RUN}" -eq 0 ]; then
    # A previous kill can leave a root-only env temp holding secrets.
    rm -f "${ENV_FILE}".tmp.*
    ENV_TMP="${ENV_FILE}.tmp.$$"
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
        # The temp copy is root-only and holds the secret key. It survives this
        # subshell on purpose: install_env_file moves it into place after the
        # database role has the new password. The caller removes it on abort,
        # and INT/TERM abort rather than let a partial file be installed.
        trap 'exit 1' INT TERM
        {
            echo "# Gotham control-plane environment. Read by gotham.service (EnvironmentFile)."
            echo "GOTHAM_DATABASE_DSN=${DSN}"
            echo "GOTHAM_REDIS_ADDR=${REDIS_ADDR}"
            echo "GOTHAM_CA_DIR=${CA_DIR}"
            echo "GOTHAM_SECRET_KEY=${SECRET_KEY}"
            echo "GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH=${JWT_KEY}"
            echo "GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH=${JWT_PUB}"
        } >"${ENV_TMP}"
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
                printf '%s\n' "${preserved}" >>"${ENV_TMP}" \
                    || die "could not preserve operator settings in ${ENV_FILE}"
            fi
        fi
        chmod 0640 "${ENV_TMP}"
    ) || {
        rm -f "${ENV_TMP}"
        ENV_TMP=""
        die "could not prepare the new ${ENV_FILE}; the existing file was kept"
    }
else
    echo "[dry-run] write ${ENV_FILE}, ${JWT_KEY}, ${JWT_PUB}"
fi
_admin_xtrace_on

# install_env_file atomically replaces gotham.env with the prepared temp file
# and fixes permissions. Returning nonzero lets the caller abort with the old
# file still in place (the temp is removed here).
install_env_file() {
    [ "${DRY_RUN}" -eq 1 ] && return 0
    [ -n "${ENV_TMP}" ] || return 0
    chmod 0640 "${JWT_KEY}" || return 1
    chmod 0644 "${JWT_PUB}" || return 1
    if [ "${TEST_MODE}" -eq 0 ]; then
        chown root:"${SERVICE_USER}" "${JWT_KEY}" || return 1
        chown root:root "${JWT_PUB}" || return 1
    fi
    mv -f "${ENV_TMP}" "${ENV_FILE}" || {
        # Leave the temp in place: it is the "env is not live" marker, so the
        # EXIT trap drops objects this fresh run created. The cleanup removes it.
        return 1
    }
    ENV_TMP=""
    if [ "${TEST_MODE}" -eq 0 ]; then
        chown root:"${SERVICE_USER}" "${ENV_FILE}" || return 1
    fi
    return 0
}

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

# ---- Self-update chain (same wrapper the node agent installs) ---------------
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

# Host helper (JUS-185): the fixed, root-owned gotham-hostctl + gotham-toolctl
# and the verb+id-pinned sudoers drop-in for the control-plane user, plus the
# root-owned payload the self-update wrapper re-applies on later updates. This
# is the trusted root install step, so no root process ever runs the
# service-owned binary. A failure is reported but does not roll back the
# control plane (the Instance page shows the honest "update the control plane"
# state).
log "installing the host helper"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] sh ${SCRIPT_DIR}/gotham-hostbootstrap.sh create ${SERVICE_USER} ${VERSION}"
elif [ "${TEST_MODE}" -eq 0 ]; then
    # Pass the release version so the helper's high-water mark is seeded from the
    # root-trusted installer: the first self-update cannot go below it.
    if ! sh "${SCRIPT_DIR}/gotham-hostbootstrap.sh" create "${SERVICE_USER}" "${VERSION}"; then
        log "WARNING: host helper install failed; retry with: sudo sh ${SCRIPT_DIR}/gotham-hostbootstrap.sh create ${SERVICE_USER} ${VERSION}"
    fi
fi

log "installing systemd unit ${SERVICE_FILE}"
render_file "${SCRIPT_DIR}/gotham.service" "${SERVICE_FILE}" 0644

# The dependency step below runs before the test-mode exit: it provisions the
# database role before gotham.env is replaced, and a test-mode run still needs
# the env file written before it stops.

# ---- Dependencies (PostgreSQL + Redis) --------------------------------------
# Compare two dotted numeric versions: 0 when $1 >= $2, 1 otherwise.
_version_at_least() {
    awk -v got="$1" -v min="$2" 'BEGIN {
        n = split(got, g, ".")
        m = split(min, r, ".")
        for (i = 1; i <= m; i++) {
            a = (i <= n && g[i] ~ /^[0-9]+$/) ? g[i] + 0 : 0
            b = r[i] + 0
            if (a > b) exit 0
            if (a < b) exit 1
        }
        exit 0
    }'
}

# Strip a Debian epoch and any suffix, then keep the first $2 components.
_version_cut() {
    printf '%s' "$1" | sed 's/^[0-9][0-9]*://' | sed 's/^[^0-9]*//' \
        | sed 's/[^0-9.].*$//' | awk -F. -v n="$2" '{
            out = $1
            for (i = 2; i <= n; i++) out = out "." $i
            print out
        }'
}

_deps_candidate() {
    apt-cache policy "$1" 2>/dev/null | sed -n 's/^  Candidate: //p' | head -n1
}

# _deps_apt_update refreshes the package lists once per install. It returns
# nonzero instead of dying so the caller can remove repository files it just
# added when the new source does not work. The flag is reset when a new
# repository is added.
_DEPS_APT_UPDATED=0
_deps_apt_update() {
    [ "${_DEPS_APT_UPDATED}" -eq 1 ] && return 0
    ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get update ) || return 1
    _DEPS_APT_UPDATED=1
}

_deps_apt_install() {
    ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" ) \
        || die "could not install: $*"
}

# _deps_distro_codename reads the codename both third-party repositories key
# their suites on.
_deps_distro_codename() {
    _dd_codename=""
    if [ -f "${_DEPS_OS_RELEASE}" ]; then
        _dd_codename="$(sed -n 's/^VERSION_CODENAME=//p' "${_DEPS_OS_RELEASE}" | head -n1 | tr -d '"')"
    fi
    if [ -z "${_dd_codename}" ]; then
        _dd_codename="$(lsb_release -cs 2>/dev/null || true)"
    fi
    [ -n "${_dd_codename}" ] \
        || die "could not determine the distribution codename from ${_DEPS_OS_RELEASE}"
}

# _deps_fetch_key downloads a third-party signing key over TLS and verifies its
# fingerprint (and that the file holds exactly one primary key) against the
# pinned value. It writes nothing outside the private scratch dir, so the caller
# can verify every key before adding any repository.
# args: key URL, pinned fingerprint, keyring/source base name, human-readable
# repository name. Sets _dk_file to the downloaded key.
_deps_fetch_key() {
    if [ -z "${_DEPS_KEY_DIR}" ]; then
        _DEPS_KEY_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gotham-apt-keys.XXXXXX")" \
            || die "could not create a temporary key directory"
        mkdir -m 0700 "${_DEPS_KEY_DIR}/gnupg"
    fi
    _dk_file="${_DEPS_KEY_DIR}/$3.asc"
    if ! curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 \
        "$1" -o "${_dk_file}"; then
        die "could not download the $4 signing key"
    fi
    _dk_fp="$(GNUPGHOME="${_DEPS_KEY_DIR}/gnupg" gpg --show-keys --with-colons "${_dk_file}" 2>/dev/null \
        | awk -F: '$1 == "fpr" { print $10; exit }' | tr -d '[:space:]')"
    _dk_keys="$(GNUPGHOME="${_DEPS_KEY_DIR}/gnupg" gpg --show-keys --with-colons "${_dk_file}" 2>/dev/null \
        | awk -F: '$1 == "pub" { n++ } END { print n + 0 }')"
    if [ "${_dk_fp}" != "$2" ] || [ "${_dk_keys}" != "1" ]; then
        die "$4 signing key fingerprint mismatch (got '${_dk_fp:-unreadable}', ${_dk_keys:-0} primary key(s)); refusing to use it"
    fi
}

# _deps_write_repo installs a verified key as a signed-by keyring plus the
# repository line. Only the key with the pinned fingerprint is exported into
# the keyring, so extra packets in the downloaded file cannot ride along. Files
# written here are recorded so a failed apt update can remove exactly what this
# run added. args: verified key file, pinned fingerprint, keyring/source base
# name, repo line tail, human-readable repository name.
_deps_write_repo() {
    _dr_keyrings="${_DEPS_APT_ROOT}/etc/apt/keyrings"
    _dr_sources="${_DEPS_APT_ROOT}/etc/apt/sources.list.d"
    mkdir -p "${_dr_keyrings}" "${_dr_sources}"
    chmod 0755 "${_dr_keyrings}" "${_dr_sources}"
    # apt requires a binary keyring ending in .gpg (a .asc signed-by file is
    # read as ASCII-armored and binary content in it is ignored). Export by
    # fingerprint from the scratch GNUPGHOME: only the verified key lands here.
    _dr_keyring="${_dr_keyrings}/$3.gpg"
    _dr_keyring_tmp="${_dr_keyring}.tmp.$$"
    if ! GNUPGHOME="${_DEPS_KEY_DIR}/gnupg" gpg --import "$1" >/dev/null 2>&1 \
        || ! GNUPGHOME="${_DEPS_KEY_DIR}/gnupg" gpg --export "$2" >"${_dr_keyring_tmp}" 2>/dev/null; then
        rm -f "${_dr_keyring_tmp}"
        die "could not import the $5 signing key"
    fi
    [ -s "${_dr_keyring_tmp}" ] || {
        rm -f "${_dr_keyring_tmp}"
        die "could not export the $5 signing key"
    }
    chmod 0644 "${_dr_keyring_tmp}"
    mv -f "${_dr_keyring_tmp}" "${_dr_keyring}" \
        || die "could not install the $5 signing key"
    _dr_source="${_dr_sources}/$3.list"
    if ! printf 'deb [signed-by=%s] %s\n' "${_dr_keyring}" "$4" >"${_dr_source}.tmp.$$"; then
        die "could not write the $5 repository line"
    fi
    chmod 0644 "${_dr_source}.tmp.$$"
    mv -f "${_dr_source}.tmp.$$" "${_dr_source}" \
        || die "could not install the $5 repository line"
    _DEPS_WRITTEN_FILES="${_DEPS_WRITTEN_FILES} ${_dr_keyring} ${_dr_source}"
    _DEPS_APT_UPDATED=0
}

# _deps_pin_redis keeps the 7.2 series after install. Without it a routine
# `apt upgrade` moves redis-server/redis-tools to 8.x (a new major with a
# different license). Priority 990 beats the repository's 500 without allowing
# a downgrade of an already-installed newer version. Idempotent: the file is
# rendered again on every run.
_deps_pin_redis() {
    _dp_dir="${_DEPS_APT_ROOT}/etc/apt/preferences.d"
    mkdir -p "${_dp_dir}"
    chmod 0755 "${_dp_dir}"
    _dp_file="${_dp_dir}/gotham-redis.pref"
    _dp_tmp="${_dp_file}.tmp.$$"
    if ! printf 'Package: redis-server redis-tools\nPin: version 6:%s.*\nPin-Priority: 990\n' \
        "${REDIS_PINNED_SERIES}" >"${_dp_tmp}"; then
        die "could not write the Redis version pin"
    fi
    chmod 0644 "${_dp_tmp}"
    mv -f "${_dp_tmp}" "${_dp_file}" || die "could not install the Redis version pin"
    _DEPS_WRITTEN_FILES="${_DEPS_WRITTEN_FILES} ${_dp_file}"
}

# _deps_ensure_redis_pin re-applies the pin whenever packages.redis.io is the
# source this installer wrote. On a re-run Redis is already installed (the
# package action is "keep"), so the pin must not depend on that action: if the
# file was removed, an `apt upgrade` would otherwise cross to 8.x.
_deps_ensure_redis_pin() {
    _dep_list="${_DEPS_APT_ROOT}/etc/apt/sources.list.d/redis.list"
    [ -f "${_dep_list}" ] || return 0
    grep -q 'packages\.redis\.io/deb' "${_dep_list}" || return 0
    grep -qF "signed-by=${_DEPS_APT_ROOT}/etc/apt/keyrings/redis.gpg" "${_dep_list}" || return 0
    _deps_pin_redis
}

# _pg_server_version prints the PostgreSQL server major serving the DSN port
# and fails when no server is installed. A psql client alone is not a server:
# pg_lsclusters only prints clusters, and the server binary path is the
# fallback for hosts without postgresql-common.
_pg_server_version() {
    _psv=""
    if command -v pg_lsclusters >/dev/null 2>&1; then
        _psv="$(pg_lsclusters --no-header 2>/dev/null | awk '$3 == 5432 { print $1; exit }')"
        [ -n "${_psv}" ] || _psv="$(pg_lsclusters --no-header 2>/dev/null | awk 'NR == 1 { print $1 }')"
    fi
    if [ -z "${_psv}" ]; then
        for _psbin in "${_DEPS_PG_LIB_DIR}"/*/bin/postgres; do
            [ -x "${_psbin}" ] || continue
            _psv="${_psbin#"${_DEPS_PG_LIB_DIR}"/}"
            _psv="${_psv%%/*}"
            break
        done
    fi
    [ -n "${_psv}" ] || return 1
    printf '%s\n' "${_psv}"
}

# plan_postgresql decides between keeping an installed PostgreSQL, the
# distribution package, and PGDG. An existing major is never upgraded; a
# too-old one only produces the warning and points at the upgrade guide.
plan_postgresql() {
    PG_ACTION=""
    if PG_VERSION="$(_pg_server_version)"; then
        PG_ACTION="keep"
        if ! _version_at_least "${PG_VERSION}" "${MIN_POSTGRES_MAJOR}"; then
            log "WARNING: PostgreSQL ${PG_VERSION} is older than ${MIN_POSTGRES_MAJOR}; it is left as-is (major upgrades are never automatic)."
            log "         Upgrade guide: ${DB_UPGRADE_DOCS_URL}"
        fi
        return 0
    fi
    _pg_candidate="$(_deps_candidate postgresql)"
    if [ -n "${_pg_candidate}" ] && [ "${_pg_candidate}" != "(none)" ] \
        && _version_at_least "$(_version_cut "${_pg_candidate}" 1)" "${MIN_POSTGRES_MAJOR}"; then
        PG_ACTION="distro"
    else
        PG_ACTION="pgdg"
    fi
}

# plan_redis decides between keeping an installed Redis (6.2 or newer is
# supported and left alone), the distribution package, and packages.redis.io
# pinned to the 7.2 series. An existing older Redis is never upgraded.
plan_redis() {
    REDIS_ACTION=""
    if command -v redis-server >/dev/null 2>&1; then
        REDIS_ACTION="keep"
        REDIS_VERSION="$(redis-server --version 2>/dev/null \
            | sed -n 's/.*v=\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')"
        [ -n "${REDIS_VERSION}" ] || REDIS_VERSION="unknown"
        if [ "${REDIS_VERSION}" != "unknown" ] \
            && ! _version_at_least "${REDIS_VERSION}" "${MIN_REDIS_VERSION}"; then
            log "WARNING: Redis ${REDIS_VERSION} is older than ${MIN_REDIS_VERSION}; it is left as-is (no automatic upgrade)."
            log "         Upgrade guide: ${DB_UPGRADE_DOCS_URL}"
        fi
        return 0
    fi
    _redis_candidate="$(_deps_candidate redis-server)"
    if [ -n "${_redis_candidate}" ] && [ "${_redis_candidate}" != "(none)" ] \
        && _version_at_least "$(_version_cut "${_redis_candidate}" 2)" "${MIN_REDIS_VERSION}"; then
        REDIS_ACTION="distro"
    else
        REDIS_ACTION="repo"
    fi
}

# install_planned_dependencies verifies every needed repository key before it
# writes any repository file, so a tampered second key leaves /etc/apt
# untouched, then updates the lists and installs the planned packages. If the
# new sources make apt update fail, the files this run added are removed
# again.
install_planned_dependencies() {
    if [ "${PG_ACTION}" = "pgdg" ]; then
        log "the distribution PostgreSQL is older than ${MIN_POSTGRES_MAJOR}; using the PGDG apt repository"
    fi
    if [ "${REDIS_ACTION}" = "repo" ]; then
        log "the distribution Redis is older than ${MIN_REDIS_VERSION}; using packages.redis.io pinned to ${REDIS_PINNED_SERIES}"
    fi
    if [ "${PG_ACTION}" = "pgdg" ] || [ "${REDIS_ACTION}" = "repo" ]; then
        # gnupg comes from the distribution and is trusted by its own keyring;
        # it is the tool used to verify the third-party keys below.
        command -v gpg >/dev/null 2>&1 || _deps_apt_install ca-certificates gnupg
        _deps_distro_codename
    fi
    _DEPS_KEY_DIR=""
    _DEPS_WRITTEN_FILES=""
    _pgdg_key=""
    _redis_key=""
    if [ "${PG_ACTION}" = "pgdg" ]; then
        _deps_fetch_key "${PGDG_KEY_URL}" "${PGDG_KEY_FINGERPRINT}" pgdg "PGDG"
        _pgdg_key="${_dk_file}"
    fi
    if [ "${REDIS_ACTION}" = "repo" ]; then
        _deps_fetch_key "${REDIS_KEY_URL}" "${REDIS_KEY_FINGERPRINT}" redis "Redis"
        _redis_key="${_dk_file}"
    fi
    # Both keys are verified; writing a repository is now safe.
    if [ -n "${_pgdg_key}" ]; then
        _deps_write_repo "${_pgdg_key}" "${PGDG_KEY_FINGERPRINT}" pgdg \
            "${PGDG_REPO_URL} ${_dd_codename}-pgdg main" "PGDG"
    fi
    if [ -n "${_redis_key}" ]; then
        _deps_write_repo "${_redis_key}" "${REDIS_KEY_FINGERPRINT}" redis \
            "${REDIS_REPO_URL} ${_dd_codename} main" "Redis"
    fi
    # Re-apply on every run, not only when Redis is installed now: a re-run
    # with Redis already installed would otherwise leave a deleted pin missing.
    _deps_ensure_redis_pin
    rm -rf "${_DEPS_KEY_DIR}"
    _DEPS_KEY_DIR=""
    if [ "${PG_ACTION}" = "distro" ] || [ "${PG_ACTION}" = "pgdg" ] \
        || [ "${REDIS_ACTION}" = "distro" ] || [ "${REDIS_ACTION}" = "repo" ]; then
        if [ "${_DEPS_APT_UPDATED}" -ne 1 ] && ! _deps_apt_update; then
            # A source the repository does not serve (for example a codename it
            # has not published) must not break every later apt update.
            for _dwf in ${_DEPS_WRITTEN_FILES}; do
                rm -f "${_dwf}"
            done
            _DEPS_WRITTEN_FILES=""
            die "could not update the package lists with the new repository; removed the files this run added"
        fi
    fi
    case "${PG_ACTION}" in
        distro)
            log "installing PostgreSQL from the distribution packages"
            _deps_apt_install postgresql
            ;;
        pgdg) _deps_apt_install "postgresql-${MIN_POSTGRES_MAJOR}" ;;
    esac
    case "${REDIS_ACTION}" in
        distro)
            log "installing Redis from the distribution packages"
            _deps_apt_install redis-server
            ;;
        repo)
            # The repository epoch is 6: on both jammy and bookworm; pinning
            # both packages keeps apt from mixing 7.2 with a newer redis-tools.
            _deps_apt_install "redis-server=6:${REDIS_PINNED_SERIES}.*" \
                "redis-tools=6:${REDIS_PINNED_SERIES}.*"
            ;;
    esac
    if ! PG_VERSION="$(_pg_server_version)"; then
        PG_VERSION="unknown"
    fi
    REDIS_VERSION="$(redis-server --version 2>/dev/null \
        | sed -n 's/.*v=\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')"
    [ -n "${REDIS_VERSION}" ] || REDIS_VERSION="unknown"
}

# provision_local_database creates the role and database for a fresh local
# install only (no DSN was stored). The password is generated above and travels
# on stdin, never on a command line. An existing role or database means this is
# not a fresh host: never reset it; abort and give the operator the safe
# recovery commands. The cleanup trap drops only what this run creates.
DB_ROLE_CREATED=0
DB_DATABASE_CREATED=0
DB_REFUSAL_HELP="If this is a brand-new server and these are leftovers from an interrupted install, remove them with:
  sudo -u postgres psql -v ON_ERROR_STOP=1 <<'SQL'
DROP DATABASE IF EXISTS gotham;
DROP ROLE IF EXISTS gotham;
SQL
Only do this if there is no Gotham data to keep. Otherwise restore ${ENV_FILE} or set GOTHAM_DATABASE_DSN (with GOTHAM_SKIP_DEPS=1) to keep the existing database."
provision_local_database() {
    _admin_xtrace_off
    case "${DB_PASSWORD}" in
        '' | *[!A-Za-z0-9_-]*) die "could not generate a valid database password" ;;
    esac
    if runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='gotham'" | grep -q 1; then
        die "an existing PostgreSQL role 'gotham' was found but no GOTHAM_DATABASE_DSN is stored in ${ENV_FILE}; refusing to reset it.
${DB_REFUSAL_HELP}"
    fi
    if runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='gotham'" | grep -q 1; then
        die "an existing PostgreSQL database 'gotham' was found but no GOTHAM_DATABASE_DSN is stored in ${ENV_FILE}; refusing to take it over.
${DB_REFUSAL_HELP}"
    fi
    # Mark the objects before creating them: a signal deferred until after the
    # command returns must still make the EXIT trap drop them.
    DB_ROLE_CREATED=1
    printf "CREATE ROLE gotham LOGIN PASSWORD '%s';\n" "${DB_PASSWORD}" \
        | runuser -u postgres -- psql -v ON_ERROR_STOP=1 -q -f - \
        || die "could not create the gotham database role; gotham.env was not changed"
    DB_DATABASE_CREATED=1
    runuser -u postgres -- createdb -O gotham -- gotham \
        || die "could not create the gotham database; gotham.env was not changed"
    _admin_xtrace_on
}

DEPS_SUMMARY=""
if [ "${DRY_RUN}" -eq 1 ] && [ "${LOCAL_DSN}" -eq 1 ] && [ "${GOTHAM_SKIP_DEPS:-0}" != "1" ]; then
    log "would install PostgreSQL ${MIN_POSTGRES_MAJOR} and Redis ${MIN_REDIS_VERSION}+ (distribution packages when new enough)"
fi
# Only provision local services when this installer owns the DSN; a managed
# DSN (from the CLI or a previous install) is left alone.
if [ "${PROVISION_LOCAL_DEPS}" -eq 1 ]; then
    command -v apt-get >/dev/null 2>&1 \
        || die "PostgreSQL and Redis are required; install them or set GOTHAM_DATABASE_DSN and GOTHAM_SKIP_DEPS=1"
    # Candidate versions come from the package lists; refresh them once when a
    # package is missing (a fully installed host never touches the network).
    if ! _pg_server_version >/dev/null || ! command -v redis-server >/dev/null 2>&1; then
        _deps_apt_update || die "could not update the package lists"
    fi
    plan_postgresql
    plan_redis
    install_planned_dependencies
    log "enabling PostgreSQL and Redis"
    systemctl enable --now postgresql redis-server || die "could not start PostgreSQL/Redis"
    # A stored DSN means an existing role and database: leave both untouched.
    # Only a fresh install creates them, and it refuses to take over an
    # existing role/database without a stored DSN.
    if [ "${FRESH_LOCAL_DB}" -eq 1 ]; then
        provision_local_database
    fi
    log "PostgreSQL ${PG_VERSION} and Redis ${REDIS_VERSION} are ready"
    DEPS_SUMMARY="  Database: PostgreSQL ${PG_VERSION} (local)
  Cache:    Redis ${REDIS_VERSION} (local)
"
fi
# Replacing gotham.env is the last step: the DSN and the role it names are
# already in place (fresh) or were never touched (existing install).
install_env_file || die "could not replace ${ENV_FILE}; the previous file was kept"
if [ "${TEST_MODE}" -eq 1 ] && [ "${GOTHAM_INSTALL_TEST_RUN_AGENT:-0}" != "1" ]; then
    log "test mode: skipping service activation and the localhost agent install"
    log "done (test install under ${PREFIX})"
    exit 0
fi
# GOTHAM_INSTALL_TEST_RUN_AGENT=1 (sandbox test mode only) runs past
# the exit above with shims for systemctl and the agent installer, so the
# suite can execute the real agent-failure path end to end.

# ---- Migrate + start --------------------------------------------------------
# Skipped in test mode (no database there; the agent-failure suite covers the
# tail with a systemctl shim instead).
if [ "${DRY_RUN}" -eq 0 ] && [ "${TEST_MODE}" -eq 0 ]; then
    log "applying database migrations"
    # The DSN (which may hold the generated password) travels on stdin, not on
    # a command line, and is exported by the child shell before exec.
    _admin_xtrace_off
    printf '%s\n' "${DSN}" | runuser -u "${SERVICE_USER}" -- sh -c \
        'IFS= read -r GOTHAM_DATABASE_DSN
         export GOTHAM_DATABASE_DSN
         GOTHAM_LEGACY_LOCAL_NODE_ID="$2"
         export GOTHAM_LEGACY_LOCAL_NODE_ID
         cd / && exec "$1" migrate up' gotham-migrate "${INSTALL_PATH}" "${LEGACY_LOCAL_NODE_ID}" \
        || die "database migrations failed (check PostgreSQL and GOTHAM_DATABASE_DSN)"
    _admin_xtrace_on
fi

# ---- First admin account: creation (phase 2) --------------------------------
# Right after the migrations and before the service starts, so the first
# account exists before registration opens (no first-account window).
admin_phase2_create

log "starting ${BINARY_NAME}"
run systemctl daemon-reload
run systemctl enable "${BINARY_NAME}.service"
# Restart, not just enable --now: enable --now leaves an already-running
# (pre-upgrade) process on the old binary, so the installed version would
# never actually run. Restart starts a fresh unit and moves an existing one
# onto the just-installed binary.
run systemctl restart "${BINARY_NAME}.service"

# The control plane creates its bootstrap credential during startup, after
# systemctl restart returns. The agent installer requires a root-owned copy.
LOCAL_AGENT_TOKEN_FILE="${ETC_DIR}/localhost-enrollment-token"
if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] wait for ${CA_DIR}/enrollment-token and install a root-owned 0600 copy at ${LOCAL_AGENT_TOKEN_FILE}"
else
    token_wait=0
    while [ ! -s "${CA_DIR}/enrollment-token" ]; do
        token_wait=$((token_wait + 1))
        [ "${token_wait}" -le 30 ] || die "control plane did not create ${CA_DIR}/enrollment-token within 30 seconds; control-plane files were kept, check journalctl -u gotham"
        sleep 1
    done
    [ -f "${CA_DIR}/enrollment-token" ] && [ ! -L "${CA_DIR}/enrollment-token" ] \
        || die "control-plane enrollment token must be a regular file"
    if [ "${TEST_MODE}" -eq 1 ]; then
        install -m 0600 "${CA_DIR}/enrollment-token" "${LOCAL_AGENT_TOKEN_FILE}"
    else
        install -m 0600 -o root -g root "${CA_DIR}/enrollment-token" "${LOCAL_AGENT_TOKEN_FILE}"
    fi
fi

# ---- Localhost agent (the control plane's first node) -----------------------
# Install over the CA-protected loopback channel. Platform/config checks above
# already passed; an agent failure below must make this install fail.
{
        for agent_sibling in install-agent.sh install-agent-lib.sh gotham-agent-updater.conf install-agent-sudoers.sh gotham-agent.service gotham-toolctl.sh install-toolctl-sudoers.sh; do
            if [ ! -f "${SCRIPT_DIR}/${agent_sibling}" ]; then
                die "install.sh: ${agent_sibling} must be next to this script for the localhost agent install"
            fi
        done
        # Use the same mandatory identity and address validated before mutation.
        derive_agent_defaults
        LOCAL_AGENT_NODE_ID="${_derived_node_id}"
        LOCAL_AGENT_CP_ADDR="${_derived_cp_addr}"
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
        LOCAL_AGENT_RETRY="${LOCAL_AGENT_ENV} sh $(sh_quote "${SCRIPT_DIR}/install-agent.sh") --full --ca $(sh_quote "${CA_DIR}/ca.crt") --enrollment-token-file $(sh_quote "${LOCAL_AGENT_TOKEN_FILE}")"
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
            _agent_label="${LOCAL_AGENT_NODE_ID}"
            [ -n "${_agent_label}" ] || _agent_label="the existing node"
            log "installing the localhost agent node ${_agent_label}"
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
                    -u GOTHAM_INSTALL_TEST_DEPS \
                    -u GOTHAM_INSTALL_TEST_PUBLIC_KEY \
                    -u GOTHAM_INSTALL_TEST_ADMIN_BIN \
                    -u GOTHAM_INSTALL_TEST_ADMIN_TTY \
                    -u GOTHAM_AGENT_FILE_MANAGER_TTY \
                    "$@" sh "${_agent_script}" --full --ca "${CA_DIR}/ca.crt" --enrollment-token-file "${LOCAL_AGENT_TOKEN_FILE}"
            ) || LOCAL_AGENT_FAILED=1
        fi
}

# ---- Optional panel domain -------------------------------------------------
PANEL_URL=""
DOMAIN_CONFIRM_NOTE=""
DOMAIN_BIN="${INSTALL_PATH}"
if [ "${TEST_MODE}" -eq 1 ]; then
    DOMAIN_BIN="${GOTHAM_INSTALL_TEST_ADMIN_BIN:-${INSTALL_PATH}}"
fi
if [ -n "${DOMAIN}" ]; then
    log "waiting up to 60 seconds for the localhost agent before domain setup"
    log "Cloudflare users: set the panel record to DNS-only (grey cloud). Ports 80/443 must reach this host."
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[dry-run] ${INSTALL_PATH} instance wait-localhost (up to 60 seconds)"
        echo "[dry-run] ${INSTALL_PATH} instance set-url https://${DOMAIN}"
    elif [ "${TEST_MODE}" -eq 1 ] && [ -z "${GOTHAM_INSTALL_TEST_ADMIN_BIN:-}" ]; then
        log "test mode: skipping domain setup (no command binary seam)"
    else
        # The DSN (with the generated password) travels in the environment of
        # these calls; keep xtrace off so `sh -x` never logs it.
        _admin_xtrace_off
        if GOTHAM_DATABASE_DSN="${DSN}" GOTHAM_REDIS_ADDR="${REDIS_ADDR}" \
            "${DOMAIN_BIN}" instance wait-localhost && \
            GOTHAM_DATABASE_DSN="${DSN}" GOTHAM_REDIS_ADDR="${REDIS_ADDR}" \
            "${DOMAIN_BIN}" instance set-url "https://${DOMAIN}"; then
            _domain_rc=0
        else
            _domain_rc=1
        fi
        _admin_xtrace_on
        if [ "${_domain_rc}" -eq 0 ]; then
            # Re-arm the persisted trial timer in the long-running service.
            run systemctl restart "${BINARY_NAME}.service"
            PANEL_URL="http://${DOMAIN}"
            DOMAIN_CONFIRM_NOTE="  Confirm the domain in General settings within 2 minutes to start HTTPS."
            log "Open ${PANEL_URL} and confirm the domain in General settings within 2 minutes to start HTTPS."
        else
            log "WARNING: domain setup failed; continuing on the host HTTP address. Set the domain in General settings later."
        fi
    fi
fi

# ---- First admin account: summary -------------------------------------------
# Credentials were collected in phase 1 (before the first mutation); the
# account itself was created in phase 2 (before the service start), so
# registration was never open before the first account existed.

HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "${HOST_IP}" ] || HOST_IP="127.0.0.1"

_admin_xtrace_off
PANEL_URL="${PANEL_URL:-http://${HOST_IP}:8000}"
ADMIN_LOGIN_LINE="Login:    ${PANEL_URL}"
case "${ADMIN_STATUS}" in
    created)
        if [ -n "${ADMIN_GENERATED}" ]; then
            ADMIN_FIRST_LOGIN="  Account:  ${ADMIN_EMAIL}
  Password: ${ADMIN_GENERATED}
  ${ADMIN_LOGIN_LINE}
  Store the password now, it is not shown again."
        else
            ADMIN_FIRST_LOGIN="  Account:  ${ADMIN_EMAIL}
  ${ADMIN_LOGIN_LINE}"
        fi
        ;;
    exists)
        ADMIN_FIRST_LOGIN="  The admin account already exists, skipping.
  ${ADMIN_LOGIN_LINE}"
        ;;
    no-email)
        ADMIN_FIRST_LOGIN="  No admin account was created (no email given).
  Create it with:
    sudo -u gotham -- ${INSTALL_PATH} admin create --email ops@example.com
  Then open:
  ${ADMIN_LOGIN_LINE}"
        ;;
    failed)
        ADMIN_FIRST_LOGIN="  The admin account could not be created automatically.
  Create it with:
    sudo -u gotham -- ${INSTALL_PATH} admin create --email ${ADMIN_EMAIL:-ops@example.com}
  Then open:
  ${ADMIN_LOGIN_LINE}"
        ;;
    legacy-manual)
        ADMIN_FIRST_LOGIN="  The installed release predates automated admin bootstrap.
  If no account exists yet, create it with:
    sudo -u gotham -- ${INSTALL_PATH} admin create --email ${ADMIN_EMAIL:-ops@example.com}
  (answer the hidden password prompt), then open:
  ${ADMIN_LOGIN_LINE}"
        ;;
    legacy-upgrade)
        ADMIN_FIRST_LOGIN="  The installed release has no admin commands at all, so no account was created.
  Re-run this installer once a newer release is available (it bootstraps the
  first admin automatically), then open:
  ${ADMIN_LOGIN_LINE}"
        ;;
    *)
        ADMIN_FIRST_LOGIN="  Open the Web UI and sign in with the admin account.
  ${ADMIN_LOGIN_LINE}"
        ;;
esac

cat <<EOF

Gotham ${VERSION} is installed.

  Web UI:   ${PANEL_URL}
${DOMAIN_CONFIRM_NOTE}
  Status:   systemctl status gotham
  Logs:     journalctl -u gotham -f
${DEPS_SUMMARY}${LEGACY_DB_PASSWORD_NOTE}

First login:
${ADMIN_FIRST_LOGIN}
  The Servers page already lists this host's agent node (ready once its
  first heartbeat lands). Add more nodes with deploy/install-agent.sh.
  Platform-global operations (node-wide proxy sync, DNS providers) are
  available to the instance owner (the first account). Lost the password?
  sudo -u gotham -- ${INSTALL_PATH} admin reset-password --email <email>.

Self-update checking is enabled by default. To apply new releases unattended,
add AUTO_UPDATE=true to /etc/gotham/gotham.env (operator edits there are kept
across reinstalls).
EOF
_admin_xtrace_on

# A failed localhost agent step leaves the control plane installed and running
# (nothing was rolled back): report it loudly with the exact retry command and
# exit nonzero only now that the control plane is fully up. A failed admin
# step behaves the same way: the summary above already shows the manual
# command, so warn with the reason and exit nonzero too.
FINAL_RC=0
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
    FINAL_RC=1
fi
if [ "${ADMIN_FAILED:-0}" -eq 1 ]; then
    cat >&2 <<EOF

================================================================
WARNING: the control plane is installed and running, but ${ADMIN_FAIL_REASON}.
Nothing was rolled back. Create the account with the command shown above
and re-run this installer to verify (an existing account is kept as-is).
================================================================
EOF
    FINAL_RC=1
fi
if [ "${FINAL_RC}" -ne 0 ]; then
    exit "${FINAL_RC}"
fi