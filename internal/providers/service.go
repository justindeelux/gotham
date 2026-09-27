package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
