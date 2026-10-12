# Retro review Phases 0–5 — summary

Dual-reviewer retro review of all code merged up to Phase 5 (PRs #1–#54), run
2026-10-02/03. **Complete 2026-10-03:** 15 review units, ≈236 deduped findings
(1 CRITICAL, 40 HIGH, ~120 MED, ~75 LOW), shipped as 16 fix clusters in PRs
#100–#130 (`main` @ `2219d78`), each with dual review and green CI. Per-cluster
triage, fix rounds and reviewer output are history: see the PRs.

## Fix clusters

| Cluster | Scope | PR |
|---|---|---|
| FX-1 | Secrets at rest | #100 |
| FX-2a–f | OAuth, sessions, FE session, logout replay, API tokens, HTTP hardening | #101–#106 |
| FX-3 | gRPC / CA | #108 |
| FX-4 | Git/SSH host trust | #107 |
| FX-5a/b | Storage host binds; registry isolation + remote builds | #109, #110 |
| FX-6a/b | Deploy state machine; config atomicity + redaction | #111, #112 |
| FX-7a/b | Agent runtime / gRPC taxonomy; installer + backoff | #113, #114 |
| FX-8a/b | DB lifecycle/fencing; engine/retention/readiness | #115, #116 |
| FX-9a/b | Restore execution; backup targets/store/scheduler | #117, #118 |
| FX-10a/b | Webhooks / deploy keys; providers | #119, #120 |
| FX-11 | Build pipeline | #121 |
| FX-12a/b | Realtime backend / frontend | #122, #124 |
| FX-13a/b | Foundation; servers hardening | #123, #126 |
| FX-14a/b | App/wizard web; databases web | #125, #127 |
| FX-15a/b | Shell/nav/a11y; theme/design-port | #128, #129 |
| FX-16 | CI/test/tooling gaps | #130 |

## Accepted residuals (owner-approved, not code fixes)

- Register-only: A2-18, A2-20, D1-10, D3-17 subset, B2-13, C4-21 (access-JWT
  post-logout window, localStorage refresh TTL, documented design adaptations).
- FX-7b: 4 fail-closed installer parity LOWs (trailing dot, leading-zero test,
  stricter IPv6 forms, header index).
- FX-8a: unconditional image-pull tradeoff; error rows keep their ports.
- FX-11: a glued `**` in `.dockerignore` is recursive for Docker, not for the CP.
- FX-12a: batch id absent, `replay` key assertion, stale start callbacks,
  seq-based upgrade path. FX-12b: per-channel buffer eviction (LRU if needed).
- FX-13b: enrollment association when node id ≠ address.
- FX-14a: 4 LOWs (stale `loading` clear, `fetchAll` teardown guard,
  `reposError` late failure, wizard call-site coverage). FX-14b: page-wiring
  test for restore toasts.
- FX-15b: `connectionDisplay` masks only the first `:…@` (use `new URL()`).
- FX-16: `TestStoreCreateFirstUserSerializes` skips on a fresh CI DB; Redis
  500 ms ping may need ~2 s on a cold runner.
- Backup artifact reaping for purged databases: tracked in `docs/TODO.md` (FX-9).
