# Gotham — Feature Inventory for UI Design

Audience: the design agent that produces/updates page layouts. This document
describes **what the product does and what each screen must let a user do**.
It does not prescribe visuals.

Context:

- This is a **fresh design direction**: do not reuse or match the previous
  mockups, CSS or visual style. Layout, visual language, and component look are
  open for redesign.
- Implementation target is Vue 3 + Naive UI (themeable); UI copy is **English**.
- Existing code (`web/src/pages`, `web/src/components`) is useful only to
  understand current behaviour, not visual style.

Status legend: **Live** = implemented in the SPA · **Stub** = sidebar entry /
placeholder only, no backend UI yet · **Partial** = works, with a known
"backend pending" area shown honestly.

---

## 1. Product summary

Gotham is a **self-hosted PaaS** (a Coolify-style platform). An operator installs
one control plane (CP) and connects one or more Linux servers ("nodes") that run
a lightweight agent. From the web UI they:

1. register servers,
2. deploy applications from Git repositories,
3. run managed databases with backups,
4. run Docker Compose services and one-click templates,
5. route custom domains with automatic HTTPS,
6. collaborate in teams and get notified of deploy/backup results,
7. watch server health and update the platform itself.

Primary persona: a solo developer or small team (operators/devs, comfortable
with DevOps terms). Typical session: "deploy this repo", "why did the deploy
fail", "is my server healthy", "add a domain".

Design priorities: dense-but-calm operational UI, strong **status legibility**
(running / failed / pending at a glance), live logs, destructive actions that
are hard to trigger by accident, and honest empty/error/"not available" states.

---

## 2. Global shell & navigation

### 2.1 App layout (authenticated)

- **Sidebar** with grouped sections:
  - **Operations**: Dashboard · Applications · Services · Databases ·
    File manager *(Stub)* · Template library · Servers · Domains & SSL
  - **Team**: Members & roles · Notification channels · API tokens *(Stub)*
  - **System**: Updates & settings *(Stub)*
- Active item follows the first path segment, so detail pages
  (`/servers/:id`, `/applications/:id`, …) keep their parent highlighted.
- **Top bar**: breadcrumbs on detail pages, global status line showing the
  serving environment, control-plane HTTP port chip (`CP :8000`), agent gRPC
  port chip, web build version tag (`vX.Y.Z`), and the **account menu**
  (avatar → Sign out).
- **Team context**: every request is scoped to the active team; the user can
  belong to several teams (see §10).
- **Server rail** (component `ServerRail`): compact list of nodes with status,
  used for quick context switching on server-related screens.

### 2.2 Auth layout (unauthenticated)

Split layout: brand/marketing pane + form pane. Pages: Sign in, Create account,
OAuth callback ("Signing in…"), Invite accept.

### 2.3 Cross-cutting UX requirements

- **Status tags** use one consistent vocabulary and color mapping across
  entities (see §13).
- **Loading / empty / error** states for every list and every detail tab; errors
  show the backend message in plain language.
- **Feature-disabled state**: add-on features (teams, previews, notifications,
  metrics) can be switched off by the operator; the UI must show a clear
  "disabled by the operator" panel instead of an error.
- **Polling & realtime**: lists poll; logs and deploy steps stream live.
- **Destructive actions** (delete server/app/database/service, rollback,
  restore, remove member) require an explicit confirmation naming the target.
- **Secrets** are never shown after creation (masked), with a reveal/copy
  affordance only where the product allows (database credentials).
- Responsive: desktop-first, must remain usable on a tablet; tables degrade to
  stacked rows on narrow widths.

---

## 3. Authentication & onboarding

| Screen | Purpose | Details |
|---|---|---|
| **Sign in** (`/login`) | Email + password | Link to Register; **Continue with GitHub** (OAuth); inline error for failed OAuth (`oauth_failed`); remembers the originally requested page and returns there after login. |
| **Create account** (`/register`) | First-run / open registration | Email, password (strength guidance), confirm. On a fresh instance the first account becomes the single admin; later registration may be closed — show a "registration is closed" state. |
| **OAuth callback** | Transitional | Spinner + failure fallback back to Sign in. |
| **Invite accept** (`/invite/accept`) | Join a team by link | Shows team name + role being granted; Accept button; handles invalid/expired/used invites. |

Session behaviour: access + refresh tokens; expired session → redirect to Sign
in preserving `redirect`. Signed-in users hitting `/login` go to Dashboard.

