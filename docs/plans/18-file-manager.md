# Phase 18 — Remote file manager & code editor

**Goal:** operators browse, transfer and edit files on a managed server from the UI: a file manager (list, create, rename, move, copy, delete, upload, download) plus a tabbed code editor with safe saves, scoped to confined roots on the node. Ported in spirit from the `pyramid` project (`api/files`, `app/components/files`, `app/components/codeEditor`), re-designed for Gotham's control-plane → agent architecture. Linear: JUS-103.

## Current state (verified)
- The file manager (read, write, editor, archives) is implemented; archives (step 9b, JUS-114) use Go `archive/zip|tar` + `compress/gzip` and no shell. `proto/agent/v1/agent.proto` (single proto file; `make proto`, `make proto-check`) has `AgentService`, `UpdateService`, `DockerService`, `BuildService`, `ProxyService`, `ComposeService`. Streaming templates: `DockerService.StreamLogs` (server-stream), `BuildService.BuildImage` (bidi), `FileService.Copy/Delete/Compress/Extract` (server-stream progress).
- Agent: runs as unprivileged `gotham-agent` (`deploy/gotham-agent.service`: `ProtectSystem=full`, `ProtectHome=true`, `SupplementaryGroups=docker`). gRPC server caps receives at 2 MiB, 64 concurrent streams (`agent/grpc_server.go:29-30,336`); the CP client sets no `MaxCallRecvMsgSize` (4 MiB default). The agent listener accepts any peer when `GOTHAM_AGENT_CA` is unset (`agent/tls.go:84-90`, TODO LOW-4), so **confinement and an opt-in switch must live on the agent**; confinement is not authentication.
- Roots: `ManagedVolumeRoot` (default `/var/lib/gotham/volumes`) and `ComposeRoot` (default `/var/lib/gotham-agent/compose`, inside the agent's own state dir) in `agent/config.go`.
- Precedents: `agent/volumes.go` (`validateAppVolume`, `rejectSymlinkComponents`, exact-match `isDangerousHostPath` at `:223`), `agent/proxy.go:393,431` (`writeFileInDir`, `openTrustedDir`). `RemoveVolume` re-validates on the agent.
- CP template module: `internal/containers` (`errors.go`, `client.go`, `service.go`, `routes.go`), wired in `internal/server/server.go:765-776` with `s.withTeam()`. `AuthorizeOptionalTeam` (`internal/teams/scope.go:67`) returns nil for servers **without** a team, even for writes — the files module must not inherit that.
- HTTP server `ReadTimeout` is 30 s for the whole request (`internal/server/server.go:56`).
- Web: HTTP client is `web/src/shared/api/http.ts` (Bearer header + `X-Team-Id`, 15 s default timeout). No editor library, no file components. `web/src/app/layouts/navigation.ts:32` has an inactive `files` nav stub. `internal/server/webdist` (2.4 MB) is committed and drift-checked.
- No files mockup in `docs/design/`.

## Spike results (step 0 gate)

**Status: PASSED (rerun) — the rebuilt box (fresh Ubuntu 22.04) was healthy and the live
read/write/list test ran as `gotham-agent` under both roots. The earlier BLOCKED status was a
failed disk on the old box and no longer applies.** Checked 2026-10-13 on the all-in-one box from
`docs/test-server.md`.

Enabling the opt-in file service surfaced one **defect**: on a stock install the default
`ComposeRoot` is rejected, so `GOTHAM_AGENT_FILES=true` aborts the agent (see below). The permission
question itself is answered, but Phase 18 cannot be enabled on a stock box until JUS-105 is fixed.

### Facts re-verified

- **Kernel:** `uname -r` = `5.15.0-60-generic` (the rebuild kept the same kernel; `openat2` is
  available since Linux 5.6, so `os.OpenRoot`'s fast path applies and the stdlib fallback still
  covers older kernels).
- **Agent identity:** `uid=116(gotham-agent) gid=122(gotham-agent)`. The running process carries
  supplementary groups `122` and `999(docker)` (`/proc/<pid>/status`), from the unit's
  `SupplementaryGroups=docker`; the `docker` group is gid 999. `id gotham-agent` alone does **not**
  list `docker`, because the membership is a unit-level supplementary group, not an `/etc/group`
  entry. **The uid/gid are allocated at install and are not stable** — the previous box had
  `998:998`; policy must key off the `gotham-agent` user and the `docker` group name, never a
  numeric uid.
- **Roots (both defaulted, no override):**
  - `ManagedVolumeRoot` = `/var/lib/gotham/volumes` (CP `internal/deploy/volumes.go`, agent
    `agent/config.go`). Parent `/var/lib/gotham` is `root:root 0755`; the root itself did not exist
    until a container bind created it.
  - `ComposeRoot` = `/var/lib/gotham-agent/compose`; parent `/var/lib/gotham-agent` is
    `gotham-agent:gotham-agent 0755`.
- **Docker:** Engine 29.9.0, data root `/var/lib/docker` on the same filesystem as both roots.
- **Traefik: not deployed on the rebuilt box** (no running container, no
  `/var/lib/gotham-agent/traefik` directory), so the timeout check could not be re-run against a
  live instance. The recorded config finding still holds from the generator:
  `internal/proxy/generate.go` emits entryPoints `web:80` / `websecure:443` carrying only `address`
  and **no `transport.respondingTimeouts`**, so Traefik v3 defaults apply (`readTimeout` 60 s,
  `writeTimeout` 0, `idleTimeout` 180 s); the CP's own `ReadTimeout` is 30 s
  (`internal/server/server.go:56`). At the edge Traefik caps reading a request **including its body**
  at 60 s, so a > 1 GiB tenant-application upload slower than that is cut off. The file API is served
  by the control plane directly and never traverses node Traefik, so this edge default is a
  tenant-application ceiling, not the file manager's concern, and is left at Traefik's default (see
  step 4).

### Live permission test (as `gotham-agent`)

Ownership is the whole story: Docker creates the bind source as `root:root 0755`, so an application
volume is **not writable by the agent**, which is a plain unprivileged uid with no
`CAP_DAC_OVERRIDE` and no group shared with container-owned trees.

| Target (mode) | list | read 0644 | read 0600 | create | mkdir | rename | delete |
|---|---|---|---|---|---|---|---|
| container bind, root-owned (0755) | ok | ok | EACCES | EACCES | EACCES | EACCES | EACCES |
| container bind, uid 1000 pre-provisioned (0755) | ok | ok | EACCES | EACCES | EACCES | EACCES | EACCES |
| agent-owned subdir under `ManagedVolumeRoot` (0755) | ok | ok | n/a | ok | ok | ok | ok |
| `ComposeRoot` (agent-owned parent, 0755) | ok | ok | n/a | ok | ok | ok | ok |

Detail and exit codes (non-zero rc after `runuser -u gotham-agent -- ...`):

- **dockerd-created bind `<root>/<appID>/data` is `root:root 0755`**; files written by a root
  container are `0644` / `0600 root:root`. As `gotham-agent`: `list` rc 0, read `0644` rc 0, read
  `0600` rc 1 (EACCES), create/append rc 2, `mkdir` rc 1, rename rc 1, delete rc 1, create directly
  in `<root>/<appID>` rc 2. A write would need the container's own uid/gid/mode, which the agent
  never has.
- **Non-root container (uid 1000):** with the bind dir auto-created by dockerd (`root:root 0755`),
  the container itself could not write into its mount (rc 1) — a uid-owned tree only appears when
  the dir is pre-provisioned or chowned by a root container. With the dir pre-provisioned
  `1000:1000 0755`, the same matrix as the root-owned case holds (agent reads `0644`, cannot read
  `0600`, cannot write).
- **Agent-created dirs:** the agent cannot create `ManagedVolumeRoot` itself (parent `/var/lib/gotham`
  is `root:root`, rc 1) — dockerd creates it. An operator-provisioned agent-owned subdir
  (`gotham-agent:gotham-agent 0755`) is fully usable (create/list/read/append/rename/delete all
  rc 0), as is the `ComposeRoot` (agent owns the parent; `rmdir` rc 0). Files the agent creates are
  `0644 gotham-agent:gotham-agent` (umask 022).
- **Named Docker volumes are irrelevant:** their files live under
  `/var/lib/docker/volumes/<name>/_data`, inside Docker's data root, which `validateFileRoot`
  refuses; they are never browsable.

### File-service enablement (opt-in) — defect found

- **Unset (baseline):** a loopback gRPC client calling `ListRoots` gets
  `code = Unimplemented desc = unknown service agent.v1.FileService` — the service is genuinely
  absent when off.
- **Enabled with the shipped unit and no overrides:** the agent **aborts startup**:
  `file service error="file root \"/var/lib/gotham-agent/compose\" is inside /var/lib/gotham-agent"`
  → exit 1, restart loop. Cause: `fileProtectedPaths` appends `{cfg.CertDir, exclusive: true}`, and
  `deploy/gotham-agent.service` sets `GOTHAM_AGENT_CERT_DIR=/var/lib/gotham-agent`, i.e. the whole
  state directory. The default `ComposeRoot` is a child of it, so `validateFileRoot` rejects it,
  even though the plain state-root entry is deliberately non-exclusive ("the compose child stays
  browsable read-only"). **On a stock install the two defaults are mutually incompatible, so Phase 18
  cannot be enabled without overriding `GOTHAM_AGENT_COMPOSE_ROOT`.**
- **Enabled with `GOTHAM_AGENT_COMPOSE_ROOT` pointed outside the state dir:** the agent starts and a
  loopback client's `ListRoots` returns `root=volumes writable=true`,
  `root=compose writable=false`, free space reported — **the `FileService` answers.**
- The systemd drop-in was removed and the agent restarted; the box is left with
  `GOTHAM_AGENT_FILES` off (`ListRoots` → `Unimplemented` again).

### Decision

- **No unit-file privilege change is warranted.** A unit-file edit cannot give a plain uid access to
  a root-owned `0755` directory or to another uid's `0600` file; only `sudo`, a capability
  (`CAP_DAC_OVERRIDE`) or root could, and the plan already excludes code-level privilege escalation.
  The measurement confirms the plan's expectation: container-created volumes are read-mostly for the
  agent (world-readable files listed and read; `0600` and every mutation denied).
- **Container-owned volumes:** keep `ManagedVolumeRoot` the writable root, but expect writes to fail
  outside agent-owned subtrees, so the per-entry "permission denied" UX stands. **Owner decision
  needed for JUS-106/108:** if operators must upload/edit inside container volumes, provisioning must
  make them agent-writable — either the CP chowns `<root>/<appID>` to `gotham-agent`, or a shared
  group plus a setgid (`2775`) app dir is introduced (setgid alone is insufficient until the agent
  joins the group). Until that decision lands, writes are limited to agent-owned roots.
- **`ComposeRoot`** is agent-owned and read-only in the service by design: the filesystem allows
  mutation, the service refuses it — matches the plan.
- **Pre-step-1 fix (JUS-105):** the cert-dir protection must not invalidate the default compose root.
  Recommended: protect the individual cert/key files (or mark the `cfg.CertDir` entry non-exclusive
  when it is or contains the browsable compose root) instead of blanket-excluding everything under
  `GOTHAM_AGENT_CERT_DIR`; the state-root entry already documents that the compose child stays
  browsable. Alternatives: move the default `ComposeRoot` out of the agent state dir, or document a
  mandatory `GOTHAM_AGENT_COMPOSE_ROOT` override. **Without this, `GOTHAM_AGENT_FILES=true`
  (JUS-105's own deliverable) cannot start on a stock box.**
- **Traefik:** the 60 s default `readTimeout` is a tenant-application edge ceiling; the file API is
  served by the control plane directly, so no explicit `respondingTimeouts.readTimeout` is set
  (see step 4).
- **Gate:** step 0 is **passed** for the permission question (measurements above). The stock-config
  startup defect is a JUS-105 fix, tracked before step 10's flag flip.

## Pyramid findings that drive the design
Pyramid's backend is an unrestricted filesystem API (single `resolvePath` = `filepath.Abs`), blind overwrite on save, whole-file string read/write, `sh -c` archive commands with a broken `shellQuote` (command injection), 200-status error paths, swallowed upload errors and no tests. Its frontend is a Monaco dock with a global key handler that hijacks Ctrl+V/Ctrl+A/Delete while typing in the editor, "new file" that truncates an existing file, stale editor tabs after rename, silent discard on delete/reload and no search. **None of that is ported.**

| Pyramid defect | Gotham requirement |
|---|---|
| No jail | Agent-side confined roots via `os.OpenRoot` (stdlib; openat2 where available, safe fallback elsewhere) |
| Special files (device/FIFO) readable | Open `O_NOFOLLOW|O_NONBLOCK`, `fstat`, regular files and dirs only |
| Blind overwrite, no conflict check | Strong etag on read; `If-Match` on write → 409 (428 if missing) |
| Create = `PUT ""` truncates | Create is exclusive; 409 if exists |
| Rename/move/copy overwrite silently | `overwrite=false` default → 409 |
| Whole-file string, no size/binary guard | Editor limit 2 MiB; binary/invalid UTF-8 → 415; download streamed |
| `sh -c` + shellQuote | No shell anywhere; Go `archive/*` (step 9b) |
| Errors returned as 200 | Sentinel errors → 400/403/404/409/413/415/428/429/501/502 |
| Upload swallows per-file errors | Per-file result with error; truncated uploads are never committed |
| Global keydown ignores focus | Shortcuts scoped to the list element; editor owns its keys |
| Rename leaves stale tabs | Store retargets/closes tabs under renamed/deleted paths; dirty tabs block |
| Save race clears dirty | Dirty cleared only if the doc version is unchanged since the save started |
| Hardcoded English | en + vi catalogs, parity test; UI maps HTTP status → i18n key |
| Zero tests | Tests per layer + traversal/symlink/special-file suite + e2e |

## Design decisions
- **Scope = files on a managed server node**, not container filesystems and not the control-plane host. Browsable roots are an agent-local allow-list: `ManagedVolumeRoot` (writable), `ComposeRoot` (**read-only**: composeguard validates at deploy and the next `ComposeUp` overwrites edits), plus optional `GOTHAM_AGENT_FILE_ROOTS` (colon-separated). `ListRoots` returns a `writable` flag per root. Files are addressed as `(root, relpath)`; absolute paths are rejected.
- **Agent opt-in:** `FileService` is registered only when `GOTHAM_AGENT_FILES=true` (default off, documented in `docs/install.md`). `FEATURE_FILES` on the CP defaults **off** until step 10 signs off; when off, routes are mounted and return 503 `ErrDisabled` (the `databases` pattern).
- **Root validation (`validateFileRoot`, new):** absolute, no symlink components (reuse `rejectSymlinkComponents`), not under `/etc /proc /sys /dev /run /boot /usr /home /root`, not an ancestor of or inside the agent state/cert/binary paths, `/var/lib/gotham`, `/etc/gotham`, or the Docker data root. Table test with those exact paths (the existing `isDangerousHostPath` is exact-match only).
- **Confinement:** `os.OpenRoot` per allowed root, no custom resolver (works on darwin dev machines and old kernels). Every open uses `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` then `fstat`; only regular files and directories are read/written/copied. `List` reports other types as `kind=special` with no actions. Top-level `<appID>` directories under `ManagedVolumeRoot` cannot be renamed or deleted (running containers bind-mount them). Rename/move/copy are **single-root only** (cross-root → 400).
- **Transport:** `FileService` on the agent. Full contract (read **and** write RPCs) is defined in step 1 so later steps never touch `agent.proto`; unimplemented methods return `Unimplemented` until their step lands. Unary: `ListRoots`, `List` (cap 5,000 entries + `truncated` + cursor), `Stat`, `Mkdir`, `Rename`, `Chmod`. Server-streams: `Read`, `Copy`, `Delete` (progress, recursive ops outlive the 30 s call timeout). Client-stream: `Write`. Chunks 256 KiB. The CP bridges streams to plain HTTP (streaming GET; `MultipartReader` streamed part-by-part, **never** `ParseMultipartForm`). No WebSocket.
- **Writes:** first `Write` message carries `path, size, expect_etag, overwrite, mode`. The agent commits only if received bytes == `size` (or a final `commit` message with sha256); on any CP-side body error the CP cancels the stream context, never closes it cleanly, so a truncated upload is never renamed in. Strategy: temp file + fsync + rename when the file is owned by the agent uid and `nlink==1`; otherwise **write in place** under a per-`(root,path)` mutex (check etag on the opened fd → statfs the fd → reserve the new length with `fallocate` when growing → write → fsync) so bind-mounted single files, hardlinks, ACLs and ownership survive. The free-space check reads the target descriptor's own filesystem and treats `free==0` (statfs failed) as refuse-unless-empty. Responses report which mode was used; the `in_place` residual non-atomicity is a documented deviation (see below).
- **Etag:** `dev:ino:mtime_ns:size` (RFC 7232 quoted); `/content` also returns a sha256 of the (≤ 2 MiB) body. Per-path mutex on the agent around check+commit with a re-`fstat` before rename. Writes by containers can still race (documented).
- **Limits (configurable on the agent):** `GOTHAM_AGENT_FILE_MAX_UPLOAD` default 1 GiB, `statfs` check keeping 5 % free, max 8 concurrent transfers per node (`ResourceExhausted` → 429).
- **RBAC:** the files `resolve` requires the server to have a team (or a platform admin) and always enforces `Role.CanWrite()` for writes; foreign team/missing node = 404. `list/stat/roots` = any member; **`content` and `download` = owner/admin** (file bodies hold secrets such as 0600 compose env). Owner may relax this to all members; revisit in step 4. Reads of content/download are audit-logged too.
- **Downloads:** `POST /download-ticket` → single-use ticket (60 s, bound to user, team, server, root, path); `GET /download?ticket=` consumes it so the browser can use a native streaming download. `ticket` is redacted from access logs. Sets `Content-Length`, `Content-Disposition: attachment`, `nosniff`; on a mid-stream agent error the handler aborts the connection (`http.ErrAbortHandler`) so the browser marks the download failed. Long transfers use `http.NewResponseController(w).SetReadDeadline/SetWriteDeadline`; the file API is served by the control plane directly, so no edge read timeout applies.
- **Old agents:** `codes.Unimplemented` → `ErrAgentOutdated` (501, "update the agent").
- **Audit:** structured `slog` (`actor_team_id`, `actor_user_id`, server, root, relpath, op, result); precedent `internal/servers/service.go`. No table.
- **Editor:** CodeMirror 6, dynamic import (own chunk), explicit language set (json, yaml, js/ts, python, go, markdown, css, html, shell, dockerfile, toml) — **not** `language-data` (~100 committed chunks). Line separator and BOM detected on load and preserved on save (CRLF file round-trips byte-identical). Monaco rejected (multi-MB in a committed `webdist`). Recorded as `F22` in `docs/library-audit.md`. CodeMirror injects styles (CSP currently allows `style-src 'unsafe-inline'`; note so nobody tightens it blindly). Editor lives inside the Files page, not a global dock.
- **Entry points:** route `files` (`/files`, server chooser → last-used/only server) and `server-files` (`servers/:id/files`); server-detail "Files" tab links to it; nav item highlights on both.
- **Deletion:** confirm modal then permanent delete inside the root (no trash this phase).
- **UX expectation:** volumes created by containers are usually root-owned, so many entries are read-only/unreadable for `gotham-agent`; the UI is read-mostly with clear per-entry "permission denied" states.

## API (all under `/api/v1/servers/{id}/files`)
| Route | Auth | Purpose |
|---|---|---|
| `GET /roots` | member | roots with `writable` + free space |
| `GET /list?root=&path=&cursor=` | member | entries (name, kind, size, mode, mtime, symlink target, etag), ≤ 5,000 |
| `GET /stat?root=&path=` | member | one entry |
| `GET /content?root=&path=` | owner/admin | text for the editor (413 > 2 MiB, 415 binary); `etag` + sha256 |
| `PUT /content?root=&path=` | owner/admin | save; `If-Match` required (428 if absent); 409 on mismatch; `?overwrite=true` explicit force |
| `POST /download-ticket` | owner/admin | single-use ticket |
| `GET /download?ticket=` | ticket | streamed attachment |
| `POST /upload?root=&path=&overwrite=` | owner/admin | streamed multipart, per-file result array |
| `POST /mkdir`, `/create` | owner/admin | exclusive create |
| `POST /rename`, `/move`, `/copy` | owner/admin | single root; `overwrite=false` default; copy-into-self guard |
| `POST /compress`, `/extract` | owner/admin | one archive inside a single root; 409 unless `overwrite`; 413 on an archive limit (step 9b) |
| `DELETE /entry?root=&path=&recursive=` | owner/admin | permanent delete |
| `POST /chmod` | owner/admin | octal, setuid/setgid/sticky rejected (step 9a) |

Errors `{message}`: 400 bad path/cross-root, 403 forbidden/permission denied, 404 not found/foreign team, 409 conflict/exists, 413 too large, 415 not editable, 428 missing precondition, 429 busy, 501 agent outdated, 502 agent unavailable, 503 disabled.

## Work packages (one PR each)
0. **Gate: permission spike** on the test server (`docs/test-server.md`); outcome recorded in this plan before step 1 code starts.
1. **Agent FS core + full `FileService` contract + read RPCs** — *strongest model*.
2. **Agent write RPCs.** Dep 1.
3. **CP read side.** Dep 1.
4. **CP write side.** Dep 2, 3.
5. **UI mockup.** No code dep.
6. **Web foundation + read-only browser.** Dep 3, 5.
7. **Web file operations.** Dep 4, 6.
8. **Web code editor.** Dep 4, 6; **serial after 7** (same page).
9a. **chmod** (JUS-113, built). Dep 2, 7.
9b. **Archives** (phase 2, optional). Dep 2, 7.
10. **Hardening, e2e, docs, flag default.** Dep all.

Parallelism: 5 ∥ 1→2; 3 starts once 1 merges (proto is complete in 1, so 2 ∥ 3 touch disjoint files); 6 ∥ 4; 9a ∥ 9b.

## Per-step detail
Common verify for code steps: `go build ./...`, `go test ./...`, `golangci-lint run`, `go vet`; `web/` steps: `npm run build`, `npm run type-check`, `npm test`, rebuilt `internal/server/webdist` committed; proto: `make proto-check`. GitNexus `impact` before editing any existing symbol, `detect_changes` before commit. Tests that need non-root skip when `os.Geteuid()==0`.

### Step 0 — Permission spike (gate)
- **Brief:** `docs/test-server.md`, `deploy/gotham-agent.service`, `agent/config.go`.
- **Tasks:** as `gotham-agent` on the test box, read/write/list under `ManagedVolumeRoot` and `ComposeRoot`, including container-created volumes; note ownership outcomes; confirm kernel version; confirm Traefik read/write timeouts. Record results in a "Spike results" section here.
- **Verify/Exit:** results recorded; decision on whether the unit file needs a change (separate ticket, never a code-level privilege escalation).
- **Rollback:** n/a (docs only).

### Step 1 — Agent FS core + full contract + read RPCs
- **Brief:** read `agent/volumes.go`, `agent/proxy.go:393-431`, `agent/grpc_server.go`, `agent/config.go`, `cmd/gotham-agent/main.go:152-163`, `proto/agent/v1/agent.proto`. `agent/` must not import `internal/`.
- **Tasks:** `agent/fsroot` wrapping `os.OpenRoot` (open-flags + `fstat` regular/dir only, `kind=special` listing); `validateFileRoot`; config `GOTHAM_AGENT_FILES`, `GOTHAM_AGENT_FILE_ROOTS`, limits; **entire** `FileService` proto (all RPCs above incl. write/copy/delete/chmod, header messages with `size/expect_etag/overwrite/mode/commit`); regenerate; `WithFileService` option registered only when enabled; implement `ListRoots/List/Stat/Read`, others return `Unimplemented`.
- **Verify:** common + `go test -race ./agent/...`, darwin build of the package, tests: traversal, symlink escape (file+dir), symlink swap, FIFO/socket/char device, dangerous & overlapping roots table, 10k-entry listing cap, unreadable file → `PermissionDenied`, chunk size < recv cap.
- **Exit:** a gRPC client lists/stats/reads inside allowed roots only; all escape and special-file tests fail closed; service absent when disabled.
- **Rollback:** revert PR; service unregistered.

**Accepted deviations (round 1 review):**
- Operator roots from `GOTHAM_AGENT_FILE_ROOTS` are **read-only** in step 1 (`writable=false`); write intent is deferred to a later step.
- Root validation resolves symlinks first: `validateFileRoot` judges the `EvalSymlinks` target and captures the directory's dev/ino identity from that same stat, then `os.OpenRoot` is the confinement boundary and `verifySameRoot` re-checks the opened descriptor against the captured identity — never a fresh stat of the path, which would race the very swap it must reject.
- `WriteHeader` carries `root`, so the write stream addresses `(root, path)` like the read RPCs.
- A missing built-in root (`ManagedVolumeRoot`, `ComposeRoot`, both created lazily) is skipped with a warning; a missing or unsafe operator root still fails closed.

### Step 2 — Agent write RPCs
- **Brief:** step 1 merged; reuse `fsroot`; reference `writeFileInDir`.
- **Tasks:** `Write` (size/commit check, etag, `overwrite`, exclusive create, temp+rename vs in-place by owner/nlink, per-path mutex, statfs reserve, upload limit, transfer semaphore); `Mkdir`; `Rename` (+ protect `<appID>` dirs, read-only roots refuse mutations); `Copy`/`Delete` server-streams with progress (copy-into-self guard, EXDEV fallback, symlinks copied as links, never delete a root); `Chmod` stays `Unimplemented`.
- **Verify:** common + tests: concurrent writers, exclusive create, overwrite refusal, aborted/short stream leaves target and temp dir clean, in-place mode keeps inode, read-only root refuses, appID dir protected.
- **Exit:** mutations are conflict-safe, confined, leak-free.
- **Rollback:** revert; read side unaffected.

### Step 3 — CP read side
- **Brief:** model on `internal/containers` (`routes.go:57-70`, `service.go resolve/mapRPCError`), `internal/servers/proxy_client.go` dialer, `server.go:765-776`, `databases/service.go:23`.
- **Tasks:** `internal/files` (`doc.go, errors.go, client.go, service.go, routes.go`); `DialFileClient` with explicit call options; `resolve` per RBAC rules above (team required, `CanWrite`); `roots/list/stat/content`, `download-ticket` + `download` (Content-Length, abort on mid-stream error, log redaction); `Unimplemented`→501; flag `FEATURE_FILES` default off (503).
- **Verify:** common + fake-service route tests, team-isolation tests including a team-less server and a `read_only` role, wiring test, download test asserting first chunk flushes before stream end (`io.Pipe`), ticket single-use/expiry.
- **Exit:** read API works against a real agent; foreign team = 404.
- **Rollback:** flag off or revert.

**Accepted deviations (round 1 review):**
- The RBAC decision above allows a platform admin to bypass the team requirement; the shipped
  `resolve` implements only the team-required path. A legacy team-less server (created before
  teams existed) is therefore **not browsable** — every file read answers 404 — until it is
  assigned to a team. This keeps file bodies from being shared with every authenticated caller,
  matching the plan's "team required, foreign/missing = 404" contract. The platform-admin bypass
  is deferred to step 4/step 10, which revisit content-read RBAC and audit every mutation.
- `GET /files/content` is deploy-gated for API tokens (added to `sensitiveReadSuffixes` next to
  `/credentials`): a read-scoped token cannot read file bodies that hold secrets such as 0600
  compose environment files. `roots`/`list`/`stat` stay read-scoped, and `POST /download-ticket`
  remains deploy-gated by method. The suffix match cleans the path first, so a trailing slash or
  encoded separator cannot bypass it (the query string is not part of `URL.Path`).

### Step 4 — CP write side
- **Brief:** step 2 and 3 merged; audit precedent `internal/servers/service.go:662-671`; error map from step 3.
- **Tasks:** write routes, `If-Match` ↔ `expect_etag` (428), `overwrite`; upload via `MultipartReader` streaming per part with ceiling and `SetReadDeadline` extension; body-read error cancels the stream; audit `slog` for all mutations and content/download reads; revisit content-read RBAC decision.
- **Verify:** common + RBAC table test over every write route (read_only → 403), slow-body upload test (> 30 s equivalent via fake clock/deadline), truncated upload not committed, partial multi-file failure result.
- **Exit:** every write route is team-scoped, role-gated, audited.
- **Rollback:** revert; read routes stay.

**Deviations (step 4):**

- **Upload staging on the control plane.** The agent's `Write` header must declare
  the exact body size, and a multipart part carries no size. The upload route
  therefore spools each part to a control-plane temp file (bounded by the same
  ceiling, `spoolBody`) to learn its size, then streams it to the agent; it never
  calls `ParseMultipartForm`, and only one part is in flight at a time. `PUT
  /content` streams directly because its `Content-Length` is the size (a chunked
  save is spooled the same way). A request-body error cancels the agent stream
  before the commit, so a partial upload is never renamed into place.
- **Content read stays owner/admin only.** File bodies hold secrets (0600
  compose environment files, `.env`), so `GET /content`, `PUT /content` and
  `POST /download-ticket` require a write-capable role (`Role.CanWrite`). The
  plan allowed the owner to relax content reads to all members later; the
  decision is to keep them owner/admin for now, revisited with step 10's flag
  flip.
- **Control-plane upload ceiling.** `FILES_MAX_UPLOAD_BYTES` bounds a write or
  upload body; default 1 GiB, matching the agent's
  `GOTHAM_AGENT_FILE_MAX_UPLOAD`. A body above it is refused 413 before the
  agent is dialed (`http.MaxBytesReader` catches a lying or chunked
  `Content-Length`).
- **Upload staging hygiene (fix round 1).** A streamed upload body is staged on
  the control plane before it reaches the agent (the write header must declare
  an exact size). Staging now lives in a bounded `spooler`
  (`internal/files/spool.go`): `FILES_SPOOL_DIR` selects the directory (default
  a private `gotham-spool` directory under `os.TempDir()`, created `0700`,
  files `0600`); `FILES_SPOOL_CONCURRENCY` bounds how many bodies are staged at
  once (default 4, `ErrBusy`/429 beyond it); the spool volume's free space is
  checked against the ceiling before any write (`ErrInsufficientStorage`/507
  when it cannot hold the body); a startup sweep and an opportunistic hourly
  sweep remove `gotham-upload-*` files older than 24 h; and every error/cancel
  path closes and removes the staging file and releases the slot.
- **ResourceExhausted is classified, not blanket 429 (fix round 1).** An agent
  `ResourceExhausted` was mapped to 429 "node is busy" for every case. It is now
  split by the message prefixes the agent already defines
  (`agent/files_write.go`): a body over the node's limit → `ErrTooLarge` (413),
  low free space or an exhausted quota → `ErrInsufficientStorage` (507), and the
  per-node transfer semaphore → `ErrBusy` (429).
- **Write stream rejects map the real status (fix round 1).** When the agent
  rejects a write stream at header time (read-only root/app dir, over-limit,
  low disk, invalid header) the client stream's `Send` returns only `io.EOF`;
  the status is recovered from `CloseAndRecv` and mapped, instead of surfacing a
  raw EOF as a 500.
- **Upload body is bounded end to end (fix round 1).** `POST /files/upload`
  bounds the **whole** request body with `http.MaxBytesReader` (the upload
  ceiling), not only each part, and a staging failure on one part no longer
  drops the parts that follow: each part is reported in the result array.
- **Chunked PUT is authorized before staging (fix round 1).** A chunked
  `PUT /files/content` runs the feature-enabled and team/RBAC checks before it
  spools the body, so a disabled feature answers 503 and a foreign team 404
  without the body being read.
- **Move is an alias of rename.** `/files/move` and `/files/rename` both call
  the single agent `Rename` RPC; both are single-root (cross-root → 400). The
  audit `op` is `rename` for either route, with the destination logged as an
  extra field.
- **Write conflict status.** An agent `AlreadyExists`/`FailedPrecondition`/
  `Aborted` maps to 409; a missing `If-Match` (and no `?overwrite=true`) is 428.
  The plan's 412 alternative was dropped to keep one conflict status (409) in
  the error contract.

### Step 5 — Mockup
- **Brief:** `docs/design/README.md` (modal rules), `assets/gotham-ui.css`, `server-detail.html`.
- **Tasks:** `docs/design/files.html`: toolbar, breadcrumb + root switcher, list with empty/loading/error/permission-denied rows, selection bar, context menu, editor pane (tabs, dirty dot, status bar, binary/too-large state), conflict modal (`.app-modal`), delete confirm, upload panel, read-only root banner; link from `docs/design/index.html`; English copy.
- **Verify/Exit:** project owner approves before step 6.
- **Rollback:** revert docs.

### Step 6 — Web foundation + read-only browser
- **Brief:** `web/src/features/updates`, `ServerDetailPage.vue:172-218`, `navigation.ts:32` + `web/tests/navigation.test.ts`, `shared/i18n/index.ts` + `web/tests/i18n-catalog.test.ts`, `shared/validation`, `shared/api/http.ts`.
- **Tasks:** `features/files/{api,schemas,stores,composables,components,pages,locales/en.ts,vi.ts,index.ts}`; routes `files` (chooser) + `server-files`; `titles.files`; activate nav item (highlight on both routes); server-detail "Files" tab; list/breadcrumb/root switcher/sort/multi-select; loading only when no data, error+retry, empty, permission-denied, read-only-root states; HTTP status → i18n key mapping; zod-parse responses.
- **Verify:** common web + vitest (store selection/sort/navigation, status→i18n), catalog parity, navigation test updated.
- **Exit:** matches mockup; shortcuts scoped to the list.
- **Rollback:** revert PR + rebuild webdist.

### Step 7 — Web file operations
- **Brief:** steps 4 and 6 merged; `shared/api/http.ts` timeout/interceptors.
- **Tasks:** create/rename/delete/move-copy dialogs; clipboard with 409 replace/skip prompt; upload (button + drag-drop, per-file progress, cancel, retry failed, per-request timeout override); native download via ticket; buttons disabled + spinner during mutations; partial-failure summary; name rules (trim, reject empty, `.`, `..`, `/`).
- **Verify:** common web + vitest (clipboard, transfer composables) + regression tests for create-over-existing and name rules.
- **Exit:** every operation works against a real node; every failure shows feedback.
- **Rollback:** revert PR + rebuild webdist.

### Step 8 — Web code editor
- **Brief:** steps 4, 6, 7 merged; `docs/library-audit.md` format; CSP note.
- **Tasks:** CodeMirror 6 dynamic import with explicit languages; tabs in the Files page; dirty dot; Ctrl/Cmd+S save with `If-Match`; 409 → conflict modal (reload / overwrite / save as copy); 413/415 → read-only/download state; dirty cleared only if doc unchanged during save; tab sync on rename/move/delete (retarget/close, block on dirty); `beforeunload` guard; CRLF/BOM preserved; wrap toggle, search, go-to-line; library-audit `F22`; rebuild `webdist`, report size delta and chunk count.
- **Verify:** common web + vitest (tab store dirty/sync/race, CRLF round-trip) + Playwright layout spec for conflict modal.
- **Exit:** editor keys never reach the list; entry chunk size unchanged; new chunk files ≤ 15.
- **Rollback:** revert PR + rebuild webdist.

### Step 9a — chmod (phase 2) — **BUILT (JUS-113)**
- **Status:** implemented. The agent's `FileService.Chmod` sets only the low 0o777
  permission bits; the CP exposes `POST /api/v1/servers/{id}/files/chmod`; the web
  browser has a Permissions dialog reached from the row action menu.
- **Agent behaviour (documented decisions):**
  - `mode` is **required**: the proto field is `optional uint32` and an absent
    value is refused with `InvalidArgument` (CP 400), never treated as 000. The
    CP's request body likewise uses a `*uint32` so `{"root","path"}` is 400.
    `mode: 0` (000) is still a real, accepted mode; only its *absence* is refused.
  - Only the `0o777` bits may change. Any `mode` carrying setuid (`0o4000`), setgid
    (`0o2000`), sticky (`0o1000`) or any higher bit is refused with
    `InvalidArgument` (CP 400) before the filesystem is touched.
  - Existing setuid/setgid/sticky bits are **preserved, not editable**: the agent
    `fstat`s first and writes `(st_mode & 0o7000) | requested`. A special bit is
    never created (the mask above still refuses one in the request), so the rule
    "only plain permission bits change" holds — the special bits are unchanged.
  - A symbolic link is **refused** with `InvalidArgument` (CP **400**; it is never
    followed and its target's mode is untouched). A special file (FIFO, socket,
    device) is refused with `FailedPrecondition` (CP **409**). The root itself is
    refused.
  - A directory must keep its owner execute bit (`0o100`), or the chmod is refused
    with `InvalidArgument`: removing it would make the entry un-traversable and
    self-lock the file manager. A managed application directory is covered by the
    same rule (it may be chmod-ed as long as it keeps owner execute and stays a
    directory).
  - A hard-linked regular file is refused with `InvalidArgument` in a **confined**
    root, because its inode may also be reachable outside the root. The
    whole-filesystem root has no "outside", so the check is moot there.
  - **Recovery:** the confinement (`fsroot.ChmodEntry`) opens the root and every
    parent with `O_PATH` (search permission only), `openat`s the basename with
    `O_NOFOLLOW`, `fstat`s the descriptor to classify the type, chmods that same
    inode, and reads the returned entry back from the same descriptor. So a
    `000`/`0o111` entry the agent itself just made unreadable — including a
    directory, and including one under an unreadable parent — is still
    chmod-able by a **non-root** agent. The chmod itself is
    `fchmodat2(AT_EMPTY_PATH)` where available (Linux 6.6+) and the
    `/proc/self/fd` magic link otherwise; neither needs read permission on the
    entry, and the descriptor pins the inode so a swapped-in link is never
    followed. `O_NOCTTY` is set on any open that could touch a device.
  - Read-only roots refuse with `PermissionDenied`, matching every other mutation.
  - Recursion is **not** implemented: `ChmodRequest` has no `recursive` field and a
    single `chmod(2)` is atomic, so there is no entry-cap/symlink-walk surface to
    guard. Adding recursion would need a new proto field and a bounded, no-follow
    walk; it is out of scope.
- **Wire:** `ChmodResponse` is empty (the step-1 contract). The CP therefore
  re-`Stat`s the entry after a successful `Chmod` and returns the fresh
  entry/etag, exactly as `Copy` already does for its destination — no proto change
  beyond making `mode` optional.
- **CP:** `POST /files/chmod` (body `{root, path, mode}`, 200 `{entry}`) is
  owner/admin, deploy-gated for an API token by the existing non-GET method gate,
  audit-logged, and maps an old agent's `Unimplemented` to 501. A body that omits
  `mode` is a 400.
- **Web:** a Permissions dialog (octal input + a 3×3 rwx grid kept in sync) from
  the row action menu, disabled for a read-only root or role, with a note that
  special bits are preserved (never editable); the grid is disabled while the
  octal field is invalid (so it never overwrites what was typed), Apply is
  disabled when the mode is unchanged or a directory would lose owner execute,
  and the listing refreshes after success. The `docs/design/files.html` mockup
  gained the menu entry and the modal before the Vue port.
- **Verify:** `go build ./...`, `go test ./...`, `golangci-lint run`, `go vet`,
  `npm run build`, `npm run type-check`, `npm test` all green; mode-bit rejection
  (setuid/setgid/sticky/>0o777), missing-mode rejection, symlink-not-followed,
  special-file, directory-owner-execute, hard-link, special-bit-preservation,
  read-only root, app-dir-stays-directory, whole-filesystem and a non-root
  `000`/`0o111` recovery (file and directory, and under an unsearchable parent)
  tests added. **Exit:** only plain permission bits change. **Rollback:** revert
  the PR.

### Step 9b — Archives (phase 2) — **BUILT (JUS-114)**
- **Status:** built. The agent `Compress`/`Extract` RPCs, the CP `/files/compress` + `/files/extract`
  routes and the web "Compress…"/"Extract here…" dialogs ship; only step 9a chmod (JUS-113) stays
  deferred.
- **Tasks:** agent `Compress`/`Extract` via `archive/zip`, `archive/tar`, `compress/gzip`, no shell;
  zip-slip, symlink/hard-link/device-entry, setuid-strip, entry-count, size and ratio guards;
  progress stream; CP routes; web dialogs. Done: all of the above.
- **Verify:** common + zip-slip/zip-bomb/symlink-entry/round-trip tests. **Exit:** archives never
  write outside the target root. **Rollback:** revert.

**Step 9b decisions:**

- **Entry names are relative to each source's parent.** Compressing `app/uploads/file.txt` stores
  `file.txt`, and a directory source stores its base name (`data/…`), so "Extract here" into the
  same directory does not nest a full path. The agent skips symbolic links and special files and
  reports the count in the final `Progress.skipped`.
- **The compression-ratio cap is fixed at 200:1** (not env-configurable): the archive's own size
  times 200 bounds the total uncompressed bytes, on top of `GOTHAM_AGENT_ARCHIVE_MAX_BYTES`
  (default 4 GiB) and `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` (default 100000). An archive-limit
  `ResourceExhausted` maps to **413** on the CP.
- **Extract refuses rather than skips** a symlink/hard-link/device/FIFO entry (a whole-archive
  failure); compress skips them. Setuid/setgid/sticky bits are stripped to `mode.Perm()`.
- **The running-app-directory guard applies to extraction.** An entry that would replace a
  top-level application directory of the managed volumes root is refused, so an archive can never
  clobber a container bind mount.
- **The CP drains the agent progress stream** (like Copy/Delete) and returns the created archive's
  entry (compress) or **204** (extract); the browser shows an indeterminate progress line, since a
  drained stream carries no byte counters to the client.
- **The skipped-entry count is reported by the agent but not surfaced to the browser yet** (the CP
  returns the archive entry, not the stream's terminal `skipped`); documented as a follow-up.

### Step 10 — Hardening, e2e, docs
- **Brief:** `.github/workflows/ui-e2e.yml`, `internal/e2e`, `docs/install.md`, `docs/TODO.md`.
- **Tasks:** `internal/e2e` scenario (upload → edit → conflict → download → delete) driven by `GOTHAM_E2E=1 go test ./internal/e2e -run Files` — it runs in CI through `.github/workflows/e2e.yml` (which sets `GOTHAM_E2E=1` and needs the runner's Postgres/Redis/Docker) and locally the same way; `web/e2e/files.spec.ts` using `page.route` fixtures (CI has no enrolled agent), run by `npm run e2e` in `.github/workflows/ui-e2e.yml`; GitNexus MCP `explain` taint review on `internal/files` and `agent/files*.go`; docs (`README.md` row, `00-roadmap.md`, `TODO.md`, `install.md` env + permissions); flip `FEATURE_FILES` default to on only with owner sign-off.
- **Exit:** CI green (Go e2e + Playwright), e2e recorded, review gate signed off by the project owner. **Rollback:** flag off.

#### Step 10 results (JUS-115)

**Flag defaults unchanged.** `FEATURE_FILES` (CP) and `GOTHAM_AGENT_FILES` (agent) stay **off**: the owner owns the enablement sign-off, so step 10 ships the hardening, coverage and docs without flipping either default.

**API e2e (`internal/e2e`, runs in CI).** `GOTHAM_E2E=1 go test ./internal/e2e -run Files -v` —
**PASS**, run 2026-10-13 on the dev machine (Docker 29.8.1, go1.27); the same suite runs in CI
via `.github/workflows/e2e.yml` on every change under `internal/e2e/**` (and the other gated
paths), so it is not manual-only. `TestFilesEndToEnd`
drives the real HTTP routes → `internal/files.Service` → the mTLS `FileService` dial → a live
agent whose two confined roots are temporary directories: `roots` (volumes writable, compose
read-only), 428 without `If-Match`, multipart upload, content + strong etag, `If-Match` save
(200), stale etag (409, file intact), read-only-root write (403), single-use download ticket
(200 then 404), delete (204) then 404 and gone from disk. `TestFilesEndToEndDisabled` pins 503
with `FEATURE_FILES=false`. Without `GOTHAM_E2E` both **skip cleanly** (verified).

**Browser e2e (`web/e2e/files.spec.ts`).** Playwright with `page.route` fixtures (CI has no
enrolled agent), run by `npm run e2e` in `.github/workflows/ui-e2e.yml`: browse, upload
(per-file result), edit + save (asserts the `If-Match` sent),
409 conflict modal + overwrite (`?overwrite=true`), delete, 503 disabled state, and the
`read_only` role. Run: **7 passed**; stable over `--repeat-each=3` (**21 passed**, 0 flaky).

**Live test-box run.** Deployed `origin/main` (`4549a352`) on the all-in-one box, built with
`make build` (Go only; the committed `webdist` is used because the box has no Node), enabled
`FEATURE_FILES` on the CP and `GOTHAM_AGENT_FILES` on the agent through **temporary** systemd
drop-ins, restarted both units, and drove the demo account through the CP API with `curl`
(password read from the box file in place — never printed, never recorded). Redacted evidence:

- `GET /roots` → `volumes` `writable:true`, `compose` `writable:false`.
- upload `e2e/hello.txt` → per-file entry; `GET /content` → `hello\n` + etag; `PUT` with the
  matching `If-Match` → 200; `PUT` with the **stale** etag → **409**; `POST /download-ticket`
  → 201, `GET /download` → the exact bytes, a second use → **404**; `DELETE` → **204**, then
  `stat` → **404**.
- **Root-owned container volume** (`root-owned.txt`, `root:root 0644`): read OK, edit **403**;
  creating a file at the `volumes` **top level** **403** — the read-mostly behaviour the step-0
  spike predicted. The `compose` root refuses a write with 403.
- **Slow uploads:** 512 KiB at 10 KiB/s (**51 s**) and 256 KiB at 3 KiB/s (**85 s**) both
  committed. This proves the control plane's 30 s `ReadTimeout` is extended per request by the
  file handlers, and shows the direct CP path has **no 60 s edge cut**.
- Cleanup: both drop-ins removed, units restarted, `FEATURE_FILES`/`GOTHAM_AGENT_FILES` absent
  again; the temporary server row and the test directories were removed.

**Security review.** The GitNexus MCP `explain` tool (its PDG layer populated by
`gitnexus analyze --pdg`) over `internal/files`, `agent/files.go`, `agent/files_write.go` and
`agent/fsroot` reports **zero `TAINTED`/`TAINT_PATH` findings**, repo-wide as well as for the file
paths. Manual review corroborates it; **no real finding needed a code fix**:

| Check | Result | Triage |
|---|---|---|
| GitNexus MCP `explain` taint on `internal/files`, `agent/files*.go`, `agent/fsroot` | 0 findings (PDG layer present, 0 repo-wide) | No modeled source→sink flow; corroborated by the manual checks below. |
| `os/exec` / `sh -c` in the file paths | none | Real: no shell anywhere in the read/write path. Archives (9b) use Go `archive/zip|tar` + `compress/gzip`, not a shell. |
| `filepath.Join` with user input outside `fsroot` | none | Real: the agent resolves every path through `os.OpenRoot` (`agent/fsroot`), never a string join. The only CP `Join` is the spool dir + `os.ReadDir` entry names (fixed prefix, regular files only). |
| Unbounded reads/allocations | bounded | `readAllLimited` caps content at 2 MiB; agent chunks 256 KiB; `List` caps 5,000/page and 100,000 scanned; the spool caps a body at `FILES_MAX_UPLOAD_BYTES`. |
| Goroutine leaks on cancel | none found | The download pump selects `ctx.Done()` and defers `cancel`+close; the write recv goroutine unblocks on handler return (stream close) via its `done` channel; the temp sweeper stops on `Close`. |
| Secrets in logs | none | Audit records `op/result/actor/server/root/relpath` only — never a body or a ticket. The download ticket rides the query string and the access logger records `r.URL.Path` only. |

#### Known residuals (step 10 follow-ups)

- **`agent/files.go` root-stat symlink swap (follow-up).** `validateFileRoot` resolves the root
  path with `filepath.EvalSymlinks` and then re-`os.Stat`s the resolved path; a component swapped
  between the two calls makes `os.Stat` follow the new target. `fsroot.Open`'s `verifySameRoot`
  re-checks the *opened descriptor* against the captured identity, so the confinement boundary
  holds, but the identity captured for comparison can itself be the swapped one. Fix: capture the
  identity fd-first (`O_PATH`/`openat` the resolved path once and `fstat` it, or `Lstat` the
  final component) so the validated identity is the one that is opened.
- **Global spool semaphore, not per-team.** `FILES_SPOOL_CONCURRENCY` bounds *all* uploads staged
  on a control plane (`internal/files/spool.go`), so one noisy team can occupy every slot and
  starve others with 429s. A per-team cap (or fair queueing) is the upgrade.
- **Hard-link bypass note (LOW).** A writable root can hold a hard link whose other name is
  outside the root; the in-place rewrite path writes the shared inode, so an edit through the
  linked name also changes the outside file. Creating such a link needs the agent uid to already
  be able to write the target, and roots are operator-controlled, so the exposure is small;
  `nlink>1` is already why the writer keeps the inode instead of renaming. Revisit if roots ever
  accept container-created links to sensitive inodes.
- **Download ticket travels in the URL query.** `GET /files/download?ticket=` carries the
  single-use, short-TTL ticket in the query string, so it may appear in any proxy or access log
  along the path; the control plane's own access log records `r.URL.Path` only, never the query.
- **Container-volume writability (owner decision).** Unchanged from step 0: container-created
  volumes are `root:root`, so the unprivileged agent reads world-readable files and is denied
  writes. To let operators upload/edit inside a container volume, provisioning must make
  `<root>/<appID>` agent-writable by **either** the CP chowning it to `gotham-agent` **or** a
  shared group plus a setgid (`2775`) app dir (the agent must also join the group). Owner decision
  before the flag is flipped on for such nodes; the UI already shows per-entry permission-denied.

## Deviations

Deliberate, documented limits of the step-2 write path (JUS-106):

- **In-place rewrite is not atomic.** A file owned by another uid, hard-linked, or bind-mounted as a single file must keep its inode, owner, ACLs and mount, so its bytes are rewritten in place instead of renamed over a fresh inode. The agent tightens the window: it statfs-es the *opened* descriptor (the target's real filesystem, which may differ from the root's), refuses when that filesystem's free bytes cannot hold the body (`free==0`, a failed statfs, means refuse unless the body is empty), and `fallocate`s the new length before writing when the file grows, so an out-of-space failure leaves the old contents intact. A crash, `EIO` or kill between the reservation and the final `fsync` can still leave the file half-written; that residue is the accepted cost of preserving the inode and is not recovered automatically.
- **A copy is staged, then renamed.** `Copy` writes into a temporary sibling of the destination and renames it into place, so a failed or cancelled copy leaves the destination untouched (no partial tree, no staging leak). Overwriting a non-empty directory therefore behaves like a rename — refused (`ENOTEMPTY` on Linux, `EEXIST` on macOS) rather than merging two trees.
- **The cross-device (`EXDEV`) move fallback reuses the staged copy**: it never removes the destination before copying, so a failure leaves both the source and the destination intact.
- **The staging-file sweeper is depth-bounded.** It walks each writable root hourly and removes `.gotham-tmp-*` entries older than 24 h, but stops descending below `maxSweepDepth` (128) so the walk stays cheap on a large volume; a staging file buried deeper than the cap is left for a manual sweep.
- **Cross-root move/copy remain out of scope** (single-root only, as designed).

## Risks
| Risk | Mitigation |
|---|---|
| Container-created files unreadable by `gotham-agent` | Step 0 spike; read-mostly UX; unit-file change as separate ticket |
| Agent listener unauthenticated when CA unset | Agent opt-in env + confinement; documented |
| Device/FIFO/hardlink/inode edge cases | Special-file rejection, in-place write mode |
| gRPC limits and 30 s HTTP timeout | 256 KiB chunks, call options, response-controller deadlines |
| Committed `webdist` growth | Lazy chunk, explicit languages, size delta per PR |
| Steps 7/8 collide on the page | Serial; shared store in step 6 |
| Agents lag behind CP | `ErrAgentOutdated` 501 |
| Scope creep to containers/terminal | Out of scope |

## Out of scope
Container filesystem browsing (Docker `archive`/`exec`), host-wide filesystem access, `chown`, trash/restore, file search, diff view, collaborative editing, terminal, durable audit table, resumable/chunked browser uploads, cross-root move/copy.

## Tickets
Parent JUS-103 (Feature, project gotham). Children, one per step:

| Step | Ticket |
|---|---|
| 0 Permission spike (gate) | JUS-104 |
| 1 Agent FS core + contract + read RPCs | JUS-105 |
| 2 Agent write RPCs | JUS-106 |
| 3 CP read side | JUS-107 |
| 4 CP write side | JUS-108 |
| 5 UI mockup | JUS-109 |
| 6 Web foundation + browser | JUS-110 |
| 7 Web file operations | JUS-111 |
| 8 Web code editor | JUS-112 |
| 9a chmod (optional) | JUS-113 |
| 9b Archives (optional) | JUS-114 |
| 10 Hardening, e2e, docs | JUS-115 |
