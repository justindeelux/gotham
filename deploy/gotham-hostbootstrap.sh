#!/bin/sh
#
# gotham-hostbootstrap: installs the fixed, root-owned host helper
# (gotham-hostctl) and host-tool installer (gotham-toolctl) with the
# verb+id-pinned sudoers drop-in for the control-plane user and, on a node that
# runs the agent, for the agent user (JUS-185).
#
# It is the single source of the host-helper payload. It runs only from a
# root-trusted source: `install.sh` (run by the operator as root, from the
# release checkout) and the root self-update wrapper `gotham-update`. The wrapper
# does not hand it a service-writable file: it fetches the payload from the
# signed release itself and verifies it root-side (see
# deploy/gotham-hostbootstrap-fetch.sh) before calling `apply-release`. The
# payload it writes under /usr/libexec/gotham/hostbootstrap is root-owned, so
# root can re-read it safely on later updates.
#
#   gotham-hostbootstrap create <cp-user> <version> [--allow-downgrade]
#                                          install payload + trust anchor + apply
#   gotham-hostbootstrap create-agent <agent-user> <version>
#                                          add the node agent's helper grant
#   gotham-hostbootstrap apply             apply the payload only
#   gotham-hostbootstrap apply-release <dir> <version>
#                                          apply a verified released payload
#   gotham-hostbootstrap version-cmp <a> <b>
#                                          print -1/0/1 (high-water-mark helper)
#
# `create` is the one-time bridge (JUS-188): besides the live helpers it installs
# the root-owned release public key, the fetch helper and the canonical update
# wrapper, so a host that predates the refresh support gains it with a single
# root command and refreshes its own helper content on every later self-update.
# It records the control-plane user in the root-owned payload so a release
# refresh can regenerate the drop-in, and it requires the installer's release
# version so it can seed the high-water mark: a bridge without a marked version
# could never accept a refresh. A re-run that would lower the mark is refused
# unless the operator passes --allow-downgrade.
#
# `create-agent` records the node agent user the same way and applies the same
# payload, but keeps the control-plane grantee: on the control-plane machine both
# users run the helper, and each keeps exactly its own rules. The agent user gets
# the five host-control verbs only, never the tool-installer repair verb.
#
# `apply-release` refuses to install a payload whose signature-verified version
# is lower than the installed one (a root-owned high-water mark) or when that
# mark is missing or malformed, so a compromised control plane cannot replay an
# older signed helper payload. The update wrapper makes the same decision in the
# already-installed helper before it runs the fetched payload (its own copy of
# this check is the second layer).
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

# Route catchable termination signals through the EXIT trap (set by the temp-file
# owners below) so a signal does not leave a temp behind; dash/bash do not run
# EXIT on an untrapped SIGTERM/SIGINT. SIGKILL cannot be trapped.
trap 'exit 1' HUP INT TERM

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
# Root-owned trust anchor + the fetch helper the update wrapper runs (JUS-188).
SIGNING_KEY="${HELPER_DIR}/gotham-signing-key.pub"
FETCH_SCRIPT="${HELPER_DIR}/gotham-hostbootstrap-fetch.sh"
WRAPPER="${HELPER_DIR}/gotham-update"
# Root-owned records in the installed payload: the users that may run the host
# helper (the control plane, and on every node its agent) so a release refresh
# can regenerate the drop-in without the users being in the release, and the
# monotonic high-water mark of the applied helper version.
CP_USER_FILE="${PAYLOAD_DIR}/cp-user"
AGENT_USER_FILE="${PAYLOAD_DIR}/agent-user"
VERSION_FILE="${PAYLOAD_DIR}/version"

# install_optional src dst mode: install a script only when this checkout ships
# it, so a caller that lacks the newer siblings still installs the helper.
install_optional() {
    [ -f "$1" ] || return 0
    install_file "$1" "$2" "$3"
}

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

