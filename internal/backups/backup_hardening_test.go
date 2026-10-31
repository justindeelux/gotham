package backups

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func TestLocalStoreFlushesBeforeReturning(t *testing.T) {
	store, err := newLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	originalFile, originalDir := fsyncFile, fsyncDir
	t.Cleanup(func() { fsyncFile, fsyncDir = originalFile, originalDir })

	var fileFlushed, dirFlushed bool
	fsyncFile = func(file *os.File) error { fileFlushed = true; return originalFile(file) }
	fsyncDir = func(dir string) error { dirFlushed = true; return originalDir(dir) }

	payload := []byte("payload")
	if _, err := store.Put(context.Background(), "k", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !fileFlushed || !dirFlushed {
		t.Errorf("flushed file=%v dir=%v, want both", fileFlushed, dirFlushed)
	}
}

func TestLocalStoreRemovesArtifactWhenFlushFails(t *testing.T) {
	root := t.TempDir()
	store, err := newLocalStore(root)
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	originalFile := fsyncFile
	t.Cleanup(func() { fsyncFile = originalFile })
	fsyncFile = func(*os.File) error { return errors.New("flush failed") }

	if _, err := store.Put(context.Background(), "k", bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("Put reported success although the flush failed")
	}
	if _, statErr := os.Stat(root + "/k"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a failed flush left an artifact behind: %v", statErr)
	}
}

func TestLocalStoreFlushesCreatedAncestors(t *testing.T) {
	root := t.TempDir()
	store, err := newLocalStore(root)
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	originalDir := fsyncDir
	t.Cleanup(func() { fsyncDir = originalDir })
	var synced []string
	fsyncDir = func(dir string) error {
		synced = append(synced, dir)
		return originalDir(dir)
	}

	key := "databases/" + uuid.NewString() + "/artifact.gz"
	if _, err := store.Put(context.Background(), key, bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	leaf := filepath.Dir(filepath.Join(root, filepath.FromSlash(key)))
	want := []string{leaf, filepath.Dir(leaf), root}
	for _, dir := range want {
		found := false
		for _, got := range synced {
			if got == dir {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("did not flush directory %s; flushed %v", dir, synced)
		}
	}
}

func TestBackupFlushesArtifactBeforeRecordingCompletion(t *testing.T) {
	fixture := newBackupFixture(t)
	originalFile := fsyncFile
	t.Cleanup(func() { fsyncFile = originalFile })

	flushed := false
	fsyncFile = func(file *os.File) error { flushed = true; return originalFile(file) }
	violation := false
	fixture.backups.finishHook = func(backup Backup) {
		if backup.Status == BackupCompleted && !flushed {
			violation = true
		}
	}

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
	if violation {
		t.Error("the run was marked completed before the artifact was flushed")
	}
}

func TestUpdateTargetLocksDestinationWithCompletedBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target, err := fixture.manager.CreateProvider(ctx, fixture.userID, ProviderRequest{
		Name: "s3", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "old",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID), Status: BackupCompleted,
		TargetID: target.ID, Location: locationS3Prefix + "old/key.gz",
	})

	if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, ProviderRequest{Bucket: "new"}); !errors.Is(err, ErrTargetStranded) {
		t.Fatalf("bucket move err = %v, want ErrTargetStranded", err)
	}
	stored, err := fixture.backups.GetBackupTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if stored.Bucket != "old" {
		t.Errorf("bucket = %q, want the locked old value", stored.Bucket)
	}

	if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, ProviderRequest{
		Name: "renamed", Prefix: strPtr("new-prefix/"),
	}); err != nil {
		t.Fatalf("name/prefix update: %v", err)
	}

	runningTarget, err := fixture.manager.CreateProvider(ctx, fixture.userID, ProviderRequest{
		Name: "s3-running", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "old",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("CreateTarget(running): %v", err)
	}
	fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID), Status: BackupRunning, TargetID: runningTarget.ID,
	})
	if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, runningTarget.ID, ProviderRequest{Bucket: "moved"}); !errors.Is(err, ErrTargetStranded) {
		t.Fatalf("running-backup bucket move err = %v, want ErrTargetStranded", err)
	}
}

