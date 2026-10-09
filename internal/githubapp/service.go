package githubapp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

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
	GetSealed(ctx context.Context, id, userID uuid.UUID) (SealedApp, error)
	// GetSealedByID returns the sealed row for webhook handling, where no
	// user session exists (the signature is the authentication).
	GetSealedByID(ctx context.Context, id uuid.UUID) (SealedApp, error)
	ListApps(ctx context.Context, userID uuid.UUID) ([]GitHubApp, error)
	DeleteApp(ctx context.Context, id, userID uuid.UUID) error
	UpsertInstallation(ctx context.Context, appID uuid.UUID, installationID int64, account string) (Installation, error)
	ListInstallations(ctx context.Context, appID uuid.UUID) ([]Installation, error)
	DeleteInstallation(ctx context.Context, installationID int64, appID uuid.UUID) error
	AppsByInstallationID(ctx context.Context, installationID int64) ([]SealedApp, error)
	ReplaceRepoCache(ctx context.Context, appID uuid.UUID, installationID int64, repos []Repo) error
	ListRepoCache(ctx context.Context, appID uuid.UUID, installationID int64) ([]Repo, error)
	// ListApplicationNamesForApp names the caller's applications linked to
	// this connection (by github_app_id, like push routing and cloning).
	ListApplicationNamesForApp(ctx context.Context, userID, appID uuid.UUID) ([]string, error)
	// PushTargets returns the github_app applications of the app's owner
	// watching repo (lowercased owner/name).
	PushTargets(ctx context.Context, appID, userID uuid.UUID, repo string) ([]AppPushTarget, error)
}

// SealedApp is a GitHub App row with its still-sealed secrets, for webhook
// verification and signing.
type SealedApp struct {
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
	// tokensFlight deduplicates concurrent installation-token mints for one
	// installation: without it N concurrent listings each mint a token.
	tokensFlight singleflight.Group
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
	return NewService(Config{
		Repository:         newStoreRepository(st),
		Secret:             secret,
		AllowUnsafeBaseURL: false,
	})
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
// the control-plane public origin (scheme + host), used for the hook URL and
// the browser callback URLs: redirect_url is the public API callback the
// browser lands on (GitHub appends ?code=...&state=... itself, so the stored
// URL carries no query), and setup_url
// is the SPA route that completes the installation. The chosen GitHub base
// URL is bound to the state, so the callback exchanges the code against the
// same host (github.com or Enterprise).
func (s *Service) StartManifest(ctx context.Context, userID uuid.UUID, baseURL, name, origin string) (Manifest, error) {
	if s.repo == nil {
		return Manifest{}, ErrNotFound
	}
	webBase, apiBase, err := s.normalizeGitHubURLs(baseURL)
	if err != nil {
		return Manifest{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "gotham"
	}
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return Manifest{}, fmt.Errorf("%w: origin is required", ErrValidation)
	}
	state, err := s.states.new(userID, uuid.Nil, stateManifest, webBase, apiBase)
	if err != nil {
		return Manifest{}, err
	}
	manifest := map[string]any{
		"name":          name,
		"url":           origin,
		"redirect_url":  origin + "/api/v1/providers/github-app/callback",
		"setup_url":     origin + "/applications/github-app/callback",
		"callback_urls": []string{origin + "/applications/github-app/callback"},
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
		// pull_request is subscribed only once previews support app-signed
		// deliveries: until then app-signed PR events would 401 in GitHub's
		// delivery log, so the manifest subscribes to push (deploy) only. The
		// installation and installation_repositories lifecycle events (cache)
		// are delivered to every GitHub App automatically and are rejected in
		// default_events ("not supported by permissions").
		"default_events": []string{"push"},
	}
	return Manifest{
		ActionURL: strings.TrimRight(webBase, "/") + "/settings/apps/new?state=" + state,
		Manifest:  manifest,
		State:     state,
	}, nil
}

// normalizeGitHubURLs resolves the caller's GitHub base URL into the web and
// API roots. Empty selects github.com; anything else must be an https URL on
// a public host (DNS-resolved, unless the explicit test setting allows
// otherwise), and an Enterprise host uses its /api/v3 root.
func (s *Service) normalizeGitHubURLs(baseURL string) (webBase, apiBase string, err error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return defaultBaseURL, defaultAPIBaseURL, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", "", fmt.Errorf("%w: github base url is not a URL", ErrValidation)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", "", fmt.Errorf("%w: github base url must use https", ErrValidation)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return "", "", fmt.Errorf("%w: github base url must be scheme and host only", ErrValidation)
	}
	if err := guardHostAllow(parsed.Hostname(), s.allowUnsafe); err != nil {
		return "", "", err
	}
	webBase = "https://" + parsed.Host
	if strings.EqualFold(parsed.Hostname(), "github.com") {
		return defaultBaseURL, defaultAPIBaseURL, nil
	}
	return webBase, webBase + "/api/v3", nil
}

