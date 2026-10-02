package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// rotateParams builds the replacement parameters for store.RotateSession.
func rotateParams(revoked, replacement string, version int32) sqlc.RotateSessionParams {
	return sqlc.RotateSessionParams{
		RevokedRefreshHash: revoked,
		NewRefreshHash:     replacement,
		ExpiresAt:          pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialVersion:  version,
	}
}

// TestStoreRotateSession proves the atomic rotate: the presented live session
// is revoked and a replacement is inserted, and the old hash can no longer be
// rotated.
func TestStoreRotateSession(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, _ := versionTestUser(t, ctx, st, "rotate")
	first := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

	replacement, err := st.RotateSession(ctx, user.ID, rotateParams(first.RefreshHash, uuid.NewString(), user.CredentialVersion))
	if err != nil {
		t.Fatalf("RotateSession: %v", err)
	}
	if replacement.RevokedAt.Valid {
		t.Fatal("replacement is revoked, want a live session")
	}

	revoked, err := st.GetSessionByRefreshHash(ctx, first.RefreshHash)
	if err != nil {
		t.Fatalf("GetSessionByRefreshHash(old): %v", err)
	}
	if !revoked.RevokedAt.Valid {
		t.Fatal("presented session was not revoked by the rotation")
	}

	// Rotating the already-rotated hash is a no-rows conflict.
	if _, err := st.RotateSession(ctx, user.ID, rotateParams(first.RefreshHash, uuid.NewString(), user.CredentialVersion)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second RotateSession error = %v, want pgx.ErrNoRows", err)
	}
}

// TestStoreRotateSessionKeepsOldSessionOnInsertFailure proves atomicity: when
// the replacement insert fails (here a refresh_hash unique violation), the
// revoke rolls back and the presented session stays usable, so a failed
// rotation cannot consume the user's only refresh token.
func TestStoreRotateSessionKeepsOldSessionOnInsertFailure(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, _ := versionTestUser(t, ctx, st, "rotate-atomic")
	first := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
	// A second session whose hash collides with the intended replacement.
	other := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

	if _, err := st.RotateSession(ctx, user.ID, rotateParams(first.RefreshHash, other.RefreshHash, user.CredentialVersion)); err == nil {
		t.Fatal("expected the replacement insert to fail on the duplicate hash")
	}

	reloaded, err := st.GetSessionByRefreshHash(ctx, first.RefreshHash)
	if err != nil {
		t.Fatalf("presented session vanished after a failed rotation: %v", err)
	}
	if reloaded.RevokedAt.Valid {
		t.Fatal("failed rotation consumed the presented session; the store is not atomic")
	}
}

// TestStoreFamilyRevocationSerializesWithRotation is the FX-2b replay race
// regression: under READ COMMITTED alone, RevokeUserSessions snapshots the
// live rows before an in-flight rotation inserts its replacement, then blocks
// on the presented row and misses the replacement. Both paths take the
// per-user advisory lock, so the revocation now runs entirely after the
// rotation commits and sees the replacement.
func TestStoreFamilyRevocationSerializesWithRotation(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, _ := versionTestUser(t, ctx, st, "reuse-race")
	presented := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

	// Simulate the first half of an uncommitted rotation B -> C: hold the
	// per-user lock, revoke the presented row, insert the replacement.
	tx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rotation: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	if err := q.LockUserSessions(ctx, uuid.UUID(user.ID.Bytes).String()); err != nil {
		t.Fatalf("lock user sessions: %v", err)
	}
	if _, err := q.RevokeSessionIfLive(ctx, presented.RefreshHash); err != nil {
		t.Fatalf("revoke presented: %v", err)
	}
	replacementHash := uuid.NewString()
	if _, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:            user.ID,
		RefreshHash:       replacementHash,
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialVersion: user.CredentialVersion,
	}); err != nil {
		t.Fatalf("create replacement: %v", err)
	}

	// The reuse handler starts while the rotation is uncommitted; it must
	// block on the session lock instead of snapshotting before C exists.
	done := make(chan error, 1)
	go func() { done <- st.RevokeUserSessions(ctx, user.ID) }()
	waitForBlockedRevoke(t, ctx, st)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rotation: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RevokeUserSessions: %v", err)
	}

	replacement, err := st.GetSessionByRefreshHash(ctx, replacementHash)
	if err != nil {
		t.Fatalf("GetSessionByRefreshHash(replacement): %v", err)
	}
	if !replacement.RevokedAt.Valid {
		t.Fatal("family revocation missed the concurrent replacement; the replay race is open")
	}
}

// waitForBlockedRevoke blocks until the family-revocation path is waiting on
// the per-user advisory lock, so the test commits the rotation at a
// deterministic point. The blocked statement is the lock SELECT itself; seeing
// no advisory waiter within the deadline means the two paths are not
// serialized.
func waitForBlockedRevoke(t *testing.T, ctx context.Context, st *store.Store) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := st.DB.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND wait_event = 'advisory'
			  AND pid <> pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("family revocation never blocked on the session lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
