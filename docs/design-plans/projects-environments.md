# Projects and environments — UI brief

Audience: the design agent. What each screen must let a user do; no backend or delivery detail.
Mockups: `docs/design/projects.html`, `project-detail.html`, `environment.html` (first drafts).
Plan: `docs/plans/13-projects-environments.md`.

## Mental model the UI must teach

A **project** is one product. It has **environments** (production, staging, …). An environment
holds **resources**: applications, services, databases. Every resource runs on exactly one
**server** that the user picks. Servers are shared by all projects.

The sidebar entry **Projects** replaces Applications, Services and Databases.

## Screens

### 1. Projects (`/projects`)
- See every project as a card: name, description, environment names, resource counts per type, a
  one-line health summary (all running / N failed).
- Search by name. Create a project (name required, description optional). A new project starts
  with a `production` environment; say so in the dialog.
- Empty state: invite the user to create the first project.
- Viewers see the list but no create button.

### 2. Project (`/projects/:project`)
- Breadcrumb `Projects / <project>`. Rename the project; delete it (disabled with an explanation
  while any environment has resources).
- Tab **Environments**: table with resource counts, open, add, rename, delete (blocked with an
  explanation when not empty; the last environment cannot be deleted).
- Tab **Shared variables**: key/value table, secret values masked and write-only (replace, never
  reveal). One line explaining the precedence: application overrides environment overrides project.

### 3. Environment (`/projects/:project/environments/:env`)
- Breadcrumb `Projects / <project> / <environment>`; switch between sibling environments.
- One list of all resources with type, server, status and a primary action; tabs filter by type.
  Preview deployments are hidden by default, with a switch to show them nested under their app.
- **Add resource**: pick type (application, service, database), then the type's existing creation
  flow. Project and environment are shown as a read-only summary with a change link. **Server is a
  required picker** (offline servers disabled with a reason; preselected when there is only one).
- Environment shared variables (same editor as the project tab), with the project ones shown
  read-only above for context.
- Empty state: explain the three kinds of resource and offer Add resource.

### 4. Resource pages (application, service, database)
- Same content as today under the nested URL, breadcrumb `Project / Environment / Resource`.
- Settings gain: **Server** (change; warn that the resource is redeployed, blocked while a deploy
  runs) and **Environment** (move within the team; show a name-collision error inline).
- Application environment-variable editor shows inherited project/environment variables as
  read-only rows with their origin ("from project", "from environment") and lets the application
  override one by adding the same key.

### 5. Template deploy
- The template flow asks for project, environment and server like any other resource, defaulting
  to the project and environment the user came from.

## Cross-cutting
- Every list and form works at phone width; long names truncate with a tooltip.
- Destructive blocks always say why and what to do ("Move or delete the 2 resources first").
- Errors render inline next to the field that caused them (name taken, server offline).
- Status vocabulary and colors are unchanged.
