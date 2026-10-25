---
title: MCP setup
---

# MCP setup

MCP (Model Context Protocol) lets an AI assistant such as Claude Code or Cursor
work with your Gotham instance: list projects, servers and applications, read
logs and metrics, and — when you allow it — create and deploy resources. The
client signs in with an API token; your console session is never handed to it.

## Before you start

MCP needs an HTTPS address. If the instance URL is plain HTTP, the setup page
warns that the token and tool output would travel unencrypted. Set an HTTPS
address before connecting a client; plain HTTP is only suitable for a literal
loopback address on the same machine during development.

The owner sets the address in **Instance → General → Control-plane URL**. A
member cannot change it, so ask the owner when the warning appears.

## Create the connection

1. Open **MCP setup** in the sidebar under **Access** (or go to
   `/settings/mcp`).
2. Choose the access level:
   - **Read only** — observe projects, servers, applications and resources.
     Nothing can be created, changed or deployed.
   - **Read and deploy** — everything read only allows, plus the create, change
     and deploy tools. Choose this when the assistant should make changes.
3. Choose **Create token**.
4. Copy one of the configurations shown. The token is displayed once, and the
   snippets keep it only while the dialog is open, so copy before closing.

The setup page never offers the `admin` scope. That scope covers owner-only
instance operations and is not part of the assistant connection. Both the owner
and a member can create a read-only or deploy token here.

The token is named `MCP setup <date>` and appears in **Settings → API tokens**,
where you can rotate or revoke it later.

## What a member's client can reach

A token acts as the account that created it. A member's client reaches only the
projects the owner granted to that member; everything else is hidden, and a
direct request answers as if the resource were missing. Access is checked on
every request, so changing the grants applies immediately.

## Paste the configuration

The page offers three forms, all using the MCP URL `<instance URL>/mcp`.

**Claude Code (terminal)**

```sh
claude mcp add --transport http gotham https://gotham.example.com/mcp --header "Authorization: Bearer <YOUR_TOKEN>"
```

**Cursor and other clients (url + headers)**

```json
{
  "mcpServers": {
    "gotham": {
      "url": "https://gotham.example.com/mcp",
      "headers": {
        "Authorization": "Bearer <YOUR_TOKEN>"
      }
    }
  }
}
```

**Other clients (URL and token)**

```text
URL: https://gotham.example.com/mcp
Token: <YOUR_TOKEN>
```

Any MCP client that supports streamable HTTP can use the raw URL and send the
token as `Authorization: Bearer <YOUR_TOKEN>`.

## Rotate or revoke

Open **Settings → API tokens** from the account menu. **Rotate** replaces the
secret and stops the old one immediately; update the client with the new token.
**Revoke** ends access immediately and cannot be undone. A revoked token gets
`401`, so remove the old entry from the client as well.
