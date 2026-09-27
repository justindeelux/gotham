package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Phase 5 (BE-5.2) backup acceptance suite. It drives the production HTTP
// surface (databases + backups) through the real control-plane services onto a
// real node agent and the local Docker daemon: create a managed PostgreSQL,
// back it up, drop only the disposable table, restore, and prove the rows come
// back byte for byte. It reuses the Phase 4 harness vocabulary (agent, store,
// Redis-free container service) instead of inventing another framework.
//
// Every test needs GOTHAM_E2E=1 and a reachable Postgres + Docker; a missing
// precondition skips the test rather than failing a plain `go test ./...`.
const (
	p5Secret = p4Secret

	// p5StagingChunkProxy is the acceptance proxy for "larger than one staging
	// chunk": the restore stages an artifact 90 000 raw bytes at a time
	// (databases.stageChunkBytes), so an artifact above 97 KiB necessarily
	// spans at least two chunks.
	p5StagingChunkProxy = 97 << 10

	// p5JobWait bounds one dump/restore job and the polling that waits for it.
	p5JobWait      = 12 * time.Minute
	p5PollInterval = 1 * time.Second
	p5SQLWait      = 5 * time.Minute

	// p5MinioImage is the disposable S3 endpoint. The upstream minio/minio
	// image is no longer published on Docker Hub; elestio/minio is the
	// maintained mirror and speaks the same S3 API.
	p5MinioImage = "elestio/minio:latest"
)

// p5Suffix returns a short, Docker-safe, per-run identifier. Every resource
// this suite creates (node, database, tables, MinIO container, bucket, target)
// carries it so parallel workers on the same host cannot collide.
func p5Suffix() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
}

// p5Harness is one control plane under test: the databases and backups HTTP
// surface wired to a real PostgreSQL store, a real node agent and the local
// Docker daemon. BackupManager resolves every object store itself, so a local
// directory and an S3 target are exercised through the production code path.
type p5Harness struct {
	baseURL  string
	client   *http.Client
	serverID uuid.UUID
}

// p5Database is the database half of the API wire format.
type p5Database struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Engine      string `json:"engine"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	ServerID    string `json:"server_id"`
	ContainerID string `json:"container_id"`
	PublicPort  int32  `json:"public_port"`
	Volume      string `json:"volume"`
}

// p5CreateDatabaseResponse carries the row and the generated credentials.
type p5CreateDatabaseResponse struct {
	Database    p5Database            `json:"database"`
	Credentials databases.Credentials `json:"credentials"`
}

// p5Backup is the backup half of the API wire format.
type p5Backup struct {
	ID         string `json:"id"`
	DatabaseID string `json:"database_id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Size       int64  `json:"size"`
	Location   string `json:"location"`
	Error      string `json:"error"`
}

// p5BackupEnvelope wraps a single backup.
type p5BackupEnvelope struct {
	Backup p5Backup `json:"backup"`
}

// p5BackupList wraps a backup list.
type p5BackupList struct {
	Backups []p5Backup `json:"backups"`
}

// p5RestoreEnvelope wraps a queued restore.
type p5RestoreEnvelope struct {
	Restore struct {
		BackupID   string `json:"backup_id"`
		DatabaseID string `json:"database_id"`
		Location   string `json:"location"`
		Status     string `json:"status"`
	} `json:"restore"`
}

// p5Target is the storage-target wire format.
type p5Target struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	Prefix         string `json:"prefix"`
	HasCredentials bool   `json:"has_credentials"`
}

// p5TargetEnvelope wraps a single target.
type p5TargetEnvelope struct {
	Target p5Target `json:"target"`
}

// p5TargetCheck is the answer of the target connection test.
type p5TargetCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// p5TargetCheckEnvelope wraps a target connection test.
type p5TargetCheckEnvelope struct {
	Check p5TargetCheck `json:"check"`
}

