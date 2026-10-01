package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// These tests exercise the P-A5 credential-version guarantee at the store
// level: a password reset bumps the account's version and deletes its sessions
// in one transaction, and any replacement a racing rotation still manages to
// mint carries the pre-reset version, so it is stale once the reset commits.

// openVersionTestStore opens the shared test database, skipping when Postgres
// is unreachable (an explicit GOTHAM_TEST_DSN turns the skip into a failure).
func openVersionTestStore(t *testing.T) (*store.Store, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Open(ctx, testDSN())
	if err != nil {
		if testDSNExplicit() {
			t.Fatalf("open store: %v", err)
		}
		t.Skipf("no database: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := store.Migrate(ctx, testDSN(), store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(pool), ctx
}

// versionTestUser creates an account (credential version 1) and schedules its
// removal, returning the stored row and its email.
func versionTestUser(t *testing.T, ctx context.Context, st *store.Store, prefix string) (sqlc.User, string) {
	t.Helper()

	email := fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})
	return user, email
}

// versionTestSession mints a live refresh session carrying version.
func versionTestSession(t *testing.T, ctx context.Context, st *store.Store, userID pgtype.UUID, version int32) sqlc.Session {
	t.Helper()

	session, err := st.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:            userID,
		RefreshHash:       uuid.NewString(),
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialVersion: version,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session
}

// rotateVersionedSession mirrors the store-level sequence of
// auth.Service.Refresh including the P-A5 guard: read the session, revoke it
// while it is live, read the account's current version, refuse a session whose
// version is older, and mint the replacement with the version read. A returned
// version of 0 means the rotation refused. The auth package's tests cover the
// service path itself; this helper only lets the store tests stage the race.
func rotateVersionedSession(ctx context.Context, st *store.Store, refreshHash string) (int32, error) {
	session, err := st.GetSessionByRefreshHash(ctx, refreshHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if session.RevokedAt.Valid {
		return 0, nil
	}
	live, err := st.RevokeSessionIfLive(ctx, refreshHash)
	if err != nil {
		return 0, err
	}
	if !live {
		return 0, nil
	}
	user, err := st.GetUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if session.CredentialVersion < user.CredentialVersion {
		return 0, nil
	}
	replacement, err := st.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:            user.ID,
		RefreshHash:       uuid.NewString(),
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialVersion: user.CredentialVersion,
	})
	if err != nil {
		return 0, err
	}
	return replacement.CredentialVersion, nil
}

// sessionsAtVersion counts the user's sessions carrying at least minVersion.
// After a reset no session minted from the old chain may count.
func sessionsAtVersion(ctx context.Context, t *testing.T, st *store.Store, userID pgtype.UUID, minVersion int32) int64 {
	t.Helper()

	var count int64
	if err := st.DB.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE user_id = $1 AND credential_version >= $2`,
		userID, minVersion).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return count
}

// TestStoreResetUserPasswordBumpsCredentialVersion proves the reset commits the
// version bump and the session delete together with the hash update.
func TestStoreResetUserPasswordBumpsCredentialVersion(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, email := versionTestUser(t, ctx, st, "cred-reset")

	if user.CredentialVersion != 1 {
		t.Fatalf("new account version = %d, want 1", user.CredentialVersion)
	}
	session := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

	if err := st.ResetUserPassword(ctx, user.ID, email, "new-hash"); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}

	after, err := st.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if after.CredentialVersion != user.CredentialVersion+1 {
		t.Fatalf("version after reset = %d, want %d", after.CredentialVersion, user.CredentialVersion+1)
	}
	if _, err := st.GetSessionByRefreshHash(ctx, session.RefreshHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("session survived the reset: err = %v, want pgx.ErrNoRows", err)
	}
}

// TestStoreResetRacesRefreshMint covers both orderings of a reset against a
// rotation of a pre-reset session.
func TestStoreResetRacesRefreshMint(t *testing.T) {
	st, ctx := openVersionTestStore(t)

	// Rotation first: the reset must delete both the old session and the
	// replacement it minted.
	t.Run("replacement committed before the reset", func(t *testing.T) {
		user, email := versionTestUser(t, ctx, st, "cred-race-before")
		session := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

		minted, err := rotateVersionedSession(ctx, st, session.RefreshHash)
		if err != nil || minted == 0 {
			t.Fatalf("rotation = (%d, %v), want a minted replacement", minted, err)
		}

		if err := st.ResetUserPassword(ctx, user.ID, email, "new-hash"); err != nil {
			t.Fatalf("ResetUserPassword: %v", err)
		}
		if count := sessionsAtVersion(ctx, t, st, user.ID, 1); count != 0 {
			t.Fatalf("%d sessions survived the reset, want 0", count)
		}
	})

	// Reset first: a rotation that already passed its guard and read the
	// account, but mints after the reset's delete, can only write a stale
	// replacement — and rotating that replacement must refuse.
	t.Run("reset committed before the replacement was minted", func(t *testing.T) {
		user, email := versionTestUser(t, ctx, st, "cred-race-after")
		session := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

		live, err := st.RevokeSessionIfLive(ctx, session.RefreshHash)
		if err != nil || !live {
			t.Fatalf("revoke = (%v, %v), want (true, nil)", live, err)
		}
		staleRead, err := st.GetUserByID(ctx, session.UserID)
		if err != nil {
			t.Fatalf("GetUserByID: %v", err)
		}

		if err := st.ResetUserPassword(ctx, user.ID, email, "new-hash"); err != nil {
			t.Fatalf("ResetUserPassword: %v", err)
		}

		replacement := versionTestSession(t, ctx, st, user.ID, staleRead.CredentialVersion)
		after, err := st.GetUserByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetUserByID: %v", err)
		}
		if replacement.CredentialVersion >= after.CredentialVersion {
			t.Fatalf("replacement version %d is not stale against the account's %d",
				replacement.CredentialVersion, after.CredentialVersion)
		}

		minted, err := rotateVersionedSession(ctx, st, replacement.RefreshHash)
		if err != nil {
			t.Fatalf("rotate stale replacement: %v", err)
		}
		if minted != 0 {
			t.Fatalf("stale replacement rotated into version %d, want a refusal", minted)
		}
	})

	// Both operations race for real; every surviving session must be stale
	// against the post-reset account.
	t.Run("concurrent", func(t *testing.T) {
		for i := 0; i < 6; i++ {
			user, email := versionTestUser(t, ctx, st, "cred-race-concurrent")
			session := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

			start := make(chan struct{})
			var wg sync.WaitGroup
			var resetErr error
			var minted int32
			var rotateErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				resetErr = st.ResetUserPassword(ctx, user.ID, email, "new-hash")
			}()
			go func() {
				defer wg.Done()
				<-start
				minted, rotateErr = rotateVersionedSession(ctx, st, session.RefreshHash)
			}()
			close(start)
			wg.Wait()

			if resetErr != nil {
				t.Fatalf("ResetUserPassword: %v", resetErr)
			}
			if rotateErr != nil {
				t.Fatalf("rotation: %v", rotateErr)
			}
			after, err := st.GetUserByID(ctx, user.ID)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			// A refusing rotation returns 0; one that slipped in after the
			// delete may only have minted the pre-reset version.
			if minted != 0 && minted >= after.CredentialVersion {
				t.Fatalf("rotation minted version %d, account is at %d: the old chain survived the reset",
					minted, after.CredentialVersion)
			}
			if count := sessionsAtVersion(ctx, t, st, user.ID, after.CredentialVersion); count != 0 {
				t.Fatalf("%d sessions at the current credential version survived the reset", count)
			}
		}
	})
}
