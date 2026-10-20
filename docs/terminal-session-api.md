# Terminal session API and lifecycle (JUS-243, container exec JUS-244)

This document is the contract dependent work packages (JUS-245 shared terminal
view, JUS-247 application terminal tab) build on. It describes the browser →
control plane → node agent path for a host shell and for an interactive
application-container exec, their permissions, limits and cleanup guarantees.

## Topology

```
host:
browser ──HTTP POST /api/v1/servers/{id}/terminal/sessions──▶ control plane
browser ──WebSocket /api/v1/.../ws─────────────────────────▶ control plane
control plane ──gRPC TerminalService.OpenTerminal (mTLS)────▶ node agent ──PTY──▶ shell

application container:
browser ──HTTP GET  /api/v1/applications/{id}/terminal/targets──▶ control plane
browser ──HTTP POST /api/v1/applications/{id}/terminal/sessions─▶ control plane
browser ──WebSocket /api/v1/.../ws─────────────────────────────▶ control plane
control plane ──gRPC TerminalService.OpenTerminal (mTLS)───────▶ node agent
node agent ──Docker Engine HTTP /exec (hijacked)────────────────▶ container shell
```

Both targets share one transport, one WebSocket frame contract, one lifecycle
and one cleanup path. The control plane selects the target; the browser chooses
a container only from the server-resolved target list, and the control plane
re-validates that selection before a session is created.

Terminal input and output travel only on that path. They are **never** published
to Redis pub/sub, the shared WebSocket log hub, or any log line. The control
plane's session registry is in-memory; a control-plane restart ends every
session (the agent tears the PTY or exec attachment down when the stream
disappears).

## 1. Create a session (HTTP)

`POST /api/v1/servers/{server_id}/terminal/sessions`

Requires an authenticated, platform-operator caller (`RequireAuth` +
`RequirePlatformAdmin`). The route is mounted with the server registry; without
one it is absent.

```json
{
  "target": "host",
  "cols": 120,
  "rows": 32
}
```

The Terminal page and the Server detail Terminal tab have no File manager
context and send exactly that: no `root` and no `cwd`. The File manager entry
adds both:

```json
{
  "target": "host",
  "root": "volumes",
  "cwd": "my-app/data",
  "cols": 120,
  "rows": 32
}
```

| Field | Rules |
|---|---|
| `target` | `host` or omitted on this route. `container` answers 501 here: container sessions have their own application routes and their own permission. Any other value answers 501. An old agent never silently falls back to the host. |
| `root` | Optional. A FileService root name (`^[a-z0-9][a-z0-9_-]{0,127}$`). The agent resolves it against its own allow-list. When it is omitted the agent starts the shell in its own default directory (section 4); the browser never chooses that path. |
| `cwd` | Optional, and only valid together with `root`. Relative to `root`, at most 4096 bytes, no NUL, no absolute path, no `..` segment. Empty/`.` means the root itself. |
| `cols`, `rows` | 1–1000. |

`cwd` without `root` answers `400` before any shell starts. An omitted `root`
and `cwd` is the no-File-manager case: the control plane accepts it and the
agent decides the start directory.

Response `201` (the example includes a File manager root; when `root`/`cwd` are
omitted the response omits those two fields):
```json
{
  "id": "…",
  "server_id": "…",
  "target": "host",
  "root": "volumes",
  "cwd": "my-app/data",
  "cols": 120,
  "rows": 32,
  "expires_at": "2026-10-20T12:01:00Z",
  "websocket_path": "/api/v1/servers/…/terminal/sessions/…/ws",
  "subprotocol": "gotham.terminal.v1"
}
```

The response body is a closed contract. A client validates it, and a `201`
whose shape drifts is reported as an unexpected response rather than a network
or transport failure.

Errors: `400` invalid request (including a `cwd` without a `root`), `404`
unknown node, `429` quota reached, `501` unsupported target, `503`/`500`
internal. Quotas: 4 sessions per user, 64 per control plane by default.

