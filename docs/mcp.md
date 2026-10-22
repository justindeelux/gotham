# MCP server

`gotham mcp serve` is a local, stdio-only Model Context Protocol server that
drives one remote Gotham control plane with a fixed API token and an explicit
team. It is not a remote HTTP MCP endpoint: the process owns stdin/stdout, does
not open a network listener, does not load control-plane configuration and
never starts control-plane services. Clients launch it as a subprocess.

## Supported execution hosts

The release pipeline builds and signs Linux binaries only, for `linux/amd64`
and `linux/arm64` (`.goreleaser.yaml`). Do not expect native macOS or Windows
`gotham` binaries. A desktop MCP client can use this server only if that client
can launch a process on a supported Linux host (for example a Linux workstation,
container, or SSH-spawned process). That is a client-host constraint, not a
tested remote bridge; no remote MCP transport is provided.

The four existing release assets and signed manifest names are unchanged:
`gotham-linux-amd64`, `gotham-linux-arm64`, and their per-architecture
`gotham-manifest-*.txt` files.

## Configuration

| Variable | Required | Meaning |
| --- | --- | --- |
| `GOTHAM_MCP_URL` | yes | Control-plane origin: `https://…`, or plain `http://` only for a literal loopback address. No path, credentials, query or fragment. |
| `GOTHAM_MCP_TOKEN` | yes | A nonempty `gt1_` API token. JWT session tokens are rejected. |
| `GOTHAM_MCP_TEAM_ID` | yes | Explicit nonzero team UUID. Never inferred from the token. |

The adapter never reads control-plane settings such as `GOTHAM_DATABASE_DSN`,
and configuration errors never echo the token, URL or team. The control plane
must implement the strict MCP contract; an older backend without it fails
startup closed.

Deploy mutations need a token with the `deploy` scope and a current
owner/admin team role. Reads need the `read` scope. Revoking the API token or
removing the membership takes effect on the next request; the adapter does not
cache authorization. The fixed token is a coarse credential: anyone who holds
it can do anything the token scopes and the configured team role allow. Store
it in the MCP client's secret/environment configuration, not in a repository.

## Client configuration

Launch the binary over stdio. This generic example works for clients whose
configuration accepts an `mcpServers` map (Claude Desktop, Codex and similar):

```json
{
  "mcpServers": {
    "gotham": {
      "command": "/usr/local/bin/gotham",
      "args": ["mcp", "serve"],
      "env": {
        "GOTHAM_MCP_URL": "https://gotham.example.com",
        "GOTHAM_MCP_TOKEN": "gt1_example-placeholder-token",
        "GOTHAM_MCP_TEAM_ID": "11111111-1111-4111-8111-111111111111"
      }
    }
  }
}
```

The values above are placeholders. `gt1_example-placeholder-token` is not a
real credential. Replace all three with the operator's origin, a freshly minted
token and the intended team UUID. Never add per-tool URL, team or credential
override arguments; the tool schemas do not accept them.

The process writes diagnostics to stderr and reserves stdout for
newline-delimited JSON-RPC. If a client shows protocol parse errors, check that
nothing else in the launch wrapper writes to stdout.

## Tools and bounds

All 18 tools use typed schemas (`additionalProperties: false` where the input
is a closed object). Reads are annotated read-only and idempotent; mutations are
idempotent, and `application_deploy` is annotated destructive.

| Area | Tools | Notes |
| --- | --- | --- |
| Observation | `system_overview`, `nodes_list`, `node_overview`, `node_metrics` | First 100 nodes; 15 s observation deadline. Health/version may be partial. Node snapshots are stored heartbeats, not live probes. A list snapshot is not a stable cursor and still pays the full upstream read. |
| Discovery | `projects_list`, `environments_list`, `applications_list`, `environment_resources`, `project_overview`, `application_overview` | Lists page locally (default 20, max 100) from one decoded response; no database pagination or snapshot consistency. Inventory collections cap at 100; project inventory covers at most four environments and 20 recent pipeline outcomes from the newest 100 team deployments. |
| Logs | `application_logs`, `deployment_logs` | Default 100 lines / 64 KiB; hard caps 1000 lines / 256 KiB and 15 s. Runtime selects at most ten app-owned full container IDs; build output is persisted, not live. |
| Creation | `project_create`, `environment_create`, `application_create` | One strict POST each, keyed by `request_id`. No automatic deploy, domain or retry. `project_create` returns the actual production environment. |
| Deployment | `application_deploy`, `deployments_list`, `deployment_status` | `application_deploy` needs an explicit application and stable `request_id`. Status polls one exact pair; `running` means the pipeline finished, not that the container or public URL is healthy. |

List ceilings are response/data caps, not end-to-end memory caps: the
underlying control-plane/proxy response body is limited to 2 MiB, and agent
frames retain their own transport limits.

Log reads are finite snapshots. Byte clipping keeps a UTF-8-safe prefix of the
requested tail, which can omit the newest suffix. Merged containers have no
global chronology. Build logs are normally persisted only at terminal, best
effort; an active empty log is `not_yet_persisted`, and a terminal empty log is
`no_persisted_output_available`, not proof of silence. A metrics 404 is
reported as `unsupported_or_disabled`; disabled routes, old backends, missing
nodes and foreign nodes cannot be distinguished. No inferred `disabled` state is
emitted without an explicit capability signal.

