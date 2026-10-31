#!/bin/sh
#
# gotham-hostctl: privileged host settings helper for the Gotham control plane.
#
# Installed root-owned at /usr/libexec/gotham/gotham-hostctl and reached only
# through the sudoers drop-in written by gotham-hostbootstrap.sh, which grants
# the gotham service user exactly these verbs (one rule each, no wildcards):
#
#   status | apply-network | confirm-network | revert-network | apply-system |
#   cleanup-status | cleanup-journal | cleanup-package-cache |
#   apply-ssh | confirm-ssh | revert-ssh | apply-agent-root
#
# Values arrive on stdin as key=value lines. Only whitelisted keys are accepted
# and every value is re-validated here with strict anchored patterns before it
# is written to a file or passed as an argument: the control plane is treated
# as untrusted input. Nothing is ever passed to a shell for evaluation.
#
# Supported stack: systemd-networkd + systemd-resolved (network) and
# systemd-timesyncd + hostnamectl + timedatectl (system, including the host
# timezone). Gotham only ever owns these files:
#   /etc/systemd/network/05-gotham.network
#   /etc/systemd/resolved.conf.d/05-gotham.conf
#   /etc/systemd/timesyncd.conf.d/05-gotham.conf
#   /etc/systemd/network/<winning>.network.d/05-gotham.conf (DNS-only link
#     override; a [Network] section with DNS= only, never Address/Gateway)
#
# `status` also reports the default-route interface as the host sees it
# (`host_network`: live addresses/gateways from `ip`, the address modes from
# the winning .network file, effective DNS from resolvectl), plus the live
# hostname, NTP state and timezone, so callers can seed forms with the host
# truth instead of stored defaults. A DNS-only
# apply (`dns_only=true`) writes only the resolved drop-in plus a DNS-only
# networkd drop-in on the winning .network file and sets the link DNS at
# runtime, and never touches the interface file, so a resolver change cannot
# drop a static address and is not shadowed by per-link DNS (JUS-101).
#
# Network changes are tentative: apply-network snapshots the owned files, applies
# the change and arms a systemd timer that runs "revert-network" after
# revert_after seconds. confirm-network cancels the timer. When the operator can
# no longer reach the control plane nobody confirms, so the host restores itself.
#
# Under sudo every GOTHAM_HOSTCTL_* override is ignored; GOTHAM_HOSTCTL_ROOT and
# GOTHAM_HOSTCTL_DRYRUN exist for tests and are honoured only without sudo.
#
# Sandbox: the control-plane and agent units run with ProtectSystem=full, so
# /etc is read-only in their mount namespace and a `sudo` child inherits that.
# The write verbs therefore re-execute themselves in a fresh transient unit
# through systemd-run (as root, outside the sandbox), like gotham-toolctl. The
# inner run is marked with GOTHAM_HOSTCTL_INNER=1 so it does not re-exec again.
set -u
umask 022

SELF="/usr/libexec/gotham/gotham-hostctl"
SELF_RUN="$0"
# Bound the transient unit just above the 60s request timeout of both callers:
# --wait does not stop the unit if the caller is killed.
RUNTIME_MAX_SEC="120"
ROOT=""
DRYRUN="0"
INNER="${GOTHAM_HOSTCTL_INNER:-0}"
SYSTEMD_RUN="systemd-run"
if [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
    ROOT="${GOTHAM_HOSTCTL_ROOT:-}"
    DRYRUN="${GOTHAM_HOSTCTL_DRYRUN:-0}"
    SYSTEMD_RUN="${GOTHAM_HOSTCTL_SYSTEMD_RUN:-systemd-run}"
fi

NETD="${ROOT}/etc/systemd/network"
RUNDIR="${ROOT}/run/systemd/network"
RESD="${ROOT}/etc/systemd/resolved.conf.d"
NTPD="${ROOT}/etc/systemd/timesyncd.conf.d"
RESOLV_CONF="${ROOT}/etc/resolv.conf"
STATE="${ROOT}/var/lib/gotham-hostctl"
NETFILE="${NETD}/05-gotham.network"
RESFILE="${RESD}/05-gotham.conf"
NTPFILE="${NTPD}/05-gotham.conf"
PENDING="${STATE}/pending"
BACKUP="${STATE}/backup"
TIMER="gotham-hostctl-revert"
SSHD_DIR="${ROOT}/etc/ssh/sshd_config.d"
SSHD_DROPIN="${SSHD_DIR}/05-gotham.conf"
SSH_PENDING="${STATE}/ssh-pending"
SSH_BACKUP="${STATE}/ssh-backup"
SSH_TIMER="gotham-hostctl-ssh-revert"
AGENT_DROPIN_DIR="${ROOT}/etc/systemd/system/gotham-agent.service.d"
AGENT_DROPIN="${AGENT_DROPIN_DIR}/file-manager.conf"
AGENT_ENV="${ROOT}/etc/gotham/agent.env"
AGENT_STATE="${ROOT}/var/lib/gotham-agent"

die() { echo "gotham-hostctl: $*" >&2; exit "${2:-1}"; }

# run executes a host command; in dry-run it only records it.
run() {
    if [ "${DRYRUN}" = "1" ]; then echo "+ $*"; return 0; fi
    "$@"
}

[ "$#" -eq 1 ] || die "exactly one verb is required" 2
VERB="$1"

# ---- re-exec outside the service's systemd sandbox -------------------------
# Root's systemd-run creates a transient unit with a clean namespace (no
# ProtectSystem), which is what lets the helper write its drop-ins under /etc.
# Skip the read-only verbs and any invocation that already runs inside the
# transient unit. stdin, stdout and the exit code pass through --pipe --wait.
case "${VERB}" in
    apply-network | revert-network | apply-system | cleanup-journal | cleanup-package-cache | \
    apply-ssh | confirm-ssh | revert-ssh | apply-agent-root) WRITE=1 ;;
    *) WRITE=0 ;;
