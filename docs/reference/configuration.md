---
title: Configuration
---

# Configuration reference

The control plane reads `gotham.yaml` from its working directory. The file is
optional: every key is optional, a missing file is not an error, and the tables
below name the default or empty state for each one. Values are layered —
built-in defaults, then the file, then `GOTHAM_*` environment variables, which
win.

The agent does not read `gotham.yaml`; it is configured from environment
variables, mostly `GOTHAM_AGENT_*`. Self-update settings and the feature
switches are read directly from the environment.

A running control plane watches the file and reloads it without a restart, but
only `log.level` is applied live. Connection, listener and certificate values
are consumed at startup, so changing them needs a restart; `log.format` also
needs a restart. An invalid edit is logged and the previous values stay in
effect.

## HTTP listener

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `server.addr` | `GOTHAM_SERVER_ADDR` | `0.0.0.0` | no |
| `server.port` | `GOTHAM_SERVER_PORT` | `8000` | no |
| `server.trusted_proxies` | `GOTHAM_TRUSTED_PROXIES` | empty — no peer trusted | no |

- `server.port` must be between 1 and 65535.
- `server.trusted_proxies` is a YAML list or a comma-separated environment
  value of IPs and CIDRs. Only these peers' `X-Forwarded-For` and
  `X-Forwarded-Proto` headers are honoured; from every other peer they are
  ignored. Set it when a TLS-terminating proxy fronts the control plane, or
  clients share one rate-limit bucket and HTTPS is not detected. Keep the list
  to the proxy's exact addresses: every host inside a trusted range can spoof
  the client address and scheme. Changing it requires a restart.

## Database and Redis

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `database.dsn` | `GOTHAM_DATABASE_DSN` | generated at install and stored in `/etc/gotham/gotham.env` | yes — reachable database |
| `redis.addr` | `GOTHAM_REDIS_ADDR` | `localhost:6379` | yes — reachable instance |

- `database.dsn` is the libpq/pgx connection string used by the store, the
  health check and `gotham migrate`. `gotham serve` exits when it cannot connect.
- `redis.addr` is a `host:port`. Redis backs realtime updates, log streaming,
  backups and background queues; it is not checked at startup, but `/healthz`
  answers 503 while it is unreachable.

## gRPC gateway and certificate authority

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `grpc.addr` | `GOTHAM_GRPC_ADDR` | `:9442` | no |
| `grpc.hosts` | `GOTHAM_GRPC_HOSTS` | empty | no |
| `grpc.insecure` | `GOTHAM_GRPC_INSECURE` | `false` | no |
| `grpc.enrollment_token` | `GOTHAM_GRPC_ENROLLMENT_TOKEN` | generated next to the CA | no |
| `grpc.allow_open_enrollment` | `GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT` | `false` | no |
| `grpc.legacy_leaf_until` | `GOTHAM_GRPC_LEGACY_LEAF_UNTIL` | built-in sunset date | no |
| `ca.dir` | `GOTHAM_CA_DIR` | `./data/ca/` | yes, in production |

- `grpc.addr` is where node agents dial. `grpc.hosts` adds DNS names and IPs to
  the listener certificate's SANs so a remote agent dialing the control plane by
  name verifies it. The loopback names and the machine hostname are always
  included. With `grpc.hosts` empty, the list saved by `gotham ca init --host`
  is used.
- `grpc.insecure: true` is the development opt-in that lets the gateway start
  in plaintext when no CA exists. Without a CA and without this flag, `serve`
  refuses to start the agent channel.
- `grpc.enrollment_token` is the bootstrap credential a node presents on its
  first registration. When empty, `serve` generates one and writes it to
  `<ca.dir>/enrollment-token`; pass that value to the node as
  `GOTHAM_AGENT_ENROLLMENT_TOKEN` (or with
  `install-agent.sh --enrollment-token-file`). First enrollment fails closed
  without it, unless open enrollment is explicitly enabled.
