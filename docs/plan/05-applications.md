# Phase 4 — Applications (Deploy from Git) (W6–W8) ⭐

**Goal:** the core deploy flow — from Git repo to running container. The biggest phase; orchestration (4.3) uses the strongest review model.

**Exit criteria (Milestone M4):**
- [ ] Connect GitHub/GitLab/Gitea, list repos/branches.
- [ ] Deploy an app from a public GitHub repo (e.g. a sample Next.js app) → build → run container → reachable via port.
- [ ] Build without a Dockerfile (Railpack/Nixpacks or Buildpacks auto-detect).
- [ ] Persistent storage, env vars, secrets work.
- [ ] Push code → webhook → auto deploy, build logs visible realtime in the UI.
- [ ] **Gate G1**: `code-reviewer` (review model `openrouter/z-ai/glm-5.3-prime`) + `e2e-runner` run the e2e deploy scenario before merge.

**Rollback:** deploy is a state machine persisted in the DB (`deployments` table) — each new deploy keeps the old image tag; the "Rollback" button in FE-4.1 points back at the old container. If the phase breaks: disable with the env flag `FEATURE_APPLICATIONS=false`, Phases 0–3 unaffected.

**Phase gate:** continuous mode applies (see 00-roadmap.md) — after the exit criteria are met and Gate G1 passes, continue to Phase 5/6 without stopping. Gate G1 itself still blocks: no Phase 4 merge without `code-reviewer` + `e2e-runner` approval.

---

## BE-4.1 — Source providers — `ws/p4-providers`

- **Context brief:** extend the Phase 1 OAuth into "source providers". Each provider: its own OAuth app (config stored in the `providers` table), API to list repos (public + private) and branches, webhook creation (in 4.4). Reuse the `OAuthProvider` interface from Phase 1, promoted to a `SourceProvider` interface (ListRepos, ListBranches, CreateWebhook, ExchangeToken).
- **Deliverables:** GitHub + GitLab + Gitea implementations (Bitbucket to backlog); `providers` + `repos_cache` migrations; routes `/api/v1/providers`, `/api/v1/providers/{id}/repos`; unit tests with mocked provider APIs.
- **Verify:** connect a dev GitHub app → list the test account's repos.
- **Depends on:** Phase 1. Parallel with BE-4.2.

## BE-4.2 — Build engines — `ws/p4-builds`

- **Context brief:** 4 engines per the README: **Dockerfile**, **Railpack** (replacing Nixpacks), **Buildpacks** (herokuish), **static**. Each engine implements the `BuildEngine` interface (Detect(repo, buildPack) → Build(ctx, opts) → image tag). Builds run **on the node (agent)** — the CP only orchestrates; images are pushed to an **internal registry** running on the node (`registry:2` container). Standardized output: image tag `gotham/{appID}:{deployID}`.
- **Deliverables:**
  - `internal/builds/`: interface + 4 engines; agent side: `BuildImage` RPC (receive the repo tarball or SSH git clone — decision: CP clones the repo → sends context to the agent via gRPC stream).
  - Internal registry: bootstrap a registry container on the node when a server is added; CP stores the info.
  - Tests: build a sample app (small Go app) with all 4 engines on the dev Docker.
- **Verify:** run a build per engine → image appears in the internal registry; `docker pull` works.
- **Depends on:** Phase 3. Parallel with BE-4.1.

## BE-4.3 ⭐ — Deploy orchestration — `ws/p4-deploy`

- **Context brief:** the heart of the system. Deploy state machine: `queued → cloning → building → pushing → starting → running | failed`, persisted in the `deployments` table, each step emitting events to Redis (for realtime logs). Run containers with env vars, secrets (AES-GCM encrypted in the DB, decrypted when sent to the agent), persistent storage (volume map), port mapping, post-start healthcheck. Parent resource: `applications` (id, name, provider, repo, branch, build_pack, base_domain, env, secrets, storage, port).
- **Deliverables:**
  - `applications`, `deployments`, `env_vars`, `secrets`, `storages` migrations.
  - `internal/deploy/`: orchestrator (worker pool, retry, timeout), events emitter, rollback (redeploy old image tag).
  - Routes: `POST /api/v1/applications/{id}/deploy`, `GET .../deployments`, `POST .../rollback`.
  - Tests: state machine with a mock agent; rollback tests.
- **Verify:** e2e deploy via API: sample repo → `running`, full log events; kill the container → healthcheck fails → status `failed`; rollback → back to the old version.
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
