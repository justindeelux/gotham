---
title: Databases
---

# Databases

A database is one container running a single engine on one node, with its own
named volume. Gotham generates the login credentials at creation, stores them
encrypted, and keeps them available on the database page for the project's team.

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
not choose them. MySQL and MariaDB also get a separate root password. The
success screen shows them, and they stay available under **Credentials** on the
database page.

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
values are stored encrypted and readable by team members with write access to
the project; they are not included in the database list responses.

The connection string depends on how the database is exposed:

- With a **public port**, it points at the node address and the published port.
- Without one, it points at the database container's name and the engine's
  internal port, which is only meaningful on the node's container network.

## Backups

Open the **Backups** tab of a database to manage runs, schedules and storage
targets. A backup is an engine dump taken by a temporary container on the same
node; the artifact is compressed and stored in the selected destination. Runs
are listed with type (`manual` or `scheduled`), status, size, destination and
timestamps. A failed run keeps its row and error text, so you can see what went
wrong before running it again.

### Back up now

**Backup now** queues a manual run. Taking a dump stops the database container
while the temporary container reads the volume, so the database is briefly
unavailable; it is started again afterwards if it was running. The same is true
of scheduled runs. Choose a destination per run, or leave the default.

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
carries its own destination; without one, runs land in the default local store.
Deleting a schedule leaves past backups untouched.

### Storage targets

Backups go to the control plane disk when no target is configured.
S3-compatible storage is supported against AWS S3, Cloudflare R2, MinIO and
similar services: configure an endpoint, region, bucket, optional key prefix,
access key and secret key.

Targets are configured under **Backups → Backup targets**, once per account
rather than per database — any member with deploy access can add them. The keys
are sealed on the server and never shown back, so an
edit form leaves stored keys in place when the key fields are blank. **Test**
checks the endpoint and credentials against the bucket and reports the result.

Deleting a target does not delete past backups, but they can no longer be read
back from that target, so restores from them will fail. The local destination
directory is set by the control plane operator.

> [!TIP]
> Backups stop the database. For a busy database, schedule dumps for a quiet
> window and tell users about the short interruption.
