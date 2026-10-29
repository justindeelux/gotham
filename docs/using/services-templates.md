---
title: Services and templates
---

# Services and templates

A service is a Docker Compose project that Gotham runs for you on one node.
You supply the `compose.yaml` and an environment for `${VAR}` substitution; the
control plane stores both, renders the document, and sends it to the node's
agent, which runs it with the node's own `docker compose` CLI.

Templates are the no-YAML path to the same thing: a built-in stack plus a form,
rendered into a compose document and an environment map, then created as a
service.

If you want to deploy code or a single image with Gotham's application features
(deployments, rollback, per-application domains), see
[Applications](/using/applications) instead.

## A service is one compose project

Each service is exactly one compose project on exactly one node. Its project
name is derived from the service id (`gotham-<id>`), so two services never share
containers or volumes.

The stored document is the one you wrote, verbatim. It is validated before
saving, so a stored service is always renderable. The deploy path accepts the
common compose subset:

- Services must reference an **image**; there is no `build`, `extends`,
  `env_file` or `include`.
- A document is capped at 1 MiB and 256 services; a service's environment at
  256 variables.
- Named volumes are the project's storage. They are namespaced under the
  project name, and stopping or deleting the project keeps them. Anonymous
  volumes are not reattached by a later `up`, so put data you need to keep in a
  named volume.
- The `gotham-db-` volume prefix is reserved for managed databases and cannot
  be declared or referenced by a service.

### Environment and interpolation

The service environment is the substitution input for `${VAR}` references in
the document. Rendering happens on the control plane before the node validates
the result, and supports the compose forms:

- `$VAR` and `${VAR}`
- `${VAR:-default}`, `${VAR-default}`
- `${VAR:?message}`, `${VAR?message}`
- `${VAR:+alternative}`, `${VAR+alternative}`
- `$$` for a literal dollar

An unset variable outside a default or error form is a validation error, and
nested references such as `${A:-${B}}` are not supported. Because values are
substituted before the document leaves the control plane, the node's compose
run does not interpolate a second time.

Environment values are treated as confidential: they are masked in the console
and redacted from stored errors and deploy-history rows. Secrets typed directly
into the YAML are your own content and are not redacted.

### Domains

A service can be routed in two ways.

**Managed domains** are added after the service exists, from the Domains tab on
the service detail page (API: `GET`/`POST /api/v1/services/{id}/domains` and
`PUT`/`DELETE /api/v1/services/{id}/domains/{domainId}`). Each row picks the
compose service, the container port and HTTP or HTTPS. Several domains may point
at the same service, and every row can be disabled. Adding, changing or
removing a domain resyncs the proxy, so the route appears without a redeploy.

