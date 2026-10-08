package providers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/justindeelux/gotham/internal/store"
)

// ProviderService is the control-plane surface the HTTP layer depends on.
type ProviderService interface {
	// List returns the caller's provider connections.
	List(ctx context.Context, userID uuid.UUID) ([]Provider, error)
	// Create stores a new, unconnected provider connection owned by userID.
	Create(ctx context.Context, userID uuid.UUID, input CreateProviderInput) (Provider, error)
	// Authorize starts an OAuth connection for a stored provider and returns
	// the provider authorization URL together with the state that binds the
	// browser to it.
	Authorize(ctx context.Context, userID, providerID uuid.UUID) (string, string, error)
	// Connect completes an OAuth connection: it redeems state, exchanges code
	// for tokens and persists them.
	Connect(ctx context.Context, userID, providerID uuid.UUID, code, state string) (Provider, error)
	// Disconnect deletes a stored provider connection together with its
	// credentials and cached repositories.
	Delete(ctx context.Context, userID, providerID uuid.UUID) error
	// ListRepos returns the repositories of one provider connection.
	ListRepos(ctx context.Context, userID, providerID uuid.UUID) ([]Repo, error)
	// ListBranches returns the branches of repo through one provider connection.
	ListBranches(ctx context.Context, userID, providerID uuid.UUID, repo string) ([]Branch, error)
	// AutoProvisionGitLab creates the GitLab OAuth application through the
	// GitLab API from a one-time admin token (never stored) and stores the
	// connection.
	AutoProvisionGitLab(ctx context.Context, userID uuid.UUID, input AutoProvisionGitLabInput) (Provider, error)
	// GitLabSetupInfoFor returns the exact redirect URI and scopes for a
	// manually created GitLab OAuth application.
	GitLabSetupInfoFor(baseURL, redirectURL string) (GitLabSetupInfo, error)
	// CreateWebhook installs a push hook on target.Repo with the caller's
	// stored connection for that provider and returns the provider's hook ID.
	CreateWebhook(ctx context.Context, target HookTarget, hook Webhook) (string, error)
	// DeleteWebhook removes the hook identified by hookID from target.Repo.
	// A hook the provider no longer knows about is a success.
	DeleteWebhook(ctx context.Context, target HookTarget, hookID string) error
	// CreatePullRequestComment posts body as a comment on pull request number
	// of target.Repo, using the caller's stored connection for that provider.
	CreatePullRequestComment(ctx context.Context, target HookTarget, number int, body string) error
}

// CreateProviderInput is the caller-supplied OAuth application for a new
// provider connection.
type CreateProviderInput struct {
	Name         string
	BaseURL      string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
}

// authorizer is implemented by provider sources that can build an authorization
// URL. It is separate from SourceProvider so that interface stays stable.
type authorizer interface {
	AuthCodeURL(state string) string
}

// pkceAuthorizer is implemented by provider sources that bind the
// authorization URL to an RFC 7636 S256 challenge. The service mints the
// verifier, keeps it beside the one-time state and redeems it in Connect, so
// the browser never sees it.
type pkceAuthorizer interface {
	AuthCodeURLWithPKCE(state, challenge string) string
}

// pkceExchanger is implemented by provider sources that redeem an
// authorization code with a PKCE verifier.
type pkceExchanger interface {
	ExchangeTokenWithVerifier(ctx context.Context, code, verifier string) (*oauth2.Token, error)
}

// tokenReporter is implemented by provider sources that can report the token
// their last client held. It lets the service persist a refresh that happened
// inside the client oauth2 built for the call. It is separate from
// SourceProvider so that interface stays stable.
type tokenReporter interface {
	Token() *oauth2.Token
}

// truncationReporter is implemented by provider sources that can report whether
// their last listing hit a page/offset bound. The service then keeps the
// previous cache instead of overwriting it with a partial list.
type truncationReporter interface {
	Truncated() bool
}

// Factory builds a SourceProvider for a stored connection.
type Factory func(p Provider) (SourceProvider, error)

