package agent

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// resetConfig builds a Config whose reset paths all live under dir.
func resetConfig(dir string) Config {
	return Config{
		UpdatePendingPath: filepath.Join(dir, "update.pending"),
		UpdateStatusPath:  filepath.Join(dir, "update.status"),
		UpdateRetryPath:   filepath.Join(dir, "update.retry"),
		UpdateBackoffPath: filepath.Join(dir, "update.backoff"),
	}
}

// TestResetUpdateStateWritesMarker is the normal path: it clears the pending and
// status files and writes a regular retry marker.
func TestResetUpdateStateWritesMarker(t *testing.T) {
	dir := t.TempDir()
	cfg := resetConfig(dir)
	if err := updatecore.NewStatusStore(cfg.UpdatePendingPath).Write(updatecore.Status{Result: updatecore.StatusStaged, Version: "v1.0.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	if err := updatecore.NewStatusStore(cfg.UpdateStatusPath).Write(updatecore.Status{Result: updatecore.StatusRolledBack, Version: "v1.0.0"}); err != nil {
		t.Fatalf("write status: %v", err)
	}

	if err := ResetUpdateState(cfg); err != nil {
		t.Fatalf("ResetUpdateState: %v", err)
	}
	if _, err := os.Stat(cfg.UpdatePendingPath); !os.IsNotExist(err) {
		t.Errorf("pending marker not removed: %v", err)
	}
	if _, err := os.Stat(cfg.UpdateStatusPath); !os.IsNotExist(err) {
		t.Errorf("status not removed: %v", err)
	}
	info, err := os.Lstat(cfg.UpdateRetryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("retry marker is not a regular file: %v (%v)", info, err)
	}
	if got := readFileString(t, cfg.UpdateRetryPath); got != "retry\n" {
		t.Fatalf("retry marker = %q, want retry", got)
	}
}

// TestResetUpdateStateLeavesSymlinkVictimUntouched is M1: a symlink planted by
// the service user must not make root truncate and overwrite its target.
func TestResetUpdateStateLeavesSymlinkVictimUntouched(t *testing.T) {
	dir := t.TempDir()
	cfg := resetConfig(dir)
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("ROOT SECRET CONFIG\n"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.Symlink(victim, cfg.UpdateRetryPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := ResetUpdateState(cfg); err != nil {
		t.Fatalf("ResetUpdateState: %v", err)
	}
	if got := readFileString(t, victim); got != "ROOT SECRET CONFIG\n" {
		t.Fatalf("victim = %q, want it untouched", got)
	}
	info, err := os.Lstat(cfg.UpdateRetryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("retry path is not a regular file after the reset: %v (%v)", info, err)
	}
	if got := readFileString(t, cfg.UpdateRetryPath); got != "retry\n" {
		t.Fatalf("retry marker = %q, want retry", got)
	}
}

// TestResetUpdateStateDoesNotHangOnFIFO is M1: a FIFO planted by the service
// user must not block a root run.
func TestResetUpdateStateDoesNotHangOnFIFO(t *testing.T) {
	dir := t.TempDir()
	cfg := resetConfig(dir)
	if err := syscall.Mkfifo(cfg.UpdateRetryPath, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- ResetUpdateState(cfg) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResetUpdateState: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResetUpdateState blocked on a FIFO")
	}
	info, err := os.Lstat(cfg.UpdateRetryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("retry path is not a regular file after a FIFO: %v (%v)", info, err)
	}
}

// TestSeedDelayEscalates is L1: the seeded failed-attempt delay doubles and caps.
func TestSeedDelayEscalates(t *testing.T) {
	cases := []struct {
		count int
		want  time.Duration
	}{
		{1, 5 * time.Minute},
		{2, 10 * time.Minute},
		{3, 20 * time.Minute},
		{4, 40 * time.Minute},
		{5, time.Hour},
		{9, time.Hour},
	}
	for _, tc := range cases {
		if got := seedDelay(tc.count); got != tc.want {
			t.Errorf("seedDelay(%d) = %v, want %v", tc.count, got, tc.want)
		}
	}
}

// TestAgentUpdaterPersistsBackoffCount is L1: the attempt count survives a
// wrapper restart (seeded from the durable status) and escalates, then resets on
// success.
func TestAgentUpdaterPersistsBackoffCount(t *testing.T) {
	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	setTestPublicKey(t, public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	dir := filepath.Dir(target)
	if err := updatecore.NewStatusStore(filepath.Join(dir, "update.status")).Write(updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: testAgentVersion, At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write rolled_back status: %v", err)
	}
	runner := NewAgent(updaterTestConfig(t, target, noopAgentRestart), discardLogger(), nil)

	if got := runner.updater.readBackoffCount(testAgentVersion); got != 1 {
		t.Fatalf("seeded count = %d, want 1", got)
	}
	if !runner.updater.inBackoff(testAgentVersion) {
		t.Fatal("backoff was not seeded")
	}
	// A further failed attempt escalates the persisted count.
	runner.updater.recordFailure(testAgentVersion)
	if got := runner.updater.readBackoffCount(testAgentVersion); got != 2 {
		t.Fatalf("count after a second failure = %d, want 2", got)
	}
	// Success resets it.
	runner.updater.clearBackoff(testAgentVersion)
	if got := runner.updater.readBackoffCount(testAgentVersion); got != 0 {
		t.Fatalf("count after success = %d, want 0", got)
	}
}