A created session is **pending**: no PTY exists yet. It expires after 60 s if
no WebSocket attaches. `DELETE /api/v1/servers/{id}/terminal/sessions/{sessionID}`
(mounted behind the same auth chain) cancels a pending or attached session.

### 1a. Application container targets (JUS-244)

`GET /api/v1/applications/{application_id}/terminal/targets`

Requires an **interactive JWT session** and the application write permission:
the caller must be an owner or admin of the application's active team. Scoped
API tokens are refused even when their owner has the team write role, because a
shell is an interactive capability. A `read_only` member is refused (403);
another team's application answers 404, never 403, so application IDs cannot be
probed. Listing resolves the application's **newest running deployment** and
reads the node's Docker/Compose metadata; an application with no running
deployment, an in-flight deployment, a stopped container or an unreachable node
answers an actionable error (409/404/502) instead of an empty list.

```json
{
  "application_id": "…",
  "application_name": "shop",
  "server_id": "…",
  "source_type": "compose",
  "containers": [
    {
      "service": "db",
      "container_id": "b2c3…",
      "name": "gotham-…-db-1",
      "image": "postgres:16",
      "state": "running",
      "replica": 1,
      "label": "db · gotham-…-db-1"
    },
    {
      "service": "web",
      "container_id": "a1b2…",
      "name": "gotham-…-web-1",
      "image": "nginx:1",
      "state": "running",
      "replica": 1,
      "label": "web · gotham-…-web-1"
    }
  ]
}
```

* Single-container applications return one row with an empty `service`.
* Compose applications return every **running** service container, not only the
  routed web service. `replica` is 1-based within its service, and the list is
  ordered by service, then container name, then id, so the same running set
  always produces the same visible selection.
* `container_id` is always resolved server-side. A browser never supplies a
  trusted container id to the agent.

`POST /api/v1/applications/{application_id}/terminal/sessions`

```json
{
  "service": "web",
  "container_id": "a1b2…",
  "cols": 120,
  "rows": 32
}
```

The control plane re-resolves the application's running set and requires an
exact `(service, container_id)` match. A forged id, another application's
container, an id from an older deployment, or a container labelled as another
service answers 404 `the selected container is no longer available`; it never
falls back to another service or deployment. The same resolution runs again at
the WebSocket attach, so a container replaced during the 60 s pending window, a
target that stopped, or a deployment that started in between is rejected with
the same actionable error before any agent stream is opened. The response is the same
`201` shape as the host create, plus the container fields:

```json
{
  "id": "…",
  "server_id": "…",
  "target": "container",
  "cols": 120,
  "rows": 32,
  "expires_at": "…",
  "websocket_path": "/api/v1/applications/…/terminal/sessions/…/ws",
  "subprotocol": "gotham.terminal.v1",
  "application_id": "…",
  "service": "web",
  "container_id": "a1b2…",
  "container_name": "gotham-…-web-1",
  "target_label": "web · gotham-…-web-1"
}
```

Errors are explicit and actionable: `400` invalid request, `403` insufficient
team role, `404` unknown application or container, `409` no running deployment /
deployment in progress / container stopped, `429` quota reached, `502` node agent
unavailable. Quotas are the same as host sessions (4 per user, 64 per control
plane) and are shared across both targets.

`DELETE /api/v1/applications/{application_id}/terminal/sessions/{sessionID}`
cancels a pending or attached container session exactly like the host route.

**Permission boundary.** Host terminal access is a platform-operator capability;
application container access is an application/team capability. Neither implies
the other: an application owner without the platform-operator role cannot open a
host shell, and a platform operator from another team cannot exec into an
application. A session is bound to the target it was created for, and the
handshake authorizer refuses a host target on the application route (and a
container target on the server route).

## 2. Attach the stream (WebSocket)

`GET /api/v1/servers/{server_id}/terminal/sessions/{session_id}/ws`

Pre-upgrade checks, each answering an HTTP status before the upgrade:

1. **Origin.** A browser `Origin` must match both the request scheme and `Host`
   (same origin) or an exact scheme/host entry in `TERMINAL_ALLOWED_ORIGINS`
   (comma-separated). A missing `Origin` is accepted for non-browser clients.
