package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
)

// defaultTestDSN points at the dev database from deploy/compose.dev.yml.
// Override with GOTHAM_TEST_DSN.
const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func testDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

// requirePostgres skips the test when no database is reachable. Only a
// connection failure skips: any migration error afterwards is a hard failure.
func requirePostgres(t *testing.T, dsn string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	_ = conn.Close(ctx)
}

// newTestService migrates the dev database, opens a store, and returns a Service
// wired to an ephemeral signer. Every test gets unique emails and cleans up.
func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()

	dsn := testDSN()
	requirePostgres(t, dsn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A reachable database whose migrations fail is a real error, not a skip.
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)

	signer, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)
	return New(st, signer, logger), st
}

// uniqueEmail returns an email that will not collide across test runs.
func uniqueEmail(prefix string) string {
	return fmt.Sprintf("be-1.1-%s-%d@example.com", prefix, time.Now().UnixNano())
}

// cleanupUser deletes the account (and, by cascade, its sessions) afterwards.
func cleanupUser(t *testing.T, st *store.Store, email string) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := st.DB.Exec(ctx, "DELETE FROM users WHERE lower(email) = lower($1)", email); err != nil {
			t.Logf("cleanup user %s: %v", email, err)
		}
	})
}

func TestServiceRegisterLoginMe(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("roundtrip")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, "  "+strings.ToUpper(email)+"  ", password, newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if registered.User.Email != email {
		t.Errorf("registered email = %q, want normalized %q", registered.User.Email, email)
	}
	if registered.AccessToken == "" || registered.RefreshToken == "" {
		t.Fatalf("Register returned an incomplete token pair: %+v", registered)
	}
	if registered.ExpiresIn <= 0 || registered.ExpiresIn > 900 {
		t.Errorf("ExpiresIn = %d, want (0, 900]", registered.ExpiresIn)
	}

	loggedIn, err := svc.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if loggedIn.User.ID != registered.User.ID {
		t.Errorf("login user ID = %q, want %q", loggedIn.User.ID, registered.User.ID)
	}

	userID, err := uuid.Parse(loggedIn.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}
	me, err := svc.Me(ctx, userID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.Email != email {
		t.Errorf("Me email = %q, want %q", me.Email, email)
	}
}

func TestServiceRegisterDuplicateEmail(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("duplicate")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	if _, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Same email in different case must also collide; a fresh invite keeps
	// the duplicate past the registration gate so the unique index answers.
	_, err := svc.Register(ctx, strings.ToUpper(email), password, newTestInvite(t, st, email), storeInvites{st})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("second Register error = %v, want ErrEmailTaken", err)
	}
}

func TestServiceLoginFailures(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("login-fail")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	if _, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := svc.Login(ctx, email, "definitely-wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(wrong password) error = %v, want ErrInvalidCredentials", err)
	}

	if _, err := svc.Login(ctx, uniqueEmail("nobody"), password); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(unknown email) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceRegisterValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "not-an-email", "s3cret-password", "", nil); !errors.Is(err, ErrValidation) {
		t.Errorf("Register(bad email) error = %v, want ErrValidation", err)
	}
	if _, err := svc.Register(ctx, uniqueEmail("short"), "short", "", nil); !errors.Is(err, ErrValidation) {
		t.Errorf("Register(short password) error = %v, want ErrValidation", err)
	}
}

func TestServiceRefreshRotation(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("refresh")
	cleanupUser(t, st, email)

	first, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("Refresh reused the old refresh token, want rotation")
	}

	// The new token must still work.
	third, err := svc.Refresh(ctx, second.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh(new token): %v", err)
	}
	if third.RefreshToken == "" {
		t.Fatal("Refresh(new token) returned an empty refresh token")
	}

	// Replaying the rotated-away first token is reuse detection: it is
	// refused, and the whole family (including the live third token) dies.
	if _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(replayed token) error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Refresh(ctx, third.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("the replacement survived reuse detection; the family was not revoked")
	}
}

