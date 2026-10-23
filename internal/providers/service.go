package providers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/justindeelux/gotham/internal/store"
)

type ProviderService interface {
	List(ctx context.Context, userID uuid.UUID) ([]Provider, error)
	Create(ctx context.Context, userID uuid.UUID, input CreateProviderInput) (Provider, error)
	Authorize(ctx context.Context, userID, providerID uuid.UUID) (string, string, error)
	Connect(ctx context.Context, userID, providerID uuid.UUID, code, state string) (Provider, error)
	ConnectCallback(ctx context.Context, code, state string) (string, error)
	Delete(ctx context.Context, userID, providerID uuid.UUID) error
	ListRepos(ctx context.Context, userID, providerID uuid.UUID) ([]Repo, error)
	ListBranches(ctx context.Context, userID, providerID uuid.UUID, repo string) ([]Branch, error)
	AutoProvisionGitLab(ctx context.Context, userID uuid.UUID, input AutoProvisionGitLabInput) (Provider, error)
	GitLabSetupInfoFor(baseURL, redirectURL string) (GitLabSetupInfo, error)
	CreateWebhook(ctx context.Context, target HookTarget, hook Webhook) (string, error)
	DeleteWebhook(ctx context.Context, target HookTarget, hookID string) error
	CreatePullRequestComment(ctx context.Context, target HookTarget, number int, body string) error
}

type CreateProviderInput struct {
	Name         string
	BaseURL      string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
}

type authorizer interface {
	AuthCodeURL(state string) string
}

type pkceAuthorizer interface {
	AuthCodeURLWithPKCE(state, challenge string) string
}

type pkceExchanger interface {
	ExchangeTokenWithVerifier(ctx context.Context, code, verifier string) (*oauth2.Token, error)
}

type tokenReporter interface {
	Token() *oauth2.Token
}

type truncationReporter interface {
	Truncated() bool
}

type Factory func(p Provider) (SourceProvider, error)

type ConnectionApplication struct {
	Name     string
	Provider string
	CloneURL string
}

type ConnectionApplicationsFunc func(ctx context.Context, userID uuid.UUID) ([]ConnectionApplication, error)

type Config struct {
	Repository             Repository
	Logger                 *slog.Logger
	Factories              map[string]Factory
	AllowUnsafeBaseURL     bool
	ConnectionApplications ConnectionApplicationsFunc
}

type Service struct {
	repo                   Repository
	logger                 *slog.Logger
	factories              map[string]Factory
	allowUnsafeBaseURL     bool
	connectionApplications ConnectionApplicationsFunc
	states                 *connectState
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	factories := defaultFactories(cfg.AllowUnsafeBaseURL)
	for name, factory := range cfg.Factories {
		factories[name] = factory
	}

	return &Service{
		repo:                   cfg.Repository,
		logger:                 logger,
		factories:              factories,
		allowUnsafeBaseURL:     cfg.AllowUnsafeBaseURL,
		connectionApplications: cfg.ConnectionApplications,
		states:                 newConnectState(),
	}
}

func NewDefaultService(st *store.Store, secret string, logger *slog.Logger, listApps ...ConnectionApplicationsFunc) ProviderService {
	if st == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(secret) == "" {
		secret = randomSecret()
		logger.Warn("providers: secret_key is empty; credential encryption uses an ephemeral key")
	}
	cfg := Config{
		Repository: newStoreRepository(st, newSecretCipher(secret)),
		Logger:     logger,
	}
	if len(listApps) > 0 {
		cfg.ConnectionApplications = listApps[0]
	}
	return NewService(cfg)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Provider, error) {
	if s.repo == nil {
		return nil, errors.New("providers: repository is not configured")
	}
	return s.repo.List(ctx, userID)
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, input CreateProviderInput) (Provider, error) {
	if s.repo == nil {
		return Provider{}, errors.New("providers: repository is not configured")
	}

	name := strings.TrimSpace(input.Name)
	if _, ok := s.factories[name]; !ok {
		return Provider{}, fmt.Errorf("%w: %s", ErrUnsupported, name)
	}
	if strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientSecret) == "" {
		return Provider{}, fmt.Errorf("%w: client_id and client_secret are required", ErrValidation)
	}
	if strings.TrimSpace(input.RedirectURL) == "" {
		return Provider{}, fmt.Errorf("%w: redirect_url is required", ErrValidation)
	}

	base := strings.TrimSpace(input.BaseURL)
	if name == NameGitLab {
		base = gitLabInstanceBase(base)
	}
	if name == NameGitea && base == "" {
		return Provider{}, fmt.Errorf("%w: gitea requires a base_url", ErrValidation)
	}
	if err := validateBaseURL(base, s.allowUnsafeBaseURL); err != nil {
		return Provider{}, err
	}

	return s.repo.Create(ctx, Provider{
		UserID:       userID,
		Name:         name,
		BaseURL:      base,
		ClientID:     strings.TrimSpace(input.ClientID),
		ClientSecret: input.ClientSecret,
		RedirectURL:  strings.TrimSpace(input.RedirectURL),
		Scopes:       strings.TrimSpace(input.Scopes),
	})
}

