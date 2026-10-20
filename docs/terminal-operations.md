# Terminal operations (JUS-251)

Operator-facing guide for the Gotham terminal: what a shell actually is, which
identity it runs as, what closing it kills, what is stored in the browser, how
to upgrade a node safely, and how to read the failures the UI and the logs
report.

The byte-level contract (HTTP routes, WebSocket frames, lifecycle, container
cleanup helper) lives in [`terminal-session-api.md`](terminal-session-api.md);
prompt marks and quick-command suggestions live in
[`terminal-shell-integration.md`](terminal-shell-integration.md). This page is
the operational summary and the validation record.

## What was validated, and on which stack

The terminal feature packages were originally covered by unit and component
tests with fakes. Those fakes accepted whatever the browser sent, so they
missed four defects that only a real browser on a real node exposed: a page
host shell sent no `root` and failed (JUS-253), the response schema required
fields the backend omits (JUS-253), closing a host shell left background jobs
and leaked a PTY master (JUS-254), and the moved-to-window page notice painted
over the floating window (JUS-255), while Compose targets carried 12-hex ids
the agent rejected (JUS-256). JUS-251 therefore validates the **real stack**:

| Layer | Real in the browser suite | Real in the Go suite |
|---|---|---|
| Control plane | `gotham serve` (embedded SPA) | `terminal.Service` + production routes |
| Database / Redis | PostgreSQL 16 + Redis 7 containers | same, throwaway |
| Node agent | `gotham-agent` built from the repo | in-process agent over mTLS |
| Host shell | real PTY (zsh on macOS, bash on Linux) | real PTY |
| Application shell | real `docker exec` in a deployed container | covered by the agent lifecycle suite (JUS-244) |
| Compose shell | real `docker compose` project | covered by deploy/GS-8 suites |

Entry points:

```sh
# Browser suite (real CP + real agent + real PTYs + Docker):
cd web
GOTHAM_E2E_BASE_URL=http://127.0.0.1:8099 \
GOTHAM_E2E_EMAIL=ui-e2e-admin@example.com \
GOTHAM_E2E_TERMINAL_NODE=localhost \
GOTHAM_E2E_AGENT_PID=$(cat "$RUNNER_TEMP/gotham-agent.pid") \
  npx playwright test e2e/terminal-host.spec.ts \
    e2e/terminal-regressions.spec.ts \
    e2e/terminal-application.spec.ts \
    e2e/terminal-compose.spec.ts

# Go integration/security proof (throwaway Postgres + Redis + Docker):
GOTHAM_E2E=1 \
GOTHAM_TEST_DSN='postgres://gotham:gotham@localhost:5433/gotham?sslmode=disable' \
GOTHAM_E2E_REDIS=127.0.0.1:6380 \
  go test ./internal/e2e -run TestTerminalRealStackNoLeak -count=1 -v
```

`.github/workflows/ui-e2e.yml` starts its own control plane, its own node agent
and its own Postgres/Redis service containers. It never reuses the long-lived
`gotham-dev-postgres` / `gotham-dev-redis`, and its final step stops the agent,
the control plane and any leftover Compose container. Follow the same rule on
a developer machine (see [`test-server.md`](test-server.md)).

### Scenario → proof map

