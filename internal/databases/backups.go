package databases

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// Label keys applied to every temporary backup/restore container. They mirror
// the gotham.* vocabulary of spec.go so an operator can tell a Gotham job
// container apart from a database container on the same node.
const (
	labelRole     = "gotham.role"
	labelBackupID = "gotham.backup_id"
	// roleBackup, roleRestore and roleStage mark the three kinds of temporary
	// container a backup flow creates: the dump job, the restore job and the
	// single chunk that stages one part of the artifact onto the volume.
	roleBackup  = "backup"
	roleRestore = "restore"
	roleStage   = "restore-stage"
)

// stagingDirName is the directory a restore stages its artifact into, inside
// the database volume. It lives under the engine's mount path (the only place
// a temporary container can write for its successor to read) and is removed
// by the restore job before the engine starts, so a fresh volume stays empty
// for initdb/initialisation.
const stagingDirName = ".gotham-restore"

// stageChunkBytes is the raw size of one staging chunk. base64 expands it by
// 4/3 and the encoded chunk travels as a single command argument inside a
// `sh -c` script, so it must respect Linux's per-argument limit
// (MAX_ARG_STRLEN = 32 * PAGE_SIZE = 128 KiB on the common 4 KiB page), not
// just the 2 MiB total ARG_MAX. 90 000 raw bytes encode to 120 000, leaving
// ~8 KiB of script and quoting overhead inside the per-argument budget.
const stageChunkBytes = 90_000

// BackupEngine builds the temporary-container jobs of one engine. The control
// plane never runs a dump tool itself: it stops the database container, runs
// the job on the same node with the database volume mounted, and reads the
// framed payload back from the container's log stream.
//
// Implementations are stateless and shared; every method must be safe for
// concurrent use.
type BackupEngine interface {
	// Name returns the engine key ("postgres", "mysql", ...).
	Name() string
	// DumpExtension is the artifact file extension without compression
	// suffix, e.g. "dump" or "sql".
	DumpExtension() string
	// DumpOptions returns the temporary container that starts the engine on
	// the database volume and writes the framed dump payload to stdout.
	DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error)
	// RestoreOptions returns the temporary container that moves the staged
	// artifact out of the volume, starts the engine and applies it.
	RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error)
}

// backupEngines holds one BackupEngine per supported engine name.
var backupEngines = map[string]BackupEngine{
	EnginePostgres: postgresBackupEngine{},
	EngineMySQL:    mysqlBackupEngine{},
	EngineMariaDB:  mysqlBackupEngine{},
	EngineMongoDB:  mongoBackupEngine{},
	EngineRedis:    redisBackupEngine{},
}

// LookupBackupEngine returns the backup engine registered for name
// (case-insensitive).
func LookupBackupEngine(name string) (BackupEngine, bool) {
	engine, ok := backupEngines[strings.ToLower(strings.TrimSpace(name))]
	return engine, ok
}

// postgresBackupEngine dumps PostgreSQL with pg_dump and restores with
// pg_restore, both against a temporary server the job starts on the mounted
// volume (the agent contract has no Exec RPC, so the tool cannot run inside
// the live database container).
type postgresBackupEngine struct{}

func (postgresBackupEngine) Name() string          { return EnginePostgres }
func (postgresBackupEngine) DumpExtension() string { return "dump" }

// DumpOptions implements BackupEngine.
func (e postgresBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

// RestoreOptions implements BackupEngine.
func (e postgresBackupEngine) RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleRestore, runID), roleRestore, runID, stagedPath, e.restoreScript())
}

func (postgresBackupEngine) dumpScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
log=/tmp/gotham-engine.log
tmp=/tmp/gotham-payload
status=1
/usr/local/bin/docker-entrypoint.sh postgres >"$log" 2>&1 &
i=0
until pg_isready -q -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do
  i=$((i + 1))
  if [ "$i" -gt 180 ]; then break; fi
  sleep 1
done
if pg_isready -q -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"; then
  export PGPASSWORD="$POSTGRES_PASSWORD"
  pg_dump -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc >"$tmp" 2>>"$log"
  status=$?
fi
if [ "$status" -eq 0 ]; then
  size=$(wc -c <"$tmp" | tr -d ' ')
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s %s\n' "$id" "$id" "$size"
  cat "$tmp"
  printf 'GOTHAM-BACKUP-END %s ok\n' "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

func (postgresBackupEngine) restoreScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
staged="$GOTHAM_STAGED"
log=/tmp/gotham-engine.log
payload=/tmp/gotham-restore.dump
status=0
mkdir -p /tmp
if [ -f "$staged" ]; then
  cp "$staged" "$payload" || status=$?
  rm -rf "$(dirname "$staged")"
else
  status=3
fi
if [ "$status" -eq 0 ]; then
  /usr/local/bin/docker-entrypoint.sh postgres >"$log" 2>&1 &
  i=0
  until pg_isready -q -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do
    i=$((i + 1))
    if [ "$i" -gt 180 ]; then break; fi
    sleep 1
  done
  if pg_isready -q -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"; then
    export PGPASSWORD="$POSTGRES_PASSWORD"
    gunzip -c "$payload" 2>>"$log" | pg_restore -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner - 2>>"$log"
    status=$?
  else
    status=4
  fi
