package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/justindeelux/gotham/internal/store"
)

// fakeOAuthProvider is a deterministic OAuthProvider: no network, scripted
// results.
type fakeOAuthProvider struct {
	name        string
	identity    *OAuthIdentity
	exchangeErr error
	identityErr error
}

func (f *fakeOAuthProvider) Name() string { return f.name }

func (f *fakeOAuthProvider) AuthCodeURL(state string) string {
	return "https://" + f.name + ".example/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeOAuthProvider) Exchange(_ context.Context, _ string) (*oauth2.Token, error) {
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	return &oauth2.Token{AccessToken: "fake-access-token"}, nil
}

func (f *fakeOAuthProvider) Identity(_ context.Context, _ *oauth2.Token) (*OAuthIdentity, error) {
	if f.identityErr != nil {
		return nil, f.identityErr
	}
	return f.identity, nil
}

// testLogger returns a logger that discards output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestOAuth builds an OAuthService without a store. It is enough for state
// and pre-persistence tests.
func newTestOAuth(t *testing.T, providers ...OAuthProvider) *OAuthService {
	t.Helper()

	oauth := NewOAuthService(nil, testLogger(), providers...)
	t.Cleanup(oauth.Close)
	return oauth
}

// newTestOAuthWithStore builds an OAuthService backed by the dev database so
// callbacks can create and look up real users.
func newTestOAuthWithStore(t *testing.T, providers ...OAuthProvider) (*OAuthService, *store.Store) {
	t.Helper()

	svc, st := newTestService(t)
	oauth := NewOAuthService(svc, testLogger(), providers...)
	t.Cleanup(oauth.Close)
	return oauth, st
}

func TestStateStoreRoundtripAndDeleteOnRead(t *testing.T) {
	states := newStateStore()
	t.Cleanup(states.Close)

	state, err := states.NewState("github")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if state == "" {
		t.Fatal("NewState returned an empty state")
	}

	provider, ok := states.Validate(state)
	if !ok || provider != "github" {
		t.Fatalf("Validate = (%q, %v), want (github, true)", provider, ok)
	}

	// Single-use: a second validation must fail.
	if _, ok := states.Validate(state); ok {
		t.Fatal("Validate accepted an already-consumed state")
	}
}

func TestStateStoreTTLExpiry(t *testing.T) {
	states := newStateStore()
	t.Cleanup(states.Close)

	base := time.Now()
	states.now = func() time.Time { return base }

	state, err := states.NewState("github")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	// Just before expiry the state is still valid.
	states.now = func() time.Time { return base.Add(oauthStateTTL - time.Second) }
	if _, ok := states.Validate(state); !ok {
		t.Fatal("Validate rejected a state before its TTL")
	}

	states.now = func() time.Time { return base }
	fresh, err := states.NewState("github")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	states.now = func() time.Time { return base.Add(oauthStateTTL + time.Second) }
	if _, ok := states.Validate(fresh); ok {
		t.Fatal("Validate accepted an expired state")
	}
}

func TestStateStoreCleanup(t *testing.T) {
	states := newStateStore()
	t.Cleanup(states.Close)

	base := time.Now()
	states.now = func() time.Time { return base }

	state, err := states.NewState("github")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	states.cleanup(base.Add(oauthStateTTL + time.Minute))

	states.mu.Lock()
	_, present := states.entries[state]
	states.mu.Unlock()
	if present {
		t.Fatal("cleanup left an expired state behind")
	}
}

func TestOAuthBeginUnknownProvider(t *testing.T) {
	oauth := newTestOAuth(t, &fakeOAuthProvider{name: "github"})

	if _, _, err := oauth.Begin(context.Background(), "gitlab", ""); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("Begin(unknown) error = %v, want ErrProviderDisabled", err)
	}
}

