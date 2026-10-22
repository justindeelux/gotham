---
title: Projects and environments
---

# Projects and environments

A project groups the applications, services and databases that make up one
product. Each project has one or more environments — `production`, `staging`,
and so on — and every resource in an environment is pinned to one node.

## Create a project

Open **Projects** and choose **New project**. Give it a name (1–64 characters,
unique within the team) and an optional description.

A **production** environment is created together with the project, so the new
project is immediately usable. Open it to see its environments and their
resource counts.

## Rename or delete a project

On the project page, **Rename** changes the name and **Delete project** removes
it. A project that still holds resources cannot be deleted: move or delete every
resource in every environment first. Deleting removes the project, all of its
environments and its shared variables, and cannot be undone.

## Environments

The project page lists each environment with counts of its applications,
services and databases. From there you can:

- **Add environment** — the name must be unique within the project
  (1–64 characters).
- **Open** an environment.
- **Rename** or **Delete** an environment. An environment that still holds
  resources cannot be deleted; the dialog names what is in the way.

Environments are independent: resources and environment-level variables belong
to one environment and are not shared with its siblings.

An environment page keeps its project in the address:
`/projects/{project}/environments/{environment}`. The page shows the project
and environment in the breadcrumb and lists everything that runs there.

## Switch sibling environments

When a project has more than one environment, the environment page shows an
**Environment** selector. Choosing a sibling moves you to the same view in that
environment; the project stays the same. Use it to compare a release between
`production` and `staging`, for example.

## Resources in an environment

The resource table has four tabs: **All**, **Applications**, **Services** and
**Databases**. Each tab shows a count next to its name.

The table has four columns: **Name**, **Type**, **Server** and **Status**.
Selecting a name opens the resource. The search field matches a resource name,
its detail line or its node.

Status is reported per resource: `running`, `deploying`, `not deployed`,
`creating`, `failed`, `error` or `unknown`. For applications it reflects the
newest deployment; preview deployments appear with a **Preview** tag.

**Show previews** lists pull-request preview deployments in the table, nested
under the application they belong to. Previews run in the same environment and
on the same node as their base application.

## Add a resource

**Add resource** opens a picker with three tabs:

- **Application** — choose a source (public or private git, a connected GitHub
  or GitLab account, a Dockerfile, a compose document or a container image) and
  continue into the application wizard.
- **Service** — choose a template from the catalog, or import a Docker Compose
  file.
- **Database** — choose an engine to provision.

The picker already knows the project and environment you opened it from, and
the rest of the wizard keeps that scope. **Add resource** is a write action: it
is not shown to `read_only` accounts.

See [Applications](/using/applications),
[Services and templates](/using/services-templates) and
[Databases](/using/databases) for what happens after you pick a type.

## Shared variables

Variables can be defined at two shared scopes and one application scope:

- **Project → Shared variables** applies to every resource in every environment
  of the project.
- **Environment → Variables** (the button in the page header, or the section
  below the resource table) applies to every resource in that environment.
  Project rows are shown read-only above the editor for context.
- **Application → Environment** applies to one application only.

The most specific value wins: application overrides environment, and
environment overrides project. The same rule is shown in each editor.

A variable can be marked **secret**. A secret value is write-only: it is stored
sealed, the API never returns it, and saving an unchanged secret row keeps the
stored value. **Save** replaces the whole collection, so removing a row removes
that variable from the scope.

## Roles and permissions

`owner` and `admin` can create, rename, delete and edit projects, environments,
resources and shared variables. `read_only` can open and read all of it but
never writes.

The API is the enforcement point: a write from a `read_only` account is rejected
server-side even when a control is still visible in the console. There is no
`member` role.
