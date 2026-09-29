#!/bin/sh
#
# install-sudoers.sh grants the gotham service user permission to run ONLY the
# fixed, root-owned update wrapper as root, with no arguments. Run as root after
# installing the control plane (see deploy/gotham.service and deploy/README.md).
#
#   sudo deploy/install-sudoers.sh [service-user]
#
# The wrapper restarts the fixed service, health-checks it, rolls back on
# failure and records the outcome; granting the bare command is what lets an
# unprivileged control plane apply a self-update without a blanket systemctl or
# arbitrary-argument rule.
set -eu

SERVICE_USER="${1:-gotham}"
WRAPPER="/usr/libexec/gotham/gotham-update"
SUDOERS_FILE="/etc/sudoers.d/gotham-update"

if [ "$(id -u)" -ne 0 ]; then
    echo "install-sudoers.sh must run as root (try sudo)" >&2
    exit 1
fi

if [ ! -x "${WRAPPER}" ]; then
    echo "install-sudoers.sh: ${WRAPPER} is missing or not executable" >&2
    exit 1
fi

cat >"${SUDOERS_FILE}" <<EOF
# Allow the Gotham control plane to trigger its own update restart wrapper.
# The wrapper is root-owned and outside every writable path; no arguments are
# permitted, so sudoers grants exactly the fixed command.
${SERVICE_USER} ALL=(root) NOPASSWD: ${WRAPPER}
EOF
chmod 0440 "${SUDOERS_FILE}"

if command -v visudo >/dev/null 2>&1; then
    visudo -cf "${SUDOERS_FILE}"
fi

echo "installed ${SUDOERS_FILE}"