// newP5Harness boots the backup stack for one Phase 5 test: real PostgreSQL
// (migrated), the local Docker daemon, one node agent over mTLS, the databases
// and backups domain services and the HTTP surface they are mounted on.
func newP5Harness(t *testing.T) *p5Harness {
	t.Helper()
	requireE2E(t)
	// The databases surface must be on, whatever the ambient environment says.
	t.Setenv(databases.FeatureEnv, "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. PostgreSQL: the suite proves the real schema — databases, secrets,
	// backups and targets — so an unreachable database skips the test.
	dsn := p4DSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available at %s: %v (run: docker compose -f deploy/compose.dev.yml up -d)", dsn, err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available at %s: %v", dsn, err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	// 2. Docker: the agent creates the database and runs every job container
	// on the local daemon.
	engine, err := agent.NewDockerClient(e2eDockerSock())
	if err != nil {
		t.Fatalf("docker client for %s: %v", e2eDockerSock(), err)
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = engine.Version(versionCtx)
	versionCancel()
	if err != nil {
		t.Skipf("docker daemon unreachable at %s: %v (start Docker and run: docker compose -f deploy/compose.dev.yml up -d)",
			e2eDockerSock(), err)
	}

	// 3. The node: one agent serving DockerService over mTLS.
	suffix := p5Suffix()
	nodeID := "p5-backup-smoke-" + suffix
	agentCtx, agentCancel := context.WithCancel(context.Background())
	t.Cleanup(agentCancel)
	agentAddr, authority := startLocalAgent(t, agentCtx, engine, nodeID)

	// 4. Rows: the caller and the node the database is assigned to.
	user, err := st.CreateUser(ctx, fmt.Sprintf("p5-backup-smoke-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p5-backup-smoke-node-" + suffix,
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		// Deleting the user cascades to its databases, secrets, backups and
		// targets; the node row has no owner and goes separately.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server row: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user row: %v", err)
		}
	})

	// 5. The shared container service, dialing the local agent exactly as the
	// control plane does. NopCache keeps the run isolated from the shared dev
	// Redis and from a parallel worker on the same box.
	containersSvc := containers.NewService(containers.Config{
		Registry: &staticRegistry{server: &servers.Server{ID: serverID}},
		Cache:    containers.NopCache{},
		Logger:   testLogger(t),
		Dial: func(dialCtx context.Context, _ *servers.Server) (containers.DockerClient, error) {
			return servers.DialDockerClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
	})
	t.Cleanup(func() { _ = containersSvc.Close() })

	// 6. The domain services and the HTTP surface, wired the way
	// internal/server wires them. The scheduler is disabled so the suite drives
	// every run itself.
	localDir := t.TempDir()
	databaseSvc := databases.NewDefaultService(databases.Config{
		Store:      st,
		Containers: containersSvc,
		Secret:     p5Secret,
		Logger:     testLogger(t),
	})
	backupSvc := databases.NewDefaultBackupService(databases.BackupConfig{
		Store:            st,
		Containers:       containersSvc,
		Secret:           p5Secret,
		Logger:           testLogger(t),
		LocalDir:         localDir,
		DisableScheduler: true,
		JobTimeout:       p5JobWait,
	})
	if databaseSvc == nil || backupSvc == nil {
		t.Fatal("databases feature reported disabled although FEATURE_DATABASES is unset")
	}
	t.Cleanup(func() { _ = backupSvc.Close() })

	userIDFunc := func(context.Context) (uuid.UUID, bool) { return userID, true }
	requireAuth := func(next http.Handler) http.Handler { return next }
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		databases.Mount(r, requireAuth, userIDFunc, databaseSvc)
		databases.MountBackups(r, requireAuth, userIDFunc, backupSvc)
	})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	return &p5Harness{
		baseURL:  httpServer.URL + "/api",
		client:   &http.Client{Timeout: 60 * time.Second},
		serverID: serverID,
	}
}

