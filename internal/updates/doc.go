// Package updates implements control-plane self-update and remote agent
// updates.
//
// The control-plane update chain is fail-closed:
//
//  1. Checker queries the GitHub Releases API for the configured channel and
//     resolves the newest release above the running version, together with the
//     platform asset and its signed manifest.
//  2. Applier downloads the manifest, verifies its detached Ed25519 signature
//     with the public key embedded in this binary, and binds the release
//     identity (version, channel, arch, file) and artifact SHA-256 to it. Only
//     then does it fetch the artifact and swap the executable, serialized by a
//     process-external lock, keeping the previous binary at <binary>.old.
//  3. The root-owned restart wrapper (deploy/gotham-update.sh) restarts the
//     service, health-checks the new binary, restores <binary>.old when the new
//     binary fails, and records the durable outcome the control plane reports.
//
// No private key is ever read, logged or embedded here: only the public key.
// With no embedded public key (or FEATURE_UPDATES=false) the surface mounts
// nothing and refuses to apply an artifact. The privileged wrapper lives
// outside every path the control plane can write; see deploy/README.md.
package updates