2. **Authentication.** The access token is read from
   `Sec-WebSocket-Protocol: gotham.token.<jwt>` (browser path) or
   `Authorization: Bearer <jwt>`. It is never accepted from the query string, so
   no reusable credential appears in URLs, proxy logs or history.
3. **Operator or application write access.** The handshake token is verified
   once, here, and must carry a `sid`; a token without one is refused because
   no server-side lineage exists that a logout could revoke. For a host
   session, operator access is resolved from the **current account record**
   (its role is `admin`) or from its account email being in
   `PLATFORM_ADMINS`. For a container session, the application's **current**
   team membership is re-read and the caller must still be an owner or admin
   of that team; the platform-operator role is irrelevant on this route. The
   stream is pinned to the session's **sign-in lineage** — the session's
   `created_at`, captured at attach. Refresh-token
   rotation revokes the presented row and inserts a replacement with a new `sid`
   but the same `created_at`, so ordinary rotation keeps the shell; logout,
   revoke-all, password reset and account deletion remove the lineage and close
   it. This check runs again every 30 seconds (with a 5-second lookup timeout),
   so a logout/revocation, demotion or removed `PLATFORM_ADMINS` entry closes
   the shell within that interval. The access token's own 15-minute expiry is
   deliberately **not** re-checked: a browser cannot present a refreshed token
   on an open socket, and the session contract is 8 hours. A demotion is read
   from the account, not from the token's immutable role claim.
4. **Ownership.** The session must belong to the authenticated user and the
   `{server_id}` in the path. Another user's session answers 404, not 403, so a
   session id cannot be probed. Ownership is validated before the upgrade;
   the session is marked attached only inside the WebSocket handshake, so an
   authenticated non-upgrade `GET` gets 400 and never burns the session.
5. **Single use.** A second attach answers 409; reconnecting always means
   creating a new session. There is no resume and no input replay.

Browsers should offer both subprotocols:

```js
new WebSocket(url, ["gotham.terminal.v1", `gotham.token.${accessToken}`])
```

The server echoes only `gotham.terminal.v1`.

### Frames

All frames are binary. The first byte is the opcode.

Client → server:

| Opcode | Payload |
|---|---|
| `0x01` input | raw bytes, 1–64 KiB per frame |
| `0x02` resize | exactly 4 bytes: columns, rows as `uint16` big-endian, each 1–1000 |
| `0x03` close | none; the server ends the session |
| `0x04` heartbeat | none; keeps a quiet session alive (any frame also works) |

Server → client:

| Opcode | Payload |
|---|---|
| `0x01` output | raw PTY bytes (UTF-8 split across frames is normal; write them verbatim to xterm.js) |
| `0x02` exited | JSON `{"exit_code":0,"signal":""}` |
| `0x03` error | bounded UTF-8 message; the socket closes afterwards |
| `0x04` opened | JSON `{"shell":"/bin/bash","cols":120,"rows":32,"service_user":"gotham-agent"}`. A container session adds `"target":"container"`, `"service"`, `"container_id"`, `"container_name"` and `"target_label"`. |
| `0x05` ping | none; a server heartbeat sent every 25 seconds |

After `opened`, exactly one `exited` or `error` ends a session unless the
transport failed. Malformed frames, oversized input and out-of-range dimensions
end the session.

A client must send at least one frame — input, resize or a `0x04` heartbeat —
every 60 seconds. The server closes a socket that goes silent, so a half-open
connection cannot hold a PTY until the 8-hour cap. A terminal view that may sit
idle should send `0x04` on an interval (25 seconds matches the server ping).

## 3. Lifecycle and cleanup

```
create ──pending (60s)──▶ attach ──confirmed (≤8h)──▶ relay ──▶ exited
   │                         │                            │
   └── expiry/DELETE ────────┴── expiry/DELETE ───────────┴── cleanup
```

* **Pending expiry** removes the session; no PTY was ever started.
* **Attach** marks the session attached and keeps a short grace lifetime until
  the relay confirms a live socket.
