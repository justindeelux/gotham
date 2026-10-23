package databases

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

const (
	labelRole     = "gotham.role"
	labelBackupID = "gotham.backup_id"
	roleBackup    = "backup"
	roleRestore   = "restore"
	roleStage     = "restore-stage"
)

const stagingDirName = ".gotham-restore"

const stageChunkBytes = 90_000

type BackupEngine interface {
	Name() string
	DumpExtension() string
	DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error)
	RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error)
}

var backupEngines = map[string]BackupEngine{
	EnginePostgres: postgresBackupEngine{},
	EngineMySQL:    mysqlBackupEngine{},
	EngineMariaDB:  mysqlBackupEngine{mariadb: true},
	EngineMongoDB:  mongoBackupEngine{},
	EngineRedis:    redisBackupEngine{},
}

func LookupBackupEngine(name string) (BackupEngine, bool) {
	engine, ok := backupEngines[strings.ToLower(strings.TrimSpace(name))]
	return engine, ok
}

type postgresBackupEngine struct{}

func (postgresBackupEngine) Name() string          { return EnginePostgres }
func (postgresBackupEngine) DumpExtension() string { return "dump" }

func (e postgresBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

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
  raw=$(wc -c <"$tmp" | tr -d ' ')
  size=$(( (raw + 2) / 3 * 4 ))
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD-B64 %s %s\n' "$id" "$id" "$size"
  base64 "$tmp" | tr -d '\n'
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
archive=/tmp/gotham-restore.bin
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
    if gunzip -c "$payload" >"$archive" 2>>"$log"; then
      pg_restore -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner --single-transaction "$archive" 2>>"$log"
      status=$?
    else
      status=5
    fi
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

type mysqlBackupEngine struct {
	mariadb bool
}

func (e mysqlBackupEngine) Name() string {
	if e.mariadb {
		return EngineMariaDB
	}
	return EngineMySQL
}

func (mysqlBackupEngine) DumpExtension() string { return "sql" }

func (e mysqlBackupEngine) bins() (server, ping, dump, client string) {
	if e.mariadb {
		return "mariadbd", "mariadb-admin", "mariadb-dump", "mariadb"
	}
	return "mysqld", "mysqladmin", "mysqldump", "mysql"
}

func (e mysqlBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

func (e mysqlBackupEngine) RestoreOptions(db Database, c Credentials, runID, stagedPath string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleRestore, runID), roleRestore, runID, stagedPath, e.restoreScript())
}

const mysqlDumpScript = `set -u
id="$GOTHAM_RUN_ID"
log=/tmp/gotham-engine.log
tmp=/tmp/gotham-payload
status=1
/usr/local/bin/docker-entrypoint.sh __SERVER__ >"$log" 2>&1 &
i=0
until __PING__ ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 240 ]; then break; fi
  sleep 1
done
if __PING__ ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; then
  __DUMP__ -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --events --triggers --databases "$MYSQL_DATABASE" >"$tmp" 2>>"$log"
  status=$?
fi
if [ "$status" -eq 0 ]; then
  raw=$(wc -c <"$tmp" | tr -d ' ')
  size=$(( (raw + 2) / 3 * 4 ))
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD-B64 %s %s\n' "$id" "$id" "$size"
  base64 "$tmp" | tr -d '\n'
  printf 'GOTHAM-BACKUP-END %s ok\n' "$id"
else
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-END %s fail %s\n' "$id" "$id" "$status"
  tail -n 40 "$log"
fi
exit "$status"`

const mysqlRestoreScript = `set -u
id="$GOTHAM_RUN_ID"
staged="$GOTHAM_STAGED"
log=/tmp/gotham-engine.log
payload=/tmp/gotham-restore.dump
archive=/tmp/gotham-restore.bin
status=0
mkdir -p /tmp
if [ -f "$staged" ]; then
  cp "$staged" "$payload" || status=$?
  rm -rf "$(dirname "$staged")"
else
  status=3
fi
if [ "$status" -eq 0 ]; then
  /usr/local/bin/docker-entrypoint.sh __SERVER__ >"$log" 2>&1 &
  i=0
  until __PING__ ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 240 ]; then break; fi
    sleep 1
  done
  if __PING__ ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; then
    if gunzip -c "$payload" >"$archive" 2>>"$log"; then
      __CLIENT__ -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" <"$archive" 2>>"$log"
      status=$?
    else
      status=5
    fi
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

func (e mysqlBackupEngine) dumpScript() string {
	server, ping, dump, _ := e.bins()
	return strings.NewReplacer("__SERVER__", server, "__PING__", ping, "__DUMP__", dump).Replace(mysqlDumpScript)
}

func (e mysqlBackupEngine) restoreScript() string {
	server, ping, _, client := e.bins()
	return strings.NewReplacer("__SERVER__", server, "__PING__", ping, "__CLIENT__", client).Replace(mysqlRestoreScript)
}

type mongoBackupEngine struct{}

func (mongoBackupEngine) Name() string          { return EngineMongoDB }
func (mongoBackupEngine) DumpExtension() string { return "archive" }

func (e mongoBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

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
  raw=$(wc -c <"$tmp" | tr -d ' ')
  size=$(( (raw + 2) / 3 * 4 ))
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD-B64 %s %s\n' "$id" "$id" "$size"
  base64 "$tmp" | tr -d '\n'
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
archive=/tmp/gotham-restore.bin
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
    if gunzip -c "$payload" >"$archive" 2>>"$log"; then
      mongorestore --host 127.0.0.1 --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --archive="$archive" --drop 2>>"$log"
      status=$?
    else
      status=5
    fi
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

type redisBackupEngine struct{}

func (redisBackupEngine) Name() string          { return EngineRedis }
func (redisBackupEngine) DumpExtension() string { return "tar" }

func (e redisBackupEngine) DumpOptions(db Database, c Credentials, runID string) (containers.RunOptions, error) {
	return backupJobOptions(db, c, tempJobName(roleBackup, runID), roleBackup, runID, "", e.dumpScript())
}

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
  raw=$(wc -c <"$tmp" | tr -d ' ')
  size=$(( (raw + 2) / 3 * 4 ))
  printf 'GOTHAM-BACKUP-START %s\nGOTHAM-BACKUP-PAYLOAD-B64 %s %s\n' "$id" "$id" "$size"
  base64 "$tmp" | tr -d '\n'
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
archive=/tmp/gotham-restore.bin
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
  if gunzip -c "$payload" >"$archive" 2>>"$log"; then
    tar -xf "$archive" -C /data 2>>"$log"
    status=$?
  else
    status=5
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
	volume := volumeSpecFor(engine, db.Version)
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

func safeJobPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "'\\\n\r\x00")
}

func stagingDir(db Database) (string, error) {
	engine, _, err := parseEngine(db.Engine)
	if err != nil {
		return "", err
	}
	mount := volumeSpecFor(engine, db.Version).MountPath
	if strings.TrimSpace(mount) == "" {
		return "", fmt.Errorf("%w: engine reports no data directory", ErrValidation)
	}
	return strings.TrimRight(mount, "/") + "/" + stagingDirName, nil
}

func stagingPath(db Database, backupID uuid.UUID) (string, error) {
	dir, err := stagingDir(db)
	if err != nil {
		return "", err
	}
	return dir + "/" + backupID.String() + ".part", nil
}