fi
if [ "$status" -eq 0 ]; then
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s 0\nGOTHAM-BACKUP-END %s ok\n' "$id" "$id" "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

// mysqlBackupEngine serves MySQL and MariaDB: both images share the same
// environment, entrypoint and tooling, so one implementation backs two
// engine names.
type mysqlBackupEngine struct{}

func (mysqlBackupEngine) Name() string          { return EngineMySQL }
func (mysqlBackupEngine) DumpExtension() string { return "sql" }

// DumpOptions implements BackupEngine.
func (e mysqlBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

// RestoreOptions implements BackupEngine.
func (e mysqlBackupEngine) RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleRestore, runID), roleRestore, runID, stagedPath, e.restoreScript())
}

func (mysqlBackupEngine) dumpScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
log=/tmp/gotham-engine.log
tmp=/tmp/gotham-payload
status=1
/usr/local/bin/docker-entrypoint.sh mysqld >"$log" 2>&1 &
i=0
until mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 240 ]; then break; fi
  sleep 1
done
if mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; then
  mysqldump -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --events --triggers --databases "$MYSQL_DATABASE" >"$tmp" 2>>"$log"
  status=$?
fi
if [ "$status" -eq 0 ]; then
  size=$(wc -c <"$tmp" | tr -d ' ')
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s %s\n' "$id" "$id" "$size"
  cat "$tmp"
  printf 'GOTHAM-BACKUP-END %s ok\n' "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

func (mysqlBackupEngine) restoreScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
staged="$GOTHAM_STAGED"
log=/tmp/gotham-engine.log
payload=/tmp/gotham-restore.dump
status=0
mkdir -p /tmp
if [ -f "$staged" ]; then
  cp "$staged" "$payload" || status=$?
  rm -rf "$(dirname "$staged")"
else
  status=3
fi
if [ "$status" -eq 0 ]; then
  /usr/local/bin/docker-entrypoint.sh mysqld >"$log" 2>&1 &
  i=0
  until mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 240 ]; then break; fi
    sleep 1
  done
  if mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; then
    gunzip -c "$payload" 2>>"$log" | mysql -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE" 2>>"$log"
    status=$?
  else
    status=4
  fi
fi
if [ "$status" -eq 0 ]; then
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s 0\nGOTHAM-BACKUP-END %s ok\n' "$id" "$id" "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

// mongoBackupEngine dumps to a BSON archive (mongodump --archive) and
// restores it with mongorestore --drop.
type mongoBackupEngine struct{}

func (mongoBackupEngine) Name() string          { return EngineMongoDB }
func (mongoBackupEngine) DumpExtension() string { return "archive" }

// DumpOptions implements BackupEngine.
func (e mongoBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

// RestoreOptions implements BackupEngine.
func (e mongoBackupEngine) RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleRestore, runID), roleRestore, runID, stagedPath, e.restoreScript())
}

func (mongoBackupEngine) dumpScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
log=/tmp/gotham-engine.log
tmp=/tmp/gotham-payload
status=1
/usr/local/bin/docker-entrypoint.sh mongod >"$log" 2>&1 &
i=0
until mongosh --quiet --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval 'db.runCommand({ping: 1})' >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 180 ]; then break; fi
  sleep 1
done
if mongosh --quiet --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval 'db.runCommand({ping: 1})' >/dev/null 2>&1; then
  mongodump --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --archive >"$tmp" 2>>"$log"
  status=$?
fi
if [ "$status" -eq 0 ]; then
  size=$(wc -c <"$tmp" | tr -d ' ')
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s %s\n' "$id" "$id" "$size"
  cat "$tmp"
  printf 'GOTHAM-BACKUP-END %s ok\n' "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

func (mongoBackupEngine) restoreScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
staged="$GOTHAM_STAGED"
log=/tmp/gotham-engine.log
payload=/tmp/gotham-restore.dump
status=0
mkdir -p /tmp
if [ -f "$staged" ]; then
  cp "$staged" "$payload" || status=$?
  rm -rf "$(dirname "$staged")"
else
  status=3
fi
if [ "$status" -eq 0 ]; then
  /usr/local/bin/docker-entrypoint.sh mongod >"$log" 2>&1 &
  i=0
  until mongosh --quiet --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval 'db.runCommand({ping: 1})' >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 180 ]; then break; fi
    sleep 1
  done
  if mongosh --quiet --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval 'db.runCommand({ping: 1})' >/dev/null 2>&1; then
    gunzip -c "$payload" 2>>"$log" | mongorestore --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --archive --drop 2>>"$log"
    status=$?
  else
    status=4
  fi
fi
if [ "$status" -eq 0 ]; then
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s 0\nGOTHAM-BACKUP-END %s ok\n' "$id" "$id" "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