# validate_dropin file: validate a rendered drop-in with visudo without touching
# any live file, so a bad drop-in aborts before the live helpers are replaced
# (no mixed old-helper/new-drop-in state).
validate_dropin() {
    vfile="$1"
    ensure_dir "${SUDOERS_DIR}" 0750
    vtmp="$(mktemp "${SUDOERS_DIR}/.hostbootstrap-check.XXXXXX")" || die "cannot create a validation temp file"
    cp "${vfile}" "${vtmp}"
    chmod 0440 "${vtmp}"
    if ! visudo -cf "${vtmp}" >/dev/null; then
        rm -f "${vtmp}"
        die "regenerated drop-in failed visudo; nothing installed" 2
    fi
    rm -f "${vtmp}"
}

# num_cmp A B: print -1, 0 or 1 comparing two unsigned decimal strings without
# relying on integer arithmetic at all (length first, then a bytewise compare of
# equal-length digit strings, so a 25-digit identifier is comparable and there is
# no overflow and no octal surprise from a leading zero).
num_cmp() {
    a="$(printf '%s' "$1" | sed 's/^0*//')"; [ -n "${a}" ] || a=0
    b="$(printf '%s' "$2" | sed 's/^0*//')"; [ -n "${b}" ] || b=0
    case "${a}" in *[!0-9]*) a=0 ;; esac
    case "${b}" in *[!0-9]*) b=0 ;; esac
    if [ "${#a}" -gt "${#b}" ]; then echo 1; return 0; fi
    if [ "${#a}" -lt "${#b}" ]; then echo -1; return 0; fi
    # Equal length and no leading zeros: the bytewise order of two digit strings
    # of the same length is their numeric order (and, unlike `test -gt`, it cannot
    # overflow). LC_ALL=C keeps it locale-independent.
    if [ "${a}" = "${b}" ]; then echo 0; return 0; fi
    first="$(printf '%s\n%s\n' "${a}" "${b}" | LC_ALL=C sort | head -n 1)"
    if [ "${first}" = "${a}" ]; then echo -1; else echo 1; fi
}

# VERSION_RE + valid_version v: a value that version_cmp can order, i.e. a
# vMAJOR.MINOR.PATCH tag with an optional -prerelease and +build metadata. A
# malformed value (a hand-typed "latest", an empty or truncated mark) must be
# rejected, not silently compared as 0.0.0 — that would reopen the "missing mark
# means anything goes" hole as "garbage mark means anything goes".
VERSION_RE='^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?(\+[0-9A-Za-z.]+)?$'
valid_version() {
    printf '%s' "$1" | grep -Eq "${VERSION_RE}"
}

# pre_cmp A B: print -1, 0 or 1 comparing two dot-separated semver pre-release
# strings (semver §11): numeric identifiers compare numerically and sort below
# alphanumeric ones, alphanumeric identifiers compare bytewise under LC_ALL=C,
# and a shorter set sorts below a longer one that extends it. This is what makes
# rc.10 > rc.9.
pre_cmp() {
    pa="$1"; pb="$2"
    while [ -n "${pa}" ] || [ -n "${pb}" ]; do
        ia="${pa%%.*}"; if [ "${pa}" = "${ia}" ]; then pa=""; else pa="${pa#*.}"; fi
        ib="${pb%%.*}"; if [ "${pb}" = "${ib}" ]; then pb=""; else pb="${pb#*.}"; fi
        if [ -z "${ia}" ] && [ -z "${ib}" ]; then break; fi
        if [ -z "${ia}" ]; then echo -1; return 0; fi
        if [ -z "${ib}" ]; then echo 1; return 0; fi
        case "${ia}" in *[!0-9]*) na=0 ;; *) na=1 ;; esac
        case "${ib}" in *[!0-9]*) nb=0 ;; *) nb=1 ;; esac
        if [ "${na}" = 1 ] && [ "${nb}" = 1 ]; then
            c="$(num_cmp "${ia}" "${ib}")"
        elif [ "${na}" = 1 ]; then
            c=-1
        elif [ "${nb}" = 1 ]; then
            c=1
        elif [ "${ia}" = "${ib}" ]; then
            c=0
        else
            first="$(printf '%s\n%s\n' "${ia}" "${ib}" | LC_ALL=C sort | head -n 1)"
            if [ "${first}" = "${ia}" ]; then c=-1; else c=1; fi
        fi
        if [ "${c}" != "0" ]; then echo "${c}"; return 0; fi
    done
    echo 0
}

