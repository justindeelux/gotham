package agent

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/justindeelux/gotham/updatecore"
)

// stagedUpdaterConfig plants a staged update (target already swapped, backup
// present, pending=staged) and returns a Config whose paths live in a temp dir
// together with the pending-marker path.
func stagedUpdaterConfig(t *testing.T, restart updatecore.RestartFunc) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "gotham-agent")
	if err := os.WriteFile(target, []byte("new-unproven"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(target+updatecore.OldSuffix, []byte("original"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	pending := filepath.Join(dir, "update.pending")
	if err := updatecore.NewStatusStore(pending).Write(updatecore.Status{Result: updatecore.StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	return updaterTestConfig(t, target, restart), pending
}

// TestAgentResumeStagedRelaunchesOnce is the BE-9.1 M2 analogue on the agent: a
// staged marker left by a crash makes agent startup relaunch the wrapper exactly
// once (marker rewritten to resuming), never in a loop.
func TestAgentResumeStagedRelaunchesOnce(t *testing.T) {
	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	setTestPublicKey(t, public)

	launches := 0
	restart := func(context.Context) (func() error, error) {
		launches++
		return func() error { return nil }, nil
	}
	cfg, pending := stagedUpdaterConfig(t, restart)

	_ = NewAgent(cfg, discardLogger(), nil)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d, want 1", launches)
	}
	status, err := updatecore.NewStatusStore(pending).Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if status == nil || status.Result != updatecore.StatusResuming {
		t.Fatalf("pending = %+v, want resuming", status)
	}

	// A second startup must not relaunch (no loop).
	_ = NewAgent(cfg, discardLogger(), nil)
	if launches != 1 {
		t.Fatalf("wrapper relaunched on a resuming marker (loop): %d", launches)
	}
}

// TestAgentResumeSkipsWhileLockHeld is the BE-9.1 C1 analogue: while the
// privileged wrapper holds the update lock, startup resume must not wait for it
// and must not touch the staged marker.
func TestAgentResumeSkipsWhileLockHeld(t *testing.T) {
	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	setTestPublicKey(t, public)

	launches := 0
	restart := func(context.Context) (func() error, error) {
		launches++
		return func() error { return nil }, nil
	}
	cfg, pending := stagedUpdaterConfig(t, restart)

	lock, err := os.OpenFile(cfg.UpdateLockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock: %v", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	_ = NewAgent(cfg, discardLogger(), nil)

	if launches != 0 {
		t.Fatalf("wrapper launches = %d, want 0 while the lock is held", launches)
	}
	status, err := updatecore.NewStatusStore(pending).Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if status == nil || status.Result != updatecore.StatusStaged {
		t.Fatalf("pending = %+v, want staged untouched", status)
	}
}
