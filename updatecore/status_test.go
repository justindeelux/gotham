package updatecore

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestStatusStoreRoundtrip covers write/read and the missing-file case.
func TestStatusStoreRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "update.status")
	store := NewStatusStore(path)
	if store == nil {
		t.Fatal("NewStatusStore returned nil for a non-empty path")
	}

	if got, err := store.Read(); err != nil || got != nil {
		t.Fatalf("Read(missing) = (%v, %v), want (nil, nil)", got, err)
	}

	want := Status{Result: StatusRolledBack, Version: "v1.2.0", Detail: "health failed", At: time.Unix(1700000000, 0).UTC()}
	if err := store.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Result != want.Result || got.Version != want.Version || got.Detail != want.Detail || !got.At.Equal(want.At) {
		t.Fatalf("Read = %+v, want %+v", got, want)
	}
}

// TestStatusStoreEmptyPath proves a nil store is a no-op.
func TestStatusStoreEmptyPath(t *testing.T) {
	if store := NewStatusStore("  "); store != nil {
		t.Fatal("NewStatusStore(blank) != nil")
	}
	var store *StatusStore
	if err := store.Write(Status{Result: StatusOK}); err != nil {
		t.Fatalf("nil Write: %v", err)
	}
	if got, err := store.Read(); err != nil || got != nil {
		t.Fatalf("nil Read = (%v, %v)", got, err)
	}
}

// TestStatusStoreReadIgnoresNonRegular proves the control-plane read of the
// pending/status file never blocks on a planted FIFO or follows a symlink.
func TestStatusStoreReadIgnoresNonRegular(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("result=ok\nversion=v9\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	symlink := filepath.Join(dir, "symlink")
	if err := os.Symlink(secret, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	for _, path := range []string{fifo, symlink} {
		done := make(chan struct {
			status *Status
			err    error
		}, 1)
		go func(p string) {
			status, err := NewStatusStore(p).Read()
			done <- struct {
				status *Status
				err    error
			}{status, err}
		}(path)
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatalf("Read(%s) error: %v", path, got.err)
			}
			if got.status != nil {
				t.Fatalf("Read(%s) = %+v, want nil (non-regular)", path, got.status)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("StatusStore.Read(%s) blocked", path)
		}
	}
}

// TestStatusStoreMalformed proves a status file without a result is an error.
func TestStatusStoreMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.status")
	if err := os.WriteFile(path, []byte("version=v1.2.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewStatusStore(path).Read(); !errors.Is(err, ErrManifest) {
		t.Fatalf("Read(malformed) = %v, want ErrManifest", err)
	}
}
