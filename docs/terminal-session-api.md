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

1. **Origin.** A browser `Origin` must match the request `Host` (same origin) or
   an entry in `TERMINAL_ALLOWED_ORIGINS` (comma-separated, scheme included). A
   missing `Origin` is accepted for non-browser clients.
2. **Authentication.** The access token is read from
   `Sec-WebSocket-Protocol: gotham.token.<jwt>` (browser path) or
   `Authorization: Bearer <jwt>`. It is never accepted from the query string, so
   no reusable credential appears in URLs, proxy logs or history.
3. **Operator.** The token's role claim is `admin`, or its account email is in
   `PLATFORM_ADMINS`. This is re-checked on attach, not only at creation.
4. **Ownership.** The session must belong to the authenticated user and the
   `{server_id}` in the path. Another user's session answers 404, not 403, so a
   session id cannot be probed.
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

Server → client:

| Opcode | Payload |
|---|---|
| `0x01` output | raw PTY bytes (UTF-8 split across frames is normal; write them verbatim to xterm.js) |
| `0x02` exited | JSON `{"exit_code":0,"signal":""}` |
| `0x03` error | bounded UTF-8 message; the socket closes afterwards |
| `0x04` opened | JSON `{"shell":"/bin/bash","cols":120,"rows":32,"service_user":"gotham-agent"}` |

After `opened`, exactly one `exited` or `error` ends a session unless the
transport failed. Malformed frames, oversized input and out-of-range dimensions
end the session.

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
* **Any end** — shell exit, client close, socket error, expiry, DELETE,
  control-plane shutdown — cancels the agent stream. The agent closes the PTY
  master, sends `SIGHUP` to the shell's process group, and `SIGKILL`s it after a
  2 s grace, so the shell's children die with it. File descriptors and
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

**The shell itself is not confined to FileService roots.** Host terminal access
is a platform-operator capability first, and the process runs as the agent's
service identity — `gotham-agent` under the shipped systemd unit
(`deploy/gotham-agent.service`, `User=gotham-agent`). It can read and write
anything that user may, including changing directory outside the starting root.
That is the intended host-administration boundary; the file-manager root only
chooses where the shell starts. Running the agent as root would make the shell
root, which the unit deliberately does not do.

The shell environment is an allowlist (locale, `PATH`, `HOME`, `USER`, terminal
description). The agent's own environment — including
`GOTHAM_AGENT_ENROLLMENT_TOKEN` and certificate paths — is not inherited.

## 5. Agent-side configuration

| Variable | Meaning |
|---|---|
| `GOTHAM_AGENT_TERMINAL_SHELL` | Absolute shell path. Empty picks the first usable of `$SHELL`, `/bin/bash`, `/bin/sh`. |
| `GOTHAM_AGENT_TERMINAL_MAX_SESSIONS` | Concurrent host shells on the node (default 8). Excess opens answer `ResourceExhausted`. |

The TerminalService is registered only when the file service is enabled
(`GOTHAM_AGENT_FILES` not false), because the initial cwd needs a root policy.
An agent without it answers `codes.Unimplemented`, which the control plane
surfaces as "unsupported".