// Config wires a Service. Repository is required; Logger defaults to
// slog.Default and Factories are merged over the built-in GitHub/GitLab/Gitea
// implementations (a test can override one). AllowUnsafeBaseURL permits the
// loopback/link-local base URLs tests use; production leaves it false.
type Config struct {
	Repository         Repository
	Logger             *slog.Logger
	Factories          map[string]Factory
	AllowUnsafeBaseURL bool
}

// Service coordinates provider connections and repository listing. It is safe
// for concurrent use.
type Service struct {
	repo               Repository
	logger             *slog.Logger
	factories          map[string]Factory
	allowUnsafeBaseURL bool
	states             *connectState
}

// NewService builds a Service from cfg.
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
		repo:               cfg.Repository,
		logger:             logger,
		factories:          factories,
		allowUnsafeBaseURL: cfg.AllowUnsafeBaseURL,
		states:             newConnectState(),
	}
}

// NewDefaultService builds the production service over st. It returns a nil
// ProviderService when st is nil (no database configured) so the HTTP layer
// skips mounting the routes. An empty secret selects an ephemeral key: provider
// credentials are still encrypted, but a restart makes stored tokens
// unreadable, so production must set GOTHAM_SECRET_KEY.
func NewDefaultService(st *store.Store, secret string, logger *slog.Logger) ProviderService {
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
	return NewService(Config{
		Repository: newStoreRepository(st, newSecretCipher(secret)),
		Logger:     logger,
	})
}

// List returns the caller's provider connections.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Provider, error) {
	if s.repo == nil {
		return nil, errors.New("providers: repository is not configured")
	}
	return s.repo.List(ctx, userID)
}

// Create stores a new, unconnected provider connection owned by userID. It
// validates the provider name, the required OAuth application fields and the
// base_url before the row is written.
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

// Authorize starts an OAuth connection for a stored provider. It returns the
// provider authorization URL and a state that binds the browser to this
// connection; the state is single-use and expires. Sources with PKCE support
// additionally bind the URL to a verifier the browser never sees.
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
		state, err := s.states.newPKCE(userID, providerID, verifier)
		if err != nil {
			return "", "", err
		}
		return pkce.AuthCodeURLWithPKCE(state, pkceChallenge(verifier)), state, nil
	}

	state, err := s.states.new(userID, providerID)
	if err != nil {
		return "", "", err
	}
	return auth.AuthCodeURL(state), state, nil
}

// Connect completes an OAuth connection started by Authorize: it redeems the
// state, exchanges the code for tokens and persists the access/refresh pair so
// later calls use the stored credentials. A PKCE verifier minted for the state
// is consumed with it and never accepted twice.
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

// exchangeToken redeems code on source, proving the PKCE verifier when the
// source minted one for this authorization. A source with PKCE support but no
// verifier for this state fails closed: the authorization was not bound, so
// the code must not be redeemed.
func (s *Service) exchangeToken(ctx context.Context, source SourceProvider, code, verifier string) (*oauth2.Token, error) {
	if pkce, ok := source.(pkceExchanger); ok {
		if verifier == "" {
			return nil, fmt.Errorf("%w: pkce verifier is missing", ErrValidation)
		}
		return pkce.ExchangeTokenWithVerifier(ctx, code, verifier)
	}
	return source.ExchangeToken(ctx, code)
}

// Delete removes a stored provider connection: its credentials and cached
// repositories go with it (repos_cache rows cascade), so disconnecting is a
// full credential forget. Deleting twice is a success.
func (s *Service) Delete(ctx context.Context, userID, providerID uuid.UUID) error {
	if s.repo == nil {
		return errors.New("providers: repository is not configured")
	}
	return s.repo.Delete(ctx, providerID, userID)
}

// ListRepos returns the repositories of one provider connection. A live fetch
// refreshes the cache; when the provider is unreachable the last cached list is
// served instead of failing.
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

// ListBranches returns the branches of repo through one provider connection.
// The stored token refreshes transparently inside the call, like ListRepos.
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