// Callback finishes the manifest flow: it redeems state once, exchanges code
// against the host bound to the state, and stores the sealed credentials.
// The secrets never appear in the returned app.
func (s *Service) Callback(ctx context.Context, userID uuid.UUID, code, state string) (GitHubApp, error) {
	if s.repo == nil {
		return GitHubApp{}, ErrNotFound
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return GitHubApp{}, fmt.Errorf("%w: code and state are required", ErrValidation)
	}
	entry, ok := s.states.redeem(state, userID, uuid.Nil, stateManifest)
	if !ok {
		return GitHubApp{}, fmt.Errorf("%w: manifest state", ErrExpiredState)
	}
	api := s.newAPI(entry.apiBaseURL)
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
	app, err := s.repo.CreateApp(ctx, GitHubApp{
		UserID:     userID,
		AppID:      conv.ID,
		Slug:       conv.Slug,
		Name:       conv.Name,
		BaseURL:    entry.baseURL,
		APIBaseURL: entry.apiBaseURL,
		ClientID:   conv.ClientID,
	}, sealedSecret, sealedKey)
	if err != nil {
		return GitHubApp{}, err
	}
	return s.repo.GetApp(ctx, app.ID, userID)
}

// StateOwner returns the user a pending manifest state was issued to, without
// consuming it. It lets the public browser callback resolve the caller from
// the state alone; the callback itself redeems the state exactly once.
func (s *Service) StateOwner(state string) (uuid.UUID, bool) {
	if s == nil || s.states == nil {
		return uuid.Nil, false
	}
	return s.states.peek(state, stateManifest)
}

// InstallStateOwner returns the user and app a pending install state was
// issued for, without consuming it. The setup landing resolves the app from
// the state GitHub returns instead of guessing, so an installation lands on
// the right app even with several pending.
func (s *Service) InstallStateOwner(state string) (userID, appID uuid.UUID, ok bool) {
	if s == nil || s.states == nil {
		return uuid.Nil, uuid.Nil, false
	}
	return s.states.peekApp(state)
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
	state, err := s.states.new(userID, appID, stateInstall, "", "")
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
// cache, so the wizard lists repositories immediately. The installation is
// verified with GitHub first (it must belong to this app), so a recorded id
// is proof, not a claim: user B cannot take over user A's installation by
// replaying its integer id.
func (s *Service) RecordInstallation(ctx context.Context, userID, appID uuid.UUID, installationID int64, state string) (Installation, error) {
	if s.repo == nil {
		return Installation{}, ErrNotFound
	}
	if installationID <= 0 {
		return Installation{}, fmt.Errorf("%w: installation id is required", ErrValidation)
	}
	if _, ok := s.states.redeem(state, userID, appID, stateInstall); !ok {
		return Installation{}, fmt.Errorf("%w: install state", ErrExpiredState)
	}
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return Installation{}, err
	}
	jwt, err := s.appJWT(ctx, app)
	if err != nil {
		return Installation{}, err
	}
	info, err := s.newAPI(app.APIBaseURL).GetInstallation(ctx, installationID, jwt)
	if err != nil {
		return Installation{}, err
	}
	// The installation must belong to this app, unconditionally: a response
	// without an app id proves nothing and is refused fail-closed.
	if info.AppID != app.AppID {
		return Installation{}, fmt.Errorf("%w: installation belongs to another app", ErrValidation)
	}
	inst, err := s.repo.UpsertInstallation(ctx, appID, installationID, info.Account)
	if err != nil {
		return Installation{}, err
	}
	if _, _, err := s.refreshRepos(ctx, userID, appID, installationID); err != nil {
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

// ListRepos returns the repositories of all installations merged and sorted,
// refreshing each cache through its installation token. The second return
// reports truncation: an installation whose page walk hit the bound. A
// refresh failure falls back to that installation's cache.
func (s *Service) ListRepos(ctx context.Context, userID, appID uuid.UUID) ([]Repo, bool, error) {
	if s.repo == nil {
		return nil, false, ErrNotFound
	}
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return nil, false, err
	}
	if len(app.Installations) == 0 {
		return nil, false, fmt.Errorf("%w: app is not installed yet", ErrValidation)
	}
	seen := make(map[string]bool)
	merged := make([]Repo, 0)
	truncated := false
	var failed error
	for _, inst := range app.Installations {
		repos, trunc, err := s.refreshRepos(ctx, userID, appID, inst.InstallationID)
		if err != nil {
			// A refresh failure falls back to the cache: the listing stays
			// available when GitHub is briefly unreachable.
			s.logger.Warn("githubapp: repo refresh failed, serving cache", "app", app.AppID, "error", err)
			failed = err
			cached, cerr := s.repo.ListRepoCache(ctx, appID, inst.InstallationID)
			if cerr != nil {
				continue
			}
			repos = cached
		}
		truncated = truncated || trunc
		for _, r := range repos {
			key := strings.ToLower(strings.TrimSpace(r.FullName))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, r)
		}
	}
	if len(merged) == 0 && failed != nil {
		return nil, false, failed
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].FullName < merged[j].FullName })
	return merged, truncated, nil
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
	jwt, err := s.appJWTFromSealed(sealed)
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