esac
case "${SELF_RUN}" in /*) SELF_OK=1 ;; *) SELF_OK=0 ;; esac
if [ "${DRYRUN}" != "1" ] && [ "${INNER}" != "1" ] && [ "${SELF_OK}" = "1" ] \
    && [ "${WRITE}" = "1" ] && command -v "${SYSTEMD_RUN}" >/dev/null 2>&1; then
    UNIT="gotham-hostctl-${VERB}-$$"
    # shortcut: this trap can be deferred during systemd-run; use an
    # interruptible background wait when prompt graceful cancellation lands.
    trap 'systemctl stop "$UNIT" >/dev/null 2>&1 || true; exit 130' TERM INT HUP
    "${SYSTEMD_RUN}" --quiet --pipe --wait --collect \
        --unit="${UNIT}" \
        --property="RuntimeMaxSec=${RUNTIME_MAX_SEC}" \
        --setenv=GOTHAM_HOSTCTL_INNER=1 \
        "${SELF_RUN}" "$@"
    exit $?
fi

# ---- validation ------------------------------------------------------------
OCT='(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])'
V4="${OCT}(\.${OCT}){3}"
is_v4()      { printf '%s\n' "$1" | grep -Eq "^${V4}$"; }
is_v4cidr()  { printf '%s\n' "$1" | grep -Eq "^${V4}/([1-9]|[12][0-9]|3[0-2])$"; }
is_v6()      { printf '%s\n' "$1" | grep -Eq '^[0-9A-Fa-f:]{2,39}$' && case "$1" in *:*) true ;; *) false ;; esac; }
is_v6cidr()  { printf '%s\n' "$1" | grep -Eq '^[0-9A-Fa-f:]{2,39}/([1-9]|[1-9][0-9]|1[01][0-9]|12[0-8])$' && case "$1" in *:*) true ;; *) false ;; esac; }
is_ip()      { is_v4 "$1" || is_v6 "$1"; }
is_host()    { printf '%s\n' "$1" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$'; }
is_bool()    { [ "$1" = "true" ] || [ "$1" = "false" ]; }
is_mode()    { [ "$1" = "dhcp" ] || [ "$1" = "static" ]; }
all_ips()    { # comma separated list of 0..max IPs
    list="$1"; max="$2"; n=0
    [ -z "${list}" ] && return 0
    oldifs="${IFS}"; IFS=','
    for item in ${list}; do
        IFS="${oldifs}"
        is_ip "${item}" || return 1
        n=$((n + 1))
        IFS=','
    done
    IFS="${oldifs}"
    [ "${n}" -le "${max}" ]
}
all_hosts()  {
    list="$1"; max="$2"; n=0
    [ -z "${list}" ] && return 0
    oldifs="${IFS}"; IFS=','
    for item in ${list}; do
        IFS="${oldifs}"
        { is_host "${item}" || is_ip "${item}"; } || return 1
        n=$((n + 1))
        IFS=','
    done
    IFS="${oldifs}"
    [ "${n}" -le "${max}" ]
}
# is_tz: an IANA zone name tzdata actually ships. The anchored pattern rejects
# whitespace, shell metacharacters and any "." (so no ".." traversal) and the
# zoneinfo file check rejects a well-formed name that is not a real zone.
TZ_RE='^[A-Za-z][A-Za-z0-9_+-]{0,63}(/[A-Za-z0-9_+-]{1,64}){0,4}$'
is_tz() {
    printf '%s\n' "$1" | grep -Eq "${TZ_RE}" || return 1
    [ -f "${ROOT}/usr/share/zoneinfo/$1" ]
}

# read_kv reads stdin into K_<key> variables for the keys in $1 (space list).
read_kv() {
    allowed=" $1 "
    while IFS= read -r line || [ -n "${line}" ]; do
        [ -z "${line}" ] && continue
        case "${line}" in *=*) ;; *) die "malformed line" 2 ;; esac
        key="${line%%=*}"; val="${line#*=}"
        case "${allowed}" in *" ${key} "*) ;; *) die "unknown key: ${key}" 2 ;; esac
        printf '%s' "${val}" | grep -q '[^[:print:]]' && die "invalid characters in ${key}" 2
        eval "K_${key}=\${val}"
    done
}

# ---- capability probe ------------------------------------------------------
active() { [ "${DRYRUN}" = "1" ] && return 0; systemctl is-active --quiet "$1" 2>/dev/null; }
net_ok() { [ "${DRYRUN}" = "1" ] && return 0; active systemd-networkd && active systemd-resolved && command -v networkctl >/dev/null 2>&1; }
sys_ok() { command -v hostnamectl >/dev/null 2>&1 || [ "${DRYRUN}" = "1" ]; }
default_iface() {
    if [ -n "${GOTHAM_HOSTCTL_IFACE:-}" ] && [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
        echo "${GOTHAM_HOSTCTL_IFACE}"; return
    fi
    ip -o route show default 2>/dev/null | sed -n 's/.* dev \([A-Za-z0-9_.:-]*\).*/\1/p' | head -n 1
}

# ---- cleanup probes --------------------------------------------------------
# The two root-only cleanup categories this helper owns: the systemd journal
# and the distribution package cache. `cleanup-status` only reads; the two
# write verbs re-exec out of the service sandbox like the other root writes.

# journal_supported: a real journald owns the journal, not just journalctl.
journal_supported() {
    command -v journalctl >/dev/null 2>&1 && active systemd-journald
}

# journal_bytes: current journal size from journalctl's human output, or from
# du when a host has no readable journal. Empty when neither can tell.
journal_bytes() {
    if journal_supported; then
        out="$(LC_ALL=C journalctl --disk-usage 2>/dev/null)" || out=""
        bytes="$(printf '%s\n' "${out}" | awk '
            {
                for (i = 1; i <= NF; i++) {
                    t = $i
                    if (substr(t, length(t), 1) == "B") t = substr(t, 1, length(t) - 1)
                    if (t ~ /^[0-9]+(\.[0-9]+)?$/) { printf "%.0f", t; exit }
                    if (t ~ /^[0-9]+(\.[0-9]+)?[KMGT]$/) {
                        n = substr(t, 1, length(t) - 1); u = substr(t, length(t), 1)
                        m = (u == "K") ? 1024 : (u == "M") ? 1048576 : (u == "G") ? 1073741824 : 1099511627776
                        printf "%.0f", n * m; exit
                    }
                }
            }')"
        [ -n "${bytes}" ] && { printf '%s' "${bytes}"; return 0; }
    fi
    du -sb "${ROOT}/var/log/journal" 2>/dev/null | awk '{ print $1; exit }'
}

