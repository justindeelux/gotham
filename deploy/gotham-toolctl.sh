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
# what runs as root: it cannot name a package, a URL or an extra flag.
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
# Exit codes: 0 = success, 1 = a command failed, 2 = usage, 3 = tool not
# allowed, 4 = checksum mismatch, 5 = unsupported platform or distribution.
set -u
umask 022

# ---- test overrides (honoured only without sudo) ---------------------------
ROOT=""
DRYRUN="0"
INNER="${GOTHAM_TOOLCTL_INNER:-0}"
ARCH_OVERRIDE=""
DISTRO_OVERRIDE=""
if [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
    ROOT="${GOTHAM_TOOLCTL_ROOT:-}"
    DRYRUN="${GOTHAM_TOOLCTL_DRYRUN:-0}"
    ARCH_OVERRIDE="${GOTHAM_TOOLCTL_ARCH:-}"
    DISTRO_OVERRIDE="${GOTHAM_TOOLCTL_DISTRO:-}"
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
        railpack | buildkit | pack | docker | git) return 0 ;;
        *) die "tool is not installable: $1" 3 ;;
    esac
}
allow_uninstall() {
    case "$1" in
        railpack | buildkit | pack | git) return 0 ;;
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
SELF="$0"
case "${SELF}" in /*) SELF_OK=1 ;; *) SELF_OK=0 ;; esac
if [ "${DRYRUN}" != "1" ] && [ "${INNER}" != "1" ] && [ "${SELF_OK}" = "1" ] \
    && command -v systemd-run >/dev/null 2>&1; then
    systemd-run --quiet --pipe --wait --collect \
        --unit="gotham-toolctl-$$" \
        --setenv=GOTHAM_TOOLCTL_INNER=1 \
        "${SELF}" "$@"
    exit $?
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
    # Pull the digest-pinned image, then (re)create the container. The detection
    # reference is tagged LAST: an image present from a failed run must not make
    # the tool look installed while no container runs.
    run docker pull "${BUILDKIT_IMAGE}"
    run docker rm -f "${BUILDKIT_CONTAINER}" || true
    run docker run -d --privileged --restart unless-stopped --name "${BUILDKIT_CONTAINER}" "${BUILDKIT_IMAGE}"
    run docker tag "${BUILDKIT_TAG}" "${BUILDKIT_DETECT_REF}"
}

uninstall_buildkit() {
    # Removing the missing container is not an error; removing the image is what
    # flips detection back to "not installed", so a real failure surfaces.
    run docker rm -f "${BUILDKIT_CONTAINER}" || true
    run docker rmi -f "${BUILDKIT_TAG}" "${BUILDKIT_DETECT_REF}"
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
            run env DEBIAN_FRONTEND=noninteractive apt-get update
            run env DEBIAN_FRONTEND=noninteractive apt-get install -y git
            ;;
        dnf) run dnf install -y git ;;
        apk) run apk add --no-cache git ;;
        *) die "git: unsupported distribution" 5 ;;
    esac
}

uninstall_git() {
    case "${DISTRO}" in
        apt) run env DEBIAN_FRONTEND=noninteractive apt-get remove -y git ;;
        dnf) run dnf remove -y git ;;
        apk) run apk del git ;;
        *) die "git: unsupported distribution" 5 ;;
    esac
}

case "${VERB}:${TOOL}" in
    install-tool:railpack) install_railpack ;;
    install-tool:buildkit) install_buildkit ;;
    install-tool:pack) install_pack ;;
    install-tool:docker) install_docker ;;
    install-tool:git) install_git ;;
    uninstall-tool:railpack) uninstall_railpack ;;
    uninstall-tool:buildkit) uninstall_buildkit ;;
    uninstall-tool:pack) uninstall_pack ;;
    uninstall-tool:git) uninstall_git ;;
    *) die "unhandled request: ${VERB} ${TOOL}" 3 ;;
esac
