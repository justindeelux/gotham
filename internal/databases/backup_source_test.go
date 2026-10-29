package databases

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/backups"
	"github.com/justindeelux/gotham/internal/containers"
)

// The driver owns everything database-specific: the engine dump, staging,
// pause/resume and credentials. These tests exercise it directly; the core
// lifecycle is covered in internal/backups.

const driverDumpPayload = "CREATE TABLE orders (id bigint PRIMARY KEY);\nINSERT INTO orders VALUES (1);\n"

func driverJobLogs(opts containers.RunOptions) [][]byte {
	runID := envValue(opts.Env, "GOTHAM_RUN_ID")
	switch opts.Labels[labelRole] {
	case roleStage, roleRestore:
		return [][]byte{[]byte(
			jobStartPrefix + runID + "\n" +
				jobPayloadPrefix + runID + " 0\n" +
				jobEndPrefix + runID + " ok\n")}
	default:
		return [][]byte{jobFrameB64(runID, []byte(driverDumpPayload))}
	}
}

type driverFixture struct {
	service    *Service
	source     *BackupSource
	databases  *fakeRepository
	containers *fakeContainers
	database   Database
	userID     uuid.UUID
}

func newDriverFixture(t *testing.T) *driverFixture {
	t.Helper()
	repo := newFakeRepository()
	serverID := repo.seedServer()
	database := testDatabase(EnginePostgres, "")
	database.UserID = uuid.New()
	database.ServerID = serverID
	seeded := repo.seed(database)
	cs := &fakeContainers{logFn: driverJobLogs}
	cs.setRunning(seeded.ContainerID)
	svc := NewService(Config{
		Repository: repo,
		Containers: cs,
		Secret:     testSecret,
		Logger:     discardLogger(),
	})
	return &driverFixture{
		service:    svc,
		source:     NewBackupSource(svc),
		databases:  repo,
		containers: cs,
		database:   seeded,
		userID:     seeded.UserID,
	}
}

func (f *driverFixture) resource() backups.Resource {
	return backups.Resource{
		Type:    backups.ResourceDatabase,
		ID:      f.database.ID,
		OwnerID: f.database.UserID,
		Name:    f.database.Name,
	}
}

