#!/bin/sh
#
# release-verify.sh: shared, fail-closed download + signature + digest
# verification for the Gotham release assets. Sourced by install.sh and
# install-agent.sh (they are run from a repository checkout, so this file sits
# next to them).
#
# Trust model
# -----------
# The Ed25519 release public key is the trust anchor. It is embedded in the
# installer (a base64 constant, or the PEM in deploy/gotham-signing-key.pub)
# and never fetched from the same channel as the artifact, so nothing is
# trusted on first use. A release publishes, per architecture:
#
#   <family>linux-<arch>                     the raw binary
#   <manifest-prefix><arch>.txt              version/channel/arch/file/sha256
#   <manifest-prefix><arch>.txt.sig          detached Ed25519 signature (base64)
#
# where <manifest-prefix> is gotham-manifest- for the control plane and
# gotham-agent-manifest- for the agent (updatecore.ManifestName /
# ManifestNameWithPrefix; see internal/updates/checker.go and agents.go).
#
# verify_release checks, in order:
#   1. the manifest signature against the pinned public key;
#   2. the manifest binds the requested version, arch and file name;
#   3. the downloaded binary digest matches the signed manifest sha256.
# Any failure aborts the caller (exit 1).

# die prints an error, removes any verification scratch dir, and exits nonzero.
die() {
    [ -n "${_GOTHAM_VERIFY_WORK:-}" ] && rm -rf "${_GOTHAM_VERIFY_WORK}" || true
    echo "gotham-install: $*" >&2
    exit 1
}

# require_cmd fails when a required tool is missing.
require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "required tool '$1' not found (install: $2)"
}

# sha256_file prints the lowercase hex SHA-256 of a file.
sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        # macOS/BSD fallback; GNU coreutils is the Ubuntu path.
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

# materialize_public_key writes a PEM public key to $2 from $1, which may be a
# PKIX PEM block or a base64-encoded raw 32-byte Ed25519 key (the format
# `cmd/signer keygen` prints for -ldflags embedding).
materialize_public_key() {
    input=$1
    out=$2
    case "${input}" in
        *"BEGIN PUBLIC KEY"*)
            printf '%s\n' "${input}" >"${out}"
            ;;
        *)
            raw="${out}.raw"
            der="${out}.der"
            printf '%s' "${input}" | base64 -d >"${raw}" 2>/dev/null \
                || die "public key is neither a PEM block nor valid base64"
            [ "$(wc -c <"${raw}" | tr -d ' ')" -eq 32 ] \
                || die "base64 public key is not 32 bytes"
            # Prefix the raw key with the fixed Ed25519 PKIX SubjectPublicKeyInfo
            # header to get a DER key openssl can read.
            {
                printf '\060\052\060\005\006\003\053\145\160\003\041\000'
                cat "${raw}"
            } >"${der}"
            openssl pkey -pubin -inform DER -in "${der}" -out "${out}" \
                || die "could not decode the public key"
            rm -f "${raw}" "${der}"
            ;;
    esac
    [ -s "${out}" ] || die "public key is empty"
}

# manifest_get prints the value of a key from a manifest, or nothing.
manifest_get() {
    # $1 = key, $2 = manifest file
    sed -n "s/^$1=//p" "$2" | head -n 1
}

# verify_release downloads, verifies and installs a signed release binary into
# $6 and echoes its path. Arguments:
#   $1 base URL (no trailing slash), e.g. https://github.com/o/r/releases/download/v0.1.0
#   $2 version as the manifest carries it (e.g. v0.1.0)
#   $3 arch (amd64|arm64)
#   $4 family ("" for the control plane, "gotham-agent" for the agent)
#   $5 PEM public key path
#   $6 destination path for the binary
verify_release() {
    base_url=$1
    version=$2
    arch=$3
    family=$4
    pubkey=$5
    dest=$6

    case "${family}" in
        "")
            asset="gotham-linux-${arch}"
            manifest_prefix="gotham-manifest-"
            ;;
        gotham-agent)
            asset="gotham-agent-linux-${arch}"
            manifest_prefix="gotham-agent-manifest-"
            ;;
        *)
            die "internal: unknown release family '${family}'"
            ;;
    esac
    manifest="${manifest_prefix}${arch}.txt"

    work=$(mktemp -d "${TMPDIR:-/tmp}/gotham-verify.XXXXXX") \
        || die "could not create a temporary directory"
    # die() removes this on any failure; cleared once the work dir is gone.
    # (No trap: the caller may have its own EXIT trap we must not clobber.)
    _GOTHAM_VERIFY_WORK="${work}"

    manifest_file="${work}/${manifest}"
    sig_file="${manifest_file}.sig"
    raw_sig="${work}/signature.raw"
    artifact="${work}/${asset}"

    curl -fsSL --retry 3 --retry-delay 2 -o "${manifest_file}" "${base_url}/${manifest}" \
        || die "download failed: ${base_url}/${manifest}"
    curl -fsSL --retry 3 --retry-delay 2 -o "${sig_file}" "${base_url}/${manifest}.sig" \
        || die "download failed: ${base_url}/${manifest}.sig"

    base64 -d <"${sig_file}" >"${raw_sig}" 2>/dev/null \
        || die "manifest signature is not valid base64"
    [ "$(wc -c <"${raw_sig}" | tr -d ' ')" -eq 64 ] \
        || die "manifest signature is not a 64-byte Ed25519 signature"
    openssl pkeyutl -verify -pubin -inkey "${pubkey}" -rawin \
        -in "${manifest_file}" -sigfile "${raw_sig}" >/dev/null 2>&1 \
        || die "manifest signature verification FAILED (refusing to install)"

    m_version=$(manifest_get version "${manifest_file}")
    m_arch=$(manifest_get arch "${manifest_file}")
    m_file=$(manifest_get file "${manifest_file}")
    m_sha=$(manifest_get sha256 "${manifest_file}")

    [ "${m_version}" = "${version}" ] || die "manifest version '${m_version}' != requested '${version}'"
    [ "${m_arch}" = "${arch}" ] || die "manifest arch '${m_arch}' != '${arch}'"
    [ "${m_file}" = "${asset}" ] || die "manifest file '${m_file}' != '${asset}'"
    case "${m_sha}" in
        *[!0-9a-fA-F]* | "") die "manifest has no valid sha256" ;;
    esac
    [ "${#m_sha}" -eq 64 ] || die "manifest sha256 is not 64 hex characters"

    curl -fsSL --retry 3 --retry-delay 2 -o "${artifact}" "${base_url}/${asset}" \
        || die "download failed: ${base_url}/${asset}"

    got=$(sha256_file "${artifact}")
    [ "${got}" = "${m_sha}" ] \
        || die "artifact digest mismatch: manifest ${m_sha}, downloaded ${got}"

    mv "${artifact}" "${dest}" || die "could not place the verified binary at ${dest}"
    chmod 0755 "${dest}"
    rm -rf "${work}"
    _GOTHAM_VERIFY_WORK=""
    echo "${dest}"
}