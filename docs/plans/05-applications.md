# Phase 4 — Applications (Deploy from Git) (W6–W8)

**Goal:** the core deploy flow — from Git repo to running container. The biggest phase; orchestration (4.3) needs `code-reviewer` design review.

**Exit criteria (Milestone M4):**
- Connect GitHub/GitLab/Gitea, list repos/branches.
- Deploy an app from a public GitHub repo (e.g. a sample Next.js app) → build → run container → reachable via port.
- Build without a Dockerfile (Railpack/Nixpacks or Buildpacks auto-detect).
- Persistent storage, env vars, secrets work.
- Push code → webhook → auto deploy, build logs visible realtime in the UI.
- **Gate G1**: `code-reviewer` + `e2e-runner` run the e2e deploy scenario before merge.

**Rollback:** deploy is a state machine persisted in the DB (`deployments` table) — each new deploy keeps the old image tag; the "Rollback" button in FE-4.1 points back at the old container. If the phase breaks: disable with the env flag `FEATURE_APPLICATIONS=false`, Phases 0–3 unaffected.

**Phase gate:** continuous mode applies (see 00-roadmap.md) — after the exit criteria are met and Gate G1 passes, continue to Phase 5/6 without stopping. Gate G1 itself still blocks: no Phase 4 merge without `code-reviewer` + `e2e-runner` approval.

---

## BE-4.1 — Source providers — `ws/p4-providers`

- **Context brief:** extend the Phase 1 OAuth into "source providers". Each provider: its own OAuth app (config stored in the `providers` table), API to list repos (public + private) and branches, webhook creation (in 4.4). Reuse the `OAuthProvider` interface from Phase 1, promoted to a `SourceProvider` interface (ListRepos, ListBranches, CreateWebhook, ExchangeToken).
- **Deliverables:** GitHub + GitLab + Gitea implementations (Bitbucket to backlog); `providers` + `repos_cache` migrations; routes `/api/v1/providers`, `/api/v1/providers/{id}/repos`; unit tests with mocked provider APIs.
- **Verify:** connect a dev GitHub app → list the test account's repos.
- **Depends on:** Phase 1. Parallel with BE-4.2.

### GitLab self-hosted limits (GS-6)

- **TLS trust:** the instance must serve TLS the control plane trusts. Plain `http://` instance URLs are refused for OAuth application provisioning (and self-signed CAs fail the call); loopback `http://` stays allowed in tests only.
- **Base URL root:** `base_url` must name the instance root (`https://git.example.com`, a pasted `/api/v4` suffix is trimmed); empty selects `gitlab.com` and is stored normalized, so one user cannot hold two connections for the same instance.
- **Admin token scope:** automatic OAuth application creation (`POST /api/v4/applications`) needs a token with `api` scope on an administrator account. The token is one-time: it authenticates the provisioning call only and is never stored, logged or returned. Without it, register the application manually with the exact redirect URI and scopes from `GET /v1/providers/gitlab/setup-info` (`api read_user read_repository`).
- **Redirect URL:** must be the exact public control-plane callback (`https://<cp-host>/api/v1/providers/gitlab/callback`); the API rejects a redirect naming any other host. After the provider redirects back, the control plane completes the connect (identity comes from the single-use PKCE state, no session needed) and lands the browser on `/providers/callback` with a result flag.
- **Disconnect:** refused with 409 while applications still deploy through the connection (the error names them). Stored tokens are deleted, not revoked at the Git host: an issued token stays valid there until it expires. A hook forgotten with `?force=true` is the other residual: it stays on GitLab and must be removed by hand.

## BE-4.2 — Build engines — `ws/p4-builds`