---

## 4. Dashboard (`/dashboard`) — Live

Fleet overview. Must answer "is everything OK?" in one glance.

- **Summary tiles**: servers (ready/total), applications (running/total),
  databases, services.
- **Recent deploys** list: app name, branch/commit, status tag, duration,
  relative time, link to the app.
- **Server health** panel: per server name, status, CPU/RAM/disk mini-gauges,
  last seen.
- Quick actions: *Add server*, *New application*, *New database*.
- Empty state for a fresh install guides the user to "Add your first server".

---

## 5. Servers

### 5.1 Servers list (`/servers`) — Live

- Grid/list of nodes: name, IP:port, OS/arch, Docker version, status tag,
  container count, last-seen (heartbeat), agent version.
- Search by name/IP; filter by status.
- **Add server** button → wizard (§5.2).
- Informational callouts: "10-second heartbeat" and "Server-authenticated TLS"
  (how the agent talks to the CP).
- Empty state with a call to add the first node.

### 5.2 Add Server wizard — Live (4 steps)

1. **Connect** — name, IP/host, SSH port, SSH user, credential:
   **private key** (paste/choose existing key, or generate/store a new named
   key) **or password**. Validation of IP/port/name inline.
2. **Validate** — automatic first probe, then a checklist: SSH reachable,
   **Docker**, **CPU**, **RAM**, **Disk** (each ok/fail with detail text).
   Re-run button. A failed check explains the remedy. Going Back must not
   register the node twice.
3. **Install** — installs/connects the Gotham agent; real state only
   (pending → installing → connected); shows install command/log fallback.
4. **Finish** — success summary with node avatar (initials), name, address,
   status; CTA *Open server*.

### 5.3 Edit server (modal) — Live

Rename, change address/port/SSH user, switch or replace the credential
(key ↔ password), re-validate. Credential values are write-only.

### 5.4 Server detail (`/servers/:id`) — Live (tabs)

Header: breadcrumb, name, status tag, actions (Edit, Re-validate, Delete).

- **Overview**: Name, Address, SSH user, Credential type, Node ID, Docker
  version, Architecture/OS, CPU / Memory / Disk usage, Containers count, Last
  seen, Registered, Updated.
- **Containers** *(entry to `/servers/:id/containers`)*: see §5.5.
- **Metrics**: charts for **CPU, RAM, disk, network**; time-range selector
  (**1m / 1h / 1d** buckets) and **auto-refresh** choice (off / intervals);
  gaps in the series render as gaps (not interpolated); "metrics disabled"
  state when the feature flag is off.
- **Proxy & Traefik** — *Partial/Stub*: shows the node's reverse-proxy state
  (routers) — currently an empty placeholder pointing to Domains & SSL.
- **Node settings** — agent update (see §12), danger zone (delete server).

### 5.5 Containers (`/servers/:id/containers`) — Live

Docker engine view of one node:

- Table: name, image, state/status tag, ports, created, uptime.
- Search; filter by status (running / exited / etc.).
- Row actions: **Start, Stop, Restart**, open **live logs** (streaming
  `LogViewer`: follow toggle, search, wrap, download, pause).
- Toolbar: **Pull image**, **Run container** (image, name, ports, env).

---

## 6. Applications (deploy from Git)

### 6.1 Applications list (`/applications`) — Live

Cards/table: name, repo + branch, build pack, node, current status tag, domain,
last deploy time. Search + status filter. **New application** → wizard.
Empty state encouraging a first deploy.

### 6.2 Create Application wizard — Live (5 steps)

1. **Source** — pick a **provider** (GitHub / GitLab / Gitea / public URL),
   repository (searchable list or manual URL), branch (list), application
   name. Private repos use a generated **SSH deploy key** (shown with
   instructions); auto-deploy webhook is created for the user.
2. **Build pack** — radio list: *Auto-detect* (Dockerfile → Railpack →
   Buildpacks → static), **Railpack**, **Dockerfile**, **Buildpacks**
   (Heroku-style), **Static** (served by Traefik, no process). Each with a
   one-line hint.
3. **Runtime** — target **node** and container **port**; optional base domain.
4. **Env & storage** — `EnvEditor` (key/value rows, values prefixed `secret:`
   are sealed/masked) and `StorageEditor` (named volume or managed path +
   container mount path).
5. **Deploy** — review summary → *Create & deploy*.

Inline validation per step; Continue disabled until the step is valid.

