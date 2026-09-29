#!/bin/sh
#
# install-sudoers.sh grants the gotham service user permission to run ONLY the
# update restart wrapper as root. Run as root after installing the control
# plane (see deploy/gotham.service).
#
#   sudo deploy/install-sudoers.sh [service-user]
#
# The wrapper restarts the unit, health-checks it, and rolls back on failure;
# granting it is what lets an unprivileged control plane apply a self-update
# without granting a blanket systemctl rule.
set -eu

SERVICE_USER="${1:-gotham}"
WRAPPER="/usr/local/bin/gotham-update"
SUDOERS_FILE="/etc/sudoers.d/gotham-update"

if [ "$(id -u)" -ne 0 ]; then
    echo "install-sudoers.sh must run as root (try sudo)" >&2
    exit 1
fi

cat >"${SUDOERS_FILE}" <<EOF
# Allow the Gotham control plane to trigger its own update restart wrapper.
${SERVICE_USER} ALL=(root) NOPASSWD: ${WRAPPER}
EOF
chmod 0440 "${SUDOERS_FILE}"

if command -v visudo >/dev/null 2>&1; then
    visudo -cf "${SUDOERS_FILE}"
fi

echo "installed ${SUDOERS_FILE}"
