package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// StateCookieName is the cookie carrying the OAuth anti-CSRF state between the
// login redirect and the provider callback. The HTTP layer owns writing it; the
// name lives here so both sides agree.
const StateCookieName = "gotham_oauth_state"

// OAuth state bookkeeping. A state is single-use and expires after stateTTL;
// expired entries are swept by a background goroutine.
const (
	oauthStateTTL           = 10 * time.Minute
	oauthStateCleanupPeriod = time.Minute
	oauthStateBytes         = 32
)

// OAuthProvider abstracts an OAuth2 identity provider so the service can drive
// several of them uniformly.
type OAuthProvider interface {
	// Name is the stable provider identifier used in URLs ("github").
	Name() string
	// AuthCodeURL is the provider authorization URL for the given state.
	AuthCodeURL(state string) string
	// Exchange swaps an authorization code for an access token.
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	// Identity resolves the authenticated account behind tok.
	Identity(ctx context.Context, tok *oauth2.Token) (*OAuthIdentity, error)
}

// OAuthIdentity is the provider-supplied account information. Email is required;
// the remaining fields are optional.
type OAuthIdentity struct {
	Email     string
	Name      string
	AvatarURL string
}

// OAuthService coordinates the OAuth2 login flow: it mints and validates the
// anti-CSRF state, delegates the provider round-trip, and then issues a session
// through the shared auth Service.
type OAuthService struct {
	providers map[string]OAuthProvider
	auth      *Service
	logger    *slog.Logger
	states    *stateStore
}

// NewOAuthService builds an OAuthService from the enabled providers. Nil
// providers are ignored, so callers may pass a provider constructor's nil
// return directly. The auth Service mints the resulting sessions.
func NewOAuthService(auth *Service, logger *slog.Logger, providers ...OAuthProvider) *OAuthService {
	if logger == nil {
		logger = slog.Default()
	}

	registered := make(map[string]OAuthProvider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		registered[provider.Name()] = provider
	}

	return &OAuthService{
		providers: registered,
		auth:      auth,
		logger:    logger,
		states:    newStateStore(),
	}
}

// Begin starts an authorization flow. It returns the provider URL to redirect
// the browser to and the freshly minted state that must be bound to the browser
// (as the state cookie) and echoed back by the provider. An unknown or disabled
// provider yields ErrProviderDisabled.
//
// redirectBase is the post-login origin the browser will return to; the flow
// stores nothing server-side for it because the HTTP layer derives the same
// value from configuration, but it is logged for diagnostics.
func (s *OAuthService) Begin(ctx context.Context, providerName, redirectBase string) (url string, state string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return "", "", fmt.Errorf("%w: %s", ErrProviderDisabled, providerName)
	}

	state, err = s.states.NewState(providerName)
	if err != nil {
		return "", "", err
	}

	s.logger.Debug("oauth: begin authorization",
		"provider", providerName,
		"redirect_base", redirectBase)

	return provider.AuthCodeURL(state), state, nil
}

// Callback completes an authorization flow. It consumes state (single-use),
// exchanges code for a provider token, resolves the identity, and finds or
// creates the local account before issuing a token pair through the same path as
// a password login. A missing, expired, or foreign state yields ErrStateMismatch;
// an identity without an email yields ErrMissingEmail.
func (s *OAuthService) Callback(ctx context.Context, providerName, code, stateFromQuery string) (*AuthResult, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderDisabled, providerName)
	}

	storedProvider, ok := s.states.Validate(stateFromQuery)
	if !ok || storedProvider != providerName {
		return nil, ErrStateMismatch
	}

	token, err := provider.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("auth: oauth exchange: %w", err)
	}

	identity, err := provider.Identity(ctx, token)
	if err != nil {
		return nil, err
	}

	email, err := NormalizeEmail(identity.Email)
	if err != nil {
		return nil, ErrMissingEmail
	}

	user, err := s.auth.store.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		// Existing account: log in below.
	case errors.Is(err, pgx.ErrNoRows):
		user, err = s.createOAuthUser(ctx, email)
	default:
		return nil, fmt.Errorf("auth: oauth get user: %w", err)
	}
	if err != nil {
		return nil, err
	}

	return s.auth.IssueSession(ctx, user)
}

