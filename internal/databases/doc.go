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
// (deleted_at); the named volume is never removed by this path, so the data
// survives the 7-day grace window of the phase rollback note.
//
// The whole surface is disable-able with FEATURE_DATABASES=false: the routes do
// not mount and no new database can be created.
package databases
