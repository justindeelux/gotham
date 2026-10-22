---
title: Troubleshooting
---

# Troubleshooting

Start with the service journal and the control plane's health endpoint. Both are
small and answer most "is it up?" questions before you dig into configuration.

```sh
journalctl -u gotham -n 200 --no-pager      # control plane
journalctl -u gotham-agent -n 200 --no-pager # node agent
journalctl -u gotham -f                     # follow
```

Logs go to the systemd journal. The default format is JSON; set
`GOTHAM_LOG_FORMAT=text` for readable lines and `GOTHAM_LOG_LEVEL=debug` for
more detail. When a `gotham.yaml` is present the log level is hot-reloaded from
it; the format is fixed at startup and needs a restart.

`GET /healthz` on the panel returns `{"status", "db", "redis"}` and answers 200
only when PostgreSQL and Redis both respond; otherwise it returns 503 with
`"status": "degraded"`.

## The control plane will not start

The service exits with the reason in the journal. The usual causes:

- **Configuration error.** `gotham serve` prints `config: ...` before doing
  anything. Check `/etc/gotham/gotham.env` for a malformed value (quotes and
  trailing backslashes are rejected), then restart.
- **Port already in use.** The panel listens on `0.0.0.0:8000` by default
  (`GOTHAM_SERVER_ADDR`, `GOTHAM_SERVER_PORT`). A second `gotham` process, an old
  unit or an unrelated service on the same port makes the listener fail. Find
  the owner with `ss -ltnp` and stop it, or move the panel to another port.
- **PostgreSQL unreachable.** The database is required: the process exits when
  it cannot connect. Check `pg_isready`, the `postgresql` service and
  `GOTHAM_DATABASE_DSN`. The bundled install uses a local database; a managed one
  needs a reachable DSN.
- **Redis unreachable.** Redis is not required for startup — the panel keeps
  serving — but `/healthz` reports 503, and realtime features and log streaming
  degrade. Check the `redis` service and `GOTHAM_REDIS_ADDR` (port 6379 by
  default) and confirm with `redis-cli ping`.
- **No certificate authority.** `gotham serve` refuses to start the agent
  channel in plaintext when no CA exists (`no CA found and GOTHAM_GRPC_INSECURE is not set`).
  Re-run the installer, which provisions the CA idempotently (`gotham ca init`
  keeps an existing one). `GOTHAM_GRPC_INSECURE=true` is a development-only escape
  hatch and must not be used in production.
- **gRPC port conflict.** The agent gateway listens on port 9442 by default
  (`GOTHAM_GRPC_ADDR`); the same port-conflict check applies.

If the service keeps restarting, look for the exit reason at the end of the
journal, and check that the unit is running the binary you think it is.

## A node shows offline

A node is offline when no heartbeat arrives for 30 seconds; the agent sends one
every 10 seconds, so this is a live connection problem, not a stale row.

1. **Is the agent running?** `systemctl status gotham-agent` and
   `journalctl -u gotham-agent -f`. The agent fails closed without a CA: no
   `GOTHAM_AGENT_CA` (or `--insecure` for local development only) and it refuses
   to start.
2. **Is the health endpoint up?** The agent serves `/healthz` on its health
   address (`GOTHAM_AGENT_HEALTH_ADDR`, loopback and port 8001 by default). It
   reports process liveness only — it stays 200 even when the control plane is
   unreachable, so use the agent log for connection errors.
3. **Can the agent reach the control plane?** `GOTHAM_AGENT_CP_ADDR` must be
   `host:port` and reachable. Its name or IP must also be one of the control
   plane listener certificate SANs (`GOTHAM_GRPC_HOSTS`, persisted by
   `gotham ca init --host`, or `--cp-host` at install time). A handshake error
   such as `certificate is valid for ..., not <name>` means the address is not a
   SAN; add it and reissue the listener certificate.