- `grpc.allow_open_enrollment: true` accepts a first registration with no token
  (trust on first use). Leave it false on a real instance.
- `grpc.legacy_leaf_until` is an RFC 3339 timestamp, or `none` to disable the
  sunset for node certificates issued before mutual TLS. Empty selects the
  built-in default; a malformed value fails startup.
- `ca.dir` holds `ca.crt`, `ca.key` (mode 0600) and the optional SAN host list.
  Create it with `gotham ca init` (see [CLI](/reference/cli)).

## Secrets and logging

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `secret_key` | `GOTHAM_SECRET_KEY` | key in `<ca.dir>/secret.key` | recommended |
| `log.level` | `GOTHAM_LOG_LEVEL` | `info` | no |
| `log.format` | `GOTHAM_LOG_FORMAT` | `json` | no |

- `secret_key` encrypts stored credentials at rest (SSH private keys and TOTP
  secrets) with AES-256-GCM. When unset, a key is read from — or created once
  in — `<ca.dir>/secret.key`; if that directory is not writable, an ephemeral
  key is generated and stored credentials do not survive a restart. Set it
  explicitly in production.
- `log.level` selects the minimum level (`debug`, `info`, `warn`, `error`); an
  unrecognized value falls back to `info`. `log.format` is `json` or `text` and
  is rejected at load time when it is neither.

## JWT signing keys

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `auth.jwt_private_key_path` | `GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH` | none — ephemeral keypair | recommended |
| `auth.jwt_public_key_path` | `GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH` | none — ephemeral keypair | recommended |

- The keypair is Ed25519 PEM: the private key in PKCS#8 form (`PRIVATE KEY`),
  the public key in PKIX form (`PUBLIC KEY`), as produced by
  `openssl genpkey -algorithm ed25519`.
- Set both paths. With either missing, the server generates an ephemeral
  keypair at startup and sessions do not survive a restart. A configured path
  that cannot be read fails startup.

## OAuth (GitHub)

GitHub is the only OAuth provider. The provider is disabled while its client ID
or secret is empty, and its login route then answers 404.

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `oauth.github.client_id` | `GOTHAM_OAUTH_GITHUB_CLIENT_ID` | empty — provider off | to enable |
| `oauth.github.client_secret` | `GOTHAM_OAUTH_GITHUB_CLIENT_SECRET` | empty — provider off | to enable |
| `oauth.github.redirect_url` | `GOTHAM_OAUTH_GITHUB_REDIRECT_URL` | derived from the control-plane URL | no |

- The GitHub OAuth app's authorization callback URL must match the effective
  redirect: `/api/v1/auth/oauth/github/callback` on the control plane's origin.
- When `redirect_url` is empty, the callback is derived from the instance's
  configured control-plane URL. With neither set, no redirect is sent and
  GitHub uses the OAuth app's registered callback.

## Git source policy

| Key | Environment variable | Default | Required |
|---|---|---|---|
| `deploy.git_allow_private_hosts` | `GOTHAM_GIT_ALLOW_PRIVATE_HOSTS` | `false` | no |

- `false` refuses git clone and probe remotes on loopback, RFC1918, ULA,
  link-local (including the cloud metadata address), unspecified, multicast,
  carrier-grade NAT, benchmarking, NAT64 and 6to4 addresses.
- `true` permits RFC1918, ULA and loopback remotes, for a self-hosted git
  server. Link-local and the other always-denied ranges stay denied.

## Managed volume root

Application bind mounts are confined to `<root>/<application id>` on both
sides. The control plane derives the path; the node re-validates it against its
own root, so the two values must be the same.

| Variable | Read by | Default | Required |
|---|---|---|---|
| `GOTHAM_MANAGED_VOLUME_ROOT` | control plane | `/var/lib/gotham/volumes` | no |
| `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` | node agent | `/var/lib/gotham/volumes` | no |

