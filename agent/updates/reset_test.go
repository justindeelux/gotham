package updates

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

func resetConfig(dir string) Config {
	return Config{
		PendingPath: filepath.Join(dir, "update.pending"),
		StatusPath:  filepath.Join(dir, "update.status"),
		RetryPath:   filepath.Join(dir, "update.retry"),
		BackoffPath: filepath.Join(dir, "update.backoff"),
	}
}

func TestResetUpdateStateWritesMarker(t *testing.T) {
	dir := t.TempDir()
	cfg := resetConfig(dir)
	if err := updatecore.NewStatusStore(cfg.PendingPath).Write(updatecore.Status{Result: updatecore.StatusStaged, Version: "v1.0.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	if err := updatecore.NewStatusStore(cfg.StatusPath).Write(updatecore.Status{Result: updatecore.StatusRolledBack, Version: "v1.0.0"}); err != nil {
		t.Fatalf("write status: %v", err)
	}

	if err := ResetState(cfg); err != nil {
		t.Fatalf("ResetState: %v", err)
	}
	if _, err := os.Stat(cfg.PendingPath); !os.IsNotExist(err) {
		t.Errorf("pending marker not removed: %v", err)
	}
	if _, err := os.Stat(cfg.StatusPath); !os.IsNotExist(err) {
		t.Errorf("status not removed: %v", err)
	}
	info, err := os.Lstat(cfg.RetryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("retry marker is not a regular file: %v (%v)", info, err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("retry marker mode = %o, want 0644 (set on the file handle)", perm)
	}
	if got := readFileString(t, cfg.RetryPath); got != "retry\n" {
		t.Fatalf("retry marker = %q, want retry", got)
	}
}

func TestResetUpdateStateLeavesSymlinkVictimUntouched(t *testing.T) {
	dir := t.TempDir()
	cfg := resetConfig(dir)
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("ROOT SECRET CONFIG\n"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.Symlink(victim, cfg.RetryPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := ResetState(cfg); err != nil {
		t.Fatalf("ResetState: %v", err)
	}
	if got := readFileString(t, victim); got != "ROOT SECRET CONFIG\n" {
		t.Fatalf("victim = %q, want it untouched", got)
	}
	info, err := os.Lstat(cfg.RetryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("retry path is not a regular file after the reset: %v (%v)", info, err)
	}
	if got := readFileString(t, cfg.RetryPath); got != "retry\n" {
		t.Fatalf("retry marker = %q, want retry", got)
	}
}

func TestResetUpdateStateDoesNotHangOnFIFO(t *testing.T) {
	dir := t.TempDir()
	cfg := resetConfig(dir)
	if err := syscall.Mkfifo(cfg.RetryPath, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- ResetState(cfg) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResetState: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResetState blocked on a FIFO")
	}
	info, err := os.Lstat(cfg.RetryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("retry path is not a regular file after a FIFO: %v (%v)", info, err)
	}
}

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
	runner := newTestUpdater(t, updaterTestConfig(t, target, noopAgentRestart))

	if got := runner.readBackoff(testAgentVersion).count; got != 1 {
		t.Fatalf("seeded count = %d, want 1", got)
	}
	if !runner.inBackoff(testAgentVersion) {
		t.Fatal("backoff was not seeded")
	}
	runner.recordFailure(testAgentVersion)
	if got := runner.readBackoff(testAgentVersion).count; got != 2 {
		t.Fatalf("count after a second failure = %d, want 2", got)
	}
	runner.clearBackoff(testAgentVersion)
	if got := runner.readBackoff(testAgentVersion).count; got != 0 {
		t.Fatalf("count after success = %d, want 0", got)
	}
}

func TestAgentUpdaterSeededBackoffDoesNotCountRestarts(t *testing.T) {
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
	statusPath := filepath.Join(dir, "update.status")
	failureAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	writeStatus := func(at time.Time) {
		t.Helper()
		if err := updatecore.NewStatusStore(statusPath).Write(updatecore.Status{
			Result: updatecore.StatusRolledBack, Version: testAgentVersion, At: at,
		}); err != nil {
			t.Fatalf("write rolled_back status: %v", err)
		}
	}
	writeStatus(failureAt)

	cfg := updaterTestConfig(t, target, noopAgentRestart)
	first := newTestUpdater(t, cfg)
	if got := first.readBackoff(testAgentVersion).count; got != 1 {
		t.Fatalf("first seed count = %d, want 1", got)
	}

	_ = newTestUpdater(t, cfg)
	third := newTestUpdater(t, cfg)
	if got := third.readBackoff(testAgentVersion).count; got != 1 {
		t.Fatalf("count after restarts = %d, want 1 (restarts must not escalate)", got)
	}

	writeStatus(failureAt.Add(time.Minute))
	fourth := newTestUpdater(t, cfg)
	if got := fourth.readBackoff(testAgentVersion).count; got != 2 {
		t.Fatalf("count after a new failure = %d, want 2", got)
	}
}