// api performs one API call against the harness control plane. body is sent as
// JSON when non-nil; out is decoded only for a 2xx response, so error payloads
// come back as raw text for the failure message.
func (h *p5Harness) api(t *testing.T, method, path string, body, out any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s body: %v", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, h.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || out == nil {
		return response.StatusCode, string(raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s %s response (%d): %v: %s", method, path, response.StatusCode, err, raw)
	}
	return response.StatusCode, string(raw)
}

// createDatabase provisions a managed database through the API and registers
// cleanup of only the resources it owns: the container and volume of this
// database id, never another worker's.
func (h *p5Harness) createDatabase(t *testing.T, name, engine string) (p5Database, databases.Credentials) {
	t.Helper()
	body := map[string]any{"name": name, "engine": engine, "server_id": h.serverID.String()}
	var out p5CreateDatabaseResponse
	status, raw := h.api(t, http.MethodPost, "/v1/databases", body, &out)
	if status != http.StatusCreated {
		t.Fatalf("create database: status %d: %s", status, raw)
	}
	databaseID, err := uuid.Parse(out.Database.ID)
	if err != nil {
		t.Fatalf("parse database id %q: %v", out.Database.ID, err)
	}
	if out.Database.ContainerID == "" {
		t.Fatalf("created database %s has no container id", out.Database.ID)
	}
	h.trackDatabase(t, databaseID)
	return out.Database, out.Credentials
}

// trackDatabase removes the container(s) and the volume of one database id
// when the test ends. Job containers carry the same gotham.db_id label, so a
// leaked staging or dump container is caught too.
func (h *p5Harness) trackDatabase(t *testing.T, databaseID uuid.UUID) {
	t.Helper()
	label := "gotham.db_id=" + databaseID.String()
	volume := "gotham-db-" + databaseID.String()
	t.Cleanup(func() {
		removeLabelledContainers(t, label)
		docker, err := exec.LookPath("docker")
		if err != nil {
			t.Logf("cleanup: docker CLI not found, remove volume %s manually", volume)
			return
		}
		if out, err := exec.Command(docker, "volume", "rm", "-f", volume).CombinedOutput(); err != nil {
			t.Logf("cleanup: docker volume rm %s: %v: %s", volume, err, strings.TrimSpace(string(out)))
		}
	})
}

// createBackup queues a manual backup, to the local directory when targetID is
// empty. 202 is the acceptance answer: the row is recorded and the job runs.
func (h *p5Harness) createBackup(t *testing.T, databaseID, targetID string) p5Backup {
	t.Helper()
	body := map[string]any{}
	if targetID != "" {
		body["target_id"] = targetID
	}
	var out p5BackupEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/databases/"+databaseID+"/backup", body, &out)
	if status != http.StatusAccepted {
		t.Fatalf("create backup: status %d: %s", status, raw)
	}
	return out.Backup
}

// waitBackup polls the backup list until the named run reaches a terminal
// state and returns it.
func (h *p5Harness) waitBackup(t *testing.T, databaseID, backupID string) p5Backup {
	t.Helper()
	deadline := time.Now().Add(p5JobWait)
	last := "not listed"
	for {
		var list p5BackupList
		status, raw := h.api(t, http.MethodGet, "/v1/databases/"+databaseID+"/backups", nil, &list)
		if status != http.StatusOK {
			t.Fatalf("list backups: status %d: %s", status, raw)
		}
		for _, backup := range list.Backups {
			if backup.ID != backupID {
				continue
			}
			if backup.Status == "completed" || backup.Status == "failed" {
				return backup
			}
			last = backup.Status
		}
		if time.Now().After(deadline) {
			t.Fatalf("backup %s never reached a terminal state; last observed %q", backupID, last)
		}
		time.Sleep(p5PollInterval)
	}
}

