#!/bin/sh
#
# gotham-hostbootstrap-fetch: fetch and verify the signed host-helper payload for
# the root update wrapper (JUS-188).
#
# The control plane never stages the payload: this script is run by the
# root-owned update wrapper, downloads the release's signed manifest, its
# detached Ed25519 signature and the helper payload archive into a root-only
# staging directory, verifies the manifest signature against the root-owned
# release public key, checks the payload digest against the signed manifest, and
# only then extracts the payload. No byte the service or agent user can write is
# read as code: the release is the only source, and it is signature-verified
# before use.
#
#   gotham-hostbootstrap-fetch <release-base> <version> <arch> <key> <dest-dir> <staged-binary>
#
# <release-base> a URL with no trailing slash (e.g.
# https://github.com/o/r/releases/download); <version> the candidate release tag
# (v...), a locator only; <arch> amd64|arm64; <key> the root-owned PEM Ed25519
# release public key; <dest-dir> an existing root-owned mode-0700 directory;
# <staged-binary> the binary the wrapper is about to restart. On success the
# extracted payload directory is printed on stdout and the signed manifest
# version is written to <dest-dir>/version; on failure the reason goes to stderr,
# the exit status is nonzero and the caller applies nothing.
#
# Anti-rollback: the candidate <version> only locates the release. The version
# that matters is read from the signature-verified manifest, and it must equal
# the candidate *and* the manifest must describe <staged-binary> (its sha256), so
# a compromised control plane cannot point root at a payload that did not ship
# with the binary being installed. The root wrapper then refuses the signed
# version when it is below a root-owned high-water mark, comparing it in
# already-installed code before it runs the payload (gotham-hostbootstrap.sh); a
# missing mark refuses too.
#
# The transport is not the trust boundary: the signature and the signed digests
# are, so a mirror (or a file:// URL in tests) is safe. The version is
# restricted to the tag charset so it cannot escape the release URL path.
set -eu
umask 077

# Cap any single download so a hostile or corrupt mirror cannot fill the root
# staging filesystem (the payload is a few KiB).
MAX_BYTES=10485760

die() { echo "gotham-hostbootstrap-fetch: $*" >&2; exit 1; }

[ "$#" -eq 6 ] || die "usage: gotham-hostbootstrap-fetch <release-base> <version> <arch> <key> <dest-dir> <staged-binary>"
release_base="$1"
version="$2"
arch="$3"
key="$4"
dest="$5"
staged_binary="$6"

case "${version}" in
    "" | *[!A-Za-z0-9._+-]*) die "invalid release version: ${version}" ;;
esac
case "${arch}" in
    amd64 | arm64) ;;
    *) die "unsupported architecture: ${arch}" ;;
esac
case "${release_base}" in
    "" | *[[:space:]]*) die "invalid release base URL" ;;
esac

# The staging directory must already exist, be owned by us, not be a symlink and
# be closed to group/other: root extracts into it and runs from it, so a
# service-writable staging directory would be a TOCTOU hole.
[ -d "${dest}" ] || die "staging directory ${dest} does not exist"
[ -L "${dest}" ] && die "refusing symlinked staging directory ${dest}"
# test -O is supported by dash/bash (the shells these scripts run under), as in
# gotham-update.sh and gotham-hostbootstrap.sh.
# shellcheck disable=SC3067
[ -O "${dest}" ] || die "staging directory ${dest} is not owned by the caller"
dest_mode="$(stat -c '%a' "${dest}" 2>/dev/null || stat -f '%Lp' "${dest}" 2>/dev/null)" \
    || die "cannot stat the staging directory ${dest}"
case "${dest_mode}" in
    700 | 0700) ;;
    *) die "staging directory ${dest} must be mode 0700 (got ${dest_mode})" ;;
esac

[ -f "${key}" ] || die "release public key ${key} is not a regular file"
[ -L "${key}" ] && die "refusing symlinked release public key ${key}"

# The binary the wrapper is about to restart is what the helper payload must
# belong to; its digest is compared with the signed manifest below.
[ -f "${staged_binary}" ] || die "staged binary ${staged_binary} is not a regular file"
[ -L "${staged_binary}" ] && die "refusing symlinked staged binary ${staged_binary}"

for tool in curl openssl tar; do
    command -v "${tool}" >/dev/null 2>&1 || die "'${tool}' is required"
done

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

manifest="gotham-manifest-${arch}.txt"
signature="${manifest}.sig"
base_url="${release_base}/${version}"

fetch() { # $1 url, $2 destination path
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 10 --max-time 60 \
        --max-filesize "${MAX_BYTES}" -o "$2" "$1" || die "download failed: $1"
}

