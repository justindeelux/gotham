package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeOAuthSettings struct {
	mu   sync.Mutex
	rows []OAuthProviderSettings
}

func (f *fakeOAuthSettings) List(context.Context) ([]OAuthProviderSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]OAuthProviderSettings(nil), f.rows...), nil
}

func (f *fakeOAuthSettings) Save(_ context.Context, in OAuthProviderSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, row := range f.rows {
		if row.Name == in.Name {
			f.rows[i] = in
			return nil
		}
	}
	f.rows = append(f.rows, in)
	return nil
}

func TestOAuthSettingsRoundTrip(t *testing.T) {
	_, st := scratchService(t)
	ctx := context.Background()
	repo := NewOAuthSettingsRepository(st, "instance-secret-key")

	const plain = "github-client-secret"
	if err := repo.Save(ctx, OAuthProviderSettings{
		Name: "github", Enabled: true, ClientID: "gh-client", ClientSecret: plain,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stored, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("List returned %d rows, want 1", len(stored))
	}
	if stored[0].ClientSecret != plain {
		t.Errorf("List secret = %q, want the decrypted secret", stored[0].ClientSecret)
	}
	if stored[0].ClientID != "gh-client" || !stored[0].Enabled {
		t.Errorf("List row = %+v, want the saved values", stored[0])
	}

	rows, err := st.ListOAuthProviders(ctx)
	if err != nil {
		t.Fatalf("ListOAuthProviders: %v", err)
	}
	if len(rows) != 1 || rows[0].ClientSecret == plain || rows[0].ClientSecret == "" {
		t.Fatalf("stored rows = %+v, want the secret sealed at rest", rows)
	}
}

func TestOAuthReloadEnablesAndDisablesProvider(t *testing.T) {
	ctx := context.Background()
	settings := &fakeOAuthSettings{}
	oauth := NewOAuthService(nil, testLogger(), settings)
	t.Cleanup(oauth.Close)

	if err := oauth.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, _, err := oauth.Begin(ctx, "github", "https://cp.example"); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("Begin before saving credentials = %v, want ErrProviderDisabled", err)
	}

	info, err := oauth.UpdateProvider(ctx, "github", OAuthProviderUpdate{
		ClientID: "gh-client", ClientSecret: "gh-secret", Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	if !info.Enabled || !info.HasSecret {
		t.Errorf("UpdateProvider info = %+v, want enabled with a secret", info)
	}
	if _, _, err := oauth.Begin(ctx, "github", "https://cp.example"); err != nil {
		t.Fatalf("Begin after saving credentials = %v, want the provider live", err)
	}

	if _, err := oauth.UpdateProvider(ctx, "github", OAuthProviderUpdate{
		ClientID: "gh-client", Enabled: false,
	}); err != nil {
		t.Fatalf("UpdateProvider(disable): %v", err)
	}
	if _, _, err := oauth.Begin(ctx, "github", "https://cp.example"); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("Begin after disabling = %v, want ErrProviderDisabled", err)
	}

	stored, err := settings.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(stored) != 1 || stored[0].ClientSecret != "gh-secret" {
		t.Errorf("a disable without a new secret dropped the stored one: %+v", stored)
	}
}

func TestOAuthUpdateProviderValidation(t *testing.T) {
	ctx := context.Background()
	oauth := NewOAuthService(nil, testLogger(), &fakeOAuthSettings{})
	t.Cleanup(oauth.Close)

	if _, err := oauth.UpdateProvider(ctx, "github", OAuthProviderUpdate{Enabled: true}); !errors.Is(err, ErrProviderIncomplete) {
		t.Errorf("enable without credentials = %v, want ErrProviderIncomplete", err)
	}
	if _, err := oauth.UpdateProvider(ctx, "gitlab", OAuthProviderUpdate{Enabled: false}); !errors.Is(err, ErrProviderUnknown) {
		t.Errorf("unknown provider = %v, want ErrProviderUnknown", err)
	}

	infos, err := oauth.Providers(ctx)
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}
	if len(infos) != 2 || infos[0].Name != "github" || infos[0].Enabled || infos[0].HasSecret ||
		infos[1].Name != "google" || infos[1].Enabled || infos[1].HasSecret {
		t.Errorf("Providers = %+v, want the unconfigured GitHub and Google providers", infos)
	}
}