// restore queues a restore of a completed backup and asserts the accepted
// shape.
func (h *p5Harness) restore(t *testing.T, databaseID, backupID string) {
	t.Helper()
	body := map[string]string{"backup_id": backupID}
	var out p5RestoreEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/databases/"+databaseID+"/restore", body, &out)
	if status != http.StatusAccepted {
		t.Fatalf("restore: status %d: %s", status, raw)
	}
	if out.Restore.BackupID != backupID {
		t.Fatalf("restore echoed backup id %q, want %q", out.Restore.BackupID, backupID)
	}
	if out.Restore.Status != "running" {
		t.Fatalf("restore queued with status %q, want running", out.Restore.Status)
	}
}

// createTarget stores an S3 target pointing at the disposable MinIO and
// asserts the endpoint is reported back with its explicit scheme.
func (h *p5Harness) createTarget(t *testing.T, minioSrv *p5Minio) p5Target {
	t.Helper()
	body := map[string]any{
		"name":       "p5-smoke-minio-" + p5Suffix(),
		"kind":       "s3",
		"endpoint":   minioSrv.apiEndpoint,
		"region":     "us-east-1",
		"bucket":     minioSrv.bucket,
		"access_key": minioSrv.accessKey,
		"secret_key": minioSrv.secretKey,
	}
	var out p5TargetEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/databases/backup-targets", body, &out)
	if status != http.StatusCreated {
		t.Fatalf("create target: status %d: %s", status, raw)
	}
	if !out.Target.HasCredentials {
		t.Fatalf("s3 target %s reports no credentials", out.Target.ID)
	}
	if out.Target.Endpoint != minioSrv.apiEndpoint {
		t.Fatalf("target endpoint %q, want %q", out.Target.Endpoint, minioSrv.apiEndpoint)
	}
	return out.Target
}

// testTarget runs the target connection check and returns its answer.
func (h *p5Harness) testTarget(t *testing.T, targetID string) p5TargetCheck {
	t.Helper()
	var out p5TargetCheckEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/databases/backup-targets/"+targetID+"/test", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("test target: status %d: %s", status, raw)
	}
	return out.Check
}

