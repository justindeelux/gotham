---
title: Security
---

# Security

Gotham separates three things that are easy to confuse: a person's role inside
a team, platform-operator access to the instance itself, and the scopes of an
API token. Node agents authenticate on a separate channel, and stored
credentials are encrypted at rest.

## Team roles

Every account acts inside one active team. A team member has exactly one of
three roles; there is no `member` role.

| Role | Can do | Cannot do |
|---|---|---|
| `owner` | Everything in the team, including deleting the team and managing owners. | — |
| `admin` | Manage resources and members. | Remove or demote an owner. |
| `read_only` | Read resources and members. | Create, update or delete anything. |

`read_only` is enforced by the server on every mutation, not only by hiding
buttons. Some console controls stay visible for a read-only member and are
rejected with a permission error when used; that is expected, and the API is
the authority.

Roles are per team. A team owner is not a platform operator, and being a
platform operator does not grant team access to a team the account does not
belong to. Node-scoped resources (containers, files, the App store queue) also
check the node's team and the caller's role.

## Platform operators

Platform-operator access gates the instance-wide surfaces: instance settings,
the global proxy sync and DNS providers, update apply, the host file manager,
the host terminal, and the agent fleet rollout. A request passes when it is:

- an API token holding the `admin` scope,
- a session whose role claim is `admin`, or
- a session whose account email is listed in `PLATFORM_ADMINS`.

This is secure by default: listing nobody grants nobody operator access by
email address, and the other two paths still apply. The first account on a
fresh instance is the platform admin; later accounts are not unless their role
claim is `admin`, they hold an `admin`-scoped token, or their email is listed.
Minting an `admin`-scoped token is itself restricted to platform operators.

## API tokens

API tokens are opaque credentials with a fixed prefix, managed through the API
(see [API tokens](/reference/api-tokens)). They carry one or more scopes:

| Scope | Grants |
|---|---|
| `read` | Read requests (`GET`, `HEAD`) on the resource API. The default when no scope is requested. |
| `deploy` | Every write, plus secret-bearing reads such as database credentials and file content. |
| `admin` | Platform-global operations, including proxy sync, DNS providers and operator settings. |

Scopes are hierarchical: `admin` covers `deploy`, and `deploy` covers `read`.
A token can only mint or rotate a token with scopes at or below its own, so a
`deploy` token cannot create an `admin` one. A token never widens its owner's
team role — the team check still runs.

The secret is shown once, at creation or rotation. Only a hash is stored;
listing tokens returns metadata (scopes, last use, revocation) and never the
secret. Rotating replaces the secret and invalidates the old one immediately;
revoking is permanent. Scope a token to the least it needs, and prefer
`read` for dashboards and `deploy` for a CI job.

## Node channel

The control plane runs a certificate authority. It issues the listener
certificate and one client certificate per enrolled node, and node agents talk
to the control plane over mutual TLS. A production instance requires the CA;
there is no plaintext agent channel without an explicit development opt-in.

Beyond TLS, each connection is bound to a node identity:

- **Enrollment pins the key.** The node's public-key fingerprint is recorded
  when it enrolls. Every RPC and stream then checks that the presented
  certificate's key matches that node's recorded fingerprint and that the node
  still exists.
- **A CSR must use the same key.** First enrollment with a certificate signing
  request is refused when the CSR's key differs from the authenticated
  certificate.
- **Refusals do not leak the registry.** An unknown node, a mismatched key and
  a removed node get the same refusal to the caller; the distinct reason is
  logged on the control plane only.
- **Deleting a node removes its authorization.** The certificate alone no
  longer authenticates after the node's registry row is gone.

### First enrollment

First enrollment is gated by an enrollment token, a bootstrap credential the
operator passes to the node installer. It is compared in constant time, and a
missing token and a wrong one produce the same error. When no token is configured,
the control plane generates one at first start and refuses enrollment without
it — enrollment fails closed. Open trust-on-first-use enrollment exists only as
an explicit operator opt-in, is logged loudly, and is not appropriate for a
real instance.

