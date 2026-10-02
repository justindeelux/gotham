package databases

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// deletedRow seeds a soft-deleted database whose volume is named by the
// gotham-db-{id} convention.
func deletedRow(repo *fakeRepository, serverID uuid.UUID, deletedAt time.Time) Database {
	id := uuid.New()
	return repo.seed(Database{
		ID:          id,
		UserID:      uuid.New(),
		ServerID:    serverID,
		Name:        "amount-" + id.String()[:8],
		Engine:      EnginePostgres,
		Status:      StatusDeleting,
		StoragePath: VolumeName(id),
		DeletedAt:   deletedAt,
	})
}

// TestRetentionSweepExpiresOnlyPastWindow is the D1-9 regression: only rows
// deleted longer ago than VolumeRetention are selected, their volume is
// removed through the container service and the row is purged. A fresh delete
// and a live row are untouched.
func TestRetentionSweepExpiresOnlyPastWindow(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	now := time.Now().UTC()

	expired := deletedRow(repo, serverID, now.Add(-VolumeRetention-time.Hour))
	fresh := deletedRow(repo, serverID, now.Add(-time.Hour))
	live := repo.seed(Database{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		ServerID:    serverID,
		Name:        "live",
		Engine:      EnginePostgres,
		Status:      StatusRunning,
		StoragePath: VolumeName(uuid.New()),
	})

	cs := &fakeContainers{}
	sweeper := newRetentionSweeper(repo, cs, discardLogger())
	sweeper.now = func() time.Time { return now }

	removed, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only the row past the window)", removed)
	}
	if len(cs.volumeRemoves) != 1 || cs.volumeRemoves[0] != expired.StoragePath {
		t.Errorf("removed volumes = %v, want [%s]", cs.volumeRemoves, expired.StoragePath)
	}
	if repo.present(expired.ID) {
		t.Error("the expired row was not purged")
	}
	if !repo.present(fresh.ID) {
		t.Error("a row inside the grace window must survive the sweep")
	}
	if !repo.present(live.ID) {
		t.Error("a live row must never be purged")
	}
}

// TestRetentionSweepKeepsRowWhenVolumeRemovalFails: the row is only purged
// after the volume is gone, so an agent outage cannot leak a volume by
// discarding the row that names it.
func TestRetentionSweepKeepsRowWhenVolumeRemovalFails(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	now := time.Now().UTC()
	expired := deletedRow(repo, serverID, now.Add(-VolumeRetention-time.Hour))

	cs := &fakeContainers{volumeErr: errors.New("agent unavailable")}
	sweeper := newRetentionSweeper(repo, cs, discardLogger())
	sweeper.now = func() time.Time { return now }

	removed, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 when volume removal fails", removed)
	}
	if !repo.present(expired.ID) {
		t.Error("the row must survive so a later sweep retries the volume removal")
	}
}

// TestRetentionSweepKeepsRowWhenPurgeFails: the volume is removed, then the
// purge fails; the sweep reports no expiry and the row stays for a retry.
func TestRetentionSweepKeepsRowWhenPurgeFails(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	now := time.Now().UTC()
	expired := deletedRow(repo, serverID, now.Add(-VolumeRetention-time.Hour))
	repo.softDeleteErr = errors.New("database down")

	cs := &fakeContainers{}
	sweeper := newRetentionSweeper(repo, cs, discardLogger())
	sweeper.now = func() time.Time { return now }

	removed, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 when the purge fails", removed)
	}
	if len(cs.volumeRemoves) != 1 || cs.volumeRemoves[0] != expired.StoragePath {
		t.Errorf("removed volumes = %v, want the volume to have been removed first", cs.volumeRemoves)
	}
	if !repo.present(expired.ID) {
		t.Error("the row must survive a failed purge so the sweep retries")
	}
}

// TestRetentionSweeperLifecycle covers the started loop and the Close/Start
// latch: the loop sweeps once at startup, Close stops it, and a later Start
// must be refused so a stopped sweeper cannot silently run again.
func TestRetentionSweeperLifecycle(t *testing.T) {
	repo := newFakeRepository()
	sweeper := newRetentionSweeper(repo, &fakeContainers{}, discardLogger())
	sweeper.interval = time.Hour

	sweeper.Start()
	deadline := time.Now().Add(2 * time.Second)
	for repo.expiredCalls() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if repo.expiredCalls() == 0 {
		t.Fatal("the started loop never ran its startup sweep")
	}
	// Start is idempotent while running.
	sweeper.Start()
	if calls := repo.expiredCalls(); calls == 0 {
		t.Fatal("second Start stopped the loop")
	}

	sweeper.Close()
	before := repo.expiredCalls()
	sweeper.Start()
	time.Sleep(50 * time.Millisecond)
	if after := repo.expiredCalls(); after != before {
		t.Fatalf("Start after Close ran a sweep (%d → %d): a closed sweeper must not restart", before, after)
	}
}

// TestRetentionSweepPurgesWhenServerGone: a row whose node is gone has no
// reachable volume, so the sweep purges it without an agent call.
func TestRetentionSweepPurgesWhenServerGone(t *testing.T) {
	repo := newFakeRepository()
	now := time.Now().UTC()
	expired := deletedRow(repo, uuid.Nil, now.Add(-VolumeRetention-time.Hour))

	cs := &fakeContainers{}
	sweeper := newRetentionSweeper(repo, cs, discardLogger())
	sweeper.now = func() time.Time { return now }

	removed, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(cs.volumeRemoves) != 0 {
		t.Errorf("removed volumes = %v, want none for a node that is gone", cs.volumeRemoves)
	}
	if repo.present(expired.ID) {
		t.Error("the orphaned row was not purged")
	}
}