- **Context brief:** 4 engines per the README: **Dockerfile**, **Railpack** (replacing Nixpacks), **Buildpacks** (herokuish), **static**. Each engine implements the `BuildEngine` interface (Detect(repo, buildPack) → Build(ctx, opts) → image tag). Builds run **on the node (agent)** — the CP only orchestrates; images are pushed to an **internal registry** running on the node (`registry:2` container). Standardized output: image tag `gotham/{appID}:{deployID}`.
- **Deliverables:**
  - `internal/builds/`: interface + 4 engines; agent side: `BuildImage` RPC (receive the repo tarball or SSH git clone — decision: CP clones the repo → sends context to the agent via gRPC stream).
  - Internal registry: bootstrap a registry container on the node when a server is added; CP stores the info.
  - Tests: build a sample app (small Go app) with all 4 engines on the dev Docker.
- **Verify:** run a build per engine → image appears in the internal registry; `docker pull` works.
- **Registry isolation & auth (retro FX-5b):** the registry publishes its port on `127.0.0.1` only and joins a dedicated `gotham-registry` bridge network, so workload containers (default bridge) cannot reach it by container IP. It runs with an htpasswd credential generated on the node, stored mode 0600 in the agent state directory (`GOTHAM_AGENT_CERT_DIR`), and every agent push/pull uses it. The control plane never holds the credential: all image transfers are agent-mediated.
- **Node-side toolchains (retro FX-5b):** Railpack and Buildpacks builds run **on the target node** via `BuildImage`, so the control-plane host does not need `railpack`/`pack`. A node building those engines must have the CLI on `PATH` and, for Railpack, a BuildKit daemon (`BUILDKIT_HOST`). A build whose image cannot reach the node registry fails the `pushing` step before the previous container is retired.
- **Depends on:** Phase 3. Parallel with BE-4.1.

## BE-4.3 — Deploy orchestration — `ws/p4-deploy`

- **Context brief:** the heart of the system. Deploy state machine: `queued → cloning → building → pushing → starting → running | failed`, persisted in the `deployments` table, each step emitting events to Redis (for realtime logs). Run containers with env vars, secrets (AES-GCM encrypted in the DB, decrypted when sent to the agent), persistent storage (volume map), port mapping, post-start healthcheck. Parent resource: `applications` (id, name, provider, repo, branch, build_pack, base_domain, env, secrets, storage, port).
- **Deliverables:**
  - `applications`, `deployments`, `env_vars`, `secrets`, `storages` migrations.
  - `internal/deploy/`: orchestrator (worker pool, retry, timeout), events emitter, rollback (redeploy old image tag).
  - Routes: `POST /api/v1/applications/{id}/deploy`, `GET .../deployments`, `POST .../rollback`.
  - Tests: state machine with a mock agent; rollback tests.
- **Verify:** e2e deploy via API: sample repo → `running`, full log events; kill the container → healthcheck fails → status `failed`; rollback → back to the old version.
- **Runtime payload (BE-4.3c):** the container payload defaults `PORT` to the application's configured container port when the app declares a port and neither an env var nor a secret sets `PORT` — an explicit value (including a sealed `secret:` reference) always wins, and `port = 0` injects nothing, so Dockerfile apps that manage `PORT` themselves are untouched. This is what makes Railpack/buildpacks images (no Dockerfile) bind the port the host mapping points at, like Heroku/Railway/Coolify.
- **Managed storage binds (FX-5a):** a storage row whose `host_path` is empty is a managed bind derived as `GOTHAM_MANAGED_VOLUME_ROOT/<app id>/<name>` (default root `/var/lib/gotham/volumes`). An explicit `host_path` may be an absolute bind only when it is a direct child of `<root>/<app id>` (nested paths, the app directory itself, and any symlink component are refused); `/`, `/etc`, `/proc`, `/sys`, `/dev` and any `docker.sock` are always refused. A non-absolute `host_path` is a Docker named volume, namespaced per application as `gotham-app-<app id>-<name>` so it can never alias another application or a managed database volume (`gotham-db-*`). The node re-validates every bind and named volume against `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` (default the same root; it also falls back to the shared variable) so a compromised control plane cannot smuggle an arbitrary host path through.
- **Managed binds scope (FX-5a):** the confinement applies to **application** containers only. The node's own proxy mount (`/var/lib/gotham-agent/traefik`), managed database/backup containers (named volumes only) and — by design — the operator-facing Compose service remain on their own rules. A team owner/admin can still run arbitrary compose projects with host binds through the Services surface; that is a pre-existing, intentional capability and is **not** covered by this confinement. Do not read FX-5a as a platform-wide host-bind lockdown.
- **Legacy storage paths (FX-5a upgrade note):** a storage row stored before FX-5a with an out-of-root absolute `host_path` now fails at deploy time with a validation error until it is re-saved. Clearing `host_path` moves the bind to a fresh managed directory; it does **not** move the existing data, and a warning is logged when a save blanks a previously explicit path. A pre-change row that used a bare named volume (`cache`) is re-namespaced to `gotham-app-<app id>-cache`, so the old bare volume is no longer mounted and its data is **not** moved; a warning is logged at deploy time (and at save time when the row is re-saved). Relocate the data or keep an explicit direct child of `<root>/<app id>` as the remedy. The managed root is set by the pair `GOTHAM_MANAGED_VOLUME_ROOT` (control plane) and `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` (node; falls back to the shared variable). No backfill is performed.
- **Depends on:** BE-4.1 + BE-4.2.

