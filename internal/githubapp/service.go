package githubapp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
)

// Defaults for github.com; Enterprise URLs derive from the caller's base URL.
const (
	defaultBaseURL    = "https://github.com"
	defaultAPIBaseURL = "https://api.github.com"
)

// tokenRefreshMargin refreshes an installation token this long before expiry,
// so a token never dies mid-listing.
const tokenRefreshMargin = time.Minute

// GitHubApp is a stored GitHub App connection. Secrets never appear here: the
// webhook secret and private key are sealed at rest and opened only for
// signing and verification.
type GitHubApp struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	AppID         int64
	Slug          string
	Name          string
	BaseURL       string
	APIBaseURL    string
	ClientID      string
	Installations []Installation
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Installation is one installation of a GitHub App.
type Installation struct {
	ID             uuid.UUID
	InstallationID int64
	Account        string
}

// Repository persists GitHub Apps, their installations and the repo cache.
// Sealed credential fields cross this boundary still sealed; the service opens
// them only when it needs them.
type Repository interface {
	CreateApp(ctx context.Context, app GitHubApp, webhookSecret, privateKey string) (GitHubApp, error)
	GetApp(ctx context.Context, id, userID uuid.UUID) (GitHubApp, error)
	// GetSealed returns the app row with its still-sealed secrets for signing.
	GetSealed(ctx context.Context, id, userID uuid.UUID) (sealedApp, error)
	ListApps(ctx context.Context, userID uuid.UUID) ([]GitHubApp, error)
	DeleteApp(ctx context.Context, id, userID uuid.UUID) error
	UpsertInstallation(ctx context.Context, appID uuid.UUID, installationID int64, account string) (Installation, error)
	ListInstallations(ctx context.Context, appID uuid.UUID) ([]Installation, error)
	DeleteInstallation(ctx context.Context, installationID int64, appID uuid.UUID) error
	AppsByInstallationID(ctx context.Context, installationID int64) ([]sealedApp, error)
	ReplaceRepoCache(ctx context.Context, appID uuid.UUID, installationID int64, repos []Repo) error
	ListRepoCache(ctx context.Context, appID uuid.UUID, installationID int64) ([]Repo, error)
	CountApplications(ctx context.Context, userID uuid.UUID) (int64, error)
}

// sealedApp is a GitHub App row with its still-sealed secrets, for webhook
// verification and signing.
type sealedApp struct {
	GitHubApp
	WebhookSecret string
	PrivateKey    string
}

// Service coordinates the GitHub App flow. It is safe for concurrent use.
type Service struct {
	repo        Repository
	newAPI      func(apiBase string) GitHubAPI
	secret      string
	logger      *slog.Logger
	states      *stateStore
	allowUnsafe bool

	mu     sync.Mutex
	tokens map[uuid.UUID]cachedToken
}

// cachedToken is one installation token, kept until it nears expiry.
type cachedToken struct {
	appID     uuid.UUID
	token     string
	expiresAt time.Time
}

// Config wires a Service. Repository is required; NewAPI builds the GitHub
// client per API base (tests inject a fake); Logger defaults to slog.Default.
// AllowUnsafeBaseURL permits the loopback fakes tests use.
type Config struct {
	Repository         Repository
	NewAPI             func(apiBase string) GitHubAPI
	Secret             string
	Logger             *slog.Logger
	AllowUnsafeBaseURL bool
}

// NewService builds a Service from cfg.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	newAPI := cfg.NewAPI
	if newAPI == nil {
		allowUnsafe := cfg.AllowUnsafeBaseURL
		newAPI = func(apiBase string) GitHubAPI { return NewHTTPAPI(apiBase, allowUnsafe) }
	}
	return &Service{
		repo:        cfg.Repository,
		newAPI:      newAPI,
		secret:      cfg.Secret,
		logger:      logger,
		states:      newStateStore(),
		allowUnsafe: cfg.AllowUnsafeBaseURL,
		tokens:      make(map[uuid.UUID]cachedToken),
	}
}

