# Phase 5 — Databases & Backups (W8–W9) — parallel with Phase 6

**Goal:** managed databases: create, access, backup/restore.

**Exit criteria (Milestone M5):**
- [ ] Create databases: PostgreSQL, MySQL/MariaDB, MongoDB, Redis (image map, standard env, persistent volume).
- [ ] Optional public access (open port for external clients).
- [ ] Manual + scheduled automatic backups, stored locally + S3-compatible (AWS S3, Cloudflare R2, MinIO).
- [ ] Restore from backup works (delete DB → restore → data is back).

**Rollback:** a DB is a container with its own volume — deleting a database only removes the container; the volume is kept 7 days (soft delete) before permanent removal. Backup/restore is the primary rollback mechanism.

**Phase gate:** continuous mode applies (see 00-roadmap.md) — after the exit criteria are met, continue to Phase 7 without stopping (Phase 6 runs in parallel on the same Phase 3 base).

---

## BE-5.1 — Database engines — `ws/p5-db`

- **Context brief:** map each engine to a spec: default image + tag, standard env (user/password/db), internal port, volume path. Create containers via the agent (reuse Phase 3), attach a `gotham-db-{id}` volume. Sensitive data (passwords) lives in the `secrets` table (Phase 4) — the DB only keeps references.
- **Deliverables:**
  - `internal/databases/`: `DatabaseEngine` interface (Image, EnvSpec, PortSpec, VolumeSpec, Healthcheck) + 5 implementations: `postgres`, `mysql`, `mariadb`, `mongodb`, `redis`.
  - `databases` migration (id, name, engine, version, status, server_id, secret refs, public_port, storage_path).
  - CRUD routes `/api/v1/databases` + `/start|stop|restart`.
  - Tests: create each engine on the dev Docker, healthcheck green, connect with credentials.
- **Verify:** create Postgres → `psql` connect from the dev machine via the public port; restart keeps data.
- **Depends on:** Phase 4 (secrets, volume map). If Phase 4 is slow: temporarily store passwords encrypted locally in this phase, switch over later.

## BE-5.2 — Backups: local + S3 + restore — `ws/p5-backups`

- **Context brief:** per-engine backup: `pg_dump` (Postgres), `mysqldump` (MySQL/MariaDB), `mongodump` (Mongo), Redis RDB/AOF copy. Jobs run in a **temporary container** on the same server (mount the DB volume, run the dump tool) → compress → upload to S3 (`minio-go` SDK) or keep locally. Backup schedules: internal CP cron (`backup_schedules` table). Restore: download → run the restore tool in a temporary container.
- **Deliverables:**
  - `internal/databases/backups.go`: BackupEngine interface + 4 implementations; S3 client (endpoint, bucket, key, secret — stored in `secrets`).
  - `backups` migration (id, db_id, type, status, size, location, created_at), `backup_schedules`.
  - Routes: `POST /api/v1/databases/{id}/backup`, `/restore`, schedules CRUD; simple cron scheduler.
  - Tests: backup Postgres → drop a table → restore → data complete.
- **Verify:** backup to local MinIO → delete the DB → restore → select shows all rows.
- **Depends on:** BE-5.1. Parallel with the backup part of FE-5.1.

## FE-5.1 — Databases UI — `ws/p5-db-ui`

- **Context brief:** databases page: create (pick engine + version), credentials display (copy), start/stop/restart, public port, backups list + backup-now/restore buttons + schedule config + S3 storage config.
- **Deliverables:** `/databases` + `/databases/{id}` pages; `CredentialsPanel`, `BackupList`, `ScheduleEditor` components.
- **Verify:** e2e: create Postgres → copy credentials and connect → backup → restore → list correct.
- **Depends on:** BE-5.1 (contract), BE-5.2 (backup UI).