## BE-4.3b — Applications CRUD + config + stop/start — `ws/p4-app-crud`

- **Context brief:** BE-4.3 mounted only the deploy/deployments/rollback routes; the FE-4.1 wizard codes against conventional REST (`GET/POST /applications`, `GET/PUT/DELETE /applications/{id}`, env/storage collections, manual stop/start). This package closes that gap; no new migration (00006 already has the tables).
- **Deliverables:** applications CRUD (ownership 404, validation 400), nested env + storage on create (values prefixed `secret:` become sealed rows), `PUT .../{id}/env`, `PUT .../{id}/storages`, `POST .../{id}/stop|start`; sqlc queries + wire envelopes matching `web/src/api/applications.ts`.
- **Verify:** create an app through the API → list/get → replace env/storages → stop/start; unit tests with the existing fakes.
- **Depends on:** BE-4.3. Unblocks FE-4.1 and BE-4.4 hook lifecycle.

## BE-4.4 — Webhooks & deploy keys — `ws/p4-webhooks`

- **Context brief:** auto-deploy on push. Create a webhook on the provider pointing at the public CP endpoint; authenticate via secret or deploy key (SSH keys reuse the `private_keys` table). Anti-spam: rate-limit + dedupe by commit SHA.
- **Deliverables:** routes `POST /api/v1/webhooks/{provider}` (verify signature) → trigger deploy; create/delete webhooks when apps are created/deleted; deploy keys for private repos.
- **Verify:** push a commit to the test repo → a deploy starts automatically (visible in DB + logs).
- **Depends on:** BE-4.3 + BE-4.1.

## BE-4.4b — SSH deploy keys for private repos — `ws/p4-deploy-keys`

- **Context brief:** BE-4.4 authenticates webhooks, but the cloner still clones anonymously, so private repos cannot be deployed. This closes that gap.
- **Deliverables:** generate an ed25519 keypair per application (reuse `private_keys`), register the public key with the provider (GitHub/GitLab/Gitea) and delete it with the application; clone via `GIT_SSH_COMMAND` with an ephemeral key file on the CP; unit tests with a fake provider + a local `git`+`ssh` fixture, no external network.
- **Verify:** deploy a private repo via SSH; deleting the app removes the key at the provider.
- **Depends on:** BE-4.4 + BE-4.3b (application lifecycle).

## FE-4.1 — Application wizard + deploy UI — `ws/p4-app-ui`

- **Context brief:** the product's main screen. App creation wizard: pick provider → repo → branch → build pack (auto-detect) → port/domain → env vars → storage → Deploy button. Detail screen: deployments list, realtime build/deploy logs (reuse the Phase 3 `LogViewer`), rollback, redeploy, stop/start buttons.
- **Deliverables:** `/applications` + `/applications/{id}` pages; Pinia stores; `DeployLogs`, `EnvEditor`, `StorageEditor` components.
- **Verify:** e2e: create an app from a public repo → deploy → see per-step logs → reach the app via port → push code → auto redeploy → rollback.
- **Depends on:** BE-4.3 (API contract finalized early so FE runs in parallel), BE-4.4 (webhook UI).

