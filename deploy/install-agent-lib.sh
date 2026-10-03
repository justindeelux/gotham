#!/bin/sh
#
# install-agent-lib.sh holds the pieces of install-agent.sh that carry
# reinstall behaviour or need root-free exercise, split out so they can be
# tested without root or a systemd host (see deploy/test-agent-install.sh).
# It is sourced, not run.
#
# Fixed production paths for the --full Docker setup. These are plain shell
# variables with fixed defaults, assigned unconditionally when this file is
# sourced (never imported from the environment), so no exported variable can
# redirect them. The test suite overrides them by assigning new values AFTER
# sourcing this file.
#
# agent_env_write     composes /etc/gotham/agent.env, preserving values a prior
#                     install wrote for keys this invocation does not set.
# agent_env_validate  checks every value agent_env_write would write without
#                     writing anything, so the installer can fail before its
#                     first mutation.
# agent_service_restart  restarts an already-running agent on the new binary
#                     (starts it otherwise) and verifies the unit's ExecStart.
# ensure_docker_full  installs Docker Engine + the compose plugin from the
#                     official Docker apt repository (--full; Ubuntu/Debian
#                     only, idempotent, fingerprint-pinned key). An engine that
#                     is already present gets only the compose plugin (the
#                     engine is never replaced), and a Docker repository the
#                     operator already defined is reused, never duplicated.

_OS_RELEASE_FILE=/etc/os-release
_APT_ROOT=

# _env_has_ctrl succeeds when $1 holds a control character (any byte below
# 0x20 or 0x7f, including newline): such a value written as KEY=value would
# smuggle a second line (e.g. GOTHAM_AGENT_INSECURE=true) into agent.env.
_env_has_ctrl() {
    _stripped="$(printf '%s' "$1" | tr -d '\000-\037\177')"
    [ "${_stripped}" != "$1" ]
}

# _is_loopback_listener mirrors the agent's isLoopbackListenAddr
# (agent/grpc_server.go): "localhost" (any case), a bracketed "[::1]",
# or a dotted 127.<d>.<d>.<d> IPv4 address. "127.example.com" and an
# unbracketed "::1" are not loopback, and leading-zero octets are
# rejected because Go's net.ParseIP rejects them.
_is_loopback_listener() {
    _addr=$1
    case "${_addr}" in
        "[::1]" | "[::1]:"*) return 0 ;;
    esac
    case "${_addr}" in
        *:*) _host="${_addr%:*}" ;;
        *) _host="${_addr}" ;;
    esac
    _lower="$(printf '%s' "${_host}" | tr '[:upper:]' '[:lower:]')"
    if [ "${_lower}" = "localhost" ]; then
        return 0
    fi
    case "${_host}" in
        *[!0-9.]* | "") return 1 ;;
    esac
    _oldifs=$IFS
    IFS=.
    # shellcheck disable=SC2086
    set -- ${_host}
    IFS=${_oldifs}
    [ "$#" -eq 4 ] || return 1
    [ "$1" = "127" ] || return 1
    shift
    for _octet in "$@"; do
        case "${_octet}" in
            "" | *[!0-9]*) return 1 ;;
        esac
        if [ "${_octet}" != "0" ] && [ "${_octet#0}" != "${_octet}" ]; then
            return 1
        fi
        [ "${#_octet}" -le 3 ] || return 1
        [ "${_octet}" -le 255 ] || return 1
    done
    return 0
}

