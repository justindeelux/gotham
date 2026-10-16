#!/bin/sh
#
# gotham-hostbootstrap: installs the fixed, root-owned host helper
# (gotham-hostctl) and host-tool installer (gotham-toolctl) with the
# verb+id-pinned sudoers drop-in for the control-plane user (JUS-185).
#
# It is the single source of the host-helper payload. It runs only from a
# root-trusted source: `install.sh` (run by the operator as root, from the
# release checkout) and the root self-update wrapper `gotham-update`, which
# re-applies the already-installed payload. It never reads a service-writable
# file as code and never executes the service binary; the payload it writes
# under /usr/libexec/gotham/hostbootstrap is root-owned, so root can re-read it
# safely on later updates.
#
#   gotham-hostbootstrap create <cp-user>   install payload + apply
#   gotham-hostbootstrap apply              apply the payload only
#
# Fixed paths, fixed content (from this directory's sibling scripts): nothing is
# read from the environment or configuration. A destination that is a symlink is
# refused, every file is written to a mktemp file in its own directory and
# renamed into place, and a sudoers drop-in is validated with visudo before the
# rename, so a broken sudoers file is never left behind. Run is idempotent.
#
# The test seam GOTHAM_HOST_BOOTSTRAP_ROOT is honoured only when NOT running
# under sudo (like the other installer seams); production always uses "/".
set -eu
umask 022

SELF_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

ROOT=""
if [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
    ROOT="${GOTHAM_HOST_BOOTSTRAP_ROOT:-}"
fi

PAYLOAD_DIR="${ROOT}/usr/libexec/gotham/hostbootstrap"
HELPER_DIR="${ROOT}/usr/libexec/gotham"
SUDOERS_DIR="${ROOT}/etc/sudoers.d"
HOSTCTL="${HELPER_DIR}/gotham-hostctl"
TOOLCTL="${HELPER_DIR}/gotham-toolctl"
HOSTCTL_DROPIN="${SUDOERS_DIR}/gotham-hostctl"

# Closed verb set the control plane may run the host helper with; the last rule
# is the single pinned repair verb (the tool installer re-applies the payload).
HOSTCTL_VERBS="status apply-network confirm-network revert-network apply-system"

die() { echo "gotham-hostbootstrap: $*" >&2; exit "${2:-1}"; }

valid_user() {
    case "$1" in
        "" | ALL | all | *[!A-Za-z0-9_-]*) return 1 ;;
        *) return 0 ;;
    esac
}

symlink_free() { # path: refuse a symlinked destination.
    [ -L "$1" ] && die "refusing to replace symlink $1"
    return 0
}

