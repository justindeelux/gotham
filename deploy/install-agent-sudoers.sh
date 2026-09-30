#!/bin/sh
#
# install-agent-sudoers.sh grants the gotham-agent service user permission to
# run ONLY the fixed, root-owned agent update wrapper as root, with no
# arguments. Run as root after installing the agent (see deploy/README.md).
#
#   sudo deploy/install-agent-sudoers.sh [service-user]
#
# The wrapper restarts the fixed gotham-agent unit, health-checks it, rolls back
# on failure and records the outcome; granting the bare command with an empty
# argument list is what lets the unprivileged agent apply a signed self-update
# without a blanket systemctl or arbitrary-argument rule.
set -eu

SERVICE_USER="${1:-gotham-agent}"
WRAPPER="/usr/libexec/gotham/gotham-agent-update"
CONF="/etc/gotham/agent-updater.conf"
SUDOERS_FILE="/etc/sudoers.d/gotham-agent-update"

if [ "$(id -u)" -ne 0 ]; then
    echo "install-agent-sudoers.sh must run as root (try sudo)" >&2
    exit 1
fi

# Fail early when the sudo package is missing: the drop-in grants a sudoers
# rule, and the agent invokes the wrapper through `sudo -n`. Validate the
# drop-in with visudo before it is trusted.
for tool in sudo visudo; do
    command -v "${tool}" >/dev/null 2>&1 \
        || { echo "install-agent-sudoers.sh: '${tool}' not found (install the sudo package)" >&2; exit 1; }
done

case "${SERVICE_USER}" in
    "" | *[!A-Za-z0-9_-]*)
        echo "install-agent-sudoers.sh: invalid service user: ${SERVICE_USER}" >&2
        exit 2
        ;;
esac

if [ ! -x "${WRAPPER}" ]; then
    echo "install-agent-sudoers.sh: ${WRAPPER} is missing or not executable" >&2
    exit 1
fi
if [ ! -f "${CONF}" ]; then
    echo "install-agent-sudoers.sh: ${CONF} is missing" >&2
    exit 1
fi
if [ "$(stat -c '%U' "${WRAPPER}" 2>/dev/null || echo unknown)" != "root" ]; then
    echo "install-agent-sudoers.sh: ${WRAPPER} must be owned by root" >&2
    exit 1
fi
if [ "$(stat -c '%U' "${CONF}" 2>/dev/null || echo unknown)" != "root" ]; then
    echo "install-agent-sudoers.sh: ${CONF} must be owned by root" >&2
    exit 1
fi

# Minimal hosts and containers may not ship /etc/sudoers.d; create it with the
# sudoers(5) convention (root:root 0750) before writing the drop-in.
install -d -m 0750 /etc/sudoers.d

# sudoers(5): a command with no argument list permits any arguments. The empty
# string "" pins the invocation to zero arguments.
cat >"${SUDOERS_FILE}" <<EOF
# Allow the Gotham node agent to trigger its own update restart wrapper.
# The wrapper is root-owned and outside every writable path; "" pins it to zero
# arguments, so sudoers grants exactly the fixed command.
${SERVICE_USER} ALL=(root) NOPASSWD: ${WRAPPER} ""
EOF
chmod 0440 "${SUDOERS_FILE}"

visudo -cf "${SUDOERS_FILE}"

echo "installed ${SUDOERS_FILE}"