// appJWT signs a fresh app JWT for the FAN-out of installation API calls.
// The private key is opened only for signing and never leaves this function.
func (s *Service) appJWT(ctx context.Context, app GitHubApp) (string, error) {
	sealed, err := s.sealed(ctx, app)
	if err != nil {
		return "", err
	}
	return s.appJWTFromSealed(sealed)
}

func (s *Service) appJWTFromSealed(sealed SealedApp) (string, error) {
	key, err := providers.OpenSecret(s.secret, sealed.PrivateKey)
	if err != nil {
		return "", err
	}
	return signAppJWT(sealed.AppID, []byte(key), time.Now())
}

// TokenCloneURL mints a fresh installation token for the installation of the
// linked connection (appID) granting repo and embeds it in cloneURL. The
// cloner calls it only for applications explicitly linked to a GitHub App
// connection, passing that link: resolution never leaves the linked
// connection, so a deploy can neither borrow another connection's grant nor
// be broken by an unrelated dead installation.
//
// The stored clone URL must live on the app's own host (github.com or the
// connected Enterprise origin) over https, so a token can never be embedded
// in an arbitrary or plaintext URL. The token authenticates one clone
// attempt: it is never persisted and never logged.
func (s *Service) TokenCloneURL(ctx context.Context, userID, appID uuid.UUID, repo, cloneURL string) (string, error) {
	if s.repo == nil {
		return "", ErrNotFound
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", fmt.Errorf("%w: repository is required", ErrValidation)
	}
	if appID == uuid.Nil {
		return "", fmt.Errorf("%w: github app connection is required", ErrValidation)
	}
	// A foreign id answers not-found, like any other foreign resource.
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return "", err
	}
	// Refused before any token is minted and before any grant is listed.
	if err := checkCloneHost(app, cloneURL); err != nil {
		return "", err
	}
	installation, err := s.installationGrantingRepo(ctx, app, repo)
	if err != nil {
		return "", err
	}
	jwt, err := s.appJWT(ctx, app)
	if err != nil {
		return "", err
	}
	token, err := s.installationToken(ctx, app, installation, jwt)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("%w: github app installation token is empty", ErrValidation)
	}
	return embedToken(cloneURL, token)
}

