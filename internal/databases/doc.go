// Package databases provisions and operates managed databases (Phase 5,
// BE-5.1): a database is a container on a managed node plus a named volume
// ("gotham-db-{id}") that outlives the container.
//
// Each supported engine (postgres, mysql, mariadb, mongodb, redis) is a
// DatabaseEngine that maps a request onto the container shape — image, standard
// environment, internal port, volume mount path and readiness probe — so the
// service never branches on engine names outside the registry.
//
// Containers are always created through the shared internal/containers
// service (ContainerService.Run with RunOptions); this package never talks to
// the node agent directly. Credentials are generated per database and stored
// AES-256-GCM sealed (providers.SealSecret) in the database_secrets table —
// plaintext exists only in the run payload handed to the agent and in the
// response that shows the credentials to their owner.
//
// Deleting a database stops and removes the container and soft-deletes the row
// (deleted_at); the named volume is never removed by that path, so the data
// survives the 7-day grace window. The RetentionSweeper enforces the window:
// on a periodic tick it removes the volume through the agent (RemoveVolume)
// and purges the row, so the documented retention is real.
//
// Backups (BE-5.2) reuse that machinery: a BackupEngine per engine builds the
// temporary container that stops the database, starts the engine on the same
// volume and streams a framed dump (GOTHAM-BACKUP-* markers with a
// length-prefixed payload) back through the container's log stream, which the
// control plane compresses and stores locally or in an S3-compatible bucket
// through the ObjectStore seam (minio-go for S3). Restore reverses it: the
// artifact is staged onto the volume in bounded base64 chunks — the agent
// contract has no file-transfer RPC — then applied by a temporary container
// while the database is stopped. Runs, schedules and storage targets live in
// the backups, backup_schedules and backup_targets tables of 00011_backups;
// target credentials are sealed (providers.SealSecret) in
// backup_target_secrets and never appear in an API response or a log line.
// The internal cron scheduler is a ticker plus one due-time query with a
// single flight per schedule.
//
// The whole surface is disable-able with FEATURE_DATABASES=false: the routes do
// not mount and no new database can be created.
package databases
