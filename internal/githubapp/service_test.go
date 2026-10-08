package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeAPI implements GitHubAPI without network access, recording how often
// each method runs so tests can prove token caching.
type fakeAPI struct {
	mu         sync.Mutex
	pem        string
	tokenCalls int
	tokenFor   []int64
	token      string
	expiresAt  time.Time
	repos      []Repo
	reposErr   error
	// reposErrFor fails one installation's listing (keyed by token), so a
	// test can break an unrelated connection while the linked one works.
	reposErrFor map[string]error
	// reposFor serves per-token listings when set (keyed by token).
	reposFor      map[string][]Repo
	branches      map[string][]Branch
	conversionErr error
	// installations is the fake's installation registry by id.
	installations map[int64]InstallationInfo
	// webhookSecret overrides the conversion secret per fake.
	webhookSecret string
}

func (f *fakeAPI) ExchangeManifest(_ context.Context, code string) (ManifestConversion, error) {
	if f.conversionErr != nil {
		return ManifestConversion{}, f.conversionErr
	}
	if code == "" {
		return ManifestConversion{}, fmt.Errorf("%w: empty code", ErrValidation)
	}
	secret := f.webhookSecret
	if secret == "" {
		secret = "wh-secret"
	}
	return ManifestConversion{
		ID:            123,
		Slug:          "gotham-test",
		Name:          "gotham-test",
		ClientID:      "cid",
		WebhookSecret: secret,
		PEM:           f.pem,
	}, nil
}

func (f *fakeAPI) CreateInstallationToken(_ context.Context, installationID int64, _ string) (InstallationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenCalls++
	f.tokenFor = append(f.tokenFor, installationID)
	// Per-installation tokens prove which installation a token came from.
	token := f.token
	if token == "" {
		token = fmt.Sprintf("tok-%d", installationID)
	}
	return InstallationToken{Token: token, ExpiresAt: f.expiresAt}, nil
}

// GetInstallation verifies the installation against the fake's registry:
// only installations the fake knows for this app verify, so a foreign id
// fails exactly like GitHub's 404.
func (f *fakeAPI) GetInstallation(_ context.Context, installationID int64, _ string) (InstallationInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.installations[installationID]
	if !ok {
		return InstallationInfo{}, fmt.Errorf("%w: unknown installation", ErrValidation)
	}
	return info, nil
}

func (f *fakeAPI) ListInstallationRepos(_ context.Context, token string) ([]Repo, bool, error) {
	if f.reposErr != nil {
		return nil, false, f.reposErr
	}
	if err, ok := f.reposErrFor[token]; ok {
		return nil, false, err
	}
	if f.reposFor != nil {
		if repos, ok := f.reposFor[token]; ok {
			return repos, false, nil
		}
		return nil, false, nil
	}
	return f.repos, false, nil
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
	apps      map[uuid.UUID]SealedApp
	insts     map[uuid.UUID][]Installation
	byInstall map[int64][]uuid.UUID
	cache     map[string][]Repo
	appsUsing []string
	// watched repos per app for PushTargets, keyed by lower(repo).
	watched map[string][]AppPushTarget
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		apps:      make(map[uuid.UUID]SealedApp),
		insts:     make(map[uuid.UUID][]Installation),
		byInstall: make(map[int64][]uuid.UUID),
		cache:     make(map[string][]Repo),
		watched:   make(map[string][]AppPushTarget),
	}
}

