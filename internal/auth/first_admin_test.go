package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
)

func scratchService(t *testing.T) (*Service, *store.Store) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	base := testDSN()
	requirePostgres(t, base)

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	name := fmt.Sprintf("fa%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect for scratch database: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)

	parsed.Path = "/" + name
	dsn := parsed.String()

	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open scratch store: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		admin, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`"`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	signer, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(store.New(pool), signer, logger), store.New(pool)
}

func claimRole(t *testing.T, svc *Service, accessToken string) string {
	t.Helper()

	claims, err := svc.VerifyAccessToken(accessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	return claims.Role
}

func TestServiceFirstRegistrationBecomesOwner(t *testing.T) {
	svc, _ := scratchService(t)
	ctx := context.Background()

	first, err := svc.Register(ctx, "first@example.com", "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if first.User.Role != RoleOwner {
		t.Fatalf("first user role = %q, want %q", first.User.Role, RoleOwner)
	}
	if got := claimRole(t, svc, first.AccessToken); got != RoleOwner {
		t.Fatalf("first access-token role = %q, want %q", got, RoleOwner)
	}

	if _, err := svc.Register(ctx, "second@example.com", "s3cret-password", SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("second Register error = %v, want ErrRegistrationClosed", err)
	}

	svc.AllowOpenRegistration = true
	second, err := svc.Register(ctx, "second@example.com", "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("override Register: %v", err)
	}
	if second.User.Role != RoleMember {
		t.Fatalf("second user role = %q, want %q", second.User.Role, RoleMember)
	}
	if got := claimRole(t, svc, second.AccessToken); got != RoleMember {
		t.Fatalf("second access-token role = %q, want %q", got, RoleMember)
	}
}

func TestServiceConcurrentFirstRegistrations(t *testing.T) {
	svc, _ := scratchService(t)
	ctx := context.Background()

	const attempts = 8
	type result struct {
		res *AuthResult
		err error
	}
	var wg sync.WaitGroup
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			email := fmt.Sprintf("race-%d-%d@example.com", time.Now().UnixNano(), i)
			res, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
			results <- result{res: res, err: err}
		}(i)
	}
	wg.Wait()
	close(results)

	wins := 0
	var winner *AuthResult
	for res := range results {
		switch {
		case res.err == nil:
			wins++
			winner = res.res
		case errors.Is(res.err, ErrRegistrationClosed):
		default:
			t.Fatalf("unexpected Register error: %v", res.err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent first registrations succeeded %d times, want exactly 1", wins)
	}
	if winner.User.Role != RoleOwner {
		t.Fatalf("race winner role = %q, want %q", winner.User.Role, RoleOwner)
	}
	if got := claimRole(t, svc, winner.AccessToken); got != RoleOwner {
		t.Fatalf("race winner access-token role = %q, want %q", got, RoleOwner)
	}
}

func TestOAuthBootstrapIsOwnerWithOverrideOnAndOff(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprintf("override=%v", override), func(t *testing.T) {
			svc, _ := scratchService(t)
			svc.AllowOpenRegistration = override
			provider := &fakeOAuthProvider{name: "github"}
			oauth := NewOAuthService(svc, testLogger(), provider)
			t.Cleanup(oauth.Close)
			ctx := context.Background()

			bootstrap := func(email string) *AuthResult {
				t.Helper()
				provider.identity = &OAuthIdentity{Email: email}
				_, state, err := oauth.Begin(ctx, "github", "")
				if err != nil {
					t.Fatalf("Begin: %v", err)
				}
				result, err := oauth.Callback(ctx, "github", "auth-code", state, SessionMeta{})
				if err != nil {
					t.Fatalf("Callback: %v", err)
				}
				return result
			}

			first := bootstrap("oauth-first@example.com")
			if first.User.Role != RoleOwner {
				t.Fatalf("bootstrap OAuth role = %q, want %q", first.User.Role, RoleOwner)
			}
			if got := claimRole(t, svc, first.AccessToken); got != RoleOwner {
				t.Fatalf("bootstrap OAuth access-token role = %q, want %q", got, RoleOwner)
			}

			if !override {
				provider.identity = &OAuthIdentity{Email: "oauth-uninvited@example.com"}
				_, state, err := oauth.Begin(ctx, "github", "")
				if err != nil {
					t.Fatalf("Begin: %v", err)
				}
				if _, err := oauth.Callback(ctx, "github", "auth-code", state, SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
					t.Fatalf("Callback(closed) error = %v, want ErrRegistrationClosed", err)
				}
				return
			}

			second := bootstrap("oauth-second@example.com")
			if second.User.Role != RoleMember {
				t.Fatalf("second OAuth role = %q, want %q", second.User.Role, RoleMember)
			}
			if got := claimRole(t, svc, second.AccessToken); got != RoleMember {
				t.Fatalf("second OAuth access-token role = %q, want %q", got, RoleMember)
			}
		})
	}
}

func TestServiceTokenRoleFollowsDatabase(t *testing.T) {
	svc, st := scratchService(t)
	ctx := context.Background()

	email := "role-follow@example.com"
	first, err := svc.Register(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if first.User.Role != RoleOwner {
		t.Fatalf("bootstrap role = %q, want %q", first.User.Role, RoleOwner)
	}

	setRole := func(role string) {
		t.Helper()
		if _, err := st.DB.Exec(ctx,
			"UPDATE users SET role = $1 WHERE lower(email) = lower($2)", role, email); err != nil {
			t.Fatalf("set role=%v: %v", role, err)
		}
	}

	setRole(RoleMember)
	loggedIn, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if loggedIn.User.Role != RoleMember {
		t.Fatalf("login role = %q, want %q", loggedIn.User.Role, RoleMember)
	}
	if got := claimRole(t, svc, loggedIn.AccessToken); got != RoleMember {
		t.Fatalf("login access-token role = %q, want %q", got, RoleMember)
	}

	userID, err := uuid.Parse(loggedIn.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}
	me, err := svc.Me(ctx, userID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.Role != RoleMember {
		t.Fatalf("Me role = %q, want %q", me.Role, RoleMember)
	}

	setRole(RoleOwner)
	promoted, err := svc.Login(ctx, email, "s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login after promote: %v", err)
	}
	if got := claimRole(t, svc, promoted.AccessToken); got != RoleOwner {
		t.Fatalf("promoted access-token role = %q, want %q", got, RoleOwner)
	}

	refreshed, err := svc.Refresh(ctx, promoted.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := claimRole(t, svc, refreshed.AccessToken); got != RoleOwner {
		t.Fatalf("refreshed access-token role = %q, want %q (refresh must keep the role)", got, RoleOwner)
	}

	setRole(RoleMember)
	afterDemote, err := svc.Refresh(ctx, refreshed.RefreshToken, SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh after demote: %v", err)
	}
	if got := claimRole(t, svc, afterDemote.AccessToken); got != RoleMember {
		t.Fatalf("post-demotion access-token role = %q, want %q", got, RoleMember)
	}
	if afterDemote.User.Role != RoleMember {
		t.Fatalf("post-demotion user role = %q, want %q", afterDemote.User.Role, RoleMember)
	}
}