* **Confirm** switches to the full 8 h lifetime (the agent enforces the same cap
  independently).
* **Revalidation** runs every 30 s against server-side state only (each lookup
  gets a 5 s timeout): the account must still exist, its *current* permission
  must still grant the route's capability (host: `admin` role or a
  `PLATFORM_ADMINS` entry; container: owner/admin of the application's current
  team, and the application must still exist on the same node), and a live
  session carrying the same sign-in `created_at` must still exist. The
  handshake token's expiry is not consulted again. Rotation of the refresh token is not a logout:
  it preserves `created_at`. A definitive refusal sends an error frame and
  cancels the relay; a tick that races a normal close is silent. A transient
  lookup error keeps the shell and is logged; three consecutive failures
  (about 90 s) fail closed with a "could not be confirmed" error.
* **Idle timeout** closes a socket that receives no frame for 60 s; the server
  sends a `0x05` heartbeat every 25 s so a live client can tell a quiet shell
  from a dead one.
* **Any end** — shell exit, client close, socket error, idle timeout, expiry,
  revocation, DELETE, agent failure, control-plane shutdown — cancels the agent
  stream. For a host shell the agent closes the PTY master, sends `SIGHUP` to
  the shell's process group, and `SIGKILL`s it after a 2 s grace, so the shell's
  children die with it. For a container exec, closing the hijacked Docker
  connection **does not** end the process: the daemon keeps the exec alive. The
  agent therefore wraps the shell in a fixed recorder that prints its pid before
  `exec`ing the shell, and on every end runs a second, fixed helper exec inside
  the same container. The helper collects every process that (a) is in the
  shell's session, (b) has a parent chain reaching the shell (walked to a
  fixpoint from `/proc/<pid>/stat` after the last `)`, so process groups,
  TAB/space separators and spaces in command names do not matter), or (c)
  carries the terminal's unique `GOTHAM_TERMINAL_ID` environment marker. The
  marker catches `nohup`, plain background jobs and a job that was reparented or
  called `setsid` while its environment remained readable. The helper then sends
  `SIGHUP`/`SIGTERM`/`SIGKILL` with a short grace, ignores zombies, re-scans,
  and reports `survivors:<pids>` if anything remains. It never signals pid 1 or
  itself, never uses `kill -1` or `pkill` by name, and receives only the
  validated numeric pid and the generated marker — no caller or container value
  is interpolated into the script. When Docker already reports the exec as not
  `Running`, the agent skips the tree/session walk (the recorded pid may have
  been reused) and runs a marker-only helper, so orphaned jobs are still cleaned
  up without touching an unrelated process. In that path only readable marker
  holders are attributable; an unreadable same-uid orphan that inherited the
  marker is a documented residual limit (the helper has no readable tree to
  attribute it from once the shell is gone).
  The agent then verifies through Docker's exec inspect that the exec is no
  longer `Running`, and only then closes the attach connection and reports the
  exit status. If the helper cannot run or still reports survivors, the cleanup
  error is returned by `Close` and logged at `ERROR` with the container id — it
  is never silently treated as success. If the helper cannot inspect markers at
  all (`tr`/`grep` missing or `/proc` absent), it exits non-zero with
  `marker-scan-unavailable` instead of claiming success, even if the
  tree/session part did run. An unreadable `/proc/<pid>/environ` is **not** a
  failure by itself. The helper attributes a process only when it is the
  recorded root, in the root's session/process group, a descendant found
  through the readable `/proc/<pid>/stat` fields, or a process whose readable
  environment carries the terminal marker. A process outside that set — a
  pre-existing worker, a different-uid process, or a daemon reparented to pid 1
  that cannot be attributed — is skipped: it cannot be shown to carry our
  marker, and a same-uid non-dumpable worker that predates the terminal must not
  turn every close into a failure. Cleanup fails on **positive evidence**: an
  attributed process is still alive after `SIGKILL`, Docker still reports the
  exec `Running`, or the helper cannot inspect at all. An in-session non-dumpable
  descendant is still attributed through `/proc/<pid>/stat` and killed; if it
  survived the kill it would be reported as a survivor. The host
  child is always reaped (even when the opened event could not be sent), and a
  child that already exited is never signalled again. File descriptors,
  hijacked connections and goroutines are released on both sides.

  **What the terminal owns.** The container itself is **never stopped,
  restarted or removed** by closing a terminal. However, the marker is
  inherited by every process **started or restarted from that terminal**,
  including intentionally persistent ones — a service restarted with
  `service x restart` or `supervisorctl restart`, or a `tmux`/`screen` server
  attached from another tab. Closing the terminal ends those processes too;
  that is the intended cleanup semantics: the terminal owns the processes it
  starts. This includes application service processes other than pid 1 if they
  were started or restarted from that terminal. To keep a service alive across
  terminals, restart it from outside a terminal (the platform's deploy/restart
  path). Processes not started or restarted from the terminal, including the
  application's pid 1, are not signalled. The attribution exceptions above
  still apply: a non-dumpable process that has left the tree/session and whose
  environment cannot be read may survive without an error.

  **What cleanup guarantees.** Close ends the interactive shell, foreground and
  background jobs in its process groups, every process in the shell's session,
  descendants reachable through the readable `/proc/<pid>/stat` parent chain
  (including HUP-ignoring jobs and an in-tree non-dumpable child, because `stat`
  is readable even when `environ` is not), and processes whose **readable**
  environment carries the terminal marker. The marker catches ordinary
  `nohup`, plain background and single-`setsid` jobs (setsid does not fork, so
  the process stays in the stat tree until a reparent), and jobs that were
  reparented while their environment is still readable. A command name
  containing a newline is parsed correctly because the helper reads the whole
  `stat` record.

  **What cleanup does not guarantee — Close reports success.** A process can
  outlive Close without an error when it is **non-dumpable** and has left the
  attributable set: it was reparented or `setsid`-ed so it is outside the
  session/tree, and its environment is unreadable so the marker cannot be seen.
  This includes a non-dumpable child of a reparented marker holder, a daemon
  that double-forks and escapes the tree, and a process that scrubs its
  environment. A pre-existing same-uid non-dumpable worker (for example a worker
  that dropped privileges) is also skipped: it predates the terminal and cannot
  carry the terminal's marker. A terminal user who uses `su`/`sudo` to switch to
  another uid and leaves processes there is outside the helper's reach: a
  process it does not own cannot be inspected or signalled without
  `CAP_SYS_PTRACE`. The marker-only path after the shell has already exited has
  the same attribution limit for an unreadable same-uid orphan. In all of these
  cases the helper has no positive evidence tying the process to the terminal,
  so it does not kill it and **does not report an error**; the process may
  outlive Close.

  **Time budget.** The helper is bounded by the control plane's 8 s context, but
  that is best-effort under CPU starvation: a deep tree on a starved container
  can exceed it. Close then reports incomplete cleanup, and the exec is still
  force-closed.