// installationGrantingRepo finds the installation of app whose repos grant
// repo (case-insensitive), preferring the lowest installation id for
// determinism. Only the linked connection is consulted: when it no longer
// grants the repo the deploy fails instead of borrowing another connection.
//
// Every installation is listed fresh from the API: a refresh or listing
// failure on any installation fails the lookup instead of degrading to the
// sentinel. The sentinel is returned only when every installation listed
// successfully and none grants the repo. Anything else (transient GitHub
// failure, unreadable cache data) fails the deploy with a clear error rather
// than a silent anonymous clone.
func (s *Service) installationGrantingRepo(ctx context.Context, app GitHubApp, repo string) (Installation, error) {
	sealed, err := s.sealed(ctx, app)
	if err != nil {
		return Installation{}, fmt.Errorf("%w: %v", ErrGrantsUnverifiable, err)
	}
	insts, err := s.repo.ListInstallations(ctx, app.ID)
	if err != nil {
		return Installation{}, fmt.Errorf("%w: %v", ErrGrantsUnverifiable, err)
	}
	var verifyErr error
	for _, inst := range insts {
		repos, _, err := s.refreshReposFor(ctx, sealed, inst.InstallationID)
		if err != nil {
			verifyErr = errors.Join(verifyErr, err)
			continue
		}
		for _, r := range repos {
			if strings.EqualFold(strings.TrimSpace(r.FullName), repo) {
				return inst, nil
			}
		}
	}
	if verifyErr != nil {
		return Installation{}, fmt.Errorf("%w: %v", ErrGrantsUnverifiable, verifyErr)
	}
	return Installation{}, fmt.Errorf("%w: %q", ErrNoInstallationGrant, repo)
}

// checkCloneHost requires the stored clone URL to live on the app's own git
// host over https: github.com or the connected Enterprise origin. Anything
// else is refused before a token is minted, so a token can never be embedded
// in an arbitrary URL or travel over plaintext http.
func checkCloneHost(app GitHubApp, cloneURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(cloneURL))
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("%w: clone url is not a URL", ErrValidation)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("%w: installation token clone needs an https url", ErrValidation)
	}
	base, err := url.Parse(app.BaseURL)
	if err != nil || base.Hostname() == "" {
		return fmt.Errorf("%w: app base url is not a URL", ErrValidation)
	}
	if !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return fmt.Errorf("%w: clone url host does not match the connected app host", ErrValidation)
	}
	return nil
}

// embedToken embeds a short-lived installation token in an http(s) clone URL.
// Logging and git stderr stay clean through the cloner's redaction.
func embedToken(raw, token string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: clone url is not a URL", ErrValidation)
	}
	parsed.User = url.UserPassword("x-access-token", token)
	return parsed.String(), nil
}

// AppPushTarget is one github_app application watching a repository: what a
// verified app push delivery may build.
type AppPushTarget struct {
	ApplicationID uuid.UUID
	Branch        string
}

// PushTargets returns the github_app applications of the app's owner watching
// repo ("owner/name"), so a verified push delivery maps to deployments.
func (s *Service) PushTargets(ctx context.Context, appID, userID uuid.UUID, repo string) ([]AppPushTarget, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	return s.repo.PushTargets(ctx, appID, userID, strings.ToLower(strings.TrimSpace(repo)))
}

// DisconnectResult reports a disconnect: how many linked applications lose
// their connection, and their names for the confirmation.
type DisconnectResult struct {
	ApplicationsUsing int64
	Applications      []string
}

// Disconnect deletes the stored credentials. It names the caller's
// applications linked to this connection (by github_app_id, the same link
// cloning and push routing consult), so the UI can warn before deleting.
// Deleting the connection unlinks those applications instead of deleting
// them; their next deploy clones without an installation token.
func (s *Service) Disconnect(ctx context.Context, userID, appID uuid.UUID) (DisconnectResult, error) {
	if s.repo == nil {
		return DisconnectResult{}, ErrNotFound
	}
	if _, err := s.repo.GetApp(ctx, appID, userID); err != nil {
		return DisconnectResult{}, err
	}
	names, err := s.repo.ListApplicationNamesForApp(ctx, userID, appID)
	if err != nil {
		return DisconnectResult{}, err
	}
	if err := s.repo.DeleteApp(ctx, appID, userID); err != nil {
		return DisconnectResult{}, err
	}
	s.mu.Lock()
	for id, cached := range s.tokens {
		if cached.appID == appID {
			delete(s.tokens, id)
		}
	}
	s.mu.Unlock()
	return DisconnectResult{ApplicationsUsing: int64(len(names)), Applications: names}, nil
}