func TestUpdateTargetIsAtomicWhenSealingFails(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target := fixture.backups.seedTarget(BackupTarget{
		UserID: fixture.userID, Name: "s3", Kind: TargetS3,
		Endpoint: "https://old.example.com", Bucket: "b",
	})
	fixture.backups.secretErr = errors.New("credential write failed")

	if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, ProviderRequest{
		Endpoint: "https://new.example.com", AccessKey: "ak", SecretKey: "sk",
	}); err == nil {
		t.Fatal("expected the credential failure to surface")
	}
	stored, err := fixture.backups.GetBackupTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if stored.Endpoint != "https://old.example.com" {
		t.Errorf("endpoint = %q, want the transaction to have rolled back", stored.Endpoint)
	}
}

type sweepContainers struct {
	containers.ContainerService
	listed     []containers.Container
	removes    []string
	freshLists int
}

func (c *sweepContainers) List(context.Context, uuid.UUID) ([]containers.Container, error) {
	return c.listed, nil
}

func (c *sweepContainers) ListFresh(context.Context, uuid.UUID) ([]containers.Container, error) {
	c.freshLists++
	return c.listed, nil
}

func (c *sweepContainers) Remove(_ context.Context, _ uuid.UUID, containerID string) error {
	c.removes = append(c.removes, containerID)
	return nil
}

func TestSweepJobContainers(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.backups.serverIDs = []uuid.UUID{fixture.serverID}

	liveRun := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID), Status: BackupRunning,
	})
	leasedDB := uuid.New()
	if !fixture.manager.claim(leasedDB) {
		t.Fatal("could not lease the database")
	}
	defer fixture.manager.release(leasedDB)

	sweep := &sweepContainers{listed: []containers.Container{
		{ID: "db-container", Labels: map[string]string{
			LabelManaged: "true", LabelResourceID: fixture.databaseID.String(),
		}},
		{ID: "leftover-backup", Labels: map[string]string{
			LabelManaged: "true", LabelRole: RoleBackup,
			LabelResourceID: uuid.New().String(), LabelBackupID: uuid.New().String(),
		}},
		{ID: "live-restore", Labels: map[string]string{
			LabelManaged: "true", LabelRole: RoleRestore,
			LabelResourceID: uuid.New().String(), LabelBackupID: liveRun.ID.String(),
		}},
		{ID: "leased-stage", Labels: map[string]string{
			LabelManaged: "true", LabelRole: RoleStage,
			LabelResourceID: leasedDB.String(), LabelBackupID: uuid.New().String(),
		}},
		{ID: "unmanaged", Labels: map[string]string{
			LabelRole: RoleBackup, LabelResourceID: uuid.New().String(),
		}},
	}}
	fixture.manager.containers = sweep

	fixture.manager.sweepJobContainers()

	removes := sweep.removes
	freshLists := sweep.freshLists
	if len(removes) != 1 || removes[0] != "leftover-backup" {
		t.Errorf("removed %v, want exactly [leftover-backup]", removes)
	}
	if freshLists == 0 {
		t.Error("the sweep used the cached List instead of ListFresh")
	}
}

func TestClampBackupListLimit(t *testing.T) {
	cases := map[int]int{
		0:                      defaultBackupListLimit,
		-1:                     defaultBackupListLimit,
		1:                      1,
		defaultBackupListLimit: defaultBackupListLimit,
		maxBackupListLimit:     maxBackupListLimit,
		maxBackupListLimit + 1: maxBackupListLimit,
	}
	for input, want := range cases {
		if got := clampBackupListLimit(input); got != want {
			t.Errorf("clampBackupListLimit(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestListBackupsRespectsLimit(t *testing.T) {
	fixture := newBackupFixture(t)
	ref2 := databaseRef(fixture.databaseID)
	for i := 0; i < 5; i++ {
		fixture.backups.seedBackup(Backup{ResourceRef: databaseRef(fixture.databaseID), Status: BackupCompleted})
	}
	backups, err := fixture.manager.ListBackups(context.Background(), fixture.userID, BackupFilter{Resource: &ref2, Limit: 2})
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 2 {
		t.Errorf("listed %d backups, want 2", len(backups))
	}
}