* **Agent failure / old agent** ends the stream with an error frame:
  `codes.Unimplemented` is reported as "the node agent does not support terminal
  sessions". The control plane never retries, reconnects or replays input.
* **Multiple sessions** are independent streams; they share no buffers and no
  PTY.

## 4. Working directory and privilege boundary

The initial cwd is resolved **on the agent**. A host session with a `root` uses
the FileService root plus the relative path, using the same policy as the file
manager: the root must be allow-listed, `..` and absolute paths are refused, a
symlink whose target leaves the root is refused, and the result must be an
existing directory. A root at `/` (the default full-access `system` root)
contains every absolute path, so a cwd such as `tmp` or `usr` resolves normally.

A host session **without** a `root` starts in the agent's default directory: the
agent service user's home directory if it exists and is a directory, otherwise
`/`. The home is read from the service user's passwd entry, never from `HOME`,
the request or any browser value; it is resolved through symlinks and must still
be a directory, otherwise the fallback is `/`. With no root there is no new path
from browser input to the start directory at all: the browser sends no start
path, and the agent alone decides it. A `cwd` without a `root` is refused
(`invalid request` at the control plane, `InvalidArgument` at the agent) before
any shell starts.

In both cases the resolved absolute path is set directly on the child process
(`exec.Cmd.Dir`); it is never interpolated into a shell command, so no quoting
or escaping is involved. A cwd the agent refuses is reported to the client as
the bounded phrase `invalid working directory` (no host path); a missing or
unreadable directory is reported separately.