# version_cmp A B: print -1, 0 or 1 comparing two Gotham release tags
# (vMAJOR.MINOR.PATCH with an optional -prerelease and an optional +build
# metadata) with semver precedence: build metadata is ignored, a release sorts
# above a pre-release of the same core, and pre-release identifiers compare
# element-wise (pre_cmp), so rc.10 > rc.9 rather than the other way round. A
# malformed argument is refused (die), never treated as 0.0.0.
version_cmp() {
    valid_version "$1" || die "not a valid release version: '$1'" 2
    valid_version "$2" || die "not a valid release version: '$2'" 2
    va="${1#v}"; va="${va%%+*}"
    vb="${2#v}"; vb="${vb%%+*}"
    va_pre=""; case "${va}" in *-*) va_pre="${va#*-}"; va="${va%%-*}" ;; esac
    vb_pre=""; case "${vb}" in *-*) vb_pre="${vb#*-}"; vb="${vb%%-*}" ;; esac
    case "${va}" in
        *.*.*) va1="${va%%.*}"; vr="${va#*.}"; va2="${vr%%.*}"; va3="${vr#*.}" ;;
        *.*)   va1="${va%%.*}"; va2="${va#*.}"; va3=0 ;;
        *)     va1="${va}"; va2=0; va3=0 ;;
    esac
    case "${vb}" in
        *.*.*) vb1="${vb%%.*}"; vr="${vb#*.}"; vb2="${vr%%.*}"; vb3="${vr#*.}" ;;
        *.*)   vb1="${vb%%.*}"; vb2="${vb#*.}"; vb3=0 ;;
        *)     vb1="${vb}"; vb2=0; vb3=0 ;;
    esac
    va2="${va2%%.*}"; va3="${va3%%.*}"; vb2="${vb2%%.*}"; vb3="${vb3%%.*}"
    c="$(num_cmp "${va1}" "${vb1}")"; [ "${c}" = "0" ] || { echo "${c}"; return 0; }
    c="$(num_cmp "${va2}" "${vb2}")"; [ "${c}" = "0" ] || { echo "${c}"; return 0; }
    c="$(num_cmp "${va3}" "${vb3}")"; [ "${c}" = "0" ] || { echo "${c}"; return 0; }
    if [ -z "${va_pre}" ] && [ -z "${vb_pre}" ]; then echo 0
    elif [ -z "${va_pre}" ]; then echo 1
    elif [ -z "${vb_pre}" ]; then echo -1
    else
        pre_cmp "${va_pre}" "${vb_pre}"
    fi
}

# payload_cp_user prints the recorded control-plane user, or the one an older
# payload granted without recording it. That fallback reads the installed
# drop-in: the pinned tool-installer repair verb is granted to the control-plane
# user only, so it names them even when an agent grantee is present too.
read_user_record() { # file: the recorded user, or empty.
    [ -f "$1" ] && [ ! -L "$1" ] || return 0
    u="$(sed -n '1p' "$1" 2>/dev/null | tr -d '\r\n')"
    valid_user "${u}" && printf '%s' "${u}"
    return 0
}

