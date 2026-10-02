package databases

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// dumpPayload is what the scripted dump job "produces": the collector turns
// it into the artifact, so tests can assert the stored bytes round-trip.
const dumpPayload = "CREATE TABLE orders (id bigint PRIMARY KEY);\nINSERT INTO orders VALUES (1);\n"

// testDatabase builds a live database row of the given engine. The storage
// path follows the same convention the provisioning path uses, because every
// job container mounts it.
func testDatabase(engine, version string) Database {
	id := uuid.New()
	now := time.Now().UTC()
	return Database{
		ID:          id,
		UserID:      uuid.New(),
		ServerID:    uuid.New(),
		Name:        "test-" + engine,
		Engine:      engine,
		Version:     version,
		Status:      StatusRunning,
		ContainerID: "db-container",
		StoragePath: VolumeName(id),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// testCredentials returns the shape every engine expects.
func testCredentials() Credentials {
	return Credentials{
		Username:     "gotham_app",
		Password:     "s3cret-password",
		Database:     "gotham_app",
		RootPassword: "root-password",
	}
}

// containsEnv reports whether the env list carries the given prefix.
func containsEnv(env []string, prefix string) bool {
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// envValue returns the value of a KEY=VALUE entry.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
	}
	return ""
}

// defaultJobLogs scripts a temporary container: dump jobs stream the payload
// framed, restore and staging jobs announce an empty payload. The run id
// comes from the job's own environment, exactly like the real script reads it.
func defaultJobLogs(opts containers.RunOptions) [][]byte {
	runID := envValue(opts.Env, "GOTHAM_RUN_ID")
	switch opts.Labels[labelRole] {
	case roleStage, roleRestore:
		return [][]byte{[]byte(
			jobStartPrefix + runID + "\n" +
				jobPayloadPrefix + runID + " 0\n" +
				jobEndPrefix + runID + " ok\n")}
	default:
		return [][]byte{jobFrameB64(runID, []byte(dumpPayload))}
	}
}

// fakeBackupRepository is a scriptable in-memory BackupRepository.
type fakeBackupRepository struct {
	mu sync.Mutex

	backups       map[uuid.UUID]Backup
	backupOrder   []uuid.UUID
	schedules     map[uuid.UUID]BackupSchedule
	scheduleOrder []uuid.UUID
	targets       map[uuid.UUID]BackupTarget
	targetOrder   []uuid.UUID
	secrets       map[uuid.UUID][]TargetSecret
	// serverIDs is what ListServerIDs answers for the container sweep.
	serverIDs []uuid.UUID
	// finishHook runs inside FinishBackup, before the row is written, so a
	// test can assert an ordering (for example that the artifact was flushed
	// before the run was marked completed).
	finishHook func(Backup)

	createBackupErr   error
	getBackupErr      error
	listBackupErr     error
	finishBackupErr   error
	deleteBackupErr   error
	dueErr            error
	createScheduleErr error
	markErr           error
	createTargetErr   error
	secretErr         error
}

// Compile-time guarantee.
var _ BackupRepository = (*fakeBackupRepository)(nil)

func newFakeBackupRepository() *fakeBackupRepository {
	return &fakeBackupRepository{
		backups:   make(map[uuid.UUID]Backup),
		schedules: make(map[uuid.UUID]BackupSchedule),
		targets:   make(map[uuid.UUID]BackupTarget),
		secrets:   make(map[uuid.UUID][]TargetSecret),
	}
}

// seedBackup stores a run directly, defaulting to a completed one.
func (r *fakeBackupRepository) seedBackup(backup Backup) Backup {
	r.mu.Lock()
	defer r.mu.Unlock()
	if backup.ID == uuid.Nil {
		backup.ID = uuid.New()
	}
	if backup.Status == "" {
		backup.Status = BackupCompleted
	}
	if backup.Type == "" {
		backup.Type = BackupManual
	}
	if backup.CreatedAt.IsZero() {
		backup.CreatedAt = time.Now().UTC()
	}
	r.backups[backup.ID] = backup
	r.backupOrder = append(r.backupOrder, backup.ID)
	return backup
}

// seedSchedule stores a schedule directly.
func (r *fakeBackupRepository) seedSchedule(schedule BackupSchedule) BackupSchedule {
	r.mu.Lock()
	defer r.mu.Unlock()
	if schedule.ID == uuid.Nil {
		schedule.ID = uuid.New()
	}
	if schedule.Cron == "" {
		schedule.Cron = "0 2 * * *"
	}
	if schedule.NextRunAt.IsZero() {
		schedule.NextRunAt = time.Now().UTC().Add(-time.Minute)
	}
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = time.Now().UTC()
	}
	r.schedules[schedule.ID] = schedule
	r.scheduleOrder = append(r.scheduleOrder, schedule.ID)
	return schedule
}

