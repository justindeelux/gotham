---
title: Database versions and upgrades
---

# Database versions and upgrades

Gotham stores its data in PostgreSQL and uses Redis as a cache and queue. On a
new install the installer sets both up for you and picks versions that are
tested with Gotham:

| Service | What a new install gets |
|---|---|
| PostgreSQL | 16, from the official PostgreSQL (PGDG) repository when your distribution ships an older version |
| Redis | your distribution's Redis when it is 6.2 or newer, otherwise 7.2 from the official Redis repository |

You do not need to choose or configure anything. The installer prints the
versions it ended up with at the end of the run, and the local database
password is generated at install time and stored in
`/etc/gotham/gotham.env` (readable by root and the `gotham` service only).

## Check the installed versions

```sh
psql --version
redis-server --version
sudo grep '^GOTHAM_DATABASE_DSN=' /etc/gotham/gotham.env
```

The `GOTHAM_DATABASE_DSN` line in that file is the connection string Gotham
uses, including the generated password.

## Existing installs are never upgraded automatically

Re-running the installer repairs Gotham but does not replace a database that
is already installed. If PostgreSQL or Redis is older than the versions above,
the installer prints a warning and keeps the existing service, because a
PostgreSQL major upgrade must be planned and backed up. That is what this page
is for.

If you see the warning, Gotham keeps working. Upgrade when convenient.

## Before you start

- Take a backup and keep it outside this server:
  ```sh
  sudo -u postgres pg_dumpall > ~/gotham-db-before-upgrade.sql
  sudo cp /etc/gotham/gotham.env ~/gotham-env-before-upgrade
  ```
- Stop Gotham so nothing writes during the upgrade:
  ```sh
  sudo systemctl stop gotham
  ```
- These commands need root and run on the Gotham host. Expect a few minutes of
  downtime.

## Upgrade PostgreSQL

Ubuntu and Debian include a wrapper around PostgreSQL's own `pg_upgrade` that
keeps your data, the `gotham` role and its password. In the examples the old
version is 16 and the new one is 17; use the versions you actually have
(`pg_lsclusters` shows them).

1. Install the new PostgreSQL version from the PGDG repository, following
   [postgresql.org/download/linux/ubuntu](https://www.postgresql.org/download/linux/ubuntu/).
   For example:
   ```sh
   sudo apt-get install postgresql-17
   ```
2. Upgrade the cluster. If installing the new version created an empty cluster
   (for example `17 main`, shown by `pg_lsclusters`), remove it first:
   ```sh
   sudo pg_dropcluster --stop 17 main
   sudo pg_upgradecluster -v 17 16 main
   ```
3. Start PostgreSQL and Gotham:
   ```sh
   sudo systemctl start postgresql
   sudo systemctl start gotham
   ```
4. Check that the panel loads and `psql --version` shows the new version. Once
   you are happy, remove the old cluster to free disk space:
   ```sh
   sudo pg_dropcluster --stop 16 main
   ```

If your setup cannot use `pg_upgradecluster`, the alternative is a dump and
restore: dump with `pg_dumpall`, create a fresh cluster, restore the dump into
it with `psql`. The [PostgreSQL documentation](https://www.postgresql.org/docs/current/upgrading.html)
covers both methods.

The installer installs the versioned `postgresql-16` package, so package
upgrades stay on 16.x; no version pin is needed for PostgreSQL.

## Upgrade Redis

Redis 6.2 and newer can be replaced in place by a newer release; it reads data
written by older versions. How to upgrade depends on whether your distribution
already ships 6.2 or newer:

```sh
redis-server --version
apt-cache policy redis-server
```

**Your distribution ships 6.2 or newer.** Upgrade the distribution packages:

```sh
sudo apt-get update
sudo apt-get install --only-upgrade redis-server redis-tools
```

**Your distribution ships an older Redis** (Ubuntu 22.04, for example, ships
6.0, which is end of life). `--only-upgrade` cannot cross to a supported
version there, so add the official Redis repository and install from it; this
replaces the distribution package. First add the repository and its signing
key:

```sh
sudo apt-get install -y lsb-release curl gpg
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://packages.redis.io/gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/redis-archive-keyring.gpg
sudo chmod 0644 /etc/apt/keyrings/redis-archive-keyring.gpg
echo "deb [signed-by=/etc/apt/keyrings/redis-archive-keyring.gpg] https://packages.redis.io/deb $(lsb_release -cs) main" \
  | sudo tee /etc/apt/sources.list.d/redis.list
sudo apt-get update
```

Then install the server and tools. `sudo apt-get install redis-server
redis-tools` takes the newest release the repository serves; if you want the
7.2 series the installer uses, pin it the same way:

```sh
sudo apt-get install "redis-server=6:7.2.*" "redis-tools=6:7.2.*"
```

Then restart Redis and Gotham:

```sh
sudo systemctl restart redis-server
sudo systemctl start gotham
```

When the installer used packages.redis.io, it also wrote
`/etc/apt/preferences.d/gotham-redis.pref`, which keeps `redis-server` and
`redis-tools` on the 7.2 series so a routine `apt upgrade` does not move to
8.x. To upgrade deliberately, delete that file, run `sudo apt-get update`, and
upgrade.

Redis holds caches and queues, not the platform records PostgreSQL keeps, so a
lost cache is rebuilt. If other workloads share this Redis, check their data
first.

## Managed databases

If you set `GOTHAM_DATABASE_DSN` or `GOTHAM_REDIS_ADDR`, Gotham does not manage
those services and the installer never touches them. Upgrade them with your
provider's procedure.