# apt_cache_bytes: only the .deb files and the partial download directory,
# which is exactly what `apt-get clean` removes.
apt_cache_bytes() {
    dir="${ROOT}/var/cache/apt/archives"
    [ -d "${dir}" ] || { printf '0'; return 0; }
    total=0
    for path in "${dir}"/*.deb "${dir}/partial"; do
        [ -e "${path}" ] || continue
        bytes="$(du -sb "${path}" 2>/dev/null | awk '{ print $1; exit }')"
        case "${bytes}" in "" | *[!0-9]*) bytes=0 ;; esac
        total=$((total + bytes))
    done
    printf '%s' "${total}"
}

dnf_cache_bytes() {
    du -sb "${ROOT}/var/cache/dnf" 2>/dev/null | awk '{ print $1; exit }'
}

# pkg_manager: apt or dnf, only when the tool and its cache directory exist.
pkg_manager() {
    if command -v apt-get >/dev/null 2>&1 && [ -d "${ROOT}/var/cache/apt" ]; then
        printf 'apt'
    elif command -v dnf >/dev/null 2>&1 && [ -d "${ROOT}/var/cache/dnf" ]; then
        printf 'dnf'
    fi
}

size_or_zero() {
    case "${1:-}" in "" | *[!0-9]*) printf '0' ;; *) printf '%s' "$1" ;; esac
}

mkdir -p "${STATE}" 2>/dev/null
chmod 0755 "${STATE}" 2>/dev/null

# ---- live host network probe (status) --------------------------------------
# host_network_json reports the default-route interface as the host sees it:
# live addresses/gateways from `ip`, the modes from the winning .network
# file (/etc shadows /run, lexical first match wins), DNS from resolvectl
# falling back to the owned drop-in and resolv.conf. Every value is
# re-validated before emission; unknown parts become "" and a missing
# interface becomes null. Tests stub `ip`/`resolvectl` on PATH and plant
# fixtures under GOTHAM_HOSTCTL_ROOT.
file_matches() { # file iface: a .network file without Name= matches all.
    names="$(sed -n 's/^[[:space:]]*Name[[:space:]]*=[[:space:]]*//p' "$1" 2>/dev/null)"
    [ -z "${names}" ] && return 0
    for pat in ${names}; do case "$2" in ${pat}) return 0 ;; esac; done
    return 1
}

winning_file() { # iface: first matching .network file (/etc first), or "".
    for d in "${NETD}" "${RUNDIR}"; do
        for f in "${d}"/*.network; do
            [ -e "${f}" ] || continue
            file_matches "${f}" "$1" && { echo "${f}"; return; }
        done
    done
}

dhcp_enabled_in() { # winning file: DHCP enabled (anything but ""/no/false).
    val="$(sed -n 's/^[[:space:]]*DHCP[[:space:]]*=[[:space:]]*//p' "$1" 2>/dev/null | head -n 1 | tr 'A-Z' 'a-z')"
    case "${val}" in ""|no|false) return 1 ;; *) return 0 ;; esac
}

cur_addr4() {
    v="$(ip -o -4 addr show dev "$1" scope global 2>/dev/null | awk '{print $4; exit}')"
    is_v4cidr "${v}" && printf '%s' "${v}"
    return 0
}

cur_gw4() {
    v="$(ip -o -4 route show default dev "$1" 2>/dev/null | sed -n 's/.* via \([0-9.][0-9.]*\).*/\1/p' | head -n 1)"
    is_v4 "${v}" && printf '%s' "${v}"
    return 0
}

cur_addr6() {
    v="$(ip -o -6 addr show dev "$1" scope global 2>/dev/null | awk '{print $4; exit}')"
    is_v6cidr "${v}" && printf '%s' "${v}"
    return 0
}

cur_gw6() {
    v="$(ip -o -6 route show default dev "$1" 2>/dev/null | sed -n 's/.* via \([^ ][^ ]*\).*/\1/p' | head -n 1)"
    is_v6 "${v}" && printf '%s' "${v}"
    return 0
}

collect_ips() { # candidates... : validated IPs joined with commas.
    out=""
    for tok in "$@"; do
        is_ip "${tok}" && out="${out:+${out},}${tok}"
    done
    printf '%s' "${out}"
}

cur_dns() { # iface: effective resolvers (at most 3, comma separated).
    # Link DNS first: systemd-resolved prefers per-link DNS over the global
    # drop-in, so the link list is what the host really uses (JUS-101).
    dns="$(link_dns "$1")"
    if [ -z "${dns}" ] && [ -e "${RESFILE}" ]; then
        dns="$(collect_ips $(sed -n 's/^[[:space:]]*DNS[[:space:]]*=[[:space:]]*//p' "${RESFILE}" 2>/dev/null | head -n 1))"
    fi
    if [ -z "${dns}" ] && [ -e "${RESOLV_CONF}" ]; then
        dns="$(collect_ips $(awk '$1=="nameserver"{print $2}' "${RESOLV_CONF}" 2>/dev/null))"
    fi
    dns="$(printf '%s' "${dns}" | cut -d, -f1-3)"
    all_ips "${dns}" 3 && printf '%s' "${dns}"
    return 0
}

link_dns() { # iface: link DNS from resolvectl only (no file fallback).
    dns="$(collect_ips $(resolvectl --no-pager dns "$1" 2>/dev/null | sed -n 's/^[^:]*:[[:space:]]*//p' | head -n 1))"
    printf '%s' "${dns}" | cut -d, -f1-3
    return 0
}

jstr() { # emit "s" or null; callers pass validated values only.
    case "$1" in ""|*\"*|*\\*) printf 'null' ;; *) printf '"%s"' "$1" ;; esac
}

jstrs() { # comma list -> "a", "b" (validated items only).
    out=""; oldifs="${IFS}"; IFS=','
    for item in $1; do
        IFS="${oldifs}"
        case "${item}" in *\"*|*\\*) ;; *) out="${out:+${out},}\"${item}\"" ;; esac
        IFS=','
    done
    IFS="${oldifs}"
    printf '%s' "${out}"
}

host_network_json() {
    iface="$(default_iface)"
    printf '%s' "${iface}" | grep -Eq '^[A-Za-z0-9_.:-]{1,15}$' || { printf 'null'; return; }
    a4="$(cur_addr4 "${iface}")"; g4="$(cur_gw4 "${iface}")"
    w="$(winning_file "${iface}")"
    m4="dhcp"
    if [ -n "${a4}" ] && { [ -z "${w}" ] || ! dhcp_enabled_in "${w}"; }; then m4="static"; fi
    [ "${m4}" = "static" ] || { a4=""; g4=""; }
    a6="$(cur_addr6 "${iface}")"; g6="$(cur_gw6 "${iface}")"
    e6="false"; m6="dhcp"
    if [ -n "${a6}" ]; then
        e6="true"
        if [ -n "${w}" ] && grep -Eq '^[[:space:]]*IPv6AcceptRA[[:space:]]*=[[:space:]]*no' "${w}" 2>/dev/null \
            && grep -Eq '^[[:space:]]*Address[[:space:]]*=.*:' "${w}" 2>/dev/null; then m6="static"; fi
    else
        a6=""; g6=""
    fi
    [ "${m6}" = "static" ] || { a6=""; g6=""; }
    dns="$(cur_dns "${iface}")"
    printf '{"interface":%s,"dns_servers":[%s],"ipv4":{"mode":%s,"address":%s,"gateway":%s},"ipv6":{"enabled":%s,"mode":%s,"address":%s,"gateway":%s}}' \
        "$(jstr "${iface}")" "$(jstrs "${dns}")" "$(jstr "${m4}")" "$(jstr "${a4}")" "$(jstr "${g4}")" \
        "${e6}" "$(jstr "${m6}")" "$(jstr "${a6}")" "$(jstr "${g6}")"
}