func (f *driverFixture) capture(t *testing.T) (string, backups.CaptureResult) {
	t.Helper()
	var buffer bytes.Buffer
	result, err := f.source.Capture(context.Background(), backups.CaptureRequest{
		Resource: f.resource(),
		RunID:    uuid.New(),
		Sink:     &buffer,
		Recorder: driverRecorder{repo: f.databases},
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	// The driver writes the raw dump into the core's sink; the core owns the
	// gzip staging file and the upload.
	return buffer.String(), result
}

type driverRecorder struct {
	repo *fakeRepository
}

func (r driverRecorder) SetWasRunning(context.Context, uuid.UUID, bool) error { return nil }

func TestBackupSourceCaptureStreamsTheDump(t *testing.T) {
	fixture := newDriverFixture(t)
	payload, result := fixture.capture(t)

	if payload != driverDumpPayload {
		t.Errorf("payload = %q, want the dump", payload)
	}
	if result.Extension != "dump" {
		t.Errorf("extension = %q, want the postgres dump extension", result.Extension)
	}
	if result.ContainerID == "" {
		t.Error("the job container id must be reported")
	}

	fixture.containers.mu.Lock()
	stops, starts, runs := fixture.containers.stops, fixture.containers.starts, len(fixture.containers.runs)
	removes := len(fixture.containers.removes)
	fixture.containers.mu.Unlock()
	if stops != 1 || starts != 1 {
		t.Errorf("stops = %d, starts = %d, want the database paused and resumed", stops, starts)
	}
	if runs != 1 || removes == 0 {
		t.Errorf("runs = %d, removes = %d, want one temporary job", runs, removes)
	}
}

func TestBackupSourceCaptureLeavesStoppedDatabaseStopped(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.containers.mu.Lock()
	fixture.containers.listed = nil
	fixture.containers.mu.Unlock()

	if _, err := fixture.source.Capture(context.Background(), backups.CaptureRequest{
		Resource: fixture.resource(),
		RunID:    uuid.New(),
		Sink:     io.Discard,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	fixture.containers.mu.Lock()
	stops, starts := fixture.containers.stops, fixture.containers.starts
	fixture.containers.mu.Unlock()
	if stops != 0 || starts != 0 {
		t.Errorf("stops = %d, starts = %d, want neither: the database was already down", stops, starts)
	}
}

func TestBackupSourceCaptureFailureResumesAndReports(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.containers.logFn = func(opts containers.RunOptions) [][]byte {
		runID := envValue(opts.Env, "GOTHAM_RUN_ID")
		return [][]byte{[]byte(
			jobStartPrefix + runID + "\n" +
				jobEndPrefix + runID + " fail 7\n" +
				"pg_dump: error: could not connect\n")}
	}

	_, err := fixture.source.Capture(context.Background(), backups.CaptureRequest{
		Resource: fixture.resource(),
		RunID:    uuid.New(),
		Sink:     io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "status 7") || !strings.Contains(err.Error(), "could not connect") {
		t.Fatalf("err = %v, want the job status and diagnostics", err)
	}
	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	fixture.containers.mu.Unlock()
	if starts != 1 {
		t.Errorf("starts = %d, want the database back up after the failure", starts)
	}
}

func TestBackupSourceRejectsUnknownResourceType(t *testing.T) {
	fixture := newDriverFixture(t)
	_, err := fixture.source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceService, ID: uuid.New()})
	if !errors.Is(err, backups.ErrUnsupportedResource) {
		t.Fatalf("err = %v, want ErrUnsupportedResource", err)
	}
}

func TestBackupSourceResolveReportsMissingDatabase(t *testing.T) {
	fixture := newDriverFixture(t)
	_, err := fixture.source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceDatabase, ID: uuid.New()})
	if !errors.Is(err, backups.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	resource, err := fixture.source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceDatabase, ID: fixture.database.ID})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resource.OwnerID != fixture.database.UserID || resource.Name != fixture.database.Name {
		t.Errorf("resource = %+v, want the seeded database identity", resource)
	}
}

func TestBackupSourceRestoreStagesArtifactAndRunsJob(t *testing.T) {
	fixture := newDriverFixture(t)
	artifact := gzipBytes(t, []byte("compressed dump"))
	backupID := uuid.New()

	if err := fixture.source.Restore(context.Background(), backups.RestoreRequest{
		Resource: fixture.resource(),
		BackupID: backupID,
		Artifact: bytes.NewReader(artifact),
	}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	fixture.containers.mu.Lock()
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	stops, starts := fixture.containers.stops, fixture.containers.starts
	fixture.containers.mu.Unlock()

	var staged, restored *containers.RunOptions
	for i := range runs {
		switch runs[i].Labels[labelRole] {
		case roleStage:
			staged = &runs[i]
		case roleRestore:
			restored = &runs[i]
		}
	}
	if staged == nil || restored == nil {
		t.Fatalf("runs = %d, want a staging and a restore container", len(runs))
	}
	pattern := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)'`)
	match := pattern.FindStringSubmatch(staged.Command[2])
	if match == nil {
		t.Fatalf("staging script has no payload: %s", staged.Command[2])
	}
	decoded, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatalf("decode staged chunk: %v", err)
	}
	if !bytes.Equal(decoded, artifact) {
		t.Errorf("staged %d bytes, artifact is %d bytes", len(decoded), len(artifact))
	}
	if want := fixture.database.StoragePath + ":/var/lib/postgresql/data"; len(restored.Volumes) != 1 || restored.Volumes[0] != want {
		t.Errorf("volumes = %v, want [%s]", restored.Volumes, want)
	}
	if stagedPath := envValue(restored.Env, "GOTHAM_STAGED"); !strings.Contains(stagedPath, stagingDirName) {
		t.Errorf("GOTHAM_STAGED = %q, want a path inside %s", stagedPath, stagingDirName)
	}
	if stops < 1 || starts < 1 {
		t.Errorf("stops = %d, starts = %d, want the database paused and resumed", stops, starts)
	}
}