- Values must be absolute. The agent falls back to
  `GOTHAM_MANAGED_VOLUME_ROOT` when its own variable is unset, so exporting the
  shared variable on both hosts keeps them in step.
- A relative or empty control-plane value falls back to the default; the agent
  refuses a relative value at startup.

## Self-update (control plane, environment only)

| Variable | Default | Purpose |
|---|---|---|
| `AUTO_UPDATE` | `false` | Run the scheduled check/apply loop. `true` enables both check and apply. |
| `AUTO_UPDATE_INTERVAL` | `6h` | Interval for the automatic loop (Go duration). |
| `GOTHAM_UPDATE_CHANNEL` | `stable` | Release channel: `stable` or `beta`. |
| `GOTHAM_UPDATE_REPO` | `justindeelux/gotham` | GitHub repository that publishes releases (`owner/name`). |
| `GOTHAM_UPDATE_BASE_URL` | `https://api.github.com` | Releases API base for a GitHub Enterprise host or mirror. Must be http(s); plain http only for loopback. |
| `GOTHAM_UPDATE_PUBLIC_KEY` | embedded in release builds | Release public key (PEM or base64). Development override; used only when no key is embedded. |
| `GOTHAM_UPDATE_SCRIPT` | `/usr/libexec/gotham/gotham-update` | Root-owned restart and health-check wrapper. |
| `GOTHAM_UPDATE_STATUS` | `/var/lib/gotham-updater/update.status` | Root-owned status file written by the wrapper. |
| `GOTHAM_UPDATE_PENDING` | `/var/lib/gotham/update.pending` | Staged-update gate marker. |
| `GOTHAM_UPDATE_BINARY` | `/var/lib/gotham/bin/gotham` | Target binary; must match the service unit's `ExecStart`. |
| `GOTHAM_UPDATE_LOCK` | `/var/lib/gotham/update.lock` | Lock shared with the wrapper. |
| `GOTHAM_UPDATE_BACKOFF` | `/var/lib/gotham/update.backoff` | Failed-attempt count for the `AUTO_UPDATE` loop; cleared by `gotham update reset`. |
| `GOTHAM_UPDATE_DOWNLOAD_TIMEOUT` | `10m` | Timeout for the release-artifact body. |
| `GOTHAM_UPDATE_CURRENT` | running version | Override the reported version (wrappers and tests). |
| `GOTHAM_UPDATE_OFFER_CACHE_TTL` | `10m` | How long an agent release offer is cached. |

The console's **Settings → Updates** schedule takes precedence over
`AUTO_UPDATE` and `AUTO_UPDATE_INTERVAL` once a schedule has been saved. See
[Updates](/guide/updates).

## Node agent (environment only)

Connection settings:

| Variable | Default | Required |
|---|---|---|
| `GOTHAM_AGENT_CP_ADDR` | `127.0.0.1:9442` | no — must point at `grpc.addr` |
| `GOTHAM_AGENT_NODE_ID` | `localhost` | no |
| `GOTHAM_AGENT_LISTEN_ADDR` | `:9443` | no |
| `GOTHAM_AGENT_CA` | empty | yes, unless `GOTHAM_AGENT_INSECURE=true` |
| `GOTHAM_AGENT_ENROLLMENT_TOKEN` | empty | first registration only |
| `GOTHAM_AGENT_CERT_DIR` | `./data/agent` | no |
| `GOTHAM_AGENT_KEY` | key inside the cert dir | no |
| `GOTHAM_AGENT_LOG_LEVEL` | `info` | no |
| `GOTHAM_AGENT_INSECURE` | `false` | development only |

- `GOTHAM_AGENT_CA` is the control plane's `ca.crt`. Without it the agent
  refuses to start unless `GOTHAM_AGENT_INSECURE=true`, which also requires a
  loopback listen address.
- `GOTHAM_AGENT_ENROLLMENT_TOKEN` must carry the control plane's enrollment
  token. Only the first registration needs it; afterwards the agent
  authenticates with its own certificate.

