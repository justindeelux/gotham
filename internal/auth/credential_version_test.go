package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

// The P-A5 regression tests: a password reset must end every refresh chain,
// including sessions minted by a login or refresh that was in flight while the
// reset committed.

// TestServiceRefreshRejectsStaleCredentialVersion covers the window PR #88
// left open: a refresh that had already passed the rotation guard can mint its
// replacement after the reset's session delete. That replacement carries the
// pre-reset credential version, and Refresh must refuse it.
func TestServiceRefreshRejectsStaleCredentialVersion(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("stale-version")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(registered.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}
	user, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	// Reset the password, then replay the session the losing refresh minted
	// after the reset's delete: it stores the account's pre-reset version.
	newHash, err := HashPassword("rotated-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := st.ResetUserPassword(ctx, user.ID, email, newHash); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}

	staleToken := "stale-" + uuid.NewString()
	if _, err := st.DB.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, expires_at, credential_version)
		VALUES ($1, $2, now() + interval '1 hour', $3)`,
		user.ID, hashRefreshToken(staleToken), user.CredentialVersion); err != nil {
		t.Fatalf("insert stale session: %v", err)
	}

	after, err := st.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID after reset: %v", err)
	}
	if after.CredentialVersion <= user.CredentialVersion {
		t.Fatalf("credential version did not advance: %d -> %d", user.CredentialVersion, after.CredentialVersion)
	}

	if _, err := svc.Refresh(ctx, staleToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(stale session) error = %v, want ErrUnauthorized", err)
	}

	// The refused session is left revoked, so it cannot be presented again.
	var revokedAt pgtype.Timestamptz
	if err := st.DB.QueryRow(ctx,
		`SELECT revoked_at FROM sessions WHERE refresh_hash = $1`,
		hashRefreshToken(staleToken)).Scan(&revokedAt); err != nil {
		t.Fatalf("read stale session: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("stale session was not revoked after the refusal")
	}
}

// TestServiceLoginRefusesVersionBumpDuringVerify covers the login window: the
// reset commits while the password is being verified, so the old password
// checked out but the session must not be minted under the new credential.
func TestServiceLoginRefusesVersionBumpDuringVerify(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("login-racer")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(registered.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	newHash, err := HashPassword("rotated-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	svc.afterPasswordVerified = func() {
		if err := st.ResetUserPassword(ctx, pgUUID(userID), email, newHash); err != nil {
			t.Errorf("reset during login: %v", err)
		}
	}

	if _, err := svc.Login(ctx, email, "s3cret-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	svc.afterPasswordVerified = nil

	// The reset really committed: the new password works, and the session it
	// mints is bound to the account's post-reset version — not a constant and
	// not an off-by-one, either of which would silently reopen the reset race.
	loggedIn, err := svc.Login(ctx, email, "rotated-password")
	if err != nil {
		t.Fatalf("Login(new password): %v", err)
	}
	account, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	assertSessionVersion(t, ctx, st, loggedIn.RefreshToken, account.CredentialVersion)
}

// TestServiceIssuedSessionsCarryAccountVersion pins the mint-time version
// binding for both issuance paths at a non-trivial version: the session a
// successful Login stores, and the replacement a successful Refresh stores,
// must carry the account's current credential version. Storing a constant
// (1) or an off-by-one (version+1) makes a pre-reset chain survive the reset
// while the guard tests stay green, so this assertion is load-bearing.
func TestServiceIssuedSessionsCarryAccountVersion(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("mint-version")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(registered.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	// Move the account to version 2 so a constant or an off-by-one differs
	// from the account's version.
	newHash, err := HashPassword("rotated-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := st.ResetUserPassword(ctx, pgUUID(userID), email, newHash); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	account, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if account.CredentialVersion != 2 {
		t.Fatalf("account version after reset = %d, want 2", account.CredentialVersion)
	}

	loggedIn, err := svc.Login(ctx, email, "rotated-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	assertSessionVersion(t, ctx, st, loggedIn.RefreshToken, account.CredentialVersion)

	rotated, err := svc.Refresh(ctx, loggedIn.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	assertSessionVersion(t, ctx, st, rotated.RefreshToken, account.CredentialVersion)
}

// assertSessionVersion reads the session behind refreshToken back from the
// store and asserts it stores want.
func assertSessionVersion(t *testing.T, ctx context.Context, st *store.Store, refreshToken string, want int32) {
	t.Helper()

	session, err := st.GetSessionByRefreshHash(ctx, hashRefreshToken(refreshToken))
	if err != nil {
		t.Fatalf("GetSessionByRefreshHash: %v", err)
	}
	if session.CredentialVersion != want {
		t.Fatalf("stored session version = %d, want the account's %d", session.CredentialVersion, want)
	}
}

// TestServiceRefreshAcceptsPreMigrationSession proves the upgrade logs nobody
// out: a session written before the credential_version column existed takes the
// migration default (1), which matches the account's version, so it still
// refreshes.
func TestServiceRefreshAcceptsPreMigrationSession(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("pre-migration")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(registered.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}
	user, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.CredentialVersion != 1 {
		t.Fatalf("fresh account version = %d, want 1", user.CredentialVersion)
	}

	// Omit credential_version, exactly as the pre-migration code did: the
	// column default backfills it to the account's current version.
	token := "pre-migration-" + uuid.NewString()
	if _, err := st.DB.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, expires_at)
		VALUES ($1, $2, now() + interval '1 hour')`,
		user.ID, hashRefreshToken(token)); err != nil {
		t.Fatalf("insert pre-migration session: %v", err)
	}

	if _, err := svc.Refresh(ctx, token); err != nil {
		t.Fatalf("Refresh(pre-migration session): %v", err)
	}
}
