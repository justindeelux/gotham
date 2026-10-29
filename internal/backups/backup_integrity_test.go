package backups

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func writeLegacyArtifact(t *testing.T, dir string, payload []byte) string {
	t.Helper()
	path := filepath.Join(dir, "legacy.gz")
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		t.Fatalf("write legacy artifact: %v", err)
	}
	return locationFilePrefix + path
}

func waitRestore(t *testing.T, fixture *backupFixture, restoreID uuid.UUID) Restore {
	t.Helper()
	waitFor(t, "the restore to finish", func() bool {
		restore, ok := fixture.backups.getRestore(restoreID)
		return ok && restore.Status != RestoreRunning
	})
	restore, _ := fixture.backups.getRestore(restoreID)
	return restore
}

func sourceRestoreCount(fixture *backupFixture) int {
	fixture.source.mu.Lock()
	defer fixture.source.mu.Unlock()
	return len(fixture.source.restores)
}

func verifyTempFilesGone(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".verify-*"))
	if err != nil {
		t.Fatalf("glob verification files: %v", err)
	}
	if len(matches) > 0 {
		t.Errorf("verification temp files left behind: %v", matches)
	}
}

func TestNewBackupCarriesAChecksumOfTheStoredArtifact(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s)", backup.Status, backup.Error)
	}
	if backup.ChecksumAlgo != ChecksumAlgoSHA256 || backup.ChecksumSHA256 == "" {
		t.Fatalf("checksum = %q/%q, want a sha256 value", backup.ChecksumAlgo, backup.ChecksumSHA256)
	}
	data, err := os.ReadFile(strings.TrimPrefix(backup.Location, locationFilePrefix))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	sum := sha256.Sum256(data)
	if backup.ChecksumSHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("checksum = %q, want %q", backup.ChecksumSHA256, hex.EncodeToString(sum[:]))
	}
	if backup.Size != int64(len(data)) {
		t.Errorf("size = %d, want %d", backup.Size, len(data))
	}
	if !newBackupResponse(backup).Verified {
		t.Error("a checksummed backup must be reported as verified")
	}
}

func TestRestoreRejectsCorruptedArtifactBeforeTheDriverRuns(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	// The core gate is resource-agnostic: it must hold for a database, an
	// application and a service before each driver stops its resource.
	for _, kind := range []ResourceType{ResourceDatabase, ResourceApplication, ResourceService} {
		fixture.manager.sources[kind] = fixture.source
		queued, err := fixture.manager.CreateBackup(ctx, fixture.userID, CreateBackupRequest{
			Resource: ResourceRef{Type: kind, ID: uuid.New()},
		})
		if err != nil {
			t.Fatalf("CreateBackup(%s): %v", kind, err)
		}
		backup := fixture.waitBackup(t, queued.ID)

		path := strings.TrimPrefix(backup.Location, locationFilePrefix)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		data[len(data)/2] ^= 0xff
		if err := os.WriteFile(path, data, 0o640); err != nil {
			t.Fatalf("corrupt artifact: %v", err)
		}

		result, err := fixture.manager.RestoreBackup(ctx, fixture.userID, backup.ID)
		if err != nil {
			t.Fatalf("RestoreBackup(%s): %v", kind, err)
		}
		restore := waitRestore(t, fixture, result.RestoreID)
		if restore.Status != RestoreFailed || !strings.Contains(restore.Error, "damaged") {
			t.Errorf("%s restore = %+v, want a damaged-backup failure", kind, restore)
		}
		verifyTempFilesGone(t, fixture.dir)
	}
	if count := sourceRestoreCount(fixture); count != 0 {
		t.Errorf("driver restores = %d, want 0: the resource must not be touched", count)
	}
}

func TestRestoreRejectsCorruptedS3Artifact(t *testing.T) {
	fixture := newBackupFixture(t)
	store := newFakeObjectStore()
	fixture.manager.objects = store
	backup := fixture.queueBackup(t)

	key := strings.TrimPrefix(backup.Location, locationS3Prefix+"bucket/")
	store.mu.Lock()
	data := append([]byte(nil), store.objects[key]...)
	data[len(data)/2] ^= 0xff
	store.objects[key] = data
	store.mu.Unlock()

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	restore := waitRestore(t, fixture, result.RestoreID)
	if restore.Status != RestoreFailed || !strings.Contains(restore.Error, "damaged") {
		t.Errorf("restore = %+v, want a damaged-backup failure", restore)
	}
	if count := sourceRestoreCount(fixture); count != 0 {
		t.Errorf("driver restores = %d, want 0", count)
	}
	verifyTempFilesGone(t, fixture.dir)
}

func TestRestoreRejectsTruncatedArtifact(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)

	path := strings.TrimPrefix(backup.Location, locationFilePrefix)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if err := os.Truncate(path, info.Size()-1); err != nil {
		t.Fatalf("truncate artifact: %v", err)
	}

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	restore := waitRestore(t, fixture, result.RestoreID)
	if restore.Status != RestoreFailed || !strings.Contains(restore.Error, "damaged") {
		t.Errorf("restore = %+v, want a damaged-backup failure", restore)
	}
	if count := sourceRestoreCount(fixture); count != 0 {
		t.Errorf("driver restores = %d, want 0", count)
	}
	verifyTempFilesGone(t, fixture.dir)
}