func (m *memoryRepo) CreateApp(_ context.Context, app GitHubApp, webhookSecret, privateKey string) (GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app.ID = uuid.New()
	m.apps[app.ID] = SealedApp{GitHubApp: app, WebhookSecret: webhookSecret, PrivateKey: privateKey}
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

func (m *memoryRepo) GetSealed(_ context.Context, id, _ uuid.UUID) (SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sealed, ok := m.apps[id]
	if !ok {
		return SealedApp{}, ErrNotFound
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

func (m *memoryRepo) GetSealedByID(_ context.Context, id uuid.UUID) (SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sealed, ok := m.apps[id]
	if !ok {
		return SealedApp{}, ErrNotFound
	}
	return sealed, nil
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

func (m *memoryRepo) AppsByInstallationID(_ context.Context, installationID int64) ([]SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var apps []SealedApp
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

func (m *memoryRepo) ListApplicationNamesForApp(_ context.Context, _, _ uuid.UUID) ([]string, error) {
	return append([]string(nil), m.appsUsing...), nil
}

// watch records that appID's owner watches repo (a github_app application).
func (m *memoryRepo) watch(appID uuid.UUID, repo, branch string) AppPushTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	target := AppPushTarget{ApplicationID: uuid.New(), Branch: branch}
	key := appID.String() + "\x00" + strings.ToLower(repo)
	m.watched[key] = append(m.watched[key], target)
	return target
}

func (m *memoryRepo) PushTargets(_ context.Context, appID, _ uuid.UUID, repo string) ([]AppPushTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AppPushTarget(nil), m.watched[appID.String()+"\x00"+strings.ToLower(repo)]...), nil
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
		installations: map[int64]InstallationInfo{
			999: {ID: 999, Account: "acme", AppID: 123},
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
	app, err := svc.Callback(context.Background(), userID, "manifest-code", manifest.State)
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

	repos, _, err := svc.ListRepos(context.Background(), userID, app.ID)
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
	if _, err := svc.Callback(context.Background(), userID, "code", manifest.State); err != nil {
		t.Fatal(err)
	}
	// Replay of the same state must fail: states are single-use.
	if _, err := svc.Callback(context.Background(), userID, "code", manifest.State); err == nil {
		t.Fatal("replayed state was accepted")
	}
	// A forged state must fail too.
	if _, err := svc.Callback(context.Background(), userID, "code", "forged-state"); err == nil {
		t.Fatal("forged state was accepted")
	}
	// A state issued for another user must fail.
	other := uuid.New()
	manifest2, err := svc.StartManifest(context.Background(), other, "", "", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Callback(context.Background(), userID, "code", manifest2.State); err == nil {
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
	if _, _, err := svc.ListRepos(context.Background(), userID, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ListRepos(context.Background(), userID, app.ID); err != nil {
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

// TestRecordInstallationVerifiesWithGitHub proves a recorded installation id
// is verified against the GitHub API: an id the app does not own is refused,
// so replaying another installation's integer id records nothing.
func TestRecordInstallationVerifiesWithGitHub(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	_, state, err := svc.InstallURL(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 4242 is unknown to the fake GitHub: recording it must fail.
	if _, err := svc.RecordInstallation(context.Background(), userID, app.ID, 4242, state); err == nil {
		t.Fatal("unverified installation was recorded")
	}
	// The consumed state cannot be reused for the real installation either:
	// states stay single-use even when verification fails.
	if _, err := svc.RecordInstallation(context.Background(), userID, app.ID, 999, state); err == nil {
		t.Fatal("consumed state was reused")
	}
}

// TestCrossUserInstallationTakeover reproduces the reviewer repro: user B
// records user A's installation id and sends a signed deleted delivery. The
// delivery must verify against B's app only and leave A's installation alone.
func TestCrossUserInstallationTakeover(t *testing.T) {
	ctx := context.Background()

	newService := func(secret string, repo *memoryRepo) (*Service, *fakeAPI, uuid.UUID) {
		userID := uuid.New()
		api := &fakeAPI{
			token:         "inst-token",
			expiresAt:     time.Now().Add(time.Hour),
			webhookSecret: "wh-secret-" + secret,
			installations: map[int64]InstallationInfo{
				999: {ID: 999, Account: "acme", AppID: 123},
			},
		}
		svc := NewService(Config{
			Repository:         repo,
			NewAPI:             func(_ string) GitHubAPI { return api },
			Secret:             "test-secret-key",
			AllowUnsafeBaseURL: true,
		})
		return svc, api, userID
	}

	// Both services share one repository: installation 999 resolves to both
	// apps, so the test proves scoping instead of isolation by separation.
	shared := newMemoryRepo()
	svcA, apiA, userA := newService("a", shared)
	svcB, apiB, userB := newService("b", shared)
	apiA.pem = testKeyPEM(t)
	apiB.pem = testKeyPEM(t)

	// B's GitHub knows no installation 999 for B's app: recording fails.
	appB := connect(t, svcB, userB)
	_, installState, err := svcB.InstallURL(ctx, userB, appB.ID)
	if err != nil {
		t.Fatal(err)
	}
	delete(apiB.installations, 999)
	if _, err := svcB.RecordInstallation(ctx, userB, appB.ID, 999, installState); err == nil {
		t.Fatal("B recorded an installation its app does not own")
	}

	// A installs 999 legitimately.
	appA := connect(t, svcA, userA)
	_, installStateA, err := svcA.InstallURL(ctx, userA, appA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.RecordInstallation(ctx, userA, appA.ID, 999, installStateA); err != nil {
		t.Fatal(err)
	}

	// B holds a stale row for 999 (recorded before verification existed), so
	// the shared repository resolves 999 to BOTH apps. B's signed deleted
	// delivery still verifies against B's app only...
	if _, err := shared.UpsertInstallation(ctx, appB.ID, 999, "acme"); err != nil {
		t.Fatal(err)
	}
	deleted := []byte(`{"action":"deleted","installation":{"id":999,"account":{"login":"acme"}}}`)
	header := http.Header{}
	header.Set("X-Hub-Signature-256", signBody("wh-secret-b", deleted))
	verifiedApp, ok := svcB.VerifyDelivery(header, deleted)
	if !ok || verifiedApp != appB.ID {
		t.Fatalf("B's delivery verified as %v, %v", verifiedApp, ok)
	}
	if err := svcB.HandleAppEvent(ctx, verifiedApp, "installation", deleted); err != nil {
		t.Fatal(err)
	}
	// ...and A's installation survives it.
	insts, err := shared.ListInstallations(ctx, appA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 1 || insts[0].InstallationID != 999 {
		t.Fatalf("A's installation after B's delivery = %+v", insts)
	}
	// B's own row is gone.
	insts, err = shared.ListInstallations(ctx, appB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 0 {
		t.Fatalf("B's installation after B's delivery = %+v", insts)
	}
}

// TestVerifyPushAndTargets proves an app-signed push verifies to the owning
// app and maps to the github_app applications watching the repository.
func TestVerifyPushAndTargets(t *testing.T) {
	svc, repo, api, userID := testFixture()
	api.pem = testKeyPEM(t)

	app := connect(t, svc, userID)
	_, state, err := svc.InstallURL(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordInstallation(context.Background(), userID, app.ID, 999, state); err != nil {
		t.Fatal(err)
	}
	watched := repo.watch(app.ID, "acme/web", "main")

	push := []byte(`{"ref":"refs/heads/main","after":"abc123",` +
		`"repository":{"full_name":"acme/web"},"installation":{"id":999}}`)
	header := http.Header{}
	header.Set("X-Hub-Signature-256", signBody("wh-secret", push))
	// The hook target header names the APP id (123), not the installation:
	// real-shaped deliveries verify through the payload installation id.
	header.Set("X-GitHub-Hook-Installation-Target-ID", "123")
	verifiedApp, ok := svc.VerifyPush(header, push)
	if !ok || verifiedApp != app.ID {
		t.Fatalf("push verified as %v, %v", verifiedApp, ok)
	}

	targets, err := svc.PushTargetsForWebhook(context.Background(), app.ID, "ACME/web")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ApplicationID != watched.ApplicationID || targets[0].Branch != "main" {
		t.Fatalf("targets = %+v", targets)
	}

	// A push signed with another secret verifies against nothing.
	forged := http.Header{}
	forged.Set("X-Hub-Signature-256", signBody("attacker-secret", push))
	if _, ok := svc.VerifyPush(forged, push); ok {
		t.Fatal("forged push was verified")
	}

	// A target header naming another app fails closed: the hint selects no
	// candidate, so even a valid signature verifies against nothing.
	mismatched := http.Header{}
	mismatched.Set("X-Hub-Signature-256", signBody("wh-secret", push))
	mismatched.Set("X-GitHub-Hook-Installation-Target-ID", "456")
	if _, ok := svc.VerifyPush(mismatched, push); ok {
		t.Fatal("push with a foreign app hint was verified")
	}
}

// TestTokenCloneURLResolvesGrant proves the clone URL carries a minted token
// for the installation granting the repo: two installations, the repo granted
// to the second, and the token minted for the second installation id. A clone
// URL on a foreign host is refused before any token is minted.
func TestTokenCloneURLResolvesGrant(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)
	api.token = "" // per-installation tokens prove the granting installation
	ctx := context.Background()

	app := connect(t, svc, userID)
	// Installation 999 grants nothing; 1000 grants acme/web. The fake serves
	// per-token listings because the service always refreshes.
	api.reposFor = map[string][]Repo{
		"tok-1000": {{ExternalID: "2", Name: "web", FullName: "acme/web", CloneURL: "https://github.com/acme/web.git"}},
	}
	mintInstall := func(id int64) {
		_, state, err := svc.InstallURL(ctx, userID, app.ID)
		if err != nil {
			t.Fatal(err)
		}
		api.installations[id] = InstallationInfo{ID: id, Account: "acme", AppID: 123}
		if _, err := svc.RecordInstallation(ctx, userID, app.ID, id, state); err != nil {
			t.Fatal(err)
		}
	}
	mintInstall(999)
	mintInstall(1000)

	mintsBefore := len(api.tokenFor)
	tokenURL, err := svc.TokenCloneURL(ctx, userID, app.ID, "acme/web", "https://github.com/acme/web.git")
	if err != nil {
		t.Fatal(err)
	}
	// The token belongs to installation 1000: per-installation tokens prove
	// the clone used the granting installation, not Installations[0].
	if tokenURL != "https://x-access-token:tok-1000@github.com/acme/web.git" {
		t.Fatalf("token url = %q", tokenURL)
	}
	// The granting installation (1000) was already minted at install time and
	// its cached token is reused: the clone lookup mints nothing new, and 999
	// (which grants nothing) is never consulted.
	if len(api.tokenFor) != mintsBefore {
		t.Fatalf("clone lookup minted tokens: %v", api.tokenFor)
	}
	seen := map[int64]bool{}
	for _, id := range api.tokenFor {
		seen[id] = true
	}
	if !seen[1000] {
		t.Fatalf("no token was ever minted for installation 1000: %v", api.tokenFor)
	}

	// A foreign host is refused before minting.
	if _, err := svc.TokenCloneURL(ctx, userID, app.ID, "acme/web", "https://evil.example/acme/web.git"); err == nil {
		t.Fatal("foreign-host clone url was accepted")
	}
	// A transient GitHub failure fails the lookup instead of the sentinel:
	// the deploy must fail, never silently clone anonymously.
	api.reposErr = errors.New("502 Bad Gateway")
	if _, err := svc.TokenCloneURL(ctx, userID, app.ID, "acme/web", "https://github.com/acme/web.git"); !errors.Is(err, ErrGrantsUnverifiable) {
		t.Fatalf("transient failure err = %v, want ErrGrantsUnverifiable", err)
	}
	if _, err := svc.TokenCloneURL(ctx, userID, app.ID, "acme/web", "https://github.com/acme/web.git"); errors.Is(err, ErrNoInstallationGrant) {
		t.Fatal("transient failure degraded to the no-grant sentinel")
	}
	api.reposErr = nil
	// An ungranted repo fails with the sentinel the cloner surfaces.
	if _, err := svc.TokenCloneURL(ctx, userID, app.ID, "acme/unknown", "https://github.com/acme/unknown.git"); !errors.Is(err, ErrNoInstallationGrant) {
		t.Fatalf("ungranted repo err = %v, want ErrNoInstallationGrant", err)
	}
	// A plaintext http URL is refused even though the host matches, before
	// any grant is listed: with GitHub failing, the error stays the
	// validation refusal, never the unverifiable-grants failure.
	api.reposErr = errors.New("502 Bad Gateway")
	if _, err := svc.TokenCloneURL(ctx, userID, app.ID, "acme/web", "http://github.com/acme/web.git"); !errors.Is(err, ErrValidation) {
		t.Fatalf("http clone url err = %v, want ErrValidation", err)
	}
	api.reposErr = nil
	// A foreign connection id answers not-found, so one user's link can
	// never resolve through another user's connection.
	if _, err := svc.TokenCloneURL(ctx, userID, uuid.New(), "acme/web", "https://github.com/acme/web.git"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign app id err = %v, want ErrNotFound", err)
	}
}

// TestTokenCloneURLUsesOnlyTheLinkedConnection proves resolution stays
// inside the linked connection: two connections both grant acme/web, yet the
// token comes from the linked connection's installation, and a broken
// listing on the unrelated connection does not fail the lookup.
func TestTokenCloneURLUsesOnlyTheLinkedConnection(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)
	api.token = "" // per-installation tokens prove which installation minted
	ctx := context.Background()

	linked := connect(t, svc, userID)
	other := connect(t, svc, userID)
	web := Repo{ExternalID: "2", Name: "web", FullName: "acme/web", CloneURL: "https://github.com/acme/web.git"}
	api.reposFor = map[string][]Repo{"tok-1000": {web}, "tok-2000": {web}}
	record := func(app GitHubApp, id int64) {
		t.Helper()
		_, state, err := svc.InstallURL(ctx, userID, app.ID)
		if err != nil {
			t.Fatal(err)
		}
		api.installations[id] = InstallationInfo{ID: id, Account: "acme", AppID: 123}
		if _, err := svc.RecordInstallation(ctx, userID, app.ID, id, state); err != nil {
			t.Fatal(err)
		}
	}
	record(linked, 1000)
	record(other, 2000)

	// The unrelated connection's listing breaks after recording: the linked
	// lookup must not notice.
	api.reposErrFor = map[string]error{"tok-2000": errors.New("500 Internal Server Error")}
	tokenURL, err := svc.TokenCloneURL(ctx, userID, linked.ID, "acme/web", "https://github.com/acme/web.git")
	if err != nil {
		t.Fatalf("TokenCloneURL through the linked connection: %v", err)
	}
	if tokenURL != "https://x-access-token:tok-1000@github.com/acme/web.git" {
		t.Fatalf("token url = %q, want the linked installation's token", tokenURL)
	}
}

// TestEmbedTokenShapes pins the token URL builder: userinfo replaced,
// anything else refused.
func TestEmbedTokenShapes(t *testing.T) {
	got, err := embedToken("https://github.com/acme/demo.git", "tok")
	if err != nil {
		t.Fatalf("embedToken: %v", err)
	}
	if got != "https://x-access-token:tok@github.com/acme/demo.git" {
		t.Errorf("embedToken = %q", got)
	}
	got, err = embedToken("https://user:old@github.com/acme/demo.git", "tok")
	if err != nil || got != "https://x-access-token:tok@github.com/acme/demo.git" {
		t.Errorf("embedToken with userinfo = %q, %v", got, err)
	}
}

func TestDisconnectReportsUsage(t *testing.T) {
	svc, repo, _, userID := testFixture()
	repo.appsUsing = []string{"api", "web"}

	app := connect(t, svc, userID)
	result, err := svc.Disconnect(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ApplicationsUsing != 2 {
		t.Fatalf("applications using = %d, want 2", result.ApplicationsUsing)
	}
	if len(result.Applications) != 2 || result.Applications[0] != "api" || result.Applications[1] != "web" {
		t.Fatalf("applications = %+v, want the linked names", result.Applications)
	}
	if _, err := svc.ListApps(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	apps, _ := svc.ListApps(context.Background(), userID)
	if len(apps) != 0 {
		t.Fatalf("apps after disconnect = %+v", apps)
	}
}
