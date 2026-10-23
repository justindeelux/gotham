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

const (
	StateCookieName       = "gotham_oauth_state"
	FlowCookieName        = "gotham_oauth_flow"
	StateCookieNameSecure = "__Host-gotham_oauth_state"
	FlowCookieNameSecure  = "__Host-gotham_oauth_flow"
)

const (
	oauthStateTTL           = 10 * time.Minute
	oauthStateCleanupPeriod = time.Minute
	oauthStateBytes         = 32
	oauthStateCapacity      = 10000
)

var errStateStoreFull = errors.New("auth: oauth state store full")

type OAuthProvider interface {
	Name() string
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	Identity(ctx context.Context, tok *oauth2.Token) (*OAuthIdentity, error)
}

type OAuthIdentity struct {
	Email     string
	Name      string
	AvatarURL string
}

type OAuthService struct {
	providers map[string]OAuthProvider
	auth      *Service
	logger    *slog.Logger
	states    *stateStore
}

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

func (s *OAuthService) SetGitHubControlPlaneURLSource(src func() string) {
	for _, provider := range s.providers {
		if g, ok := provider.(interface{ SetControlPlaneURLSource(func() string) }); ok {
			g.SetControlPlaneURLSource(src)
		}
	}
}

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

func (s *OAuthService) Callback(ctx context.Context, providerName, code, stateFromQuery string, meta SessionMeta) (*AuthResult, error) {
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
	case errors.Is(err, pgx.ErrNoRows):
		user, err = s.createOAuthUser(ctx, email)
	default:
		return nil, fmt.Errorf("auth: oauth get user: %w", err)
	}
	if err != nil {
		return nil, err
	}

	if avatar, ok := SanitizeAvatarURL(identity.AvatarURL); ok {
		if user.Avatar == nil || *user.Avatar != avatar {
			if updated, updErr := s.auth.store.UpdateUserAvatar(ctx, user.ID, &avatar); updErr != nil {
				s.logger.Warn("auth: oauth update avatar", "error", updErr, "email", email)
			} else {
				user = updated
			}
		}
	}

	return s.auth.IssueSession(ctx, user, meta)
}

func (s *OAuthService) createOAuthUser(ctx context.Context, email string) (sqlc.User, error) {
	count, err := s.auth.store.CountUsers(ctx)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("auth: oauth count users: %w", err)
	}
	if count > 0 && !s.auth.AllowOpenRegistration {
		return sqlc.User{}, ErrRegistrationClosed
	}

	var user sqlc.User
	if count == 0 {
		user, err = s.auth.store.CreateFirstUser(ctx, email, nil)
	} else {
		user, err = s.auth.store.CreateUser(ctx, email, nil)
	}
	if err == nil {
		return user, nil
	}
	if errors.Is(err, store.ErrInstanceHasAccount) {
		if s.auth.AllowOpenRegistration {
			user, err = s.auth.store.CreateUser(ctx, email, nil)
			if err == nil {
				return user, nil
			}
		} else {
			if existing, readErr := s.auth.store.GetUserByEmail(ctx, email); readErr == nil {
				return existing, nil
			}
			return sqlc.User{}, ErrRegistrationClosed
		}
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

func (s *OAuthService) Close() {
	if s.states != nil {
		s.states.Close()
	}
}

type stateStore struct {
	mu      sync.Mutex
	entries map[string]oauthStateEntry
	now     func() time.Time
	stop    chan struct{}
	once    sync.Once
}

type oauthStateEntry struct {
	provider  string
	expiresAt time.Time
}

func newStateStore() *stateStore {
	s := &stateStore{
		entries: make(map[string]oauthStateEntry),
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

func (s *stateStore) NewState(provider string) (string, error) {
	buf := make([]byte, oauthStateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate oauth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= oauthStateCapacity {
		return "", errStateStoreFull
	}
	s.entries[state] = oauthStateEntry{
		provider:  provider,
		expiresAt: s.now().Add(oauthStateTTL),
	}

	return state, nil
}

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

func (s *stateStore) cleanup(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for state, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, state)
		}
	}
}

func (s *stateStore) Close() {
	s.once.Do(func() { close(s.stop) })
}
