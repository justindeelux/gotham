# Phase 15 — Add Resource flow & automatic Git sources

**Goal:** one "Add resource" entry point with brand-icon cards (Application / Service / Database), seven application source types, and Git source management where connecting GitHub/GitLab is automatic (no hand-copied client IDs, secrets or webhooks).

## Current state (verified)
- `internal/providers/` has GitHub, GitLab and Gitea OAuth-app sources (ListRepos, ListBranches, webhooks, deploy keys). The OAuth app is configured manually in the `providers` table.
- `web/src/features/applications/components/CreateAppWizard.vue` + `WizardSourceStep.vue` drive app creation; builds live in `internal/builds/` (Dockerfile, Railpack, Buildpacks, static).
- `features/services` and `features/databases` have their own create flows; there is no shared picker.

## Work packages
| Id | Title | Depends on | Linear | PR | Migration |
|---|---|---|---|---|---|
| GS-1 | Add-resource picker: cards with brand icon + description | — | JUS-57 | #202 | — |
| GS-2 | Source model: `application.source_type` enum + migration + wizard step | — | JUS-58 | #203 | 00037 |
| GS-3 | Public git repo source | GS-2 | JUS-59 | #204 | — |
| GS-4 | Private git repo source (deploy key / token) | GS-2 | JUS-60 | #208 | 00041 |
| GS-5 | GitHub App: automatic connect, installation tokens, repo list, webhooks | GS-2 | JUS-61 | #206 | 00038 |
| GS-6 | GitLab: automatic connect, repo list, webhooks | GS-2 | JUS-62 | #205 | — |
| GS-7 | Dockerfile source | GS-2 | JUS-63 | #207 | 00039 |
| GS-8 | Docker Compose source | GS-2 | JUS-64 | #210 | 00042 |
| GS-9 | Docker image source (+ private registry credentials) | GS-2 | JUS-65 | #209 | 00040 |
| GS-10 | Git sources management page | GS-5, GS-6 | JUS-66 | #211 | — |

## Design decisions
- **GS-2 first.** `source_type ∈ {git_public, git_private, github_app, gitlab_app, dockerfile, compose, image}`; forward-only migration, existing apps backfill to `git_public`/`github_app` based on their provider. Wizard step switches on this enum; deploy orchestrator picks clone-vs-pull from it.
- **Automatic GitHub connect = GitHub App manifest flow.** Gotham POSTs a manifest to `github.com/settings/apps/new`, GitHub redirects back with a code, Gotham exchanges it (`/app-manifests/{code}/conversions`) and stores app id, private key, webhook secret (AES-GCM, like other secrets). The user only clicks "Connect GitHub" and "Install". Clone/API calls use short-lived installation tokens (JWT signed with the app key); the webhook endpoint is the existing `/api/v1/webhooks/github`.
- **GitLab has no manifest flow.** Automatic = OAuth authorization (PKCE) against gitlab.com or a self-hosted URL, with the OAuth application created through the API when the user supplies an admin token once; per-repo webhooks and deploy keys are created automatically on app creation. Document the self-hosted limits in the PR.
- **Private git** reuses the per-application ed25519 deploy key (`private_keys`) from Phase 4; token-over-HTTPS as the alternative.
- **Dockerfile / Compose / image** skip the build-pack detect step: Dockerfile = pasted content or repo path; Compose reuses the Services compose runner; image = `image:tag` with optional registry credential and optional digest pinning.
- **UI** follows `docs/design/applications.html` and the Add-resource mockup (create one under `docs/design/` before GS-1 if missing); brand icons are inline SVG in `shared/` (no CDN), English copy plus `vi.ts` values.

## Verify (per package)
`go build ./...`, `go test ./...`, `golangci-lint run`, `go vet`, `npm run build`, `npm run type-check`, rebuilt `webdist` committed. GS-5/GS-6 get fake-provider integration tests; no external network in CI.

## Out of scope
Bitbucket, Gitea changes, per-branch preview rework, org-level multi-installation UX beyond a list.

## Outcome (2026-10-09)
All ten packages merged (GS-2 #203 → GS-3 #204 → GS-6 #205 → GS-5 #206 → GS-7 #207 → GS-9 #209 → GS-4 #208 → GS-10 #211 → GS-8 #210). Migration numbers follow merge order: 00037 GS-2, 00038 GS-5, 00039 GS-7, 00040 GS-9, 00041 GS-4, 00042 GS-8.

Decisions taken during review (each package had 2-6 review rounds by Claude Code and an OpenCode subagent):
- GitHub connect: manifest flow with browser-reachable callbacks identified by a single-use, user-bound state; installations are verified with GitHub; applications link to their app (`applications.github_app_id`); the token clone path runs only for linked apps with http(s) URLs, legacy provider=github deploy-key apps are untouched.
- GitLab: OAuth 2.0 + PKCE, optional one-time admin-token provisioning (never stored), disconnect refused with 409 while applications use the connection. Self-hosted limits: see `05-applications.md`.
- Private git: per-app ed25519 deploy key or sealed HTTPS token (TLS only, host-bound askpass helper, redirects refused); `git_public` refuses ssh.
- Docker image: registry credential bound to the registry host it was entered for; digest recorded and used by rollback.
- Compose: strict allowlist (`composeguard`) enforced on the control plane and the node (confined by default, capability probe refuses old agents); raw document stored per deployment, secrets re-rendered on rollback. Upgrade the control plane before the agents.
- Git sources page: per-user connections, so disconnect usage covers only the active team's applications.

Follow-ups: JUS-67 (host deny-list / SSRF for keyless clone and ls-remote), JUS-68 (GitHub App clone hardening residuals). Not verified against live GitHub, GitLab or registries (fakes only).