payload_cp_user() {
    u="$(read_user_record "${CP_USER_FILE}")"
    if [ -z "${u}" ] && [ -f "${PAYLOAD_DIR}/gotham-hostctl.sudoers" ]; then
        u="$(sed -n 's|^\([A-Za-z0-9_-]\{1,\}\) ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-toolctl install-tool gotham-hostctl$|\1|p' "${PAYLOAD_DIR}/gotham-hostctl.sudoers" 2>/dev/null | head -n 1)"
    fi
    [ -n "${u}" ] || return 1
    valid_user "${u}" || return 1
    printf '%s' "${u}"
}

payload_agent_user() {
    u="$(read_user_record "${AGENT_USER_FILE}")"
    [ -n "${u}" ] || return 0
    printf '%s' "${u}"
}

# write_record file value: write a root-owned record atomically (temp + rename in
# the same root-owned directory), removing the temp on any exit including a signal.
write_record() {
    rtmp="$(mktemp "${PAYLOAD_DIR}/.record.XXXXXX")" || die "cannot create a temp file in ${PAYLOAD_DIR}"
    trap 'rm -f "${rtmp}" 2>/dev/null || true' EXIT
    printf '%s\n' "$2" >"${rtmp}" || { rm -f "${rtmp}"; die "cannot write ${1}"; }
    chmod 0644 "${rtmp}"
    mv -f "${rtmp}" "$1" || { rm -f "${rtmp}"; die "cannot install ${1}"; }
    trap - EXIT
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

# sudoers_for cp_user agent_user: the host-helper drop-in. Every recorded user
# gets the five fixed host-control verbs; the control-plane user additionally
# gets the one pinned tool-installer verb used to repair the helper, so an
# agent cannot reinstall root payloads. Both may be empty (the other is then
# the only grantee). The rules always name the fixed production paths, never
# the test root prefix.
sudoers_for() {
    echo "# Allow the Gotham control plane and node agent to run its fixed host helper verbs."
    for verb in ${HOSTCTL_VERBS}; do
        [ -z "$1" ] || echo "$1 ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-hostctl ${verb}"
        [ -z "$2" ] || echo "$2 ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-hostctl ${verb}"
    done
    [ -z "$1" ] || echo "$1 ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-toolctl install-tool gotham-hostctl"
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
    # Validate the drop-in before replacing any live file, so a visudo failure
    # cannot leave new live helpers with an old (or missing) drop-in.
    validate_dropin "${PAYLOAD_DIR}/gotham-hostctl.sudoers"
    install_file "${PAYLOAD_DIR}/gotham-hostctl" "${HOSTCTL}" 0755
    install_file "${PAYLOAD_DIR}/gotham-toolctl" "${TOOLCTL}" 0755
    install_sudoers "${PAYLOAD_DIR}/gotham-hostctl.sudoers" "${HOSTCTL_DROPIN}"
}

# check_version version allow_downgrade: validate the release version and refuse
# to lower the installed high-water mark. The bridge must seed the mark: a host
# left without one could only ever refuse a refresh (a missing mark is never
# "version 0, anything goes"), so the version is required at install time. A
# malformed existing mark is repaired instead (a valid version can only improve
# it), so a broken seed is not a dead end.
check_version() {
    v="$1"
    allow_downgrade="${2:-}"
    [ -n "${v}" ] || die "the release version is required: gotham-hostbootstrap create <user> <version>" 2
    valid_version "${v}" \
        || die "invalid version: '${v}' (expected vMAJOR.MINOR.PATCH[-prerelease][+build])" 2
    if [ -f "${VERSION_FILE}" ] && [ ! -L "${VERSION_FILE}" ]; then
        prev="$(sed -n '1p' "${VERSION_FILE}" 2>/dev/null | tr -d '\r\n')"
        if [ -n "${prev}" ] && ! valid_version "${prev}"; then
            echo "gotham-hostbootstrap: replacing the malformed host-helper high-water mark '${prev}'" >&2
        elif [ -n "${prev}" ] && [ "${allow_downgrade}" != "--allow-downgrade" ] \
            && [ "$(version_cmp "${v}" "${prev}")" = "-1" ]; then
            die "would lower the host-helper version from ${prev} to ${v}; pass --allow-downgrade to force it" 2
        fi
    fi
}

# write_payload version: install the fixed payload (root-owned) from this
# directory's scripts with a drop-in for every recorded user, then apply it.
write_payload() {
    version="$1"
    # The bridge is only complete with its trust anchor and fetch helper:
    # refuse rather than silently install a helper that can never be refreshed
    # (the reviewer's "no anchor" case).
    for sibling in gotham-hostctl.sh gotham-toolctl.sh gotham-hostbootstrap.sh \
        gotham-hostbootstrap-fetch.sh gotham-update.sh gotham-signing-key.pub; do
        [ -f "${SELF_DIR}/${sibling}" ] || die "${SELF_DIR}/${sibling} is missing" 2
    done

    ensure_dir "${PAYLOAD_DIR}" 0755
    install_file "${SELF_DIR}/gotham-hostctl.sh" "${PAYLOAD_DIR}/gotham-hostctl" 0755
    install_file "${SELF_DIR}/gotham-toolctl.sh" "${PAYLOAD_DIR}/gotham-toolctl" 0755
    install_file "${SELF_DIR}/gotham-hostbootstrap.sh" "${PAYLOAD_DIR}/gotham-hostbootstrap.sh" 0755
    install_file "${SELF_DIR}/gotham-hostbootstrap-fetch.sh" "${PAYLOAD_DIR}/gotham-hostbootstrap-fetch.sh" 0755

    # A dedicated name: install_file assigns its own global `tmp`, so reusing that
    # name here would leave this temp behind on the way out.
    # payload_cp_user falls back to the installed drop-in, so a host whose older
    # install predates the cp-user record keeps its control-plane grantee.
    cp_user="$(payload_cp_user)" || cp_user=""
    agent_user="$(payload_agent_user)"
    [ -n "${cp_user}${agent_user}" ] || die "no host-helper user is recorded" 2
    dropin_tmp="$(mktemp "${PAYLOAD_DIR}/.sudoers.XXXXXX")" || die "cannot create a payload temp file"
    trap 'rm -f "${dropin_tmp}" 2>/dev/null || true' EXIT
    sudoers_for "${cp_user}" "${agent_user}" >"${dropin_tmp}"
    chmod 0644 "${dropin_tmp}"
    install_file "${dropin_tmp}" "${PAYLOAD_DIR}/gotham-hostctl.sudoers" 0644
    rm -f "${dropin_tmp}" 2>/dev/null || true
    trap - EXIT

    # Seed the high-water mark from the installer's release version, so the very
    # first self-update cannot be steered below what the installer installed.
    write_record "${VERSION_FILE}" "${version}"

    apply_payload
    # One-time bridge (JUS-188): install the root-owned release public key, the
    # fetch helper and the canonical update wrapper next to the helper, so the
    # wrapper can verify and apply released payloads on every later self-update.
    install_file "${SELF_DIR}/gotham-hostbootstrap-fetch.sh" "${FETCH_SCRIPT}" 0755
    install_file "${SELF_DIR}/gotham-update.sh" "${WRAPPER}" 0755
    install_file "${SELF_DIR}/gotham-signing-key.pub" "${SIGNING_KEY}" 0644
}

# create adds the control-plane user to the payload and applies it. cp_user is
# validated here and again by the caller.
create() {
    cp_user="$1"
    cp_version="${2:-}"
    allow_downgrade="${3:-}"
    valid_user "${cp_user}" || die "invalid control-plane user: ${cp_user}" 2
    check_version "${cp_version}" "${allow_downgrade}"
    if [ -z "${ROOT}" ] && ! id -u "${cp_user}" >/dev/null 2>&1; then
        die "control-plane user does not exist: ${cp_user}" 2
    fi
    # Record the user (root-owned) so a release refresh can regenerate the
    # drop-in without the user being in the release.
    ensure_dir "${PAYLOAD_DIR}" 0755
    write_record "${CP_USER_FILE}" "${cp_user}"
    write_payload "${cp_version}"
    echo "gotham-hostbootstrap: installed the host helper for ${cp_user}"
}

# create-agent adds the node agent user to the payload and applies it. It keeps
# the control-plane grantee of the same host: on the control-plane machine both
# users exist, and neither may silently replace the other's drop-in.
create_agent() {
    agent_user="$1"
    agent_version="${2:-}"
    valid_user "${agent_user}" || die "invalid agent user: ${agent_user}" 2
    check_version "${agent_version}"
    if [ -z "${ROOT}" ] && ! id -u "${agent_user}" >/dev/null 2>&1; then
        die "agent user does not exist: ${agent_user}" 2
    fi
    ensure_dir "${PAYLOAD_DIR}" 0755
    write_record "${AGENT_USER_FILE}" "${agent_user}"
    write_payload "${agent_version}"
    echo "gotham-hostbootstrap: installed the host helper for ${agent_user}"
}

apply() {
    [ -d "${PAYLOAD_DIR}" ] || die "no installed payload at ${PAYLOAD_DIR}; run install.sh first" 2
    apply_payload
    echo "gotham-hostbootstrap: re-applied the host helper payload"
}

# apply_release src version: install a payload the root update wrapper fetched
# from the signed release and verified root-side (JUS-188). src is a root-only
# staging directory, so these bytes are already authenticated; version is the
# signature-verified release version it belongs to. The rendered drop-in is not
# shipped in the release (it names the control-plane user), so it is regenerated
# from the recorded user by the released hostbootstrap and re-validated with
# visudo on every refresh. A version lower than the installed high-water mark is
# refused (anti-rollback) and so is a missing mark (a host with no mark can only
# be one that predates the seeded bridge, and treating it as "version 0" would
# accept any replayable signed payload); an equal version is an idempotent
# re-apply. This check is the second layer: the wrapper makes the same decision
# in already-installed code before it runs this script.
apply_release() {
    # rel_src, not src: install_file assigns its own global src/dst/mode.
    rel_src="$1"
    rel_version="${2:-}"
    [ -d "${rel_src}" ] || die "payload source ${rel_src} is not a directory" 2
    [ -n "${rel_version}" ] || die "apply-release needs the signature-verified version" 2
    valid_version "${rel_version}" || die "not a valid release version: '${rel_version}'" 2
    for f in gotham-hostctl gotham-toolctl; do
        [ -f "${rel_src}/${f}" ] || die "payload source is missing ${f}" 2
    done
    if [ -L "${rel_src}/gotham-hostctl" ] || [ -L "${rel_src}/gotham-toolctl" ]; then
        die "refusing a symlinked payload source in ${rel_src}" 2
    fi
    ensure_dir "${PAYLOAD_DIR}" 0755
    ensure_dir "${SUDOERS_DIR}" 0750

    cp_user="$(payload_cp_user)" || cp_user=""
    agent_user="$(payload_agent_user)"
    [ -n "${cp_user}${agent_user}" ] \
        || die "no host-helper user recorded in ${PAYLOAD_DIR}; run create or create-agent first" 2

    # Anti-rollback: refuse a version below the installed high-water mark, and
    # refuse when the mark is missing or not a regular root-owned record.
    hwm=""
    if [ -f "${VERSION_FILE}" ] && [ ! -L "${VERSION_FILE}" ]; then
        hwm="$(sed -n '1p' "${VERSION_FILE}" 2>/dev/null | tr -d '\r\n')"
    fi
    if [ -z "${hwm}" ]; then
        die "no installed host-helper high-water mark at ${VERSION_FILE}; refusing the release (run create with the release version)" 2
    fi
    if ! valid_version "${hwm}"; then
        die "installed host-helper high-water mark '${hwm}' is not a valid release version; refusing the release (run create with the release version)" 2
    fi
    case "$(version_cmp "${rel_version}" "${hwm}")" in
        -1) die "refusing to downgrade the host helper: ${rel_version} is older than the installed ${hwm}" 2 ;;
        0 | 1) ;;
        *) die "cannot compare ${rel_version} with the installed ${hwm}" 2 ;;
    esac

    # Regenerate the drop-in for the recorded users from this (released)
    # hostbootstrap and validate it BEFORE any file is written, so a bad release
    # cannot leave a mixed state. The temp is removed on any exit, so a visudo
    # failure cannot leak it into the root-owned payload directory.
    dropin_tmp="$(mktemp "${PAYLOAD_DIR}/.sudoers.XXXXXX")" || die "cannot create a payload temp file"
    trap 'rm -f "${dropin_tmp}" 2>/dev/null || true' EXIT
    sudoers_for "${cp_user}" "${agent_user}" >"${dropin_tmp}"
    chmod 0644 "${dropin_tmp}"
    validate_dropin "${dropin_tmp}"

    install_file "${rel_src}/gotham-hostctl" "${PAYLOAD_DIR}/gotham-hostctl" 0755
    install_file "${rel_src}/gotham-toolctl" "${PAYLOAD_DIR}/gotham-toolctl" 0755
    install_optional "${rel_src}/gotham-hostbootstrap.sh" "${PAYLOAD_DIR}/gotham-hostbootstrap.sh" 0755
    install_optional "${rel_src}/gotham-hostbootstrap-fetch.sh" "${PAYLOAD_DIR}/gotham-hostbootstrap-fetch.sh" 0755
    install_file "${dropin_tmp}" "${PAYLOAD_DIR}/gotham-hostctl.sudoers" 0644
    rm -f "${dropin_tmp}" 2>/dev/null || true
    trap - EXIT

    apply_payload
    install_optional "${rel_src}/gotham-hostbootstrap-fetch.sh" "${FETCH_SCRIPT}" 0755
    # Refresh the wrapper too, so future updates use the released logic.
    install_optional "${rel_src}/gotham-update.sh" "${WRAPPER}" 0755
    # Record the applied version (>= the high-water mark) atomically.
    write_record "${VERSION_FILE}" "${rel_version}"
    echo "gotham-hostbootstrap: applied the released host-helper payload ${rel_version}"
}

