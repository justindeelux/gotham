package auth

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// OAuthProviderSettings is one provider as configured in the instance settings.
// ClientSecret is plaintext in memory; the store keeps it sealed.
type OAuthProviderSettings struct {
	Name         string
	Enabled      bool
	ClientID     string
	ClientSecret string
}

// OAuthSettingsRepository persists the instance's OAuth provider settings.
type OAuthSettingsRepository interface {
	List(ctx context.Context) ([]OAuthProviderSettings, error)
	Save(ctx context.Context, in OAuthProviderSettings) error
}

// OAuthProviderInfo is a provider as the settings API shows it. The secret is
// never returned, only whether one is stored.
type OAuthProviderInfo struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	ClientID  string `json:"client_id"`
	HasSecret bool   `json:"has_secret"`
}

// OAuthProviderUpdate is a provider save. An empty ClientSecret keeps the
// stored one, so a provider can be toggled without re-entering it.
type OAuthProviderUpdate struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Enabled      bool   `json:"enabled"`
}

type oauthSettingsStore struct {
	store     *store.Store
	secretKey string
}

func NewOAuthSettingsRepository(st *store.Store, secretKey string) OAuthSettingsRepository {
	return &oauthSettingsStore{store: st, secretKey: secretKey}
}

func (r *oauthSettingsStore) List(ctx context.Context) ([]OAuthProviderSettings, error) {
	rows, err := r.store.ListOAuthProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: list oauth providers: %w", err)
	}
	out := make([]OAuthProviderSettings, 0, len(rows))
	for _, row := range rows {
		secret, err := providers.OpenSecret(r.secretKey, row.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("auth: decrypt oauth secret for %s: %w", row.Name, err)
		}
		out = append(out, OAuthProviderSettings{
			Name:         row.Name,
			Enabled:      row.Enabled,
			ClientID:     row.ClientID,
			ClientSecret: secret,
		})
	}
	return out, nil
}

func (r *oauthSettingsStore) Save(ctx context.Context, in OAuthProviderSettings) error {
	sealed, err := providers.SealSecret(r.secretKey, in.ClientSecret)
	if err != nil {
		return fmt.Errorf("auth: seal oauth secret: %w", err)
	}
	_, err = r.store.UpsertOAuthProvider(ctx, sqlc.UpsertOAuthProviderParams{
		Name:         in.Name,
		Enabled:      in.Enabled,
		ClientID:     in.ClientID,
		ClientSecret: sealed,
	})
	if err != nil {
		return fmt.Errorf("auth: save oauth provider: %w", err)
	}
	return nil
}

// Providers lists every provider Gotham supports, with the stored values when
// the operator has configured it.
func (s *OAuthService) Providers(ctx context.Context) ([]OAuthProviderInfo, error) {
	stored := map[string]OAuthProviderSettings{}
	if s.settings != nil {
		rows, err := s.settings.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			stored[row.Name] = row
		}
	}

	names := make([]string, 0, len(oauthProviderFactories))
	for name := range oauthProviderFactories {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]OAuthProviderInfo, 0, len(names))
	for _, name := range names {
		row := stored[name]
		out = append(out, OAuthProviderInfo{
			Name:      name,
			Enabled:   row.Enabled,
			ClientID:  row.ClientID,
			HasSecret: row.ClientSecret != "",
		})
	}
	return out, nil
}

// UpdateProvider stores one provider and rebuilds the live provider set.
func (s *OAuthService) UpdateProvider(ctx context.Context, name string, in OAuthProviderUpdate) (OAuthProviderInfo, error) {
	if _, ok := oauthProviderFactories[name]; !ok {
		return OAuthProviderInfo{}, ErrProviderUnknown
	}
	if s.settings == nil {
		return OAuthProviderInfo{}, ErrProviderUnknown
	}

	secret := in.ClientSecret
	if secret == "" {
		rows, err := s.settings.List(ctx)
		if err != nil {
			return OAuthProviderInfo{}, err
		}
		for _, row := range rows {
			if row.Name == name {
				secret = row.ClientSecret
			}
		}
	}
	clientID := strings.TrimSpace(in.ClientID)
	if in.Enabled && (clientID == "" || secret == "") {
		return OAuthProviderInfo{}, ErrProviderIncomplete
	}

	if err := s.settings.Save(ctx, OAuthProviderSettings{
		Name:         name,
		Enabled:      in.Enabled,
		ClientID:     clientID,
		ClientSecret: secret,
	}); err != nil {
		return OAuthProviderInfo{}, err
	}
	if err := s.Reload(ctx); err != nil {
		return OAuthProviderInfo{}, err
	}

	return OAuthProviderInfo{
		Name:      name,
		Enabled:   in.Enabled,
		ClientID:  clientID,
		HasSecret: secret != "",
	}, nil
}