func (s *Service) Authorize(ctx context.Context, userID, providerID uuid.UUID) (string, string, error) {
	if s.repo == nil {
		return "", "", errors.New("providers: repository is not configured")
	}
	provider, err := s.repo.Get(ctx, providerID, userID)
	if err != nil {
		return "", "", err
	}
	source, err := s.sourceProvider(provider)
	if err != nil {
		return "", "", err
	}
	auth, ok := source.(authorizer)
	if !ok {
		return "", "", fmt.Errorf("%w: %s cannot authorize", ErrUnsupported, provider.Name)
	}

	if pkce, ok := source.(pkceAuthorizer); ok {
		verifier, err := generatePKCEVerifier()
		if err != nil {
			return "", "", err
		}
		state, err := s.states.new(userID, providerID, verifier)
		if err != nil {
			return "", "", err
		}
		return pkce.AuthCodeURLWithPKCE(state, pkceChallenge(verifier)), state, nil
	}

	state, err := s.states.new(userID, providerID, "")
	if err != nil {
		return "", "", err
	}
	return auth.AuthCodeURL(state), state, nil
}

func (s *Service) Connect(ctx context.Context, userID, providerID uuid.UUID, code, state string) (Provider, error) {
	if s.repo == nil {
		return Provider{}, errors.New("providers: repository is not configured")
	}
	if strings.TrimSpace(code) == "" {
		return Provider{}, fmt.Errorf("%w: authorization code is empty", ErrValidation)
	}

	provider, err := s.repo.Get(ctx, providerID, userID)
	if err != nil {
		return Provider{}, err
	}
	verifier, ok := s.states.redeem(state, userID, providerID)
	if !ok {
		return Provider{}, fmt.Errorf("%w: invalid or expired oauth state", ErrValidation)
	}
	source, err := s.sourceProvider(provider)
	if err != nil {
		return Provider{}, err
	}

	tok, err := s.exchangeToken(ctx, source, code, verifier)
	if err != nil {
		s.logger.Warn("providers: token exchange failed",
			"provider_id", providerID.String(), "error", err)
		return Provider{}, fmt.Errorf("%w: authorization code exchange failed", ErrValidation)
	}
	if strings.TrimSpace(tok.AccessToken) == "" {
		return Provider{}, fmt.Errorf("%w: provider returned no access token", ErrValidation)
	}

	updated, err := s.repo.UpdateToken(ctx, provider.ID, tok.AccessToken, tok.RefreshToken, tokenExpiry(tok))
	if err != nil {
		return Provider{}, err
	}
	return updated, nil
}

func (s *Service) exchangeToken(ctx context.Context, source SourceProvider, code, verifier string) (*oauth2.Token, error) {
	if pkce, ok := source.(pkceExchanger); ok {
		if verifier == "" {
			return nil, fmt.Errorf("%w: pkce verifier is missing", ErrValidation)
		}
		return pkce.ExchangeTokenWithVerifier(ctx, code, verifier)
	}
	return source.ExchangeToken(ctx, code)
}