// seedTarget stores a target directly.
func (r *fakeBackupRepository) seedTarget(target BackupTarget) BackupTarget {
	r.mu.Lock()
	defer r.mu.Unlock()
	if target.ID == uuid.Nil {
		target.ID = uuid.New()
	}
	if target.Kind == "" {
		target.Kind = TargetS3
	}
	r.targets[target.ID] = target
	r.targetOrder = append(r.targetOrder, target.ID)
	return target
}

// getBackup is the test-side accessor.
func (r *fakeBackupRepository) getBackup(id uuid.UUID) (Backup, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	backup, ok := r.backups[id]
	return backup, ok
}

// getSchedule is the test-side accessor.
func (r *fakeBackupRepository) getSchedule(id uuid.UUID) (BackupSchedule, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule, ok := r.schedules[id]
	return schedule, ok
}

// countBackups reports how many rows exist.
func (r *fakeBackupRepository) countBackups() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.backups)
}

// CreateBackup implements BackupRepository.
func (r *fakeBackupRepository) CreateBackup(_ context.Context, backup Backup) (Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createBackupErr != nil {
		return Backup{}, r.createBackupErr
	}
	return r.seedBackupLocked(backup), nil
}

// seedBackupLocked is seedBackup for callers already holding the lock.
func (r *fakeBackupRepository) seedBackupLocked(backup Backup) Backup {
	if backup.ID == uuid.Nil {
		backup.ID = uuid.New()
	}
	if backup.CreatedAt.IsZero() {
		backup.CreatedAt = time.Now().UTC()
	}
	r.backups[backup.ID] = backup
	r.backupOrder = append(r.backupOrder, backup.ID)
	return backup
}

// GetBackup implements BackupRepository.
func (r *fakeBackupRepository) GetBackup(_ context.Context, backupID uuid.UUID) (Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getBackupErr != nil {
		return Backup{}, r.getBackupErr
	}
	backup, ok := r.backups[backupID]
	if !ok {
		return Backup{}, ErrNotFound
	}
	return backup, nil
}

// ListBackupsByDatabase implements BackupRepository, newest first, capped at
// limit (zero means unbounded, matching the repository seam).
func (r *fakeBackupRepository) ListBackupsByDatabase(_ context.Context, databaseID uuid.UUID, limit int) ([]Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listBackupErr != nil {
		return nil, r.listBackupErr
	}
	list := make([]Backup, 0, len(r.backupOrder))
	for i := len(r.backupOrder) - 1; i >= 0; i-- {
		backup := r.backups[r.backupOrder[i]]
		if backup.DatabaseID == databaseID {
			list = append(list, backup)
			if limit > 0 && len(list) >= limit {
				break
			}
		}
	}
	return list, nil
}

// ListRunningBackups implements BackupRepository.
func (r *fakeBackupRepository) ListRunningBackups(_ context.Context) ([]Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listBackupErr != nil {
		return nil, r.listBackupErr
	}
	list := make([]Backup, 0, len(r.backupOrder))
	for _, id := range r.backupOrder {
		backup := r.backups[id]
		if backup.Status == BackupRunning {
			list = append(list, backup)
		}
	}
	return list, nil
}

// FinishBackup implements BackupRepository.
func (r *fakeBackupRepository) FinishBackup(_ context.Context, backup Backup) (Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finishBackupErr != nil {
		return Backup{}, r.finishBackupErr
	}
	if _, ok := r.backups[backup.ID]; !ok {
		return Backup{}, ErrNotFound
	}
	if r.finishHook != nil {
		r.finishHook(backup)
	}
	r.backups[backup.ID] = backup
	return backup, nil
}

// DeleteBackup implements BackupRepository.
func (r *fakeBackupRepository) DeleteBackup(_ context.Context, backupID uuid.UUID) (Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteBackupErr != nil {
		return Backup{}, r.deleteBackupErr
	}
	backup, ok := r.backups[backupID]
	if !ok {
		return Backup{}, ErrNotFound
	}
	delete(r.backups, backupID)
	for i, id := range r.backupOrder {
		if id == backupID {
			r.backupOrder = append(r.backupOrder[:i], r.backupOrder[i+1:]...)
			break
		}
	}
	return backup, nil
}