fetch "${base_url}/${manifest}" "${dest}/${manifest}"
fetch "${base_url}/${signature}" "${dest}/${signature}"

# 1. Verify the detached Ed25519 signature over the exact manifest bytes with the
#    root-owned key. The signature is standard base64.
base64 -d <"${dest}/${signature}" >"${dest}/signature.raw" 2>/dev/null \
    || die "manifest signature is not valid base64"
[ "$(wc -c <"${dest}/signature.raw" | tr -d ' ')" -eq 64 ] \
    || die "manifest signature is not a 64-byte Ed25519 signature"
openssl pkeyutl -verify -pubin -inkey "${key}" -rawin \
    -in "${dest}/${manifest}" -sigfile "${dest}/signature.raw" >/dev/null 2>&1 \
    || die "manifest signature verification FAILED (refusing to apply the payload)"

# 2. Bind the signed manifest to the release identity, to the exact staged
#    binary, and to the payload name. Every field here is inside the signed
#    bytes: a field cannot be stripped, added or altered without invalidating
#    the signature checked in step 1.
manifest_get() { sed -n "s/^$1=//p" "${dest}/${manifest}" | head -n 1; }
m_version="$(manifest_get version)"
[ "${m_version}" = "${version}" ] || die "manifest version does not match ${version}"
[ "$(manifest_get arch)" = "${arch}" ] || die "manifest arch does not match ${arch}"
m_sha="$(manifest_get sha256)"
case "${m_sha}" in
    "" | *[!0-9a-fA-F]*) die "manifest has no valid binary sha256" ;;
esac
[ "${#m_sha}" -eq 64 ] || die "manifest binary sha256 is not 64 hex characters"
staged_sha="$(sha256_file "${staged_binary}")"
[ "${staged_sha}" = "${m_sha}" ] \
    || die "manifest does not describe the staged binary: manifest ${m_sha}, staged ${staged_sha}"
helper_file="$(manifest_get helper_file)"
helper_sha="$(manifest_get helper_sha256)"
if [ -z "${helper_file}" ] && [ -z "${helper_sha}" ]; then
    # A release that ships no helper payload. That is not a downgrade: there is
    # nothing to apply, so exit nonzero and let the caller no-op (it must never
    # fall back to applying unverified bytes).
    die "manifest carries no host-helper payload binding (nothing to apply)"
fi
[ "${helper_file}" = "gotham-hosthelpers.tar.gz" ] || die "manifest helper_file is not gotham-hosthelpers.tar.gz"
case "${helper_sha}" in
    "" | *[!0-9a-fA-F]*) die "manifest has no valid helper_sha256" ;;
esac
[ "${#helper_sha}" -eq 64 ] || die "manifest helper_sha256 is not 64 hex characters"

# 3. Fetch the payload named by the signed manifest and check its digest.
fetch "${base_url}/${helper_file}" "${dest}/helpers.tar.gz"
got="$(sha256_file "${dest}/helpers.tar.gz")"
[ "${got}" = "${helper_sha}" ] \
    || die "helper payload digest mismatch: manifest ${helper_sha}, downloaded ${got}"

# 4. Extract into an empty payload directory. Refuse an entry that is absolute or
#    escapes the directory, any entry that is not a regular file (symlink,
#    hardlink, device, fifo, directory) and any duplicate name, so a hostile
#    archive cannot write outside the root-only staging directory, plant a link
#    that is later run as code, or shadow a payload file.
tar -tzf "${dest}/helpers.tar.gz" >"${dest}/entries" || die "helper payload is not a readable tar archive"
if grep -Eq '^/|(^|/)\.\.(/|$)' "${dest}/entries"; then
    die "helper payload contains an unsafe path"
fi
if [ -n "$(sort "${dest}/entries" | uniq -d)" ]; then
    die "helper payload contains a duplicate entry"
fi
tar -tvzf "${dest}/helpers.tar.gz" >"${dest}/entries.verbose" || die "helper payload is not a readable tar archive"
if grep -qv '^-' "${dest}/entries.verbose"; then
    die "helper payload contains a non-regular entry"
fi
mkdir -p "${dest}/payload"
tar -xzf "${dest}/helpers.tar.gz" -C "${dest}/payload" \
    --no-same-owner --no-same-permissions \
    || die "could not extract the helper payload"

for f in gotham-hostctl gotham-toolctl gotham-hostbootstrap.sh; do
    [ -f "${dest}/payload/${f}" ] || die "helper payload is missing ${f}"
    [ -L "${dest}/payload/${f}" ] && die "refusing symlinked ${f} in the helper payload"
done

# The signature-verified version, for the caller's high-water-mark check.
printf '%s\n' "${m_version}" >"${dest}/version"

printf '%s' "${dest}/payload"
