# Release signing key rotation

The release trust anchor is an Ed25519 public key compiled into every built
binary. This runbook covers the embedded current + next key ring, planned
rotations, and what is (and is not) recoverable when the signing key is lost or
compromised.

Related: `docs/install.md` (Keypair), `deploy/README.md` (release pipeline and
known residuals), `.goreleaser.yaml`, `.github/workflows/release.yml`,
`updatecore/sign.go`.

## The key ring

Every release binary embeds two base64-encoded raw Ed25519 public keys at build
time (`.goreleaser.yaml`):

| ldflag symbol | Role |
|---|---|
| `updatecore.PublicKey` | **Current** key: signs releases. Required; the workflow asserts it is non-empty and matches `deploy/gotham-signing-key.pub`. |
| `updatecore.NextPublicKey` | **Next** key: pre-positioned for a rotation. Optional; empty when unset. |

`updatecore.LoadPublicKeys()` resolves the ring and the verifier
(`NewVerifierSet`) accepts a detached manifest signature produced by **any**
key in the ring. Fail-closed rules:

- an embedded next key without an embedded current key is an error;
- a malformed embedded key (current or next) is an error — it is never
  silently ignored;
- the development override `GOTHAM_UPDATE_PUBLIC_KEY` applies only when
  nothing is embedded;
- a blank/empty ring fails closed (`ErrNoPublicKey`), and a signature no key
  produced fails with `ErrBadSignature`.

The current key is still the only **signing** key: the release workflow asserts
`GOTHAM_UPDATE_SIGNING_KEY` matches `GOTHAM_UPDATE_PUBLIC_KEY` and the
committed `deploy/gotham-signing-key.pub`, and signs the manifests with it.

### Why the next key must ship in a release before it is needed

The trust anchor is compiled in. A node runs the ring it was built with: a
v0.1.x binary knows only its one embedded key and can never learn a new one at
runtime. So a new key must be inside an **already-installed** binary before it
starts signing releases:

1. Release `vX` ships with ring `{K1, K2}`, signed by K1. Every existing node
   verifies it, because they know K1.
2. The fleet upgrades to `vX`; those nodes now trust K1 **and** K2.
3. K2 is promoted to current; the next release is signed by K2 and verifies on
   every node at `vX` or newer.
4. A node still on a pre-`vX` binary cannot verify K2-signed releases. Upgrade
   it to `vX` (or newer) or reinstall it **before** K2 starts signing.

## Key custody