// CreateBackupSchedule implements BackupRepository.
func (r *fakeBackupRepository) CreateBackupSchedule(_ context.Context, schedule BackupSchedule) (BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createScheduleErr != nil {
		return BackupSchedule{}, r.createScheduleErr
	}
	if schedule.ID == uuid.Nil {
		schedule.ID = uuid.New()
	}
	r.schedules[schedule.ID] = schedule
	r.scheduleOrder = append(r.scheduleOrder, schedule.ID)
	return schedule, nil
}

// GetBackupSchedule implements BackupRepository.
func (r *fakeBackupRepository) GetBackupSchedule(_ context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule, ok := r.schedules[scheduleID]
	if !ok {
		return BackupSchedule{}, ErrNotFound
	}
	return schedule, nil
}

// ListBackupSchedulesByDatabase implements BackupRepository, newest first.
func (r *fakeBackupRepository) ListBackupSchedulesByDatabase(_ context.Context, databaseID uuid.UUID) ([]BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]BackupSchedule, 0, len(r.scheduleOrder))
	for i := len(r.scheduleOrder) - 1; i >= 0; i-- {
		schedule := r.schedules[r.scheduleOrder[i]]
		if schedule.DatabaseID == databaseID {
			list = append(list, schedule)
		}
	}
	return list, nil
}

// UpdateBackupSchedule implements BackupRepository.
func (r *fakeBackupRepository) UpdateBackupSchedule(_ context.Context, schedule BackupSchedule) (BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.schedules[schedule.ID]; !ok {
		return BackupSchedule{}, ErrNotFound
	}
	schedule.UpdatedAt = time.Now().UTC()
	r.schedules[schedule.ID] = schedule
	return schedule, nil
}

// DeleteBackupSchedule implements BackupRepository.
func (r *fakeBackupRepository) DeleteBackupSchedule(_ context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule, ok := r.schedules[scheduleID]
	if !ok {
		return BackupSchedule{}, ErrNotFound
	}
	delete(r.schedules, scheduleID)
	for i, id := range r.scheduleOrder {
		if id == scheduleID {
			r.scheduleOrder = append(r.scheduleOrder[:i], r.scheduleOrder[i+1:]...)
			break
		}
	}
	return schedule, nil
}

// ListDueBackupSchedules implements BackupRepository: enabled and due.
func (r *fakeBackupRepository) ListDueBackupSchedules(_ context.Context, now time.Time) ([]BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dueErr != nil {
		return nil, r.dueErr
	}
	due := make([]BackupSchedule, 0, len(r.scheduleOrder))
	for _, id := range r.scheduleOrder {
		schedule := r.schedules[id]
		if schedule.Enabled && !schedule.NextRunAt.After(now) {
			due = append(due, schedule)
		}
	}
	return due, nil
}

// MarkBackupScheduleRun implements BackupRepository.
func (r *fakeBackupRepository) MarkBackupScheduleRun(_ context.Context, scheduleID uuid.UUID, lastRun, nextRun time.Time) (BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.markErr != nil {
		return BackupSchedule{}, r.markErr
	}
	schedule, ok := r.schedules[scheduleID]
	if !ok {
		return BackupSchedule{}, ErrNotFound
	}
	schedule.LastRunAt = lastRun
	schedule.NextRunAt = nextRun
	schedule.UpdatedAt = time.Now().UTC()
	r.schedules[scheduleID] = schedule
	return schedule, nil
}

// CreateBackupTarget implements BackupRepository.
func (r *fakeBackupRepository) CreateBackupTarget(_ context.Context, target BackupTarget) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createTargetErr != nil {
		return BackupTarget{}, r.createTargetErr
	}
	if target.ID == uuid.Nil {
		target.ID = uuid.New()
	}
	for _, id := range r.targetOrder {
		existing := r.targets[id]
		if existing.UserID == target.UserID && existing.Name == target.Name {
			return BackupTarget{}, ErrConflict
		}
	}
	r.targets[target.ID] = target
	r.targetOrder = append(r.targetOrder, target.ID)
	return target, nil
}

// CreateBackupTargetWithSecrets implements BackupRepository as one atomic
// step: a credential failure leaves no target behind.
func (r *fakeBackupRepository) CreateBackupTargetWithSecrets(_ context.Context, target BackupTarget, secrets []TargetSecret) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createTargetErr != nil {
		return BackupTarget{}, r.createTargetErr
	}
	if r.secretErr != nil {
		return BackupTarget{}, r.secretErr
	}
	for _, id := range r.targetOrder {
		existing := r.targets[id]
		if existing.UserID == target.UserID && existing.Name == target.Name {
			return BackupTarget{}, ErrConflict
		}
	}
	if target.ID == uuid.Nil {
		target.ID = uuid.New()
	}
	r.targets[target.ID] = target
	r.targetOrder = append(r.targetOrder, target.ID)
	for _, secret := range secrets {
		if secret.TargetID == uuid.Nil {
			secret.TargetID = target.ID
		}
		r.upsertSecretLocked(secret)
	}
	return target, nil
}

