---
title: API tokens
---

# API tokens

API tokens are bearer credentials for automation: CI jobs, scripts, and
integrations that call the same `/api/v1` API the web console uses. A token
belongs to the account that created it, and every request acts as that account
within the token's scopes.

Tokens start with `gt1_`, which is how the server tells them apart from a JWT.
The plaintext is random, returned once when the token is created or rotated, and
only its SHA-256 hash is stored. Send it in the standard header:

```http
Authorization: Bearer gt1_<token>
```

There is no console page for tokens yet. They are created, listed, rotated and
revoked through the API itself, using either a signed-in session or an existing
token.

## Scopes

| Scope | Grants |
|---|---|
| `read` | Read-only access: `GET` and `HEAD` on the resource API. |
| `deploy` | Everything `read` grants, plus writes (every non-`GET`/`HEAD` method) and secret-bearing reads. |
| `admin` | Everything `deploy` grants, plus platform-global operations: team management, the node file manager, global proxy/DNS management, and applying updates. |

The scopes are hierarchical: `admin` covers `deploy`, and `deploy` covers
`read`. A token created without `scopes` (an omitted field or an empty list) gets
`read`; the JSON body itself is still required.

Scope enforcement is the read/deploy method rule on team-scoped resources:
`GET`/`HEAD` needs `read`, every other method needs `deploy`. Reads that return
secrets are deploy-gated even though they are `GET` requests:

- `GET /api/v1/databases/{id}/credentials` — decrypted database and root
  passwords.
- `GET /api/v1/servers/{id}/files/content` — file bodies, which can include
  `0600` environment files. The whole file manager additionally requires the
  `admin` scope and platform-operator access.

## Endpoints

| Method and path | Purpose |
|---|---|
| `GET /api/v1/tokens` | List the caller's tokens. Metadata only, never the secret. |
| `POST /api/v1/tokens` | Create a token. Returns the plaintext once. |
| `POST /api/v1/tokens/{id}/rotate` | Revoke the token and issue a replacement with the same name and scopes. |
| `DELETE /api/v1/tokens/{id}` | Revoke the token. Answers 204. |

A token descriptor contains `id`, `name`, `scopes`, `last_used_at`,
`revoked_at` and `created_at`. The list endpoint never returns `token` or the
hash. `last_used_at` is updated on each authenticated request on a best-effort
basis.

## Create

```sh
curl -X POST https://<control-plane>/api/v1/tokens \
  -H "Authorization: Bearer <session-or-token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"ci-deploy","scopes":["deploy"]}'
```

The response (`201`) carries the only copy of the secret:

```json
{
  "id": "<uuid>",
  "name": "ci-deploy",
  "scopes": ["deploy"],
  "token": "gt1_<token>",
  "created_at": "<timestamp>"
}
```

Rules enforced on create:

- `name` is required and at most 64 characters.
- A token can only grant scopes it already holds: a `read` token cannot mint a
  `deploy` token, and a `deploy` token cannot mint an `admin` token.
- Only a platform operator can create (or rotate) an `admin`-scoped token. A
  session holds every scope, but the operator check still applies to `admin`.

## Rotate and revoke

Rotation immediately revokes the old secret and returns a new one with the same
name and scopes:

```sh
curl -X POST https://<control-plane>/api/v1/tokens/<id>/rotate \
  -H "Authorization: Bearer <session-or-token>"
```

Revocation is permanent and answers `204`:

```sh
curl -X DELETE https://<control-plane>/api/v1/tokens/<id> \
  -H "Authorization: Bearer <session-or-token>"
```

An unknown, already-revoked, or someone else's token answers `404`. A revoked
token no longer authenticates: requests that present it get `401`. When the
caller is itself an API token, it must hold every scope carried by the token it
manages; a session skips that scope check. Update the stored secret immediately
after rotating.

## Operational notes

- Tokens do not expire. Treat the plaintext as a password: keep it out of shell
  history and repositories, and rotate on a schedule you control.
- Deleting the owning account or revoking a token ends its access immediately;
  nothing is cached across requests.
- A token is not a session. Account settings, password changes, session
  management and two-factor authentication are interactive-session only; use a
  token solely for the automation that needs it.
- The console has no token screen yet, so the API is the only management
  surface; this page is the reference for it.
