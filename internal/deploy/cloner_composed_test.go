package deploy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/githubapp"
)

// composedFakeAPI implements githubapp.GitHubAPI with per-installation tokens
// and no network.
type composedFakeAPI struct {
	mu            sync.Mutex
	pem           string
	tokenFor      []int64
	repos         map[int64][]githubapp.Repo
	reposErr      error
	installations map[int64]githubapp.InstallationInfo
}

func (f *composedFakeAPI) ExchangeManifest(_ context.Context, code string) (githubapp.ManifestConversion, error) {
	if code == "" {
		return githubapp.ManifestConversion{}, fmt.Errorf("empty code")
	}
	return githubapp.ManifestConversion{
		ID: 123, Slug: "gotham-test", Name: "gotham-test",
		WebhookSecret: "wh-secret", PEM: f.pem,
	}, nil
}

func (f *composedFakeAPI) GetInstallation(_ context.Context, id int64, _ string) (githubapp.InstallationInfo, error) {
	info, ok := f.installations[id]
	if !ok {
		return githubapp.InstallationInfo{}, fmt.Errorf("unknown installation")
	}
	return info, nil
}

func (f *composedFakeAPI) CreateInstallationToken(_ context.Context, id int64, _ string) (githubapp.InstallationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenFor = append(f.tokenFor, id)
	return githubapp.InstallationToken{
		Token:     fmt.Sprintf("tok-%d", id),
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (f *composedFakeAPI) ListInstallationRepos(_ context.Context, token string) ([]githubapp.Repo, bool, error) {
	if f.reposErr != nil {
		return nil, false, f.reposErr
	}
	var id int64
	_, _ = fmt.Sscanf(token, "tok-%d", &id)
	return append([]githubapp.Repo(nil), f.repos[id]...), false, nil
}

func (f *composedFakeAPI) ListBranches(_ context.Context, _ string, _ string) ([]githubapp.Branch, error) {
	return nil, nil
}

// composedAppRepo is an in-memory githubapp.Repository.
type composedAppRepo struct {
	mu      sync.Mutex
	apps    map[uuid.UUID]githubapp.SealedApp
	insts   map[uuid.UUID][]githubapp.Installation
	cache   map[string][]githubapp.Repo
	targets map[string][]githubapp.AppPushTarget
}

func newComposedAppRepo() *composedAppRepo {
	return &composedAppRepo{
		apps:    make(map[uuid.UUID]githubapp.SealedApp),
		insts:   make(map[uuid.UUID][]githubapp.Installation),
		cache:   make(map[string][]githubapp.Repo),
		targets: make(map[string][]githubapp.AppPushTarget),
	}
}

func cacheKey(appID uuid.UUID, installationID int64) string {
	return appID.String() + "/" + fmt.Sprintf("%d", installationID)
}

func (m *composedAppRepo) CreateApp(_ context.Context, app githubapp.GitHubApp, secret, key string) (githubapp.GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app.ID = uuid.New()
	m.apps[app.ID] = githubapp.SealedApp{GitHubApp: app, WebhookSecret: secret, PrivateKey: key}
	return app, nil
}

func (m *composedAppRepo) withInstalls(id uuid.UUID) (githubapp.GitHubApp, githubapp.SealedApp, bool) {
	sealed, ok := m.apps[id]
	if !ok {
		return githubapp.GitHubApp{}, githubapp.SealedApp{}, false
	}
	app := sealed.GitHubApp
	app.Installations = append([]githubapp.Installation(nil), m.insts[id]...)
	return app, sealed, true
}

func (m *composedAppRepo) GetApp(_ context.Context, id, _ uuid.UUID) (githubapp.GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, _, ok := m.withInstalls(id)
	if !ok {
		return githubapp.GitHubApp{}, fmt.Errorf("no app")
	}
	return app, nil
}

func (m *composedAppRepo) GetSealed(_ context.Context, id, _ uuid.UUID) (githubapp.SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, sealed, ok := m.withInstalls(id)
	if !ok {
		return githubapp.SealedApp{}, fmt.Errorf("no app")
	}
	return sealed, nil
}

func (m *composedAppRepo) GetSealedByID(_ context.Context, id uuid.UUID) (githubapp.SealedApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, sealed, ok := m.withInstalls(id)
	if !ok {
		return githubapp.SealedApp{}, fmt.Errorf("no app")
	}
	return sealed, nil
}

func (m *composedAppRepo) ListApps(_ context.Context, _ uuid.UUID) ([]githubapp.GitHubApp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var apps []githubapp.GitHubApp
	for id := range m.apps {
		app, _, _ := m.withInstalls(id)
		apps = append(apps, app)
	}
	return apps, nil
}

func (m *composedAppRepo) DeleteApp(_ context.Context, _, _ uuid.UUID) error { return nil }

func (m *composedAppRepo) UpsertInstallation(_ context.Context, appID uuid.UUID, installationID int64, account string) (githubapp.Installation, error) {
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

func (m *composedAppRepo) ListInstallations(_ context.Context, appID uuid.UUID) ([]githubapp.Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]githubapp.Installation(nil), m.insts[appID]...), nil
}

func (m *composedAppRepo) DeleteInstallation(_ context.Context, _ int64, _ uuid.UUID) error {
	return nil
}

func (m *composedAppRepo) AppsByInstallationID(_ context.Context, installationID int64) ([]githubapp.SealedApp, error) {
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

func (m *composedAppRepo) ReplaceRepoCache(_ context.Context, appID uuid.UUID, installationID int64, repos []githubapp.Repo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[cacheKey(appID, installationID)] = append([]githubapp.Repo(nil), repos...)
	return nil
}

func (m *composedAppRepo) ListRepoCache(_ context.Context, appID uuid.UUID, installationID int64) ([]githubapp.Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]githubapp.Repo(nil), m.cache[cacheKey(appID, installationID)]...), nil
}

