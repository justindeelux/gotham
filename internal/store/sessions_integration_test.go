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
// regression: under READ COMMITTED alone, the family revocation snapshots the
// live rows before an in-flight rotation inserts its replacement, then blocks
// on the presented row and misses the replacement. RevokeFamilyIfStolen holds
// the per-user advisory lock, so the revocation runs entirely after the
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
	type result struct {
		stolen bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		stolen, err := st.RevokeFamilyIfStolen(ctx, user.ID, presented.RefreshHash)
		done <- result{stolen, err}
	}()
	waitForSessionLockWaiters(t, ctx, st, user.ID, 1)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rotation: %v", err)
	}
	res := <-done
	if res.err != nil {
		t.Fatalf("RevokeFamilyIfStolen: %v", res.err)
	}
	if !res.stolen {
		t.Fatal("the replay was not classified as reuse")
	}

	replacement, err := st.GetSessionByRefreshHash(ctx, replacementHash)
	if err != nil {
		t.Fatalf("GetSessionByRefreshHash(replacement): %v", err)
	}
	if !replacement.RevokedAt.Valid {
		t.Fatal("family revocation missed the concurrent replacement; the replay race is open")
	}
}

// TestStoreRotateSessionTakesSessionLock pins the rotation side of the
// serialization: with the per-user lock held by an outer transaction, a
// rotation must block on the advisory wait and complete only after the lock is
// released. Deleting the LockUserSessions call from Store.RotateSession makes
// this test fail, because the rotation would no longer wait.
func TestStoreRotateSessionTakesSessionLock(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, _ := versionTestUser(t, ctx, st, "rotate-lock")
	presented := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

	// Hold the per-user lock while the rotation starts.
	blocker, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	if err := sqlc.New(blocker).LockUserSessions(ctx, uuid.UUID(user.ID.Bytes).String()); err != nil {
		t.Fatalf("lock user sessions: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := st.RotateSession(ctx, user.ID, rotateParams(presented.RefreshHash, uuid.NewString(), user.CredentialVersion))
		done <- err
	}()
	waitForSessionLockWaiters(t, ctx, st, user.ID, 1)

	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release session lock: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RotateSession: %v", err)
	}

	revoked, err := st.GetSessionByRefreshHash(ctx, presented.RefreshHash)
	if err != nil {
		t.Fatalf("GetSessionByRefreshHash(presented): %v", err)
	}
	if !revoked.RevokedAt.Valid {
		t.Fatal("rotation did not revoke the presented session")
	}
}

