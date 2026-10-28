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

const dumpPayload = "CREATE TABLE orders (id bigint PRIMARY KEY);\nINSERT INTO orders VALUES (1);\n"

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

func testCredentials() Credentials {
	return Credentials{
		Username:     "gotham_app",
		Password:     "s3cret-password",
		Database:     "gotham_app",
		RootPassword: "root-password",
	}
}

func strPtr(s string) *string { return &s }

func containsEnv(env []string, prefix string) bool {
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
	}
	return ""
}

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

type fakeBackupRepository struct {
	mu sync.Mutex

	backups                      map[uuid.UUID]Backup
	backupOrder                  []uuid.UUID
	restores                     map[uuid.UUID]Restore
	restoreOrder                 []uuid.UUID
	schedules                    map[uuid.UUID]BackupSchedule
	scheduleOrder                []uuid.UUID
	targets                      map[uuid.UUID]BackupTarget
	targetOrder                  []uuid.UUID
	secrets                      map[uuid.UUID][]TargetSecret
	serverIDs                    []uuid.UUID
	finishHook                   func(Backup)
	beforeCreateBackupWithTarget func()

	createBackupErr   error
	getBackupErr      error
	listBackupErr     error
	finishBackupErr   error
	deleteBackupErr   error
	setWasRunningErr  error
	listRestoresErr   error
	createRestoreErr  error
	finishRestoreErr  error
	dueErr            error
	createScheduleErr error
	markErr           error
	createTargetErr   error
	secretErr         error
}

var _ BackupRepository = (*fakeBackupRepository)(nil)

func newFakeBackupRepository() *fakeBackupRepository {
	return &fakeBackupRepository{
		backups:   make(map[uuid.UUID]Backup),
		restores:  make(map[uuid.UUID]Restore),
		schedules: make(map[uuid.UUID]BackupSchedule),
		targets:   make(map[uuid.UUID]BackupTarget),
		secrets:   make(map[uuid.UUID][]TargetSecret),
	}
}

func (r *fakeBackupRepository) seedRestore(restore Restore) Restore {
	r.mu.Lock()
	defer r.mu.Unlock()
	if restore.ID == uuid.Nil {
		restore.ID = uuid.New()
	}
	if restore.Status == "" {
		restore.Status = RestoreCompleted
	}
	if restore.CreatedAt.IsZero() {
		restore.CreatedAt = time.Now().UTC()
	}
	r.restores[restore.ID] = restore
	r.restoreOrder = append(r.restoreOrder, restore.ID)
	return restore
}

func (r *fakeBackupRepository) getRestore(id uuid.UUID) (Restore, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	restore, ok := r.restores[id]
	return restore, ok
}

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
	if schedule.MaxRunSeconds == 0 {
		schedule.MaxRunSeconds = DefaultMaxBackupRunSeconds
	}
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = time.Now().UTC()
	}
	r.schedules[schedule.ID] = schedule
	r.scheduleOrder = append(r.scheduleOrder, schedule.ID)
	return schedule
}

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

func (r *fakeBackupRepository) getBackup(id uuid.UUID) (Backup, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	backup, ok := r.backups[id]
	return backup, ok
}

func (r *fakeBackupRepository) getSchedule(id uuid.UUID) (BackupSchedule, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule, ok := r.schedules[id]
	return schedule, ok
}

func (r *fakeBackupRepository) countBackups() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.backups)
}

func (r *fakeBackupRepository) CreateBackup(_ context.Context, backup Backup) (Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createBackupErr != nil {
		return Backup{}, r.createBackupErr
	}
	return r.seedBackupLocked(backup), nil
}

func (r *fakeBackupRepository) CreateBackupWithTarget(_ context.Context, backup Backup, validate func(*BackupTarget) error) (Backup, *BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createBackupErr != nil {
		return Backup{}, nil, r.createBackupErr
	}
	if backup.TargetID == uuid.Nil {
		return r.seedBackupLocked(backup), nil, nil
	}
	if r.beforeCreateBackupWithTarget != nil {
		r.beforeCreateBackupWithTarget()
	}
	target, ok := r.targets[backup.TargetID]
	if !ok {
		return Backup{}, nil, ErrNotFound
	}
	if validate != nil {
		if err := validate(&target); err != nil {
			return Backup{}, nil, err
		}
	}
	stored := r.seedBackupLocked(backup)
	return stored, &target, nil
}

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

func (r *fakeBackupRepository) FinishBackup(_ context.Context, backup Backup) (Backup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finishBackupErr != nil {
		return Backup{}, r.finishBackupErr
	}
	stored, ok := r.backups[backup.ID]
	if !ok {
		return Backup{}, ErrNotFound
	}
	if r.finishHook != nil {
		r.finishHook(backup)
	}
	stored.Status = backup.Status
	stored.Size = backup.Size
	stored.Location = backup.Location
	stored.Error = backup.Error
	stored.ContainerID = backup.ContainerID
	stored.FinishedAt = backup.FinishedAt
	r.backups[backup.ID] = stored
	return stored, nil
}

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

func (r *fakeBackupRepository) SetBackupWasRunning(_ context.Context, backupID uuid.UUID, wasRunning bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setWasRunningErr != nil {
		return r.setWasRunningErr
	}
	backup, ok := r.backups[backupID]
	if !ok {
		return ErrNotFound
	}
	backup.WasRunning = wasRunning
	r.backups[backupID] = backup
	return nil
}