func (m *composedAppRepo) CountApplicationsForApp(_ context.Context, _, _ uuid.UUID) (int64, error) {
	return 0, nil
}

func (m *composedAppRepo) PushTargets(_ context.Context, _, _ uuid.UUID, _ string) ([]githubapp.AppPushTarget, error) {
	return nil, nil
}

func composedTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// TestCloneSshWithGrantKeepsDeployKey runs a linked application with an SSH
// clone URL and a deploy key through the REAL service and cloner: the repo
// is granted, but SSH URLs never take the token path, so the clone uses the
// deploy key and mints no token.
func TestCloneSshWithGrantKeepsDeployKey(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	api := &composedFakeAPI{
		pem: composedTestKey(t),
		repos: map[int64][]githubapp.Repo{
			1000: {{ExternalID: "2", Name: "legacy", FullName: "acme/legacy", DefaultBranch: "main"}},
		},
		installations: map[int64]githubapp.InstallationInfo{
			1000: {ID: 1000, Account: "acme", AppID: 123},
		},
	}
	appRepo := newComposedAppRepo()
	appSvc := githubapp.NewService(githubapp.Config{
		Repository:         appRepo,
		NewAPI:             func(_ string) githubapp.GitHubAPI { return api },
		Secret:             "test-secret-key",
		AllowUnsafeBaseURL: true,
	})

	manifest, err := appSvc.StartManifest(ctx, userID, "", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appSvc.Callback(ctx, userID, "manifest-code", manifest.State); err != nil {
		t.Fatal(err)
	}
	apps, err := appSvc.ListApps(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("apps = %+v, %v", apps, err)
	}
	_, installState, err := appSvc.InstallURL(ctx, userID, apps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appSvc.RecordInstallation(ctx, userID, apps[0].ID, 1000, installState); err != nil {
		t.Fatal(err)
	}

	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	api.mu.Lock()
	mintsBefore := len(api.tokenFor)
	api.mu.Unlock()
	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	app.GitHubAppID = apps[0].ID
	app.Repo = "acme/legacy"
	app.CloneURL = "git@github.com:acme/legacy.git"

	var gotArgv, gotEnv []string
	source := gitSource{
		keys:      &staticKeyResolver{pem: privatePEM},
		appTokens: appSvc,
		run: func(_ context.Context, argv, env []string) ([]byte, error) {
			gotArgv = append([]string(nil), argv...)
			gotEnv = append([]string(nil), env...)
			return nil, nil
		},
	}
	if err := source.Clone(ctx, app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	joined := strings.Join(gotArgv, " ")
	if !strings.Contains(joined, "git@github.com:acme/legacy.git") {
		t.Errorf("argv = %v, want the deploy-key SSH clone", gotArgv)
	}
	if strings.Contains(joined, "x-access-token") {
		t.Errorf("argv = %v, want no token on an SSH URL", gotArgv)
	}
	if !hasEntry(gotEnv, "GIT_SSH_COMMAND") {
		t.Errorf("env = %v, want the deploy-key SSH command", gotEnv)
	}
	api.mu.Lock()
	mints := len(api.tokenFor) - mintsBefore
	api.mu.Unlock()
	if mints != 0 {
		t.Errorf("token mints during clone = %d, want 0 (SSH never mints)", mints)
	}
}

// TestCloneLegacyHttpAppNeverTouchesService runs an unlinked https
// application through the REAL service as resolver: no grant lookup happens,
// no token is minted, the anonymous clone proceeds.
func TestCloneLegacyHttpAppNeverTouchesService(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	api := &composedFakeAPI{
		pem:           composedTestKey(t),
		repos:         map[int64][]githubapp.Repo{},
		installations: map[int64]githubapp.InstallationInfo{},
	}
	appSvc := githubapp.NewService(githubapp.Config{
		Repository:         newComposedAppRepo(),
		NewAPI:             func(_ string) githubapp.GitHubAPI { return api },
		Secret:             "test-secret-key",
		AllowUnsafeBaseURL: true,
	})

	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	app.Repo = "acme/plain"
	app.CloneURL = "https://github.com/acme/plain.git"

	var gotArgv []string
	source := gitSource{
		keys:      &staticKeyResolver{},
		appTokens: appSvc,
		run: func(_ context.Context, argv, _ []string) ([]byte, error) {
			gotArgv = append([]string(nil), argv...)
			return nil, nil
		},
	}
	if err := source.Clone(ctx, app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if joined := strings.Join(gotArgv, " "); !strings.Contains(joined, "https://github.com/acme/plain.git") {
		t.Errorf("argv = %v, want the plain clone URL", gotArgv)
	}
	api.mu.Lock()
	mints := len(api.tokenFor)
	api.mu.Unlock()
	if mints != 0 {
		t.Errorf("token mints = %d, want 0 for an unlinked application", mints)
	}
}

// TestCloneTransientFailureFailsNoSilentClone proves a transient GitHub
// failure on a linked application fails the deploy: no plain-URL clone is
// attempted, so a private repo never degrades to an anonymous clone.
func TestCloneTransientFailureFailsNoSilentClone(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	api := &composedFakeAPI{
		pem: composedTestKey(t),
		repos: map[int64][]githubapp.Repo{
			1000: {{ExternalID: "2", Name: "web", FullName: "acme/web", DefaultBranch: "main"}},
		},
		installations: map[int64]githubapp.InstallationInfo{
			1000: {ID: 1000, Account: "acme", AppID: 123},
		},
	}
	appRepo := newComposedAppRepo()
	appSvc := githubapp.NewService(githubapp.Config{
		Repository:         appRepo,
		NewAPI:             func(_ string) githubapp.GitHubAPI { return api },
		Secret:             "test-secret-key",
		AllowUnsafeBaseURL: true,
	})

	manifest, err := appSvc.StartManifest(ctx, userID, "", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appSvc.Callback(ctx, userID, "manifest-code", manifest.State); err != nil {
		t.Fatal(err)
	}
	apps, err := appSvc.ListApps(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("apps = %+v, %v", apps, err)
	}
	_, installState, err := appSvc.InstallURL(ctx, userID, apps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appSvc.RecordInstallation(ctx, userID, apps[0].ID, 1000, installState); err != nil {
		t.Fatal(err)
	}

	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	app.GitHubAppID = apps[0].ID
	app.Repo = "acme/web"
	app.CloneURL = "https://github.com/acme/web.git"

	// GitHub 502s the grant listing while the cache holds a stale grant.
	api.reposErr = errors.New("502 Bad Gateway")
	ran := false
	source := gitSource{
		keys:      &staticKeyResolver{},
		appTokens: appSvc,
		run: func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		},
	}
	if err := source.Clone(ctx, app, filepath.Join(t.TempDir(), "repo"), nil); err == nil {
		t.Fatal("clone with unverifiable grants succeeded")
	}
	if ran {
		t.Error("git ran despite unverifiable grants: no plain-URL clone may be attempted")
	}
}

// TestCloneThroughRealAppService runs connect -> install -> github_app
// application -> Clone through the REAL githubapp.Service and the REAL
// gitSource: only the git binary is faked. The clone URL must carry the
// token minted for the installation granting the repo.
func TestCloneThroughRealAppService(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	api := &composedFakeAPI{
		pem: composedTestKey(t),
		repos: map[int64][]githubapp.Repo{
			// Installation 999 grants nothing; 1000 grants acme/web.
			1000: {{ExternalID: "2", Name: "web", FullName: "acme/web", DefaultBranch: "main", CloneURL: "https://github.com/acme/web.git"}},
		},
		installations: map[int64]githubapp.InstallationInfo{
			999:  {ID: 999, Account: "acme", AppID: 123},
			1000: {ID: 1000, Account: "acme", AppID: 123},
		},
	}
	appRepo := newComposedAppRepo()
	appSvc := githubapp.NewService(githubapp.Config{
		Repository:         appRepo,
		NewAPI:             func(_ string) githubapp.GitHubAPI { return api },
		Secret:             "test-secret-key",
		AllowUnsafeBaseURL: true,
	})

	manifest, err := appSvc.StartManifest(ctx, userID, "", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appSvc.Callback(ctx, userID, "manifest-code", manifest.State); err != nil {
		t.Fatal(err)
	}
	apps, err := appSvc.ListApps(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("apps = %+v, %v", apps, err)
	}
	for _, id := range []int64{999, 1000} {
		_, installState, err := appSvc.InstallURL(ctx, userID, apps[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := appSvc.RecordInstallation(ctx, userID, apps[0].ID, id, installState); err != nil {
			t.Fatal(err)
		}
	}

	// The deploy application the wizard would create: https clone URL for a
	// private repo, github_app source. Its UUID is unrelated to any
	// github_apps row: resolution runs by repository grant.
	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	app.GitHubAppID = apps[0].ID
	app.Repo = "acme/web"
	app.CloneURL = "https://github.com/acme/web.git"

	var gotArgv []string
	var logged []string
	source := gitSource{
		keys:      &staticKeyResolver{},
		appTokens: appSvc,
		run: func(_ context.Context, argv, _ []string) ([]byte, error) {
			gotArgv = append([]string(nil), argv...)
			return nil, nil
		},
	}
	if err := source.Clone(ctx, app, filepath.Join(t.TempDir(), "repo"), func(line string) {
		logged = append(logged, line)
	}); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	joined := strings.Join(gotArgv, " ")
	if !strings.Contains(joined, "https://x-access-token:tok-1000@github.com/acme/web.git") {
		t.Errorf("argv = %v, want the token of installation 1000", gotArgv)
	}
	for _, line := range logged {
		if strings.Contains(line, "tok-1000") {
			t.Errorf("log line leaks the token: %q", line)
		}
	}
	if app.CloneURL != "https://github.com/acme/web.git" {
		t.Errorf("stored clone URL = %q, want it unchanged", app.CloneURL)
	}
}