func TestRestoreOfALegacyBackupSkipsVerification(t *testing.T) {
	fixture := newBackupFixture(t)
	payload := []byte("payload from before checksums existed")
	backup := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Status:      BackupCompleted,
		Location:    writeLegacyArtifact(t, fixture.dir, payload),
		Size:        int64(len(payload)),
	})

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	restore := waitRestore(t, fixture, result.RestoreID)
	if restore.Status != RestoreCompleted {
		t.Errorf("restore = %+v, want completed", restore)
	}
	if count := sourceRestoreCount(fixture); count != 1 {
		t.Errorf("driver restores = %d, want 1", count)
	}
}

// truncatingStore accepts the upload but leaves a shorter object behind, the
// way a lifecycle rule or an interrupted multipart upload does.
type truncatingStore struct {
	*fakeObjectStore
}

func (s truncatingStore) Put(_ context.Context, key string, data io.Reader, _ int64) (string, error) {
	if _, err := io.Copy(io.Discard, data); err != nil {
		return "", err
	}
	return locationS3Prefix + "bucket/" + key, nil
}

func (s truncatingStore) Stat(context.Context, string) (int64, error) { return 0, nil }

func TestTruncatedUploadFailsTheRun(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.manager.objects = truncatingStore{newFakeObjectStore()}

	backup := fixture.queueBackup(t)
	if backup.Status != BackupFailed {
		t.Fatalf("status = %q, want failed", backup.Status)
	}
	if !strings.Contains(backup.Error, "expected") {
		t.Errorf("error = %q, want a stored-size mismatch", backup.Error)
	}
	if backup.ChecksumSHA256 != "" {
		t.Errorf("checksum = %q, want empty on a failed run", backup.ChecksumSHA256)
	}
}

func TestProviderCheckRoundTripsAProbeObject(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target, err := fixture.backups.EnsureBuiltinTarget(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("ensure builtin target: %v", err)
	}
	check, err := fixture.manager.TestProvider(ctx, fixture.userID, target.ID)
	if err != nil {
		t.Fatalf("TestProvider: %v", err)
	}
	if !check.OK {
		t.Fatalf("check = %+v, want ok", check)
	}
	matches, err := filepath.Glob(filepath.Join(fixture.dir, ".gotham-probe-*"))
	if err != nil {
		t.Fatalf("glob probe files: %v", err)
	}
	if len(matches) > 0 {
		t.Errorf("probe object left behind: %v", matches)
	}
}

func TestProviderCheckCleansUpTheProbeObject(t *testing.T) {
	fixture := newBackupFixture(t)
	store := newFakeObjectStore()
	fixture.manager.objects = store
	target := fixture.backups.seedTarget(BackupTarget{
		UserID:   fixture.userID,
		Name:     "bucket",
		Kind:     TargetS3,
		Endpoint: "https://s3.example.test",
		Bucket:   "backups",
	})

	check, err := fixture.manager.TestProvider(context.Background(), fixture.userID, target.ID)
	if err != nil {
		t.Fatalf("TestProvider: %v", err)
	}
	if !check.OK {
		t.Fatalf("check = %+v, want ok", check)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.puts) != 1 || len(store.gets) != 1 || len(store.deletes) != 1 {
		t.Errorf("probe round trip = %d put/%d get/%d delete, want one each",
			len(store.puts), len(store.gets), len(store.deletes))
	}
	if len(store.objects) != 0 {
		t.Errorf("probe object left behind: %d objects", len(store.objects))
	}
}

func TestProviderCheckReportsAnUnwritableDestination(t *testing.T) {
	fixture := newBackupFixture(t)
	store := newFakeObjectStore()
	store.putErr = errors.New("access denied")
	fixture.manager.objects = store
	target := fixture.backups.seedTarget(BackupTarget{
		UserID:   fixture.userID,
		Name:     "bucket",
		Kind:     TargetS3,
		Endpoint: "https://s3.example.test",
		Bucket:   "backups",
	})

	check, err := fixture.manager.TestProvider(context.Background(), fixture.userID, target.ID)
	if err != nil {
		t.Fatalf("TestProvider: %v", err)
	}
	if check.OK || !strings.Contains(check.Message, "not writable") {
		t.Errorf("check = %+v, want a not-writable failure", check)
	}
}

func TestProviderCheckReportsAFailedProbeCleanup(t *testing.T) {
	fixture := newBackupFixture(t)
	store := newFakeObjectStore()
	store.deleteErr = errors.New("delete denied")
	fixture.manager.objects = store
	target := fixture.backups.seedTarget(BackupTarget{
		UserID:   fixture.userID,
		Name:     "bucket",
		Kind:     TargetS3,
		Endpoint: "https://s3.example.test",
		Bucket:   "backups",
	})

	check, err := fixture.manager.TestProvider(context.Background(), fixture.userID, target.ID)
	if err != nil {
		t.Fatalf("TestProvider: %v", err)
	}
	if check.OK || !strings.Contains(check.Message, "deleting") {
		t.Errorf("check = %+v, want a delete failure", check)
	}
}
