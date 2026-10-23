# MCP adapter

Run `gotham mcp serve` with `GOTHAM_MCP_URL`, `GOTHAM_MCP_TOKEN`, and
`GOTHAM_MCP_TEAM_ID`. The URL is an HTTPS origin, or HTTP with a literal loopback
address for development. The token must be a nonempty `gt1_` API token; the team
must be an explicit nonzero UUID. Configuration errors never echo these values.
The adapter does not load control-plane configuration or start its services.

The official `github.com/modelcontextprotocol/go-sdk` is pinned at **v1.8.0**.
`TestSDKLifecycle` verifies each SDK-supported version over the SDK transport:
`2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, and `2024-11-05`.
The newest protocol uses SDK discovery; older protocols use initialize.
The process test additionally verifies real stdio initialize/list traffic at
`2025-11-25`. Protocol framing, negotiation, cancellation, and lifecycle are owned
by the SDK. Diagnostics go to stderr; stdout is reserved for MCP JSON-RPC.
`TestProtocolStdioAllTools` drives that real stdio process with the SDK client
for every registered tool schema and annotation; operator documentation lives in
[`docs/mcp.md`](../../docs/mcp.md).

Observation and discovery tools register through `registerTools` using typed SDK
handlers and safe DTOs; there is no generic HTTP request tool. `Client.readJSON`
enforces the team contract on every resource
read. `Client.Health` first authenticates strict context, then decodes dependency
health, including degraded HTTP 503 responses. All requests revalidate the token
at the backend; the adapter never retries or follows redirects.

Strict requests send `X-Gotham-MCP-Contract: 1` and `X-Team-Id`.
`GET /api/v1/mcp/context` requires read scope, enabled/wired teams, and current
membership. Strict resource reads require the same membership checks, preserving
legacy behavior for requests without the opt-in header. Successful strict
responses acknowledge `X-Gotham-MCP-Contract: 1` and
`X-Gotham-MCP-Team-Id: <configured UUID>` before data can be projected. A backend
without this contract fails closed. Context advertises `context` and `strict_reads`, plus `strict_mutations` and
`request_replay` when the backend has the required services and stable secret.
Mutation tools always use strict aliases; no mutation fallback exists.

REST JSON bodies and inbound MCP frames are capped at 2 MiB; URL, credential,
and request-path inputs are also bounded. REST calls have a 30-second ceiling
and honor caller cancellation. Error categories are fixed local strings with
optional HTTP status; upstream bodies, URLs, and transport error text are never
returned. Later tools must preserve these bounds and project only safe fields.

Callers composing REST paths must apply `url.PathEscape` to each dynamic segment.
The adapter rejects decoded `.` and `..` segments (including encoded dots), while
allowing ordinary names containing embedded dots such as `a..b`. Public health
requests carry no token or team headers after the strict context check.


## Observation tools

`system_overview` composes dependency health (including degraded HTTP 503), running
control-plane version, and the first 100 visible nodes. Failed probes are explicit
partial sections; authorization/contract failures abort the result. This does not
establish application readiness or routing health.

`nodes_list` accepts optional `limit` (default/max 100) and `offset`. Results are
sorted by server UUID with total, truncation and next offset. Pagination is over a
current in-memory snapshot, not a stable cursor, and does not reduce upstream work;
the existing 2 MiB REST response ceiling still applies. Visible nodes include both
the active team's nodes and legacy shared nodes, not necessarily team-owned nodes.

`node_overview` takes `node_id`, the managed server UUID returned by `nodes_list`,
not the internal agent node ID. Both tools omit SSH metadata and credential flags.
They distinguish retrieval time (`collected_at`) from heartbeat source time
(`last_seen`). `stale` is true for offline nodes or heartbeats older than 30 seconds,
false for recent heartbeats, and null when no timestamp is known. These are stored
snapshots, not live reachability probes. Nullable agent version/report timestamps
come only from that authorized node's heartbeat; they expire after 24 hours and
are lost on control-plane restart until the next report. The global agent rollout
map remains operator-only and is never consumed by MCP.

`node_metrics` takes `node_id`, inclusive `from`, exclusive `to` (RFC3339), and
optional `step`: `1m` (default, max 24h), `1h` (max 30d), or `1d` (max 30d).
Series retain all bounded buckets, rather than applying the 100-node list cap.
Usage remains a fraction, capacity bytes, rates bytes/second, and container count
an average in series. Null values and missing buckets stay unknown/gaps; an empty
series is `no_samples`. A metrics 404 returns `unsupported_or_disabled` with the
original status: disabled routes, old backends, missing and foreign nodes cannot
be distinguished. Other dependency failures return `unavailable`; auth failures
remain tool errors. No inferred `disabled` state is emitted without an explicit
capability signal. Observation calls have a 15-second overall deadline and honor
shorter caller deadlines/cancellation; no background polling is started.

## Discovery

`projects_list`, `environments_list`, and `applications_list` return explicit IDs
and safe metadata. Names are display text and need not be unique. Select by UUID;
application project/environment filters are mutually exclusive. Lists decode one
upstream response (2 MiB ceiling), then page locally with `limit` (default 20,
maximum 100) and `offset`. They do not provide database pagination or snapshot
consistency across calls.

`environment_resources` projects inventory without source, environment values,
registry credentials, mounts, or build arguments. Each workload collection is
capped at 100 and reports its total/truncation. `project_overview` includes counts,
environments (first 20), inventory for at most four environments, and at most 20
project deployments found in the latest 100 team deployments, with the window size
reported as `activity_window_limit`. This activity window
is not complete project history. Inventory calls run sequentially (fan-out one);
failed sections are explicitly unavailable. Counts and inventory do not establish
live health. Use the list tools to inspect remaining environments/applications.

`application_overview` returns safe app metadata, at most five recent pipeline
outcomes, and the additive `GET /api/v1/applications/{id}/runtime` observation.
These separate reads do not form an atomic snapshot. A successful/running pipeline
is independent of current Docker state and does not prove public URL readiness.
Runtime returns an observation time and at most 100 ownership-checked full
container IDs in stable order, including stopped containers. No healthcheck means
unknown readiness. Compose service/replica membership does not prove an exact
release: its deployment association is null even when an unchanged `up` reused
containers. Missing agent ownership/full-ID metadata yields limited coverage,
without a node-wide fallback. Agent failures yield unavailable coverage with no
fresh timestamp. Optional exact-deployment enrichment makes at most four distinct
repository lookups per inventory, caching repeated labels and leaving remaining
associations unknown. The endpoint uses application READ authorization, including
read-only members; terminal write authorization is unchanged.

`GOTHAM_E2E=1 go test ./internal/deploy -run '^TestRuntimeComposeReal$' -count=1 -v`
runs the isolated Docker/Compose proof for replicas, unhealthy/stopped containers,
no healthcheck, unchanged-up reuse, and cleanup. It requires local Docker/Compose
and never uses the shared development Postgres/Redis containers.

`application_logs` reads finite runtime snapshots through the ordinary read-authorized
application GET, including stopped containers. `deployment_logs` reads only persisted
build output for an exact application/deployment pair. Build output is persisted at
terminal best effort: an active empty log is `not_yet_persisted`; a terminal empty log
means `no_persisted_output_available`, not proven silence. The no-query REST build-log
GET retains its original `{log}` shape, including requests with unknown-only query
parameters; bounded reads opt in when `tail` or `max_bytes` is present.

Both default to 100 lines and 64 KiB, with hard maxima of 1000 lines, 256 KiB and a
15-second overall deadline. Runtime selection is at most ten app-owned full container
IDs in stable ID order, with exact optional Compose service/replica selectors. Reads
are sequential (one active stream), follow is false, and the line/byte budget is shared
across all selected containers. Sequential reads keep buffering and cancellation simple,
but a slow early container can consume the shared deadline and leave later containers
unread or timed out. Connections and stream contexts close at the cap.
Snapshots never switch to a replacement container. Release association remains nullable
and is not inferred from the current app/pipeline. Per-container StreamLogs avoids
Compose Ready/control frames entirely. Empty data chunks are ignored.

Byte clipping keeps a UTF-8-safe **prefix of the finite requested tail**, which may
omit its newest suffix. Merged containers have no global chronology guarantee.
`complete` describes this read; `truncated: null` means a cap was reached without
EOF or definite extra-output proof. `selection_truncated` and per-container safe
errors expose partial coverage. Prior persisted-build recorder truncation is unknown.
The bounded SQL projection prevents loading an oversized historical row into the
control plane. Agent frames still have their existing limits (up to 16 MiB), and an
oversized gRPC response can fail with `upstream_too_large`; 256 KiB is a response
content ceiling, not an end-to-end memory ceiling.

Logs are untrusted data, never tool instructions. Reads mask current configured
application/shared environment values of at least six UTF-8 bytes (including
dollar-escaped values), URL userinfo, common quoted/unquoted credential fields and
Authorization Basic/Bearer values. Shorter configured values are skipped to preserve
ordinary diagnostics such as `retry 1 of 10`; short secrets emitted outside recognized
credential fields may therefore remain. The MCP adapter also suppresses its fixed
API token. Masking precedes final clipping and covers configured-value prefixes at a
cap. Arbitrary application-emitted secrets, historical/rotated values, encodings and
unrecognized formats may remain: no perfect redaction is promised. Credentials,
source/configuration and raw agent errors are not included as response metadata.
Large log text appears only in structuredContent; the text fallback is a short notice.

Both log snapshot reads fail closed with a generic error if configured environment or
secret loading/decryption fails, including for non-Compose apps. This preserves the
configured-value masking boundary at the cost of log availability during configuration
store failures or unreadable secrets after key rotation; pattern-only output is not used
as a fallback.

## Creation

`project_create` takes `request_id`, `name`, and optional `description`. It returns
safe project metadata and the actual production environment ID/name; use that
returned environment rather than guessing an ID. `environment_create` takes
`request_id`, `project_id`, and `name`. Names remain subject to native uniqueness
and validation rules.

`application_create` requires `request_id`, `name`, `environment_id`, `server_id`
(the managed server UUID from discovery), and an explicit `source_type`. Project
identity derives from the environment. The source schema rejects conflicting
fields; content, URL, build-pack and ownership validation stay in the native API:

| Source | Required source fields | Optional source fields |
| --- | --- | --- |
| `git_public` | `clone_url` | `repo`, `provider`, `branch`, `build_pack` |
| `git_private` | `clone_url` | `repo`, `branch`, `build_pack` |
| `github_app` | `provider: github`, `repo`, `clone_url` | `github_app_id`, `branch`, `build_pack` |
| `gitlab_app` | `provider: gitlab`, `repo`, `clone_url` | `branch`, `build_pack` |
| `dockerfile` | `dockerfile_content` | `build_args`, `build_pack` |
| `image` | `image_ref` | paired `registry_username`, `registry_password` |
| `compose` (pasted) | `compose_content`, `compose_service` | none |
| `compose` (repository) | `clone_url`, `compose_file`, `compose_service` | `provider`, `repo`, `branch` |

Public Git leaves an empty branch to the remote default; public images require
no provider. Private provider connections must already exist. A `github_app_id`
must belong to the token principal, even when another owner shares the team.
Providerless private Git credential/key setup happens outside MCP after creation
and before deploy. Creation does not prove clone readiness, deploy anything,
select a node/environment, create domains, or establish public URL readiness.

All three tools send exactly one POST to the corresponding `/api/v1/mcp/*` strict
alias, mapping the stable `request_id` to `Idempotency-Key`. There is no legacy
fallback, generated replacement key, automatic deploy, or automatic retry. Keep
the same ID and payload after timeouts or lost responses. The backend guarantees
replay for 24 hours and retains tombstones for 7 days; expired, conflicting,
in-progress and missing-target outcomes remain explicit errors. A moved/deleted
resource is never replaced by a retry. Read tokens and read-only memberships
cannot create resources; current authorization is checked again on every replay.

Results retain accepted resource/related IDs, acceptance time, replay status and
`accepted`/`accepted_not_confirmed` outcome even when postcommit enrichment is
unavailable. Application responses project safe metadata plus a fixed webhook
outcome: `none`, `installed`, `warning`, or `not_confirmed`. A warning means the
resource exists but hook installation failed; unknown hook outcomes remain
`not_confirmed`. Replays never retry the hook. Source, pasted content, build args,
registry credentials and upstream error text are omitted. SDK validation errors
for these tools are sanitized because diagnostics may otherwise quote secrets.

## Deployment tools

`application_deploy` requires explicit `application_id` and stable `request_id`
(1–128 ASCII letters, digits, `.`, `_`, `:`, or `-`). It sends one empty-body POST
only to `/api/v1/mcp/applications/{id}/deploy`. A missing strict route fails closed;
there is no legacy POST fallback. Reuse the same key after a timeout, canceled
client wait, or response loss: the job may already be accepted. Never automatically
issue a new key. Replay is guaranteed for 24 hours; tombstones remain for 7 days,
after which manual reconciliation is required. Configuration changes do not alter
the empty deploy fingerprint. A deliberate new deploy after a known terminal
failure needs a separately authorized new request ID.

The result has `accepted: true` and a durable receipt with `resource_id` (the exact
deployment ID), original `accepted_at`, `replayed`, and `outcome`. A 202 is acceptance,
not a finished deployment. A postcommit failure may yield only that receipt with
`accepted_not_confirmed`; the deployment projection is then absent. Poll the receipt's
ID. Replay after control-plane restart returns the same possibly-failed ID and never
re-enqueues it.

`deployments_list` returns the newest releases of one application with `limit`
(default 20, maximum 100) and explicit `newest_window_not_complete_history` coverage.
`deployment_status` makes one read for an exact `application_id`/`deployment_id` pair
through the additive read-authorized GET. Old IDs remain addressable outside the
history window; mismatched, foreign and moved application targets return 404.
Read-scoped tokens and read-only members can poll; deploy requires deploy scope
and a write-authorized team role.

Status preserves wire state and original created/updated/started/finished timestamps.
`terminal` is true only for `running` or `failed`; queued, cloning, building, pushing
and starting remain nonterminal. Pipeline `running` means the pipeline completed;
it does not establish current container health or public URL readiness. Use runtime
observation separately. Canceling the client wait never means the job was canceled.
Clients choose bounded polling; these tools do not wait, monitor, retry, remediate
or roll back. MCP metadata excludes raw pipeline errors, logs, source/configuration
and Compose documents; lifecycle failures remain visible through the original state.