// installationToken mints (or reuses a cached) installation token.
// Concurrent minting for one installation collapses onto one flight.
func (s *Service) installationToken(ctx context.Context, app GitHubApp, installation Installation, jwt string) (string, error) {
	s.mu.Lock()
	cached, ok := s.tokens[installation.ID]
	s.mu.Unlock()
	if ok && time.Now().Add(tokenRefreshMargin).Before(cached.expiresAt) {
		return cached.token, nil
	}
	key := app.ID.String() + "/" + installation.ID.String()
	token, err, _ := s.tokensFlight.Do(key, func() (any, error) {
		s.mu.Lock()
		recached, ok := s.tokens[installation.ID]
		s.mu.Unlock()
		if ok && time.Now().Add(tokenRefreshMargin).Before(recached.expiresAt) {
			return recached.token, nil
		}
		tok, err := s.newAPI(app.APIBaseURL).CreateInstallationToken(ctx, installation.InstallationID, jwt)
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		s.tokens[installation.ID] = cachedToken{appID: app.ID, token: tok.Token, expiresAt: tok.ExpiresAt}
		s.mu.Unlock()
		return tok.Token, nil
	})
	if err != nil {
		return "", err
	}
	return token.(string), nil
}

// refreshRepos lists through the installation token and rewrites the cache.
func (s *Service) refreshRepos(ctx context.Context, userID, appID uuid.UUID, installationID int64) ([]Repo, bool, error) {
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return nil, false, err
	}
	sealed, err := s.sealed(ctx, app)
	if err != nil {
		return nil, false, err
	}
	return s.refreshReposFor(ctx, sealed, installationID)
}

// sealed loads the app row with its sealed secrets for signing.
func (s *Service) sealed(ctx context.Context, app GitHubApp) (SealedApp, error) {
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

// baseURLFor and apiBaseURL derivation moved to normalizeGitHubURLs, bound to
// the manifest state: the callback exchanges the code against the same host
// the manifest was started for (github.com or Enterprise).

// appEvent is the subset of installation deliveries this package acts on.
// AppID disambiguates the app when a delivery names it (payload app id or
// the hook-installation-target header); without it the installation id alone
// selects candidates.
type appEvent struct {
	Action       string `json:"action"`
	Installation *struct {
		ID      int64 `json:"id"`
		Account *struct {
			Login string `json:"login"`
		} `json:"account"`
	} `json:"installation"`
}

// installationTargetID reads who a delivery is for: the installation id
// always comes from payload.installation.id. The
// X-GitHub-Hook-Installation-Target-ID header names the APP id for app
// webhooks (target type "integration"), never the installation, so it is only
// a cross-check hint that disambiguates candidates.
func installationTargetID(header http.Header, body []byte) (installationID, appHint int64) {
	if id, err := strconv.ParseInt(strings.TrimSpace(header.Get("X-GitHub-Hook-Installation-Target-ID")), 10, 64); err == nil && id > 0 {
		appHint = id
	}
	var event appEvent
	if err := json.Unmarshal(body, &event); err != nil || event.Installation == nil {
		return 0, appHint
	}
	return event.Installation.ID, appHint
}

// VerifyDelivery verifies body against the app secret of the installation the
// delivery targets and returns that app's ID. Only the owning app's secret is
// tried and only that app is ever mutated: a delivery signed with another
// app's secret never touches this app's installations, so a cross-user
// installation id replay fails closed.
func (s *Service) VerifyDelivery(header http.Header, body []byte) (uuid.UUID, bool) {
	if s.repo == nil {
		return uuid.Nil, false
	}
	provided := header.Get("X-Hub-Signature-256")
	if !strings.HasPrefix(provided, "sha256=") {
		return uuid.Nil, false
	}
	provided = strings.TrimPrefix(provided, "sha256=")
	installationID, appHint := installationTargetID(header, body)
	if installationID <= 0 {
		return uuid.Nil, false
	}
	ctx := context.Background()
	apps, err := s.repo.AppsByInstallationID(ctx, installationID)
	if err != nil || len(apps) == 0 {
		return uuid.Nil, false
	}
	for _, app := range apps {
		// The header hint disambiguates when several rows share the
		// installation id: only the hinted app's secret is tried.
		if appHint > 0 && app.AppID != appHint {
			continue
		}
		secret, err := providers.OpenSecret(s.secret, app.WebhookSecret)
		if err != nil || secret == "" {
			continue
		}
		if verifyAppSignature(provided, secret, body) {
			return app.ID, true
		}
	}
	return uuid.Nil, false
}

// verifyAppSignature reports whether the hex HMAC-SHA256 digest authenticates
// body. The comparison is constant time.
func verifyAppSignature(provided, secret string, body []byte) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal([]byte(provided), []byte(hex.EncodeToString(mac.Sum(nil))))
}