**The shell itself is not confined to FileService roots or to the default
directory, and on a standard install it is effectively root on the host.** A
root/cwd only chooses where the shell starts; the process may `cd` anywhere the
service user can read, and the rootless entry grants exactly the same
capability. Host terminal access is a platform-operator capability first; the
process runs as the agent's service identity (`User=gotham-agent` in
`deploy/gotham-agent.service`), but that identity is not a sandbox:

* The shipped unit adds `SupplementaryGroups=docker`. The shell can therefore
  talk to `/var/run/docker.sock` and start a privileged container that mounts
  the host filesystem — effective root on the node.
* `NoNewPrivileges` is intentionally unset (the updater needs setuid `sudo`),
  and the installer grants `NOPASSWD` sudo for the fixed update and toolctl
  helpers. The shell can run those exact commands without a password.
* The agent's state directory (`/var/lib/gotham-agent`, `WorkingDirectory` of the
  unit) holds the agent's mTLS certificate and private key. The shell can read
  them, so it can impersonate the agent to the control plane for as long as
  that credential lives.

Treat a host terminal as **full host administration (effectively root)**, not as
a file-browser session. The platform-operator gate and the periodic
re-authorization are the real boundary. The file-manager root (or the agent's
default directory) only chooses where the shell starts; `cwd` confinement is not
a privilege boundary. The UI should
state this next to the `opened` event's `service_user` (JUS-245). The agent unit
is not weakened for the terminal: the capabilities above are required by
Docker management and self-update.

The shell environment is an allowlist (locale, `PATH`, `HOME`, `USER`, terminal
description). The agent's own environment — including
`GOTHAM_AGENT_ENROLLMENT_TOKEN` and certificate paths — is not inherited; the
files themselves remain readable by the service user, as described above.

## 4a. Container resolution and privilege boundary

A container terminal is an **application** capability, not a node capability:

* The control plane resolves the application's newest running deployment. A
  non-terminal (in-flight) deployment answers 409; no running deployment answers
  409 with "no running deployment"; an unreachable agent answers 502.
* For a Compose application, the project name is derived from the application
  id (`gotham-<uuid>`) and every running service container is listed with
  `ComposePs`. The project's stopped containers are not targets; a stale or
  forged id cannot be used.
* For a single-container application, the deployment's recorded container is
  read back from Docker. A missing container ("redeploy the application") and a
  stopped container ("start the application first") are distinct errors.
* Creating a session re-resolves the set and requires an exact match on both
  `service` and `container_id`. This is what prevents a forged target, a
  container of another application and an old release from being attached.
* The agent accepts only the full 64-character lowercase hexadecimal Docker id
  the control plane resolved; container names, short prefixes and upper-case
  spellings are refused, so the agent never resolves an ambiguous or
  caller-chosen name.
* The exec runs as the container's configured user (`root` when unset), inside
  the container's namespaces and mounts. It does not grant host access, and the
  host shell does not grant container access.
* The agent probes the image for `/bin/bash`, `/bin/sh` or `/bin/ash` with a
  fixed, non-interpolated command. An image with none of them answers
  "the container has no supported shell" instead of starting a broken terminal.
  The chosen shell is reported in `opened.shell`; the container user is reported
  in `opened.service_user`.

## 5. Agent-side configuration

| Variable | Meaning |
|---|---|
| `GOTHAM_AGENT_TERMINAL_SHELL` | Absolute shell path. Empty picks the first usable of `$SHELL`, `/bin/bash`, `/bin/sh`. |
| `GOTHAM_AGENT_TERMINAL_MAX_SESSIONS` | Concurrent host shells on the node (default 8). Excess opens answer `ResourceExhausted`. |

