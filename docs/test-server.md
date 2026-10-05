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

### How the box is actually run (updated 2026-10-03)

The box does not match the clone-and-serve snippet above literally: the working
copy lives at **`/root/gotham`** and is a git working tree pinned to the release
tag (no separate clone), driven by two systemd units.

- **Control plane** — `gotham.service`: `WorkingDirectory=/root/gotham`,
  `ExecStart=/root/gotham/bin/gotham serve`; Postgres is the **native** server
  (`postgres` role, database `gotham`), not the compose dev stack.
- **Node agent** — `gotham-agent.service`: runs
  `/var/lib/gotham-agent/bin/gotham-agent`, config in `/etc/gotham/agent.env`
  (`GOTHAM_AGENT_NODE_ID=test-node-1`).
- **No CA on this box**, so both sides run in dev plaintext via drop-ins:
  `/etc/systemd/system/gotham.service.d/dev-insecure.conf`
  (`GOTHAM_GRPC_INSECURE=true`) and
  `/etc/systemd/system/gotham-agent.service.d/dev-insecure.conf`
  (`GOTHAM_AGENT_INSECURE=true`). Without these, both binaries fail closed
  ("no CA found … refusing to serve the agent channel in plaintext").
- **Platform operator** — `/etc/systemd/system/gotham.service.d/platform-admins.conf`
  sets `PLATFORM_ADMINS=demo@gotham.dev` (added 2026-10-04, then `systemctl
  daemon-reload && systemctl restart gotham`). Without it the Domains & SSL page
  shows "You need the admin scope" for every login, because no session can carry
  an `admin` role (see Notes). Drop-ins on this box: `dev-insecure.conf` and
  `platform-admins.conf`; there is no `/etc/gotham/gotham.env` here.
- **Go** is at `/usr/local/go/bin/go` (not on PATH by default).

Update procedure (release → box):

```sh
cd /root/gotham
git fetch --tags origin && git checkout -f vX.Y.Z
export PATH=/usr/local/go/bin:$PATH
PUBKEY="$(cat deploy/gotham-signing-key.pub | openssl pkey -pubin -outform DER | tail -c32 | base64)"
make build LDFLAGS="-s -w -X main.version=X.Y.Z \
  -X github.com/justindeelux/gotham/updatecore.PublicKey=$PUBKEY"
./bin/gotham migrate up            # forward-only; the DB tracks applied versions
systemctl restart gotham gotham-agent
./bin/gotham version                # expect "gotham X.Y.Z"
```

Caveats learned 2026-10-03:

- `make build` alone stamps `dev`; pass `LDFLAGS` (as above) for a real version.
- Copying a new agent binary over a **running** one fails with
  `Text file busy` — `systemctl stop gotham-agent` first, or let the update
  path swap it.
- A schema-lagging database makes the agent registration fail
  (`column "host_key_fingerprint" does not exist` and similar) — run
  `gotham migrate up` before restarting the control plane.
- The control plane logs dev-mode warnings on every start (no CA, ephemeral
  JWT/credential keys); they are expected on this box, not errors.

Unreleased `main` deploy (2026-10-04): there is no tag after `v0.2.0` yet, so the
box was moved to `main` at `9ea55ce` (`v0.2.0-41-g9ea55ce`, PRs #134-#142: server
password auth and edit, installer hardening, runtime version tag, add-server wizard
layout, truthful dashboard data, OAuth avatar persistence). It was first deployed at
`0378f65` (`v0.2.0-21-g0378f65`) the same day and redeployed with no new migrations
(`00029`/`00030` were already applied). Same procedure with a
commit instead of a tag and a dev stamp (`git checkout -f 0378f65`, build with
`-X main.version=0.2.1-dev`), plus:

- `git clean -fdq internal/server/webdist` first: earlier builds left untracked
  assets there that would otherwise be embedded in the binary.
- Migrations `00029_server_password` and `00030_private_key_teams` applied with
  `gotham migrate up`.
- Agent swap order that works: `systemctl stop gotham-agent`, `install -m 0755
  bin/gotham-agent /var/lib/gotham-agent/bin/gotham-agent`, restart `gotham`,
  then start `gotham-agent`. The CP logs one `agent unavailable` warning during
  the gap; the agent re-registers with `cp_version=0.2.1-dev`.
- Smoke after deploy: both units `active`, `/healthz` 200, `GET /api/v1/version`
  401 unauthenticated, `PATCH /api/v1/servers/{id}` 401 unauthenticated.