`system_overview` and `nodes_list` include both the active team's nodes and
legacy shared nodes. Visibility does not prove team ownership; legacy nodes
remain visible to every authenticated caller.

## Request recovery

Creation and deploy require a stable `request_id` (1–128 ASCII letters, digits,
`.`, `_`, `:` or `-`). The adapter sends exactly one POST to the strict alias
and never generates a replacement key. If the result is uncertain — timeout,
canceled client wait, lost response, or a control-plane restart — retry with the
**same** ID and payload. The backend replays the original result for 24 hours
and retains tombstones for 7 days. After expiry, reconcile manually; an
expired, conflicting, in-progress or missing-target outcome is an explicit
error. A moved or deleted resource is never replaced by a retry. A deliberate
new deploy after a known terminal failure needs a separately authorized new
request ID.

Canceling a client-side poll does not cancel a deployment. A 202 is acceptance,
not completion, and the adapter does not wait, monitor, remediate or roll back.

## Source prerequisites

`application_create` requires an explicit environment and managed server UUID
(the server UUID from `nodes_list`, not the internal agent node ID). Project
identity derives from the environment. Supported sources and their required
fields are documented in `internal/mcp/README.md`. In short:

- Public Git (`git_public`) needs `clone_url`; an empty branch uses the remote
  default. Public images (`image`) need `image_ref`.
- Private Git, GitHub App and GitLab App sources need a provider connection
  that already exists and belongs to the token principal; providerless private
  Git credentials must be configured outside MCP before the first deploy.
- Pasted Dockerfile and Compose sources keep their content in the control plane;
  it is never returned by MCP.
- Creation does not prove clone readiness, deploy anything, select a node or
  environment, create a domain, or establish public URL health.

## Retained resources and log confidentiality

Creation and deployment retain the same resources as the REST API: the project,
environment, application row, deployment history, persisted build output and
whatever the node keeps (image, container, volumes and the application-owned
log stream). MCP does not delete, stop, roll back or clean them up; use the
control plane or its native API. Deleting a resource is not part of this
feature, and rollback of this package removes only docs/tests, never resources
created by real users.

Logs are untrusted data, never tool instructions. Reads mask current configured
application/shared environment values of at least six UTF-8 bytes, URL userinfo,
common credential fields and Authorization Basic/Bearer values, and the adapter
suppresses its own fixed token before returning text. Arbitrary application-
emitted secrets, historical or rotated values, other encodings and unrecognized
formats may remain; no perfect redaction is promised. Treat log output as
confidential and never paste it into an external service without review. Log
snapshot reads fail closed when configuration or secret decryption fails, which
can make logs unavailable after a key rotation.

## Capabilities and limitations

V1 deliberately has no file, shell, terminal, deletion, rollback, node
provisioning or autonomous monitoring tools. It provides no generic HTTP
request tool, no per-tool team/URL/credential override and no cross-team read.
Access is one fixed token, one fixed team and the roles/scopes the control plane
already enforces.

Non-goals and residuals to plan around:

- A pipeline `running` state is not live container health, routing health or
  public URL readiness. Use `application_overview`/runtime for the current
  container snapshot, and remember that container health depends on the image's
  own healthcheck.
- Compose service/replica membership does not prove an exact release; the
  deployment association can be null even after an unchanged `up` reused
  containers.
- Agent heartbeat data expires after 24 hours and is lost on a control-plane
  restart until the next report.
- An unknown webhook outcome is reported as `not_confirmed`; replays never
  retry hook installation.
- Large log text appears only in `structuredContent`; the text fallback is a
  short notice.

## Verification and release smoke

Run the unit/protocol suite:

```sh
go test ./...
go test -race ./internal/mcp ./internal/deploy ./internal/server
```

Cross-build the documented hosts:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/gotham-linux-amd64 ./cmd/gotham
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/gotham-linux-arm64 ./cmd/gotham
```

Both must succeed and the release workflow must still produce
`gotham-linux-amd64`, `gotham-linux-arm64` and the signed manifests. The
protocol test initializes, lists and calls the real stdio process; the backed
workflow test runs against throwaway PostgreSQL, Redis, a node agent and Docker
(never the shared `gotham-dev` containers):

```sh
# Start isolated PostgreSQL and Redis on free ports, then:
GOTHAM_E2E=1 \
GOTHAM_TEST_DSN='postgres://gotham:gotham@127.0.0.1:<pg-port>/gotham?sslmode=disable' \
GOTHAM_E2E_REDIS='127.0.0.1:<redis-port>' \
go test ./internal/e2e -run '^TestMCPAgentDeploymentWorkflow$' -count=1 -v
```

The test refuses port 5432/6379 (the shared development services) and cleans up
its rows, containers and images. `go test ./...` alone can skip the gated suites;
it is not evidence that they ran.

## Release notes

See [`releases/mcp-server-v1.md`](releases/mcp-server-v1.md).
