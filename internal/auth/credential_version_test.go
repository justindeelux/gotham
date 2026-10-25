package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

func TestServiceRefreshRejectsStaleCredentialVersion(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("stale-version")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	if _, err := svc.Refresh(ctx, staleToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(stale session) error = %v, want ErrUnauthorized", err)
	}

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

func TestServiceRefreshConflictAfterResetDoesNotRevokeFreshSession(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("refresh-reset-race")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	var fresh *AuthResult
	svc.beforeRotate = func() {
		if err := st.ResetUserPassword(ctx, pgUUID(userID), email, newHash); err != nil {
			t.Errorf("reset during refresh: %v", err)
		}
		fresh, err = svc.Login(ctx, email, "rotated-password", SessionMeta{})
		if err != nil {
			t.Errorf("login during refresh: %v", err)
		}
	}

	if _, err := svc.Refresh(ctx, registered.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("stale Refresh error = %v, want ErrUnauthorized", err)
	}
	svc.beforeRotate = nil

	if fresh == nil || fresh.RefreshToken == "" {
		t.Fatal("fresh login did not mint a session")
	}
	if _, err := svc.Refresh(ctx, fresh.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("fresh session was revoked by the stale refresh conflict: %v", err)
	}
}

func TestServiceRefreshStaleTokenReplayDoesNotRevokeFreshSession(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("stale-replay")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	newHash, err := HashPassword("rotated-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := st.ResetUserPassword(ctx, pgUUID(userID), email, newHash); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	fresh, err := svc.Login(ctx, email, "rotated-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	staleToken := "stale-" + uuid.NewString()
	if _, err := st.DB.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, expires_at, credential_version)
		VALUES ($1, $2, now() + interval '1 hour', $3)`,
		user.ID, hashRefreshToken(staleToken), user.CredentialVersion); err != nil {
		t.Fatalf("insert stale session: %v", err)
	}

	if _, err := svc.Refresh(ctx, staleToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("first stale Refresh error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Refresh(ctx, staleToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("second stale Refresh error = %v, want ErrUnauthorized", err)
	}

	if _, err := svc.Refresh(ctx, fresh.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("fresh session was revoked by a stale-token replay: %v", err)
	}
}

func TestServiceLoginRefusesVersionBumpDuringVerify(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("login-racer")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	if _, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	svc.afterPasswordVerified = nil

	loggedIn, err := svc.Login(ctx, email, "rotated-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login(new password): %v", err)
	}
	account, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	assertSessionVersion(t, ctx, st, loggedIn.RefreshToken, account.CredentialVersion)
}

func TestServiceIssuedSessionsCarryAccountVersion(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("mint-version")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	loggedIn, err := svc.Login(ctx, email, "rotated-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	assertSessionVersion(t, ctx, st, loggedIn.RefreshToken, account.CredentialVersion)

	rotated, err := svc.Refresh(ctx, loggedIn.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	assertSessionVersion(t, ctx, st, rotated.RefreshToken, account.CredentialVersion)
}

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

func TestServiceRefreshAcceptsPreMigrationSession(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("pre-migration")
	cleanupUser(t, st, email)

	registered, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	token := "pre-migration-" + uuid.NewString()
	if _, err := st.DB.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, expires_at)
		VALUES ($1, $2, now() + interval '1 hour')`,
		user.ID, hashRefreshToken(token)); err != nil {
		t.Fatalf("insert pre-migration session: %v", err)
	}

	if _, err := svc.Refresh(ctx, token, SessionMeta{}); err != nil {
		t.Fatalf("Refresh(pre-migration session): %v", err)
	}
}
