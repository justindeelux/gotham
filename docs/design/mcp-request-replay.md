# Replay-safe create and deploy

The existing project, environment, application-create and manual-deploy POSTs accept
an optional `Idempotency-Key`. Their no-header behavior and response shapes are
unchanged. MCP clients must use only these strict aliases:

- `POST /api/v1/mcp/projects`
- `POST /api/v1/mcp/projects/{id}/environments`
- `POST /api/v1/mcp/applications`
- `POST /api/v1/mcp/applications/{id}/deploy`

Aliases require an API token with deploy scope, `X-Gotham-MCP-Contract: 1`, an
explicit nonzero `X-Team-Id`, current owner/admin membership, and a key matching
`[A-Za-z0-9._:-]{1,128}`. They reuse the ordinary handlers and domain validation.
The authenticated context advertises `strict_mutations` and `request_replay` when
persistence is wired. Applications must also be enabled for application routes.
An older control plane returns 404: never fall back to a legacy mutation path.

## Identity and canonical input

A receipt is scoped to the token's user principal, active team, operation, route
target and opaque key. Project/application collection creates have a zero route
target; environment creates target the parent project, and deploy targets the
application. Different tokens owned by the same user share this namespace.
Different principals or teams do not share receipts. Application environment/node
assignment remains part of its meaningful input. Reusing a key for changed input
returns HTTP 409 with code `request_conflict`.

Decoded, validated names/descriptions and application identifiers are trimmed as
in ordinary creation. Omitted description and empty description are equivalent.
Application source/branch defaults and cleared Dockerfile/Compose repository
fields follow existing domain normalization. Empty and omitted collections are
equivalent; build-argument maps are ordered by JSON encoding, environment entries
by normalized key and storage entries by name. Secret-prefix whitespace and
registry credential whitespace follow existing validation. Managed storage paths
are canonicalized with a fixed placeholder ID before the real UUID is generated.
Text/config values otherwise retain their meaningful bytes. Secret-bearing app
input uses a versioned HMAC-SHA256 digest with the configured server encryption
key, before secret encryption and UUID generation. Keep that key stable across
restarts; changing it makes old application fingerprints conflict. No payload,
configuration, source text, credentials or provider error is saved in receipts.
Manual deploy accepts an empty body or `{}`; its fingerprint never incorporates
mutable application metadata.

## Acceptance and retry outcomes

Receipt and resource IDs commit in the resource's actual PostgreSQL transaction,
including the production environment, all application configuration, or queued
deployment. Same-identity requests serialize through a transaction advisory lock
across connections and service instances. A two-second lock wait returns HTTP 409
`request_in_progress` and `Retry-After: 2`: retry the same key and payload. Rollback
leaves neither a resource nor a permanent pending receipt.

Opted-in success responses add a `receipt` with original `resource_id`,
`related_id`, database `accepted_at`, `replayed`, and `outcome`. Related ID is the
production environment for project creation, parent project for environment
creation, and original environment for application/deploy. Safe current resource
metadata may differ on replay. If enrichment/counting/enqueue fails after commit,
a 201/202 containing only the receipt reports `accepted_not_confirmed`; it does
not mean deployment success. Clients must retain the accepted IDs even when the
normal resource envelope is absent. Output/connection loss is recovered by the
same key, never by creating a replacement operation.

Replay is guaranteed for 24 hours from acceptance under current authorization.
Every request rechecks the current token, team membership and write role; replay
also checks the accepted resource's current team/parent assignment and existence.
Deleted, moved or foreign targets return 404, without creating replacements.
Known expired keys return HTTP 409 `request_expired`, even if their input changes.
Minimal identity/fingerprint tombstones do not cascade with resource deletion.
Each successful transactional receipt operation lazily removes at most 100
receipts older than seven days; the store also exposes the same bounded cleanup
for an operator sweep. Cleanup is not a seven-day deletion deadline. Once a
tombstone is removed, the now-unknown key has no deduplication guarantee.

**Never blindly retry an uncertain mutation after 24 hours, automatically choose
a new key, or infer failure from a timeout. Operator reconciliation is required.**

## Postcommit side effects

Only first acceptance enqueues a deployment, after the queued row and receipt
commit. Replay never enqueues or resumes work. The existing restart sweep still
marks nonterminal deployments failed; replay returns the same possibly failed
ID. A crash between commit and enqueue can therefore leave a failed deployment
after restart. This is durable acceptance identity, not exactly-once execution.
Queue rejection and previous-container lookup failures retain that identity and
use the existing failed-deployment/task-event paths. Normal deploy/rollback/system
locks and the active-deployment unique index remain in force.

Only the first successful application-create path attempts the bounded best-effort
provider hook. Its safe `installed`, `warning`, or `none` outcome is saved after
that attempt. A crash/enrichment failure before the attempt, or lost persistence
after the provider accepted it, leaves `not_confirmed`. Replay never installs a
hook to reconstruct its response; known warnings and unknown outcomes are
returned with the accepted application ID. Explicit provider onboarding and hook
retry remain separate operator actions.

Rollback disables the opt-in handlers/aliases and retains migration 00062 and its
receipts. Do not roll back or edit the merged schema migration.