// GetBackupTarget implements BackupRepository.
func (r *fakeBackupRepository) GetBackupTarget(_ context.Context, targetID uuid.UUID) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, ok := r.targets[targetID]
	if !ok {
		return BackupTarget{}, ErrNotFound
	}
	return target, nil
}

// ListBackupTargetsByUser implements BackupRepository, newest first.
func (r *fakeBackupRepository) ListBackupTargetsByUser(_ context.Context, userID uuid.UUID) ([]BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]BackupTarget, 0, len(r.targetOrder))
	for i := len(r.targetOrder) - 1; i >= 0; i-- {
		target := r.targets[r.targetOrder[i]]
		if target.UserID == userID {
			list = append(list, target)
		}
	}
	return list, nil
}

// UpdateBackupTarget implements BackupRepository.
func (r *fakeBackupRepository) UpdateBackupTarget(_ context.Context, target BackupTarget) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.targets[target.ID]; !ok {
		return BackupTarget{}, ErrNotFound
	}
	r.targets[target.ID] = target
	return target, nil
}

// UpdateBackupTargetWithSecrets implements BackupRepository as one atomic
// step: a credential failure leaves the previous configuration in place.
func (r *fakeBackupRepository) UpdateBackupTargetWithSecrets(_ context.Context, target BackupTarget, secrets []TargetSecret) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.targets[target.ID]; !ok {
		return BackupTarget{}, ErrNotFound
	}
	if r.secretErr != nil {
		return BackupTarget{}, r.secretErr
	}
	r.targets[target.ID] = target
	for _, secret := range secrets {
		if secret.TargetID == uuid.Nil {
			secret.TargetID = target.ID
		}
		r.upsertSecretLocked(secret)
	}
	return target, nil
}

// HasBackupsForTarget implements BackupRepository.
func (r *fakeBackupRepository) HasBackupsForTarget(_ context.Context, targetID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, backup := range r.backups {
		if backup.TargetID == targetID && (backup.Status == BackupCompleted || backup.Status == BackupRunning) {
			return true, nil
		}
	}
	return false, nil
}

// ListServerIDs implements backupServerLister for the container sweep.
func (r *fakeBackupRepository) ListServerIDs(_ context.Context) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]uuid.UUID, len(r.serverIDs))
	copy(ids, r.serverIDs)
	return ids, nil
}

// DeleteBackupTarget implements BackupRepository with the owner filter of the
// SQL query.
func (r *fakeBackupRepository) DeleteBackupTarget(_ context.Context, targetID, userID uuid.UUID) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, ok := r.targets[targetID]
	if !ok || target.UserID != userID {
		return BackupTarget{}, ErrNotFound
	}
	delete(r.targets, targetID)
	for i, id := range r.targetOrder {
		if id == targetID {
			r.targetOrder = append(r.targetOrder[:i], r.targetOrder[i+1:]...)
			break
		}
	}
	return target, nil
}

// CreateTargetSecret implements BackupRepository.
func (r *fakeBackupRepository) CreateTargetSecret(_ context.Context, secret TargetSecret) (TargetSecret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secretErr != nil {
		return TargetSecret{}, r.secretErr
	}
	if secret.ID == uuid.Nil {
		secret.ID = uuid.New()
	}
	r.secrets[secret.TargetID] = append(r.secrets[secret.TargetID], secret)
	return secret, nil
}

// UpsertTargetSecret implements BackupRepository.
func (r *fakeBackupRepository) UpsertTargetSecret(_ context.Context, secret TargetSecret) (TargetSecret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secretErr != nil {
		return TargetSecret{}, r.secretErr
	}
	return r.upsertSecretLocked(secret), nil
}

// upsertSecretLocked is the replace-or-insert of one sealed credential for
// callers already holding the lock.
func (r *fakeBackupRepository) upsertSecretLocked(secret TargetSecret) TargetSecret {
	stored := r.secrets[secret.TargetID]
	for i, item := range stored {
		if item.Key == secret.Key {
			secret.ID = item.ID
			stored[i] = secret
			r.secrets[secret.TargetID] = stored
			return secret
		}
	}
	if secret.ID == uuid.Nil {
		secret.ID = uuid.New()
	}
	r.secrets[secret.TargetID] = append(stored, secret)
	return secret
}

