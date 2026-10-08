package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/githubapp"
	"github.com/justindeelux/gotham/internal/providers"
)

// composedFakeAPI implements githubapp.GitHubAPI with real shapes and no
// network: the manifest conversion returns a plain-string webhook secret,
// the installation object carries account and app id.
type composedFakeAPI struct {
	pem           string
	webhookSecret string
	repos         []githubapp.Repo
}

func (f *composedFakeAPI) ExchangeManifest(_ context.Context, code string) (githubapp.ManifestConversion, error) {
	if code == "" {
		return githubapp.ManifestConversion{}, errComposed("empty code")
	}
	return githubapp.ManifestConversion{
		ID:            123,
		Slug:          "gotham-test",
		Name:          "gotham-test",
		WebhookSecret: f.webhookSecret,
		PEM:           f.pem,
	}, nil
}

func (f *composedFakeAPI) GetInstallation(_ context.Context, installationID int64, _ string) (githubapp.InstallationInfo, error) {
	if installationID != 999 {
		return githubapp.InstallationInfo{}, errComposed("unknown installation")
	}
	return githubapp.InstallationInfo{ID: 999, Account: "acme", AppID: 123}, nil
}

func (f *composedFakeAPI) CreateInstallationToken(_ context.Context, _ int64, _ string) (githubapp.InstallationToken, error) {
	return githubapp.InstallationToken{Token: "inst-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *composedFakeAPI) ListInstallationRepos(_ context.Context, _ string) ([]githubapp.Repo, error) {
	return f.repos, nil
}

func (f *composedFakeAPI) ListBranches(_ context.Context, _ string, _ string) ([]githubapp.Branch, error) {
	return nil, nil
}

type composedError string

func (e composedError) Error() string { return "webhooks: composed fake: " + string(e) }

func errComposed(msg string) error { return composedError(msg) }

// composedRepo is an in-memory githubapp.Repository for the composed test.
type composedRepo struct {
	mu      sync.Mutex
	apps    map[uuid.UUID]githubapp.SealedApp
	insts   map[uuid.UUID][]githubapp.Installation
	targets map[string][]githubapp.AppPushTarget
}

func newComposedRepo() *composedRepo {
	return &composedRepo{
		apps:    make(map[uuid.UUID]githubapp.SealedApp),
		insts:   make(map[uuid.UUID][]githubapp.Installation),
		targets: make(map[string][]githubapp.AppPushTarget),
	}
}

func (m *composedRepo) CreateApp(_ context.Context, app githubapp.GitHubApp, secret, key string) (githubapp.GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app.ID = uuid.New()
	m.apps[app.ID] = githubapp.SealedApp{GitHubApp: app, WebhookSecret: secret, PrivateKey: key}
	return app, nil
}

func (m *composedRepo) withInstalls(id uuid.UUID) (githubapp.GitHubApp, githubapp.SealedApp, bool) {
	sealed, ok := m.apps[id]
	if !ok {
		return githubapp.GitHubApp{}, githubapp.SealedApp{}, false
	}
	app := sealed.GitHubApp
	app.Installations = append([]githubapp.Installation(nil), m.insts[id]...)
	return app, sealed, true
}

func (m *composedRepo) GetApp(_ context.Context, id, _ uuid.UUID) (githubapp.GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, _, ok := m.withInstalls(id)
	if !ok {
		return githubapp.GitHubApp{}, errComposed("no app")
	}
	return app, nil
}

func (m *composedRepo) GetSealed(_ context.Context, id, _ uuid.UUID) (githubapp.SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, sealed, ok := m.withInstalls(id)
	if !ok {
		return githubapp.SealedApp{}, errComposed("no app")
	}
	return sealed, nil
}

func (m *composedRepo) GetSealedByID(_ context.Context, id uuid.UUID) (githubapp.SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, sealed, ok := m.withInstalls(id)
	if !ok {
		return githubapp.SealedApp{}, errComposed("no app")
	}
	return sealed, nil
}

func (m *composedRepo) ListApps(_ context.Context, _ uuid.UUID) ([]githubapp.GitHubApp, error) {
	return nil, nil
}

func (m *composedRepo) DeleteApp(_ context.Context, _, _ uuid.UUID) error { return nil }

func (m *composedRepo) UpsertInstallation(_ context.Context, appID uuid.UUID, installationID int64, account string) (githubapp.Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inst := range m.insts[appID] {
		if inst.InstallationID == installationID {
			return inst, nil
		}
	}
	inst := githubapp.Installation{ID: uuid.New(), InstallationID: installationID, Account: account}
	m.insts[appID] = append(m.insts[appID], inst)
	return inst, nil
}

func (m *composedRepo) ListInstallations(_ context.Context, appID uuid.UUID) ([]githubapp.Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]githubapp.Installation(nil), m.insts[appID]...), nil
}

func (m *composedRepo) DeleteInstallation(_ context.Context, _ int64, _ uuid.UUID) error {
	return nil
}