// NewDefaultService builds the production service over st. It returns nil when
// st is nil so the HTTP layer skips mounting the routes. An empty secret
// selects an ephemeral key like the providers package.
func NewDefaultService(st *store.Store, secret string, logger *slog.Logger) *Service {
	if st == nil {
		return nil
	}
	if strings.TrimSpace(secret) == "" {
		secret = randomSecret()
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("githubapp: secret_key is empty; app credentials use an ephemeral key")
	}
	return NewService(Config{Repository: newStoreRepository(st, secret), AllowUnsafeBaseURL: false})
}

// randomSecret mirrors providers.randomSecret without importing unexported
// helpers: 32 random bytes, base64url-encoded.
func randomSecret() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "githubapp-ephemeral"
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Manifest describes the form the browser posts to start the manifest flow.
type Manifest struct {
	// ActionURL is where the browser posts the manifest
	// (github.com/settings/apps/new or the Enterprise equivalent).
	ActionURL string `json:"action_url"`
	// Manifest is the manifest payload the form posts as the manifest field.
	Manifest map[string]any `json:"manifest"`
	// State binds the callback to this browser; it is single-use and expires.
	State string `json:"state"`
}

// StartManifest serves the manifest for userID: the form posts it to the Git
// host, which redirects back with a code the callback exchanges. origin is
// the control-plane public origin (scheme + host), used for the hook and
// redirect URLs.
func (s *Service) StartManifest(ctx context.Context, userID uuid.UUID, baseURL, name, origin string) (Manifest, error) {
	if s.repo == nil {
		return Manifest{}, ErrNotFound
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if !s.allowUnsafe {
		if err := guardPublicURL(baseURL); err != nil {
			return Manifest{}, err
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "gotham"
	}
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return Manifest{}, fmt.Errorf("%w: origin is required", ErrValidation)
	}
	state, err := s.states.new(userID, uuid.Nil, stateManifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest := map[string]any{
		"name":         name,
		"url":          origin,
		"redirect_url": origin + "/api/v1/providers/github-app/callback?state=" + state,
		"hook_attributes": map[string]any{
			"url": origin + "/api/v1/webhooks/github",
		},
		"public":          false,
		"setup_on_update": true,
		"default_permissions": map[string]any{
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "write",
		},
		"default_events": []string{"push", "pull_request", "installation", "installation_repositories"},
	}
	return Manifest{
		ActionURL: strings.TrimRight(baseURL, "/") + "/settings/apps/new?state=" + state,
		Manifest:  manifest,
		State:     state,
	}, nil
}

// Callback finishes the manifest flow: it redeems state once, exchanges code
// for the app credentials and stores them sealed. The secrets never appear in
// the returned app.
func (s *Service) Callback(ctx context.Context, userID uuid.UUID, code, state, origin string) (GitHubApp, error) {
	if s.repo == nil {
		return GitHubApp{}, ErrNotFound
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return GitHubApp{}, fmt.Errorf("%w: code and state are required", ErrValidation)
	}
	if !s.states.redeem(state, userID, uuid.Nil, stateManifest) {
		return GitHubApp{}, fmt.Errorf("%w: invalid or expired manifest state", ErrValidation)
	}
	apiBase := apiBaseFor(strings.TrimSpace(origin))
	api := s.newAPI(apiBase)
	conv, err := api.ExchangeManifest(ctx, code)
	if err != nil {
		return GitHubApp{}, err
	}
	sealedSecret, err := providers.SealSecret(s.secret, conv.WebhookSecret)
	if err != nil {
		return GitHubApp{}, err
	}
	sealedKey, err := providers.SealSecret(s.secret, conv.PEM)
	if err != nil {
		return GitHubApp{}, err
	}
	baseURL := baseURLFor(strings.TrimSpace(origin))
	app, err := s.repo.CreateApp(ctx, GitHubApp{
		UserID:     userID,
		AppID:      conv.ID,
		Slug:       conv.Slug,
		Name:       conv.Name,
		BaseURL:    baseURL,
		APIBaseURL: apiBase,
		ClientID:   conv.ClientID,
	}, sealedSecret, sealedKey)
	if err != nil {
		return GitHubApp{}, err
	}
	return s.repo.GetApp(ctx, app.ID, userID)
}

// InstallURL starts the installation step: the browser visits the URL to
// install the app, and the setup callback records the installation.
func (s *Service) InstallURL(ctx context.Context, userID, appID uuid.UUID) (string, string, error) {
	if s.repo == nil {
		return "", "", ErrNotFound
	}
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return "", "", err
	}
	state, err := s.states.new(userID, appID, stateInstall)
	if err != nil {
		return "", "", err
	}
	var installURL string
	if strings.TrimSpace(app.Slug) == "" {
		installURL = strings.TrimRight(app.BaseURL, "/") + "/settings/installations/new?state=" + state
	} else if app.BaseURL == defaultBaseURL {
		installURL = "https://github.com/apps/" + app.Slug + "/installations/new?state=" + state
	} else {
		installURL = strings.TrimRight(app.BaseURL, "/") + "/github-apps/" + app.Slug + "/installations/new?state=" + state
	}
	return installURL, state, nil
}

// RecordInstallation stores the installation callback and refreshes its repo
// cache, so the wizard lists repositories immediately.
func (s *Service) RecordInstallation(ctx context.Context, userID, appID uuid.UUID, installationID int64, state string) (Installation, error) {
	if s.repo == nil {
		return Installation{}, ErrNotFound
	}
	if installationID <= 0 {
		return Installation{}, fmt.Errorf("%w: installation id is required", ErrValidation)
	}
	if !s.states.redeem(state, userID, appID, stateInstall) {
		return Installation{}, fmt.Errorf("%w: invalid or expired install state", ErrValidation)
	}
	inst, err := s.repo.UpsertInstallation(ctx, appID, installationID, "")
	if err != nil {
		return Installation{}, err
	}
	if _, err := s.refreshRepos(ctx, userID, appID, installationID); err != nil {
		s.logger.Warn("githubapp: installation repo refresh failed", "installation", installationID, "error", err)
	}
	return inst, nil
}

// ListApps returns the caller's GitHub Apps with their installations.
func (s *Service) ListApps(ctx context.Context, userID uuid.UUID) ([]GitHubApp, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	return s.repo.ListApps(ctx, userID)
}

// ListRepos returns the repositories of the app's first installation,
// refreshing the cache through the installation token.
func (s *Service) ListRepos(ctx context.Context, userID, appID uuid.UUID) ([]Repo, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if len(app.Installations) == 0 {
		return nil, fmt.Errorf("%w: app is not installed yet", ErrValidation)
	}
	installation := app.Installations[0].InstallationID
	repos, err := s.refreshRepos(ctx, userID, appID, installation)
	if err != nil {
		// A refresh failure falls back to the cache: the listing stays
		// available when GitHub is briefly unreachable.
		s.logger.Warn("githubapp: repo refresh failed, serving cache", "app", app.AppID, "error", err)
		return s.repo.ListRepoCache(ctx, appID, installation)
	}
	return repos, nil
}

// ListBranches returns the branches of repo through the installation token.
// repo is "owner/name" in the installation that owns it.
func (s *Service) ListBranches(ctx context.Context, userID, appID uuid.UUID, repo string) ([]Branch, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	if strings.Count(strings.TrimSpace(repo), "/") != 1 {
		return nil, fmt.Errorf("%w: repository must be owner/name", ErrValidation)
	}
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealed(ctx, app)
	if err != nil {
		return nil, err
	}
	key, err := providers.OpenSecret(s.secret, sealed.PrivateKey)
	if err != nil {
		return nil, err
	}
	jwt, err := signAppJWT(app.AppID, []byte(key), time.Now())
	if err != nil {
		return nil, err
	}
	installation, err := s.installationForRepo(ctx, app, strings.TrimSpace(repo))
	if err != nil {
		return nil, err
	}
	token, err := s.installationToken(ctx, app, installation, jwt)
	if err != nil {
		return nil, err
	}
	return s.newAPI(app.APIBaseURL).ListBranches(ctx, token, strings.TrimSpace(repo))
}

// Disconnect deletes the stored credentials. It reports how many of the
// caller's applications use the github_app source, so the UI can warn before
// deleting.
func (s *Service) Disconnect(ctx context.Context, userID, appID uuid.UUID) (int64, error) {
	if s.repo == nil {
		return 0, ErrNotFound
	}
	if _, err := s.repo.GetApp(ctx, appID, userID); err != nil {
		return 0, err
	}
	using, err := s.repo.CountApplications(ctx, userID)
	if err != nil {
		return 0, err
	}
	if err := s.repo.DeleteApp(ctx, appID, userID); err != nil {
		return 0, err
	}
	s.mu.Lock()
	for id, cached := range s.tokens {
		if cached.appID == appID {
			delete(s.tokens, id)
		}
	}
	s.mu.Unlock()
	return using, nil
}

// installationToken mints (or reuses a cached) installation token.
func (s *Service) installationToken(ctx context.Context, app GitHubApp, installation Installation, jwt string) (string, error) {
	s.mu.Lock()
	cached, ok := s.tokens[installation.ID]
	s.mu.Unlock()
	if ok && time.Now().Add(tokenRefreshMargin).Before(cached.expiresAt) {
		return cached.token, nil
	}
	tok, err := s.newAPI(app.APIBaseURL).CreateInstallationToken(ctx, installation.InstallationID, jwt)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.tokens[installation.ID] = cachedToken{appID: app.ID, token: tok.Token, expiresAt: tok.ExpiresAt}
	s.mu.Unlock()
	return tok.Token, nil
}

// refreshRepos lists through the installation token and rewrites the cache.
func (s *Service) refreshRepos(ctx context.Context, userID, appID uuid.UUID, installationID int64) ([]Repo, error) {
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealed(ctx, app)
	if err != nil {
		return nil, err
	}
	key, err := providers.OpenSecret(s.secret, sealed.PrivateKey)
	if err != nil {
		return nil, err
	}
	jwt, err := signAppJWT(app.AppID, []byte(key), time.Now())
	if err != nil {
		return nil, err
	}
	var installation Installation
	for _, inst := range app.Installations {
		if inst.InstallationID == installationID {
			installation = inst
		}
	}
	if installation.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: unknown installation", ErrValidation)
	}
	token, err := s.installationToken(ctx, app, installation, jwt)
	if err != nil {
		return nil, err
	}
	repos, err := s.newAPI(app.APIBaseURL).ListInstallationRepos(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceRepoCache(ctx, appID, installationID, repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// sealed loads the app row with its sealed secrets for signing.
func (s *Service) sealed(ctx context.Context, app GitHubApp) (sealedApp, error) {
	return s.repo.GetSealed(ctx, app.ID, app.UserID)
}

// installationForRepo picks the installation whose cache holds repo, falling
// back to the first installation.
func (s *Service) installationForRepo(ctx context.Context, app GitHubApp, repo string) (Installation, error) {
	for _, inst := range app.Installations {
		cached, err := s.repo.ListRepoCache(ctx, app.ID, inst.InstallationID)
		if err != nil {
			continue
		}
		for _, r := range cached {
			if strings.EqualFold(r.FullName, repo) {
				return inst, nil
			}
		}
	}
	if len(app.Installations) == 0 {
		return Installation{}, fmt.Errorf("%w: app is not installed yet", ErrValidation)
	}
	return app.Installations[0], nil
}

// baseURLFor derives the web base URL from a manifest origin hint: an empty
// hint (github.com flow) stays on github.com, anything else is the Enterprise
// host the manifest was started for.
func baseURLFor(origin string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" || strings.Contains(origin, "github.com") {
		return defaultBaseURL
	}
	return origin
}

// apiBaseFor derives the API base URL the same way: github.com uses
// api.github.com, an Enterprise host uses its /api/v3 root.
func apiBaseFor(origin string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" || strings.Contains(origin, "github.com") {
		return defaultAPIBaseURL
	}
	return origin + "/api/v3"
}

// guardPublicURL refuses manifest base URLs that are not public.
func guardPublicURL(raw string) error {
	host := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	if !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("%w: github host must use https", ErrValidation)
	}
	return guardHost(host)
}

