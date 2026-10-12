# Phase 18 — Remote file manager & code editor

**Goal:** operators browse, transfer and edit files on a managed server from the UI: a file manager (list, create, rename, move, copy, delete, upload, download) plus a tabbed code editor with safe saves, scoped to confined roots on the node. Ported in spirit from the `pyramid` project (`api/files`, `app/components/files`, `app/components/codeEditor`), re-designed for Gotham's control-plane → agent architecture. Linear: JUS-103.

## Current state (verified)
- No file/exec/archive RPC exists. `proto/agent/v1/agent.proto` (single proto file; `make proto`, `make proto-check`) has `AgentService`, `UpdateService`, `DockerService`, `BuildService`, `ProxyService`, `ComposeService`. Streaming templates: `DockerService.StreamLogs` (server-stream), `BuildService.BuildImage` (bidi).
- Agent: runs as unprivileged `gotham-agent` (`deploy/gotham-agent.service`: `ProtectSystem=full`, `ProtectHome=true`, `SupplementaryGroups=docker`). gRPC server caps receives at 2 MiB, 64 concurrent streams (`agent/grpc_server.go:29-30,336`); the CP client sets no `MaxCallRecvMsgSize` (4 MiB default). The agent listener accepts any peer when `GOTHAM_AGENT_CA` is unset (`agent/tls.go:84-90`, TODO LOW-4), so **confinement and an opt-in switch must live on the agent**; confinement is not authentication.
- Roots: `ManagedVolumeRoot` (default `/var/lib/gotham/volumes`) and `ComposeRoot` (default `/var/lib/gotham-agent/compose`, inside the agent's own state dir) in `agent/config.go`.
- Precedents: `agent/volumes.go` (`validateAppVolume`, `rejectSymlinkComponents`, exact-match `isDangerousHostPath` at `:223`), `agent/proxy.go:393,431` (`writeFileInDir`, `openTrustedDir`). `RemoveVolume` re-validates on the agent.
- CP template module: `internal/containers` (`errors.go`, `client.go`, `service.go`, `routes.go`), wired in `internal/server/server.go:765-776` with `s.withTeam()`. `AuthorizeOptionalTeam` (`internal/teams/scope.go:67`) returns nil for servers **without** a team, even for writes — the files module must not inherit that.
- HTTP server `ReadTimeout` is 30 s for the whole request (`internal/server/server.go:56`).
- Web: HTTP client is `web/src/shared/api/http.ts` (Bearer header + `X-Team-Id`, 15 s default timeout). No editor library, no file components. `web/src/app/layouts/navigation.ts:32` has an inactive `files` nav stub. `internal/server/webdist` (2.4 MB) is committed and drift-checked.
- No files mockup in `docs/design/`.

## Spike results (step 0 gate)

**Status: BLOCKED — the test box's root filesystem is read-only after a disk
failure, so the live read/write test could not be run.** Checked 2026-10-12 on
the all-in-one box from `docs/test-server.md` (Ubuntu 22.04).

### Facts recorded

- **Kernel:** `Linux linux4857 5.15.0-60-generic #66-Ubuntu SMP Fri Jan 20
  14:29:49 UTC 2023 x86_64` (`uname -r` = `5.15.0-60-generic`). `openat2` is
  available (Linux ≥ 5.6), so `os.OpenRoot`'s fast path applies; the stdlib
  fallback still covers older kernels.
- **Agent identity:** `uid=998(gotham-agent) gid=998(gotham-agent)
  groups=998(gotham-agent),999(docker)` — the supplementary `docker` group the
  unit file sets.
- **Roots (both defaulted; no override on either side):**
  - `ManagedVolumeRoot` = `/var/lib/gotham/volumes` (CP
    `internal/deploy/volumes.go`, agent `agent/config.go`; both env vars unset
    on the box). Parent `/var/lib/gotham` is `root:root 0755`. The root itself
    does **not** exist yet (no storage had been deployed).
  - `ComposeRoot` = `/var/lib/gotham-agent/compose`
    (`GOTHAM_AGENT_COMPOSE_ROOT` unset). Parent `/var/lib/gotham-agent` is
    `gotham-agent:gotham-agent 0755`; the root itself does **not** exist yet.
- **Docker root** `/var/lib/docker` (overlayfs), on the same filesystem.
- **Traefik (v3.7, container `gotham-traefik`):** static config
  `/var/lib/gotham-agent/traefik/traefik.yml` (agent-owned 0644; written by the
  agent, not the file manager) declares entryPoints `traefik:8080`, `web:80`,
  `websecure:443` and **no `transport.respondingTimeouts`**, so the Traefik v3
  defaults apply on `web`/`websecure`: **`readTimeout` 60 s, `writeTimeout` 0
  (no limit), `idleTimeout` 180 s**. The CP's own `ReadTimeout` is 30 s
  (`internal/server/server.go:56`); at the edge Traefik caps reading a request
  **including its body** at 60 s and the CP's response-controller deadlines
  cannot extend it — a > 1 GiB upload that takes longer than 60 s at the edge
  would be cut off. Set an explicit `respondingTimeouts.readTimeout` on
  `web`/`websecure` (or document the 60 s ceiling) in step 4/10.
- **No application storage exists on the box:** `/var/lib/gotham/volumes` is
  absent and no running container has a bind under it (the Phase 13 migrations
  truncated resources), so there was no live container-created volume to inspect
  either.

### Not verified (blocked) — and why

Live read/write/list as `gotham-agent` under both roots, plus the
container-created-volume ownership outcome. The box's root filesystem is
**read-only after a disk failure**:

- `/proc/mounts`: `/dev/mapper/vg0-lv--0 / ext4 ro,relatime 0 0`.
- `dmesg`: `blk_update_request: I/O error, dev sda`, `sd 32:0:0:0: [sda]
  timing out command, waited 1080s`, `FAILED Result: hostbyte=DID_RESET`,
  `EXT4-fs error ... Journal has aborted`, then `EXT4-fs (dm-0): Remounting
  filesystem read-only` at uptime `1291781` s (~14 h before the check).
- Even `root` cannot write (`touch → Read-only file system`); `mount -o
  remount,rw /` → `is write-protected` (rc 32).
- Docker cannot run or exec containers (`docker run → read-only file system`),
  so a container-created volume cannot be produced.
- Knock-on effects: Postgres is `inactive`, CP `/healthz` returns 503, and
  `gotham-agent` loops on `register failed ... internal error`. The agent's own
  state dir is on the same filesystem, so it cannot create the ComposeRoot
  either.

### To run once the box is repaired

Repair or replace the failing disk, reboot (fsck), and confirm `/proc/mounts`
shows `/ rw` and `systemctl is-active postgresql gotham gotham-agent` are all
`active`. Then, from a machine with the `gotham` SSH alias:

```sh
ROOT=/var/lib/gotham/volumes
COMPOSE=/var/lib/gotham-agent/compose
APPID=$(uuidgen)

# 1. Who creates the root and the app dir, and with what ownership:
docker run --rm -v "$ROOT/$APPID/data:/data" alpine:3 sh -c \
  'echo hi > /data/from-container.txt; ls -la /data'  # dockerd creates the source as root:root
ls -ld "$ROOT" "$ROOT/$APPID" "$ROOT/$APPID/data"

# 2. As the agent: list, read 0644, read 0600, write, mkdir
sudo -u gotham-agent ls    "$ROOT/$APPID/data"
sudo -u gotham-agent cat   "$ROOT/$APPID/data/from-container.txt"  # expect ok
sudo -u gotham-agent sh -c "echo x > '$ROOT/$APPID/data/newfile'"  # expect denied
sudo -u gotham-agent mkdir "$ROOT/$APPID/newdir"                   # expect denied

# 3. ComposeRoot (agent-owned parent):
sudo -u gotham-agent mkdir -p "$COMPOSE/probe" && echo writable
```

Record the ownership of `$ROOT/$APPID/data` and each op's exit code here.

### Decision

- **`deploy/gotham-agent.service`: no change decided** — the measurement is
  blocked. The expected result on re-run (to confirm, not assumed): a unit-file
  change cannot let a uid-998 process read files another uid owns with mode
  0600, nor write a root-owned 0755 directory; only root or userns remapping
  could, which the plan already excludes. The read-mostly UX and per-entry
  "permission denied" states stay the mitigation.
- **Gate:** step 0 is **not passed**. Step 1 must not start until the box is
  repaired and this section is completed with live results.

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
- **Writes:** first `Write` message carries `path, size, expect_etag, overwrite, mode`. The agent commits only if received bytes == `size` (or a final `commit` message with sha256); on any CP-side body error the CP cancels the stream context, never closes it cleanly, so a truncated upload is never renamed in. Strategy: temp file + fsync + rename when the file is owned by the agent uid and `nlink==1`; otherwise **write in place** under a per-`(root,path)` mutex (check etag → `O_TRUNC` → write → fsync) so bind-mounted single files, hardlinks, ACLs and ownership survive. Responses report which mode was used.
- **Etag:** `dev:ino:mtime_ns:size` (RFC 7232 quoted); `/content` also returns a sha256 of the (≤ 2 MiB) body. Per-path mutex on the agent around check+commit with a re-`fstat` before rename. Writes by containers can still race (documented).
- **Limits (configurable on the agent):** `GOTHAM_AGENT_FILE_MAX_UPLOAD` default 1 GiB, `statfs` check keeping 5 % free, max 8 concurrent transfers per node (`ResourceExhausted` → 429).
- **RBAC:** the files `resolve` requires the server to have a team (or a platform admin) and always enforces `Role.CanWrite()` for writes; foreign team/missing node = 404. `list/stat/roots` = any member; **`content` and `download` = owner/admin** (file bodies hold secrets such as 0600 compose env). Owner may relax this to all members; revisit in step 4. Reads of content/download are audit-logged too.
- **Downloads:** `POST /download-ticket` → single-use ticket (60 s, bound to user, team, server, root, path); `GET /download?ticket=` consumes it so the browser can use a native streaming download. `ticket` is redacted from access logs. Sets `Content-Length`, `Content-Disposition: attachment`, `nosniff`; on a mid-stream agent error the handler aborts the connection (`http.ErrAbortHandler`) so the browser marks the download failed. Long transfers use `http.NewResponseController(w).SetReadDeadline/SetWriteDeadline`; check Traefik `readTimeout` (v3 default 60 s) and document or set it.
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
9a. **chmod** (phase 2, optional). Dep 2, 7.
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

### Step 4 — CP write side
- **Brief:** step 2 and 3 merged; audit precedent `internal/servers/service.go:662-671`; error map from step 3.
- **Tasks:** write routes, `If-Match` ↔ `expect_etag` (428), `overwrite`; upload via `MultipartReader` streaming per part with ceiling and `SetReadDeadline` extension; body-read error cancels the stream; audit `slog` for all mutations and content/download reads; revisit content-read RBAC decision.
- **Verify:** common + RBAC table test over every write route (read_only → 403), slow-body upload test (> 30 s equivalent via fake clock/deadline), truncated upload not committed, partial multi-file failure result.
- **Exit:** every write route is team-scoped, role-gated, audited.
- **Rollback:** revert; read routes stay.

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

### Step 9a — chmod (phase 2)
- **Tasks:** agent `Chmod` (reject setuid/setgid/sticky, no recursion following symlinks), CP route, properties dialog (octal + rwx grid).
- **Verify:** common + mode-bit rejection tests. **Exit:** only plain permission bits change. **Rollback:** revert.

### Step 9b — Archives (phase 2)
- **Tasks:** agent `Compress`/`Extract` via `archive/zip|tar`, `compress/gzip`, no shell; zip-slip, symlink-entry, size and entry-count guards; progress stream; CP routes; web dialogs.
- **Verify:** common + zip-slip/zip-bomb/symlink-entry tests. **Exit:** archives never write outside the target root. **Rollback:** revert.

### Step 10 — Hardening, e2e, docs
- **Brief:** `.github/workflows/ui-e2e.yml`, `internal/e2e`, `docs/install.md`, `docs/TODO.md`.
- **Tasks:** `internal/e2e` scenario (upload → edit → conflict → download → delete) run manually with `GOTHAM_E2E=1 go test ./internal/e2e -run Files`; `web/e2e/files.spec.ts` using `page.route` fixtures (CI has no enrolled agent); `gitnexus explain` taint review on `internal/files` and `agent/files*.go`; docs (`README.md` row, `00-roadmap.md`, `TODO.md`, `install.md` env + permissions); flip `FEATURE_FILES` default to on only with owner sign-off.
- **Exit:** CI green, manual e2e recorded, review gate signed off by the project owner. **Rollback:** flag off.

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