host_name() {
    v="$(hostnamectl hostname 2>/dev/null)" || v=""
    [ -n "${v}" ] || v="$(hostname 2>/dev/null)"
    is_host "${v}" && printf '%s' "${v}"
    return 0
}

ntp_state() {
    v="$(timedatectl show --property=NTP --value 2>/dev/null | tr 'A-Z' 'a-z')"
    case "${v}" in
        true | yes) printf 'true' ;;
        *) printf 'false' ;;
    esac
}

ntp_list() { # comma list: Gotham's own drop-in, else the synced server.
    if [ -f "${NTPFILE}" ]; then
        v="$(sed -n 's/^[[:space:]]*NTP[[:space:]]*=[[:space:]]*//p' "${NTPFILE}" 2>/dev/null | head -n 1 | tr ' ' ',')"
        all_hosts "${v}" 4 && printf '%s' "${v}"
        return 0
    fi
    v="$(timedatectl show-timesync --property=ServerName --value 2>/dev/null | head -n 1)"
    { is_host "${v}" || is_ip "${v}"; } && printf '%s' "${v}"
    return 0
}

current_tz() {
    v="$(timedatectl show --property=Timezone --value 2>/dev/null)"
    is_tz "${v}" && printf '%s' "${v}"
    return 0
}

# ---- SSH settings ----------------------------------------------------------
# A port change is tentative: the new drop-in lists the old AND the new port,
# so the operator keeps a way in while the control plane verifies the new one.
# confirm-ssh keeps only the new port; revert-ssh restores the snapshot. A
# revert timer restores the old port even when the control plane never answers.
ssh_available() { command -v sshd >/dev/null 2>&1; }

ssh_effective() { sshd -T 2>/dev/null; }

ssh_live_port() {
    ssh_effective | awk 'tolower($1) == "port" { p = $2 } END { if (p != "") print p }'
}

ssh_password_auth() {
    ssh_effective | awk 'tolower($1) == "passwordauthentication" { v = tolower($2) } END { if (v != "") print v }'
}

ssh_pubkey_enabled() {
    [ "$(ssh_effective | awk 'tolower($1) == "pubkeyauthentication" { print tolower($2); exit }')" != "no" ]
}

