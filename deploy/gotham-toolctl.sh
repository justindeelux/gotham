#!/bin/sh
#
# gotham-toolctl: privileged host-tool installer/uninstaller for the Gotham node
# agent.
#
# Installed root-owned at /usr/libexec/gotham/gotham-toolctl and reached only
# through the sudoers drop-in written by install-toolctl-sudoers.sh, which grants
# the gotham-agent service user exactly one rule per verb and tool id:
#
#   install-tool <id>   |   uninstall-tool <id>
#
# The agent passes only a catalog id. This script owns every executable argv and
# validates the id against an allow-list, so a compromised agent cannot widen
# what runs as root: it cannot name a package, a URL or an extra flag. The
# control plane uses the same helper to repair the host helper
# (`install-tool gotham-hostctl`), reaching it through the verb-pinned rule the
# `gotham host-bootstrap` step writes; the content it installs is the root-owned
# staged copy next to this script, never caller-supplied bytes.
#
# Versions and their sha256 digests are pinned below. Downloads are verified
# before a binary is installed; a mismatch aborts (exit 4).
#
# Sandbox: the agent unit runs with ProtectSystem=full, so /usr, /etc and /boot
# are read-only in its mount namespace and a `sudo` child inherits that. This
# script therefore re-executes itself in a fresh transient unit through
# systemd-run (as root), which is outside the agent's sandbox. The inner run is
# marked with GOTHAM_TOOLCTL_INNER=1 so it does not re-exec again. The
# GOTHAM_TOOLCTL_* test overrides below are honoured only without sudo, like
# gotham-hostctl.
#
# One job at a time: the transient unit holds a flock for its whole life, so a
# retry after a cancelled or lingering job fails fast instead of overlapping
# it, and the unit carries a RuntimeMaxSec bound so it cannot outlive the
# request.
#
# Exit codes: 0 = success, 1 = a command failed, 2 = usage, 3 = tool not
# allowed, 4 = checksum mismatch, 5 = unsupported platform or distribution,
# 6 = another job is already running.
set -u
umask 022

# A fixed PATH: with systemd-run the transient unit gets systemd's clean
# default, but the no-systemd-run fallback would inherit sudo's PATH (Alpine's
# sudo has no secure_path). A fixed list keeps a writable directory off it.
PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

# Hard wall-clock bound for the transient unit, kept under the agent's install
# timeout (agent/tool_server.go defaultToolTimeout, 15m) so a job cannot
# outlive its request even if the agent is killed mid-run.
RUNTIME_MAX_SEC="840"

# ---- test overrides (honoured only without sudo) ---------------------------
ROOT=""
DRYRUN="0"
INNER="${GOTHAM_TOOLCTL_INNER:-0}"
ARCH_OVERRIDE=""
DISTRO_OVERRIDE=""
# Test-only seams (like gotham-hostctl's GOTHAM_HOSTCTL_*): they can redirect
# where the helper looks for systemd-run or its lock, but never grant privilege
# — under sudo they are ignored, and the helper is only ever reached via sudo.
SYSTEMD_RUN="systemd-run"
LOCK_OVERRIDE=""
if [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
    ROOT="${GOTHAM_TOOLCTL_ROOT:-}"
    DRYRUN="${GOTHAM_TOOLCTL_DRYRUN:-0}"
    ARCH_OVERRIDE="${GOTHAM_TOOLCTL_ARCH:-}"
    DISTRO_OVERRIDE="${GOTHAM_TOOLCTL_DISTRO:-}"
    SYSTEMD_RUN="${GOTHAM_TOOLCTL_SYSTEMD_RUN:-systemd-run}"
    LOCK_OVERRIDE="${GOTHAM_TOOLCTL_LOCK:-}"
fi

# ---- pinned install material ----------------------------------------------
RAILPACK_VERSION="0.42.0"
RAILPACK_BASE="https://github.com/railwayapp/railpack/releases/download"
PACK_VERSION="0.40.9"
PACK_BASE="https://github.com/buildpacks/pack/releases/download"
BUILDKIT_TAG="moby/buildkit:v0.34.0"
BUILDKIT_DIGEST="sha256:b059f8d7226d0b326bc871af489a5dddf65e36af4d20d14251b072974d9ee87c"
BUILDKIT_IMAGE="${BUILDKIT_TAG}@${BUILDKIT_DIGEST}"
BUILDKIT_CONTAINER="buildkit"
# The untagged reference the catalog's detection probe inspects.
BUILDKIT_DETECT_REF="moby/buildkit"

BIN_DIR="${ROOT}/usr/local/bin"

die() { echo "gotham-toolctl: $*" >&2; exit "${2:-1}"; }

# ---- argument + id validation ---------------------------------------------
[ "$#" -eq 2 ] || die "usage: gotham-toolctl install-tool|uninstall-tool <id>" 2
VERB="$1"
TOOL="$2"

allow_install() {
    case "$1" in
        railpack | buildkit | pack | docker | git | gotham-hostctl) return 0 ;;
        *) die "tool is not installable: $1" 3 ;;
    esac
}
allow_uninstall() {
    case "$1" in
        railpack | buildkit | pack) return 0 ;;
        git) die "cannot uninstall git: required by the platform for source deployments" 3 ;;
        docker) die "cannot uninstall docker: required by the platform to run workloads" 3 ;;
        gotham-hostctl) die "cannot uninstall gotham-hostctl: required by the platform for host network and system settings" 3 ;;
        *) die "tool is not uninstallable: $1" 3 ;;
    esac
}
case "${VERB}" in
    install-tool) allow_install "${TOOL}" ;;
    uninstall-tool) allow_uninstall "${TOOL}" ;;
    *) die "unknown verb: ${VERB}" 2 ;;