func TestBackupSourceFailedRestoreLeavesDatabaseStopped(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.containers.logFn = func(opts containers.RunOptions) [][]byte {
		runID := envValue(opts.Env, "GOTHAM_RUN_ID")
		if opts.Labels[labelRole] == roleRestore {
			return [][]byte{[]byte(
				jobStartPrefix + runID + "\n" +
					jobEndPrefix + runID + " fail 1\n" +
					"engine: restore died midway\n")}
		}
		return driverJobLogs(opts)
	}

	err := fixture.source.Restore(context.Background(), backups.RestoreRequest{
		Resource: fixture.resource(),
		BackupID: uuid.New(),
		Artifact: bytes.NewReader(gzipBytes(t, []byte("dump"))),
	})
	if err == nil {
		t.Fatal("expected the restore to fail")
	}

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want the database left down after a partial restore", starts)
	}
	cleanups := 0
	for _, opts := range runs {
		if opts.Labels[labelRole] == roleStage && strings.Contains(opts.Name, "cleanup") {
			cleanups++
		}
	}
	if cleanups == 0 {
		t.Error("no staged-artifact cleanup container ran after the failed restore")
	}

	database, err := fixture.databases.GetDatabase(context.Background(), fixture.database.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if database.Status != StatusError {
		t.Errorf("database status = %q, want %q", database.Status, StatusError)
	}
}

func TestPG18MountPaths(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "postgres16", version: "16-alpine", want: postgresLegacyMountPath},
		{name: "postgres18", version: "18-alpine", want: postgres18MountPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDatabase(EnginePostgres, tt.version)
			credentials := Credentials{Username: "app", Password: "pw", Database: "appdb"}

			opts, err := backupJobOptions(db, credentials, "gotham-backup-abc", roleBackup, "run-1", "", "true")
			if err != nil {
				t.Fatalf("backupJobOptions: %v", err)
			}
			want := db.StoragePath + ":" + tt.want
			if len(opts.Volumes) != 1 || opts.Volumes[0] != want {
				t.Errorf("backup volumes = %v, want [%s]", opts.Volumes, want)
			}

			dir, err := stagingDir(db)
			if err != nil {
				t.Fatalf("stagingDir: %v", err)
			}
			if wantDir := strings.TrimRight(tt.want, "/") + "/" + stagingDirName; dir != wantDir {
				t.Errorf("stagingDir = %q, want %q", dir, wantDir)
			}
		})
	}
}

func TestStageChunkUsesTheRightMountAndOperator(t *testing.T) {
	fixture := newDriverFixture(t)
	database := fixture.database
	database.Version = "18-alpine"
	want := database.StoragePath + ":" + postgres18MountPath
	staged := postgres18MountPath + "/" + stagingDirName + "/" + database.ID.String() + ".part"

	if err := fixture.source.stageChunk(context.Background(), database, database.ID, []byte("chunk-0"), staged, 0); err != nil {
		t.Fatalf("stageChunk: %v", err)
	}
	if err := fixture.source.stageChunk(context.Background(), database, database.ID, []byte("chunk-1"), staged, 1); err != nil {
		t.Fatalf("stageChunk index 1: %v", err)
	}

	fixture.containers.mu.Lock()
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	fixture.containers.mu.Unlock()
	if len(runs) != 2 {
		t.Fatalf("staging runs = %d, want 2", len(runs))
	}
	for i, run := range runs {
		if len(run.Volumes) != 1 || run.Volumes[0] != want {
			t.Errorf("run %d volumes = %v, want [%s]", i, run.Volumes, want)
		}
	}
	if !strings.Contains(runs[0].Command[2], "base64 -d > '"+staged+"'") {
		t.Errorf("first chunk does not decode into the staged file:\n%s", runs[0].Command[2])
	}
	if !strings.Contains(runs[1].Command[2], "base64 -d >> '"+staged+"'") {
		t.Errorf("later chunks do not decode with append:\n%s", runs[1].Command[2])
	}
}