# install_file src dst mode: atomically place a regular file, refusing a
# symlinked destination and skipping an identical one.
install_file() {
    src="$1"; dst="$2"; mode="$3"
    symlink_free "${dst}"
    if [ -f "${dst}" ] && cmp -s "${src}" "${dst}"; then
        return 0
    fi
    tmp="$(mktemp "${dst%/*}/.hostbootstrap.XXXXXX")" || die "cannot create a temp file in ${dst%/*}"
    if [ -z "${ROOT}" ]; then
        install -m "${mode}" -o root -g root "${src}" "${tmp}" || { rm -f "${tmp}"; die "cannot stage ${dst}"; }
    else
        install -m "${mode}" "${src}" "${tmp}" || { rm -f "${tmp}"; die "cannot stage ${dst}"; }
    fi
    mv -f "${tmp}" "${dst}" || { rm -f "${tmp}"; die "cannot install ${dst}"; }
}

# install_sudoers src dst: validate with visudo, then atomically place.
install_sudoers() {
    src="$1"; dst="$2"
    symlink_free "${dst}"
    if [ -f "${dst}" ] && cmp -s "${src}" "${dst}"; then
        return 0
    fi
    tmp="$(mktemp "${dst%/*}/.hostbootstrap-sudoers.XXXXXX")" || die "cannot create a temp file in ${dst%/*}"
    cp "${src}" "${tmp}"
    chmod 0440 "${tmp}"
    if ! visudo -cf "${tmp}" >/dev/null; then
        rm -f "${tmp}"
        die "generated ${dst} failed visudo; nothing installed"
    fi
    mv -f "${tmp}" "${dst}" || { rm -f "${tmp}"; die "cannot install ${dst}"; }
}

# ensure_dir dir mode: create the directory root-owned with the given mode, and
# refuse a symlink. A pre-existing directory that is not root-owned (and so
# could be swapped or written) is refused before anything is installed.
ensure_dir() {
    d="$1"; m="$2"
    symlink_free "${d}"
    if [ -z "${ROOT}" ]; then
        if [ -e "${d}" ]; then
            # test -O is supported by dash/bash (the shells these installers run
            # under) and is already used in gotham-update.sh.
            # shellcheck disable=SC3067
            [ -O "${d}" ] || die "${d} is not root-owned"
        fi
        install -d -m "${m}" -o root -g root "${d}" || die "cannot prepare ${d}"
    else
        [ -d "${d}" ] || mkdir -p "${d}" || die "cannot create ${d}"
    fi
}

# hostctl_sudoers user: the control-plane drop-in (five verbs + repair). The
# rules always name the fixed production paths, never the test root prefix.
hostctl_sudoers() {
    echo "# Allow the Gotham control plane to run its fixed host helper verbs,"
    echo "# plus the one pinned tool-installer verb used to repair the helper."
    for verb in ${HOSTCTL_VERBS}; do
        echo "$1 ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-hostctl ${verb}"
    done
    echo "$1 ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-toolctl install-tool gotham-hostctl"
}

# apply_payload: install the live helpers and the hostctl drop-in from the
# root-owned payload. Shared by `create` (right after it writes the payload)
# and the self-update wrapper.
apply_payload() {
    ensure_dir "${HELPER_DIR}" 0755
    ensure_dir "${SUDOERS_DIR}" 0750
    for f in gotham-hostctl gotham-toolctl gotham-hostctl.sudoers; do
        [ -f "${PAYLOAD_DIR}/${f}" ] || die "payload is missing ${PAYLOAD_DIR}/${f}" 2
    done
    install_file "${PAYLOAD_DIR}/gotham-hostctl" "${HOSTCTL}" 0755
    install_file "${PAYLOAD_DIR}/gotham-toolctl" "${TOOLCTL}" 0755
    install_sudoers "${PAYLOAD_DIR}/gotham-hostctl.sudoers" "${HOSTCTL_DROPIN}"
}

# create writes the payload (root-owned) from this directory's scripts, then
# applies it. cp_user is validated here and again by the caller.
create() {
    cp_user="$1"
    valid_user "${cp_user}" || die "invalid control-plane user: ${cp_user}" 2
    if [ -z "${ROOT}" ] && ! id -u "${cp_user}" >/dev/null 2>&1; then
        die "control-plane user does not exist: ${cp_user}" 2
    fi
    [ -f "${SELF_DIR}/gotham-hostctl.sh" ] || die "${SELF_DIR}/gotham-hostctl.sh is missing" 2
    [ -f "${SELF_DIR}/gotham-toolctl.sh" ] || die "${SELF_DIR}/gotham-toolctl.sh is missing" 2

    ensure_dir "${PAYLOAD_DIR}" 0755
    install_file "${SELF_DIR}/gotham-hostctl.sh" "${PAYLOAD_DIR}/gotham-hostctl" 0755
    install_file "${SELF_DIR}/gotham-toolctl.sh" "${PAYLOAD_DIR}/gotham-toolctl" 0755
    install_file "${SELF_DIR}/gotham-hostbootstrap.sh" "${PAYLOAD_DIR}/gotham-hostbootstrap.sh" 0755

    tmp="$(mktemp "${PAYLOAD_DIR}/.sudoers.XXXXXX")" || die "cannot create a payload temp file"
    hostctl_sudoers "${cp_user}" >"${tmp}"
    chmod 0644 "${tmp}"
    install_file "${tmp}" "${PAYLOAD_DIR}/gotham-hostctl.sudoers" 0644
    rm -f "${tmp}"

    apply_payload
    echo "gotham-hostbootstrap: installed the host helper for ${cp_user}"
}

apply() {
    [ -d "${PAYLOAD_DIR}" ] || die "no installed payload at ${PAYLOAD_DIR}; run install.sh first" 2
    apply_payload
    echo "gotham-hostbootstrap: re-applied the host helper payload"
}

MODE="${1:-}"
[ "$#" -ge 1 ] || die "usage: gotham-hostbootstrap create <cp-user> | apply" 2

# A root-owned operation: refuse a non-root caller unless the test root seam is
# in use (which writes only under that test root).
if [ "$(id -u)" -ne 0 ] && [ -z "${ROOT}" ]; then
    die "must run as root (try sudo)" 2
fi

case "${MODE}" in
    create)
        [ "$#" -ge 2 ] || die "usage: gotham-hostbootstrap create <cp-user>" 2
        create "$2"
        ;;
    apply)
        apply
        ;;
    *)
        die "usage: gotham-hostbootstrap create <cp-user> | apply" 2
        ;;
esac