A service created from a template can also start with a domain. The wizard's
Domain field is optional; a non-empty value is stored as a managed row in the
same transaction as the service, and an empty value creates the service with no
route. Templates declare that route themselves, so the stored compose document
carries no `gotham.domain` label (see [template routes](#template-routes)).

**Declared in compose** labels keep working for imported documents and existing
services:

```yaml
services:
  web:
    image: nginx:1.27
    labels:
      gotham.domain: app.example.com
      gotham.domain.port: "80"
```

`gotham.domain` is validated with the same rules as application domains, and
`gotham.domain.port` defaults to 80. The domain label may itself be a `${VAR}`
reference. Label routes are listed read-only with source `compose`; a label and
a managed row for the same host are both held back with a diagnostic instead of
silently picking one.

When the control plane renders the document for deploy, validation and proxy, it
injects `GOTHAM_DOMAIN` (the first enabled host) and `GOTHAM_DOMAINS` (the
comma-separated host list). They are empty strings when the service has no
domain. Templates can reference them; the two names are reserved and rejected in
the service environment. A domain change marks the service `pending_changes:
["domains"]` until the next successful deploy picks up the variables.

HTTPS rows serve through the default ACME resolver and redirect HTTP to HTTPS,
exactly like application domains. A host is claimed platform-wide: a domain
already used by an application, a redirect source, another service or a compose
label is rejected at write time with `409`. See
[Domains and previews](/using/domains-previews) for DNS and certificates, and
[Proxy and DNS](/operating/proxy-dns) for the operator side.

## Create a service

Use **Add resource → Service**, then either pick a template or **Import a
compose file**. Importing asks for a name, a node and the document; the
environment is edited afterwards on the detail page.

A service created from a template remembers its slug: the API returns it as
`template` in the service JSON, and `POST /api/v1/services` accepts an optional
`template` slug that must exist in the catalog (an unknown slug is a `400`). A
compose import sends no `template` and reads back empty.

Saving validates the document against the environment. A change only updates
the stored document — the running project keeps running until the next deploy.

Creating from a template is one action: **Create and deploy** stores the
service and starts the deploy in the background, closes the wizard and opens the
service detail page, which follows the deploy until it is `running` or `failed`.
Importing a compose file creates the service only; deploy it from the detail
page.

## Deploy, restart, stop, delete

- **Deploy** renders the stored document, has the node validate it with
  `docker compose config`, starts the project, and records one deploy row. The
  API accepts the deploy with `202` and runs it in the background, so the
  response does not wait for images to pull: the deploy row moves from
  `deploying` to `running` or `failed`, and the service detail page polls until
  it reaches that state.
- **Restart** restarts a running project in place and starts a stopped one.
- **Stop** takes the project down with `compose down`. Named volumes keep every
  byte, so a stopped service is fully recoverable by a deploy or restart.
- **Delete** stops the project and soft-deletes the service. Named volumes stay
  on the node.

A lifecycle action issued while a deploy is still running is refused with a
conflict (`409`) instead of waiting for the deploy to finish. If the control
plane restarts mid-deploy, the leftover deploy is recorded as failed
("interrupted") on startup and a service still `creating` is moved to `error`,
so nothing stays pending forever.

The API contract matches the console: `POST /api/v1/services/{id}/deploy`
answers `202 Accepted` with the service and the new deploy row, and the client
polls `GET /api/v1/services/{id}` and `GET /api/v1/services/{id}/deploys` until
the newest deploy is `running` or `failed`. `POST /api/v1/services` accepts an
optional `"deploy": true` (used by the template wizard) to create and start in
one call; it answers `201 Created` with the service while the deploy continues
in the background.

## Containers and logs

The **Containers** card lists the project's containers as reported by the node
agent: compose service, container name, image, state, status and health.
Reading the list is an on-demand call, so it reflects the node at the moment you
refresh it. If the agent is unreachable the card shows the error (and any last
successful read), not an empty project.

**Logs** streams `docker compose logs -f` through the node agent. Choose all
services or one compose service; the stream can be paused, followed or cleared.
Only a live stream is available — there is no stored log file to download — and
the API defaults to the last 200 lines when no tail is requested. Log content
passes through untouched; terminal stream errors are redacted like every other
node error.

## Deploy history

Every deploy attempt is recorded with its state (`deploying`, `running`,
`failed`), redacted error, and timestamps. The history shows the newest 50
attempts, newest first. The rendered document of each deploy is stored with the
attempt.

The API records one row per attempt and exposes no per-step progress, so there
is no step timeline: the history plus the live log stream is the whole picture.
A failure while starting the project also sets the service status to `error`
until the next successful deploy.

## Moving a service

The **Server and environment** card changes the service's environment and node.

- Moving to another environment stays inside the project. The name must be free
  there, or the move is refused as a conflict.
- The node can only be changed before the first deploy. Once the project has
  run on a node it is pinned, and moving it to another environment keeps the
  node.

## Templates

The built-in catalog has four templates. Each is a fixed stack; there are no
optional services to toggle at render time.

| Template | Stack | Required fields |
| --- | --- | --- |
| WordPress | `wordpress:6-apache` + `mysql:8.4` | database password, database root password (domain optional) |
| Nextcloud | `nextcloud:31-apache` + `postgres:16-alpine` + `redis:7-alpine` | domain, admin password, database password, Redis password |
| n8n | `n8nio/n8n:1` | encryption key (domain optional) |
| Uptime Kuma | `louislam/uptime-kuma:1` | none (domain optional) |

Each template ships named volumes for the data that must survive a restart.
Other fields (database name and user, admin user, timezone, log level, n8n
diagnostics and execution retention, Uptime Kuma timezone) have defaults and can
be left alone. Every template declares one route: WordPress and Nextcloud use
port 80, n8n 5678 and Uptime Kuma 3001. WordPress, n8n and Uptime Kuma accept an
empty domain, so the service starts with no route and you add one later from the
Domains tab.

Nextcloud keeps its domain required. The official image only reads
`NEXTCLOUD_TRUSTED_DOMAINS` on the first install; a host attached to an existing
install is rejected as untrusted. Add further Nextcloud hosts from the Domains
tab after the install, not by changing the first one.

### Template routes

A template can declare the route the platform should create, instead of writing
a `gotham.domain` label into its compose document:

```yaml
routes:
  - service: wordpress
    port: 80
    domain_field: domain
```

`service` must be a compose service declared in `compose.yaml`, `port` is the
container port (1-65535), and `domain_field` must name a `text` field. The field
may be empty: an empty value produces no route and no error, so the same
template works with and without a domain. A template without `routes` behaves
as before and can still use compose labels. Rendering returns one
`{service, port, domain}` entry per non-empty route, and the wizard sends those
entries as `domains` on `POST /api/v1/services`; the API inserts them with the
service in one transaction, so a host already taken fails the whole create.

### Generating secret values

Secret fields marked generatable show a **Generate** button next to the input.
It fills the field with a fresh 24-character value made of letters and digits,
generated in the browser; nothing is sent anywhere until you create the service.
Use **Copy** to store the value somewhere safe — a secret is write-only after
creation — but you can still type your own value instead.

Template authors opt in per field with `generate: true` on a `secret` field. Such
a field must not declare `pattern`, and `max_length`, when set, must be at least
16. Declaring `generate` on any other field type is a load error.

### What rendering produces

Rendering is strict, non-executing string substitution — every template field is
a Go-template placeholder such as:

```text
{{ .field }}
```

placeholders. It produces:

- a **compose YAML** document with every placeholder resolved,
- an **env map** holding the values of secret fields, and
- a **domains** list with one `{service, port, domain}` entry per non-empty
  route declared by the template.

Secret fields are never written into the YAML. A secret placeholder becomes a
`${field}` reference, and its value travels in the env map, so the service
pipeline substitutes it at deploy time and redacts it from errors and deploy
history. Keep secrets in value positions (environment variables, command
arguments); a secret used as a service name or domain label is rejected.

The render is validated with the same parser the deploy path uses, so a
rendered document is deployable. Rendering itself executes nothing server-side:
creating a service from a template writes the service row and its environment,
then starts the deploy in the background. The preview step in the wizard shows
the document, its service and volume counts, and any domain routes.

## Choosing what to create

| Option | What it is | Choose it when |
| --- | --- | --- |
| Application | Workload from Git, Dockerfile, image or compose source, with deployments, rollback, domains, terminal and storage. | You deploy code or one image and want the application features. |
| Service | One compose project you write, plus its environment. | You have a compose file or need several containers without a build step. |
| Template | A built-in stack rendered into a service. | You want WordPress, Nextcloud, n8n or Uptime Kuma without YAML. |
| Managed database | One engine container with generated credentials and managed backups. | You need a database for an application or service. |
| Bundled database in a compose stack | The database is a container inside your project (WordPress, Nextcloud). | You will run the database yourself — there are no managed backups or credentials. |

Templates never provision a managed database. The database a template ships is
an ordinary compose service inside the same project, with the password you
entered in the form.

> [!NOTE]
> The database and service surfaces can be turned off instance-wide by the
> owner. When they are, the corresponding routes and console entries are absent.