// createOAuthUser inserts an account for an OAuth-only identity (no local
// password, empty avatar). A concurrent insert of the same email is resolved by
// re-reading the row rather than failing the login.
func (s *OAuthService) createOAuthUser(ctx context.Context, email string) (sqlc.User, error) {
	// Closed registration applies to every account-creation path (P-A2): an
	// unseen OAuth identity may only bootstrap an empty instance, or sign up
	// while the test/dev override is on. Existing accounts keep signing in.
	count, err := s.auth.store.CountUsers(ctx)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("auth: oauth count users: %w", err)
	}
	if count > 0 && !s.auth.AllowOpenRegistration {
		return sqlc.User{}, ErrRegistrationClosed
	}

	var user sqlc.User
	if count == 0 && !s.auth.AllowOpenRegistration {
		user, err = s.auth.store.CreateFirstUser(ctx, email, nil)
	} else {
		user, err = s.auth.store.CreateUser(ctx, email, nil)
	}
	if err == nil {
		return user, nil
	}
	if errors.Is(err, store.ErrInstanceHasAccount) {
		// We lost the bootstrap race. If the winner is this same identity, the
		// callback is a valid login, not a closed registration.
		if existing, readErr := s.auth.store.GetUserByEmail(ctx, email); readErr == nil {
			return existing, nil
		}
		return sqlc.User{}, ErrRegistrationClosed
	}
	if !isUniqueViolation(err) {
		return sqlc.User{}, fmt.Errorf("auth: oauth create user: %w", err)
	}

	user, err = s.auth.store.GetUserByEmail(ctx, email)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("auth: oauth get user after conflict: %w", err)
	}
	return user, nil
}

// Close stops the background state-cleanup goroutine. It is safe to call more
// than once.
func (s *OAuthService) Close() {
	if s.states != nil {
		s.states.Close()
	}
}

// stateStore keeps issued OAuth states in memory, each bound to the provider
// that started the flow. Entries are single-use and expire after oauthStateTTL.
type stateStore struct {
	mu      sync.Mutex
	entries map[string]oauthStateEntry
	now     func() time.Time
	stop    chan struct{}
	once    sync.Once
}

// oauthStateEntry is one pending authorization.
type oauthStateEntry struct {
	provider  string
	expiresAt time.Time
}

// newStateStore builds a store and starts its cleanup goroutine. Callers must
// Close it to stop that goroutine.
func newStateStore() *stateStore {
	s := &stateStore{
		entries: make(map[string]oauthStateEntry),
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// NewState mints a random state bound to provider and remembers it until TTL
// expiry.
func (s *stateStore) NewState(provider string) (string, error) {
	buf := make([]byte, oauthStateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate oauth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	s.entries[state] = oauthStateEntry{
		provider:  provider,
		expiresAt: s.now().Add(oauthStateTTL),
	}
	s.mu.Unlock()

	return state, nil
}

// Validate consumes state (delete-on-read) and reports the provider that started
// the flow. It returns ok=false for an unknown, already-consumed, or expired
// state.
func (s *stateStore) Validate(state string) (provider string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[state]
	if !ok {
		return "", false
	}
	delete(s.entries, state)

	if s.now().After(entry.expiresAt) {
		return "", false
	}
	return entry.provider, true
}

// cleanupLoop evicts expired states until Close is called.
func (s *stateStore) cleanupLoop() {
	ticker := time.NewTicker(oauthStateCleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.cleanup(now)
		}
	}
}

// cleanup drops states whose expiry has passed.
func (s *stateStore) cleanup(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for state, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, state)
		}
	}
}

// Close stops the cleanup goroutine. It is safe to call more than once.
func (s *stateStore) Close() {
	s.once.Do(func() { close(s.stop) })
}