### 6.3 Application detail (`/applications/:id`) — Live (tabs)

Header: breadcrumb, name, status tag, primary actions **Deploy/Redeploy**,
**Stop/Start**, **Rollback**, overflow (Delete). Link to the live app URL.

- **Overview**: Name, Branch, Build pack, Domain, Port, Node, plus the
  **deploy pipeline** visualization: `queued → cloning → building → pushing →
  starting → running | failed`, current step highlighted, per-step duration.
  Deployments table: kind (manual / webhook / rollback / preview), image tag,
  registry image, container id, created, duration, status, per-row **Rollback
  to this build**.
- **Logs**: realtime build + deploy logs per deployment (`DeployLogs` /
  `LogViewer`), step-grouped, follow/pause/search/download; falls back to
  stored logs for finished deploys.
- **Environment**: edit env vars and secrets (`EnvEditor`), unsaved-changes
  bar, "redeploy to apply" hint.
- **Storage**: edit persistent volumes (`StorageEditor`); warnings for legacy
  paths that require re-saving.
- **Domains**: attach/detach domains with `DomainEditor`; shows per-domain
  HTTPS/cert status (see §8).
- **Previews** (if enabled): list of **PR preview deployments** — PR number,
  branch, head SHA, preview URL, state (`active / deploying / failed /
  deleted`); auto-created when a PR opens, deleted when it closes; "previews
  disabled" state otherwise.
- Webhook info: webhook URL + secret/deploy key guidance for auto-deploy on
  push.

---

## 7. Databases

### 7.1 Databases list (`/databases`) — Live

Table/cards: name, engine + version, node, status tag, public port, created.
Search. **New database** → wizard.

### 7.2 Create Database wizard — Live (3 steps)

1. **Engine** — PostgreSQL, MySQL, MariaDB, MongoDB, Redis (logo + short note).
2. **Configure** — name, version (image tag dropdown), node, optional public
   port exposure.
3. **Review** — image reference, resources, create.

### 7.3 Database detail (`/databases/:id`) — Live (tabs)

Header: name, engine badge, status tag, actions **Start / Stop / Restart**,
Rename, Delete.

- **Overview**: Engine, Node, Container, Volume, Public port, Created;
  **connection credentials** card (host, port, user, password, DSN) with
  reveal/copy; password masked by default.
- **Backups**:
  - *Backups list*: time, size, trigger (manual/scheduled), target, status;
    row actions **Restore** (typed confirmation, outcome shown: success /
    partial / failed with message) and **Delete**; **Back up now**.
  - *Schedules*: cron expression (with human-readable preview), destination,
    enable toggle, edit/delete.
  - *Backup targets (destinations)*: **local** or **S3-compatible** (endpoint,
    region, bucket, key prefix, access key, secret key); **Test connection**
    button with result; edit/delete.
  - *Restore history*.

---

## 8. Domains & SSL (`/domains`) — Live

Page-level tabs:

- **Certificates**: list per domain — status (`pending / issuing / active /
  failed / expiring`), issuer, expiry, challenge type (**HTTP-01** or
  **DNS-01**). Create/edit via `CertificateForm` (domain(s), challenge,
  DNS provider when DNS-01, wildcard). Notice: *DNS-01 requires the zone to be
  delegated to the provider*.
- **DNS providers**: Cloudflare-style provider cards (name, type, zones,
  enabled toggle, credential write-only). Create/edit/delete.
- **Redirects**: source domain → target domain, HTTP code (301/302/307/308),
  preserve-path toggle, enabled toggle, attach to an application.
- **Routers** — *Partial*: live Traefik router list from the owning node
  ("status is observed live from the owning node"); shows backend-pending
  honestly where data is unavailable.

---

## 9. Services & Templates

### 9.1 Services (`/services`) — Live (tabs)

- **Compose services**: table of Docker Compose projects — name, node, status
  tag, domains, updated. Search + status filter. **New service** → create modal
  with name, node, and a `ComposeEditor` (YAML, validation).
- **Template gallery**: embedded tab with the template library (§9.3).
- Info panel: "How a template is structured".

### 9.2 Service detail (`/services/:id`) — Live/Partial

Header: name (mono), status tag, actions **Deploy, Restart, Stop, Delete**.
Contents: Node, Status, Service id, Compose project, Domains, Created/Updated;
**compose.yaml** viewer/editor; **environment variables** editor (add/remove
rows); **containers** of the project; live **service logs**
(`ServiceLogs`); deploy history with states; a **deploy step timeline** marked
"backend pending".