# ssh_key_auth_available: at least one authorized key exists for root or a
# home-directory user. Password login is only switched off when this is true.
ssh_key_auth_available() {
    ssh_pubkey_enabled || return 1
    for f in "${ROOT}/root/.ssh/authorized_keys" "${ROOT}"/home/*/.ssh/authorized_keys; do
        [ -f "${f}" ] || continue
        grep -Eq '^[[:space:]]*(ssh-|ecdsa-sha2-|sk-)[A-Za-z0-9@._-]+[[:space:]]' "${f}" && return 0
        grep -Eq '^[[:space:]]*[^#[:space:]].*[[:space:]](ssh-|ecdsa-sha2-|sk-)[A-Za-z0-9@._-]+[[:space:]]' "${f}" && return 0
    done
    return 1
}

# sshd_configure writes the owned drop-in: one or more Port lines plus the
# PasswordAuthentication value.
sshd_configure() {
    mkdir -p "${SSHD_DIR}" || die "cannot prepare ${SSHD_DIR}"
    case "$2" in
        true) password=yes ;;
        false) password=no ;;
        *) password="$2" ;;
    esac
    tmp="${SSHD_DROPIN}.new.$$"
    {
        echo "# Managed by Gotham (gotham-hostctl). Manual edits are overwritten."
        for p in $1; do echo "Port ${p}"; done
        echo "PasswordAuthentication ${password}"
    } >"${tmp}" || die "cannot write the SSH configuration"
    chmod 0644 "${tmp}" 2>/dev/null || true
    mv -f "${tmp}" "${SSHD_DROPIN}" || die "cannot install the SSH configuration"
}

sshd_validate() {
    [ "${DRYRUN}" = "1" ] && { echo "+ sshd -t"; return 0; }
    sshd -t 2>/dev/null
}

ssh_reload() {
    [ "${DRYRUN}" = "1" ] && { echo "+ systemctl reload ssh"; return 0; }
    if systemctl is-active --quiet ssh 2>/dev/null; then
        systemctl reload ssh 2>/dev/null || systemctl restart ssh
        return $?
    fi
    if systemctl is-active --quiet sshd 2>/dev/null; then
        systemctl reload sshd 2>/dev/null || systemctl restart sshd
        return $?
    fi
    return 1
}

# firewall_open opens one TCP port in the active firewall and prints the
# firewall it used ("none" when neither ufw nor firewalld is active).
firewall_open() {
    [ "${DRYRUN}" = "1" ] && { echo none; return 0; }
    if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
        ufw allow "$1/tcp" >/dev/null 2>&1 && { echo ufw; return 0; }
        return 1
    fi
    if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld 2>/dev/null; then
        firewall-cmd --permanent --add-port="$1/tcp" >/dev/null 2>&1 \
            && firewall-cmd --reload >/dev/null 2>&1 && { echo firewalld; return 0; }
        return 1
    fi
    echo none
}

firewall_close() {
    [ "${DRYRUN}" = "1" ] && { echo "+ close firewall port $1"; return 0; }
    case "$2" in
        ufw) command -v ufw >/dev/null 2>&1 && ufw delete allow "$1/tcp" >/dev/null 2>&1 ;;
        firewalld) command -v firewall-cmd >/dev/null 2>&1 && {
            firewall-cmd --permanent --remove-port="$1/tcp" >/dev/null 2>&1
            firewall-cmd --reload >/dev/null 2>&1
        } ;;
    esac
    return 0
}

ssh_snapshot() {
    rm -rf "${SSH_BACKUP}" || die "cannot reset the SSH backup"
    mkdir -p "${SSH_BACKUP}" || die "cannot prepare the SSH backup"
    if [ -e "${SSHD_DROPIN}" ]; then
        cp -p "${SSHD_DROPIN}" "${SSH_BACKUP}/dropin" || die "cannot back up the SSH configuration"
    else
        : >"${SSH_BACKUP}/dropin.absent"
    fi
}

ssh_restore() {
    if [ -e "${SSH_BACKUP}/dropin" ]; then
        cp -p "${SSH_BACKUP}/dropin" "${SSHD_DROPIN}"
    elif [ -e "${SSH_BACKUP}/dropin.absent" ]; then
        rm -f "${SSHD_DROPIN}"
    fi
}

pending_val() { sed -n "s/^$1=//p" "${SSH_PENDING}" 2>/dev/null | head -n 1; }

ssh_stop_revert() { systemctl stop "${SSH_TIMER}.timer" >/dev/null 2>&1 || true; }

# ---- agent root mode -------------------------------------------------------
# The same drop-in install-agent.sh writes for the file manager. The live
# report reads the MainPID owner, so a written drop-in alone does not count
# until the restarted agent actually runs as root.
agent_root_live() {
    pid="$(systemctl show -p MainPID --value gotham-agent 2>/dev/null)"
    case "${pid}" in "" | 0 | *[!0-9]*) return 1 ;; esac
    user="$(ps -o user= -p "${pid}" 2>/dev/null | tr -d '[:space:]')"
    [ "${user}" = "root" ]
}

agent_env_set_files() {
    [ -f "${AGENT_ENV}" ] || die "the agent configuration ${AGENT_ENV} is missing" 3
    tmp="${AGENT_ENV}.new.$$"
    awk -v value="$1" '
        /^[[:space:]]*GOTHAM_AGENT_FILES=/ { next }
        { print }
        END { print "GOTHAM_AGENT_FILES=" value }
    ' "${AGENT_ENV}" >"${tmp}" || die "cannot stage the agent configuration"
    chmod 0640 "${tmp}" 2>/dev/null || true
    if [ -z "${ROOT}" ]; then chown root:gotham-agent "${tmp}" 2>/dev/null || true; fi
    mv -f "${tmp}" "${AGENT_ENV}" || die "cannot update the agent configuration"
}

agent_files_current() {
    _files_value="$(sed -n 's/^[[:space:]]*GOTHAM_AGENT_FILES=//p' "${AGENT_ENV}" 2>/dev/null | tail -n 1 | tr -d '[:space:]' | tr 'A-Z' 'a-z')"
    case "${_files_value}" in
        true | false) printf '%s' "${_files_value}" ;;
        *) printf 'true' ;;
    esac
}

ssh_status_json() {
    available=false; port=0; password=false; key=false
    if ssh_available; then
        available=true
        p="$(ssh_live_port)"
        case "${p}" in "" | *[!0-9]*) port=0 ;; *) port="${p}" ;; esac
        [ "$(ssh_password_auth)" = "yes" ] && password=true
        ssh_key_auth_available && key=true
    fi
    printf '{"available":%s,"port":%s,"password_authentication":%s,"key_auth_available":%s}' \
        "${available}" "${port}" "${password}" "${key}"
}

case "${VERB}" in
status)
    n=false; s=false
    net_ok && [ -n "$(default_iface)" ] && n=true
    sys_ok && s=true
    hn="null"
    [ "${n}" = "true" ] && hn="$(host_network_json)"
    agent_root=false
    agent_root_live && agent_root=true
    printf '{"network":%s,"system":%s,"host_network":%s,"hostname":%s,"ntp_enabled":%s,"ntp_servers":[%s],"timezone":%s,"cleanup":true,"ssh":%s,"agent_root":%s}\n' \
        "${n}" "${s}" "${hn}" "$(jstr "$(host_name)")" "$(ntp_state)" \
        "$(jstrs "$(ntp_list)")" "$(jstr "$(current_tz)")" "$(ssh_status_json)" "${agent_root}"
    ;;

apply-network)
    net_ok || die "systemd-networkd/systemd-resolved are not active" 3
    [ ! -e "${PENDING}" ] || die "a network change is already pending" 4
    K_dns=""; K_ipv4_mode=""; K_ipv4_address=""; K_ipv4_gateway=""; K_ipv6_enabled=""
    K_ipv6_mode=""; K_ipv6_address=""; K_ipv6_gateway=""; K_revert_after=""; K_dns_only=""
    read_kv "dns ipv4_mode ipv4_address ipv4_gateway ipv6_enabled ipv6_mode ipv6_address ipv6_gateway revert_after dns_only"
    K_dns_only="${K_dns_only:-false}"
    is_bool "${K_dns_only}" || die "invalid dns_only" 2
    all_ips "${K_dns}" 3 || die "invalid dns" 2
    is_mode "${K_ipv4_mode}" || die "invalid ipv4_mode" 2
    is_bool "${K_ipv6_enabled}" || die "invalid ipv6_enabled" 2
    is_mode "${K_ipv6_mode}" || die "invalid ipv6_mode" 2
    printf '%s\n' "${K_revert_after}" | grep -Eq '^[0-9]{2,3}$' || die "invalid revert_after" 2
    [ "${K_revert_after}" -ge 30 ] && [ "${K_revert_after}" -le 600 ] || die "revert_after out of range" 2
    if [ "${K_ipv4_mode}" = "static" ]; then
        is_v4cidr "${K_ipv4_address}" && is_v4 "${K_ipv4_gateway}" || die "invalid ipv4 static config" 2
    else
        [ -z "${K_ipv4_address}${K_ipv4_gateway}" ] || die "ipv4 address set in dhcp mode" 2
    fi
    if [ "${K_ipv6_enabled}" = "true" ] && [ "${K_ipv6_mode}" = "static" ]; then
        is_v6cidr "${K_ipv6_address}" && is_v6 "${K_ipv6_gateway}" || die "invalid ipv6 static config" 2
    else
        [ -z "${K_ipv6_address}${K_ipv6_gateway}" ] || die "ipv6 address set but not static" 2
    fi
    IFACE="$(default_iface)"
    printf '%s\n' "${IFACE}" | grep -Eq '^[A-Za-z0-9_.:-]{1,15}$' || die "no usable default interface" 3

    # Snapshot the owned files (absence is recorded as a .absent marker).
    rm -rf "${BACKUP}"; mkdir -p "${BACKUP}" "${NETD}" "${RESD}" || die "cannot prepare directories"
    snap() {
        if [ -e "$1" ]; then cp -p "$1" "${BACKUP}/$(basename "$1")"; else : >"${BACKUP}/$(basename "$1").absent"; fi
    }
    write_resolved() {
        if [ -n "${K_dns}" ]; then
            { echo "# Managed by Gotham (gotham-hostctl)."; echo "[Resolve]"; echo "DNS=$(printf '%s' "${K_dns}" | tr ',' ' ')"; } >"${RESFILE}.new" \
                && mv -f "${RESFILE}.new" "${RESFILE}" || die "cannot write resolved file"
        else
            rm -f "${RESFILE}"
        fi
    }
    if [ "${K_dns_only}" = "true" ]; then
        # Resolver change only: the interface file is never touched, so a
        # static address cannot be dropped by a DNS edit. The global drop-in
        # alone is shadowed by per-link DNS (netplan hosts), so the new
        # servers also go into a DNS-only drop-in on the winning .network
        # file plus a runtime `resolvectl dns` (JUS-101). Both carry DNS=
        # only, never Address/Gateway/routes.
        snap "${RESFILE}"
        LINKDNS="$(link_dns "${IFACE}")"
        printf '%s' "${LINKDNS}" >"${BACKUP}/link-dns"
        DROPIN=""
        WFILE="$(winning_file "${IFACE}")"
        if [ -n "${WFILE}" ]; then
            DROPIN="${NETD}/$(basename "${WFILE}").d/05-gotham.conf"
            printf '%s' "${DROPIN}" >"${BACKUP}/dropin-path"
            if [ -e "${DROPIN}" ]; then cp -p "${DROPIN}" "${BACKUP}/link-dns-dropin.conf"
            else : >"${BACKUP}/link-dns-dropin.conf.absent"; fi
        fi
        write_resolved
        if [ -n "${DROPIN}" ]; then
            if [ -n "${K_dns}" ]; then
                mkdir -p "$(dirname "${DROPIN}")" || die "cannot prepare link drop-in directory"
                { echo "# Managed by Gotham (gotham-hostctl). DNS override only; addresses/routes untouched."; echo "[Network]"; echo "DNS=$(printf '%s' "${K_dns}" | tr ',' ' ')"; } >"${DROPIN}.new" \
                    && mv -f "${DROPIN}.new" "${DROPIN}" || die "cannot write link drop-in"
            else
                rm -f "${DROPIN}"; rmdir "$(dirname "${DROPIN}")" 2>/dev/null
            fi
        fi
    else
        snap "${NETFILE}"; snap "${RESFILE}"

    {
        echo "# Managed by Gotham (gotham-hostctl). Manual edits are overwritten."
        echo "[Match]"; echo "Name=${IFACE}"; echo
        echo "[Network]"
        if [ "${K_ipv4_mode}" = "dhcp" ]; then echo "DHCP=ipv4"; else echo "DHCP=no"; echo "Address=${K_ipv4_address}"; fi
        if [ "${K_ipv6_enabled}" = "true" ]; then
            if [ "${K_ipv6_mode}" = "static" ]; then echo "IPv6AcceptRA=no"; echo "Address=${K_ipv6_address}"; else echo "IPv6AcceptRA=yes"; fi
        else
            echo "IPv6AcceptRA=no"; echo "LinkLocalAddressing=ipv4"
        fi
        if [ "${K_ipv4_mode}" = "static" ]; then echo; echo "[Route]"; echo "Gateway=${K_ipv4_gateway}"; fi
        if [ "${K_ipv6_enabled}" = "true" ] && [ "${K_ipv6_mode}" = "static" ]; then echo; echo "[Route]"; echo "Gateway=${K_ipv6_gateway}"; fi
    } >"${NETFILE}.new" && mv -f "${NETFILE}.new" "${NETFILE}" || die "cannot write network file"
        write_resolved
    fi

    date +%s >"${PENDING}"
    # Arm the revert BEFORE touching the live network.
    run systemd-run --quiet --unit="${TIMER}" --on-active="${K_revert_after}s" "${SELF}" revert-network \
        || { rm -f "${PENDING}"; "$0" revert-network 2>/dev/null; die "cannot arm revert timer"; }
    if [ "${K_dns_only}" = "true" ]; then
        # reload (not reconfigure) keeps addresses/routes; the runtime set
        # runs after the resolved restart so the restart cannot wipe it.
        run networkctl reload
        run systemctl restart systemd-resolved
        if [ -n "${K_dns}" ]; then
            run resolvectl dns "${IFACE}" $(printf '%s' "${K_dns}" | tr ',' ' ')
        else
            run resolvectl revert "${IFACE}"
        fi
    else
        run networkctl reload
        run networkctl reconfigure "${IFACE}"
        run systemctl restart systemd-resolved
    fi
    ;;

confirm-network)
    [ -e "${PENDING}" ] || die "no pending network change" 4
    run systemctl stop "${TIMER}.timer" 2>/dev/null
    rm -f "${PENDING}"; rm -rf "${BACKUP}"
    ;;

revert-network)
    # Idempotent: the timer may fire after a confirm race.
    [ -e "${PENDING}" ] || exit 0
    run systemctl stop "${TIMER}.timer" 2>/dev/null
    nb="${BACKUP}/$(basename "${NETFILE}")"
    iface_touched=true
    [ -e "${nb}" ] || [ -e "${nb}.absent" ] || iface_touched=false
    for f in "${NETFILE}" "${RESFILE}"; do
        b="${BACKUP}/$(basename "${f}")"
        if [ -e "${b}" ]; then cp -p "${b}" "${f}"
        elif [ -e "${b}.absent" ]; then rm -f "${f}"; fi
    done
    if [ -e "${BACKUP}/link-dns" ]; then
        # DNS-only apply: restore the link drop-in and the runtime link DNS
        # exactly; the interface file above is untouched either way.
        if [ -e "${BACKUP}/dropin-path" ]; then
            DROPIN="$(cat "${BACKUP}/dropin-path" 2>/dev/null)"
            case "${DROPIN}" in
                "${NETD}/"*.d/05-gotham.conf)
                    b="${BACKUP}/link-dns-dropin.conf"
                    if [ -e "${b}" ]; then
                        mkdir -p "$(dirname "${DROPIN}")" 2>/dev/null
                        cp -p "${b}" "${DROPIN}"
                    elif [ -e "${b}.absent" ]; then
                        rm -f "${DROPIN}"; rmdir "$(dirname "${DROPIN}")" 2>/dev/null
                    fi
                    run networkctl reload
                    ;;
            esac
        fi
        SAVED="$(cat "${BACKUP}/link-dns" 2>/dev/null)"
        IFACE_R="$(default_iface)"
        if [ -n "${SAVED}" ] && [ -n "${IFACE_R}" ]; then
            run resolvectl dns "${IFACE_R}" $(printf '%s' "${SAVED}" | tr ',' ' ')
        elif [ -n "${IFACE_R}" ]; then
            run resolvectl revert "${IFACE_R}"
        fi
    fi
    if [ "${iface_touched}" = "true" ]; then
        run networkctl reload
        IFACE="$(default_iface)"
        [ -n "${IFACE}" ] && run networkctl reconfigure "${IFACE}"
    fi
    # A DNS-only change never touched the interface file: restoring the
    # resolver drop-in, the link drop-in and the runtime link DNS above is
    # enough, reconfiguring would only flap the link.
    run systemctl restart systemd-resolved
    rm -f "${PENDING}"; rm -rf "${BACKUP}"
    ;;

apply-system)
    sys_ok || die "hostnamectl is not available" 3
    K_hostname=""; K_ntp_enabled=""; K_ntp_servers=""; K_timezone=""
    read_kv "hostname ntp_enabled ntp_servers timezone"
    { [ -z "${K_hostname}" ] || is_host "${K_hostname}"; } || die "invalid hostname" 2
    is_bool "${K_ntp_enabled}" || die "invalid ntp_enabled" 2
    all_hosts "${K_ntp_servers}" 4 || die "invalid ntp_servers" 2
    { [ -z "${K_timezone}" ] || is_tz "${K_timezone}"; } || die "invalid timezone" 2
    # Write the NTP drop-in before the hostname: a failed write must not leave
    # the host half-applied. Only touch it when servers are set or one is left
    # to remove, so a timezone-only change never depends on that directory.
    if [ -n "${K_ntp_servers}" ]; then
        mkdir -p "${NTPD}" || die "cannot prepare directories"
        { echo "# Managed by Gotham (gotham-hostctl)."; echo "[Time]"; echo "NTP=$(printf '%s' "${K_ntp_servers}" | tr ',' ' ')"; } >"${NTPFILE}.new" \
            && mv -f "${NTPFILE}.new" "${NTPFILE}" || die "cannot write timesyncd file"
    elif [ -e "${NTPFILE}" ]; then
        rm -f "${NTPFILE}" || die "cannot remove timesyncd file"
    fi
    if [ -n "${K_hostname}" ]; then run hostnamectl set-hostname "${K_hostname}" || die "hostnamectl failed"; fi
    run timedatectl set-ntp "${K_ntp_enabled}" || die "timedatectl failed"
    run systemctl restart systemd-timesyncd 2>/dev/null
    if [ -n "${K_timezone}" ]; then run timedatectl set-timezone "${K_timezone}" || die "timedatectl failed"; fi
    ;;

cleanup-status)
    journal=false; jbytes=0
    if journal_supported; then
        journal=true; jbytes="$(journal_bytes)"
    fi
    pm="$(pkg_manager)"; pkg=false; pbytes=0
    if [ -n "${pm}" ]; then
        pkg=true
        if [ "${pm}" = "apt" ]; then pbytes="$(apt_cache_bytes)"; else pbytes="$(dnf_cache_bytes)"; fi
    fi
    printf '{"journal":{"supported":%s,"bytes":%s},"package_cache":{"supported":%s,"manager":"%s","bytes":%s}}\n' \
        "${journal}" "$(size_or_zero "${jbytes}")" "${pkg}" "${pm}" "$(size_or_zero "${pbytes}")"
    ;;

cleanup-journal)
    journal_supported || die "unsupported" 3
    K_min_age_seconds=""
    read_kv "min_age_seconds"
    case "${K_min_age_seconds}" in
        "" | *[!0-9]*) die "invalid min_age_seconds" 2 ;;
    esac
    # Bound the digits so the arithmetic below stays inside the shell's range.
    [ "${#K_min_age_seconds}" -le 10 ] || die "invalid min_age_seconds" 2
    [ "${K_min_age_seconds}" -ge 86400 ] || K_min_age_seconds=86400
    before="$(size_or_zero "$(journal_bytes)")"
    run journalctl --vacuum-time="${K_min_age_seconds}s" >/dev/null || die "journalctl failed"
    after="$(size_or_zero "$(journal_bytes)")"
    printf '{"before":%s,"after":%s}\n' "${before}" "${after}"
    ;;

cleanup-package-cache)
    pm="$(pkg_manager)"
    [ -n "${pm}" ] || die "unsupported" 3
    read_kv ""
    if [ "${pm}" = "apt" ]; then
        before="$(size_or_zero "$(apt_cache_bytes)")"
        run apt-get clean >/dev/null || die "apt-get clean failed"
        after="$(size_or_zero "$(apt_cache_bytes)")"
    else
        before="$(size_or_zero "$(dnf_cache_bytes)")"
        run dnf clean packages >/dev/null || die "dnf clean failed"
        after="$(size_or_zero "$(dnf_cache_bytes)")"
    fi
    printf '{"before":%s,"after":%s}\n' "${before}" "${after}"
    ;;

apply-ssh)
    ssh_available || die "sshd is not installed on this host" 3
    K_port=""; K_password_authentication=""
    read_kv "port password_authentication"
    printf '%s\n' "${K_port}" | grep -Eq '^[0-9]{1,5}$' || die "invalid port" 2
    K_port="$(printf '%s' "${K_port}" | sed 's/^0*//')"
    [ -n "${K_port}" ] || K_port=0
    [ "${K_port}" -ge 1 ] && [ "${K_port}" -le 65535 ] || die "port out of range" 2
    is_bool "${K_password_authentication}" || die "invalid password_authentication" 2
    [ ! -e "${SSH_PENDING}" ] || die "an SSH change is already pending" 4
    live_port="$(ssh_live_port)"
    printf '%s\n' "${live_port}" | grep -Eq '^[0-9]{1,5}$' || die "cannot read the live SSH port" 3
    live_password="$(ssh_password_auth)"
    if [ "${K_password_authentication}" = "false" ] && [ "${live_password}" != "no" ]; then
        ssh_key_auth_available || die "cannot disable password login: no authorized key is configured" 5
    fi
    ssh_snapshot
    if [ "${K_port}" = "${live_port}" ]; then
        sshd_configure "${K_port}" "${K_password_authentication}"
        if ! sshd_validate || ! ssh_reload; then
            ssh_restore
            sshd_validate >/dev/null 2>&1 || true
            ssh_reload >/dev/null 2>&1 || true
            rm -rf "${SSH_BACKUP}"
            die "sshd rejected the new configuration; the previous values were restored"
        fi
        rm -rf "${SSH_BACKUP}"
        printf '{"port":%s,"previous_port":%s}\n' "${K_port}" "${live_port}"
        exit 0
    fi
    # Trial: both ports are live until the control plane confirms the new one.
    sshd_configure "${live_port} ${K_port}" "${K_password_authentication}"
    if ! sshd_validate; then
        ssh_restore
        rm -rf "${SSH_BACKUP}"
        die "sshd rejected the new configuration; the previous values were restored"
    fi
    fw="$(firewall_open "${K_port}")" || {
        ssh_restore
        rm -rf "${SSH_BACKUP}"
        die "cannot open port ${K_port} in the firewall; the previous values were restored"
    }
    if ! ssh_reload; then
        firewall_close "${K_port}" "${fw}"
        ssh_restore
        ssh_reload >/dev/null 2>&1 || true
        rm -rf "${SSH_BACKUP}"
        die "sshd did not reload; the previous values were restored"
    fi
    {
        echo "previous_port=${live_port}"
        echo "new_port=${K_port}"
        echo "password_authentication=${K_password_authentication}"
        echo "firewall=${fw}"
    } >"${SSH_PENDING}" || die "cannot record the pending SSH change"
    ssh_stop_revert
    if ! run systemd-run --quiet --collect --unit="${SSH_TIMER}" --on-active=900 "${SELF}" revert-ssh; then
        firewall_close "${K_port}" "${fw}"
        ssh_restore
        ssh_reload >/dev/null 2>&1 || true
        rm -f "${SSH_PENDING}"; rm -rf "${SSH_BACKUP}"
        die "cannot arm the SSH revert timer; the previous values were restored"
    fi
    printf '{"port":%s,"previous_port":%s}\n' "${K_port}" "${live_port}"
    ;;

confirm-ssh)
    [ -e "${SSH_PENDING}" ] || exit 0
    new_port="$(pending_val new_port)"
    password="$(pending_val password_authentication)"
    printf '%s\n' "${new_port}" | grep -Eq '^[0-9]{1,5}$' || die "the pending SSH change is unreadable" 3
    is_bool "${password}" || die "the pending SSH change is unreadable" 3
    sshd_configure "${new_port}" "${password}"
    if ! sshd_validate || ! ssh_reload; then
        ssh_restore
        sshd_validate >/dev/null 2>&1 || true
        ssh_reload >/dev/null 2>&1 || true
        ssh_stop_revert
        rm -f "${SSH_PENDING}"; rm -rf "${SSH_BACKUP}"
        die "could not keep the new SSH port; the previous values were restored"
    fi
    ssh_stop_revert
    rm -f "${SSH_PENDING}"; rm -rf "${SSH_BACKUP}"
    ;;

