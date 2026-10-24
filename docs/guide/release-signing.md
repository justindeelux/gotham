---
title: Release and signing
---

# Release and signing

This page is for maintainers. It describes how a signed Gotham release is built
and published, where the signing keys live, and how to test the install chain
without GitHub.

## What you need

- Write access to the repository and its GitHub **`release` environment**.
- The two environment secrets below, configured by a project owner.
- The `v*` tag ruleset and the required reviewers kept to project owners: that
  gate decides who can produce a signed release.

## How a release is built

Releases are built and published by `.github/workflows/release.yml` on a `v*`
tag, on a GitHub-hosted runner. The release job targets the GitHub **`release`
environment** (with a required reviewer), so it reads the environment secrets
and waits for the owner's approval before it can use them:

1. The job fails closed unless **both** environment secrets are set and
   non-empty: `GOTHAM_UPDATE_PUBLIC_KEY` (base64 raw Ed25519 public key,
   embedded into both binaries with `-ldflags`) and `GOTHAM_UPDATE_SIGNING_KEY`
   (PKCS#8 PEM Ed25519 private key, used only on the runner).
2. It also checks that the private key corresponds to
   `GOTHAM_UPDATE_PUBLIC_KEY` and to the committed
   `deploy/gotham-signing-key.pub` (and therefore to the key embedded in
   `deploy/install.sh`).
3. GoReleaser builds `gotham` and `gotham-agent` for `linux/amd64` and
   `linux/arm64` as raw binaries named `gotham-linux-<arch>` /
   `gotham-agent-linux-<arch>` (the names the update checkers resolve), embeds
   the version and public key, and creates a **draft** GitHub Release with the
   binaries, `checksums.txt` and the published public key.
4. The workflow signs the per-arch manifests (`gotham-manifest-<arch>.txt` and
   `gotham-agent-manifest-<arch>.txt`) over the exact built bytes. It locates
   the **draft** through the releases list (drafts are not returned by
   `/releases/tags/<tag>`), checks the 6 base assets GoReleaser uploaded,
   uploads the 8 manifest/signature files, then asserts the full 14-asset set
   (4 binaries, `checksums.txt`, the public key and all 8 manifest/signature
   files) before publishing the draft and verifying it is no longer a draft.
   Draft releases are skipped by the checker, so an incomplete release is never
   offered.

The asset naming is a contract with the update code — see
`internal/updates/checker.go` (control plane), `internal/updates/agents.go`
(agent) and `updatecore.ManifestName` / `ManifestNameWithPrefix`.

## Release runner

The release job and every PR workflow (`ci.yml`, `e2e.yml`, `ui-e2e.yml`) run on
GitHub-hosted runners, so untrusted `pull_request` code never executes on the
machine that builds and signs a release. The signing key stays in the `release`
environment, and a `v*` tag build only reads it after the required reviewer
approves the run. The reviewer gate therefore governs *who can produce a signed
release*: keep the `v*` tag ruleset and the required reviewers limited to
project owners.

## Keypair

- **Public key** (`deploy/gotham-signing-key.pub`): safe to publish. It is
  embedded in both binaries, in `deploy/install.sh`, and uploaded as a release
  asset so users can compare it.
- **Private key** (`GOTHAM_UPDATE_SIGNING_KEY` environment secret): the only
  secret. It is never committed or printed. **Back the private key up
  offline** — if it is lost no existing installation can verify a new release
  and a new keypair must be rotated in (which requires re-installing or
  embedding the new key in a manual release). Generate it with:

  ```sh
  go run ./cmd/signer keygen -out signing.key
  # put signing.key (PKCS#8 PEM) in the GOTHAM_UPDATE_SIGNING_KEY release environment secret
  # put signing.key.pub at deploy/gotham-signing-key.pub
  # put the base64 public key from the printed -ldflags line in
  # GOTHAM_UPDATE_PUBLIC_KEY and in deploy/install.sh
  # (GOTHAM_RELEASE_PUBLIC_KEY_B64) and install-agent.sh
  ```

## Testing the install chain without GitHub

`deploy/test-release-install.sh` builds a signer and snapshot binaries, signs
the manifests, serves them from a loopback fake releases server (including a
`/releases/latest` redirect), and runs `deploy/install.sh` against it under a
scratch `GOTHAM_INSTALL_ROOT`. It covers the pinned-tag path, the default
latest-tag resolution, the agent family, re-install with no operator additions
(the B1 regression), re-install preservation (operator keys and the managed
DSN), and the fail-closed cases (tampered artifact, tampered manifest,
pinned-key mismatch):

```sh
sh deploy/test-release-install.sh
```

Two installer variables exist only for this harness: `GOTHAM_INSTALL_ROOT`
installs under a non-`/` prefix and enables test mode (non-root), and
`GOTHAM_INSTALL_TEST_PUBLIC_KEY` replaces the pinned release key — it is
honoured only when `GOTHAM_INSTALL_ROOT` is set, otherwise the installer warns
and ignores it. Never set either on a real host.