The TerminalService is always registered by the agent (JUS-244). A host target
with a `root` needs the file service's root policy: with
`GOTHAM_AGENT_FILES=false` the Roots resolver is absent and such an open answers
`FailedPrecondition`. A host target **without** a root does not use the resolver
and still opens in the default directory, so a node with the file service
disabled can run a host shell. Container targets keep working through the
Docker client. An agent that predates the service answers `codes.Unimplemented`,
which the control plane surfaces as "unsupported". Container exec uses the
existing Docker Engine HTTP client (no second SDK): create an exec, hijack
`POST /exec/{id}/start`, resize with `POST /exec/{id}/resize`, and read the
final status from `GET /exec/{id}/json`.

**Older agents.** A JUS-243 agent requires a root on every host open. A new
control plane that sends the empty root of the no-File-manager entry gets
`InvalidArgument` ("a bounded root name is required") from that agent. The
control plane maps exactly that refusal to the bounded error frame `the node
agent needs an update to open a terminal without a folder` instead of the
generic `the terminal request was rejected`. It never retries, never falls back
to another directory, and the File manager entry (root + cwd) keeps working
against the older agent. Upgrade the agent to JUS-253 or later for rootless host
sessions.

## 6. Verification

The PTY path is exercised by real shells, not mocks. Commands below were run on
the JUS-243 and JUS-253 branches; the Linux runs use the cross-compiled agent
test binary inside a container so the kernel, PTY driver and `/bin/sh` are Linux
even on a macOS host.

```sh
# Cross-builds for both release architectures.
GOOS=linux GOARCH=amd64 go build ./...
GOOS=linux GOARCH=arm64 go build ./...

# Linux/arm64 (native on an arm64 host).
GOOS=linux GOARCH=arm64 go test -c ./agent -o /tmp/gotham-agent-arm64.test
docker run --rm -v /tmp/gotham-agent-arm64.test:/agent.test alpine:3 \
  /agent.test -test.run 'TestTerminal|TestResolveShellDir|TestResolveDefaultTerminalDir'
# PASS

GOOS=linux GOARCH=arm64 go test -c ./internal/terminal -o /tmp/gotham-terminal-arm64.test
docker run --rm -v /tmp/gotham-terminal-arm64.test:/terminal.test alpine:3 /terminal.test
# PASS

# Linux/amd64 (emulated on the same arm64 host).
GOOS=linux GOARCH=amd64 go test -c ./agent -o /tmp/gotham-agent-amd64.test
docker run --rm --platform linux/amd64 -v /tmp/gotham-agent-amd64.test:/agent.test \
  alpine:3 /agent.test -test.run 'TestTerminal|TestResolveShellDir|TestResolveDefaultTerminalDir'
# PASS

GOOS=linux GOARCH=amd64 go test -c ./internal/terminal -o /tmp/gotham-terminal-amd64.test
docker run --rm --platform linux/amd64 -v /tmp/gotham-terminal-amd64.test:/terminal.test \
  alpine:3 /terminal.test
# PASS
```

The same suites also run natively on the development host (darwin). `go test
-race ./agent ./internal/terminal` passes as part of the pre-push checks; the
terminal tests cover session isolation, fd/goroutine release after close and
disconnect, and process-group death on connection loss.