- Registration is closed on this box, so authenticated checks need an existing
  account. The `demo@gotham.dev` account's password was reset on 2026-10-04 for a
  UI walkthrough (the password is not recorded here; reset it again if you need
  to). Verified in a browser through an SSH tunnel on 2026-10-04: sidebar tag
  `v0.2.1-dev`, round header avatar, add server with a password, edit and delete a
  server, dashboard `Running applications 2 / 2`, sidebar role `owner`, install
  snippet with `--full`, login page without the sample footer.
- **Redeploy at `0f6341c` (2026-10-04, same day, no new migrations after `00031`):** `main`
  after PRs #144-#162: form feedback/spacing/row layout (JUS-16..19), install card removed
  (JUS-20), first registered account is platform admin with migration `00031_platform_admin`
  (JUS-21), installer creates the first admin (JUS-22: the interactive prompt flow was only
  verified in containers, this box already has accounts; `gotham admin exists` and
  `admin create --password-stdin/--generate-password` are live here), web client moved to
  `app/`, `shared/`, `features/<module>/` with no oversized SFC left (JUS-24), zod validation
  and `@vueuse/core` (JUS-23). Verified in the browser: every page loads, every form modal opens
  with zero overflowing elements and the required marks present, no console errors. The two
  accounts that existed before `00031` are not platform admins; `demo@gotham.dev` is an operator
  through `PLATFORM_ADMINS` (drop-in above).
- **Browser automation gotchas (Orca embedded browser):** a background tab does not run
  `requestAnimationFrame`, so Naive UI/Vue transitions can stay stuck mid-way (for example a
  field hint that never swaps for its error message, class `fade-down-transition-leave-active`
  left on the element). That is a harness artifact, not an app bug: bring the tab to the front
  or check the DOM state, not a screenshot taken right after the click.
- **CI note:** the `DB-backed tests` job used to fail intermittently with
  `TestServiceRegisterClosedWithoutInvite` because closed-instance tests shared the database
  with other packages' cleanups; fixed in #162 (those tests now use private scratch
  databases). The `proto lint + drift check` job can fail with a GitHub API rate limit in
  `Set up buf` (no token supplied): rerun it.
- **Browsing the UI from a workstation (2026-10-04):** forward the CP port with
  `ssh -f -N -L 18000:localhost:8000 gotham` and open `http://localhost:18000`.
  In Orca's embedded browser, `orca click` did not open the Naive UI modals;
  calling `button.click()` through `orca eval` did. A backgrounded tab freezes CSS
  transitions, so modals screenshot half-faded unless transitions are disabled
  first (`*{transition:none!important;animation:none!important}`). Close the
  tunnel with `pkill -f 'ssh.*18000:localhost:8000'`.
