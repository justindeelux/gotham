// Package updates implements control-plane self-update and remote agent
// updates.
//
// The control-plane update chain is deliberately small and fail-closed:
//
//  1. Checker queries the GitHub Releases API for the configured channel and
//     resolves the newest release above the running version.
//  2. Applier downloads the platform asset, verifies its detached Ed25519
//     signature with the public key embedded in this binary, verifies the
//     SHA-256 checksum, and only then swaps the executable via minio/selfupdate
//     (keeping the previous binary at <binary>.old).
//  3. The deployed restart wrapper (deploy/gotham-update.sh) restarts the
//     service, health-checks the new binary, and restores <binary>.old when the
//     new binary fails.
//
// No private key is ever read, logged or embedded here: only the public key.
// With no embedded public key (or FEATURE_UPDATES=false) the surface mounts
// nothing and refuses to apply an artifact.
package updates