// appEvent is the subset of installation deliveries this package acts on.
type appEvent struct {
	Action       string `json:"action"`
	Installation *struct {
		ID      int64 `json:"id"`
		Account *struct {
			Login string `json:"login"`
		} `json:"account"`
	} `json:"installation"`
}

// VerifyDelivery reports whether body authenticates as a GitHub App webhook
// delivery: some stored app secret must produce the sha256=HMAC signature.
// It mirrors the webhooks signature check, so the existing receiver keeps its
// semantics for push deliveries while this covers installation events.
func (s *Service) VerifyDelivery(header http.Header, body []byte) bool {
	if s.repo == nil {
		return false
	}
	provided := header.Get("X-Hub-Signature-256")
	if !strings.HasPrefix(provided, "sha256=") {
		return false
	}
	provided = strings.TrimPrefix(provided, "sha256=")
	var event appEvent
	if err := json.Unmarshal(body, &event); err != nil || event.Installation == nil {
		return false
	}
	ctx := context.Background()
	apps, err := s.repo.AppsByInstallationID(ctx, event.Installation.ID)
	if err != nil || len(apps) == 0 {
		return false
	}
	for _, app := range apps {
		secret, err := providers.OpenSecret(s.secret, app.WebhookSecret)
		if err != nil || secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		if hmac.Equal([]byte(provided), []byte(hex.EncodeToString(mac.Sum(nil)))) {
			return true
		}
	}
	return false
}

