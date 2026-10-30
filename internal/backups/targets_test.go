package backups

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestExistingDoubleGzipLocationRestoresAndCleansUp(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	objects := newFakeObjectStore()
	fixture.manager.objects = objects
	location := locationS3Prefix + "bucket/legacy.tar.gz.gz"
	payload := []byte("legacy artifact bytes")
	objects.objects["legacy.tar.gz.gz"] = payload
	backup := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Status:      BackupCompleted,
		Location:    location,
		Size:        int64(len(payload)),
	})

	result, err := fixture.manager.RestoreBackup(ctx, fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	waitFor(t, "the restore to finish", func() bool {
		restore, ok := fixture.backups.getRestore(result.RestoreID)
		return ok && restore.Status != RestoreRunning
	})
	if restore, _ := fixture.backups.getRestore(result.RestoreID); restore.Status != RestoreCompleted {
		t.Fatalf("restore status = %q (%s), want completed", restore.Status, restore.Error)
	}
	objects.mu.Lock()
	gets := append([]string(nil), objects.gets...)
	objects.mu.Unlock()
	if len(gets) != 1 || gets[0] != location {
		t.Fatalf("restore read %v, want the stored location %q", gets, location)
	}

	if err := fixture.manager.DeleteBackup(ctx, fixture.userID, backup.ID); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	objects.mu.Lock()
	deletes := append([]string(nil), objects.deletes...)
	objects.mu.Unlock()
	if len(deletes) != 1 || deletes[0] != location {
		t.Fatalf("cleanup deleted %v, want the stored location %q", deletes, location)
	}
	if _, ok := fixture.backups.getBackup(backup.ID); ok {
		t.Fatal("the row must be removed after a successful cleanup")
	}
}

func TestStoreAndKeyDoesNotRepeatTheGzipSuffix(t *testing.T) {
	manager := &BackupManager{objects: newFakeObjectStore()}
	resource := Resource{Type: ResourceApplication, ID: uuid.New()}
	runID := uuid.New()

	cases := []struct {
		extension string
		want      string
	}{
		{"tar.gz", "applications/" + resource.ID.String() + "/" + runID.String() + ".tar.gz"},
		{"dump", "applications/" + resource.ID.String() + "/" + runID.String() + ".dump.gz"},
	}
	for _, tc := range cases {
		_, key, err := manager.storeAndKey(context.Background(), nil, resource, runID, tc.extension)
		if err != nil {
			t.Fatalf("storeAndKey(%q): %v", tc.extension, err)
		}
		if key != tc.want {
			t.Errorf("storeAndKey(%q) = %q, want %q", tc.extension, key, tc.want)
		}
	}
}
