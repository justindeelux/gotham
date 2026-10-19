# Terminal session API and lifecycle (JUS-243)

This document is the contract dependent work packages (JUS-244 container exec,
JUS-245 shared terminal view) build on. It describes the browser → control
plane → node agent path for a host shell, its limits and its cleanup
guarantees.

## Topology

```
browser ──HTTP POST /api/v1/servers/{id}/terminal/sessions──▶ control plane
browser ──WebSocket /api/v1/.../ws─────────────────────────▶ control plane
control plane ──gRPC TerminalService.OpenTerminal (mTLS)────▶ node agent ──PTY──▶ shell
```

Terminal input and output travel only on that path. They are **never** published
to Redis pub/sub, the shared WebSocket log hub, or any log line. The control
plane's session registry is in-memory; a control-plane restart ends every
session (the agent tears the PTY down when the stream disappears).

## 1. Create a session (HTTP)

`POST /api/v1/servers/{server_id}/terminal/sessions`

Requires an authenticated, platform-operator caller (`RequireAuth` +
`RequirePlatformAdmin`). The route is mounted with the server registry; without
one it is absent.

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
| `target` | `host` or omitted. Anything else answers 501 `not_implemented`; JUS-244 adds container targets behind the same field. An old agent never silently falls back to the host. |
| `root` | A FileService root name (`^[a-z0-9][a-z0-9_-]{0,127}$`). The agent resolves it against its own allow-list. |
| `cwd` | Relative to `root`, at most 4096 bytes, no NUL, no absolute path, no `..` segment. Empty/`.` means the root itself. |
| `cols`, `rows` | 1–1000. |

Response `201`:

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

Errors: `400` invalid request, `404` unknown node, `429` quota reached, `501`
unsupported target, `503`/`500` internal. Quotas: 4 sessions per user, 64 per
control plane by default.

A created session is **pending**: no PTY exists yet. It expires after 60 s if
no WebSocket attaches. `DELETE /api/v1/servers/{id}/terminal/sessions/{sessionID}`
(mounted behind the same auth chain) cancels a pending or attached session.

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
3. **Operator.** The handshake token is verified once, here. Operator access is
   then resolved from the **current account record** (its role is `admin`) or
   from its account email being in `PLATFORM_ADMINS`; the account must still
   exist and, when the token carries a `sid`, its refresh session must still be
   live. This check runs again every 30 seconds for the life of the stream, so a
   logout/revocation, demotion or removed `PLATFORM_ADMINS` entry closes the
   shell within that interval. The access token's own 15-minute expiry is
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
| `0x04` opened | JSON `{"shell":"/bin/bash","cols":120,"rows":32,"service_user":"gotham-agent"}` |
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
* **Revalidation** runs every 30 s against server-side state only: the account
  must still exist, its *current* role (or `PLATFORM_ADMINS` entry) must still
  grant operator access, and the `sid` refresh session must still be live. The
  handshake token's expiry is not consulted again. A real failure sends an
  error frame and cancels the relay; a tick that races a normal close is silent.
* **Idle timeout** closes a socket that receives no frame for 60 s; the server
  sends a `0x05` heartbeat every 25 s so a live client can tell a quiet shell
  from a dead one.
* **Any end** — shell exit, client close, socket error, idle timeout, expiry,
  revocation, DELETE, agent failure, control-plane shutdown — cancels the agent
  stream. The agent closes the PTY master, sends `SIGHUP` to the shell's process
  group, and `SIGKILL`s it after a 2 s grace, so the shell's children die with
  it. The child is always reaped (even when the opened event could not be sent),
  and a child that already exited is never signalled again. File descriptors and
  goroutines are released on both sides.
* **Agent failure / old agent** ends the stream with an error frame:
  `codes.Unimplemented` is reported as "the node agent does not support terminal
  sessions". The control plane never retries, reconnects or replays input.
* **Multiple sessions** are independent streams; they share no buffers and no
  PTY.

## 4. Working directory and privilege boundary

The initial cwd is resolved **on the agent** from the FileService root plus the
relative path, using the same policy as the file manager: the root must be
allow-listed, `..` and absolute paths are refused, a symlink whose target leaves
the root is refused, and the result must be an existing directory. The resolved
absolute path is set directly on the child process (`exec.Cmd.Dir`); it is never
interpolated into a shell command, so no quoting or escaping is involved.

**The shell itself is not confined to FileService roots, and on a standard
install it is effectively root on the host.** Host terminal access is a
platform-operator capability first; the process runs as the agent's service
identity (`User=gotham-agent` in `deploy/gotham-agent.service`), but that
identity is not a sandbox:

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
re-authorization are the real boundary. The file-manager root only chooses where
the shell starts; `cwd` confinement is not a privilege boundary. The UI should
state this next to the `opened` event's `service_user` (JUS-245). The agent unit
is not weakened for the terminal: the capabilities above are required by
Docker management and self-update.

The shell environment is an allowlist (locale, `PATH`, `HOME`, `USER`, terminal
description). The agent's own environment — including
`GOTHAM_AGENT_ENROLLMENT_TOKEN` and certificate paths — is not inherited; the
files themselves remain readable by the service user, as described above.

## 5. Agent-side configuration

| Variable | Meaning |
|---|---|
| `GOTHAM_AGENT_TERMINAL_SHELL` | Absolute shell path. Empty picks the first usable of `$SHELL`, `/bin/bash`, `/bin/sh`. |
| `GOTHAM_AGENT_TERMINAL_MAX_SESSIONS` | Concurrent host shells on the node (default 8). Excess opens answer `ResourceExhausted`. |

The TerminalService is registered only when the file service is enabled
(`GOTHAM_AGENT_FILES` not false), because the initial cwd needs a root policy.
An agent without it answers `codes.Unimplemented`, which the control plane
surfaces as "unsupported".

## 6. Verification

The PTY path is exercised by real shells, not mocks. Commands below were run on
the JUS-243 branch; the Linux runs use the cross-compiled agent test binary
inside a container so the kernel, PTY driver and `/bin/sh` are Linux even on a
macOS host.

```sh
# Cross-builds for both release architectures.
GOOS=linux GOARCH=amd64 go build ./...
GOOS=linux GOARCH=arm64 go build ./...

# Linux/arm64 (native on an arm64 host).
GOOS=linux GOARCH=arm64 go test -c ./agent -o /tmp/gotham-agent-arm64.test
docker run --rm -v /tmp/gotham-agent-arm64.test:/agent.test alpine:3 \
  /agent.test -test.run 'TestTerminal|TestResolveShellDir'
# PASS

GOOS=linux GOARCH=arm64 go test -c ./internal/terminal -o /tmp/gotham-terminal-arm64.test
docker run --rm -v /tmp/gotham-terminal-arm64.test:/terminal.test alpine:3 /terminal.test
# PASS

# Linux/amd64 (emulated on the same arm64 host).
GOOS=linux GOARCH=amd64 go test -c ./agent -o /tmp/gotham-agent-amd64.test
docker run --rm --platform linux/amd64 -v /tmp/gotham-agent-amd64.test:/agent.test \
  alpine:3 /agent.test -test.run 'TestTerminal|TestResolveShellDir'
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
Findings from review round 1 (periodic revalidation, privilege wording, arm64
proof, isolation/resource tests, idle bound, early-return reaping, post-reap
signalling, Origin scheme and non-upgrade attach) are covered by these tests.