func (s *Service) Delete(ctx context.Context, userID, providerID uuid.UUID) error {
	if s.repo == nil {
		return errors.New("providers: repository is not configured")
	}
	connection, err := s.repo.Get(ctx, providerID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if s.connectionApplications != nil {
		using, err := s.connectionUsers(ctx, userID, connection)
		if err != nil {
			return err
		}
		if len(using) > 0 {
			return fmt.Errorf("%w: %s", ErrInUse, strings.Join(using, ", "))
		}
	}
	return s.repo.Delete(ctx, providerID, userID)
}

func (s *Service) connectionUsers(ctx context.Context, userID uuid.UUID, connection Provider) ([]string, error) {
	applications, err := s.connectionApplications(ctx, userID)
	if err != nil {
		return nil, err
	}
	host := connectionHost(connection)
	if host == "" {
		return nil, nil
	}
	var using []string
	for _, app := range applications {
		if app.Provider == connection.Name && cloneHostOf(app.CloneURL) == host {
			using = append(using, app.Name)
		}
	}
	sort.Strings(using)
	return using, nil
}

func (s *Service) ConnectCallback(ctx context.Context, code, state string) (string, error) {
	if s.repo == nil {
		return "", errors.New("providers: repository is not configured")
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return "", fmt.Errorf("%w: authorization code or state is empty", ErrValidation)
	}
	userID, providerID, ok := s.states.lookup(state)
	if !ok {
		return "", fmt.Errorf("%w: invalid or expired oauth state", ErrValidation)
	}
	provider, err := s.Connect(ctx, userID, providerID, code, state)
	if err != nil {
		return "", err
	}
	return provider.Name, nil
}

func (s *Service) ListRepos(ctx context.Context, userID, providerID uuid.UUID) ([]Repo, error) {
	if s.repo == nil {
		return nil, errors.New("providers: repository is not configured")
	}

	provider, err := s.repo.Get(ctx, providerID, userID)
	if err != nil {
		return nil, err
	}
	if !provider.Connected() {
		return nil, fmt.Errorf("%w: %s", ErrNotConnected, provider.Name)
	}

	source, err := s.sourceProvider(provider)
	if err != nil {
		return nil, err
	}

	var repos []Repo
	err = s.runWithToken(ctx, source, provider, func(tok *oauth2.Token) error {
		var callErr error
		repos, callErr = source.ListRepos(ctx, tok)
		return callErr
	})
	if err != nil {
		if cached, cacheErr := s.repo.ListCachedRepos(ctx, providerID); cacheErr == nil && len(cached) > 0 {
			s.logger.Warn("providers: live repo list failed; serving cache",
				"provider_id", providerID.String(),
				"error", err)
			return cached, nil
		}
		return nil, err
	}

	truncated := false
	if reporter, ok := source.(truncationReporter); ok {
		truncated = reporter.Truncated()
	}
	if truncated {
		s.logger.Warn("providers: repo listing truncated; keeping the previous cache",
			"provider_id", providerID.String(),
			"repos", len(repos))
		return repos, nil
	}

	if err := s.repo.ReplaceRepos(ctx, providerID, repos); err != nil {
		s.logger.Warn("providers: cache repo list failed",
			"provider_id", providerID.String(),
			"error", err)
	}
	return repos, nil
}

func (s *Service) ListBranches(ctx context.Context, userID, providerID uuid.UUID, repo string) ([]Branch, error) {
	if s.repo == nil {
		return nil, errors.New("providers: repository is not configured")
	}

	provider, err := s.repo.Get(ctx, providerID, userID)
	if err != nil {
		return nil, err
	}
	if !provider.Connected() {
		return nil, fmt.Errorf("%w: %s", ErrNotConnected, provider.Name)
	}

	source, err := s.sourceProvider(provider)
	if err != nil {
		return nil, err
	}

	var branches []Branch
	err = s.runWithToken(ctx, source, provider, func(tok *oauth2.Token) error {
		var callErr error
		branches, callErr = source.ListBranches(ctx, tok, repo)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if branches == nil {
		return []Branch{}, nil
	}
	return branches, nil
}

func (s *Service) CreateWebhook(ctx context.Context, target HookTarget, hook Webhook) (string, error) {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(hook.URL) == "" {
		return "", fmt.Errorf("%w: webhook url is empty", ErrValidation)
	}
	var hookID string
	err = s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		var callErr error
		hookID, callErr = source.CreateWebhook(ctx, tok, target.Repo, hook)
		return callErr
	})
	return hookID, err
}

func (s *Service) DeleteWebhook(ctx context.Context, target HookTarget, hookID string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		return source.DeleteWebhook(ctx, tok, target.Repo, hookID)
	})
}