4. **Is the mTLS credential intact?** Over TLS the channel is mutual: the node
   keeps its certificate (`agent.crt`) and key (`agent.key`) in its certificate
   directory, and trusts the control plane through `/etc/gotham/ca.crt`
   (`GOTHAM_AGENT_CA`). To install the current public CA and restart the agent:

   ```sh
   sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
       gotham/deploy/install-agent.sh --ca ./ca.crt
   ```

   Never copy `ca.key` to a node. If the control plane's CA was replaced, the
   node's old certificate is no longer valid and it cannot re-enroll under the
   same id while its key is still bound: delete the node (allowed only once it
   has no resources) and add it again, which makes it a first enrollment and asks
   for the enrollment token. The same applies when the node's certificate and key
   are lost. **Reset host key** in the console only re-pins the SSH host key used
   for validation; it does not touch the agent certificate.

5. **Version skew.** The agent reports its build version on every heartbeat, and
   `GET /api/v1/servers/agents` lists the version per node. The control plane
   offers the newest agent release when it is newer than the version the node
   reported; the node accepts it only if the channel matches (a `stable` node
   refuses `beta`), and no offer is made when it is already current. Update the
   node (see [Updates](/guide/updates)) or reinstall it. Node certificates issued
   before mutual TLS stop being accepted after the legacy-leaf sunset
   (`GOTHAM_GRPC_LEGACY_LEAF_UNTIL`); they renew on the next connection, and a
   node that cannot renew must be reinstalled with the CA.

If the node is reachable over SSH but the wizard's validation fails, check
Docker, free disk, and that the SSH user can run Docker without an interactive
prompt. The wizard can install Docker for you when that is the only failed check.

## Panel HTTPS or DNS fails

The panel certificate is issued through the registered node's Traefik, so HTTPS
depends on DNS and on ports 80/443 reaching that node.

- **HTTP-01 is the default.** The domain's A and AAAA records must point directly
  at this machine. Remove stale AAAA records if the machine cannot serve IPv6.
  On Cloudflare the records must be **DNS only** (proxy disabled) for HTTP-01;
  keep the proxy off until a certificate exists.
- **The node must run Traefik.** HTTPS for the panel uses the registered
  `localhost` node, so that node needs Traefik and open inbound 80 and 443.
- **The trial must be confirmed.** Saving the instance `control_plane_url` as
  `https://...` starts a confirmation trial and creates a panel certificate
  intent. Confirm through the proposed host before the trial expires. Both the
  proposed and the previous host stay routed during the trial; revert restores
  the previous host if issuance fails.
- **HTTP stays up until issuance succeeds.** The panel redirects to HTTPS only
  after the local agent reports an unexpired certificate (readiness is refreshed
  every 30 seconds). Pending or failed issuance keeps HTTP available.
- **Where to look.** Panel rows in the routers API carry `certificate.status`
  (`issued`, `pending`, `failed`) and `certificate.not_after`. `pending` means no
  usable certificate has been observed yet; `failed` covers unusable
  configuration or unavailable storage, because the agent does not report ACME
  order errors.

Keeping the Cloudflare proxy enabled requires a configured DNS provider and
DNS-01 for the panel certificate, which is set through the platform API rather
than the console. See [Proxy and DNS](/operating/proxy-dns) and
[Instance network](/operating/instance-network).

## Check migration state

After an upgrade, a manual binary replacement or a failed start, confirm the
schema before anything else:

```sh
sudo -u gotham env GOTHAM_DATABASE_DSN="<dsn>" \
  /var/lib/gotham/bin/gotham migrate status
```

It prints one row per migration — `VERSION`, `STATE`, `APPLIED AT`, `PATH` — and
only reads, so it takes no lock and is safe while diagnosing. `gotham migrate`
reads `gotham.yaml` and its own `GOTHAM_*` environment, not the unit's
`EnvironmentFile`, so point it at the service's database explicitly; otherwise it
checks the wrong database (or none).

`gotham migrate up` applies the pending migrations; `gotham migrate down` rolls
back only the most recently applied one. Migrations are forward-only: never edit
a migration that has already run — fix the schema with a new one. The installer
runs `migrate up` for you, so a missing migration usually means the service was
started with a different DSN or the installer was interrupted.

## Still stuck

- [Install](/guide/install) — layout, CA, agent install and the service env file.
- [Updates](/guide/updates) — rollback, reset and the failed-release backoff.
- [Reference: configuration](/reference/configuration) — environment keys.