// TestServiceRefreshExpiredDoesNotRevokeFamily: a merely expired, never-revoked
// token is not evidence of theft, so it gets a plain 401 and leaves the user's
// live sessions alone. A rotated-away (revoked) replay is covered by
// TestServiceRefreshRotation.
func TestServiceRefreshExpiredDoesNotRevokeFamily(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("expired-no-revoke")
	cleanupUser(t, st, email)

	live, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID, err := uuid.Parse(live.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}
	user, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	expiredToken := "expired-" + uuid.NewString()
	if _, err := st.DB.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, expires_at, credential_version)
		VALUES ($1, $2, now() - interval '1 hour', $3)`,
		user.ID, hashRefreshToken(expiredToken), user.CredentialVersion); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	if _, err := svc.Refresh(ctx, expiredToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(expired) error = %v, want ErrUnauthorized", err)
	}

	// The live session must survive: a revoked replay is theft, an expired one
	// is not.
	if _, err := svc.Refresh(ctx, live.RefreshToken); err != nil {
		t.Fatalf("live session was revoked by an expired-token replay: %v", err)
	}
}

func TestServiceLogoutDeletesSession(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("logout")
	cleanupUser(t, st, email)

	result, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.Logout(ctx, result.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Refresh(ctx, result.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh after logout error = %v, want ErrUnauthorized", err)
	}

	// The row is deleted, not merely revoked.
	var count int
	if err := st.DB.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE refresh_hash = $1`,
		hashRefreshToken(result.RefreshToken)).Scan(&count); err != nil {
		t.Fatalf("count session rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("logged-out session still exists (%d rows), want deleted", count)
	}

	// Logout is idempotent, including for unknown tokens.
	if err := svc.Logout(ctx, result.RefreshToken); err != nil {
		t.Fatalf("second Logout: %v", err)
	}
	if err := svc.Logout(ctx, "unknown-refresh-token"); err != nil {
		t.Fatalf("Logout(unknown): %v", err)
	}
}

// TestServiceLogoutReplayDoesNotRevokeOtherSessions is the FX-2f regression: a
// replayed token from a logged-out browser (a stale tab, an old copy) must be a
// plain 401 and must not revoke the account's other live sessions. Logout
// deletes the row, so the reuse classifier sees a deleted row and attributes
// nothing; a revoked row would have been classified as rotation theft.
func TestServiceLogoutReplayDoesNotRevokeOtherSessions(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("logout-replay")
	cleanupUser(t, st, email)

	loggedOut, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A second live session on another device that must survive the replay.
	other, err := svc.Login(ctx, email, "s3cret-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := svc.Logout(ctx, loggedOut.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, err := svc.Refresh(ctx, loggedOut.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(logged-out token) error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Refresh(ctx, other.RefreshToken); err != nil {
		t.Fatalf("other live session was revoked by a logged-out token replay: %v", err)
	}
}

// TestServiceLogoutPreservesRevokedRow is the fix-round-1 regression: logout
// must delete only a live row. Presenting a token that was already rotated away
// must leave the revoked row in place, because that row is the
// reuse-detection evidence for the rotation.
func TestServiceLogoutPreservesRevokedRow(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("logout-revoked")
	cleanupUser(t, st, email)

	first, err := svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, email), storeInvites{st})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Rotating first revokes its row and mints a replacement.
	if _, err := svc.Refresh(ctx, first.RefreshToken); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if err := svc.Logout(ctx, first.RefreshToken); err != nil {
		t.Fatalf("Logout(rotated token): %v", err)
	}

	var revoked bool
	if err := st.DB.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM sessions WHERE refresh_hash = $1`,
		hashRefreshToken(first.RefreshToken)).Scan(&revoked); err != nil {
		t.Fatalf("rotated row was deleted by logout: %v", err)
	}
	if !revoked {
		t.Fatal("rotated row is no longer revoked")
	}
}

func TestServiceRefreshRejectsUnknownToken(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Refresh(ctx, "unknown-refresh-token"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(unknown) error = %v, want ErrUnauthorized", err)
	}
}

func TestServiceMeUnknownUser(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Me(ctx, uuid.New()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Me(unknown) error = %v, want ErrUnauthorized", err)
	}
}

// TestServiceRegisterOpenOverride covers the test/dev escape hatch: with
// AllowOpenRegistration set on an instance that already has accounts, a
// registration must take the plain-insert path. It must NOT reach
// CreateFirstUser, whose emptiness guard would answer ErrRegistrationClosed
// (that combination broke the UI smoke, which seeds extra accounts).
func TestServiceRegisterOpenOverride(t *testing.T) {
	svc, st := newTestService(t)
	svc.AllowOpenRegistration = true
	ctx := context.Background()

	email := uniqueEmail("open-override")
	cleanupUser(t, st, email)

	if _, err := svc.Register(ctx, email, "s3cret-password", "", nil); err != nil {
		t.Fatalf("Register with the open override: %v", err)
	}
}
