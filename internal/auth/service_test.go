package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
)

const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func testDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

func requirePostgres(t *testing.T, dsn string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	_ = conn.Close(ctx)
}

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	return scratchService(t)
}

func requireClosedInstance(t *testing.T, svc *Service) {
	t.Helper()

	email := uniqueEmail("closed-seed")
	_, err := svc.Register(context.Background(), email, "s3cret-password", SessionMeta{})
	if err != nil && !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("seed closed-instance account: %v", err)
	}
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("be-1.1-%s-%d@example.com", prefix, time.Now().UnixNano())
}

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

	registered, err := svc.Register(ctx, "  "+strings.ToUpper(email)+"  ", password, SessionMeta{})
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

	loggedIn, err := svc.Login(ctx, email, password, SessionMeta{})
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

	owner, err := svc.Register(ctx, uniqueEmail("duplicate-owner"), "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register owner: %v", err)
	}
	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}
	email := uniqueEmail("duplicate")
	cleanupUser(t, st, email)
	_, token, err := svc.CreateInvite(ctx, ownerID, email, nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	// The account appears after the invite was issued, so registration itself
	// hits the unique address instead of the invite check.
	hash, err := HashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := st.CreateUser(ctx, email, &hash); err != nil {
		t.Fatalf("seed duplicate account: %v", err)
	}
	if _, err := svc.RegisterInvited(ctx, strings.ToUpper(email), "s3cret-password", token, SessionMeta{}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("RegisterInvited(duplicate) error = %v, want ErrEmailTaken", err)
	}
}

func TestServiceLoginFailures(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("login-fail")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	if _, err := svc.Register(ctx, email, password, SessionMeta{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := svc.Login(ctx, email, "definitely-wrong", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(wrong password) error = %v, want ErrInvalidCredentials", err)
	}

	if _, err := svc.Login(ctx, uniqueEmail("nobody"), password, SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(unknown email) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceLoginMissStillVerifies(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	calls := 0
	var firstEncoded string
	svc.verifyPassword = func(encoded, password string) (bool, error) {
		calls++
		if calls == 1 {
			firstEncoded = encoded
		}
		return VerifyPassword(encoded, password)
	}

	if _, err := svc.Register(ctx, uniqueEmail("miss-owner"), "s3cret-password", SessionMeta{}); err != nil {
		t.Fatalf("bootstrap Register: %v", err)
	}
	if _, err := svc.Login(ctx, uniqueEmail("missing"), "s3cret-password", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(unknown email) error = %v, want ErrInvalidCredentials", err)
	}
	if calls != 1 {
		t.Fatalf("unknown-email verifications = %d, want 1", calls)
	}
	if firstEncoded == "" {
		t.Fatal("miss path verified against an empty hash (verification short-circuited)")
	}
	if _, _, _, err := decodeHash(firstEncoded); err != nil {
		t.Fatalf("dummy hash %q is not decodable: %v", firstEncoded, err)
	}

	email := uniqueEmail("passwordless")
	cleanupUser(t, st, email)
	if _, err := st.CreateUser(ctx, email, nil); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(passwordless) error = %v, want ErrInvalidCredentials", err)
	}
	if calls != 2 {
		t.Fatalf("passwordless-account verifications = %d, want 2", calls)
	}

	known := uniqueEmail("known")
	cleanupUser(t, st, known)
	hash, err := HashPassword("right-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := st.CreateUser(ctx, known, &hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.Login(ctx, known, "wrong-password", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if calls != 3 {
		t.Fatalf("wrong-password verifications = %d, want 3", calls)
	}
}

func TestServiceRegisterValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "not-an-email", "s3cret-password", SessionMeta{}); !errors.Is(err, ErrValidation) {
		t.Errorf("Register(bad email) error = %v, want ErrValidation", err)
	}
	if _, err := svc.Register(ctx, uniqueEmail("short"), "short", SessionMeta{}); !errors.Is(err, ErrValidation) {
		t.Errorf("Register(short password) error = %v, want ErrValidation", err)
	}
}

func TestServiceRefreshRotation(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("refresh")
	cleanupUser(t, st, email)

	first, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	second, err := svc.Refresh(ctx, first.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("Refresh reused the old refresh token, want rotation")
	}

	third, err := svc.Refresh(ctx, second.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh(new token): %v", err)
	}
	if third.RefreshToken == "" {
		t.Fatal("Refresh(new token) returned an empty refresh token")
	}

	if _, err := svc.Refresh(ctx, first.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(replayed token) error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Refresh(ctx, third.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("the replacement survived reuse detection; the family was not revoked")
	}
}

func TestServiceRefreshExpiredDoesNotRevokeFamily(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("expired-no-revoke")
	cleanupUser(t, st, email)

	live, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
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

	if _, err := svc.Refresh(ctx, expiredToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(expired) error = %v, want ErrUnauthorized", err)
	}

	if _, err := svc.Refresh(ctx, live.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("live session was revoked by an expired-token replay: %v", err)
	}
}

func TestServiceLogoutDeletesSession(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("logout")
	cleanupUser(t, st, email)

	result, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.Logout(ctx, result.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Refresh(ctx, result.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh after logout error = %v, want ErrUnauthorized", err)
	}

	var count int
	if err := st.DB.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE refresh_hash = $1`,
		hashRefreshToken(result.RefreshToken)).Scan(&count); err != nil {
		t.Fatalf("count session rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("logged-out session still exists (%d rows), want deleted", count)
	}

	if err := svc.Logout(ctx, result.RefreshToken); err != nil {
		t.Fatalf("second Logout: %v", err)
	}
	if err := svc.Logout(ctx, "unknown-refresh-token"); err != nil {
		t.Fatalf("Logout(unknown): %v", err)
	}
}

func TestServiceLogoutReplayDoesNotRevokeOtherSessions(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("logout-replay")
	cleanupUser(t, st, email)

	loggedOut, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	other, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := svc.Logout(ctx, loggedOut.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, err := svc.Refresh(ctx, loggedOut.RefreshToken, SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Refresh(logged-out token) error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Refresh(ctx, other.RefreshToken, SessionMeta{}); err != nil {
		t.Fatalf("other live session was revoked by a logged-out token replay: %v", err)
	}
}

func TestServiceLogoutPreservesRevokedRow(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("logout-revoked")
	cleanupUser(t, st, email)

	first, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.Refresh(ctx, first.RefreshToken, SessionMeta{}); err != nil {
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

	if _, err := svc.Refresh(ctx, "unknown-refresh-token", SessionMeta{}); !errors.Is(err, ErrUnauthorized) {
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

func TestServiceRegisterRequiresInviteAfterOwner(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()
	if _, err := svc.Register(ctx, uniqueEmail("closed-seed"), "s3cret-password", SessionMeta{}); err != nil {
		t.Fatalf("seed first account: %v", err)
	}

	email := uniqueEmail("closed-second")
	cleanupUser(t, st, email)

	if _, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register without invite error = %v, want ErrRegistrationClosed", err)
	}

	second := registerTestMember(t, svc, email)
	if second.User.Role != RoleMember {
		t.Fatalf("invited user role = %q, want %q", second.User.Role, RoleMember)
	}
}
