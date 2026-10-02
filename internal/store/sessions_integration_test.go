package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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

	replacement, err := st.RotateSession(ctx, rotateParams(first.RefreshHash, uuid.NewString(), user.CredentialVersion))
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
	if _, err := st.RotateSession(ctx, rotateParams(first.RefreshHash, uuid.NewString(), user.CredentialVersion)); !errors.Is(err, pgx.ErrNoRows) {
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

	if _, err := st.RotateSession(ctx, rotateParams(first.RefreshHash, other.RefreshHash, user.CredentialVersion)); err == nil {
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