// psql runs one SQL statement inside the managed database container and
// returns its trimmed output. It reads through the real running container, so
// a stopped or still-initialising engine surfaces as an error the caller can
// poll through.
func (h *p5Harness) psql(t *testing.T, containerID string, creds databases.Credentials, sql string) (string, error) {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatalf("docker CLI not found: %v", err)
	}
	args := []string{
		"exec", "-i", "-e", "PGPASSWORD=" + creds.Password, containerID,
		"psql", "-v", "ON_ERROR_STOP=1", "-U", creds.Username, "-d", creds.Database, "-tAc", sql,
	}
	out, err := exec.Command(docker, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// psqlWait runs sql until it succeeds, failing the test after p5SQLWait.
func (h *p5Harness) psqlWait(t *testing.T, containerID string, creds databases.Credentials, sql string) string {
	t.Helper()
	deadline := time.Now().Add(p5SQLWait)
	var last string
	for time.Now().Before(deadline) {
		out, err := h.psql(t, containerID, creds, sql)
		if err == nil {
			return out
		}
		last = err.Error() + ": " + out
		time.Sleep(p5PollInterval)
	}
	t.Fatalf("psql never succeeded within %s; last: %s", p5SQLWait, last)
	return ""
}

// psqlEventually polls sql until it returns want, failing the test after
// p5SQLWait. It is the restore-completion signal: the expected checksum can
// only appear once the restore job has applied the artifact.
func (h *p5Harness) psqlEventually(t *testing.T, containerID string, creds databases.Credentials, sql, want string) {
	t.Helper()
	deadline := time.Now().Add(p5SQLWait)
	last := "no successful read"
	for time.Now().Before(deadline) {
		out, err := h.psql(t, containerID, creds, sql)
		if err == nil {
			if out == want {
				return
			}
			last = out
		} else {
			last = err.Error() + ": " + out
		}
		time.Sleep(p5PollInterval)
	}
	t.Fatalf("psql output never became %q within %s; last: %s", want, p5SQLWait, last)
}

// p5RowChecksumSQL builds the stability check of a table: its row count and the
// md5 of its ordered rows. Comparing it before a backup and after a restore
// proves the bytes survived the round trip, not just that some rows exist.
func p5RowChecksumSQL(table, valueExpr string) string {
	return "SELECT count(*)::text || ':' || coalesce(md5(string_agg(id::text || '|' || (" +
		valueExpr + "), ',' ORDER BY id)), 'empty') FROM " + table
}

// TestP5BackupLocalRoundTrip is the BE-5.2 exit criterion: create a managed
// PostgreSQL through the API, back it up locally, drop only the disposable
// table, restore and verify the rows and their checksum are identical.
func TestP5BackupLocalRoundTrip(t *testing.T) {
	h := newP5Harness(t)
	suffix := p5Suffix()
	database, creds := h.createDatabase(t, "p5smoke-local-"+suffix, "postgres")
	containerID := database.ContainerID
	table := "p5smoke_rows_" + suffix

	// Seed a deterministic table and remember its exact checksum.
	h.psqlWait(t, containerID, creds,
		"CREATE TABLE "+table+" (id integer PRIMARY KEY, payload text NOT NULL)")
	h.psqlWait(t, containerID, creds,
		"INSERT INTO "+table+" SELECT g, repeat('row-' || g || '-payload-', 8) FROM generate_series(1, 200) AS g")
	checksumSQL := p5RowChecksumSQL(table, "payload")
	before := h.psqlWait(t, containerID, creds, checksumSQL)
	if !strings.HasPrefix(before, "200:") {
		t.Fatalf("seeded table reports %q, want 200 rows", before)
	}
	t.Logf("before backup: rows+checksum = %s", before)

	// Back it up to the local backup directory.
	backup := h.waitBackup(t, database.ID, h.createBackup(t, database.ID, "").ID)
	if backup.Status != "completed" {
		t.Fatalf("backup did not complete: status %q error %q", backup.Status, backup.Error)
	}
	if backup.Size <= 0 {
		t.Fatalf("completed backup reports size %d", backup.Size)
	}
	if !strings.HasPrefix(backup.Location, "file://") {
		t.Fatalf("local backup recorded location %q, want file://", backup.Location)
	}
	artifactPath := strings.TrimPrefix(backup.Location, "file://")
	info, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("stat local artifact %s: %v", artifactPath, err)
	}
	if info.Size() != backup.Size {
		t.Fatalf("local artifact is %d bytes, row records %d", info.Size(), backup.Size)
	}
	t.Logf("backup artifact: location=%s bytes=%d", backup.Location, backup.Size)

	// Drop ONLY our own disposable table.
	h.psqlWait(t, containerID, creds, "DROP TABLE "+table)
	gone := h.psqlWait(t, containerID, creds,
		"SELECT count(*) FROM information_schema.tables WHERE table_name = '"+table+"'")
	if gone != "0" {
		t.Fatalf("table %s still exists after drop (count=%s)", table, gone)
	}

	// Restore and wait for the exact checksum to reappear.
	h.restore(t, database.ID, backup.ID)
	h.psqlEventually(t, containerID, creds, checksumSQL, before)
	after := h.psqlWait(t, containerID, creds, checksumSQL)
	t.Logf("after restore: rows+checksum = %s", after)
	if after != before {
		t.Fatalf("rows/checksum changed across backup+restore: before=%s after=%s", before, after)
	}
}