Update settings:

| Variable | Default | Purpose |
|---|---|---|
| `GOTHAM_AGENT_AUTO_UPDATE` | `false` | Apply a plain control-plane offer unattended. An operator-triggered rollout always applies. |
| `GOTHAM_AGENT_UPDATE_INTERVAL` | `5m` | How often the agent polls for an offer. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | `stable` | Channel the node accepts: `stable` or `beta`. A stable node refuses beta offers. |
| `GOTHAM_AGENT_BINARY` | `/var/lib/gotham-agent/bin/gotham-agent` | Fixed agent binary. |
| `GOTHAM_AGENT_UPDATE_SCRIPT` | `/usr/libexec/gotham/gotham-agent-update` | Root-owned restart wrapper. |
| `GOTHAM_AGENT_UPDATE_STATUS` | `/var/lib/gotham-agent-updater/update.status` | Root-owned update status. |
| `GOTHAM_AGENT_UPDATE_PENDING` | `/var/lib/gotham-agent/update.pending` | Staged-update gate marker. |
| `GOTHAM_AGENT_UPDATE_LOCK` | `/var/lib/gotham-agent/update.lock` | Lock shared with the wrapper. |
| `GOTHAM_AGENT_UPDATE_RETRY` | `/var/lib/gotham-agent/update.retry` | Marker that makes a running agent retry after `gotham-agent update reset`. |
| `GOTHAM_AGENT_UPDATE_BACKOFF` | `/var/lib/gotham-agent/update.backoff` | Persisted failed-attempt count. |
| `GOTHAM_AGENT_HEALTH_ADDR` | `127.0.0.1:8001` | Loopback liveness endpoint the wrapper probes. |

File service settings:

| Variable | Default | Purpose |
|---|---|---|
| `GOTHAM_AGENT_FILE_MAX_UPLOAD` | `1073741824` (1 GiB) | Ceiling for one upload body; a larger body is refused with `ResourceExhausted` (413). Should match the control plane's `FILES_MAX_UPLOAD_BYTES`. |
| `GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS` | `30` | Days an item may stay in one root's recycle bin before the agent purges it; `0` keeps entries forever. |
| `GOTHAM_AGENT_FILE_TRASH_MAX_BYTES` | `0` (unbounded) | Ceiling in bytes for one root's recycle bin; the oldest entries are purged first until the bin fits. |

## Feature switches

These ten switches are environment-only — there is no YAML key. All are on by
default, and only the exact value `false` (case-insensitive) disables one;
unset or any other value leaves the module on. They are read when the process
starts, and a disabled module logs its flag at startup.

| Variable | Default | Module switched off |
|---|---|---|
| `FEATURE_APPLICATIONS` | on | Applications and deploys |
| `FEATURE_DATABASES` | on | Managed databases and backups |
| `FEATURE_SERVICES` | on | Compose services |
| `FEATURE_TEAMS` | on | Teams and invites |
| `FEATURE_FILES` | on | Node file manager |
| `FEATURE_NOTIFICATIONS` | on | Notification channels |
| `FEATURE_PROXY` | on | Proxy, DNS and certificate management |
| `FEATURE_METRICS` | on | Node metrics collection and queries |
| `FEATURE_UPDATES` | on | Self-update surface and the automatic loop |
| `FEATURE_PREVIEWS` | on | Pull-request preview deployments |

Turning a module off unmounts its routes for that process (the file manager's
routes stay mounted but answer 503), so its API calls fail instead of running
degraded. Feature flags are not a per-user permission — use team roles for that.

Separate from the switches, the file manager reads one more environment limit:

Control-plane file limit:

| Variable | Default | Purpose |
|---|---|---|
| `FILES_MAX_UPLOAD_BYTES` | `1073741824` (1 GiB) | Ceiling for one write/upload body (413 above it); should match the node's `GOTHAM_AGENT_FILE_MAX_UPLOAD`. |