// HandleAppEvent refreshes the repo cache for installation and
// installation_repositories deliveries. Callers verify the signature first
// (VerifyDelivery); an unverifiable body is refused here too. Push deliveries
// keep flowing through the existing webhook receiver, which validates with the
// per-hook secret and triggers the deploy.
func (s *Service) HandleAppEvent(ctx context.Context, event string, body []byte) error {
	if s.repo == nil {
		return ErrNotFound
	}
	event = strings.ToLower(strings.TrimSpace(event))
	if event != "installation" && event != "installation_repositories" {
		return nil
	}
	var parsed appEvent
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Installation == nil {
		return fmt.Errorf("%w: delivery names no installation", ErrValidation)
	}
	apps, err := s.repo.AppsByInstallationID(ctx, parsed.Installation.ID)
	if err != nil {
		return err
	}
	for _, app := range apps {
		account := ""
		if parsed.Installation.Account != nil {
			account = parsed.Installation.Account.Login
		}
		if strings.ToLower(strings.TrimSpace(parsed.Action)) == "deleted" && event == "installation" {
			if err := s.repo.DeleteInstallation(ctx, parsed.Installation.ID, app.ID); err != nil {
				return err
			}
			continue
		}
		if _, err := s.repo.UpsertInstallation(ctx, app.ID, parsed.Installation.ID, account); err != nil {
			return err
		}
		if _, err := s.refreshReposFor(ctx, app, parsed.Installation.ID); err != nil {
			s.logger.Warn("githubapp: app event refresh failed",
				"app", app.AppID, "installation", parsed.Installation.ID, "error", err)
		}
	}
	return nil
}

// refreshReposFor refreshes one installation when the app row (with secrets)
// is already in hand.
func (s *Service) refreshReposFor(ctx context.Context, app sealedApp, installationID int64) ([]Repo, error) {
	key, err := providers.OpenSecret(s.secret, app.PrivateKey)
	if err != nil {
		return nil, err
	}
	jwt, err := signAppJWT(app.AppID, []byte(key), time.Now())
	if err != nil {
		return nil, err
	}
	insts, err := s.repo.ListInstallations(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	var installation Installation
	for _, inst := range insts {
		if inst.InstallationID == installationID {
			installation = inst
		}
	}
	if installation.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: unknown installation", ErrValidation)
	}
	token, err := s.installationToken(ctx, app.GitHubApp, installation, jwt)
	if err != nil {
		return nil, err
	}
	repos, err := s.newAPI(app.APIBaseURL).ListInstallationRepos(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceRepoCache(ctx, app.ID, installationID, repos); err != nil {
		return nil, err
	}
	return repos, nil
}