- **Prefer the Playwright MCP over the Orca browser for UI checks (2026-10-04, JUS-15):** it
  has a real viewport (`browser_resize` to 900 or 1280, no CLI limit), real clicks that open
  Naive UI modals, no background-tab frozen transitions, and element screenshots
  (`browser_take_screenshot` with `target`). Recipe: open the SSH tunnel above, then
  `browser_navigate` to `http://localhost:18000/login`, `browser_resize`, `browser_fill_form`
  (`input[type=text]`, `input[type=password]`), click `button[type=submit]`, then measure with
  `browser_evaluate` and screenshot. The profile starts logged out: reset the password first with
  `gotham admin reset-password --email demo@gotham.dev --password <tmp>` (revokes sessions,
  including the Orca browser's). Console 401s before login are expected. Keep screenshots out of
  the repo (`filename` must stay inside the repo roots: an absolute scratchpad path is refused with "outside allowed roots"; use `.playwright-mcp/<name>.png`, which is gitignored and also the default; a bare relative name lands in the repo root and must be deleted).
- **Redeploy at `b22fd08` (2026-10-04, migrations `00032_profile_display_name` and
  `00033_session_metadata`):** the profile feature (JUS-25..JUS-27, JUS-28; plan
  `docs/plans/12-user-profile.md`) is live: `/settings/profile` with display name, change
  password and an active-sessions list (end one / end all others). Verified through the
  Playwright MCP at 1280px and 480px: display name updates the sidebar, a password change
  keeps the current session and kills the old password, ending a device removes its row,
  "Sign out all other devices" leaves only this device, and the Account card shows "Platform
  admin" for the env operator (`PLATFORM_ADMINS`) account. The `demo@gotham.dev` password was
  changed again during that check (not recorded; reset it with `gotham admin reset-password`
  when needed) and its display name is now "Demo Operator".
- The CP's CSP allows exactly one remote image host, `avatars.githubusercontent.com`
  (`img-src 'self' data: https://avatars.githubusercontent.com`), shared with the
  OAuth avatar validator. GitHub OAuth is the only provider, and it has not been
  exercised on this box (no OAuth app configured), so avatar rendering from GitHub
  is covered by tests only.


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

## Verified on real hardware (2026-09-27, box updated to v0.2.0 on 2026-10-03)

On 2026-10-03 the box was updated from a `0.1.2-dev` control plane / `0.1.1`
agent to **v0.2.0** (both binaries), the database migrated through
`00028_backup_was_running`, and the agent re-registered against the new control
plane (`cp_version=0.2.0`, node `test-node-1`). The two new migration-gated
surfaces (credential versioning, server host-key fingerprint, restores,
`was_running`) are now present on this host.

Environment prerequisites installed for the current feature set:

- `railpack` 0.40 on PATH and a `buildkit` container with
  `BUILDKIT_HOST=docker-container://buildkit` in
  `/etc/systemd/system/gotham.service.d/buildkit.conf` (Railpack apps).
- `docker-compose-plugin` (Compose v2+) on the node's PATH: the agent shells
  out to `docker compose` for Phase 7 services and writes each project's
  compose file under `GOTHAM_AGENT_COMPOSE_ROOT` (default
  `/var/lib/gotham-agent/compose`).
- `servers.ip = 127.0.0.1` for `test-node-1` (the all-in-one registration has
  no operator address; the CP dials the agent on the node port 9443).
- Dev mode: no CA configured, so CP and agent speak plaintext. This is opt-in
  on both sides: the CP needs `GOTHAM_GRPC_INSECURE=true` (a CA-less `serve`
  otherwise refuses to start) and the agent `GOTHAM_AGENT_INSECURE=true`.

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

- **Platform operators (BE-8.2, JUS-21):** the node-wide `POST /api/v1/proxy/sync` and
  the DNS-provider CRUD require a platform operator. An API token holding the
  `admin` scope always passes; a session (JWT) passes with an `admin` role
  claim or when the account email is listed in the comma-separated
  `PLATFORM_ADMINS` environment variable. **On a fresh instance the first
  registered account is automatically the platform admin** (its sessions carry
  the `admin` claim), so no `PLATFORM_ADMINS` entry is needed there; on
  upgraded instances with existing accounts nothing changes (all rows migrate
  as non-admin) and an operator must still set `PLATFORM_ADMINS` (or mint an
  admin-scoped token) to manage global DNS providers. Takeover risk: on a
  fresh instance whoever registers first becomes the admin, so create that
  account before exposing an unconfigured public instance. Per-application
  certificates and redirects are not affected: they stay with the owning
  team's `owner`/`admin` members.
  The team role `admin` is not a platform role. Minting an `admin` token
  itself needs operator access. Making the first registered account the
  platform admin was done in Linear JUS-21.
- **API-token scopes (FX-2c):** the resource routes enforce the token scope
  boundary — reads (`GET`/`HEAD`) need `read`, mutations need `deploy`, and
  platform-management surfaces need `admin`. The decrypted database credentials
  read (`GET /api/v1/databases/{id}/credentials`) requires `deploy`, not `read`,
  because it returns the database and root passwords; application environment
  reads (`GET /api/v1/applications/{id}/env`) stay `read`. JWT sessions hold
  every scope, so the SPA is unaffected. The project and environment routes
  (Phase 13 PE-1, JUS-30: `GET/POST /api/v1/projects`, `GET/PATCH/DELETE
  /api/v1/projects/{id}`, `GET/POST /api/v1/projects/{id}/environments`,
  `PATCH/DELETE /api/v1/environments/{id}`) ride the same chain: reads need
  `read`, mutations need `deploy` plus an owner/admin team role.
- Development mode: when there is no CA (empty `GOTHAM_CA_DIR`), the control
  plane refuses to start unless `GOTHAM_GRPC_INSECURE=true`, and the agent
  unless `GOTHAM_AGENT_INSECURE=true`; both then dial/serve plaintext. As soon
  as a CA exists, registration issues certificates and both sides use mTLS.
- Go toolchain: install a Go release matching `go.mod` before `make build`.
- Ports used on this host: 8000 (CP HTTP), 9442 (CP gRPC), 9443 (agent
  DockerService), 5432 (Postgres), 6379 (Redis).
- This box holds no production data; wiping the dev database or reinstalling
  the agent is always acceptable.
