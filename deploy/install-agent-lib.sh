#!/bin/sh
#
# install-agent-lib.sh holds the two pieces of install-agent.sh that carry
# reinstall behaviour, split out so they can be exercised without root or a
# systemd host (see deploy/test-agent-install.sh). It is sourced, not run.
#
# agent_env_write     composes /etc/gotham/agent.env, preserving values a prior
#                     install wrote for keys this invocation does not set.
# agent_service_restart  restarts an already-running agent on the new binary
#                     (starts it otherwise) and verifies the unit's ExecStart.

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