# _env_check_value rejects a single agent.env value before it is written.
# Empty values are unset keys (never written, nothing to check). Every
# non-empty value is rejected on control characters and on a trailing
# backslash (systemd would join such a line with the next one, swallowing a
# key); the remaining keys get a shape check matching what the agent itself
# accepts:
# - dial/listen addresses: host:port systemd parses verbatim (spaces, quotes
#   and = are refused on both: systemd strips a leading quote and unquotes a
#   balanced pair, so the unit would see a different value than the operator
#   typed — proven on Ubuntu 22.04 systemd 249: KEY="abc" delivers abc,
#   KEY="a b" delivers a b);
# - NODE_ID mirrors the agent's validNodeID (agent/config.go) and the control
#   plane's validateNodeID (internal/servers/ca.go): at most 253 bytes, no
#   wildcard, path or whitespace characters. Quotes are refused although the
#   agent accepts them: an EnvironmentFile line does not hold them verbatim
#   (a leading quote is silently stripped, a balanced pair is unquoted), so
#   the running agent would see a different id than the installer wrote;
# - CERT_DIR/KEY/CA must be absolute paths without whitespace, quotes or
#   backslashes (a relative path would resolve against the service's working
#   directory, not the operator's checkout);
# - DOCKER_SOCK must be unix:///abs/path, a bare /abs/path or
#   tcp://host:port, without whitespace, quotes or backslashes;
# - AUTO_UPDATE is exactly what the agent parses (true/false, any case,
#   surrounding whitespace trimmed): 1/yes/on silently mean false to the
#   agent, so the installer refuses them instead of pretending otherwise;
# - UPDATE_INTERVAL mirrors Go's time.ParseDuration grammar (optional sign,
#   surrounding whitespace trimmed as the agent trims it, int-or-float
#   magnitudes, ns/us/ms/s/m/h units, bare 0) and its overflow errors (a
#   total past ~292 years is refused, not defaulted); a zero or negative
#   value still falls through to the agent default, as today;
# - UPDATE_CHANNEL accepts [A-Za-z0-9_.-]+ (the agent maps unknown channels
#   to stable).
# Prints the reason on stderr and returns 1 on rejection.
_env_check_value() {
    _ck_key=$1
    _ck_value=$2
    [ -n "${_ck_value}" ] || return 0
    if _env_has_ctrl "${_ck_value}"; then
        echo "install-agent.sh: ${_ck_key} contains a control character (rejected)" >&2
        return 1
    fi
    case "${_ck_value}" in
        *\\)
            echo "install-agent.sh: ${_ck_key} must not end with a backslash (systemd would join it with the next line; got '${_ck_value}')" >&2
            return 1
            ;;
    esac
    # A literal tab cannot appear in a case pattern on every shell, so it
    # travels in a variable (quoted below, hence matched literally).
    _ck_tab="$(printf '\t')"
    case "${_ck_key}" in
        GOTHAM_AGENT_CP_ADDR)
            case "${_ck_value}" in
                *" "* | *"'"* | *'"'* | *=*)
                    echo "install-agent.sh: ${_ck_key} must not contain spaces, quotes or = (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            case "${_ck_value}" in
                *:*)
                    ;;
                *)
                    echo "install-agent.sh: ${_ck_key} must be host:port (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_LISTEN_ADDR)
            case "${_ck_value}" in
                *" "* | *"$_ck_tab"* | *"'"* | *'"'* | *=*)
                    echo "install-agent.sh: ${_ck_key} must not contain spaces, quotes or = (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            case "${_ck_value}" in
                *:*)
                    ;;
                *)
                    echo "install-agent.sh: ${_ck_key} must be host:port or :port (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_NODE_ID)
            if [ "${#_ck_value}" -gt 253 ]; then
                echo "install-agent.sh: ${_ck_key} exceeds 253 bytes (got ${#_ck_value})" >&2
                return 1
            fi
            case "${_ck_value}" in
                *\** | *\\* | */* | *" "* | *"$_ck_tab"* | *"'"* | *'"'*)
                    echo "install-agent.sh: ${_ck_key} must not contain spaces, tabs, quotes, *, / or backslashes (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_CERT_DIR | GOTHAM_AGENT_KEY | GOTHAM_AGENT_CA)
            case "${_ck_value}" in
                /*) ;;
                *)
                    echo "install-agent.sh: ${_ck_key} must be an absolute path (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            case "${_ck_value}" in
                *" "* | *"$_ck_tab"* | *"'"* | *'"'* | *\\*)
                    echo "install-agent.sh: ${_ck_key} must not contain whitespace, quotes or backslashes (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_DOCKER_SOCK)
            case "${_ck_value}" in
                *" "* | *"$_ck_tab"* | *"'"* | *'"'* | *\\*)
                    echo "install-agent.sh: ${_ck_key} must not contain whitespace, quotes or backslashes (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            case "${_ck_value}" in
                unix://*)
                    _ck_rest="${_ck_value#unix://}"
                    case "${_ck_rest}" in
                        /*) ;;
                        *)
                            echo "install-agent.sh: ${_ck_key} unix:// endpoints must carry an absolute socket path (got '${_ck_value}')" >&2
                            return 1
                            ;;
                    esac
                    ;;
                tcp://*)
                    _ck_rest="${_ck_value#tcp://}"
                    case "${_ck_rest}" in
                        ?*:?*)
                            ;;
                        *)
                            echo "install-agent.sh: ${_ck_key} must be unix:///abs/path, /abs/path or tcp://host:port (got '${_ck_value}')" >&2
                            return 1
                            ;;
                    esac
                    ;;
                /*)
                    ;;
                *)
                    echo "install-agent.sh: ${_ck_key} must be unix:///abs/path, /abs/path or tcp://host:port (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_LOG_LEVEL)
            _ck_norm="$(printf '%s' "${_ck_value}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | tr '[:upper:]' '[:lower:]')"
            case "${_ck_norm}" in
                debug | info | warn | warning | error)
                    ;;
                *)
                    echo "install-agent.sh: ${_ck_key} must be debug, info, warn or error (got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_AUTO_UPDATE)
            # The agent enables unattended updates only on EqualFold(trimmed,
            # "true") (agent/config.go): 1/yes/on silently mean off there, so
            # the installer accepts only an explicit true or false instead of
            # pretending those spellings switch anything on.
            _ck_norm="$(printf '%s' "${_ck_value}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | tr '[:upper:]' '[:lower:]')"
            case "${_ck_norm}" in
                true | false)
                    ;;
                *)
                    echo "install-agent.sh: ${_ck_key} must be true or false (the agent only honours true; got '${_ck_value}')" >&2
                    return 1
                    ;;
            esac
            ;;
        GOTHAM_AGENT_UPDATE_INTERVAL)
            # Mirrors Go's time.ParseDuration grammar (agent/config.go parses
            # with it and falls back to the 5m default on error or when the
            # result is not positive): an optional sign, surrounding
            # whitespace trimmed exactly as the agent trims it, then one or
            # more int-or-float magnitudes each with ns/us/ms/s/m/h units, or
            # a bare 0. A total past the int64 nanosecond range (~292 years)
            # is an overflow error in Go, so the installer refuses it instead
            # of writing a value the agent would silently default. Zero or
            # negative values still fall through to the agent default, as
            # today.
            _ck_dur="$(printf '%s' "${_ck_value}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
            case "${_ck_dur}" in
                [+-]*) _ck_dur="${_ck_dur#?}" ;;
            esac
            # µs/μs spellings are normalized before the ASCII-only checks.
            _ck_dur="$(printf '%s' "${_ck_dur}" | sed 's/µs/us/g;s/μs/us/g')"
            if [ "${_ck_dur}" != "0" ] && ! printf '%s' "${_ck_dur}" | grep -Eq '^(([0-9]+(\.[0-9]*)?|\.[0-9]+)(ns|us|ms|s|m|h))+$'; then
                echo "install-agent.sh: ${_ck_key} must be a Go duration such as 5m or 1h30m (got '${_ck_value}')" >&2
                return 1
            fi
            if [ "${_ck_dur}" != "0" ] && ! printf '%s' "${_ck_dur}" | awk '
                # Exact integer overflow check against the int64 nanosecond
                # range: awk numbers are doubles, which round 2^63 to itself,
                # so 9223372036854775808ns compared numerically equal and a
                # mutant off-by-one total passed. Every term is therefore kept
                # as a digit string (the mul/add helpers below never create
                # a float) and the total is compared digit by digit.
                function strmul(s, n,   out, carry, i, d) {
                    out = ""; carry = 0
                    for (i = length(s); i >= 1; i--) {
                        d = (substr(s, i, 1) + 0) * n + carry
                        out = (d % 10) out
                        carry = int(d / 10)
                    }
                    while (carry > 0) { out = (carry % 10) out; carry = int(carry / 10) }
                    sub(/^0+/, "", out)
                    return out == "" ? "0" : out
                }
                function zeros(n,   out) { out = ""; while (n > 0) { out = out "0"; n-- }; return out }
                function stradd(a, b,   out, carry, i, da, db, d) {
                    out = ""; carry = 0; i = 0
                    while (i < length(a) || i < length(b) || carry > 0) {
                        da = i < length(a) ? substr(a, length(a) - i, 1) + 0 : 0
                        db = i < length(b) ? substr(b, length(b) - i, 1) + 0 : 0
                        d = da + db + carry
                        out = (d % 10) out
                        carry = int(d / 10); i++
                    }
                    sub(/^0+/, "", out)
                    return out == "" ? "0" : out
                }
                function strgt(a, b) {
                    sub(/^0+/, "", a); sub(/^0+/, "", b)
                    if (a == "") a = "0"; if (b == "") b = "0"
                    if (length(a) != length(b)) return length(a) > length(b)
                    # Same length: compare lexicographically. The "x" prefix
                    # forces a string comparison: two numeric strings would
                    # otherwise compare as (rounded) numbers.
                    return ("x" a) > ("x" b)
                }
                BEGIN { m_of["ns"]=1; e_of["ns"]=0; m_of["us"]=1; e_of["us"]=3; m_of["ms"]=1; e_of["ms"]=6; m_of["s"]=1; e_of["s"]=9; m_of["m"]=6; e_of["m"]=10; m_of["h"]=36; e_of["h"]=11 }
                { s=$0; total="0";
                  while (s != "") {
                    if (match(s, /^([0-9]+(\.[0-9]*)?|\.[0-9]+)/)) { mag=substr(s, 1, RLENGTH); s=substr(s, RLENGTH+1) } else { exit 2 }
                    if (match(s, /^(ns|us|ms|s|m|h)/)) { unit=substr(s, 1, RLENGTH); s=substr(s, RLENGTH+1) } else { exit 2 }
                    if (match(mag, /\./)) { I=substr(mag, 1, RSTART-1); F=substr(mag, RSTART+1) } else { I=mag; F="" }
                    if (I == "") I="0"
                    m=m_of[unit]; e=e_of[unit]
                    base=strmul(I, m) zeros(e)
                    if (F == "") { fpart="0" }
                    else {
                        k=length(F); fval=strmul(F, m)
                        if (e >= k) { fpart=fval zeros(e-k) }
                        else {
                            # A sub-nanosecond fraction: Go truncates toward
                            # zero, so drop the excess digits the same way.
                            keep=length(fval)-(k-e)
                            fpart=(keep > 0) ? substr(fval, 1, keep) : "0"
                        }
                    }
                    total=stradd(total, stradd(base, fpart)) }
                  if (strgt(total, "9223372036854775807")) { exit 1 } }'; then
                echo "install-agent.sh: ${_ck_key} overflows Go time.Duration (got '${_ck_value}')" >&2
                return 1
            fi
            ;;
        GOTHAM_AGENT_UPDATE_CHANNEL)
            if ! printf '%s' "${_ck_value}" | grep -Eq '^[A-Za-z0-9_.-]+$'; then
                echo "install-agent.sh: ${_ck_key} must be a channel name (letters, digits, . _ -; got '${_ck_value}')" >&2
                return 1
            fi
            ;;
    esac
    return 0
}

# agent_env_validate checks every value agent_env_write would write to
# <env_file> (ambient GOTHAM_AGENT_* values first, then the previous file's
# values they would preserve) without writing anything. install-agent.sh runs
# it before the first mutation so a bad value never leaves a half-applied
# install; agent_env_write runs it again at write time.
#
#   $1 env_file, $2 agent_ca, $3 insecure (same arguments as agent_env_write)
agent_env_validate() (
    _v_env_file=$1
    _v_ca=$2
    _v_insecure=$3
    _v_prev=""
    if [ -f "${_v_env_file}" ]; then
        _v_prev="$(cat "${_v_env_file}")"
    fi
    _v_prev_value() {
        printf '%s\n' "${_v_prev}" | sed -n "s/^[[:space:]]*$1=//p" | tail -n1
    }
    _v_value() {
        eval "_v_ambient=\${$1:-}"
        if [ -n "${_v_ambient}" ]; then
            printf '%s' "${_v_ambient}"
        else
            _v_prev_value "$1"
        fi
    }
    for _v_key in \
        GOTHAM_AGENT_CP_ADDR \
        GOTHAM_AGENT_NODE_ID \
        GOTHAM_AGENT_CERT_DIR \
        GOTHAM_AGENT_KEY \
        GOTHAM_AGENT_DOCKER_SOCK \
        GOTHAM_AGENT_LOG_LEVEL \
        GOTHAM_AGENT_AUTO_UPDATE \
        GOTHAM_AGENT_UPDATE_INTERVAL \
        GOTHAM_AGENT_UPDATE_CHANNEL; do
        _v_val="$(_v_value "${_v_key}")"
        _env_check_value "${_v_key}" "${_v_val}" || exit 1
    done
    # The listener resolves exactly as in agent_env_write (ambient wins; a
    # plaintext install's stale loopback listener is dropped on the way to
    # TLS; --insecure without a CA defaults it and pins it to loopback).
    _v_prior_insecure="$(printf '%s' "$(_v_prev_value GOTHAM_AGENT_INSECURE)" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
    [ "${_v_prior_insecure}" = "true" ] || _v_prior_insecure=""
    eval "_v_ambient_listen=\${GOTHAM_AGENT_LISTEN_ADDR:-}"
    _v_prior_listen="$(_v_prev_value GOTHAM_AGENT_LISTEN_ADDR)"
    _v_listen=""
    if [ -n "${_v_ambient_listen}" ]; then
        _v_listen="${_v_ambient_listen}"
    elif [ -n "${_v_prior_listen}" ]; then
        if [ -n "${_v_ca}" ] && [ "${_v_prior_insecure}" = "true" ]; then
            _v_listen=""
        else
            _v_listen="${_v_prior_listen}"
        fi
    fi
    if [ -z "${_v_ca}" ]; then
        if [ -z "${_v_listen}" ] && [ "${_v_insecure}" -eq 1 ]; then
            _v_listen="127.0.0.1:9443"
        fi
        if [ "${_v_insecure}" -eq 1 ] && [ -n "${_v_listen}" ] && ! _is_loopback_listener "${_v_listen}"; then
            if [ -n "${_v_ambient_listen}" ]; then
                echo "install-agent.sh: GOTHAM_AGENT_LISTEN_ADDR=${_v_listen} is not loopback; --insecure requires a loopback listener" >&2
                exit 1
            fi
        fi
    fi
    _env_check_value GOTHAM_AGENT_LISTEN_ADDR "${_v_listen}" || exit 1
    # The CA path is installer-resolved (never ambient), but a poisoned prior
    # file feeds it on reinstalls, so it gets the same absolute-path shape
    # check as the other paths.
    _env_check_value GOTHAM_AGENT_CA "${_v_ca}" || exit 1
)

# agent_env_write writes <env_file> for the agent.
#
#   $1 env_file       destination path (its directory must exist)
#   $2 agent_ca       resolved control-plane CA path, or empty
#   $3 insecure       1 when --insecure was given, else 0
#
# Ambient GOTHAM_AGENT_* variables (the values the operator supplied this run)
# win; for a key left unset the previous file's value is kept. When a key appears
# more than once, the last occurrence wins, matching systemd's EnvironmentFile.
# Operator-added keys are preserved verbatim. This keeps a reinstall from wiping
# the control plane address and node id and resetting the agent to the local
# default. A symlinked env file keeps its link (the target is rewritten). The
# caller owns the file's ownership (chown needs root); this sets mode 0640.
# Every value is validated (control characters rejected, per-key shapes
# checked) before the first byte is written; see agent_env_validate.
agent_env_write() {
    _env_file=$1
    _agent_ca=$2
    _insecure=$3
    # The whole body runs in a subshell so helper variables cannot leak into the
    # caller and the temp file's EXIT trap is scoped to this write.
    (
        umask 077
        # _resolve_path follows a symlink chain so a symlinked agent.env keeps its
        # link: the write replaces the target, not the link.
        _resolve_path() {
            _rp=$1
            _hops=0
            while [ -L "${_rp}" ] && [ "${_hops}" -lt 40 ]; do
                _link="$(readlink "${_rp}" 2>/dev/null || true)"
                [ -n "${_link}" ] || break
                case "${_link}" in
                    /*) _rp="${_link}" ;;
                    *) _rp="$(dirname "${_rp}")/${_link}" ;;
                esac
                _hops=$((_hops + 1))
            done
            printf '%s' "${_rp}"
        }
        _env_target="$(_resolve_path "${_env_file}")"
        _env_prev=""
        if [ -f "${_env_target}" ]; then
            _env_prev="$(cat "${_env_target}")"
        fi
        # The last occurrence wins (systemd EnvironmentFile), and leading
        # whitespace is tolerated so a hand-indented key keeps its value.
        _env_prev_value() {
            printf '%s\n' "${_env_prev}" | sed -n "s/^[[:space:]]*$1=//p" | tail -n1
        }
        _env_value() {
            eval "_ambient=\${$1:-}"
            if [ -n "${_ambient}" ]; then
                printf '%s' "${_ambient}"
            else
                _env_prev_value "$1"
            fi
        }
        # Resolve the listener before writing: a plaintext install's loopback
        # address must not survive a switch to TLS. The --insecure loopback rules
        # apply only when there is no CA: with a CA the agent serves TLS and may
        # bind any address, so --insecure alongside --ca must not force loopback.
        # The prior INSECURE flag is compared the way the agent reads it
        # (trimmed, case-insensitive).
        _prior_insecure="$(printf '%s' "$(_env_prev_value GOTHAM_AGENT_INSECURE)" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
        [ "${_prior_insecure}" = "true" ] || _prior_insecure=""
        eval "_ambient_listen=\${GOTHAM_AGENT_LISTEN_ADDR:-}"
        _prior_listen="$(_env_prev_value GOTHAM_AGENT_LISTEN_ADDR)"
        _listen=""
        if [ -n "${_ambient_listen}" ]; then
            _listen="${_ambient_listen}"
        elif [ -n "${_prior_listen}" ]; then
            if [ -n "${_agent_ca}" ] && [ "${_prior_insecure}" = "true" ]; then
                # Plaintext -> TLS: the old loopback listener would leave a
                # remote control plane unable to reach the agent; drop it.
                _listen=""
            else
                _listen="${_prior_listen}"
            fi
        fi
        if [ -z "${_agent_ca}" ]; then
            if [ -z "${_listen}" ] && [ "${_insecure}" -eq 1 ]; then
                _listen="127.0.0.1:9443"
            fi
            if [ "${_insecure}" -eq 1 ] && [ -n "${_listen}" ] && ! _is_loopback_listener "${_listen}"; then
                if [ -n "${_ambient_listen}" ]; then
                    echo "install-agent.sh: GOTHAM_AGENT_LISTEN_ADDR=${_listen} is not loopback; --insecure requires a loopback listener" >&2
                    exit 1
                fi
                _listen="127.0.0.1:9443"
            fi
        fi
        # Every value below is validated before the first byte is written (the
        # same pass install-agent.sh runs before its first mutation).
        agent_env_validate "${_env_file}" "${_agent_ca}" "${_insecure}" || exit 1
        _env_tmp="${_env_target}.tmp.$$"
        # Root-only temp holding operator settings; remove it if any step
        # aborts. The success path moves it into place first, so the trap is a
        # no-op then.
        trap 'rm -f "${_env_tmp}"' EXIT
        trap 'exit 1' INT TERM
        : >"${_env_tmp}"
        for _key in \
            GOTHAM_AGENT_CP_ADDR \
            GOTHAM_AGENT_NODE_ID \
            GOTHAM_AGENT_CERT_DIR \
            GOTHAM_AGENT_KEY \
            GOTHAM_AGENT_DOCKER_SOCK \
            GOTHAM_AGENT_LOG_LEVEL \
            GOTHAM_AGENT_AUTO_UPDATE \
            GOTHAM_AGENT_UPDATE_INTERVAL \
            GOTHAM_AGENT_UPDATE_CHANNEL; do
            _value="$(_env_value "${_key}")"
            if [ -n "${_value}" ]; then
                printf '%s=%s\n' "${_key}" "${_value}" >>"${_env_tmp}"
            fi
        done
        if [ -n "${_listen}" ]; then
            printf 'GOTHAM_AGENT_LISTEN_ADDR=%s\n' "${_listen}" >>"${_env_tmp}"
        fi
        # The CA path is resolved by the installer (never taken verbatim from
        # the ambient GOTHAM_AGENT_CA, which the installer itself does not
        # write). Without a CA, --insecure is the only path.
        if [ -n "${_agent_ca}" ]; then
            printf 'GOTHAM_AGENT_CA=%s\n' "${_agent_ca}" >>"${_env_tmp}"
        elif [ "${_insecure}" -eq 1 ]; then
            printf 'GOTHAM_AGENT_INSECURE=true\n' >>"${_env_tmp}"
        fi
        # Keep the operator lines: drop every managed key (tolerating leading
        # whitespace, so a hand-indented key cannot silently override the
        # managed value). A single grep keeps the exit status exact: 1 means
        # "nothing matched" (no operator settings, the normal case); anything
        # else aborts rather than silently dropping operator settings.
        _filter_status=0
        _preserved=$(printf '%s\n' "${_env_prev}" \
            | grep -v -E '^[[:space:]]*GOTHAM_AGENT_(CP_ADDR|NODE_ID|LISTEN_ADDR|CERT_DIR|KEY|DOCKER_SOCK|LOG_LEVEL|AUTO_UPDATE|UPDATE_INTERVAL|UPDATE_CHANNEL|CA|INSECURE)=') \
            || _filter_status=$?
        case "${_filter_status}" in
            0) ;;
            1) _preserved="" ;;
            *)
                echo "install-agent.sh: could not filter the existing ${_env_file} (grep exit ${_filter_status})" >&2
                exit 1
                ;;
        esac
        if [ -n "${_preserved}" ]; then
            printf '%s\n' "${_preserved}" >>"${_env_tmp}" \
                || {
                    echo "install-agent.sh: could not preserve operator settings in ${_env_file}" >&2
                    exit 1
                }
        fi
        chmod 0640 "${_env_tmp}"
        mv -f "${_env_tmp}" "${_env_target}"
        chmod 0640 "${_env_target}"
    )
}

# agent_service_restart brings the agent onto the freshly installed binary:
# restart a running unit, start an inactive one, then verify the unit's
# ExecStart points at <binary>. Returns non-zero when either step fails.
#
#   $1 service   systemd unit name (e.g. gotham-agent.service)
#   $2 binary    absolute path of the installed binary
agent_service_restart() {
    _service=$1
    _binary=$2
    if systemctl is-active --quiet "${_service}"; then
        systemctl restart "${_service}" || return 1
    else
        systemctl start "${_service}" || return 1
    fi
    _exec_start="$(systemctl show -p ExecStart --value "${_service}" 2>/dev/null || true)"
    case "${_exec_start}" in
        *"${_binary}"*) return 0 ;;
        *)
            echo "install-agent.sh: ${_service} ExecStart does not reference ${_binary}" >&2
            return 1
            ;;
    esac
}

# Docker apt repository pinned for --full installs. Packages are verified by
# apt against this keyring; the key itself is fetched over HTTPS and checked
# against the published fingerprint below, never piped to a shell.
_docker_apt_key_url="https://download.docker.com/linux"
_docker_apt_key_fingerprint="9DC858229FC7DD38854AE2D88D81803C0EBFCD88"

# _docker_repo_serves reports whether the Docker apt repository serves the
# given suite by probing .../dists/<codename>/Release with the hardened curl
# flags (same pinning as the key download, plus a connect and a total
# timeout). Exit 0: served (HTTP 200). Exit 1: not served (HTTP 404) — the
# caller may fall back to another suite. Exit 2: the repository could not be
# reached at all (network failure, unexpected HTTP status) — the caller's
# curl already printed the real error (errors are NOT sent to /dev/null), and
# the caller must refuse, never silently fall back to an older suite it could
# not even verify is served. $1 distro, $2 codename.
_docker_repo_serves() {
    _probe_code="$(curl -SL --proto '=https' --tlsv1.2 --retry 3 \
        --connect-timeout 10 --max-time 30 --silent --show-error \
        -o /dev/null -w '%{http_code}' \
        "${_docker_apt_key_url}/$1/dists/$2/Release")" || return 2
    case "${_probe_code}" in
        200) return 0 ;;
        404) return 1 ;;
        *)
            echo "install-agent.sh --full: unexpected HTTP ${_probe_code} probing ${_docker_apt_key_url}/$1/dists/$2/Release" >&2
            return 2
            ;;
    esac
}

# _docker_repo_has_active_entry reports whether the apt source file defines an
# enabled download.docker.com entry. Full-line comments (#...) never count,
# and a DEB822 stanza (.sources) carrying "Enabled: no" is disabled.
_docker_repo_has_active_entry() {
    case "$1" in
        *.sources)
            awk '
                function stanza_end() {
                    if (has && !disabled) found = 1
                    has = 0; disabled = 0
                }
                BEGIN { has = 0; disabled = 0; found = 0 }
                /^[[:space:]]*$/ { stanza_end(); next }
                {
                    line = $0
                    sub(/^[[:space:]]+/, "", line)
                    if (line ~ /^#/) next
                    if (index(line, "download.docker.com") > 0) has = 1
                    if (tolower(line) ~ /^enabled:[[:space:]]*no([[:space:]]|$)/) disabled = 1
                }
                END { stanza_end(); exit(!found) }' "$1" 2>/dev/null
            ;;
        *)
            sed 's/^[[:space:]]*#.*//' "$1" 2>/dev/null | grep -qF 'download.docker.com'
            ;;
    esac
}

