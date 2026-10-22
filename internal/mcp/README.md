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

Observation tools register through `registerTools` using typed SDK handlers and
explicit safe DTOs; there is no generic HTTP request tool. `Client.readJSON` enforces the team contract on every resource
read. `Client.Health` first authenticates strict context, then decodes dependency
health, including degraded HTTP 503 responses. All requests revalidate the token
at the backend; the adapter never retries or follows redirects.

Strict requests send `X-Gotham-MCP-Contract: 1` and `X-Team-Id`.
`GET /api/v1/mcp/context` requires read scope, enabled/wired teams, and current
membership. Strict resource reads require the same membership checks, preserving
legacy behavior for requests without the opt-in header. Successful strict
responses acknowledge `X-Gotham-MCP-Contract: 1` and
`X-Gotham-MCP-Team-Id: <configured UUID>` before data can be projected. A backend
without this contract fails closed. Context advertises only `context` and
`strict_reads`; no mutation capability or mutation fallback exists in S1.

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