// CreateWebhook installs a push hook on target.Repo using the caller's stored
// connection for that provider and returns the provider's hook ID.
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

// DeleteWebhook removes the hook identified by hookID from target.Repo. A hook
// the provider has already forgotten is reported as deleted, so callers can
// run it twice without special-casing.
func (s *Service) DeleteWebhook(ctx context.Context, target HookTarget, hookID string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		return source.DeleteWebhook(ctx, tok, target.Repo, hookID)
	})
}

// CreatePullRequestComment posts a comment on a pull request (merge request
// on GitLab) of target.Repo using the caller's stored connection.
func (s *Service) CreatePullRequestComment(ctx context.Context, target HookTarget, number int, body string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		return source.CreatePullRequestComment(ctx, tok, target.Repo, number, body)
	})
}

// AddDeployKey registers a public key on target.Repo using the caller's stored
// connection for that provider and returns the provider's key ID.
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

// RemoveDeployKey removes the key identified by keyID from target.Repo. A key
// the provider has already forgotten is reported as removed, so callers can
// run it twice without special-casing.
func (s *Service) RemoveDeployKey(ctx context.Context, target HookTarget, keyID string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return s.runWithToken(ctx, source, connection, func(tok *oauth2.Token) error {
		return source.RemoveDeployKey(ctx, tok, target.Repo, keyID)
	})
}

// runWithToken runs fn with the connection's stored token and persists the pair
// when oauth2 refreshed it in place during the call. Without this, the next
// call would present a consumed refresh token (GitLab rotates both) and the
// connection would be bricked with invalid_grant.
func (s *Service) runWithToken(ctx context.Context, source SourceProvider, connection Provider, fn func(tok *oauth2.Token) error) error {
	tok := connection.token()
	err := fn(tok)

	// oauth2 swaps the token inside the client it built rather than mutating
	// tok, so read the latest pair back from the source when it can report it.
	latest := tok
	if reporter, ok := source.(tokenReporter); ok {
		if reported := reporter.Token(); reported != nil {
			latest = reported
		}
	}
	s.persistRefreshedToken(ctx, connection, latest)
	return err
}

// persistRefreshedToken stores a token oauth2 rotated during a call. It runs on
// a context detached from the request, because the write must survive a request
// that hit the HTTP cap or a disconnected client: GitLab rotates both tokens,
// so losing the rotated pair ends in invalid_grant. It is best-effort: a failed
// write is logged and the call result is unaffected.
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

// tokenExpiry materialises a token's expiry as an optional time.
func tokenExpiry(tok *oauth2.Token) *time.Time {
	if tok == nil || tok.Expiry.IsZero() {
		return nil
	}
	expiry := tok.Expiry
	return &expiry
}

// sameExpiry reports whether two optional expiries name the same instant.
func sameExpiry(a time.Time, b *time.Time) bool {
	if b == nil {
		return a.IsZero()
	}
	return a.Equal(*b)
}

// sourceForTarget resolves the stored connection that authenticates a webhook
// call and builds the provider implementation for it.
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

// chooseConnection picks the connection a webhook call runs on. One
// connection of that provider is always unambiguous; several are resolved
// against the host of the application's clone URL so two self-hosted Gitea
// (or GitLab) instances cannot steal each other's hooks.
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

// connectionHost returns the host a connection serves: the stored base_url
// when present (self-hosted), otherwise the provider's public host.
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
		// GitHub stores the REST API host, while clone URLs name the web host.
		if host == "api.github.com" {
			return "github.com"
		}
		return host
	}
	return ""
}

// publicHost is the host a connection with no base_url points at. Gitea is
// always self-hosted, so it has none.
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

