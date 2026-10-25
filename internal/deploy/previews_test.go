package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func seedBaseForPreview(t *testing.T, app Application) *fakeRepository {
	t.Helper()
	repo := &fakeRepository{app: app}
	primary := app.PrimaryDomain
	if primary == nil {
		primary = &ApplicationDomain{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}
	}
	if _, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
		ApplicationID: app.ID,
		Domain:        primary.Domain,
		IsPrimary:     true,
		Port:          primary.Port,
		Protocol:      primary.Protocol,
	}); err != nil {
		t.Fatalf("seed primary domain: %v", err)
	}
	if _, err := repo.CreateDeployKey(context.Background(), DeployKey{
		ApplicationID: app.ID,
		Provider:      "github",
		Repo:          app.Repo,
		PublicKey:     "ssh-ed25519 AAAA",
		Fingerprint:   "SHA256:test",
	}, "PRIVATE KEY PEM"); err != nil {
		t.Fatalf("seed deploy key: %v", err)
	}
	return repo
}

func TestCreatePreviewApplicationClonesConfig(t *testing.T) {
	userID := uuid.New()
	base := testApplication(userID)
	base.PrimaryDomain = &ApplicationDomain{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}
	repo := seedBaseForPreview(t, base)
	repo.envVars = []EnvVar{{ApplicationID: base.ID, Key: "FOO", Value: "bar"}}
	repo.secrets = []Secret{{ID: uuid.New(), ApplicationID: base.ID, Key: "TOKEN", Ciphertext: "sealed"}}
	repo.storages = []Storage{{ApplicationID: base.ID, Name: "data", HostPath: "/srv/data", ContainerPath: "/data"}}

	svc := newTestService(t, repo)
	created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name:       "demo app-pr-7",
		Branch:     "feat/x",
		BaseDomain: "PR-7-Demo.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}

	if created.UserID != base.UserID ||
		created.ServerID != base.ServerID || created.Provider != base.Provider ||
		created.Repo != base.Repo || created.CloneURL != base.CloneURL ||
		created.BuildPack != base.BuildPack {
		t.Errorf("clone config = %+v, want everything copied from %+v", created, base)
	}
	if created.Name != "demo app-pr-7" || created.Branch != "feat/x" ||
		created.PrimaryDomain == nil || created.PrimaryDomain.Domain != "pr-7-demo.app.example.com" {
		t.Errorf("clone identity = %+v", created)
	}
	if created.PrimaryDomain.Port != base.PrimaryDomain.Port || created.PrimaryDomain.Protocol != base.PrimaryDomain.Protocol {
		t.Errorf("preview target = %#v, want the base routing shape", created.PrimaryDomain)
	}
	if !created.IsPreview {
		t.Error("the clone is not marked is_preview")
	}
	if base.IsPreview {
		t.Error("the base application was marked is_preview")
	}

	envVars, _ := repo.ListEnvVars(context.Background(), created.ID)
	secrets, _ := repo.ListSecrets(context.Background(), created.ID)
	storages, _ := repo.ListStorages(context.Background(), created.ID)
	if len(envVars) != 1 || envVars[0].Key != "FOO" {
		t.Errorf("cloned env vars = %v, want the base's plain variables", envVars)
	}
	if len(secrets) != 0 {
		t.Errorf("cloned secrets = %v, want none", secrets)
	}
	if len(storages) != 0 {
		t.Errorf("cloned storages = %v, want none", storages)
	}

	key, err := repo.GetDeployKey(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("the preview must reuse the base deploy key: %v", err)
	}
	if key.Fingerprint != "SHA256:test" {
		t.Errorf("cloned deploy key = %+v", key)
	}
	if pem, _ := repo.DeployKeyPrivatePEM(context.Background(), created.ID); pem != "PRIVATE KEY PEM" {
		t.Errorf("cloned private key = %q", pem)
	}
	if !repo.hasDeployKey(base.ID) {
		t.Error("the base deploy key was removed by the clone")
	}

	second, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-8", Branch: "feat/y", BaseDomain: "pr-8-demo.app.example.com",
	})
	if err != nil {
		t.Fatalf("second CreatePreviewApplication: %v", err)
	}
	if second.ID == created.ID {
		t.Error("two previews share one application row")
	}
}

