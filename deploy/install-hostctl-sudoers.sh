#!/bin/sh
#
# install-hostctl-sudoers.sh grants the gotham service user permission to run
# the fixed, root-owned host helper with exactly its five verbs, as root. Run as
# root after installing the helper (see deploy/README.md):
#
#   sudo deploy/install-hostctl-sudoers.sh [service-user]
#
# Same privilege model as install-sudoers.sh: every rule pins the verb, so the
# service user cannot pass any other argument; values travel on stdin and are
# re-validated by the helper.
set -eu

SERVICE_USER="${1:-gotham}"
HELPER="/usr/libexec/gotham/gotham-hostctl"
SUDOERS_FILE="/etc/sudoers.d/gotham-hostctl"

if [ "$(id -u)" -ne 0 ]; then
    echo "install-hostctl-sudoers.sh must run as root (try sudo)" >&2
    exit 1
fi
for tool in sudo visudo; do
    command -v "${tool}" >/dev/null 2>&1 \
        || { echo "install-hostctl-sudoers.sh: '${tool}' not found (install the sudo package)" >&2; exit 1; }
done
case "${SERVICE_USER}" in
    "" | *[!A-Za-z0-9_-]*)
        echo "install-hostctl-sudoers.sh: invalid service user: ${SERVICE_USER}" >&2
        exit 2
        ;;
esac
if [ ! -x "${HELPER}" ] || [ "$(stat -c '%U' "${HELPER}" 2>/dev/null || echo unknown)" != "root" ]; then
    echo "install-hostctl-sudoers.sh: ${HELPER} must exist, be executable and be owned by root" >&2
    exit 1
fi

install -d -m 0750 /etc/sudoers.d
TMP="$(mktemp /etc/sudoers.d/.gotham-hostctl.XXXXXX)" \
    || { echo "install-hostctl-sudoers.sh: could not create a temp file in /etc/sudoers.d" >&2; exit 1; }
{
    echo "# Allow the Gotham control plane to run its fixed host helper verbs."
    for verb in status apply-network confirm-network revert-network apply-system; do
        echo "${SERVICE_USER} ALL=(root) NOPASSWD: ${HELPER} ${verb}"
    done
} >"${TMP}"
chmod 0440 "${TMP}"
if ! visudo -cf "${TMP}"; then
    rm -f "${TMP}"
    echo "install-hostctl-sudoers.sh: generated drop-in failed visudo; nothing installed" >&2
    exit 1
fi
mv -f "${TMP}" "${SUDOERS_FILE}"
visudo -cf "${SUDOERS_FILE}"
echo "installed ${SUDOERS_FILE}"
