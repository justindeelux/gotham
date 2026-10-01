# Phase 2 — Server Management + Agent (W3–W4)

**Goal:** node management infrastructure — the CP controls remote machines via a gRPC mTLS agent. This is a **contract-design** phase; the proto part needs `code-reviewer` design review.

**Exit criteria (Milestone M2):**
- `buf lint && buf generate` clean; the `proto/agent/v1` contract is versioned.
- Run the agent on 1 machine (local Docker) → CP receives heartbeats, shows `ready` in the UI.
- Add-server wizard: enter IP + SSH key → validate (Docker version, CPU, RAM, disk) → show results.
- Install script `deploy/install-agent.sh` installs the agent + systemd unit on the target machine.

**Rollback:** everything new is an add-on (server registry, agent). If it breaks: the agent exits on its own when it loses the CP connection (safe), the `servers` table can be wiped on dev. Phases 0–1 are unaffected.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to Phase 3 (see the phase gate in [`../../AGENTS.md`](../../AGENTS.md)).

---

## BE-2.1 — gRPC contract (buf) — `ws/p2-proto`

- **Context brief:** design the full CP↔Agent protocol in `proto/agent/v1/`. Principles: **version in the package name** (e.g. `agent.v1`), every RPC stream-friendly, explicit optional fields (use `optional` / `google.protobuf.Timestamp`). mTLS: self-signed CA managed by the CP, per-agent certs issued by the CP at registration.
- **Deliverables:**
  - `buf.yaml`, `buf.gen.yaml` (generate Go; pick ONE of `connectrpc` or plain gRPC — plain gRPC recommended for simplicity).
  - `proto/agent/v1/agent.proto`:
    - `Register(RegisterRequest{node_id, os, docker_version, arch, total_mem, total_disk}) → RegisterResponse{cert, cp_version}`.
    - `Heartbeat(stream)`: agent sends every 10s `{cpu_usage, mem_usage, disk_usage, container_count}`.
    - `DockerService`: `ListContainers`, `StartContainer`, `StopContainer`, `RestartContainer`, `PullImage`, `StreamLogs(stream container_id) → LogChunk`, `CreateContainer`, `RunImage`.
    - `UpdateService`: `RequestUpdate` (skeleton, implemented in Phase 9).
  - `Makefile` target `proto`.
- **Verify:** `buf lint` clean; `buf generate` outputs Go code that compiles (stub server/client ping-pong in tests).
- **Depends on:** Phase 0. **Blocks tasks 2.2/2.3.**

## BE-2.2 — Agent binary — `ws/p2-agent`

- **Context brief:** the second binary in the repo, in `agent/` (must NOT import `internal/`). Use the **Docker Engine API via the `/var/run/docker.sock` socket** (no heavy SDK — either `docker/docker/client` or a hand-written HTTP client, pick one). The agent keeps a gRPC connection to the CP, runs a heartbeat loop, and executes Docker commands.
- **Deliverables:**
  - `agent/`: `main.go`, `docker.go` (client + list/start/stop/pull/log stream), `grpc_server.go` (implements the BE-2.1 contract, streams logs from Docker events), `heartbeat.go`, TLS helper (reads the Gotham-issued cert).
  - `deploy/gotham-agent.service` (systemd unit), `deploy/install-agent.sh` (download binary, place cert, enable service).
  - Tests: mock Docker daemon → call list/start/logs RPCs.
- **Verify:** `make build` → run `bin/gotham-agent` locally against a stub CP → heartbeat logs visible; manually cross-check against the socket with `curl`.
- **Depends on:** BE-2.1. Parallel with BE-2.3.

## BE-2.3 — CP: gRPC gateway + server registry + SSH validation — `ws/p2-gateway`

- **Context brief:** CP side: run the gRPC server (mTLS, self-managed CA), registry of connected agents, `servers` store. SSH validation: Gotham SSHes into the target machine (`golang.org/x/crypto/ssh`, passphrase-protected key support) to check Docker version/CPU/RAM/disk before issuing a cert.
- **Deliverables:**
  - `servers` migration (id, name, ip, port, ssh_user, ssh_key_id (FK to the `private_keys` table — create it now, reused in Phase 4), status enum, agent info, timestamps), `private_keys` table (encrypted at rest).
  - `internal/server/` (domain): `ServerService` (Add, Validate, Delete), gRPC gateway (handle Register/Heartbeat → update store + Redis), `internal/server/ssh.go` (validation).
  - Routes: `POST /api/v1/servers/validate`, `GET/POST/DELETE /api/v1/servers`.
  - Certificate authority: `internal/server/ca.go` (generate CA, issue agent certs at registration).
- **Verify:** integration test: local agent registers → DB shows a `ready` record, heartbeats update `last_seen`; validate 1 real machine (or dev VM) successfully.
- **Depends on:** BE-2.1. Parallel with BE-2.2 (run the joint test at the end).

## FE-2.1 — Servers UI — `ws/p2-servers-ui`

- **Context brief:** server management page: list (name, IP, ready/offline status badge, CPU/RAM/disk from heartbeats — 5s poll or WS), add-server wizard (enter IP/SSH → call validate → show per-step results: Docker check, CPU, RAM, disk → "Install agent" button runs the install script → status flips to `ready`).
- **Deliverables:** `/servers` page + 3-step wizard; Pinia `servers` store; status badge component.
- **Verify:** e2e: add a local server → validate shows OK per item → after running the agent it shows `ready` + metrics updating live.
- **Depends on:** BE-2.3 (API contract).