# ensure_docker_full installs Docker Engine and the compose plugin from the
# official Docker apt repository (Ubuntu/Debian only). It is idempotent: when
# `docker` and `docker compose` already work it only logs and returns 0. When
# only the engine is present it installs just the compose plugin, never an
# engine package (naming one could make apt replace or remove the working
# engine, e.g. docker.io). A Docker repository the operator already defined
# (docker.sources, a docker.asc Signed-By line, another .list) is reused
# instead of adding a conflicting docker.list. On any other distro it fails
# with a message naming the manual step instead of attempting an unverified
# install. The distro and apt paths come from the fixed _OS_RELEASE_FILE /
# _APT_ROOT variables above; there are no environment seams.
ensure_docker_full() {
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        echo "==> Docker Engine and the compose plugin are already installed; skipping"
        return 0
    fi
    _os_release="${_OS_RELEASE_FILE}"
    _distro="" _codename=""
    if [ -f "${_os_release}" ]; then
        _distro="$(sed -n 's/^ID=//p' "${_os_release}" | head -n1 | tr -d '"')"
        _codename="$(sed -n 's/^VERSION_CODENAME=//p' "${_os_release}" | head -n1 | tr -d '"')"
    fi
    case "${_distro}" in
        ubuntu | debian) ;;
        *)
            echo "install-agent.sh --full: Docker Engine is not installed and automatic setup supports Ubuntu/Debian only (found '${_distro:-unknown}')." >&2
            echo "  Install Docker Engine and the compose plugin manually (https://docs.docker.com/engine/install/), then re-run without --full." >&2
            return 1
            ;;
    esac
    [ -n "${_codename}" ] \
        || {
            echo "install-agent.sh --full: could not read VERSION_CODENAME from ${_os_release}" >&2
            return 1
        }
    command -v apt-get >/dev/null 2>&1 \
        || {
            echo "install-agent.sh --full: apt-get not found; install Docker manually, then re-run without --full" >&2
            return 1
        }
    command -v curl >/dev/null 2>&1 || die_simple "required tool 'curl' is missing (apt-get install -y curl)"
    command -v gpg >/dev/null 2>&1 || {
        echo "==> installing ca-certificates curl gnupg for the Docker repository setup"
        ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl gnupg ) \
            || return 1
    }
    _apt_root="${_APT_ROOT}"
    _keyrings_dir="${_apt_root}/etc/apt/keyrings"
    # NOTE: the dearmored (binary) keyring must end in .gpg, not .asc: apt
    # treats a .asc signed-by file as ASCII-armored and silently ignores binary
    # content in it, failing later with NO_PUBKEY (proven on Ubuntu 22.04).
    _keyring_file="${_keyrings_dir}/docker.gpg"
    _source_file="${_apt_root}/etc/apt/sources.list.d/docker.list"
    # A Docker repository defined outside our own file (a docker.sources
    # entry, a docker.asc Signed-By line, another .list) is reused as-is:
    # adding our docker.list next to it makes apt fail with a Signed-By
    # conflict. Our own file from a previous run does not count: it is
    # rewritten below, so a partial run still refreshes the keyring.
    _existing_repo=""
    for _repo_file in "${_apt_root}/etc/apt/sources.list" "${_apt_root}/etc/apt/sources.list.d/"*.list "${_apt_root}/etc/apt/sources.list.d/"*.sources; do
        [ -f "${_repo_file}" ] || continue
        [ "${_repo_file}" = "${_source_file}" ] && continue
        if _docker_repo_has_active_entry "${_repo_file}"; then
            _existing_repo="${_repo_file}"
            break
        fi
    done
    if [ -n "${_existing_repo}" ]; then
        echo "==> existing Docker apt repository at ${_existing_repo}; reusing it instead of adding a conflicting source"
    else
        mkdir -p "${_keyrings_dir}" "$(dirname "${_source_file}")"
        chmod 0755 "${_keyrings_dir}" "$(dirname "${_source_file}")"
        # The architecture is read before the download so a broken dpkg fails
        # fast with a clear message instead of falling back to amd64, which
        # would write a repo line for the wrong architecture.
        if ! _arch="$(dpkg --print-architecture 2>/dev/null)" || [ -z "${_arch}" ]; then
            echo "install-agent.sh --full: could not determine the system architecture (dpkg --print-architecture failed)" >&2
            return 1
        fi
        # Resolve the codename against what the Docker repository actually
        # serves: probe .../dists/<codename>/Release first (VERSION_CODENAME
        # is authoritative for what this host is). An HTTP 404 (the repo does
        # not serve this suite: Debian testing/sid/unstable, an EOL suite, a
        # typo) falls back to the newest stable codename the repo serves; when
        # nothing is served, refuse with a clear message. A dead docker.list
        # is never written. A probe that cannot reach the repository at all
        # (network failure, unexpected HTTP status) refuses immediately with
        # the real error: falling back to a suite that was never confirmed
        # served would write a repo line on no evidence. The probe runs after
        # the arch check so a broken dpkg fails before any network.
        _probe_rc=0
        _docker_repo_serves "${_distro}" "${_codename}" || _probe_rc=$?
        case "${_probe_rc}" in
            0) ;;
            1)
                _fallback=""
                case "${_distro}" in
                    ubuntu) _stable_codenames="noble jammy focal" ;;
                    debian) _stable_codenames="trixie bookworm bullseye" ;;
                esac
                for _candidate in ${_stable_codenames}; do
                    _probe_rc=0
                    _docker_repo_serves "${_distro}" "${_candidate}" || _probe_rc=$?
                    case "${_probe_rc}" in
                        0)
                            _fallback="${_candidate}"
                            break
                            ;;
                        1) ;;
                        *)
                            echo "install-agent.sh --full: could not reach the Docker apt repository while probing '${_candidate}' for ${_distro}; refusing (not falling back on an unverified repository)" >&2
                            echo "  Install Docker Engine and the compose plugin manually (https://docs.docker.com/engine/install/), then re-run without --full." >&2
                            return 1
                            ;;
                    esac
                done
                if [ -z "${_fallback}" ]; then
                    echo "install-agent.sh --full: the Docker apt repository does not serve '${_codename}' for ${_distro}" >&2
                    echo "  (checked ${_docker_apt_key_url}/${_distro}/dists/${_codename}/Release) and no fallback suite is served either." >&2
                    echo "  Install Docker Engine and the compose plugin manually (https://docs.docker.com/engine/install/), then re-run without --full." >&2
                    return 1
                fi
                echo "==> the Docker apt repository does not serve '${_codename}'; falling back to '${_fallback}'"
                _codename="${_fallback}"
                ;;
            *)
                echo "install-agent.sh --full: could not reach the Docker apt repository while probing '${_codename}' for ${_distro}; refusing (not falling back on an unverified repository)" >&2
                echo "  Install Docker Engine and the compose plugin manually (https://docs.docker.com/engine/install/), then re-run without --full." >&2
                return 1
                ;;
        esac
        # No EXIT trap here: this library is sourced by install-agent.sh, whose own
        # EXIT trap owns the installer scratch dir; installing another one would
        # clobber it (I5). The temp key is removed on every path below instead.
        _key_tmp="$(mktemp "${TMPDIR:-/tmp}/docker-key.XXXXXX")" || return 1
        if ! curl -fSL --proto '=https' --tlsv1.2 --retry 3 --silent --show-error "${_docker_apt_key_url}/${_distro}/gpg" -o "${_key_tmp}"; then
            echo "install-agent.sh --full: could not download the Docker signing key" >&2
            rm -f "${_key_tmp}"
            return 1
        fi
        # Verify the key against the published fingerprint before trusting it: the
        # download channel alone is not the anchor. The fingerprint is field 10 of
        # the fpr record in --with-colons output.
        _key_fp="$(gpg --show-keys --with-colons "${_key_tmp}" 2>/dev/null | awk -F: '$1 == "fpr" { print $10; exit }' | tr -d '[:space:]')"
        case "${_key_fp}" in
            "${_docker_apt_key_fingerprint}")
                ;;
            *)
                echo "install-agent.sh --full: Docker signing key fingerprint mismatch (got '${_key_fp:-unreadable}'); refusing to use it" >&2
                rm -f "${_key_tmp}"
                return 1
                ;;
        esac
        # gpg refuses to overwrite an existing -o file, so dearmoring straight
        # into the keyring breaks a re-run after a partial run: dearmor into a
        # temp file in the same directory and move it into place atomically.
        _keyring_tmp="${_keyring_file}.tmp.$$"
        if ! gpg --dearmor -o "${_keyring_tmp}" "${_key_tmp}"; then
            echo "install-agent.sh --full: could not import the Docker signing key" >&2
            rm -f "${_key_tmp}" "${_keyring_tmp}"
            return 1
        fi
        rm -f "${_key_tmp}"
        chmod 0644 "${_keyring_tmp}"
        mv -f "${_keyring_tmp}" "${_keyring_file}"
        _repo_tmp="${_source_file}.tmp.$$"
        printf 'deb [arch=%s signed-by=%s] %s/%s %s stable\n' \
            "${_arch}" "${_keyring_file}" "${_docker_apt_key_url}" "${_distro}" "${_codename}" >"${_repo_tmp}" \
            || return 1
        chmod 0644 "${_repo_tmp}"
        mv -f "${_repo_tmp}" "${_source_file}"
    fi
    # A working engine whose compose plugin is missing gets only the plugin:
    # this branch must never name an engine package (docker-ce and friends can
    # make apt replace or remove the running engine, e.g. docker.io).
    if command -v docker >/dev/null 2>&1; then
        echo "==> installing only the Docker compose plugin (keeping the existing engine)"
        ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get update ) || return 1
        ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose-plugin ) \
            || ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose-v2 ) \
            || {
                echo "install-agent.sh --full: compose plugin installation failed" >&2
                return 1
            }
    else
        echo "==> installing Docker Engine and the compose plugin from the Docker apt repository"
        ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get update ) || return 1
        ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin ) \
            || {
                echo "install-agent.sh --full: Docker installation failed" >&2
                return 1
            }
    fi
    command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 \
        || {
            echo "install-agent.sh --full: Docker installed but 'docker compose version' does not work" >&2
            return 1
        }
    echo "==> Docker Engine and the compose plugin are ready"
    return 0
}

# die_simple aborts without depending on the caller's die (release-verify.sh is
# not necessarily sourced when this library is exercised standalone).
die_simple() {
    echo "install-agent.sh: $*" >&2
    exit 1
}