func (r *fakeBackupRepository) CreateRestore(_ context.Context, restore Restore) (Restore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createRestoreErr != nil {
		return Restore{}, r.createRestoreErr
	}
	if restore.ID == uuid.Nil {
		restore.ID = uuid.New()
	}
	if restore.CreatedAt.IsZero() {
		restore.CreatedAt = time.Now().UTC()
	}
	r.restores[restore.ID] = restore
	r.restoreOrder = append(r.restoreOrder, restore.ID)
	return restore, nil
}

func (r *fakeBackupRepository) FinishRestore(_ context.Context, restore Restore) (Restore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finishRestoreErr != nil {
		return Restore{}, r.finishRestoreErr
	}
	stored, ok := r.restores[restore.ID]
	if !ok {
		return Restore{}, ErrNotFound
	}
	stored.Status = restore.Status
	stored.Error = restore.Error
	stored.FinishedAt = restore.FinishedAt
	r.restores[restore.ID] = stored
	return stored, nil
}

func (r *fakeBackupRepository) ListRunningRestores(_ context.Context) ([]Restore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listRestoresErr != nil {
		return nil, r.listRestoresErr
	}
	list := make([]Restore, 0, len(r.restoreOrder))
	for _, id := range r.restoreOrder {
		if restore := r.restores[id]; restore.Status == RestoreRunning {
			list = append(list, restore)
		}
	}
	return list, nil
}

func (r *fakeBackupRepository) ListRestoresByDatabase(_ context.Context, databaseID uuid.UUID, limit int) ([]Restore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listRestoresErr != nil {
		return nil, r.listRestoresErr
	}
	list := make([]Restore, 0, len(r.restoreOrder))
	for i := len(r.restoreOrder) - 1; i >= 0; i-- {
		restore := r.restores[r.restoreOrder[i]]
		if restore.DatabaseID == databaseID {
			list = append(list, restore)
		}
		if limit > 0 && len(list) == limit {
			break
		}
	}
	return list, nil
}

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

func (r *fakeBackupRepository) GetBackupSchedule(_ context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule, ok := r.schedules[scheduleID]
	if !ok {
		return BackupSchedule{}, ErrNotFound
	}
	return schedule, nil
}

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
	schedule.LastSlotAt = schedule.NextRunAt
	schedule.LastRunAt = lastRun
	schedule.NextRunAt = nextRun
	schedule.UpdatedAt = time.Now().UTC()
	r.schedules[scheduleID] = schedule
	return schedule, nil
}

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

func (r *fakeBackupRepository) GetBackupTarget(_ context.Context, targetID uuid.UUID) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, ok := r.targets[targetID]
	if !ok {
		return BackupTarget{}, ErrNotFound
	}
	return target, nil
}

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

func (r *fakeBackupRepository) UpdateBackupTarget(_ context.Context, target BackupTarget) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.targets[target.ID]; !ok {
		return BackupTarget{}, ErrNotFound
	}
	r.targets[target.ID] = target
	return target, nil
}

func (r *fakeBackupRepository) UpdateBackupTargetWithSecrets(_ context.Context, target BackupTarget, secrets []TargetSecret) (BackupTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.targets[target.ID]
	if !ok {
		return BackupTarget{}, ErrNotFound
	}
	if r.secretErr != nil {
		return BackupTarget{}, r.secretErr
	}
	if fakeDestinationChanged(current, target) {
		for _, backup := range r.backups {
			if backup.TargetID == target.ID && (backup.Status == BackupRunning || backup.Status == BackupCompleted) {
				return BackupTarget{}, ErrTargetStranded
			}
		}
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

func fakeDestinationChanged(current, next BackupTarget) bool {
	return current.Kind != next.Kind ||
		current.Endpoint != next.Endpoint ||
		current.Region != next.Region ||
		current.Bucket != next.Bucket
}

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

func (r *fakeBackupRepository) ListServerIDs(_ context.Context) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]uuid.UUID, len(r.serverIDs))
	copy(ids, r.serverIDs)
	return ids, nil
}

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

func (r *fakeBackupRepository) UpsertTargetSecret(_ context.Context, secret TargetSecret) (TargetSecret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secretErr != nil {
		return TargetSecret{}, r.secretErr
	}
	return r.upsertSecretLocked(secret), nil
}

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

func (r *fakeBackupRepository) getSecrets(targetID uuid.UUID) []TargetSecret {
	secrets, err := r.ListTargetSecrets(context.Background(), targetID)
	if err != nil {
		return nil
	}
	return secrets
}

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

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	return newBackupFixtureWith(t, discardLogger())
}

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

func (f *backupFixture) queueBackup(t *testing.T) Backup {
	t.Helper()
	queued, err := f.manager.CreateBackup(context.Background(), f.userID, f.database.ID, CreateBackupRequest{})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	return f.waitBackup(t, queued.ID)
}

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

type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    []string
	gets    []string
	deletes []string
	putErr  error
	getErr  error
}

var _ ObjectStore = (*fakeObjectStore)(nil)

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: make(map[string][]byte)}
}

func (f *fakeObjectStore) Kind() string { return "s3" }

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

func (f *fakeObjectStore) Delete(_ context.Context, location string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(location, locationS3Prefix+"bucket/")
	delete(f.objects, key)
	f.deletes = append(f.deletes, location)
	return nil
}