// redisBackupEngine has no dump tool to run: the RDB file and the AOF
// directory are copied as a tar stream while the database is stopped, so both
// persistence modes survive the round trip.
type redisBackupEngine struct{}

func (redisBackupEngine) Name() string          { return EngineRedis }
func (redisBackupEngine) DumpExtension() string { return "tar" }

// DumpOptions implements BackupEngine.
func (e redisBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

// RestoreOptions implements BackupEngine.
func (e redisBackupEngine) RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleRestore, runID), roleRestore, runID, stagedPath, e.restoreScript())
}

func (redisBackupEngine) dumpScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
log=/tmp/gotham-engine.log
tmp=/tmp/gotham-payload
status=1
: >"$log"
tar -cf - -C /data . >"$tmp" 2>>"$log"
status=$?
if [ "$status" -eq 0 ]; then
  size=$(wc -c <"$tmp" | tr -d ' ')
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s %s\n' "$id" "$id" "$size"
  cat "$tmp"
  printf 'GOTHAM-BACKUP-END %s ok\n' "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

func (redisBackupEngine) restoreScript() string {
	return `set -u
id="$GOTHAM_RUN_ID"
staged="$GOTHAM_STAGED"
log=/tmp/gotham-engine.log
payload=/tmp/gotham-restore.dump
status=0
mkdir -p /tmp
: >"$log"
if [ -f "$staged" ]; then
  cp "$staged" "$payload" || status=$?
  rm -rf "$(dirname "$staged")"
else
  status=3
fi
if [ "$status" -eq 0 ]; then
  gunzip -c "$payload" 2>>"$log" | tar -xf - -C /data 2>>"$log"
  status=$?
fi
if [ "$status" -eq 0 ]; then
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD %s 0\nGOTHAM-BACKUP-END %s ok\n' "$id" "$id" "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`
}

// backupJobOptions assembles the agent payload of a temporary job: the
// engine's image (so the dump tool the node already has is the one used), the
// standard environment, the database volume and the job's script. Credentials
// arrive in the environment — the same place the live database container
// keeps them — so the script carries no secrets of its own.
func backupJobOptions(
	db Database,
	c Credentials,
	name, role, runID, stagedPath string,
	script string,
) (containers.RunOptions, error) {
	engine, canonical, err := parseEngine(db.Engine)
	if err != nil {
		return containers.RunOptions{}, err
	}
	volume := engine.VolumeSpec()
	if strings.TrimSpace(volume.MountPath) == "" {
		return containers.RunOptions{}, fmt.Errorf("%w: engine reports no data directory", ErrValidation)
	}
	if !strings.HasPrefix(db.StoragePath, volumePrefix) {
		return containers.RunOptions{}, fmt.Errorf("%w: database has no gotham volume", ErrValidation)
	}
	if strings.TrimSpace(runID) == "" {
		return containers.RunOptions{}, fmt.Errorf("%w: backup job needs a run id", ErrValidation)
	}

	labels := map[string]string{
		labelManaged:    "true",
		labelDatabaseID: db.ID.String(),
		labelEngine:     canonical,
		labelRole:       role,
		labelBackupID:   runID,
	}
	env := append([]string{
		"GOTHAM_RUN_ID=" + runID,
	}, engine.EnvSpec(c)...)
	if stagedPath != "" {
		env = append(env, "GOTHAM_STAGED="+stagedPath)
	}

	return containers.RunOptions{
		Image:   engine.Image(db.Version),
		Name:    name,
		Env:     env,
		Command: []string{"sh", "-c", script},
		Labels:  labels,
		Volumes: []string{db.StoragePath + ":" + volume.MountPath},
	}, nil
}

// tempJobName derives the Docker-safe, unique name of a job container:
// "gotham-<role>-<id prefix>", where id is the run (backup) id. The short id
// keeps two runs of the same database from colliding while their containers
// still exist, without ever repeating a name after a crashed job.
func tempJobName(role, id string) string {
	suffix := id
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	suffix = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, suffix)
	name := "gotham-" + role + "-" + suffix
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// safeJobPath reports whether a path can travel inside a single-quoted shell
// argument: no quote, no backslash, no control character. Engine mount paths
// are constants and the file name is a UUID, so this is a guard rather than a
// sanitiser — a path that fails it is rejected, never rewritten.
func safeJobPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "'\\\n\r\x00")
}

// stagingDir returns the directory inside the database volume where a restore
// stages its artifact for the job container to pick up.
func stagingDir(db Database) (string, error) {
	engine, _, err := parseEngine(db.Engine)
	if err != nil {
		return "", err
	}
	mount := engine.VolumeSpec().MountPath
	if strings.TrimSpace(mount) == "" {
		return "", fmt.Errorf("%w: engine reports no data directory", ErrValidation)
	}
	return strings.TrimRight(mount, "/") + "/" + stagingDirName, nil
}

// stagingPath returns the file a backup's artifact is staged into.
func stagingPath(db Database, backupID uuid.UUID) (string, error) {
	dir, err := stagingDir(db)
	if err != nil {
		return "", err
	}
	return dir + "/" + backupID.String() + ".part", nil
}
