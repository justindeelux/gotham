#!/bin/sh
#
# gotham-hostctl: privileged host settings helper for the Gotham control plane.
#
# Installed root-owned at /usr/libexec/gotham/gotham-hostctl and reached only
# through the sudoers drop-in written by install-hostctl-sudoers.sh, which grants
# the gotham service user exactly these verbs (one rule each, no wildcards):
#
#   status | apply-network | confirm-network | revert-network | apply-system
#
# Values arrive on stdin as key=value lines. Only whitelisted keys are accepted
# and every value is re-validated here with strict anchored patterns before it
# is written to a file or passed as an argument: the control plane is treated
# as untrusted input. Nothing is ever passed to a shell for evaluation.
#
# Supported stack: systemd-networkd + systemd-resolved (network) and
# systemd-timesyncd + hostnamectl (system). Gotham only ever owns these files:
#   /etc/systemd/network/05-gotham.network
#   /etc/systemd/resolved.conf.d/05-gotham.conf
#   /etc/systemd/timesyncd.conf.d/05-gotham.conf
#
# Network changes are tentative: apply-network snapshots the owned files, applies
# the change and arms a systemd timer that runs "revert-network" after
# revert_after seconds. confirm-network cancels the timer. When the operator can
# no longer reach the control plane nobody confirms, so the host restores itself.
#
# Under sudo every GOTHAM_HOSTCTL_* override is ignored; GOTHAM_HOSTCTL_ROOT and
# GOTHAM_HOSTCTL_DRYRUN exist for tests and are honoured only without sudo.
set -u
umask 022

SELF="/usr/libexec/gotham/gotham-hostctl"
ROOT=""
DRYRUN="0"
if [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
    ROOT="${GOTHAM_HOSTCTL_ROOT:-}"
    DRYRUN="${GOTHAM_HOSTCTL_DRYRUN:-0}"
fi

NETD="${ROOT}/etc/systemd/network"
RESD="${ROOT}/etc/systemd/resolved.conf.d"
NTPD="${ROOT}/etc/systemd/timesyncd.conf.d"
STATE="${ROOT}/var/lib/gotham-hostctl"
NETFILE="${NETD}/05-gotham.network"
RESFILE="${RESD}/05-gotham.conf"
NTPFILE="${NTPD}/05-gotham.conf"
PENDING="${STATE}/pending"
BACKUP="${STATE}/backup"
TIMER="gotham-hostctl-revert"

die() { echo "gotham-hostctl: $*" >&2; exit "${2:-1}"; }

# run executes a host command; in dry-run it only records it.
run() {
    if [ "${DRYRUN}" = "1" ]; then echo "+ $*"; return 0; fi
    "$@"
}

[ "$#" -eq 1 ] || die "exactly one verb is required" 2
VERB="$1"

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

mkdir -p "${STATE}" 2>/dev/null
chmod 0755 "${STATE}" 2>/dev/null

case "${VERB}" in
status)
    n=false; s=false
    net_ok && [ -n "$(default_iface)" ] && n=true
    sys_ok && s=true
    printf '{"network":%s,"system":%s}\n' "${n}" "${s}"
    ;;

apply-network)
    net_ok || die "systemd-networkd/systemd-resolved are not active" 3
    [ ! -e "${PENDING}" ] || die "a network change is already pending" 4
    K_dns=""; K_ipv4_mode=""; K_ipv4_address=""; K_ipv4_gateway=""; K_ipv6_enabled=""
    K_ipv6_mode=""; K_ipv6_address=""; K_ipv6_gateway=""; K_revert_after=""
    read_kv "dns ipv4_mode ipv4_address ipv4_gateway ipv6_enabled ipv6_mode ipv6_address ipv6_gateway revert_after"
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
    for f in "${NETFILE}" "${RESFILE}"; do
        if [ -e "${f}" ]; then cp -p "${f}" "${BACKUP}/$(basename "${f}")"; else : >"${BACKUP}/$(basename "${f}").absent"; fi
    done

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
    if [ -n "${K_dns}" ]; then
        { echo "# Managed by Gotham (gotham-hostctl)."; echo "[Resolve]"; echo "DNS=$(printf '%s' "${K_dns}" | tr ',' ' ')"; } >"${RESFILE}.new" \
            && mv -f "${RESFILE}.new" "${RESFILE}" || die "cannot write resolved file"
    else
        rm -f "${RESFILE}"
    fi

    date +%s >"${PENDING}"
    # Arm the revert BEFORE touching the live network.
    run systemd-run --quiet --unit="${TIMER}" --on-active="${K_revert_after}s" "${SELF}" revert-network \
        || { rm -f "${PENDING}"; "$0" revert-network 2>/dev/null; die "cannot arm revert timer"; }
    run networkctl reload
    run networkctl reconfigure "${IFACE}"
    run systemctl restart systemd-resolved
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
    for f in "${NETFILE}" "${RESFILE}"; do
        b="${BACKUP}/$(basename "${f}")"
        if [ -e "${b}" ]; then cp -p "${b}" "${f}"; else rm -f "${f}"; fi
    done
    run networkctl reload
    IFACE="$(default_iface)"
    [ -n "${IFACE}" ] && run networkctl reconfigure "${IFACE}"
    run systemctl restart systemd-resolved
    rm -f "${PENDING}"; rm -rf "${BACKUP}"
    ;;

apply-system)
    sys_ok || die "hostnamectl is not available" 3
    K_hostname=""; K_ntp_enabled=""; K_ntp_servers=""
    read_kv "hostname ntp_enabled ntp_servers"
    { [ -z "${K_hostname}" ] || is_host "${K_hostname}"; } || die "invalid hostname" 2
    is_bool "${K_ntp_enabled}" || die "invalid ntp_enabled" 2
    all_hosts "${K_ntp_servers}" 4 || die "invalid ntp_servers" 2
    if [ -n "${K_hostname}" ]; then run hostnamectl set-hostname "${K_hostname}" || die "hostnamectl failed"; fi
    mkdir -p "${NTPD}" || die "cannot prepare directories"
    if [ -n "${K_ntp_servers}" ]; then
        { echo "# Managed by Gotham (gotham-hostctl)."; echo "[Time]"; echo "NTP=$(printf '%s' "${K_ntp_servers}" | tr ',' ' ')"; } >"${NTPFILE}.new" \
            && mv -f "${NTPFILE}.new" "${NTPFILE}" || die "cannot write timesyncd file"
    else
        rm -f "${NTPFILE}"
    fi
    if [ "${K_ntp_enabled}" = "true" ]; then run timedatectl set-ntp true; else run timedatectl set-ntp false; fi
    run systemctl restart systemd-timesyncd 2>/dev/null
    ;;

*)
    die "unknown verb: ${VERB}" 2
    ;;
esac
