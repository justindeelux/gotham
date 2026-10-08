package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeAPI implements GitHubAPI without network access, recording how often
// each method runs so tests can prove token caching.
type fakeAPI struct {
	mu            sync.Mutex
	pem           string
	tokenCalls    int
	token         string
	expiresAt     time.Time
	repos         []Repo
	branches      map[string][]Branch
	conversionErr error
}

func (f *fakeAPI) ExchangeManifest(_ context.Context, code string) (ManifestConversion, error) {
	if f.conversionErr != nil {
		return ManifestConversion{}, f.conversionErr
	}
	if code == "" {
		return ManifestConversion{}, fmt.Errorf("%w: empty code", ErrValidation)
	}
	return ManifestConversion{
		ID:            123,
		Slug:          "gotham-test",
		Name:          "gotham-test",
		ClientID:      "cid",
		WebhookSecret: "wh-secret",
		PEM:           f.pem,
	}, nil
}

func (f *fakeAPI) CreateInstallationToken(_ context.Context, _ int64, _ string) (InstallationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenCalls++
	return InstallationToken{Token: f.token, ExpiresAt: f.expiresAt}, nil
}

func (f *fakeAPI) ListInstallationRepos(_ context.Context, _ string) ([]Repo, error) {
	return f.repos, nil
}

func (f *fakeAPI) ListBranches(_ context.Context, _ string, repo string) ([]Branch, error) {
	return f.branches[repo], nil
}

func (f *fakeAPI) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

// memoryRepo implements Repository in memory for service tests.
type memoryRepo struct {
	mu        sync.Mutex
	apps      map[uuid.UUID]sealedApp
	insts     map[uuid.UUID][]Installation
	byInstall map[int64][]uuid.UUID
	cache     map[string][]Repo
	appsUsing int64
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		apps:      make(map[uuid.UUID]sealedApp),
		insts:     make(map[uuid.UUID][]Installation),
		byInstall: make(map[int64][]uuid.UUID),
		cache:     make(map[string][]Repo),
	}
}

func (m *memoryRepo) CreateApp(_ context.Context, app GitHubApp, webhookSecret, privateKey string) (GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app.ID = uuid.New()
	m.apps[app.ID] = sealedApp{GitHubApp: app, WebhookSecret: webhookSecret, PrivateKey: privateKey}
	return app, nil
}

func (m *memoryRepo) GetApp(_ context.Context, id, _ uuid.UUID) (GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sealed, ok := m.apps[id]
	if !ok {
		return GitHubApp{}, ErrNotFound
	}
	app := sealed.GitHubApp
	app.Installations = append([]Installation(nil), m.insts[id]...)
	return app, nil
}

func (m *memoryRepo) GetSealed(_ context.Context, id, _ uuid.UUID) (sealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sealed, ok := m.apps[id]
	if !ok {
		return sealedApp{}, ErrNotFound
	}
	return sealed, nil
}

func (m *memoryRepo) ListApps(_ context.Context, _ uuid.UUID) ([]GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var apps []GitHubApp
	for _, sealed := range m.apps {
		app := sealed.GitHubApp
		app.Installations = append([]Installation(nil), m.insts[app.ID]...)
		apps = append(apps, app)
	}
	return apps, nil
}

func (m *memoryRepo) DeleteApp(_ context.Context, id, _ uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.apps[id]; !ok {
		return ErrNotFound
	}
	delete(m.apps, id)
	delete(m.insts, id)
	return nil
}

func (m *memoryRepo) UpsertInstallation(_ context.Context, appID uuid.UUID, installationID int64, account string) (Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inst := range m.insts[appID] {
		if inst.InstallationID == installationID {
			return inst, nil
		}
	}
	inst := Installation{ID: uuid.New(), InstallationID: installationID, Account: account}
	m.insts[appID] = append(m.insts[appID], inst)
	m.byInstall[installationID] = append(m.byInstall[installationID], appID)
	return inst, nil
}

func (m *memoryRepo) ListInstallations(_ context.Context, appID uuid.UUID) ([]Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Installation(nil), m.insts[appID]...), nil
}

func (m *memoryRepo) DeleteInstallation(_ context.Context, installationID int64, appID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.insts[appID][:0]
	for _, inst := range m.insts[appID] {
		if inst.InstallationID != installationID {
			kept = append(kept, inst)
		}
	}
	m.insts[appID] = kept
	return nil
}

func (m *memoryRepo) AppsByInstallationID(_ context.Context, installationID int64) ([]sealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var apps []sealedApp
	for _, appID := range m.byInstall[installationID] {
		if sealed, ok := m.apps[appID]; ok {
			apps = append(apps, sealed)
		}
	}
	return apps, nil
}

func cacheKey(appID uuid.UUID, installationID int64) string {
	return appID.String() + "/" + fmt.Sprintf("%d", installationID)
}

func (m *memoryRepo) ReplaceRepoCache(_ context.Context, appID uuid.UUID, installationID int64, repos []Repo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[cacheKey(appID, installationID)] = append([]Repo(nil), repos...)
	return nil
}

func (m *memoryRepo) ListRepoCache(_ context.Context, appID uuid.UUID, installationID int64) ([]Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Repo(nil), m.cache[cacheKey(appID, installationID)]...), nil
}

func (m *memoryRepo) CountApplications(_ context.Context, _ uuid.UUID) (int64, error) {
	return m.appsUsing, nil
}