func (m *composedRepo) AppsByInstallationID(_ context.Context, installationID int64) ([]githubapp.SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var apps []githubapp.SealedApp
	for appID, insts := range m.insts {
		for _, inst := range insts {
			if inst.InstallationID == installationID {
				apps = append(apps, m.apps[appID])
			}
		}
	}
	return apps, nil
}

func (m *composedRepo) ReplaceRepoCache(_ context.Context, _ uuid.UUID, _ int64, _ []githubapp.Repo) error {
	return nil
}

func (m *composedRepo) ListRepoCache(_ context.Context, _ uuid.UUID, _ int64) ([]githubapp.Repo, error) {
	return nil, nil
}

func (m *composedRepo) CountApplicationsForApp(_ context.Context, _, _ uuid.UUID) (int64, error) {
	return 0, nil
}

func (m *composedRepo) PushTargets(_ context.Context, _, _ uuid.UUID, repo string) ([]githubapp.AppPushTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]githubapp.AppPushTarget(nil), m.targets[strings.ToLower(repo)]...), nil
}

// realAppPush adapts the REAL githubapp.Service to AppPushHandler: only the
// transport (fake API + memory repo) is fake, verification and targeting run
// the production code.
type realAppPush struct {
	svc *githubapp.Service
}

func (a realAppPush) VerifyPush(header http.Header, body []byte) (uuid.UUID, bool) {
	return a.svc.VerifyPush(header, body)
}

func (a realAppPush) PushTargets(ctx context.Context, appID uuid.UUID, repo string) ([]AppPushTarget, error) {
	targets, err := a.svc.PushTargetsForWebhook(ctx, appID, repo)
	if err != nil {
		return nil, err
	}
	out := make([]AppPushTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, AppPushTarget{ApplicationID: target.ApplicationID, Branch: target.Branch})
	}
	return out, nil
}

func composedTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func signComposed(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestComposedAppPushToDeploy runs the real chain with no fakes for the
// service: manifest connect -> install -> application row -> real-shaped
// signed push through Receive -> deployment queued.
func TestComposedAppPushToDeploy(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	api := &composedFakeAPI{
		pem:           composedTestKey(t),
		webhookSecret: "app-secret",
		repos: []githubapp.Repo{
			{ExternalID: "1", Name: "web", FullName: "acme/web", DefaultBranch: "main", CloneURL: "https://github.com/acme/web.git"},
		},
	}
	appRepo := newComposedRepo()
	appSvc := githubapp.NewService(githubapp.Config{
		Repository:         appRepo,
		NewAPI:             func(_ string) githubapp.GitHubAPI { return api },
		Secret:             "test-secret-key",
		AllowUnsafeBaseURL: true,
	})

	// Connect through the manifest flow against the fake.
	manifest, err := appSvc.StartManifest(ctx, userID, "", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	app, err := appSvc.Callback(ctx, userID, "manifest-code", manifest.State)
	if err != nil {
		t.Fatal(err)
	}
	_, installState, err := appSvc.InstallURL(ctx, userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appSvc.RecordInstallation(ctx, userID, app.ID, 999, installState); err != nil {
		t.Fatal(err)
	}

	// The application row a user would create from the wizard.
	hookRepo := newFakeRepository()
	hookRepo.app.Repo = "acme/web"
	hookRepo.app.Branch = "main"
	hookRepo.app.Provider = providers.NameGitHub
	appRepo.mu.Lock()
	appRepo.targets["acme/web"] = []githubapp.AppPushTarget{
		{ApplicationID: hookRepo.app.ID, Branch: "main"},
	}
	appRepo.mu.Unlock()

	deployer := &fakeDeployer{}
	svc := newTestServiceWith(Config{
		Repository:  hookRepo,
		Installer:   &fakeInstaller{},
		Deployer:    deployer,
		Provisioner: &fakeDeployer{},
		AppEvents:   nil,
		AppPush:     realAppPush{svc: appSvc},
	})

	// A real-shaped push delivery: event + delivery headers, the app-id hook
	// target header, installation payload, app-secret signature.
	body := `{"ref":"refs/heads/main","after":"abc123def456",` +
		`"repository":{"id":1,"full_name":"acme/web"},` +
		`"installation":{"id":999,"account":{"login":"acme"}}`
	pushBody := body + `,"sender":{"login":"acme"}}`
	req := deliveryRequest(providers.NameGitHub, pushBody, map[string]string{
		headerGitHubEvent:                        "push",
		headerGitHubDelivery:                     "delivery-composed-1",
		headerGitHubSignature:                    signComposed("app-secret", []byte(pushBody)),
		"X-GitHub-Hook-Installation-Target-ID":   "123",
		"X-GitHub-Hook-Installation-Target-Type": "integration",
	}, "203.0.113.7:1234")

	delivery, err := svc.Receive(ctx, providers.NameGitHub, req)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("status = %q, want queued", delivery.Status)
	}
	deployer.mu.Lock()
	defer deployer.mu.Unlock()
	if len(deployer.deployed) != 1 || deployer.deployed[0] != hookRepo.app.ID {
		t.Fatalf("deployed = %v, want [%v]", deployer.deployed, hookRepo.app.ID)
	}
}
