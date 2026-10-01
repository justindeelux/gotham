package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestUpdateBackoffSeedAndClear covers the durable seeded backoff: a rollback of
// a newer version blocks that version, a different (newer) version is not
// blocked, and the operator reset clears it.
func TestUpdateBackoffSeedAndClear(t *testing.T) {
	store := updatecore.NewStatusStore(filepath.Join(t.TempDir(), "update.backoff"))
	backoff := newUpdateBackoff(store, discardLogger())

	now := time.Now().UTC().Truncate(time.Second)
	backoff.seedFromStatus(&updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: "v1.2.0", At: now,
	}, "v1.0.0")

	if !backoff.blocked("v1.2.0") {
		t.Fatal("a freshly rolled-back version must be blocked")
	}
	if backoff.blocked("v1.3.0") {
		t.Fatal("a different (newer) version must not be blocked")
	}

	// Re-seeding the same timestamp keeps the count instead of bumping it (a
	// restart must not escalate the delay).
	count, at, ok := backoff.read("v1.2.0")
	if !ok || count != 1 || !at.Equal(now) {
		t.Fatalf("seeded record = (%d, %v, %v), want (1, %v, true)", count, at, ok, now)
	}
	backoff.seedFromStatus(&updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: "v1.2.0", At: now,
	}, "v1.0.0")
	if count, _, _ := backoff.read("v1.2.0"); count != 1 {
		t.Fatalf("re-seeding the same rollback bumped the count to %d, want 1", count)
	}

	backoff.clear()
	if backoff.blocked("v1.2.0") {
		t.Fatal("cleared backoff must not block")
	}

	// An `ok` status clears any leftover record too.
	backoff.seedFromStatus(&updatecore.Status{Result: updatecore.StatusRolledBack, Version: "v1.2.0", At: now}, "v1.0.0")
	backoff.seedFromStatus(&updatecore.Status{Result: updatecore.StatusOK, Version: "v1.2.0", At: now}, "v1.2.0")
	if backoff.blocked("v1.2.0") {
		t.Fatal("an ok status must clear the backoff")
	}
}

// TestUpdateBackoffExpires proves the delay is bounded: a record older than its
// window no longer blocks.
func TestUpdateBackoffExpires(t *testing.T) {
	store := updatecore.NewStatusStore(filepath.Join(t.TempDir(), "update.backoff"))
	backoff := newUpdateBackoff(store, discardLogger())

	old := time.Now().Add(-2 * time.Hour).UTC()
	backoff.seedFromStatus(&updatecore.Status{
		Result: updatecore.StatusRollbackFailed, Version: "v1.2.0", At: old,
	}, "v1.0.0")
	if backoff.blocked("v1.2.0") {
		t.Fatal("a long-expired backoff must not block")
	}
}

// TestUpdateBackoffMonotonic proves a restart cannot move the backoff window
// backwards: when a synchronous failure already recorded a later time than the
// durable rollback, re-seeding keeps the later time and the escalated count.
func TestUpdateBackoffMonotonic(t *testing.T) {
	store := updatecore.NewStatusStore(filepath.Join(t.TempDir(), "update.backoff"))
	backoff := newUpdateBackoff(store, discardLogger())

	rollback := time.Now().Add(-20 * time.Minute).UTC().Truncate(time.Second)
	syncFailure := time.Now().Add(-1 * time.Minute).UTC().Truncate(time.Second)
	// A prior synchronous failure at syncFailure, later than the rollback.
	backoff.write("v1.2.0", 2, syncFailure)

	backoff.seedFromStatus(&updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: "v1.2.0", At: rollback,
	}, "v1.0.0")

	count, at, ok := backoff.read("v1.2.0")
	if !ok || count != 2 || !at.Equal(syncFailure) {
		t.Fatalf("record = (%d, %v, %v), want the later sync-failure time %v kept", count, at, ok, syncFailure)
	}

	// A genuinely later rollback still bumps the count and advances the window.
	later := time.Now().UTC().Truncate(time.Second)
	backoff.seedFromStatus(&updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: "v1.2.0", At: later,
	}, "v1.0.0")
	count, at, ok = backoff.read("v1.2.0")
	if !ok || count != 3 || !at.Equal(later) {
		t.Fatalf("record = (%d, %v, %v), want a bumped count at the later rollback %v", count, at, ok, later)
	}
}

// TestServiceAutoUpdateBacksOffRolledBackRelease is LOW-2: after a rollback the
// AUTO_UPDATE loop skips the same version (no re-download/re-apply) but still
// offers a newer release.
func TestServiceAutoUpdateBacksOffRolledBackRelease(t *testing.T) {
	t.Setenv(AutoUpdateEnv, "true")
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "status", "update.status")
	if err := os.MkdirAll(filepath.Dir(statusPath), 0o755); err != nil {
		t.Fatalf("mkdir status dir: %v", err)
	}
	if err := updatecore.NewStatusStore(statusPath).Write(updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: "v1.2.0", At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write rolled_back status: %v", err)
	}
	backoffPath := filepath.Join(dir, "update.backoff")
	binPath := filepath.Join(dir, "gotham")

	newSvc := func(t *testing.T, server *httptest.Server) *service {
		t.Helper()
		svc, err := NewService(Config{
			Current:      "v1.0.0",
			Repo:         "owner/name",
			BaseURL:      server.URL,
			Channel:      ChannelStable,
			Client:       server.Client(),
			GOARCH:       "amd64",
			Logger:       discardLogger(),
			Restart:      noopRestart,
			BinaryPath:   binPath,
			LockPath:     filepath.Join(dir, "update.lock"),
			StatusPath:   statusPath,
			PendingPath:  filepath.Join(dir, "update.pending"),
			BackoffPath:  backoffPath,
			Auto:         true,
			AutoInterval: time.Hour,
		})
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		s, ok := svc.(*service)
		if !ok {
			t.Fatalf("NewService returned %T, want *service", svc)
		}
		return s
	}

	// The same version is still rolled back: the loop must skip it without
	// attempting an apply (a failure would surface as ErrNoPublicKey).
	rolledBackServer := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.2.0", Assets: platformAssets()},
	}, 0, 0))
	defer rolledBackServer.Close()
	svc := newSvc(t, rolledBackServer)
	version, applied, err := svc.applyAutoUpdate(context.Background())
	if err != nil {
		t.Fatalf("applyAutoUpdate(rolled back) = %v, want it skipped", err)
	}
	if applied {
		t.Fatal("applyAutoUpdate applied a backed-off release")
	}
	if version != "v1.2.0" {
		t.Fatalf("applyAutoUpdate reported version %q, want the offered v1.2.0", version)
	}

	// A newer release is not blocked: the loop attempts it (and fails closed on
	// the missing test public key, which proves the attempt happened).
	newerServer := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.3.0", Assets: platformAssets()},
	}, 0, 0))
	defer newerServer.Close()
	svc = newSvc(t, newerServer)
	version, _, err = svc.applyAutoUpdate(context.Background())
	if !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("applyAutoUpdate(newer) = %v, want an apply attempt (ErrNoPublicKey)", err)
	}
	if version != "v1.3.0" {
		t.Fatalf("applyAutoUpdate reported version %q, want the offered v1.3.0", version)
	}
}