The JUS-253 rootless entry is covered by real PTY tests: an open with no
`root`/`cwd` runs `printf` and asserts `$PWD` equals the agent's default
directory (the service user's home or `/`); a server built **without** a Roots
resolver (`GOTHAM_AGENT_FILES=false`) still opens that shell while a named root
is refused with `FailedPrecondition`; a `cwd` without a root fails in validation
before `start` is called; and an older agent's `InvalidArgument` refusal is
mapped in the relay test. Default-directory fallbacks (file, missing target,
symlink to a file, relative or empty home) are unit-tested without a shell. A
Go test pins the exact 201 key set (`root`/`cwd` omitted when empty, for both a
rootless host and a container), and the same literals are parsed by the web
schema tests (host with/without a root and the container response). The web
regression in `web/tests/terminal-page.spec.ts` drives both the Terminal page
and the Server detail tab against a fake that validates the create body with the
backend rules (root optional but shaped like the regex; `cwd` requires `root`),
returns backend-shaped responses, and asserts the exact payload
`{"target":"host","cols":120,"rows":32}`. A malformed 2xx body is asserted to
show the bounded "unexpected response" copy, never a network error, and to open
no socket.

Container exec additionally runs **real throwaway container** checks. They
start unique `alpine:3` containers (never the shared `gotham-dev-postgres` /
`gotham-dev-redis`) and remove them afterwards. The suite covers:

* the review table on `alpine:3`, `busybox:1.36` and `debian:bookworm-slim`:
  foreground `sleep`, `trap '' HUP TERM INT`, `nohup x &`, plain `x &`,
  `(setsid x &)`, nested `( (x &) & )`, a shell whose command name contains a
  newline with an env-scrubbed child, and a shell that exits before Close. Each
  case scans `/proc` from a fresh exec (not the kill helper) for the session's
  unique environment marker and checks Docker's exec inspect after Close;
* the same hard rows after **transport loss** (cancelled terminal stream),
  scanning the unique job's command line;
* **Ctrl-Z / `jobs` / `bg` / `fg`** followed by Ctrl-C and a usable prompt;
* **concurrent CloseSend + cancellation** (5 racing cycles) and the **lifetime
  cap** (short `MaxDuration`) with no double-kill panic, run under `-race`;
* a cleanup-failure case (`chmod -x /bin/sh` after the terminal opens) proving
  `Close` surfaces the failure instead of reporting success;
* `marker-scan-unavailable` when the image has no `tr`/`grep` or no `/proc`;
* a **pre-existing** same-uid non-dumpable worker (a perl process that drops to
  `nobody`, like an nginx/php-fpm worker) does not fail a nobody terminal's
  Close and survives; an **in-tree** non-dumpable descendant is attributed via
  `/proc/<pid>/stat` and killed;
* a **non-root** terminal (`nobody`, numeric uid `2000`) sharing the container
  with a root-owned process on alpine, busybox and debian: the foreign-uid
  unreadable environment is skipped and `Close` succeeds, while the unrelated
  root process keeps running;
* **120 foreign-uid processes** with a nobody terminal: `Close` completes in
  about 0.7 s, well inside the 8 s helper budget (the test asserts <4 s and
  logs the elapsed time);
* helper-output cleanliness while processes exit during the `/proc` scan, on
  alpine (ash) and debian (dash);
* resize observed through `stty size`, Ctrl-C, and three open/close cycles.

```sh
GOTHAM_E2E=1 go test -race ./agent -run 'TestDockerClientExecReal|TestDockerExecReal' -v
```

The same suite is a step in `.github/workflows/e2e.yml` (the existing
`GOTHAM_E2E=1` job on GitHub-hosted Ubuntu with Docker), so the lifecycle is
gated in CI; `alpine:3` and `debian:bookworm-slim` are pre-pulled with the other
e2e images (`busybox:1.36` is already there). A developer without a local Docker
daemon can run the unit suite only.

Unit coverage (no daemon needed) exercises the hijacked exec stream, shell
probe, wrapper pid, fixed kill helper, resize and exit status against a faithful
Docker HTTP fake, plus the terminal server's container target mapping,
stopped/missing/no-shell errors, attach-time re-resolution and the
control-plane resolution/permission/error cases.
Findings from review round 1 (periodic revalidation, privilege wording, arm64
proof, isolation/resource tests, idle bound, early-return reaping, post-reap
signalling, Origin scheme and non-upgrade attach) are covered by these tests.
The JUS-244 round-1 fixes add: explicit exec cleanup verified through Docker
inspect, the real-container lifecycle suite above, attach-time target
re-resolution, interactive-session-only control routes, strict 64-hex container
ids, and a bounded container write.
