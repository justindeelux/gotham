---
title: Using Gotham
---

# Using Gotham

This track is for team members who deploy and run workloads in the Gotham web
console. Everything you create belongs to a project and one of its environments,
and every resource runs on a node an operator has registered.

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
- [Teams and notifications](/using/teams-notifications) — members, invites,
  roles and alert channels.

## Roles

A team member holds one of three roles: `owner`, `admin` or `read_only`. There
is no separate `member` role.

`owner` and `admin` can write. `read_only` can read everything but never writes:
the API rejects every mutation from a `read_only` account. The console hides most
write controls, and a few remain visible and rely on that rejection — a visible
button is not a permission.
