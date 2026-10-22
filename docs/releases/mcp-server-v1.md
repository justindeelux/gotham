# MCP server v1 — release notes (unreleased)

Gotham adds a local stdio MCP server, `gotham mcp serve`, that an agent client
can launch to inspect and operate one team's resources through the existing
REST authorization model. It ships inside the existing `gotham` binary for
Linux amd64 and arm64; no new runtime, npm package, network listener or release
platform is added.

## Added

- 18 typed MCP tools in five groups: observation (4), discovery (6), logs (2),
  creation (3) and deployment (3).
- Protocol-level coverage of initialize, list and call over the real stdio
  process, including strict JSON schemas, read/write annotations and structured
  results.
- A backed workflow proof: inspect a node, create a project and staging
  environment, create public Git and image applications, deploy, poll the exact
  deployment ID, read persisted build and live runtime logs, and separate
  pipeline from runtime state. A failed build is covered too.
- Operator documentation in [`../mcp.md`](../mcp.md): client configuration,
  supported hosts, tool bounds, request-ID recovery, fixed team context, source
  prerequisites, retained resources and log confidentiality limits.

## Compatibility and limits

- Requires a control plane that implements the strict MCP contract. Older
  backends fail startup closed.
- `GOTHAM_MCP_URL`, `GOTHAM_MCP_TOKEN` (`gt1_`) and `GOTHAM_MCP_TEAM_ID` are the
  whole configuration; no per-tool overrides exist.
- Read scope is required for reads; deploy scope plus an owner/admin team role
  is required for mutations. Read-only membership and cross-team reads are
  refused by the backend, not merely hidden by the adapter.
- Replays are guaranteed for 24 hours with tombstones for 7 days; after expiry,
  reconcile manually. A canceled poll never cancels a deployment.
- Log reads are bounded snapshots. Masking is best effort and arbitrary emitted
  secrets may remain; treat log text as confidential.
- No file, shell, terminal, delete, rollback, node-provisioning or autonomous
  monitoring tools are included in v1.

## Rollback

Remove this documentation and its tests only. The shipped MCP contract remains
unless a separate rollback disables MCP. Do not delete resources that real users
created while the feature was live.
