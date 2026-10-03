#!/bin/sh
#
# install-agent-lib.sh holds the pieces of install-agent.sh that carry
# reinstall behaviour or need root-free exercise, split out so they can be
# tested without root or a systemd host (see deploy/test-agent-install.sh).
# It is sourced, not run.
#
# agent_env_write     composes /etc/gotham/agent.env, preserving values a prior
#                     install wrote for keys this invocation does not set.
# agent_service_restart  restarts an already-running agent on the new binary
#                     (starts it otherwise) and verifies the unit's ExecStart.
# ensure_docker_full  installs Docker Engine + the compose plugin from the
#                     official Docker apt repository (--full; Ubuntu/Debian
#                     only, idempotent, fingerprint-pinned key).

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

# Docker apt repository pinned for --full installs. Packages are verified by
# apt against this keyring; the key itself is fetched over HTTPS and checked
# against the published fingerprint below, never piped to a shell.
_docker_apt_key_url="https://download.docker.com/linux"
_docker_apt_key_fingerprint="9DC858229FC7DD38854AE2D88D81803C0EBFCD88"

# ensure_docker_full installs Docker Engine and the compose plugin from the
# official Docker apt repository (Ubuntu/Debian only). It is idempotent: when
# `docker` and `docker compose` already work it only logs and returns 0. On
# any other distro it fails with a message naming the manual step instead of
# attempting an unverified install. Test seam: GOTHAM_OS_RELEASE_FILE overrides
# /etc/os-release; GOTHAM_APT_ROOT prefixes the apt paths (/etc/apt/...).
ensure_docker_full() {
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        echo "==> Docker Engine and the compose plugin are already installed; skipping"
        return 0
    fi
    _os_release="${GOTHAM_OS_RELEASE_FILE:-/etc/os-release}"
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
    _apt_root="${GOTHAM_APT_ROOT:-}"
    _keyrings_dir="${_apt_root}/etc/apt/keyrings"
    # NOTE: the dearmored (binary) keyring must end in .gpg, not .asc: apt
    # treats a .asc signed-by file as ASCII-armored and silently ignores binary
    # content in it, failing later with NO_PUBKEY (proven on Ubuntu 22.04).
    _keyring_file="${_keyrings_dir}/docker.gpg"
    _source_file="${_apt_root}/etc/apt/sources.list.d/docker.list"
    mkdir -p "${_keyrings_dir}" "$(dirname "${_source_file}")"
    chmod 0755 "${_keyrings_dir}" "$(dirname "${_source_file}")"
    # No EXIT trap here: this library is sourced by install-agent.sh, whose own
    # EXIT trap owns the installer scratch dir; installing another one would
    # clobber it (I5). The temp key is removed on every path below instead.
    _key_tmp="$(mktemp "${TMPDIR:-/tmp}/docker-key.XXXXXX")" || return 1
    if ! curl -fsSL "${_docker_apt_key_url}/${_distro}/gpg" -o "${_key_tmp}"; then
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
    if ! gpg --dearmor -o "${_keyring_file}" "${_key_tmp}"; then
        echo "install-agent.sh --full: could not import the Docker signing key" >&2
        rm -f "${_key_tmp}"
        return 1
    fi
    rm -f "${_key_tmp}"
    chmod 0644 "${_keyring_file}"
    _arch="$(dpkg --print-architecture 2>/dev/null || echo amd64)"
    _repo_tmp="${_source_file}.tmp.$$"
    printf 'deb [arch=%s signed-by=%s] %s/%s %s stable\n' \
        "${_arch}" "${_keyring_file}" "${_docker_apt_key_url}" "${_distro}" "${_codename}" >"${_repo_tmp}" \
        || return 1
    chmod 0644 "${_repo_tmp}"
    mv -f "${_repo_tmp}" "${_source_file}"
    echo "==> installing Docker Engine and the compose plugin from the Docker apt repository"
    ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get update ) || return 1
    ( umask 022; DEBIAN_FRONTEND=noninteractive apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin ) \
        || {
            echo "install-agent.sh --full: Docker installation failed" >&2
            return 1
        }
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
