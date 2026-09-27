# Shared Test Server

This is the long-lived test box used by both the project owner and coding
agents for manual verification (real-machine SSH validation, agent
registration, end-to-end smoke tests). It is an **all-in-one** host: the
control plane, Postgres, Redis, and a node agent all run on the same machine.

## Connection

- SSH alias: `gotham` (see local `~/.ssh/config` on each operator machine).
- OS: Ubuntu 22.04, root access.
- Specs at provisioning time: 4 vCPU, 7 GB RAM, ~68 GB free disk.
- Docker Engine + compose plugin installed (see below).

```sh
ssh gotham
```

New operators: add a `Host gotham` entry (hostname, port, user, identity file)
to your own `~/.ssh/config`. Never commit connection details or private keys
to this repository.

## Provisioning (done once, 2026-09-27)

1. Waited for first-boot `unattended-upgrades` to release the apt lock
   (do NOT kill it mid-upgrade).
2. Installed Docker from the official Docker apt repository:
   `docker-ce`, `docker-ce-cli`, `containerd.io`,
   `docker-buildx-plugin`, `docker-compose-plugin`; enabled via systemd.
3. The kernel was upgraded by the auto-updater; reboot once when convenient.

## All-in-one test workflow

Run on the server after cloning the repository:

```sh
git clone https://github.com/justindeelux/gotham.git && cd gotham
docker compose -f deploy/compose.dev.yml up -d
make migrate
make build
./bin/gotham serve
# CP serves the UI/API on :8000 and the agent gRPC gateway on :9442.
```

In a second shell on the same server:

```sh
GOTHAM_AGENT_CP_ADDR=localhost:9442 \
GOTHAM_AGENT_NODE_ID=test-node-1 \
GOTHAM_AGENT_CERT_DIR=./data/agent \
./bin/gotham-agent serve
```

Expected: the agent registers, the control plane shows the server as `ready`
with live CPU/RAM/disk metrics, and heartbeats arrive every 10s.

## Browser UI smoke (Playwright)

The `web/e2e` suite drives the embedded SPA in headless Chromium against a
running control plane. It complements the API-level suite in `internal/e2e`
by proving the browser paths work end to end: real login form, core-page
navigation, and an API-seeded application appearing in the UI.

It never starts a server itself — point it at a running `gotham serve` (see
the all-in-one workflow above) with `GOTHAM_E2E_BASE_URL`.

```sh
cd web
npm ci
npm run e2e:install           # Linux: Chromium + OS deps (with --with-deps)
# macOS: npx playwright install chromium

# Against the default test-server CP on :8000:
GOTHAM_E2E_BASE_URL=http://localhost:8000 npm run e2e
```

Scenarios:

- **auth** — register one account through the API, then sign in through the
  real login form and assert the dashboard renders.
- **navigation** — walk Dashboard, Servers, Applications and Databases; assert
  each heading renders with no console errors or failed `/api/v1/*` requests.
- **applications** — seed a server row and an application row through the API
  (public clone URL, no deployment) and assert the app lists and its detail
  page opens.
- **guardrail** — any `console.error`, page error, or `/api/v1/*` 5xx fails the
  test; the navigation and applications scenarios also fail on any 4xx.

Every run namespaces its data with a unique suffix (account email, server and
application names) and removes nothing owned by the shared box.

CI runs the same suite in `.github/workflows/ui-e2e.yml` (Postgres 16 + Redis
7 services, port 8099, report/trace artifacts on failure). Trigger it manually
via *workflow_dispatch* or by opening a PR that touches `web/**`.

## Verified on real hardware (2026-09-27)

Environment prerequisites installed for the current feature set:

- `railpack` 0.40 on PATH and a `buildkit` container with
  `BUILDKIT_HOST=docker-container://buildkit` in
  `/etc/systemd/system/gotham.service.d/buildkit.conf` (Railpack apps).
- `servers.ip = 127.0.0.1` for `test-node-1` (the all-in-one registration has
  no operator address; the CP dials the agent on the node port 9443).
- Dev mode: no CA configured, so CP and agent speak plaintext.

Verified live (beyond CI):

- Deploy `docker/welcome-to-docker` (Dockerfile) → build on the agent →
  container running → HTTP 200 via host port; update + redeploy; rollback →
  previous image runs → HTTP 200; manual stop/start.
- Deploy `heroku/node-js-getting-started` (no Dockerfile, Railpack) →
  running → HTTP 200; `PORT=3000` auto-injected; env vars applied;
  persistent `/data` mount present.
- Containers list through the agent (`/api/v1/servers/{id}/containers`).
- Migrations `00010_deploy_keys`, `00011_backups` applied; new routes mounted
  (`/api/v1/databases/backup-targets` → 401 unauthenticated).
- Managed PostgreSQL backup/restore through the real CP/API → agent → Docker
  path (2026-09-27, disposable local stack: `GOTHAM_E2E=1 go test
  ./internal/e2e/ -run TestP5Backup`): local backup → drop only the disposable
  table → restore → identical row count and md5 checksum
  (`200:3dfbedd249eae27832cc4181ee42a6c7`); a 473 411-byte incompressible
  artifact spanned six 90 000-byte staging chunks and restored to the same
  400-row checksum; an S3 target with an explicit `http://127.0.0.1:<port>`
  endpoint passed the connection check, its object
  `databases/<db>/<backup>.dump.gz` was verified in the bucket, and the
  restore reproduced `150:536d8ea0f682d75eff6602e3ce38e970`. The smoke found
  and fixed three latent BE-5.2 defects on the real path (binary dump bytes
  replaced by the Docker log driver's UTF-8 handling, restore staging writing
  base64 instead of decoding it, `pg_restore -` opening a file named `-`).

Not yet verified live on this host: webhook delivery end-to-end (needs a
provider connection or a seeded row), Traefik/domains, GitHub OAuth (owner
credentials). Managed database backup/restore and S3 targets are verified on
the local disposable `internal/e2e` stack described above but have not been
repeated against this shared box's CP/agent yet.

## Notes

- Development mode: when there is no CA (empty `GOTHAM_CA_DIR`), the control
  plane dials agents over plaintext and the agent serves its DockerService
  without TLS. As soon as a CA exists, registration issues certificates and
  both sides use mTLS.
- Go toolchain: install a Go release matching `go.mod` before `make build`.
- Ports used on this host: 8000 (CP HTTP), 9442 (CP gRPC), 9443 (agent
  DockerService), 5432 (Postgres), 6379 (Redis).
- This box holds no production data; wiping the dev database or reinstalling
  the agent is always acceptable.
