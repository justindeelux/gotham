//go:build unix

package updates

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestResumeStagedSkipsWhileLockHeld is the C1 regression: the wrapper holds the
// update lock (as fd 9) through the restart and the whole health window, so a
// restarted binary calling ResumeStaged must return promptly without launching
// anything and without waiting for the lock.
func TestResumeStagedSkipsWhileLockHeld(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "gotham")
	if err := os.WriteFile(target, []byte("new-unproven"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(target+OldSuffix, []byte("original"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	lockPath := filepath.Join(dir, "update.lock")

	// Hold the lock exactly as the privileged wrapper does: a separate open
	// file description with LOCK_EX.
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock: %v", err)
	}
	defer func() { _ = syscall.Flock(int(holder.Fd()), syscall.LOCK_UN) }()

	launched := 0
	applier := &Applier{
		BinaryPath: target,
		LockPath:   lockPath,
		Pending:    NewStatusStore(filepath.Join(dir, "update.pending")),
		Status:     NewStatusStore(filepath.Join(dir, "update.status")),
		Restart: func(context.Context) (func() error, error) {
			launched++
			return func() error { return nil }, nil
		},
	}
	if err := applier.Pending.Write(Status{Result: StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- applier.ResumeStaged(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResumeStaged: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("C1: ResumeStaged blocked while the wrapper held the update lock")
	}

	if launched != 0 {
		t.Fatalf("resume launched the wrapper while the lock was held (%d)", launched)
	}
	pending, err := applier.Pending.Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pending == nil || pending.Result != StatusStaged {
		t.Fatalf("pending = %+v, want it left staged", pending)
	}
}