revert-ssh)
    [ -e "${SSH_PENDING}" ] || exit 0
    previous_port="$(pending_val previous_port)"
    new_port="$(pending_val new_port)"
    password="$(pending_val password_authentication)"
    fw="$(pending_val firewall)"
    ssh_restore
    if ! sshd_validate || ! ssh_reload; then
        # Keep a way in: the trial configuration (old + new port) is restored
        # and the pending change stays so the operator can retry.
        sshd_configure "${previous_port} ${new_port}" "${password}"
        sshd_validate >/dev/null 2>&1 || true
        ssh_reload >/dev/null 2>&1 || true
        die "could not restore the previous SSH port; the new port stays open"
    fi
    firewall_close "${new_port}" "${fw}"
    ssh_stop_revert
    rm -f "${SSH_PENDING}"; rm -rf "${SSH_BACKUP}"
    ;;

apply-agent-root)
    K_enabled=""
    read_kv "enabled"
    is_bool "${K_enabled}" || die "invalid enabled" 2
    systemctl cat gotham-agent.service >/dev/null 2>&1 \
        || die "gotham-agent.service is not installed on this host" 3
    live_root=false
    agent_root_live && live_root=true
    dropin=false
    [ -e "${AGENT_DROPIN}" ] && dropin=true
    if [ "${K_enabled}" = "true" ]; then
        mkdir -p "${AGENT_DROPIN_DIR}" || die "cannot prepare ${AGENT_DROPIN_DIR}"
        tmp="${AGENT_DROPIN}.new.$$"
        cat >"${tmp}" <<'DROPIN' || die "cannot write the agent drop-in"
