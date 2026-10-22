# MCP foundation

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

S1 intentionally registers no production tools. Later packages extend
`registerTools` with typed SDK handlers and explicit DTOs; there is no generic
HTTP request tool. `Client.readJSON` enforces the team contract on every resource
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