// waitForSessionLockWaiters blocks until want session mutations for userID are
// waiting on that user's per-user advisory lock, so a test can release an outer
// lock at a deterministic point. The count is scoped to the lock key (derived
// the same way LockUserSessions derives it) so a parallel test process using a
// different key cannot satisfy the barrier.
func waitForSessionLockWaiters(t *testing.T, ctx context.Context, st *store.Store, userID pgtype.UUID, want int) {
	t.Helper()

	key := uuid.UUID(userID.Bytes).String()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := st.DB.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_locks l, (SELECT hashtextextended($1::text, 0) AS key) k
			WHERE l.locktype = 'advisory'
			  AND NOT l.granted
			  AND l.objsubid = 1
			  AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
			  AND l.classid = ((k.key >> 32) & 4294967295)::oid
			  AND l.objid = (k.key & 4294967295)::oid`, key).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_locks: %v", err)
		}
		if waiting >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d session mutations blocked on the session lock for %s", waiting, want, key)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStoreFamilyRevocationIsSingleCriticalSection pins A1: the classification
// read and the family revocation must stay in one transaction under the
// per-user lock. The seam starts a competing ResetUserPassword inside that
// window; with the correct single critical section the reset is still blocked
// on the lock, whereas a split implementation (commit, then revoke unlocked)
// lets it complete and fails the test.
func TestStoreFamilyRevocationIsSingleCriticalSection(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, email := versionTestUser(t, ctx, st, "single-section")
	presented := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
	if err := st.RevokeSession(ctx, presented.RefreshHash); err != nil {
		t.Fatalf("revoke presented: %v", err)
	}

	resetDone := make(chan error, 1)
	st.BeforeFamilyRevoke = func() {
		go func() { resetDone <- st.ResetUserPassword(ctx, user.ID, email, "new-hash") }()
		select {
		case <-resetDone:
			t.Error("ResetUserPassword completed inside the classifier's critical section; the lock was released early")
		case <-time.After(200 * time.Millisecond):
			// Still blocked: the classifier holds the lock across the revoke.
		}
	}
	defer func() { st.BeforeFamilyRevoke = nil }()

	stolen, err := st.RevokeFamilyIfStolen(ctx, user.ID, presented.RefreshHash)
	if err != nil {
		t.Fatalf("RevokeFamilyIfStolen: %v", err)
	}
	if !stolen {
		t.Fatal("genuine reuse was not classified as stolen")
	}

	// The reset must now complete: the classifier committed and released the
	// lock.
	select {
	case err := <-resetDone:
		if err != nil {
			t.Fatalf("ResetUserPassword: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResetUserPassword did not complete after the classifier committed")
	}
}

// TestStoreRevokeFamilyIfStolen pins the classifier: genuine reuse (presented
// row revoked, current credential version) revokes the live family; a stale
// post-reset chain or a deleted row does not.
func TestStoreRevokeFamilyIfStolen(t *testing.T) {
	st, ctx := openVersionTestStore(t)

	t.Run("genuine reuse revokes the family", func(t *testing.T) {
		user, _ := versionTestUser(t, ctx, st, "stolen-true")
		presented := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
		live := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
		if err := st.RevokeSession(ctx, presented.RefreshHash); err != nil {
			t.Fatalf("revoke presented: %v", err)
		}

		stolen, err := st.RevokeFamilyIfStolen(ctx, user.ID, presented.RefreshHash)
		if err != nil {
			t.Fatalf("RevokeFamilyIfStolen: %v", err)
		}
		if !stolen {
			t.Fatal("genuine reuse was not classified as stolen")
		}
		got, err := st.GetSessionByRefreshHash(ctx, live.RefreshHash)
		if err != nil {
			t.Fatalf("GetSessionByRefreshHash(live): %v", err)
		}
		if !got.RevokedAt.Valid {
			t.Fatal("the live family survived a genuine reuse revocation")
		}
	})

	t.Run("stale post-reset chain is not reuse", func(t *testing.T) {
		user, email := versionTestUser(t, ctx, st, "stolen-stale")
		// Reset bumps the version and deletes every session.
		if err := st.ResetUserPassword(ctx, user.ID, email, "new-hash"); err != nil {
			t.Fatalf("ResetUserPassword: %v", err)
		}
		after, err := st.GetUserByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetUserByID: %v", err)
		}
		// A fresh post-reset session (the one that must survive) and a stale
		// pre-reset session, as a racing login would have left behind.
		live := versionTestSession(t, ctx, st, user.ID, after.CredentialVersion)
		stale := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
		if err := st.RevokeSession(ctx, stale.RefreshHash); err != nil {
			t.Fatalf("revoke stale: %v", err)
		}

		stolen, err := st.RevokeFamilyIfStolen(ctx, user.ID, stale.RefreshHash)
		if err != nil {
			t.Fatalf("RevokeFamilyIfStolen: %v", err)
		}
		if stolen {
			t.Fatal("a stale post-reset chain was classified as reuse")
		}
		got, err := st.GetSessionByRefreshHash(ctx, live.RefreshHash)
		if err != nil {
			t.Fatalf("GetSessionByRefreshHash(live): %v", err)
		}
		if got.RevokedAt.Valid {
			t.Fatal("a stale chain revoked the family")
		}
	})

	t.Run("deleted row is not reuse", func(t *testing.T) {
		user, _ := versionTestUser(t, ctx, st, "stolen-deleted")
		presented := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
		if err := st.DeleteUserSessions(ctx, user.ID); err != nil {
			t.Fatalf("DeleteUserSessions: %v", err)
		}

		stolen, err := st.RevokeFamilyIfStolen(ctx, user.ID, presented.RefreshHash)
		if err != nil {
			t.Fatalf("RevokeFamilyIfStolen: %v", err)
		}
		if stolen {
			t.Fatal("a deleted row was classified as reuse")
		}
	})
}

// TestStoreResetWaitsForFamilyRevocation is the H2 regression: ResetUserPassword
// must take the same per-user lock as the reuse classifier, so it cannot delete
// the presented session and mint a fresh login between the classifier's read
// and its family revocation. With the lock held externally, both the revocation
// and the reset must queue; without the reset's lock the reset completes during
// the pause and a post-reset session would be at risk.
func TestStoreResetWaitsForFamilyRevocation(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, email := versionTestUser(t, ctx, st, "reset-lock")
	presented := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
	if err := st.RevokeSession(ctx, presented.RefreshHash); err != nil {
		t.Fatalf("revoke presented: %v", err)
	}

	blocker, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	if err := sqlc.New(blocker).LockUserSessions(ctx, uuid.UUID(user.ID.Bytes).String()); err != nil {
		t.Fatalf("lock user sessions: %v", err)
	}

	revokeDone := make(chan error, 1)
	go func() {
		_, err := st.RevokeFamilyIfStolen(ctx, user.ID, presented.RefreshHash)
		revokeDone <- err
	}()
	waitForSessionLockWaiters(t, ctx, st, user.ID, 1)

	resetDone := make(chan error, 1)
	go func() { resetDone <- st.ResetUserPassword(ctx, user.ID, email, "new-hash") }()
	// If ResetUserPassword did not take the lock it would finish during the
	// pause and the waiter count would never reach two.
	waitForSessionLockWaiters(t, ctx, st, user.ID, 2)

	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release session lock: %v", err)
	}
	if err := <-revokeDone; err != nil {
		t.Fatalf("RevokeFamilyIfStolen: %v", err)
	}
	if err := <-resetDone; err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}

	// A login that completes after the serialized reset+revocation survives.
	after, err := st.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	fresh := versionTestSession(t, ctx, st, user.ID, after.CredentialVersion)
	if fresh.RevokedAt.Valid {
		t.Fatal("fresh post-reset session was revoked")
	}
}
