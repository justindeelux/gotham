# Phase 3 — Docker Engine Core (W5)

**Goal:** raw container management on every node — the base layer for Applications, Databases, Services.

**Exit criteria (Milestone M3):**
- [ ] List containers on any node (added in Phase 2).
- [ ] Start / stop / restart containers via UI.
- [ ] Realtime log streaming via WebSocket; dead containers → live status update.
- [ ] Pull image + run raw container (preparation for deploy).

**Rollback:** container operations via the agent are control commands — if the UI breaks, only display is affected, no data is touched. Redis pub/sub can be disabled with the env flag `REALTIME_ENABLED=false` (5s poll fallback).

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to Phase 4 (see Model policy & Phase gate in `00-roadmap.md`).

---

## BE-3.1 — Containers service (CP) — `ws/p3-containers`

- **Context brief:** CP-side domain service routing Docker commands via gRPC to the agent. Never call Docker directly from the CP. Cache container state (from heartbeats / last command) in Redis to avoid hammering the agent.
- **Deliverables:**
  - `internal/containers/` (new package): `ContainerService` interface (List(serverID), Start, Stop, Restart, Pull, Run), routing to the right agent (from the Phase 2 registry), Redis cache with 10s TTL.
  - Routes `/api/v1/servers/{id}/containers` + action endpoints; map responses to a shared DTO (id, name, image, state, ports, created).
  - Unit tests with a mock agent gRPC; cache invalidation tests.
- **Verify:** `go test ./internal/containers/...`; curl container list on the local server and see really-running containers.
- **Depends on:** Phase 2.

## BE-3.2 — Realtime: WebSocket hub + Redis pub/sub — `ws/p3-realtime`

- **Context brief:** the shared realtime channel for the whole project (Phase 4 deploy logs go through it too). Architecture: agent streams logs → gRPC → CP publishes to Redis channel `logs:{serverID}:{containerID}` → hub subscribes → pushes via WebSocket to subscribed clients. Clients connect to `WS /api/v1/ws?token=...`.
- **Deliverables:**
  - `internal/server/ws/`: hub (client register/unregister, broadcast), Redis pub/sub bridge, auth via query token.
  - Agent side: `StreamLogs` using Docker logs follow, pushing chunks (contract already in Phase 2 — extend if missing).
  - Poll fallback when Redis is off (env flag).
  - Tests: fake stream → 2 WS clients receive the same data.
- **Verify:** `docker run -d` a container logging continuously → open WS and see realtime logs; kill the agent network → client receives a disconnect notice.
- **Depends on:** Phase 2. Parallel with BE-3.1.

## FE-3.1 — Containers UI — `ws/p3-containers-ui`

- **Context brief:** containers page for the selected server: table (name, image, state, ports) + start/stop/restart buttons; clicking opens a log terminal drawer (Coolify-style: monospace, auto-scroll, pause/clear buttons).
- **Deliverables:** `/servers/{id}/containers` page, `LogViewer` component (reused in Phase 4), `useWebSocket` composable (reconnect, auth token).
- **Verify:** e2e: start/stop an Nginx container → table status changes; log viewer streams correctly.
- **Depends on:** BE-3.1 + BE-3.2.