func TestCreatePreviewApplicationClonesWildcardCertificate(t *testing.T) {
	base := testApplication(uuid.New())
	base.PrimaryDomain = &ApplicationDomain{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}
	repo := seedBaseForPreview(t, base)
	providerID := uuid.New()
	repo.certificates = []CertificateIntent{{
		ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: true,
		Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
	}}
	repo.dnsProviders = map[uuid.UUID]DNSProviderInfo{
		providerID: {Zones: []string{"example.com"}, Enabled: true},
	}

	svc := newTestService(t, repo)
	created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7-demo.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}

	intent, ok := repo.certificateIntent(created.ID)
	if !ok {
		t.Fatal("the sibling got no certificate intent")
	}
	if intent.Domain != "pr-7-demo.app.example.com" || !intent.Enabled || !intent.Wildcard ||
		intent.Challenge != "dns-01" || intent.DNSProviderID != providerID {
		t.Fatalf("cloned intent = %+v, want the base's wildcard configuration for the preview host", intent)
	}
	if baseIntent, _ := repo.certificateIntent(base.ID); baseIntent.Domain != "app.example.com" {
		t.Fatalf("base intent = %+v, want it untouched", baseIntent)
	}

	if err := svc.DeleteSystemApplication(context.Background(), created.ID); err != nil {
		t.Fatalf("DeleteSystemApplication: %v", err)
	}
	if _, ok := repo.certificateIntent(created.ID); ok {
		t.Fatal("the sibling intent survived the teardown")
	}
}

func TestCreatePreviewApplicationSkipsUncoveredCertificates(t *testing.T) {
	base := testApplication(uuid.New())
	base.PrimaryDomain = &ApplicationDomain{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}
	providerID := uuid.New()
	enabled := map[uuid.UUID]DNSProviderInfo{
		providerID: {Zones: []string{"example.com"}, Enabled: true},
	}

	cases := []struct {
		name      string
		intent    *CertificateIntent
		providers map[uuid.UUID]DNSProviderInfo
	}{
		{name: "no intent"},
		{name: "plain http-01 intent", intent: &CertificateIntent{
			ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: true, Challenge: "http-01",
		}},
		{name: "disabled wildcard intent", intent: &CertificateIntent{
			ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: false,
			Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
		}, providers: enabled},
		{name: "stale recorded domain", intent: &CertificateIntent{
			ApplicationID: base.ID, Domain: "old.example.com", Enabled: true,
			Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
		}, providers: enabled},
		{name: "disabled provider", intent: &CertificateIntent{
			ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: true,
			Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
		}, providers: map[uuid.UUID]DNSProviderInfo{
			providerID: {Zones: []string{"example.com"}, Enabled: false},
		}},
		{name: "provider zones do not cover the preview host", intent: &CertificateIntent{
			ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: true,
			Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
		}, providers: map[uuid.UUID]DNSProviderInfo{
			providerID: {Zones: []string{"other.com"}, Enabled: true},
		}},
		{name: "missing provider", intent: &CertificateIntent{
			ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: true,
			Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := seedBaseForPreview(t, base)
			if tc.intent != nil {
				repo.certificates = []CertificateIntent{*tc.intent}
			}
			repo.dnsProviders = tc.providers
			svc := newTestService(t, repo)
			created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
				Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7-demo.app.example.com",
			})
			if err != nil {
				t.Fatalf("CreatePreviewApplication: %v", err)
			}
			if intent, ok := repo.certificateIntent(created.ID); ok {
				t.Fatalf("cloned intent = %+v, want none", intent)
			}
		})
	}
}

func TestCreatePreviewApplicationCertificateFailureIsBestEffort(t *testing.T) {
	base := testApplication(uuid.New())
	base.PrimaryDomain = &ApplicationDomain{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}
	repo := seedBaseForPreview(t, base)
	providerID := uuid.New()
	repo.certificates = []CertificateIntent{{
		ApplicationID: base.ID, Domain: base.PrimaryDomain.Domain, Enabled: true,
		Challenge: "dns-01", DNSProviderID: providerID, Wildcard: true,
	}}
	repo.dnsProviders = map[uuid.UUID]DNSProviderInfo{
		providerID: {Zones: []string{"example.com"}, Enabled: true},
	}
	repo.certErr = errors.New("database down")

	svc := newTestService(t, repo)
	created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7-demo.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("the clone failed because the best-effort certificate write failed")
	}
}

func TestCreatePreviewApplicationRejectsUnusableInput(t *testing.T) {
	base := testApplication(uuid.New())
	base.PrimaryDomain = &ApplicationDomain{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}

	cases := []struct {
		name  string
		input PreviewApplicationInput
		want  error
	}{
		{"missing name", PreviewApplicationInput{Branch: "feat/x", BaseDomain: "pr-1.app.example.com"}, ErrValidation},
		{"missing branch falls back to the base branch", PreviewApplicationInput{Name: "p-pr-1", BaseDomain: "pr-1.app.example.com"}, nil},
		{"invalid domain", PreviewApplicationInput{Name: "p-pr-2", Branch: "feat/x", BaseDomain: "not a domain"}, ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := seedBaseForPreview(t, base)
			svc := newTestService(t, repo)
			created, err := svc.CreatePreviewApplication(context.Background(), base.ID, tc.input)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CreatePreviewApplication: %v", err)
				}
				if created.Branch != base.Branch {
					t.Errorf("branch = %q, want the base branch %q", created.Branch, base.Branch)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(repo.apps) != 0 {
				t.Errorf("stored %d applications, want none", len(repo.apps))
			}
		})
	}

	t.Setenv(FeatureEnv, "false")
	repo := &fakeRepository{app: base}
	svc := newTestService(t, repo)
	if _, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "p-pr-1", Branch: "feat/x", BaseDomain: "pr-1.app.example.com",
	}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled CreatePreviewApplication = %v, want ErrDisabled", err)
	}
}