| Scenario | Real browser | Real Go |
|---|---|---|
| Main-menu Terminal, lazy server selection, one create per selection | `terminal-host.spec.ts` | — |
| Server detail / page: full logo banner exactly once, zero banner input frames | `terminal-host.spec.ts` | — |
| Page → floating window: same session id, socket and buffer | `terminal-host.spec.ts` | — |
| Minimize / rail restore, close dialog Cancel and Confirm | `terminal-host.spec.ts` | — |
| Theme change: no PTY input, no new session | `terminal-host.spec.ts` | — |
| UTF-8 typing (CDP key events) and paste, `stty size` after resize | `terminal-host.spec.ts` | resize frame in `TestTerminalRealStackNoLeak` |
| Ctrl-C, `exit`, session-ended overlay | `terminal-host.spec.ts` | agent PTY tests |
| Quick-command suggestion from a real bash OSC 133 `B` mark | `terminal-host.spec.ts` | `terminal-quick-command-pty.test.ts` |
| Sign-out with a live shell: confirmation, Cancel keeps the PTY | `terminal-host.spec.ts` | authorizer tests |
| Host shell with no `root`/`cwd` | `terminal-regressions.spec.ts` | `TestTerminalRealStackNoLeak` |
| File manager "Open terminal here": `root` + `cwd`, window mode, double-click guard | `terminal-regressions.spec.ts` | — |
| `sleep N &` job killed, PTY fd released on close | `terminal-regressions.spec.ts` | `TestTerminalRealStackNoLeak` |
| Moved notice never above the window (1366/1280/480/390 px) | `terminal-regressions.spec.ts` | — |
| Application tab lazy start, exact service + 64-hex container id | `terminal-application.spec.ts` | `internal/deploy` terminal tests |
| Retained window session + "Start new session" (1 GET + 1 POST) | `terminal-application.spec.ts` | — |
| Replaced container: rediscover then POST the fresh id | `terminal-application.spec.ts` | — |
| read_only: 403 on targets/create, 404 without team scope | `terminal-application.spec.ts` | `TestApplicationAuthorizerRequiresWriteAccess` |
| Real Compose project: every service, full id, shell, container survives close | `terminal-compose.spec.ts` | GS-8 / JUS-256 deploy tests |
| Terminal bytes absent from CP/agent logs and Redis pub/sub | — | `TestTerminalRealStackNoLeak` |
| `cwd` through an escaping symlink refused before a shell starts | — | `TestTerminalRealStackNoLeak` |
| Unauthorized, cross-user and cross-node session ids | — | `internal/terminal` route/service tests, `internal/server` wiring tests |
| Wrong Origin, token/session expiry, logout vs refresh rotation | — | `TestOriginSchemeMustMatch`, `TestShellSurvivesAccessTokenExpiry`, `TestTerminalAuthorizerSurvivesSessionRotation` |
| Agent loss mid-session | — | `TestTerminalDisconnectKillsProcessGroup` |
| Container loss / replaced container / Docker loss | — | JUS-244 lifecycle suite (`agent` real-Docker tests) |

## Supported shells and shortcut integration

A host terminal runs `$SHELL` when it is a usable absolute path, else
`/bin/bash`, else `/bin/sh`. The agent accepts an explicit override with
`GOTHAM_AGENT_TERMINAL_SHELL` (absolute path). Application container shells use
the container's own `/bin/sh`/`/bin/bash`; a container without either reports
"This container has no supported shell."

| Shell | Host | Container | Prompt marks (quick-command suggestions) |
|---|---|---|---|
| bash ≥ 4.4 | yes | yes (if installed) | yes — proven against a real bash PTY (Playwright + Go) |
| zsh ≥ 5.3 | yes | rare | yes — snippet documented; validated manually on macOS |
| sh / dash / busybox ash | yes (`/bin/sh`) | yes | no `PROMPT_COMMAND`; suggestions stay off until a shell that emits OSC 133 runs |
| fish, PowerShell, cmd | not supported | not supported | — |

Gotham never injects shell integration. The operator adds the additive snippet
from [`terminal-shell-integration.md`](terminal-shell-integration.md) to
`~/.bashrc` or `~/.zshrc`; without it the shell works and only the abbreviation
suggestions stay disabled. The suggestion engine accepts OSC 133/633 marks,
starts a suggestion only after an editable-prompt (`B`) mark, and drops any
state it cannot prove (Enter, Escape, history recall, Tab completion, paste,
alternate-screen programs). Suggestions are never executed by UI code: the
command is inserted, and the operator presses Enter.

## Host process identity and privileges

* **Identity.** The host shell runs as the node agent's service identity. On a
  standard install the agent runs as `root` (the installer's
  `file-manager.conf` drop-in runs the agent unsandboxed as root), so a host
  shell is **full host administration**. It is not a sandbox and it is not
  confined to the File manager roots: `root`/`cwd` only choose the starting
  directory, they do not restrict where the shell can go.
* **Permission.** Only a platform operator (an account with the `admin` role,
  or an email listed in `PLATFORM_ADMINS`) may create a host session. A team
  owner/admin role never grants a host shell, and a platform operator role
  never grants a container shell.
* **Default start directory.** A session without `root`/`cwd` (the Terminal
  page and the Server detail tab) starts in the agent's own working directory,
  falling back to `/` for a root agent with no usable home (JUS-253). The File
  manager's "Open terminal here" sends the root name and the root-relative
  folder, and the agent resolves it through the FileService root (symlinks and
  `..` are refused when they leave the root).
* **Audit.** Creating, attaching and failing sessions are logged with
  `session_id`, user and node. Terminal **bytes are never logged** and never
  published to Redis; the Go proof subscribes to every channel while typing a
  unique canary and asserts the canary appears in neither log.