# Managed by Gotham (gotham-hostctl). The agent runs as root so the file
# manager and terminal reach every file on this node.
[Service]
User=root
Group=root
ProtectSystem=no
ProtectHome=no
PrivateTmp=no
DROPIN
        chmod 0644 "${tmp}" 2>/dev/null || true
        mv -f "${tmp}" "${AGENT_DROPIN}" || die "cannot install the agent drop-in"
        # Root mode needs the file manager to be enabled; remember the previous
        # answer so disabling root restores it.
        [ -e "${STATE}/agent-files" ] || agent_files_current >"${STATE}/agent-files"
        agent_env_set_files true
    else
        rm -f "${AGENT_DROPIN}"
        if [ -e "${STATE}/agent-files" ]; then
            previous_files="$(sed -n '1p' "${STATE}/agent-files" 2>/dev/null)"
            case "${previous_files}" in
                true | false) agent_env_set_files "${previous_files}" ;;
            esac
            rm -f "${STATE}/agent-files"
        fi
        if [ -z "${ROOT}" ] && id -u gotham-agent >/dev/null 2>&1 && [ -d "${AGENT_STATE}" ]; then
            chown -R gotham-agent:gotham-agent "${AGENT_STATE}" || die "cannot restore ownership of ${AGENT_STATE}"
        fi
    fi
    if [ "${K_enabled}" = "${live_root}" ] && [ "${dropin}" = "${K_enabled}" ]; then
        printf '{"enabled":%s}\n' "${K_enabled}"
        exit 0
    fi
    run systemctl daemon-reload || die "systemd could not reload the agent unit"
    if [ "${DRYRUN}" = "1" ]; then
        echo "[dry-run] schedule gotham-agent.service restart"
    else
        # The restart drops this RPC's connection; schedule it just after the
        # helper returns so the control plane gets the result, then reconnects.
        systemd-run --quiet --collect --on-active=1 --unit="gotham-agent-root-$$" \
            systemctl restart gotham-agent.service || die "cannot schedule the agent restart"
    fi
    printf '{"enabled":%s}\n' "${K_enabled}"
    ;;

*)
    die "unknown verb: ${VERB}" 2
    ;;
esac
