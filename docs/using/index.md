---
title: Using Gotham
---

# Using Gotham

This track is for members who deploy and run workloads in the Gotham web
console. Everything you create belongs to a project and one of its environments,
and every resource runs on a node the owner has registered.

There is no top-level Applications, Databases or Services list: a resource is
always reached through its project and environment, for example
`/projects/{project}/environments/{environment}/applications/{id}`.

## Pages

- [Projects and environments](/using/projects-environments) — create a project,
  add or switch environments, add resources and manage shared variables.
- [Applications](/using/applications) — sources, the deploy pipeline, deploy and
  rollback, logs, detail tabs and environment variables.
- [Databases](/using/databases) — engines, credentials, backups and restores.
- [Services and templates](/using/services-templates) — one compose project per
  service, and the template catalog.
- [Domains and previews](/using/domains-previews) — attach hostnames and
  certificates, and review pull-request previews.
- [Members and access](/using/members-access) — the owner and member roles,
  project access and invites.
- [MCP setup](/using/mcp) — connect an AI assistant with a read-only or deploy
  token.
- [Notifications](/using/notifications) — alert channels for one application or
  database.

## Roles

A member works inside the projects the owner granted. There is no per-project
role: inside a granted project a member can read and write everything, while
everything outside the grant is hidden. The owner manages the instance itself —
people, servers, Git connections, global Domain & SSL, updates and the host
tools. See [Members and access](/using/members-access).