## Close, disconnect, expiry: what ends and what is killed

Any end — the operator's close, tab/window close, sign-out, socket loss, idle
timeout, the 8 h cap, a revalidation refusal, DELETE, agent loss or control
plane shutdown — ends the agent stream. For a host shell:

1. `SIGHUP` to the shell's process group and close the PTY master (bounded,
   inline).
2. A tracked background walk finds every process in the shell's **session**
   (`/proc/<pid>/stat` session id), sends `SIGTERM`, then `SIGKILL` after a 2 s
   grace. This ends `sleep N &`, `nohup x &`, nested subshell jobs and
   HUP/TERM-ignoring jobs.
3. The PTY master descriptor and the output goroutine are released; a
   `setsid`-escaped job cannot pin the agent because the master is non-blocking.

What cleanup guarantees and what it does not:

| Case | Result |
|---|---|
| Foreground and background jobs, same session | killed |
| `nohup`, HUP/TERM-ignoring job | killed |
| Nested subshell job | killed |
| `(setsid x &)` (new session) | **may survive**; the fd is still released |
| Process that changed uid (`su`, `sudo`) | root agent kills it; non-root agent leaves it (`EPERM`) |
| Process hidden by `hidepid` / restricted `/proc` | cannot be attributed; `WARN` + group fallback |
| Double-fork daemon reparented to pid 1 | may survive; reported only with positive evidence |
| Another user's session | never signalled |

Residual windows are documented in
[`terminal-session-api.md`](terminal-session-api.md) §3. A truncated or
incomplete scan is **never** silent: the agent logs

```
WARN terminal: session scan incomplete; falling back to the process group session_id=… members_found=…
```

and keeps the group fallback plus hard-kills the members it did find. Treat
that line as "the scan could not see everything" (large host, `hidepid`, a
restricted namespace), not as a failure of the close.

A **container** terminal additionally runs the fixed cleanup helper described
in the API doc: it never stops, restarts or removes the application container,
but it does end every process started or restarted from that shell (including
a service restarted via `service … restart` or a `tmux`/`screen` server started
there). To keep something alive across terminals, start it from the platform's
deploy/restart path. Cleanup failures are logged at `ERROR` with the container
id and returned by `Close`; they are never reported as success.

## Browser-local state

| Item | Where | Scope | Secret? |
|---|---|---|---|
| Quick commands | `localStorage` `gotham.terminal.commands.v1.<user id>` | one account, one browser | **no** — the UI says so; never store secrets |
| Terminal theme | `localStorage` (appearance store) | one browser | no |
| Floating-window position / minimized state | `localStorage` | one browser | no |
| Session tokens | the SPA's session storage | one account, one browser | yes — logout clears them |

Quick commands are limited to 40 entries and a bounded payload; a corrupt or
oversized entry reads as an empty list instead of breaking the terminal. They
never leave the browser and are never sent to the control plane.

## Keyboard

| Shortcut | Effect |
|---|---|
| `Ctrl+Shift+E` | leave the terminal input; focus returns to the window tab or the pane action |
| `Shift+Tab` | same escape, for a shell that uses the former |
| `Ctrl+Shift+Space` | focus a visible quick-command suggestion |
| `Enter` / `Escape` / `Tab` (in the suggestion list) | insert without running / dismiss / return focus |
| `Alt+M` (window header or tab focused) | minimize the floating window |
| `Alt+Shift+W` (window header or tab focused) | close all window terminals (confirmation) |
| `Delete` (a focused window tab) | close that terminal (confirmation) |

Inside the shell, `Alt`/`Option` keys go to the terminal (readline, vim, tmux).
The theme menu and shortcut popover stop their own keys, so nothing typed in
them reaches the PTY.

## Upgrade and compatibility

* The terminal surface is **additive**: a new control plane talking to an old
  agent receives `Unimplemented` and renders "This node agent does not support
  terminal sessions". Nothing else on the node is affected.
* An old control plane never opens a terminal session on a new agent; the
  routes simply do not exist.
* Proto changes are additive only (new fields, new RPCs); a mixed fleet during
  a rollout keeps working.
* Recommended order: upgrade nodes first (they gain the feature), then the
  control plane, then the SPA ships embedded in the control plane. A node
  restart closes its live shells (the agent is the PTY owner); the UI reports
  "Connection lost" and never replays input.