esac

# ---- re-exec outside the agent's systemd sandbox --------------------------
# Root's systemd-run creates a transient unit with a clean namespace (no
# ProtectSystem), which is what lets the install write /usr and /etc. It is
# attempted only for the installed, absolute helper path.
#
# --wait only waits: it does not stop the unit when this process is killed. The
# RuntimeMaxSec property bounds a job whose agent went away. POSIX shells may
# defer the TERM/INT trap until the foreground systemd-run returns; cancellation
# is bounded by RuntimeMaxSec and flock, not an immediate unit stop.
SELF="$0"
case "${SELF}" in /*) SELF_OK=1 ;; *) SELF_OK=0 ;; esac
if [ "${DRYRUN}" != "1" ] && [ "${INNER}" != "1" ] && [ "${SELF_OK}" = "1" ] \
    && command -v "${SYSTEMD_RUN}" >/dev/null 2>&1; then
    UNIT="gotham-toolctl-${VERB}-${TOOL}-$$"
    # shortcut: this trap can be deferred during systemd-run; use an interruptible
    # background wait when prompt graceful cancellation is implemented.
    trap 'systemctl stop "$UNIT" >/dev/null 2>&1 || true; exit 130' TERM INT HUP
    "${SYSTEMD_RUN}" --quiet --pipe --wait --collect \
        --unit="${UNIT}" \
        --property="RuntimeMaxSec=${RUNTIME_MAX_SEC}" \
        --setenv=GOTHAM_TOOLCTL_INNER=1 \
        "${SELF}" "$@"
    exit $?
fi

# ---- one job at a time on the node ----------------------------------------
# Held for the whole job (the transient unit's process, or this process when
# systemd-run is unavailable). A concurrent second job is refused, so two units
# can never overlap even if a previous one outlived its agent request. Skipped
# in a dry-run unless a test pins the lock path.
if [ "${DRYRUN}" != "1" ] || [ -n "${LOCK_OVERRIDE}" ]; then
    LOCK="${LOCK_OVERRIDE:-/run/gotham-toolctl.lock}"
    command -v flock >/dev/null 2>&1 \
        || die "flock is unavailable; refusing to run without the node lock" 6
    : >>"${LOCK}" 2>/dev/null || die "cannot open the node lock ${LOCK}" 6
    exec 9>>"${LOCK}"
    flock -n 9 || die "another tool install or uninstall is already running on this node" 6
fi

# ---- platform -------------------------------------------------------------
ARCH="${ARCH_OVERRIDE:-$(uname -m)}"
case "${ARCH}" in
    x86_64 | amd64)
        ARCH="amd64"
        RAILPACK_TARGET="x86_64-unknown-linux-musl"
        RAILPACK_SHA256="81bca91d0577c7650378762609116d12d8630e2cfc1779e41d3ac3964fc7194b"
        PACK_ASSET="pack-v${PACK_VERSION}-linux.tgz"
        PACK_SHA256="dc0ee1e931cf8a106d7555a01a214864f9acb60b77adf15d69b74df4404758e9"
        ;;
    aarch64 | arm64)
        ARCH="arm64"
        RAILPACK_TARGET="arm64-unknown-linux-musl"
        RAILPACK_SHA256="63c0417d6c0a437162fa2c688ddf58aabf9e4433eeffb69d8c3fb498603e2c3b"
        PACK_ASSET="pack-v${PACK_VERSION}-linux-arm64.tgz"
        PACK_SHA256="091ccb213823656c727731537ef8f1000eb4dc3ec61641506653e7f9d6da0c5e"
        ;;
    *) die "unsupported architecture: ${ARCH}" 5 ;;
esac

detect_distro() {
    release="${ROOT}/etc/os-release"
    if [ -r "${release}" ]; then
        ids="$(sed -n -E 's/^(ID|ID_LIKE)="?([^"]*)"?$/\2/p' "${release}" 2>/dev/null | tr '[:upper:]' '[:lower:]')"
        for id in ${ids}; do
            case "${id}" in
                debian | ubuntu | raspbian | linuxmint | pop | kali) echo apt; return 0 ;;
                fedora | rhel | centos | rocky | almalinux | amzn | oracle) echo dnf; return 0 ;;
                alpine) echo apk; return 0 ;;
            esac
        done
    fi
    for candidate in "apt-get:apt" "dnf:dnf" "apk:apk"; do
        bin="${candidate%%:*}"
        name="${candidate##*:}"
        if command -v "${bin}" >/dev/null 2>&1; then
            echo "${name}"
            return 0
        fi
    done
    echo ""
}

DISTRO="${DISTRO_OVERRIDE:-$(detect_distro)}"

# ---- helpers --------------------------------------------------------------
run() {
    if [ "${DRYRUN}" = "1" ]; then
        echo "+ $*"
        return 0
    fi
    "$@"
}

# fetch_verified downloads url into dest and aborts unless its sha256 matches
# want. In dry-run it only prints, so a test never hits the network.
fetch_verified() {
    url="$1"
    want="$2"
    dest="$3"
    if [ "${DRYRUN}" = "1" ]; then
        echo "+ curl -fsSL --retry 3 -o ${dest} ${url}"
        echo "+ verify sha256 ${want} ${dest}"
        return 0
    fi
    curl -fsSL --retry 3 --retry-all-errors --retry-delay 2 -o "${dest}" "${url}" \
        || die "download failed: ${url}" 1
    got="$(sha256sum "${dest}" | awk '{print $1}')"
    [ "${got}" = "${want}" ] || die "checksum mismatch for ${url}" 4
}

WORK=""
cleanup() {
    [ -n "${WORK}" ] && rm -rf "${WORK}"
    return 0
}
trap 'cleanup' EXIT
WORK="$(mktemp -d "${TMPDIR:-/tmp}/gotham-toolctl.XXXXXX")" || die "could not create a work directory" 1

# ---- installers / uninstallers --------------------------------------------
install_railpack() {
    asset="railpack-v${RAILPACK_VERSION}-${RAILPACK_TARGET}.tar.gz"
    fetch_verified "${RAILPACK_BASE}/v${RAILPACK_VERSION}/${asset}" "${RAILPACK_SHA256}" "${WORK}/${asset}"
    run tar -xzf "${WORK}/${asset}" -C "${WORK}" railpack
    run install -m 0755 "${WORK}/railpack" "${BIN_DIR}/railpack"
}

uninstall_railpack() {
    run rm -f "${BIN_DIR}/railpack"
}

install_pack() {
    fetch_verified "${PACK_BASE}/v${PACK_VERSION}/${PACK_ASSET}" "${PACK_SHA256}" "${WORK}/${PACK_ASSET}"
    run tar -xzf "${WORK}/${PACK_ASSET}" -C "${WORK}" pack
    run install -m 0755 "${WORK}/pack" "${BIN_DIR}/pack"
}

uninstall_pack() {
    run rm -f "${BIN_DIR}/pack"
}

install_buildkit() {
    # Docker stores a digest-qualified pull digest-only: the tag
    # `moby/buildkit:v0.34.0` does not exist locally afterwards, so every
    # reference after the pull must use the digest ref.
    run docker pull "${BUILDKIT_IMAGE}"
    run docker rm -f "${BUILDKIT_CONTAINER}" || true
    run docker run -d --privileged --restart unless-stopped --name "${BUILDKIT_CONTAINER}" "${BUILDKIT_IMAGE}"
    # Tag LAST, from the digest ref, so detection only flips to installed once
    # the container is actually running (a failed run leaves no detection tag).
    run docker tag "${BUILDKIT_IMAGE}" "${BUILDKIT_DETECT_REF}"
}

uninstall_buildkit() {
    run docker rm -f "${BUILDKIT_CONTAINER}" || true
    # Removing the detection tag may also delete the image. Use the repository
    # digest left by pull for partial installs, and quietly accept absent refs.
    for ref in "${BUILDKIT_DETECT_REF}:latest" "${BUILDKIT_DETECT_REF}@${BUILDKIT_DIGEST}"; do
        if output="$(run docker rmi -f "${ref}" 2>&1)"; then
            [ -z "${output}" ] || printf '%s\n' "${output}"
        else
            case "${output}" in
                *"No such image:"*) ;;
                *) printf '%s\n' "${output}" >&2; return 1 ;;
            esac
        fi
    done
}

install_docker() {
    case "${DISTRO}" in
        apt)
            run env DEBIAN_FRONTEND=noninteractive apt-get update
            run env DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io
            run systemctl enable --now docker
            ;;
        dnf)
            run dnf install -y docker
            run systemctl enable --now docker
            ;;
        apk)
            run apk add --no-cache docker
            ;;
        *) die "docker: unsupported distribution" 5 ;;
    esac
}

install_git() {
    # git has no service: unlike docker, never enable a unit for it.
    case "${DISTRO}" in
        apt)
            run env DEBIAN_FRONTEND=noninteractive apt-get update || return $?
            # install also upgrades an existing package, while allowing a first install.
            run env DEBIAN_FRONTEND=noninteractive apt-get install -y git
            ;;
        dnf) run dnf install -y git && run dnf upgrade -y git ;;
        apk) run apk add --no-cache --upgrade git ;;
        *) die "git: unsupported distribution" 5 ;;
    esac
}

# ---- host helper (gotham-hostctl) ------------------------------------------
# Installs the fixed, root-owned host helper the CONTROL PLANE (not the agent)
# uses for instance settings, plus its verb-pinned sudoers drop-in. Content is
# the root-owned staged copy (gotham-hostctl.dist) and staged sudoers
# (gotham-hostctl.sudoers) that the binary's `host-bootstrap` writes next to
# this helper, so a caller naming only the catalog id cannot substitute either.
HOSTCTL_LIVE="${ROOT}/usr/libexec/gotham/gotham-hostctl"
HOSTCTL_DIST="${ROOT}/usr/libexec/gotham/gotham-hostctl.dist"
HOSTCTL_SUDOERS="${ROOT}/usr/libexec/gotham/gotham-hostctl.sudoers"
HOSTCTL_DROPIN="${ROOT}/etc/sudoers.d/gotham-hostctl"

install_hostctl() {
    [ -f "${HOSTCTL_DIST}" ] || die "gotham-hostctl content is missing (${HOSTCTL_DIST})" 5
    [ -f "${HOSTCTL_SUDOERS}" ] || die "gotham-hostctl sudoers is missing (${HOSTCTL_SUDOERS})" 5
    run install -m 0755 -o root -g root "${HOSTCTL_DIST}" "${HOSTCTL_LIVE}"
    if [ "${DRYRUN}" = "1" ]; then
        echo "+ visudo -cf <temp>"
        echo "+ install -m 0440 ${HOSTCTL_SUDOERS} ${HOSTCTL_DROPIN}"
        return 0
    fi
    command -v visudo >/dev/null 2>&1 || die "visudo is required to validate the sudoers drop-in" 1
    tmp="$(mktemp "${ROOT}/etc/sudoers.d/.gotham-hostctl.XXXXXX")" \
        || die "could not create a sudoers temp file" 1
    cp "${HOSTCTL_SUDOERS}" "${tmp}"
    chmod 0440 "${tmp}"
    if ! visudo -cf "${tmp}"; then
        rm -f "${tmp}"
        die "generated host-helper sudoers failed visudo; nothing installed" 1
    fi
    mv -f "${tmp}" "${HOSTCTL_DROPIN}"
}

case "${VERB}:${TOOL}" in
    install-tool:railpack) install_railpack ;;
    install-tool:buildkit) install_buildkit ;;
    install-tool:pack) install_pack ;;
    install-tool:docker) install_docker ;;
    install-tool:git) install_git ;;
    install-tool:gotham-hostctl) install_hostctl ;;
    uninstall-tool:railpack) uninstall_railpack ;;
    uninstall-tool:buildkit) uninstall_buildkit ;;
    uninstall-tool:pack) uninstall_pack ;;
    *) die "unhandled request: ${VERB} ${TOOL}" 3 ;;
esac
