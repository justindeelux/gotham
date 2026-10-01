# Phase 1 — Auth & Users (W2)

**Goal:** a complete authentication system — the foundation for all API and UI access control.

**Exit criteria (Milestone M1):**
- Sign up (email + password), sign in, sign out via UI.
- Login with GitHub OAuth.
- Access token (JWT) expiry → refresh token issues a new token.
- API returns 401 when the token is missing; auth middleware works correctly.
- API tokens (scoped) can be created / rotated / deleted.

**Rollback:** if something breaks midway: the `users/sessions` migrations are forward-only; run `goose down` on the dev environment (no real data yet) and revert the code. No other flow is affected since every later phase is the one depending on auth.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to Phase 2 (see the phase gate in [`../../AGENTS.md`](../../AGENTS.md)).

---

## BE-1.1 — Core auth: users, sessions, JWT — `ws/p1-auth`

- **Context brief:** extend the Phase 0 schema. Design to modern standards: password hashing with **argon2id** (not bcrypt), short-lived **JWT access tokens (15 min)** + long-lived **refresh tokens (30 days, rotating)** stored in the DB (`sessions` table), revocable. Chi middleware: take the Bearer token → put the user into context.
- **Deliverables:**
  - Migration: complete `users` (id uuid, email unique, password_hash, avatar, created_at), `sessions` table (id, user_id, refresh_hash, expires_at, revoked_at).
  - `internal/auth/`: `password.go` (argon2id, OWASP-standard params), `jwt.go` (sign/verify with Ed25519, user_id + role claims), `service.go` (Register, Login, Refresh, Logout, Me).
  - `internal/server/` routes: `POST /api/v1/auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout`, `GET /api/v1/auth/me`; `RequireAuth` middleware.
  - Unit tests: hash/verify, token roundtrip, refresh rotation, revocation.
- **Verify:** `go test ./internal/auth/...`; manual curl: register → login → me (200) → logout → me (401).
- **Depends on:** Phase 0.

## BE-1.2 — OAuth2 GitHub — `ws/p1-oauth`

- **Context brief:** use `golang.org/x/oauth2`. Flow: FE calls `/auth/github` (redirect) → callback exchanges the code → find or create the user (email from GitHub) → issue the token pair like a regular login. Config `OAuth{GitHub{ClientID, ClientSecret, RedirectURL}}` in viper. The provider structure must be an **`OAuthProvider` interface** so Phase 4 can add GitLab/Gitea without touching the core.
- **Deliverables:** `OAuthProvider` interface (AuthCodeURL, Exchange, Identity), GitHub implementation; callback routes; tests with a mock provider (no network calls).
- **Verify:** mock tests green; manual run with a dev GitHub OAuth app (localhost callback).
- **Depends on:** BE-1.1.

## BE-1.3 — API tokens — `ws/p1-tokens`

- **Context brief:** API tokens for integrations (used by webhooks/CLI in Phase 4+). Tokens are generated once, only the **hash** is stored in the DB, the plaintext is shown exactly once. Minimum scopes: `read`, `deploy`, `admin` (mapped to roles later).
- **Deliverables:** `api_tokens` migration (id, user_id, name, hash, scopes[], last_used_at, revoked_at); service + CRUD routes `/api/v1/tokens`; auth middleware accepts both Bearer JWT and API tokens.
- **Verify:** unit tests for hash + scope checks; curl: create token → call API with token → revoke → 401.
- **Depends on:** BE-1.1. Parallel with BE-1.2.

## FE-1.1 — Auth UI + session store — `ws/p1-auth-ui`

- **Context brief:** build on the Phase 0 scaffold. Pinia `auth` store (user, accessToken, refreshToken — persisted to localStorage), axios interceptor: attach Bearer, on 401 call refresh once then retry, if refresh fails → redirect to `/login`. Router guard: protect routes requiring auth.
- **Deliverables:** `/login`, `/register` pages (Naive UI forms, email/password validation), "Sign in with GitHub" button, header showing the user + logout; call `GET /auth/me` on reload to restore the session.
- **Verify:** manual e2e: register → enter dashboard; F5 keeps the session; access token expiry (wait 15 min or shorten in dev) → auto refresh.
- **Depends on:** BE-1.1 (API contract), BE-1.2 (GitHub button).
