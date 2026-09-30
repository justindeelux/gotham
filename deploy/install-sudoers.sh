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

# Fail early when the sudo package is missing: the drop-in grants a sudoers
# rule, and the control plane invokes the wrapper through `sudo -n`. Validate
# the drop-in with visudo before it is trusted.
for tool in sudo visudo; do
    command -v "${tool}" >/dev/null 2>&1 \
        || { echo "install-sudoers.sh: '${tool}' not found (install the sudo package)" >&2; exit 1; }
done

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

# Minimal hosts and containers may not ship /etc/sudoers.d; create it with the
# sudoers(5) convention (root:root 0750) before writing the drop-in.
install -d -m 0750 /etc/sudoers.d

# sudoers(5): a command with no argument list permits any arguments. The empty
# string "" pins the invocation to zero arguments.
#
# Write the drop-in to a temp file and validate it BEFORE it lands in
# /etc/sudoers.d: a broken file there could disable sudo on the host. The move
# is atomic within the directory and the final path is re-checked.
SUDOERS_TMP="$(mktemp /etc/sudoers.d/.gotham-update.XXXXXX)" \
    || { echo "install-sudoers.sh: could not create a temp file in /etc/sudoers.d" >&2; exit 1; }
cat >"${SUDOERS_TMP}" <<EOF
# Allow the Gotham control plane to trigger its own update restart wrapper.
# The wrapper is root-owned and outside every writable path; "" pins it to zero
# arguments, so sudoers grants exactly the fixed command.
${SERVICE_USER} ALL=(root) NOPASSWD: ${WRAPPER} ""
EOF
chmod 0440 "${SUDOERS_TMP}"
if ! visudo -cf "${SUDOERS_TMP}"; then
    rm -f "${SUDOERS_TMP}"
    echo "install-sudoers.sh: generated drop-in failed visudo; nothing installed" >&2
    exit 1
fi
mv -f "${SUDOERS_TMP}" "${SUDOERS_FILE}"
visudo -cf "${SUDOERS_FILE}"

echo "installed ${SUDOERS_FILE}"