* `GOTHAM_AGENT_TERMINAL_MAX_SESSIONS` bounds concurrent host sessions per node
  (control-plane quota: 4 per operator, 64 overall). Raising it does not change
  the per-session 8 h cap.
* The container terminal requires the applications feature on the control
  plane. Turning it off answers "Container terminals are disabled on this
  control plane".

## Troubleshooting

| UI card | Meaning | Action |
|---|---|---|
| "Only a platform operator can open a host terminal." | account is not an operator | add the email to `PLATFORM_ADMINS` or grant the admin role |
| "The node was not found or is no longer managed." | server row deleted / node re-registered | reload Servers; re-register the agent |
| "This node agent does not support terminal sessions." | old agent | update the node agent |
| "The node agent is unavailable." | CP cannot dial `node:9443` | check `GOTHAM_AGENT_LISTEN_ADDR`, firewall, `servers.ip` |
| "This node agent needs an update to open a terminal without a folder." | pre-JUS-253 agent | update the node agent |
| "The working directory is not allowed." | `cwd` escaped the FileService root, or a symlink leaves it | pick a folder inside the root |
| "That folder no longer exists on the server." | folder deleted after listing | refresh the File manager |
| "The terminal session quota is reached." | per-operator/node limit | close a session |
| "This application has no running deployment." | nothing to exec into | deploy or start it |
| "The selected container is not running." / "… no longer available." | stopped or replaced container | "Refresh services" then pick the running one |
| "A deployment is in progress." | container being replaced | wait for the deploy, then retry |
| "You need write access to this application's team…" | read_only team role | ask a team admin |
| "Container terminals are disabled on this control plane." | feature flag off | enable applications |
| "Your session has ended. Sign in again…" | token/session revoked | sign in |

Log lines worth knowing:

| Line | Level | Meaning |
|---|---|---|
| `terminal: session failed session_id=… reason=…` | INFO | the relay ended by itself (agent loss, refusal); the reason is bounded |
| `terminal: session scan incomplete; falling back to the process group` | WARN | the cleanup walk could not see every process (cap, deadline, `hidepid`, namespace); the group fallback and the found members were still handled |
| `terminal: cleanup incomplete` / container cleanup error with a container id | ERROR | positive evidence a container helper failed; investigate the node (Docker/exec, missing `tr`/`grep`, `/proc` unavailable) |
| `terminal: revalidation failed` (repeated) | WARN | the CP cannot confirm the sign-in; after ~90 s the shell fails closed |
| `terminal: pty master release incomplete` | WARN | the master could not be closed within the bound; report with the session id |

## Known limits found during validation

* **JUS-257 (open):** a shared-team application detail page answers 404 for
  every member because `getApplication` sends no `X-Team-Id` while the backend
  defaults to the personal team. This blocks the UI half of the read-only
  scenario (a `read_only` role can only exist in a shared team); the terminal
  permission boundary is still proven at the API level (targets/create 403).
  The browser test auto-skips with the ticket reference until it is fixed.
* **JUS-256 / PR #405:** Compose terminals resolved 12-hex CLI ids, which the
  agent rejects. The browser Compose test detects that state and skips with the
  ticket reference; with the fix merged it asserts a full 64-hex id and a real
  shell.
* **Headless and background tabs.** A background browser tab does not run
  `requestAnimationFrame`; xterm rendering and Naive UI transitions can look
  frozen. This is a harness artifact, not a terminal bug. The Playwright suite
  keeps the tab foregrounded.
* **Synthetic text input.** Playwright's `keyboard.type`/`insertText` does not
  drive xterm. The suite dispatches CDP `Input.dispatchKeyEvent` for BMP
  characters and a real `ClipboardEvent` for paste; astral characters (emoji)
  are covered through paste, not synthetic key events.
* **Banner size.** The full 19-line logo needs a pane at least 63 columns wide
  and 23 rows tall; narrower or shorter panes show the compact two-line banner
  by design. The browser test pins a desktop viewport for the full logo and
  checks the committed asset's SHA-256 (`76a2f2e1…74`) plus every rendered line.
* **Node-local differences.** On a non-Linux developer machine the agent has no
  `/proc` session walks and macOS `lsof` replaces `/proc/<pid>/fd` for the leak
  assertion; the release target is Linux. The fd/process regression is asserted
  on Linux CI and locally where the tooling exists.
* **Old firmware/busybox.** Busybox `ash` emits no prompt marks, so shortcut
  suggestions are unavailable in minimal containers even though the shell works.