- The **private key** (`GOTHAM_UPDATE_SIGNING_KEY`, PKCS#8 PEM) is the only
  secret: never committed, printed, or emailed. It lives in the approval-gated
  `release` environment, is materialized to a `0600` temp file only for the
  signing step, and is deleted on exit. **Back it up offline** (encrypted
  offline media or a hardware token); the ring makes a loss survivable, but the
  backup is still the fastest recovery.
- The **next private key** stays offline until its promotion. Do **not** put it
  in `GOTHAM_UPDATE_SIGNING_KEY` ahead of time; that secret always holds the
  current signing key only. Keeping the next private key offline is what makes
  a compromise of the release environment survivable.
- The **public keys** are not secrets. `deploy/gotham-signing-key.pub` is
  published as a release asset, and both installer scripts pin
  `GOTHAM_RELEASE_PUBLIC_KEY_B64`.

Generate a keypair (`keygen` writes the private key `0600` and prints the
base64 value for the `-ldflags`/secret embed):

```sh
go run ./cmd/signer keygen -out rotation-K2.key
# rotation-K2.key      PKCS#8 PEM private key -> offline custody
# rotation-K2.key.pub  PKIX PEM public key
```

## Planned rotation

Suppose K1 is current, K2 is the key generated for this rotation, and K3 will
be the next-next key.

### 1. Generate the new next-next key (offline)

```sh
go run ./cmd/signer keygen -out rotation-K3.key
```

Store `rotation-K3.key` offline; keep `rotation-K3.key.pub` for step 3.

### 2. Ship a release with the ring {K1, K2}

In the `release` environment:

- `GOTHAM_UPDATE_NEXT_PUBLIC_KEY` = base64 of K2 (the value `keygen` prints —
  raw 32 bytes, standard padded base64);
- `GOTHAM_UPDATE_PUBLIC_KEY` and `GOTHAM_UPDATE_SIGNING_KEY` stay K1.

Tag the release. The workflow fails closed when the next key does not decode to
32 raw bytes, equals the current key, or is missing from any of the four built
binaries.

Roll the fleet onto this release (control-plane `gotham update apply`, the
agent rollout, or a fresh install) and confirm every node reaches it before
continuing.

### 3. Promote K2

In one commit:

- `GOTHAM_UPDATE_SIGNING_KEY` = K2 private key (PKCS#8 PEM);
- `GOTHAM_UPDATE_PUBLIC_KEY` = base64 K2;
- `GOTHAM_UPDATE_NEXT_PUBLIC_KEY` = base64 K3;
- `deploy/gotham-signing-key.pub` = K2 public key;
- `GOTHAM_RELEASE_PUBLIC_KEY_B64` in `deploy/install.sh` **and**
  `deploy/install-agent.sh` = base64 K2 (the release contract test fails the
  build otherwise).

Tag the next release. It is signed by K2, verifies on every node running the
{K1,K2} ring release, and embeds `{K2,K3}` so the following rotation can be
K2 -> K3. Nodes below the ring release cannot verify it: upgrade them, or keep
signing with K1 until they are.

### 4. Retire K1

Once K2 is current, new binaries carry `{K2,K3}` — K1 is already out of the
ring. Do not destroy the K1 private key until no supported node relies on it;
archive it offline instead.

## Compromise or loss

| Scenario | Recoverable with a pre-shipped ring? | Action |
|---|---|---|
| K1 private key lost (not compromised) | Yes, where the fleet runs a ring release | Promote the pre-positioned K2 (step 3) and ship a K2-signed release; nodes on the ring update normally. Nodes below the ring release need a manual upgrade or reinstall. |
| K1 private key compromised | Partially | K1 is burned. Promote K2 immediately, ship a release signed by K2 whose ring no longer embeds K1, and force the fleet onto it. Nodes below the ring release still trust K1 and must be reinstalled — a compiled-in key cannot be revoked remotely. |
| Next key lost before promotion | Yes | Generate K3 and ship a release that replaces the next key; nothing signed with it yet. |
| Next key compromised before promotion | Yes | Replace it as above. If it was already promoted (is now current), treat it as a current-key compromise. |
| Release environment/current signing key compromised | Partially | The attacker can sign releases accepted by every node trusting the current key. Rotate to the next key, publish the revocation release, reinstall what cannot be upgraded off the old anchor, and rotate the environment's other secrets. |
| A node never received a ring release | No | It only trusts the key built into it. Reinstall from the updated installer (fresh installs verify against the promoted key) or hand-install a verified release. |

### Emergency sequencing (suspected current-key compromise)

1. Stop the pipeline: pause the `v*` tag rules so no further release can be
   published while the keys are in question.
2. Check the pre-positioned next-key private key **offline**. If it may have
   been exposed, generate a fresh key instead and accept that nodes below a
   ring release will need reinstalling.
3. Promote the chosen key (environment secrets + `deploy/gotham-signing-key.pub`
   + both installer pins) in one commit, and tag a revocation release that
   embeds the promoted key plus a new next key.
4. Roll the fleet onto it immediately. Nodes that refuse it are below the ring
   release: reinstall them from the updated installer or hand-install the
   verified binary.
5. Rotate any other credentials that shared the compromised environment, audit
   the release assets for anything the attacker uploaded, and rotate the new
   next key once the fleet is verified.

## Verification commands

Run after every rotation and before announcing a release:

```sh
# 1. The manifest signature verifies with the current (promoted) key.
go run ./cmd/signer verify -key deploy/gotham-signing-key.pub \
  -in gotham-manifest-amd64.txt -sig gotham-manifest-amd64.txt.sig

# 2. The artifact matches the signed digest (manifest line: sha256=<hex>).
sha256sum gotham-linux-amd64
grep '^sha256=' gotham-manifest-amd64.txt

# 3. The embedded ring contains the expected keys. Check the current key, and
#    during a rotation also the pre-positioned next key's public half.
b64() { openssl pkey -pubin -in "$1" -outform DER | tail -c 32 | base64 | tr -d '\n'; }
grep -aqF "$(b64 deploy/gotham-signing-key.pub)" gotham-linux-amd64
grep -aqF "$(b64 rotation-K2.key.pub)" gotham-linux-amd64   # when K2 is pre-positioned

# 4. Full installer chain against a checkout (sign -> serve -> verify -> install).
sh deploy/test-release-install.sh
```

The agent family uses the same commands with
`gotham-agent-manifest-<arch>.txt` / `gotham-agent-linux-<arch>`.

The release workflow's own gates must also be green in the run log:
"Validate the optional next release key" and "Assert the version and key ring
are embedded". A node proves it trusts the new ring only by applying a release
signed by the promoted key; after a rotation, exercise `gotham update apply`
(or the agent rollout) on at least one node per architecture.
