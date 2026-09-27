package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// ProviderService is the control-plane surface the HTTP layer depends on.
type ProviderService interface {
	// List returns the caller's provider connections.
	List(ctx context.Context, userID uuid.UUID) ([]Provider, error)
	// ListRepos returns the repositories of one provider connection.
	ListRepos(ctx context.Context, userID, providerID uuid.UUID) ([]Repo, error)
	// CreateWebhook installs a push hook on target.Repo with the caller's
	// stored connection for that provider and returns the provider's hook ID.
	CreateWebhook(ctx context.Context, target HookTarget, hook Webhook) (string, error)
	// DeleteWebhook removes the hook identified by hookID from target.Repo.
	// A hook the provider no longer knows about is a success.
	DeleteWebhook(ctx context.Context, target HookTarget, hookID string) error
}

// Factory builds a SourceProvider for a stored connection.
type Factory func(p Provider) (SourceProvider, error)

// Config wires a Service. Repository is required; Logger defaults to
// slog.Default and Factories are merged over the built-in GitHub/GitLab/Gitea
// implementations (a test can override one).
type Config struct {
	Repository Repository
	Logger     *slog.Logger
	Factories  map[string]Factory
}

// Service coordinates provider connections and repository listing. It is safe
// for concurrent use.
type Service struct {
	repo      Repository
	logger    *slog.Logger
	factories map[string]Factory
}

// NewService builds a Service from cfg.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	factories := defaultFactories()
	for name, factory := range cfg.Factories {
		factories[name] = factory
	}

	return &Service{
		repo:      cfg.Repository,
		logger:    logger,
		factories: factories,
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

	repos, err := source.ListRepos(ctx, provider.token())
	if err != nil {
		if cached, cacheErr := s.repo.ListCachedRepos(ctx, providerID); cacheErr == nil && len(cached) > 0 {
			s.logger.Warn("providers: live repo list failed; serving cache",
				"provider_id", providerID.String(),
				"error", err)
			return cached, nil
		}
		return nil, err
	}

	if err := s.repo.ReplaceRepos(ctx, providerID, repos); err != nil {
		s.logger.Warn("providers: cache repo list failed",
			"provider_id", providerID.String(),
			"error", err)
	}
	return repos, nil
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
	return source.CreateWebhook(ctx, connection.token(), target.Repo, hook)
}

// DeleteWebhook removes the hook identified by hookID from target.Repo. A hook
// the provider has already forgotten is reported as deleted, so callers can
// run it twice without special-casing.
func (s *Service) DeleteWebhook(ctx context.Context, target HookTarget, hookID string) error {
	source, connection, err := s.sourceForTarget(ctx, target)
	if err != nil {
		return err
	}
	return source.DeleteWebhook(ctx, connection.token(), target.Repo, hookID)
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
func (s *Service) sourceProvider(p Provider) (SourceProvider, error) {
	factory, ok := s.factories[p.Name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, p.Name)
	}
	if p.Name == NameGitea && strings.TrimSpace(p.BaseURL) == "" {
		return nil, fmt.Errorf("%w: gitea requires a base_url", ErrValidation)
	}
	return factory(p)
}

// defaultFactories returns the built-in provider implementations.
func defaultFactories() map[string]Factory {
	return map[string]Factory{
		NameGitHub: func(p Provider) (SourceProvider, error) { return newGitHubSource(p), nil },
		NameGitLab: func(p Provider) (SourceProvider, error) { return newGitLabSource(p), nil },
		NameGitea:  func(p Provider) (SourceProvider, error) { return newGiteaSource(p), nil },
	}
}
