#!/bin/sh
#
# install-sudoers.sh grants the gotham service user permission to run ONLY the
# fixed, root-owned update wrapper as root, with no arguments. Run as root after
# installing the control plane (see deploy/gotham.service and deploy/README.md).
#
#   sudo deploy/install-sudoers.sh [service-user]
#
# The wrapper restarts the fixed service, health-checks it, rolls back on
# failure and records the outcome; granting the bare command with an empty
# argument list is what lets an unprivileged control plane apply a self-update
# without a blanket systemctl or arbitrary-argument rule.
set -eu

SERVICE_USER="${1:-gotham}"
WRAPPER="/usr/libexec/gotham/gotham-update"
CONF="/etc/gotham/updater.conf"
SUDOERS_FILE="/etc/sudoers.d/gotham-update"

if [ "$(id -u)" -ne 0 ]; then
    echo "install-sudoers.sh must run as root (try sudo)" >&2
    exit 1
fi

case "${SERVICE_USER}" in
    "" | *[!A-Za-z0-9_-]*)
        echo "install-sudoers.sh: invalid service user: ${SERVICE_USER}" >&2
        exit 2
        ;;
esac

if [ ! -x "${WRAPPER}" ]; then
    echo "install-sudoers.sh: ${WRAPPER} is missing or not executable" >&2
    exit 1
fi
if [ ! -f "${CONF}" ]; then
    echo "install-sudoers.sh: ${CONF} is missing" >&2
    exit 1
fi
if [ "$(stat -c '%U' "${WRAPPER}" 2>/dev/null || echo unknown)" != "root" ]; then
    echo "install-sudoers.sh: ${WRAPPER} must be owned by root" >&2
    exit 1
fi
if [ "$(stat -c '%U' "${CONF}" 2>/dev/null || echo unknown)" != "root" ]; then
    echo "install-sudoers.sh: ${CONF} must be owned by root" >&2
    exit 1
fi

# sudoers(5): a command with no argument list permits any arguments. The empty
# string "" pins the invocation to zero arguments.
cat >"${SUDOERS_FILE}" <<EOF
# Allow the Gotham control plane to trigger its own update restart wrapper.
# The wrapper is root-owned and outside every writable path; "" pins it to zero
# arguments, so sudoers grants exactly the fixed command.
${SERVICE_USER} ALL=(root) NOPASSWD: ${WRAPPER} ""
EOF
chmod 0440 "${SUDOERS_FILE}"

if command -v visudo >/dev/null 2>&1; then
    visudo -cf "${SUDOERS_FILE}"
fi

echo "installed ${SUDOERS_FILE}"