// TestP5BackupChunkScaleArtifact proves the restore staging path survives an
// artifact larger than one 90 000-byte chunk: a table of incompressible random
// bytes produces a stored artifact comfortably above 97 KiB, and the restored
// contents match byte for byte.
func TestP5BackupChunkScaleArtifact(t *testing.T) {
	h := newP5Harness(t)
	suffix := p5Suffix()
	database, creds := h.createDatabase(t, "p5smoke-chunk-"+suffix, "postgres")
	containerID := database.ContainerID
	table := "p5smoke_chunk_" + suffix

	// 400 rows of 1 024 random bytes each: no runs, no repeats, so neither
	// pg_dump's own compression nor the control plane's gzip can shrink it
	// below the staging chunk size.
	h.psqlWait(t, containerID, creds,
		"CREATE TABLE "+table+" (id integer PRIMARY KEY, payload bytea NOT NULL)")
	h.psqlWait(t, containerID, creds,
		"INSERT INTO "+table+" (id, payload) SELECT g, decode(string_agg(md5(random()::text || g::text || s::text), ''), 'hex') "+
			"FROM generate_series(1, 400) AS g CROSS JOIN generate_series(1, 64) AS s GROUP BY g")
	checksumSQL := p5RowChecksumSQL(table, "encode(payload, 'hex')")
	before := h.psqlWait(t, containerID, creds, checksumSQL)
	if !strings.HasPrefix(before, "400:") {
		t.Fatalf("seeded table reports %q, want 400 rows", before)
	}
	t.Logf("before backup: rows+checksum = %s", before)

	backup := h.waitBackup(t, database.ID, h.createBackup(t, database.ID, "").ID)
	if backup.Status != "completed" {
		t.Fatalf("backup did not complete: status %q error %q", backup.Status, backup.Error)
	}
	if backup.Size <= p5StagingChunkProxy {
		t.Fatalf("artifact is %d bytes, not larger than one staging chunk proxy (%d bytes)", backup.Size, p5StagingChunkProxy)
	}
	t.Logf("chunk-scale artifact: bytes=%d (staging chunk proxy=%d, raw staging chunk=%d)",
		backup.Size, p5StagingChunkProxy, 90_000)

	h.psqlWait(t, containerID, creds, "DROP TABLE "+table)
	h.restore(t, database.ID, backup.ID)
	h.psqlEventually(t, containerID, creds, checksumSQL, before)
	after := h.psqlWait(t, containerID, creds, checksumSQL)
	if after != before {
		t.Fatalf("chunk-scale rows/checksum changed across backup+restore: before=%s after=%s", before, after)
	}
	t.Logf("chunk-scale restore verified: rows+checksum = %s", after)
}

// TestP5BackupS3TargetMinIO proves the S3 target path against a disposable
// MinIO over an explicit http:// endpoint: the target test succeeds, the
// backup's object exists in the bucket with the recorded size, and a restore
// from S3 reproduces the rows exactly.
func TestP5BackupS3TargetMinIO(t *testing.T) {
	h := newP5Harness(t)
	minioSrv := startP5Minio(t)
	target := h.createTarget(t, minioSrv)
	check := h.testTarget(t, target.ID)
	if !check.OK {
		t.Fatalf("target connection check failed: %s", check.Message)
	}
	t.Logf("s3 target check: ok=%v message=%q endpoint=%s", check.OK, check.Message, target.Endpoint)

	suffix := p5Suffix()
	database, creds := h.createDatabase(t, "p5smoke-s3-"+suffix, "postgres")
	containerID := database.ContainerID
	table := "p5smoke_s3_" + suffix

	h.psqlWait(t, containerID, creds,
		"CREATE TABLE "+table+" (id integer PRIMARY KEY, payload text NOT NULL)")
	h.psqlWait(t, containerID, creds,
		"INSERT INTO "+table+" SELECT g, repeat('s3-row-' || g || '-', 8) FROM generate_series(1, 150) AS g")
	checksumSQL := p5RowChecksumSQL(table, "payload")
	before := h.psqlWait(t, containerID, creds, checksumSQL)
	t.Logf("before backup: rows+checksum = %s", before)

	backup := h.waitBackup(t, database.ID, h.createBackup(t, database.ID, target.ID).ID)
	if backup.Status != "completed" {
		t.Fatalf("s3 backup did not complete: status %q error %q", backup.Status, backup.Error)
	}
	prefix := "s3://" + minioSrv.bucket + "/"
	if !strings.HasPrefix(backup.Location, prefix) {
		t.Fatalf("backup location %q does not live in bucket %q", backup.Location, minioSrv.bucket)
	}
	objectKey := strings.TrimPrefix(backup.Location, prefix)
	objectSize := minioSrv.statObject(t, objectKey)
	if objectSize != backup.Size {
		t.Fatalf("minio object %s is %d bytes, backup row records %d", objectKey, objectSize, backup.Size)
	}
	t.Logf("s3 object verified: bucket=%s key=%s bytes=%d", minioSrv.bucket, objectKey, objectSize)

	h.psqlWait(t, containerID, creds, "DROP TABLE "+table)
	h.restore(t, database.ID, backup.ID)
	h.psqlEventually(t, containerID, creds, checksumSQL, before)
	after := h.psqlWait(t, containerID, creds, checksumSQL)
	if after != before {
		t.Fatalf("s3 rows/checksum changed across backup+restore: before=%s after=%s", before, after)
	}
	t.Logf("s3 restore verified: rows+checksum = %s", after)
}