Keep the token out of shell history, images and tickets. A leaked token allows
a new node to enroll; remove the bogus node and rotate the token.

### Legacy certificate sunset

Nodes enrolled before mutual TLS hold a certificate valid only for server
authentication. The gateway accepts such a leaf until a sunset date so an
existing fleet keeps working during upgrade, and logs the date plus how many
nodes still present one. The built-in sunset is 2027-01-15 and can be moved or
disabled; a disabled sunset leaves the weaker leaf accepted indefinitely.
Re-enroll remaining nodes before the date — zero legacy leaves is the healthy
state.

## Secrets at rest

Stored credentials are encrypted with AES-256-GCM before they reach the
database: a fresh nonce per value, authenticated so a corrupted or wrong-key
value fails to open instead of decrypting to garbage. Credential values are not
returned in list responses; a feature opens one only when it needs to use it or
display it.

This covers the credential-bearing data the platform stores: Git deploy keys
and HTTPS tokens, connected Git-provider secrets, database credentials, S3
backup credentials, notification channel secrets, DNS API tokens, and
application and project variables.

The encryption key comes from `GOTHAM_SECRET_KEY`. When it is unset, the
control plane uses a durable key generated on first start so sealed values
survive a restart; only when that key cannot be persisted does it fall back to
an ephemeral key, log a warning, and leave stored credentials unreadable after
a restart. Set the key explicitly in production, keep it out of the repository,
and back it up together with the database: losing either makes sealed values
unrecoverable, and changing the key without a migration does the same.

Session tokens are signed with an Ed25519 key. Without a configured keypair a
fresh one is generated at startup, so sessions do not survive a restart; that
is convenient for development and unsuitable for production.

## Feature switches

Ten environment switches disable optional surfaces. All default to **on**: an
unset or empty value keeps the module, and only an explicit `false`
(case-insensitive) turns it off. They are read when the control plane starts,
so a change needs a restart, and each disabled module writes one warning line
to the startup log naming its flag.

| Flag | Default | What turning it off disables |
|---|---|---|
| `FEATURE_APPLICATIONS` | on | The application and deployment API: create, deploy, roll back, logs, environment, domains. Projects and environments remain. |
| `FEATURE_PREVIEWS` | on | Pull-request preview deployments, their listing, and the orphan sweep. Push webhooks keep working. |
| `FEATURE_DATABASES` | on | Managed database lifecycle, credentials, backup and restore routes, and the backup scheduler. |
| `FEATURE_SERVICES` | on | Compose services and the one-click template catalog. |
| `FEATURE_TEAMS` | on | Team and invite routes. Resource routes resolve through each account's personal team instead. |
| `FEATURE_FILES` | on | The remote file-manager API, which answers 503 while off. |
| `FEATURE_NOTIFICATIONS` | on | Notification channels, test sends, the delivery log, and the deploy/backup notification hooks. |
| `FEATURE_PROXY` | on | Proxy sync, the router list, certificate configurations, redirects, DNS providers, panel-certificate reconciliation, and the deploy hook. Nodes keep the last configuration they were pushed. |
| `FEATURE_METRICS` | on | The node metrics route, metric persistence on heartbeat, and the 30-day retention sweep. Metric charts stay empty. |
| `FEATURE_UPDATES` | on | The console update surface: check, schedule, apply, and the automatic update loop. The `gotham update` CLI remains available. |

Disabling a module removes its routes rather than leaving a half-working
surface; the exception is the file API, whose mounted routes answer 503.

## Operator responsibilities

- Configure `GOTHAM_SECRET_KEY` and the session signing keypair, and keep their
  backups with the database backup.
- Keep the certificate authority material private and backed up; without it,
  agents cannot be re-enrolled or the listener restarted safely.
- Restrict `PLATFORM_ADMINS` to the accounts that genuinely operate the
  instance, and hand out scoped tokens instead of admin ones.
- Retire legacy node leaves before the sunset date.
- Keep the control plane behind TLS and serve the panel over HTTPS, so the
  panel certificate and application TLS are both active.

Release artifacts are signed and verified automatically; see
[Updates](/guide/updates) for the release-signing model.