// ListTargetSecrets implements BackupRepository.
func (r *fakeBackupRepository) ListTargetSecrets(_ context.Context, targetID uuid.UUID) ([]TargetSecret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secretErr != nil {
		return nil, r.secretErr
	}
	stored := r.secrets[targetID]
	secrets := make([]TargetSecret, len(stored))
	copy(secrets, stored)
	return secrets, nil
}

// getSecrets is the test-side accessor for a target's sealed rows.
func (r *fakeBackupRepository) getSecrets(targetID uuid.UUID) []TargetSecret {
	secrets, err := r.ListTargetSecrets(context.Background(), targetID)
	if err != nil {
		return nil
	}
	return secrets
}

// backupFixture wires a BackupManager over fakes: one database, its
// repository, a scriptable container service and a local backup directory.
type backupFixture struct {
	manager    *BackupManager
	databases  *fakeRepository
	backups    *fakeBackupRepository
	containers *fakeContainers
	dir        string
	userID     uuid.UUID
	serverID   uuid.UUID
	database   Database
}

// newBackupFixture builds the fixture with a silent logger.
func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	return newBackupFixtureWith(t, discardLogger())
}

// newBackupFixtureWith builds the fixture with the given logger so a test can
// assert on what was written.
func newBackupFixtureWith(t *testing.T, logger *slog.Logger) *backupFixture {
	t.Helper()

	databaseRepo := newFakeRepository()
	serverID := databaseRepo.seedServer()
	fixture := &backupFixture{
		databases:  databaseRepo,
		backups:    newFakeBackupRepository(),
		containers: &fakeContainers{},
		dir:        t.TempDir(),
		userID:     uuid.New(),
		serverID:   serverID,
	}
	database := testDatabase(EnginePostgres, "")
	database.UserID = fixture.userID
	database.ServerID = serverID
	fixture.database = databaseRepo.seed(database)
	fixture.containers.setRunning(database.ContainerID)
	fixture.containers.logFn = defaultJobLogs

	fixture.manager = NewBackupService(BackupConfig{
		Repository:         fixture.backups,
		DatabaseRepository: databaseRepo,
		Containers:         fixture.containers,
		Secret:             testSecret,
		Logger:             logger,
		LocalDir:           fixture.dir,
		JobTimeout:         30 * time.Second,
		SchedulerInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = fixture.manager.Close() })
	return fixture
}

// waitBackup blocks until the run reaches a terminal state (or the deadline).
func (f *backupFixture) waitBackup(t *testing.T, backupID uuid.UUID) Backup {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if backup, ok := f.backups.getBackup(backupID); ok && backup.Status != BackupRunning {
			return backup
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("backup %s never left the running state", backupID)
	return Backup{}
}

// queueBackup runs CreateBackup and waits for the job to finish.
func (f *backupFixture) queueBackup(t *testing.T) Backup {
	t.Helper()
	queued, err := f.manager.CreateBackup(context.Background(), f.userID, f.database.ID, CreateBackupRequest{})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	return f.waitBackup(t, queued.ID)
}

// readArtifact gunzips a stored local artifact and returns its bytes.
func readArtifact(t *testing.T, location string) []byte {
	t.Helper()
	path := strings.TrimPrefix(location, locationFilePrefix)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	return data
}

// waitFor blocks until cond holds, failing the test when it never does.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// fakeObjectStore is the S3 seam: it records what was written instead of
// talking to a network.
type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    []string
	gets    []string
	deletes []string
	putErr  error
	getErr  error
}

// Compile-time guarantee.
var _ ObjectStore = (*fakeObjectStore)(nil)

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string][]byte)}
}

// Kind implements ObjectStore.
func (f *fakeObjectStore) Kind() string { return "s3" }

// Put implements ObjectStore.
func (f *fakeObjectStore) Put(_ context.Context, key string, data io.Reader, size int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return "", f.putErr
	}
	payload, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	if size >= 0 && int64(len(payload)) != size {
		return "", fmt.Errorf("size mismatch: %d != %d", len(payload), size)
	}
	f.objects[key] = payload
	f.puts = append(f.puts, key)
	return locationS3Prefix + "bucket/" + key, nil
}

// Get implements ObjectStore.
func (f *fakeObjectStore) Get(_ context.Context, location string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	key := strings.TrimPrefix(location, locationS3Prefix+"bucket/")
	payload, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("%w: object missing", ErrNotFound)
	}
	f.gets = append(f.gets, location)
	return io.NopCloser(bytes.NewReader(payload)), nil
}

// Delete implements ObjectStore.
func (f *fakeObjectStore) Delete(_ context.Context, location string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(location, locationS3Prefix+"bucket/")
	delete(f.objects, key)
	f.deletes = append(f.deletes, location)
	return nil
}