func TestDeleteSystemApplicationRemovesLocalKeyOnly(t *testing.T) {
	base := testApplication(uuid.New())
	repo := seedBaseForPreview(t, base)
	registrar := &fakeRegistrar{}
	svc := newKeyService(t, repo, registrar)

	preview, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if !repo.hasDeployKey(preview.ID) {
		t.Fatal("the preview has no local deploy key to clean up")
	}

	if err := svc.DeleteSystemApplication(context.Background(), preview.ID); err != nil {
		t.Fatalf("DeleteSystemApplication: %v", err)
	}
	if _, err := repo.GetApplication(context.Background(), preview.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("preview still exists: %v", err)
	}
	if repo.hasDeployKey(preview.ID) {
		t.Error("the preview's local deploy key survived the teardown")
	}
	if !repo.hasDeployKey(base.ID) {
		t.Error("the base deploy key was removed by the preview teardown")
	}
	registrar.mu.Lock()
	removed := len(registrar.removed)
	registrar.mu.Unlock()
	if removed != 0 {
		t.Errorf("remote removals = %d, want 0 (the key is shared with the base)", removed)
	}
	if err := svc.DeleteSystemApplication(context.Background(), preview.ID); err != nil {
		t.Fatalf("second DeleteSystemApplication: %v", err)
	}

	t.Setenv(FeatureEnv, "false")
	if err := svc.DeleteSystemApplication(context.Background(), base.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled DeleteSystemApplication = %v, want ErrDisabled", err)
	}
}

func TestDeleteSystemApplicationRefusesNonPreview(t *testing.T) {
	base := testApplication(uuid.New())
	repo := &fakeRepository{app: base}
	svc := newTestService(t, repo)

	err := svc.DeleteSystemApplication(context.Background(), base.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("DeleteSystemApplication(base) = %v, want ErrValidation", err)
	}
	if _, err := repo.GetApplication(context.Background(), base.ID); err != nil {
		t.Fatalf("the base application was deleted: %v", err)
	}
}

func TestDeleteApplicationOfAPreviewKeepsTheRemoteKey(t *testing.T) {
	base := testApplication(uuid.New())
	repo := seedBaseForPreview(t, base)
	registrar := &fakeRegistrar{}
	svc := newKeyService(t, repo, registrar)

	preview, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if err := svc.DeleteApplication(context.Background(), preview.UserID, preview.ID); err != nil {
		t.Fatalf("DeleteApplication(preview): %v", err)
	}
	registrar.mu.Lock()
	removed := len(registrar.removed)
	registrar.mu.Unlock()
	if removed != 0 {
		t.Errorf("remote removals = %d, want 0 (the preview shares the base's key)", removed)
	}
	if repo.hasDeployKey(preview.ID) {
		t.Error("the preview's local deploy key survived the user delete")
	}
	if !repo.hasDeployKey(base.ID) {
		t.Error("the base deploy key was removed")
	}
	if _, err := repo.GetApplication(context.Background(), base.ID); err != nil {
		t.Fatalf("base application after the preview delete: %v", err)
	}
}

func TestDeleteApplicationTearsDownPreviewsFirst(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	cleanupErr := error(nil)
	cleaned := 0
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		PreviewCleanup: func(_ context.Context, appID uuid.UUID) error {
			if appID != app.ID {
				t.Errorf("cleanup app = %s, want %s", appID, app.ID)
			}
			cleaned++
			return cleanupErr
		},
	})
	t.Cleanup(func() { _ = svc.Close() })

	cleanupErr = errors.New("preview teardown failed")
	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err == nil {
		t.Fatal("DeleteApplication with a failing preview cleanup: no error, want one")
	}
	if _, err := repo.GetApplication(context.Background(), app.ID); err != nil {
		t.Fatalf("base application was deleted despite the failed cleanup: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleaned)
	}

	cleanupErr = nil
	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("DeleteApplication(retry): %v", err)
	}
	if cleaned != 2 {
		t.Fatalf("cleanup calls = %d, want 2", cleaned)
	}
	if _, err := repo.GetApplication(context.Background(), app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("application still exists: %v", err)
	}
}