## QA-4.1 — E2E gate — `ws/p4-e2e`

- **Context brief:** automated e2e scenarios for all of Phase 4, running in a dedicated CI (`e2e.yml`) with a test Docker daemon. Use Playwright for the UI + direct API calls for the deploy flow. This is **gate G1** — blocks merge when red.
- **Deliverables:** e2e tests: (1) public repo deploys successfully, (2) rollback, (3) webhook auto-deploy, (4) failed build → `failed` status with error logs.
- **Verify:** full e2e run green on CI.
- **Depends on:** BE-4.3 + BE-4.4 + FE-4.1.

### How to run the suite

The suite lives in `internal/e2e` (`p4_*_test.go`) and is gated by `GOTHAM_E2E=1`,
same as the Phase 3 suite: without it every test skips, so plain `go test ./…`
stays green on machines without Docker, Redis or Postgres. Each scenario boots a
control plane in-process (real HTTP routes, real PostgreSQL, real Redis, a real
mTLS agent on the local Docker daemon) and cleans up after itself.

Locally, against Docker and the dev stack:

```bash
docker compose -f deploy/compose.dev.yml up -d     # Postgres 16 + Redis 7
GOTHAM_E2E=1 go test ./internal/e2e/... -count=1 -timeout 20m
```

Against the shared test box, or any host whose services do not listen on the
defaults — the suite needs `git`, the Docker daemon and the three env vars below
(all optional):

| Variable | Default | Meaning |
|---|---|---|
| `GOTHAM_E2E` | — | `1` runs the gated suite (anything else skips it) |
| `GOTHAM_TEST_DSN` | `postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable` | database the suite migrates and tests against |
| `GOTHAM_E2E_REDIS` | `127.0.0.1:6379` | Redis the deploy logs are published to |
| `GOTHAM_E2E_DOCKER_SOCK` | `/var/run/docker.sock` | daemon the agent builds and runs on |

```bash
GOTHAM_E2E=1 \
GOTHAM_TEST_DSN='postgres://gotham:gotham@<test-box>:5432/gotham?sslmode=disable' \
GOTHAM_E2E_REDIS='<test-box>:6379' \
go test ./internal/e2e/... -count=1 -timeout 20m -v
```

Fixture repositories are created on disk and cloned with `GOTHAM_DEV_CLONE_LOCAL=true`
(set by the harness); no provider API, no GitHub token and no external network
are used — webhook deliveries are signed and replayed against
`POST /api/v1/webhooks/github` with a seeded secret row.

CI: `.github/workflows/e2e.yml` (separate from `ci.yml`) starts Postgres 16 +
Redis 7 as service containers, applies migrations and runs the same command on
`ubuntu-latest`. It triggers on `workflow_dispatch`, on pushes to `main` and on
PRs touching `internal/deploy`, `internal/webhooks`, `internal/builds`,
`internal/store`, `internal/e2e`, `internal/server`, `internal/config`, `cmd`,
`agent`, `proto`, `go.mod`/`go.sum` or the workflow itself.

### G1 decisions (2026-09-27, owner-approved)

The API-level suite above is the G1 gate for Phase 4. Two QA-4.1 deliverables
were deferred by explicit owner decision:

- **Playwright UI leg** (QA-4.1 spec says "Playwright for the UI + direct API
  calls") moves to `ws/p4-ui-e2e` (QA-4.1b), which runs beside Phase 5/6.
  The API leg already exercises the real HTTP routes; only the browser layer
  is missing.
- **Network clone of a real public repo** is not part of CI (fixtures keep the
  gate deterministic). It was verified live on the shared test box before the
  waiver: `docker/welcome-to-docker` (Dockerfile) and
  `heroku/node-js-getting-started` (Railpack, no Dockerfile) both deployed to
  `running` and answered HTTP 200 through their host ports, including env vars
  and a persistent `/data` mount. A network-gated optional test can be added
  with QA-4.1b.