// p5Minio is a disposable MinIO server: one container on an ephemeral
// loopback port with a bucket owned by this run. Nothing outside the run is
// touched.
type p5Minio struct {
	apiEndpoint string // "http://127.0.0.1:PORT", the explicit-scheme value
	accessKey   string
	secretKey   string
	bucket      string
	client      *minio.Client
}

// startP5Minio boots elestio/minio on the local Docker daemon, waits for its
// readiness endpoint and creates a private bucket through the S3 API. The
// container is removed when the test ends.
func startP5Minio(t *testing.T) *p5Minio {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skipf("docker CLI not found: %v", err)
	}
	suffix := p5Suffix()
	name := "p5-backup-smoke-minio-" + suffix
	port := freeHostPort(t)
	accessKey := "p5smoke" + suffix
	secretKey := "p5BackupSmokeSecret" + suffix
	bucket := "p5-backup-smoke-" + suffix

	args := []string{
		"run", "-d", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:9000", port),
		"-e", "MINIO_ROOT_USER=" + accessKey,
		"-e", "MINIO_ROOT_PASSWORD=" + secretKey,
		p5MinioImage, "server", "/data",
	}
	if out, err := exec.Command(docker, args...).CombinedOutput(); err != nil {
		t.Fatalf("start minio: %v: %s", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() {
		if out, err := exec.Command(docker, "rm", "-f", name).CombinedOutput(); err != nil {
			t.Logf("cleanup: docker rm -f %s: %v: %s", name, err, strings.TrimSpace(string(out)))
		}
	})

	endpoint := fmt.Sprintf("127.0.0.1:%d", port)
	waitMinioReady(t, endpoint)

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
		Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		exists, existsErr := client.BucketExists(ctx, bucket)
		if existsErr != nil || !exists {
			t.Fatalf("make minio bucket: %v", err)
		}
	}

	return &p5Minio{
		apiEndpoint: "http://" + endpoint,
		accessKey:   accessKey,
		secretKey:   secretKey,
		bucket:      bucket,
		client:      client,
	}
}

// waitMinioReady polls the MinIO readiness endpoint until the server answers.
func waitMinioReady(t *testing.T, endpoint string) {
	t.Helper()
	url := "http://" + endpoint + "/minio/health/ready"
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(60 * time.Second)
	last := "no response"
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
			last = response.Status
		} else {
			last = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("minio at %s never became ready; last: %s", endpoint, last)
}

// statObject returns the size of an object in the disposable bucket, failing
// the test when the object is missing.
func (m *p5Minio) statObject(t *testing.T, key string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("stat minio object %q in %s: %v", key, m.bucket, err)
	}
	return info.Size
}
