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
sudo cat /etc/gotham/gotham.env
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

## Upgrade Redis

Redis 6.2 and newer can be replaced in place; it reads data written by older
versions.

- If you use the distribution packages:
  ```sh
  sudo apt-get update
  sudo apt-get install --only-upgrade redis-server redis-tools
  ```
- If you use the official Redis repository, follow
  [redis.io/docs/latest/operate/oss_and_stack/install/install-stack/apt](https://redis.io/docs/latest/operate/oss_and_stack/install/install-stack/apt/)
  and upgrade the `redis` packages.

Then restart Redis and Gotham:

```sh
sudo systemctl restart redis-server
sudo systemctl start gotham
```

Redis holds no data Gotham cannot rebuild (sessions and queues), so a lost
cache is not a data loss. PostgreSQL is the database that matters: always back
that one up first.

## Managed databases

If you set `GOTHAM_DATABASE_DSN` or `GOTHAM_REDIS_ADDR`, Gotham does not manage
those services and the installer never touches them. Upgrade them with your
provider's procedure.
