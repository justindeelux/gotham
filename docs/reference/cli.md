---
title: CLI
---

# Command-line reference

Gotham ships two service binaries and one release tool:

| Binary | Runs on | Role |
|---|---|---|
| `gotham` | control-plane host | HTTP console/API, gRPC gateway, migrations, operator account recovery |
| `gotham-agent` | every node | Docker/build/file/terminal services the control plane dials |
| `signer` | release workstation | Signs release artifacts before they are published |

Flags may be written with one or two leading dashes (`-email` and `--email` are
the same). Exit codes are `0` for success, `1` for a failed operation and `2` for
a usage error.

## gotham

| Command | Flags | Purpose |
|---|---|---|
| `serve` | none | Start the control plane: HTTP listener, gRPC gateway, background services. |
| `admin create` | `--email <address>` (required), `--password <pw>`, `--password-stdin`, `--generate-password`, `--force` | Create the first account. Refuses when the instance already has an account unless `--force` is given. |
| `admin exists` | none | Print `admin-exists: true` or `admin-exists: false` and exit `0`; exit `1` on error. |
| `admin reset-password` | `--email <address>` (required), `--password <pw>` | Replace a password, revoke the account's refresh sessions and clear its two-factor authentication. |
| `ca init` | `--host <name-or-ip>` (repeatable, comma-separated values accepted) | Create the gRPC mTLS certificate authority under `ca.dir` and record extra listener SAN hosts. Idempotent: an existing CA is loaded, never replaced. |
| `migrate [up\|down\|status]` | verb only; default `up` | Apply all pending migrations (`up`), roll back the most recently applied one (`down`), or print the migration status (`status`). |
| `update check` | none | Report the newest release on the configured channel, or that the running version is current. |
| `update apply` | `--channel stable\|beta` (default: the installation's configured channel) | Download, verify and stage the newest release. |
| `update rollback` | none | Restore the previous binary from `<binary>.old`. |
| `update reset` | none | Clear the pending-update marker and the failed-release backoff. |
| `version` | none | Print the build version. `-v` and `--version` are aliases. |

Examples:

```sh
gotham serve
gotham admin create --email ops@example.com
gotham admin exists
gotham admin reset-password --email ops@example.com
gotham ca init --host cp.example.com
gotham migrate status
gotham update check
gotham update apply --channel beta
gotham update rollback
gotham update reset
gotham version
```

Notes:

- `serve`, `migrate`, `ca` and `admin` read configuration from `gotham.yaml` in
  the working directory plus `GOTHAM_*` environment variables (see
  [Configuration](/reference/configuration)). `update` takes its own settings
  from the environment only, though it still validates `gotham.yaml`.
- `admin` commands also read `/etc/gotham/gotham.env` when it exists, without
  overriding variables already set in the environment. Run them against the
  service's database, for example
  `sudo -u gotham /var/lib/gotham/bin/gotham admin create --email ops@example.com`.
- Three password sources exist for `admin create` and they are mutually
  exclusive: `--password`, `--password-stdin` (one piped line) and
  `--generate-password` (prints `generated-password: <value>` once). With none
  of them the command prompts twice with hidden input.
- `admin reset-password` revokes refresh sessions and clears TOTP. Access
  tokens already issued stay valid until they expire (15 minutes).
- `serve` exits when PostgreSQL is unreachable, and refuses to start the agent
  channel without a CA unless `grpc.insecure: true` is set. `update apply` and
  `update rollback` refuse to run when the target binary belongs to another
  user; run them as the service user, not as root.
- `migrate` is fail-fast: a second run that cannot take the migration lock
  returns an error instead of queueing.

## gotham-agent

| Command | Flags | Purpose |
|---|---|---|
| `serve` | none | Run the node agent. `gotham-agent` with no arguments does the same; `run` is an alias. |
| `update reset` | none | Clear the failed-update state and backoff so a fixed release is retried. |
| `version` | none | Print the build version. `-v` and `--version` are aliases. |

Examples:

```sh
gotham-agent serve
gotham-agent update reset
gotham-agent version
```

Notes:

- The agent reads its settings from the environment; see
  [Configuration](/reference/configuration) for the connection, update and
  volume settings.
- `update reset` resolves paths from the process environment, then
  `/etc/gotham/agent.env` (override the path with `GOTHAM_AGENT_ENV_FILE`), then
  the built-in defaults. Run it as root to also remove the root-owned status
  file.
- A newer release is installed as part of an operator-triggered rollout;
  `reset` is only needed to retry the same version after a rollback.

## signer

`signer` (`keygen`, `sign`, `verify`, `manifest`) is needed only by operators
building Gotham releases; every artifact is signature-verified automatically on
install, so hosts never handle the key. See [Updates](/guide/updates) for the
release and verification flow, and `signer help` for the subcommand flags.
