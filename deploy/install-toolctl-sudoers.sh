#!/bin/sh
#
# install-toolctl-sudoers.sh grants the gotham-agent service user permission to
# run ONLY the fixed, root-owned host-tool helper, one rule per verb and tool id
# (no wildcards). Run as root after installing the helper (see deploy/install-agent.sh):
#
#   sudo deploy/install-toolctl-sudoers.sh [service-user]
#
# Same privilege model as the host-helper drop-in written by
# gotham-hostbootstrap.sh: every rule pins the verb
# and the catalog id, so the service user cannot pass any other argument. The
# helper itself validates the id against the same allow-list and owns every
# executable argv.
set -eu

SERVICE_USER="${1:-gotham-agent}"
# Test seam: the hostbootstrap fake-root test passes this when it runs without
# sudo. Production is always "/" and the value is ignored under sudo.
ROOT=""
if [ -z "${SUDO_USER:-}${SUDO_UID:-}" ]; then
    ROOT="${GOTHAM_HOST_BOOTSTRAP_ROOT:-}"
fi
HELPER="${ROOT}/usr/libexec/gotham/gotham-toolctl"
SUDOERS_DIR="${ROOT}/etc/sudoers.d"
SUDOERS_FILE="${SUDOERS_DIR}/gotham-toolctl"

# The exact verb/id pairs the agent may invoke. Keep in sync with
# agent/tools/server.go (helperInstallVerb/helperUninstallVerb) and the
# allow-lists in deploy/gotham-toolctl.sh.
INSTALL_IDS="railpack buildkit pack docker git systemd-networkd systemd-resolved resolvectl"
UNINSTALL_IDS="railpack buildkit pack git"

if [ -z "${ROOT}" ] && [ "$(id -u)" -ne 0 ]; then
    echo "install-toolctl-sudoers.sh must run as root (try sudo)" >&2
    exit 1
fi
for tool in sudo visudo; do
    command -v "${tool}" >/dev/null 2>&1 \
        || { echo "install-toolctl-sudoers.sh: '${tool}' not found (install the sudo package)" >&2; exit 1; }
done
case "${SERVICE_USER}" in
    "" | *[!A-Za-z0-9_-]*)
        echo "install-toolctl-sudoers.sh: invalid service user: ${SERVICE_USER}" >&2
        exit 2
        ;;
esac
if [ ! -x "${HELPER}" ]; then
    echo "install-toolctl-sudoers.sh: ${HELPER} must exist and be executable" >&2
    exit 1
fi
if [ -z "${ROOT}" ] && [ "$(stat -c '%U' "${HELPER}" 2>/dev/null || echo unknown)" != "root" ]; then
    echo "install-toolctl-sudoers.sh: ${HELPER} must be owned by root" >&2
    exit 1
fi

install -d -m 0750 "${SUDOERS_DIR}"
TMP="$(mktemp "${SUDOERS_DIR}/.gotham-toolctl.XXXXXX")" \
    || { echo "install-toolctl-sudoers.sh: could not create a temp file in ${SUDOERS_DIR}" >&2; exit 1; }
{
    echo "# Allow the Gotham node agent to run its fixed host-tool helper."
    for id in ${INSTALL_IDS}; do
        echo "${SERVICE_USER} ALL=(root) NOPASSWD: ${HELPER} install-tool ${id}"
    done
    for id in ${UNINSTALL_IDS}; do
        echo "${SERVICE_USER} ALL=(root) NOPASSWD: ${HELPER} uninstall-tool ${id}"
    done
} >"${TMP}"
chmod 0440 "${TMP}"
if ! visudo -cf "${TMP}"; then
    rm -f "${TMP}"
    echo "install-toolctl-sudoers.sh: generated drop-in failed visudo; nothing installed" >&2
    exit 1
fi
mv -f "${TMP}" "${SUDOERS_FILE}"
visudo -cf "${SUDOERS_FILE}"
echo "installed ${SUDOERS_FILE}"
