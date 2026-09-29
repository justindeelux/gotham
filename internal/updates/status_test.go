package updates

import (
	"errors"
	"os"
	"path/filepath"
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