// cloneHostOf extracts the lowercased host from a clone URL in any of the
// shapes Git produces (https://host/o/r.git, ssh://git@host/o/r.git or the
// scp-like git@host:o/r.git). The port is dropped: the same instance is often
// reached on 443 through a proxy and on 3000 directly.
func cloneHostOf(cloneURL string) string {
	raw := strings.TrimSpace(cloneURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		// scp-like syntax: git@host:owner/repo.git
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

// sourceProvider selects and builds the implementation for a stored connection.
// It re-validates the stored base_url at use time so a row written before
// validation, or out of band, cannot become an SSRF target.
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

// defaultFactories returns the built-in provider implementations. allowUnsafe
// disables the outbound guards for the loopback hosts tests use.
func defaultFactories(allowUnsafe bool) map[string]Factory {
	return map[string]Factory{
		NameGitHub: func(p Provider) (SourceProvider, error) { return newGitHubSource(p, allowUnsafe), nil },
		NameGitLab: func(p Provider) (SourceProvider, error) { return newGitLabSource(p, allowUnsafe), nil },
		NameGitea:  func(p Provider) (SourceProvider, error) { return newGiteaSource(p, allowUnsafe), nil },
	}
}

// connectStateTTL bounds how long an authorization started by Authorize stays
// redeemable.
const connectStateTTL = 10 * time.Minute

// connectStateCapacity bounds outstanding authorizations across all users, so a
// flood of starts cannot grow the store without limit. Expired entries are
// swept on write.
const connectStateCapacity = 10000

// connectStatePerUser bounds one account's outstanding authorizations, so a
// single owner cannot mint states until the global cap and make every other
// user's Authorize fail.
const connectStatePerUser = 20

// connectState keeps issued OAuth connect states in memory, each bound to the
// user and provider connection that started the flow. It is single-use and
// expires after connectStateTTL. There is no background goroutine: expired
// entries are swept opportunistically on the next write. The store is
// process-local, so a multi-instance control plane would need to share it
// (Redis/DB) before running provider connects on more than one node.
type connectState struct {
	mu      sync.Mutex
	entries map[string]connectStateEntry
	byUser  map[uuid.UUID]int
	now     func() time.Time
}

// connectStateEntry is one pending provider authorization. verifier holds the
// PKCE verifier minted for sources that bind the authorization URL to a
// challenge; it is empty for legacy authorizations and consumed with the state.
type connectStateEntry struct {
	userID     uuid.UUID
	providerID uuid.UUID
	verifier   string
	expiresAt  time.Time
}

// newConnectState builds an empty store.
func newConnectState() *connectState {
	return &connectState{
		entries: make(map[string]connectStateEntry),
		byUser:  make(map[uuid.UUID]int),
		now:     time.Now,
	}
}

// new mints a random single-use state bound to userID and providerID. It
// answers ErrTooManyRequests when the user's or the global cap is reached, so a
// caller gets a 429 rather than a 500 for everyone.
func (s *connectState) new(userID, providerID uuid.UUID) (string, error) {
	return s.newWithVerifier(userID, providerID, "")
}

// newPKCE mints a random single-use state bound to userID and providerID and
// stores verifier beside it, under the same caps, single-use and expiry as
// new.
func (s *connectState) newPKCE(userID, providerID uuid.UUID, verifier string) (string, error) {
	return s.newWithVerifier(userID, providerID, verifier)
}

// newWithVerifier mints a random single-use state bound to userID and
// providerID, keeping verifier for the Connect that redeems it.
func (s *connectState) newWithVerifier(userID, providerID uuid.UUID, verifier string) (string, error) {
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

// redeem consumes state and reports whether it bound exactly this user and
// provider, returning the PKCE verifier minted for it ("", false has none). A
// missing, expired, already-used or foreign state is false.
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

// releaseLocked decrements a user's outstanding count; the caller holds the lock.
func (s *connectState) releaseLocked(userID uuid.UUID) {
	s.byUser[userID]--
	if s.byUser[userID] <= 0 {
		delete(s.byUser, userID)
	}
}

// sweepLocked drops expired entries; the caller holds the lock.
func (s *connectState) sweepLocked(now time.Time) {
	for state, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, state)
			s.releaseLocked(entry.userID)
		}
	}
}