func TestOAuthBeginReturnsProviderURLAndState(t *testing.T) {
	oauth := newTestOAuth(t, &fakeOAuthProvider{name: "github"})

	loginURL, state, err := oauth.Begin(context.Background(), "github", "http://localhost:8000")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if state == "" {
		t.Fatal("Begin returned an empty state")
	}
	if want := "https://github.example/authorize?state=" + url.QueryEscape(state); loginURL != want {
		t.Fatalf("Begin url = %q, want %q", loginURL, want)
	}

	if provider, ok := oauth.states.Validate(state); !ok || provider != "github" {
		t.Fatalf("state not registered for github: (%q, %v)", provider, ok)
	}
}

func TestOAuthCallbackStateMismatch(t *testing.T) {
	oauth := newTestOAuth(t,
		&fakeOAuthProvider{name: "github", identity: &OAuthIdentity{Email: "a@example.com"}},
		&fakeOAuthProvider{name: "gitlab", identity: &OAuthIdentity{Email: "a@example.com"}},
	)
	ctx := context.Background()

	// Unknown state.
	if _, err := oauth.Callback(ctx, "github", "code", "bogus-state"); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("Callback(bad state) error = %v, want ErrStateMismatch", err)
	}

	// State issued for one provider cannot complete another.
	if _, _, err := oauth.Begin(ctx, "github", ""); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	state, err := oauth.states.NewState("github")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if _, err := oauth.Callback(ctx, "gitlab", "code", state); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("Callback(foreign provider) error = %v, want ErrStateMismatch", err)
	}
}

func TestOAuthCallbackMissingEmail(t *testing.T) {
	oauth := newTestOAuth(t, &fakeOAuthProvider{name: "github", identity: &OAuthIdentity{}})
	ctx := context.Background()

	state, err := oauth.states.NewState("github")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if _, err := oauth.Callback(ctx, "github", "code", state); !errors.Is(err, ErrMissingEmail) {
		t.Fatalf("Callback(no email) error = %v, want ErrMissingEmail", err)
	}
}

func TestOAuthCallbackUnknownProvider(t *testing.T) {
	oauth := newTestOAuth(t, &fakeOAuthProvider{name: "github", identity: &OAuthIdentity{Email: "a@example.com"}})

	if _, err := oauth.Callback(context.Background(), "gitlab", "code", "state"); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("Callback(unknown provider) error = %v, want ErrProviderDisabled", err)
	}
}

func TestOAuthCallbackCreatesUser(t *testing.T) {
	provider := &fakeOAuthProvider{name: "github"}
	oauth, st := newTestOAuthWithStore(t, provider)
	ctx := context.Background()

	email := uniqueEmail("oauth-new")
	cleanupUser(t, st, email)
	provider.identity = &OAuthIdentity{Email: email, Name: "New User"}

	_, state, err := oauth.Begin(ctx, "github", "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	result, err := oauth.Callback(ctx, "github", "auth-code", state)
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if result.User.Email != email {
		t.Errorf("Callback email = %q, want %q", result.User.Email, email)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("Callback returned an incomplete token pair: %+v", result)
	}

	user, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if user.PasswordHash != nil {
		t.Error("OAuth user has a password hash, want nil")
	}
}

func TestOAuthCallbackExistingUserLogsIn(t *testing.T) {
	provider := &fakeOAuthProvider{name: "github"}
	oauth, st := newTestOAuthWithStore(t, provider)
	ctx := context.Background()

	email := uniqueEmail("oauth-existing")
	cleanupUser(t, st, email)
	registered, err := oauth.auth.Register(ctx, email, "s3cret-password")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	provider.identity = &OAuthIdentity{Email: email}
	_, state, err := oauth.Begin(ctx, "github", "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	result, err := oauth.Callback(ctx, "github", "auth-code", state)
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if result.User.ID != registered.User.ID {
		t.Fatalf("Callback user ID = %q, want existing %q", result.User.ID, registered.User.ID)
	}

	// The local password must still work: OAuth login must not clobber it.
	if _, err := oauth.auth.Login(ctx, email, "s3cret-password"); err != nil {
		t.Fatalf("Login after OAuth: %v", err)
	}
}