func (s *Service) CreatePullRequestComment(ctx context.Context, target HookTarget, number int, body string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		return source.CreatePullRequestComment(ctx, tok, target.Repo, number, body)
	})
}

func (s *Service) AddDeployKey(ctx context.Context, target HookTarget, key DeployKey) (string, error) {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return "", err
	}
	if err := validateDeployKey(key); err != nil {
		return "", err
	}
	var keyID string
	err = s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		var callErr error
		keyID, callErr = source.AddDeployKey(ctx, tok, target.Repo, key)
		return callErr
	})
	return keyID, err
}

func (s *Service) RemoveDeployKey(ctx context.Context, target HookTarget, keyID string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		return source.RemoveDeployKey(ctx, tok, target.Repo, keyID)
	})
}

func (s *Service) runWithToken(ctx context.Context, source SourceProvider, connection Provider, fn func(tok *oauth2.Token) error) error {
	tok := connection.token()
	err := fn(tok)

	latest := tok
	if reporter, ok := source.(tokenReporter); ok {
		if reported := reporter.Token(); reported != nil {
			latest = reported
		}
	}
	s.persistRefreshedToken(ctx, connection, latest)
	return err
}

func (s *Service) persistRefreshedToken(ctx context.Context, connection Provider, tok *oauth2.Token) {
	if tok == nil || (tok.AccessToken == connection.AccessToken &&
		tok.RefreshToken == connection.RefreshToken &&
		sameExpiry(tok.Expiry, connection.TokenExpiresAt)) {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := s.repo.UpdateToken(persistCtx, connection.ID, tok.AccessToken, tok.RefreshToken, tokenExpiry(tok)); err != nil {
		s.logger.Warn("providers: persist refreshed token failed",
			"provider_id", connection.ID.String(), "error", err)
	}
}

func tokenExpiry(tok *oauth2.Token) *time.Time {
	if tok == nil || tok.Expiry.IsZero() {
		return nil
	}
	expiry := tok.Expiry
	return &expiry
}

func sameExpiry(a time.Time, b *time.Time) bool {
	if b == nil {
		return a.IsZero()
	}
	return a.Equal(*b)
}

func (s *Service) sourceForTarget(ctx context.Context, target HookTarget) (SourceProvider, Provider, error) {
	if s.repo == nil {
		return nil, Provider{}, errors.New("providers: repository is not configured")
	}
	if strings.TrimSpace(target.Repo) == "" {
		return nil, Provider{}, fmt.Errorf("%w: repository is empty", ErrValidation)
	}

	connections, err := s.repo.List(ctx, target.UserID)
	if err != nil {
		return nil, Provider{}, err
	}
	candidates := make([]Provider, 0, len(connections))
	for _, connection := range connections {
		if connection.Name == target.Provider {
			candidates = append(candidates, connection)
		}
	}
	if len(candidates) == 0 {
		return nil, Provider{}, fmt.Errorf("%w: %s", ErrNotFound, target.Provider)
	}

	selected, err := chooseConnection(candidates, target.CloneURL)
	if err != nil {
		return nil, Provider{}, err
	}
	if !selected.Connected() {
		return nil, Provider{}, fmt.Errorf("%w: %s", ErrNotConnected, selected.Name)
	}
	source, err := s.sourceProvider(selected)
	if err != nil {
		return nil, Provider{}, err
	}
	return source, selected, nil
}

func chooseConnection(candidates []Provider, cloneURL string) (Provider, error) {
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	cloneHost := cloneHostOf(cloneURL)
	if cloneHost == "" {
		return Provider{}, fmt.Errorf("%w: %d connections for this provider and no clone url to tell them apart",
			ErrValidation, len(candidates))
	}

	matches := make([]Provider, 0, 1)
	for _, candidate := range candidates {
		if connectionHost(candidate) == cloneHost {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Provider{}, fmt.Errorf("%w: no %s connection for %s", ErrNotFound, candidates[0].Name, cloneHost)
	default:
		return Provider{}, fmt.Errorf("%w: %d %s connections for %s",
			ErrValidation, len(matches), matches[0].Name, cloneHost)
	}
}

func connectionHost(p Provider) string {
	base := strings.TrimSpace(p.BaseURL)
	if base == "" {
		return publicHost(p.Name)
	}
	if !strings.Contains(base, "://") {
		base = "//" + base
	}
	if u, err := url.Parse(base); err == nil && u.Hostname() != "" {
		host := strings.ToLower(u.Hostname())
		if host == "api.github.com" {
			return "github.com"
		}
		return host
	}
	return ""
}

func publicHost(provider string) string {
	switch provider {
	case NameGitHub:
		return "github.com"
	case NameGitLab:
		return "gitlab.com"
	default:
		return ""
	}
}

func cloneHostOf(cloneURL string) string {
	raw := strings.TrimSpace(cloneURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		if at := strings.IndexByte(raw, '@'); at >= 0 {
			raw = raw[at+1:]
		}
		if colon := strings.IndexByte(raw, ':'); colon > 0 {
			return strings.ToLower(raw[:colon])
		}
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (s *Service) sourceProvider(p Provider) (SourceProvider, error) {
	factory, ok := s.factories[p.Name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, p.Name)
	}
	if p.Name == NameGitea && strings.TrimSpace(p.BaseURL) == "" {
		return nil, fmt.Errorf("%w: gitea requires a base_url", ErrValidation)
	}
	if err := validateBaseURL(p.BaseURL, s.allowUnsafeBaseURL); err != nil {
		return nil, err
	}
	return factory(p)
}

func defaultFactories(allowUnsafe bool) map[string]Factory {
	return map[string]Factory{
		NameGitHub: func(p Provider) (SourceProvider, error) { return newGitHubSource(p, allowUnsafe), nil },
		NameGitLab: func(p Provider) (SourceProvider, error) { return newGitLabSource(p, allowUnsafe), nil },
		NameGitea:  func(p Provider) (SourceProvider, error) { return newGiteaSource(p, allowUnsafe), nil },
	}
}

const connectStateTTL = 10 * time.Minute

const connectStateCapacity = 10000

const connectStatePerUser = 20

type connectState struct {
	mu      sync.Mutex
	entries map[string]connectStateEntry
	byUser  map[uuid.UUID]int
	now     func() time.Time
}

type connectStateEntry struct {
	userID     uuid.UUID
	providerID uuid.UUID
	verifier   string
	expiresAt  time.Time
}

func newConnectState() *connectState {
	return &connectState{
		entries: make(map[string]connectStateEntry),
		byUser:  make(map[uuid.UUID]int),
		now:     time.Now,
	}
}

func (s *connectState) new(userID, providerID uuid.UUID, verifier string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("providers: generate connect state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byUser[userID] >= connectStatePerUser || len(s.entries) >= connectStateCapacity {
		s.sweepLocked(s.now())
		if s.byUser[userID] >= connectStatePerUser || len(s.entries) >= connectStateCapacity {
			return "", ErrTooManyRequests
		}
	}
	s.entries[state] = connectStateEntry{
		userID:     userID,
		providerID: providerID,
		verifier:   verifier,
		expiresAt:  s.now().Add(connectStateTTL),
	}
	s.byUser[userID]++
	return state, nil
}

func (s *connectState) redeem(state string, userID, providerID uuid.UUID) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[state]
	if !ok {
		return "", false
	}
	delete(s.entries, state)
	s.releaseLocked(entry.userID)
	if s.now().After(entry.expiresAt) {
		return "", false
	}
	if entry.userID != userID || entry.providerID != providerID {
		return "", false
	}
	return entry.verifier, true
}

func (s *connectState) lookup(state string) (userID, providerID uuid.UUID, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[state]
	if !ok || s.now().After(entry.expiresAt) {
		return uuid.Nil, uuid.Nil, false
	}
	return entry.userID, entry.providerID, true
}

func (s *connectState) releaseLocked(userID uuid.UUID) {
	s.byUser[userID]--
	if s.byUser[userID] <= 0 {
		delete(s.byUser, userID)
	}
}

func (s *connectState) sweepLocked(now time.Time) {
	for state, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, state)
			s.releaseLocked(entry.userID)
		}
	}
}