func TestStageChunkFitsArgumentLimit(t *testing.T) {
	const maxArgStrlen = 128 * 1024
	const scriptOverhead = 4096
	encoded := (stageChunkBytes/3 + 1) * 4
	if encoded+scriptOverhead >= maxArgStrlen {
		t.Fatalf("chunk encodes to %d bytes; with %d overhead it must stay under %d",
			encoded, scriptOverhead, maxArgStrlen)
	}
}

func TestBackupSourceRecoverResumesOnlyWhenTheRunStoppedIt(t *testing.T) {
	for _, tt := range []struct {
		name       string
		wasRunning bool
		wantStarts int
	}{
		{name: "was running", wasRunning: true, wantStarts: 1},
		{name: "was stopped", wasRunning: false, wantStarts: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newDriverFixture(t)
			if err := fixture.source.Recover(context.Background(), backups.RecoverRequest{
				Resource:   fixture.resource(),
				BackupID:   uuid.New(),
				WasRunning: tt.wasRunning,
			}); err != nil {
				t.Fatalf("Recover: %v", err)
			}
			fixture.containers.mu.Lock()
			starts := fixture.containers.starts
			fixture.containers.mu.Unlock()
			if starts != tt.wantStarts {
				t.Errorf("starts = %d, want %d", starts, tt.wantStarts)
			}
		})
	}
}

func TestBackupSourceRecoverMarksInterruptedRestore(t *testing.T) {
	fixture := newDriverFixture(t)
	if err := fixture.source.Recover(context.Background(), backups.RecoverRequest{
		Resource:   fixture.resource(),
		BackupID:   uuid.New(),
		WasRestore: true,
	}); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	database, err := fixture.databases.GetDatabase(context.Background(), fixture.database.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if database.Status != StatusError {
		t.Errorf("database status = %q, want %q after an interrupted restore", database.Status, StatusError)
	}
}

func TestDriverRecordsWasRunning(t *testing.T) {
	fixture := newDriverFixture(t)
	var recorded []bool
	recorder := &recordingDriverRecorder{recorded: &recorded}
	if _, err := fixture.source.Capture(context.Background(), backups.CaptureRequest{
		Resource: fixture.resource(),
		RunID:    uuid.New(),
		Sink:     io.Discard,
		Recorder: recorder,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(recorded) != 1 || !recorded[0] {
		t.Errorf("recorded = %v, want the running database before the pause", recorded)
	}
}

type recordingDriverRecorder struct {
	recorded *[]bool
	err      error
}

func (r *recordingDriverRecorder) SetWasRunning(_ context.Context, _ uuid.UUID, running bool) error {
	if r.err != nil {
		return r.err
	}
	*r.recorded = append(*r.recorded, running)
	return nil
}

func TestCaptureAbortsBeforeStoppingWhenStateCannotBeRecorded(t *testing.T) {
	fixture := newDriverFixture(t)
	recorder := &recordingDriverRecorder{recorded: new([]bool), err: errors.New("metadata store unavailable")}
	_, err := fixture.source.Capture(context.Background(), backups.CaptureRequest{
		Resource: fixture.resource(),
		RunID:    uuid.New(),
		Sink:     io.Discard,
		Recorder: recorder,
	})
	if err == nil || !strings.Contains(err.Error(), "record the pre-backup database state") {
		t.Fatalf("err = %v, want the record-write cause", err)
	}
	fixture.containers.mu.Lock()
	stops := fixture.containers.stops
	fixture.containers.mu.Unlock()
	if stops != 0 {
		t.Errorf("stops = %d, want 0: the database was stopped before its state was recorded", stops)
	}
}

func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buffer.Bytes()
}
