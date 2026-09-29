package databases

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/justindeelux/gotham/internal/containers"
)

// recordingBackupNotifier captures the terminal results handed to the backup
// hook.
type recordingBackupNotifier struct {
	mu       sync.Mutex
	recorded []BackupResult
}

// Compile-time guarantee.
var _ BackupNotifier = (*recordingBackupNotifier)(nil)

// BackupFinished implements BackupNotifier synchronously so tests can assert
// right after the row turns terminal; the production implementation queues the
// delivery.
func (n *recordingBackupNotifier) BackupFinished(_ context.Context, result BackupResult) {
	n.mu.Lock()
	n.recorded = append(n.recorded, result)
	n.mu.Unlock()
}

// results returns the recorded outcomes.
func (n *recordingBackupNotifier) results() []BackupResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]BackupResult(nil), n.recorded...)
}

// TestBackupCompletionNotifies proves a completed backup calls the hook once
// with the database, team and status.
func TestBackupCompletionNotifies(t *testing.T) {
	fixture := newBackupFixture(t)
	notifier := &recordingBackupNotifier{}
	fixture.manager.notifier = notifier

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q, want completed", backup.Status)
	}
	waitFor(t, "the backup completion notification", func() bool { return len(notifier.results()) == 1 })

	result := notifier.results()[0]
	if result.Status != BackupCompleted {
		t.Errorf("status = %q, want completed", result.Status)
	}
	if result.BackupID != backup.ID || result.DatabaseID != fixture.database.ID {
		t.Errorf("ids = %s/%s, want %s/%s", result.BackupID, result.DatabaseID, backup.ID, fixture.database.ID)
	}
	if result.Database != fixture.database.Name {
		t.Errorf("database = %q, want %q", result.Database, fixture.database.Name)
	}
	if result.TeamID != fixture.database.TeamID {
		t.Errorf("team = %s, want %s", result.TeamID, fixture.database.TeamID)
	}
	if result.Error != "" {
		t.Errorf("error = %q, want empty on success", result.Error)
	}
	if result.FinishedAt.IsZero() {
		t.Error("finished_at is not set")
	}
}

// TestBackupFailureNotifies proves a failed backup calls the hook once with
// the failure text.
func TestBackupFailureNotifies(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.containers.logFn = func(opts containers.RunOptions) [][]byte {
		runID := envValue(opts.Env, "GOTHAM_RUN_ID")
		return [][]byte{[]byte(
			jobStartPrefix + runID + "\n" +
				jobEndPrefix + runID + " fail 7\n" +
				"pg_dump: error: could not connect\n")}
	}
	notifier := &recordingBackupNotifier{}
	fixture.manager.notifier = notifier

	backup := fixture.queueBackup(t)
	if backup.Status != BackupFailed {
		t.Fatalf("status = %q, want failed", backup.Status)
	}
	waitFor(t, "the backup failure notification", func() bool { return len(notifier.results()) == 1 })

	result := notifier.results()[0]
	if result.Status != BackupFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "status 7") {
		t.Errorf("error = %q, want the job failure", result.Error)
	}
	if result.Database != fixture.database.Name {
		t.Errorf("database = %q, want %q", result.Database, fixture.database.Name)
	}
}

// TestBackupWithoutNotifierIsSafe pins the nil (flag-off) hook path.
func TestBackupWithoutNotifierIsSafe(t *testing.T) {
	fixture := newBackupFixture(t)

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q, want completed with a nil notifier", backup.Status)
	}
}