// VerifyPush verifies a push (or pull request) body the same way:
// app-signed deliveries carry the installation id, and the match returns the
// owning app. It backs the webhook push fallback that triggers deploys for
// github_app applications.
func (s *Service) VerifyPush(header http.Header, body []byte) (uuid.UUID, bool) {
	return s.VerifyDelivery(header, body)
}

// PushTargetsForWebhook lists the github_app applications watching repo for
// the app's owner, resolving the owner from the app row (webhook context has
// no user session).
func (s *Service) PushTargetsForWebhook(ctx context.Context, appID uuid.UUID, repo string) ([]AppPushTarget, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	sealed, err := s.repo.GetSealedByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.repo.PushTargets(ctx, appID, sealed.UserID, strings.ToLower(strings.TrimSpace(repo)))
}

// HandleAppEvent refreshes the repo cache for installation and
// installation_repositories deliveries of the verified app. appID must come
// from VerifyDelivery: only installations owned by that same app are mutated,
// so one app's delivery can never delete or refresh another app's rows.
func (s *Service) HandleAppEvent(ctx context.Context, appID uuid.UUID, event string, body []byte) error {
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
	owned, err := s.ownsInstallation(ctx, appID, parsed.Installation.ID)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("%w: installation is not owned by this app", ErrValidation)
	}
	sealed, err := s.repo.GetSealedByID(ctx, appID)
	if err != nil {
		return err
	}
	account := ""
	if parsed.Installation.Account != nil {
		account = parsed.Installation.Account.Login
	}
	if strings.ToLower(strings.TrimSpace(parsed.Action)) == "deleted" && event == "installation" {
		return s.repo.DeleteInstallation(ctx, parsed.Installation.ID, appID)
	}
	if _, err := s.repo.UpsertInstallation(ctx, appID, parsed.Installation.ID, account); err != nil {
		return err
	}
	if _, _, err := s.refreshReposFor(ctx, sealed, parsed.Installation.ID); err != nil {
		s.logger.Warn("githubapp: app event refresh failed",
			"app", sealed.AppID, "installation", parsed.Installation.ID, "error", err)
	}
	return nil
}

// ownsInstallation reports whether installationID is recorded under appID.
func (s *Service) ownsInstallation(ctx context.Context, appID uuid.UUID, installationID int64) (bool, error) {
	insts, err := s.repo.ListInstallations(ctx, appID)
	if err != nil {
		return false, err
	}
	for _, inst := range insts {
		if inst.InstallationID == installationID {
			return true, nil
		}
	}
	return false, nil
}

// refreshReposFor refreshes one installation when the sealed app row (with
// secrets) is already in hand.
func (s *Service) refreshReposFor(ctx context.Context, app SealedApp, installationID int64) ([]Repo, bool, error) {
	jwt, err := s.appJWTFromSealed(app)
	if err != nil {
		return nil, false, err
	}
	insts, err := s.repo.ListInstallations(ctx, app.ID)
	if err != nil {
		return nil, false, err
	}
	var installation Installation
	for _, inst := range insts {
		if inst.InstallationID == installationID {
			installation = inst
		}
	}
	if installation.ID == uuid.Nil {
		return nil, false, fmt.Errorf("%w: unknown installation", ErrValidation)
	}
	token, err := s.installationToken(ctx, app.GitHubApp, installation, jwt)
	if err != nil {
		return nil, false, err
	}
	repos, truncated, err := s.newAPI(app.APIBaseURL).ListInstallationRepos(ctx, token)
	if err != nil {
		return nil, false, err
	}
	if err := s.repo.ReplaceRepoCache(ctx, app.ID, installationID, repos); err != nil {
		return nil, false, err
	}
	return repos, truncated, nil
}
