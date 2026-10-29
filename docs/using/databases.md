---
title: Databases
---

# Databases

A database is one container running a single engine on one node, with its own
named volume. Gotham generates the login credentials at creation, stores them
encrypted, and keeps them available on the database page for the members of the
project.

Databases live inside a project environment, next to applications and services.
See [Projects and environments](/using/projects-environments) for how resources
are grouped, and [Services and templates](/using/services-templates#choosing-what-to-create)
for when to use a managed database instead of a database bundled in a compose
stack.

## Engines

Five engines are supported. The create form offers known-good image tags per
engine and falls back to the default below when none is chosen.

| Engine | Default image | Container port |
| --- | --- | --- |
| PostgreSQL | `postgres:16-alpine` | 5432 |
| MySQL | `mysql:8.4` | 3306 |
| MariaDB | `mariadb:11.4` | 3306 |
| MongoDB | `mongo:7.0` | 27017 |
| Redis | `redis:7.2-alpine` | 6379 |

All engines use their official images. Gotham adds the engine's initialisation
environment, the data volume and a readiness healthcheck.

## Create a database

Use **Add resource → Database** and follow the three steps: engine, configure,
review.

- **Engine and version** — pick one of the five engines, then an image tag.
- **Name** — 1–63 characters: letters, digits, `.`, `_` or `-`. It is used for
  the container and the generated credentials.
- **Node** — the node the database runs on. It is pinned at creation: the
  container and its volume live there, and the database cannot be moved to
  another node afterwards.
- **Public port** — optional and off by default. A public port publishes the
  engine port on the node so external clients can connect. The binding is fixed
  at creation and cannot be changed later, so leave it off unless an external
  client needs it.

Credentials (user, password, database name) are generated server-side; you do
not choose them. MySQL and MariaDB also get a separate root password. They are
shown under **Credentials** on the database page.

Creation returns as soon as the database row and its credentials exist; the
wizard then closes and opens the database page, which shows the status
**Creating** while the image downloads and the container becomes healthy. This
can take a few minutes on a node that does not have the image yet. The page
polls the API and switches to **Running** by itself when the healthcheck
passes, or to **Error** with a short reason if provisioning fails. The
underlying contract is `POST /api/v1/databases`, which answers `202 Accepted`
with the database in `creating` plus its credentials; poll
`GET /api/v1/databases/{id}` until `status` is `running` or `error`.

Each database gets a named volume (`gotham-db-<id>`) mounted at the engine's
data directory. The volume is independent of the container: removing the
container does not remove the data.

## Lifecycle

Start, stop and restart are available on the database page.

- **Stop** stops the container; the volume and its data are untouched.
- **Start** starts the existing container again.
- **Restart** restarts it in place.
- **Delete** removes the container from the node but keeps the volume for
  seven days before the retention sweep removes it permanently. Deleting is
  therefore not instantly destructive, but it is not a backup either: back up
  first if the data matters.

## Credentials and connection string

The **Credentials** card shows the generated user, password and database name,
with reveal/copy actions, and a connection string using the engine's URL scheme
(`postgresql://`, `mysql://`, `mariadb://`, `mongodb://`, `redis://`). The
values are stored encrypted and readable to members with access to the project;
they are not included in the database list responses.

The connection string depends on how the database is exposed:

- With a **public port**, it points at the node address and the published port.
- Without one, it points at the database container's name and the engine's
  internal port, which is only meaningful on the node's container network.

## Backups

Open the **Backups** tab of a database to manage runs, schedules and storage
providers. A backup is an engine dump taken by a temporary container on the same
node; the artifact is compressed and stored in the selected destination. Runs
are listed with type (`manual` or `scheduled`), status, size, destination and
timestamps. A failed run keeps its row and error text, so you can see what went
wrong before running it again.

Backups are a feature of their own: the same core serves databases today and
will serve application storage and service volumes, and the **Backups** page
lists every run and schedule in one place. Next to it, `GET /v1/backups`,
`POST /v1/backups`, `GET /v1/backups/{id}`, `POST /v1/backups/{id}/restore`,
`GET/POST /v1/backups/schedules`, `PATCH/DELETE /v1/backups/schedules/{id}`,
`GET /v1/backups/restores` and `GET/POST /v1/backups/providers` are the
resource-agnostic API. Only `database` is accepted as a resource type for now;
other types answer `400` until their driver lands.

The database-scoped routes listed on this page
(`/v1/databases/{id}/backups`, `.../schedules`, `.../restore` and
`/v1/databases/backup-targets`) are **deprecated aliases** of that core. They
keep their request and response shapes, so existing tokens and MCP clients keep
working, and they always describe the same job rows and the same artifact layout
as the new routes.

### Back up now

**Backup now** queues a manual run. Taking a dump stops the database container
while the temporary container reads the volume, so the database is briefly
unavailable; it is started again afterwards if it was running. The same is true
of scheduled runs. Choose a provider per run, or leave the default.

### Restore

**Restore** on a completed backup queues a restore. The database is stopped
while the artifact is staged and applied, and the restore overwrites the
current data of the database — this cannot be undone, so back up first if the
live data still matters. **Restore history** keeps the durable result of every
queued restore, including failures. A failed restore leaves the database
stopped with the cause shown on the page; it never restarts into
half-restored data.

### Schedules

A schedule is a five-field cron expression in the control plane: minute, hour,
day of month, month, day of week. Fields accept `*`, single values, ranges,
steps (`*/5`, `1-10/2`) and comma lists of those. The next run is computed when
you save and shown in the list. Each schedule can be enabled or paused and
carries its own provider; without one, runs land in the built-in Local disk
provider. Deleting a schedule leaves past backups untouched.

### Storage providers

Providers are an owner-level catalogue: configure one once and use it from every
database. Every install ships a built-in **Local disk** provider that writes to
the control plane's backup directory. It is always listed, cannot be changed
into another kind and cannot be deleted, so a fresh install can back up with no
setup and an artifact never lands on an unnamed destination.

S3-compatible storage is supported against AWS S3, Cloudflare R2, MinIO and
similar services: configure an endpoint, region, bucket, optional key prefix,
access key and secret key. Providers are configured under **Backups → Storage
providers**, once for the owner account rather than per database. Only the owner
configures them; a member can run, list and restore backups of a database in a
project they belong to, but cannot choose or read a provider. The keys are
sealed on the server and never shown back, so an edit form leaves stored keys in
place when the key fields are blank. **Test** checks the endpoint and
credentials against the bucket, or the local directory's writability, and
reports the result.

Deleting a provider does not delete past backups, but they can no longer be read
back from that provider, so restores from them will fail. The local backup
directory is a control-plane setting on the host.

> [!TIP]
> Backups stop the database. For a busy database, schedule dumps for a quiet
> window and tell users about the short interruption.

## Resource history

The **Metrics** tab charts CPU, memory, network and disk I/O for the database
container over 1h, 24h, 7d and 30d ranges. It uses the same agent samples,
units and retention as application history: CPU in cores (`1.0` = one full
core), memory as the working set after subtracting reclaimable page cache, and
bytes-per-second rates from counter deltas over the actual elapsed time. A
first sample, a counter reset or a nonpositive interval is a gap, not zero.
With no memory limit the chart shows bytes and **unlimited**, never an invented
percentage. A **partial** badge exposes missing replica coverage, and deployment
markers separate container incarnations so a restart or replacement does not
merge with the previous container. Raw samples are kept for 24 hours,
1-minute rollups for 7 days and 1-hour rollups for 30 days.

The API is `GET /api/v1/databases/{id}/metrics?range=1h|24h|7d|30d&service=`.
An older agent reports history as unavailable instead of an empty healthy
chart. Project members see only databases in a granted project.