### 9.3 Template library (`/templates`) — Live

One-click catalog (built-ins today: **WordPress, n8n, Nextcloud, Uptime Kuma**).

- Gallery cards: icon, name, description, tags.
- **Template wizard** (`TemplateWizard` + `DynamicForm`): form is generated
  from the template's fields — types `text | secret | number | select | bool`,
  with label, help text, placeholder, defaults, min/max, required, regex
  validation errors. Choose node + service name → *Render preview* → Deploy as
  a Compose service. Generated secrets should be offered where applicable.

---

## 10. Teams & collaboration (`/teams`) — Live

- **Team switcher / list**: create team, rename, delete; shows the user's role
  in each.
- **Members & roles**: table with email, role tag (**Owner / Admin /
  Read-only**), joined date; change role (owner/admin only), remove member.
  Read-only users see the page without management controls (capability
  differences must be visible, not just disabled silently).
- **Invites**: create invite (email + role) → returns a **one-time accept
  link** (copy button, shown once); pending invites list with revoke.
- "Teams disabled" state when the feature flag is off.

## 11. Notification channels (`/settings/notifications`) — Live

- Channel list: name, kind badge (**Discord, Slack, Telegram, Email**), enabled
  toggle, subscribed events, scope, last test result. Test / edit / delete.
- Create/edit form (fields vary by kind):
  - Discord / Slack: webhook URL (masked).
  - Telegram: bot token (masked), chat ID.
  - Email: SMTP host, port, username, password (masked), from address,
    recipients.
- **Events** multi-select: deploy success, deploy failure, backup success,
  backup failure.
- **Resource scope**: whole team or a specific application/database.
- "Notifications disabled" state.

---

## 12. System: updates & agent (Stub → needs design)

Backend is complete; the sidebar entry **Updates & settings** is a stub.
Design the page for:

- **Control-plane update**: current version, available version(s) per channel
  (**stable / beta**), release notes, *Check for updates*, *Update now*
  (signed manifest, atomic swap, automatic rollback on failed health check),
  progress and result states; optional auto-update toggle.
- **Agent update**: per-node agent version vs. offered version, rollout button
  per node/all, status (up to date / update available / updating / failed).
- Build/version info and environment.

## 13. Other stubs to account for in navigation

- **File manager**: browse/edit files of a
  service/application volume — future phase; keep the nav slot.
- **API tokens**: create/revoke personal tokens (name, scope, expiry, shown
  once). Backend exists; UI not built.

---

## 14. Shared vocabulary & status mapping

| Entity | States |
|---|---|
| Server | pending · validating · ready · offline · error |
| Deployment | queued · cloning · building · pushing · starting · running · failed |
| Application / Service | running · stopped · deploying · failed |
| Database | running · stopped · starting · error |
| Backup / Restore | pending · running · success · partial · failed |
| Certificate | pending · issuing · active · expiring · failed |
| Preview | active · deploying · failed · deleted |
| Team role | owner · admin · read-only |

Map consistently: green = running/ready/active/success, blue = in progress,
amber = pending/expiring/partial, red = failed/error/offline, grey =
stopped/deleted.

## 15. Reusable component inventory

`AddServerWizard`, `EditServerModal`, `ServerRail`, `ServerStatusTag`,
`CreateAppWizard`, `CreateDatabaseWizard`, `TemplateWizard`, `DynamicForm`,
`TemplateGallery`, `EnvEditor`, `StorageEditor`, `DomainEditor`,
`CertificateForm`, `ComposeEditor`, `LogViewer`, `DeployLogs`, `ServiceLogs`,
`MetricsChart`, `DeploymentStatusTag`, `DatabaseStatusTag`, `MeCard`,
`GothamIcon`. New screens should reuse or extend these before inventing
parallel patterns.

## 16. Deliverables expected from the design agent

1. A new visual language (colors, type, spacing, iconography, light/dark) and
   a design system definition, independent of the previous design.
2. Designs for every screen in this document, including the **Stub** screens
   (*Updates & settings*, *API tokens*, *File manager*) and the *Partial* gaps
   (server Proxy tab, service deploy timeline).
3. For each screen: loading, empty, error, and feature-disabled states
   (§2.3) and the status mapping (§14).
4. Light/dark parity driven by tokens only; no hard-coded colors.