MODE="${1:-}"
[ "$#" -ge 1 ] || die "usage: gotham-hostbootstrap create <cp-user> <version> | create-agent <agent-user> <version> | apply | apply-release <dir> <version>" 2

# A root-owned operation: refuse a non-root caller unless the test root seam is
# in use (which writes only under that test root). version-cmp is a pure
# read-only string comparison (the high-water-mark helper and its tests) and
# touches no file, so it is exempt.
if [ "${MODE}" != "version-cmp" ] && [ "$(id -u)" -ne 0 ] && [ -z "${ROOT}" ]; then
    die "must run as root (try sudo)" 2
fi

case "${MODE}" in
    create)
        [ "$#" -ge 3 ] || die "usage: gotham-hostbootstrap create <cp-user> <version> [--allow-downgrade]" 2
        create "$2" "$3" "${4:-}"
        ;;
    create-agent)
        [ "$#" -ge 3 ] || die "usage: gotham-hostbootstrap create-agent <agent-user> <version>" 2
        create_agent "$2" "$3"
        ;;
    apply)
        apply
        ;;
    apply-release)
        [ "$#" -ge 3 ] || die "usage: gotham-hostbootstrap apply-release <dir> <version>" 2
        apply_release "$2" "$3"
        ;;
    version-cmp)
        [ "$#" -ge 3 ] || die "usage: gotham-hostbootstrap version-cmp <a> <b>" 2
        version_cmp "$2" "$3"
        ;;
    *)
        die "usage: gotham-hostbootstrap create <cp-user> <version> | apply | apply-release <dir> <version>" 2
        ;;
esac
