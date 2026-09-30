package updates

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestServiceLastStatus proves the durable outcome is surfaced through the
// service.
func TestServiceLastStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.status")
	store := NewStatusStore(path)
	if err := store.Write(Status{Result: StatusOK, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write status: %v", err)
	}

	svc, err := NewService(Config{
		Current:     "v1.2.0",
		BinaryPath:  filepath.Join(dir, "gotham"),
		LockPath:    filepath.Join(dir, "update.lock"),
		StatusPath:  path,
		PendingPath: filepath.Join(dir, "update.pending"),
		Restart:     noopRestart,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	status, err := svc.LastStatus()
	if err != nil {
		t.Fatalf("LastStatus: %v", err)
	}
	if status == nil || status.Result != StatusOK {
		t.Fatalf("LastStatus = %+v, want ok", status)
	}
}

// TestServiceReset clears a stale pending marker.
func TestServiceReset(t *testing.T) {
	dir := t.TempDir()
	pendingPath := filepath.Join(dir, "update.pending")
	if err := NewStatusStore(pendingPath).Write(Status{Result: StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	svc, err := NewService(Config{
		Current:     "v1.0.0",
		BinaryPath:  filepath.Join(dir, "gotham"),
		LockPath:    filepath.Join(dir, "update.lock"),
		StatusPath:  filepath.Join(dir, "status", "update.status"),
		PendingPath: pendingPath,
		Restart:     noopRestart,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if last, _ := svc.LastStatus(); last == nil || last.Result != StatusStaged {
		t.Fatalf("LastStatus = %+v, want staged", last)
	}
	if err := svc.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if last, _ := svc.LastStatus(); last != nil && last.Result == StatusStaged {
		t.Fatalf("LastStatus after reset = %+v, want no pending", last)
	}
}

// TestDefaultRestartSuccess proves the wrapper is launched (detached) when it
// exists and a waiter is returned.
func TestDefaultRestartSuccess(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "sudo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write sudo shim: %v", err)
	}
	script := filepath.Join(dir, "gotham-update")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	wait, err := defaultRestart(script)(context.Background())
	if err != nil {
		t.Fatalf("defaultRestart: %v", err)
	}
	if wait == nil {
		t.Fatal("defaultRestart returned no waiter")
	}
	if err := wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}