// testKeyPEM generates an RSA private key in PKCS#1 PEM form.
func testKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func testFixture() (*Service, *memoryRepo, *fakeAPI, uuid.UUID) {
	userID := uuid.New()
	repo := newMemoryRepo()
	api := &fakeAPI{
		token:     "inst-token",
		expiresAt: time.Now().Add(time.Hour),
		repos: []Repo{
			{ExternalID: "1", Name: "web", FullName: "acme/web", Private: true, DefaultBranch: "main", CloneURL: "https://github.com/acme/web.git"},
		},
		branches: map[string][]Branch{
			"acme/web": {{Name: "main", Commit: "abc"}, {Name: "dev", Commit: "def"}},
		},
	}
	svc := NewService(Config{
		Repository:         repo,
		NewAPI:             func(_ string) GitHubAPI { return api },
		Secret:             "test-secret-key",
		AllowUnsafeBaseURL: true,
	})
	return svc, repo, api, userID
}

// connect runs the manifest flow end to end against the fake.
func connect(t *testing.T, svc *Service, userID uuid.UUID) GitHubApp {
	t.Helper()
	manifest, err := svc.StartManifest(context.Background(), userID, "", "gotham-test", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.State == "" || manifest.ActionURL == "" {
		t.Fatal("manifest is missing action url or state")
	}
	perms, ok := manifest.Manifest["default_permissions"].(map[string]any)
	if !ok || perms["contents"] != "read" || perms["pull_requests"] != "write" || perms["metadata"] != "read" {
		t.Fatalf("manifest permissions = %v", manifest.Manifest["default_permissions"])
	}
	hooks, ok := manifest.Manifest["hook_attributes"].(map[string]any)
	if !ok || hooks["url"] != "https://gotham.example/api/v1/webhooks/github" {
		t.Fatalf("manifest hook url = %v", manifest.Manifest["hook_attributes"])
	}
	app, err := svc.Callback(context.Background(), userID, "manifest-code", manifest.State, "")
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestManifestConnectInstallList(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	if app.AppID != 123 || app.Slug != "gotham-test" {
		t.Fatalf("app = %+v", app)
	}

	installURL, state, err := svc.InstallURL(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if installURL == "" || state == "" {
		t.Fatal("install url or state is empty")
	}

	inst, err := svc.RecordInstallation(context.Background(), userID, app.ID, 999, state)
	if err != nil {
		t.Fatal(err)
	}
	if inst.InstallationID != 999 {
		t.Fatalf("installation = %+v", inst)
	}

	repos, err := svc.ListRepos(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/web" {
		t.Fatalf("repos = %+v", repos)
	}

	branches, err := svc.ListBranches(context.Background(), userID, app.ID, "acme/web")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0].Name != "main" {
		t.Fatalf("branches = %+v", branches)
	}
}

func TestManifestStateSingleUse(t *testing.T) {
	svc, _, _, userID := testFixture()

	manifest, err := svc.StartManifest(context.Background(), userID, "", "", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Callback(context.Background(), userID, "code", manifest.State, ""); err != nil {
		t.Fatal(err)
	}
	// Replay of the same state must fail: states are single-use.
	if _, err := svc.Callback(context.Background(), userID, "code", manifest.State, ""); err == nil {
		t.Fatal("replayed state was accepted")
	}
	// A forged state must fail too.
	if _, err := svc.Callback(context.Background(), userID, "code", "forged-state", ""); err == nil {
		t.Fatal("forged state was accepted")
	}
	// A state issued for another user must fail.
	other := uuid.New()
	manifest2, err := svc.StartManifest(context.Background(), other, "", "", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Callback(context.Background(), userID, "code", manifest2.State, ""); err == nil {
		t.Fatal("another user's state was accepted")
	}
}

func TestInstallStateBoundToApp(t *testing.T) {
	svc, _, _, userID := testFixture()

	appA := connect(t, svc, userID)
	appB := connect(t, svc, userID)

	_, state, err := svc.InstallURL(context.Background(), userID, appA.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The install state is bound to appA: recording it against appB fails.
	if _, err := svc.RecordInstallation(context.Background(), userID, appB.ID, 111, state); err == nil {
		t.Fatal("install state was accepted for another app")
	}
	// ...and replaying it against appA fails too (redeem consumed it).
	if _, err := svc.RecordInstallation(context.Background(), userID, appA.ID, 111, state); err == nil {
		t.Fatal("install state replay was accepted")
	}
}

func TestInstallationTokenCached(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	_, state, err := svc.InstallURL(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordInstallation(context.Background(), userID, app.ID, 999, state); err != nil {
		t.Fatal(err)
	}
	callsAfterInstall := api.calls()
	if _, err := svc.ListRepos(context.Background(), userID, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListRepos(context.Background(), userID, app.ID); err != nil {
		t.Fatal(err)
	}
	// The install-time mint is reused: no new token crosses the wire.
	if got := api.calls() - callsAfterInstall; got != 0 {
		t.Fatalf("installation token minted %d extra times, want 0 (cached)", got)
	}
	if total := api.calls(); total != 1 {
		t.Fatalf("installation token minted %d times total, want 1", total)
	}
}

func TestDisconnectReportsUsage(t *testing.T) {
	svc, repo, _, userID := testFixture()
	repo.appsUsing = 2

	app := connect(t, svc, userID)
	using, err := svc.Disconnect(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if using != 2 {
		t.Fatalf("applications using = %d, want 2", using)
	}
	if _, err := svc.ListApps(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	apps, _ := svc.ListApps(context.Background(), userID)
	if len(apps) != 0 {
		t.Fatalf("apps after disconnect = %+v", apps)
	}
}
