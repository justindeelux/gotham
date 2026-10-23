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

const (
	defaultBaseURL    = "https://github.com"
	defaultAPIBaseURL = "https://api.github.com"
)

const tokenRefreshMargin = time.Minute

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

type Installation struct {
	ID             uuid.UUID
	InstallationID int64
	Account        string
}

type Repository interface {
	CreateApp(ctx context.Context, app GitHubApp, webhookSecret, privateKey string) (GitHubApp, error)
	GetApp(ctx context.Context, id, userID uuid.UUID) (GitHubApp, error)
	GetSealed(ctx context.Context, id, userID uuid.UUID) (SealedApp, error)
	GetSealedByID(ctx context.Context, id uuid.UUID) (SealedApp, error)
	ListApps(ctx context.Context, userID uuid.UUID) ([]GitHubApp, error)
	DeleteApp(ctx context.Context, id, userID uuid.UUID) error
	UpsertInstallation(ctx context.Context, appID uuid.UUID, installationID int64, account string) (Installation, error)
	ListInstallations(ctx context.Context, appID uuid.UUID) ([]Installation, error)
	DeleteInstallation(ctx context.Context, installationID int64, appID uuid.UUID) error
	AppsByInstallationID(ctx context.Context, installationID int64) ([]SealedApp, error)
	ReplaceRepoCache(ctx context.Context, appID uuid.UUID, installationID int64, repos []Repo) error
	ListRepoCache(ctx context.Context, appID uuid.UUID, installationID int64) ([]Repo, error)
	ListApplicationNamesForApp(ctx context.Context, userID, appID uuid.UUID) ([]string, error)
	PushTargets(ctx context.Context, appID, userID uuid.UUID, repo string) ([]AppPushTarget, error)
}

type SealedApp struct {
	GitHubApp
	WebhookSecret string
	PrivateKey    string
}

type Service struct {
	repo        Repository
	newAPI      func(apiBase string) GitHubAPI
	secret      string
	logger      *slog.Logger
	states      *stateStore
	allowUnsafe bool

	mu           sync.Mutex
	tokens       map[uuid.UUID]cachedToken
	tokensFlight singleflight.Group
}

type cachedToken struct {
	appID     uuid.UUID
	token     string
	expiresAt time.Time
}

type Config struct {
	Repository         Repository
	NewAPI             func(apiBase string) GitHubAPI
	Secret             string
	Logger             *slog.Logger
	AllowUnsafeBaseURL bool
}

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

func randomSecret() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "githubapp-ephemeral"
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

type Manifest struct {
	ActionURL string         `json:"action_url"`
	Manifest  map[string]any `json:"manifest"`
	State     string         `json:"state"`
}

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
		"default_events": []string{"push"},
	}
	return Manifest{
		ActionURL: strings.TrimRight(webBase, "/") + "/settings/apps/new?state=" + state,
		Manifest:  manifest,
		State:     state,
	}, nil
}

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

func (s *Service) StateOwner(state string) (uuid.UUID, bool) {
	if s == nil || s.states == nil {
		return uuid.Nil, false
	}
	return s.states.peek(state, stateManifest)
}

func (s *Service) InstallStateOwner(state string) (userID, appID uuid.UUID, ok bool) {
	if s == nil || s.states == nil {
		return uuid.Nil, uuid.Nil, false
	}
	return s.states.peekApp(state)
}

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

func (s *Service) ListApps(ctx context.Context, userID uuid.UUID) ([]GitHubApp, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	return s.repo.ListApps(ctx, userID)
}

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
	app, err := s.repo.GetApp(ctx, appID, userID)
	if err != nil {
		return "", err
	}
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

func embedToken(raw, token string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: clone url is not a URL", ErrValidation)
	}
	parsed.User = url.UserPassword("x-access-token", token)
	return parsed.String(), nil
}

type AppPushTarget struct {
	ApplicationID uuid.UUID
	Branch        string
}

func (s *Service) PushTargets(ctx context.Context, appID, userID uuid.UUID, repo string) ([]AppPushTarget, error) {
	if s.repo == nil {
		return nil, ErrNotFound
	}
	return s.repo.PushTargets(ctx, appID, userID, strings.ToLower(strings.TrimSpace(repo)))
}

type DisconnectResult struct {
	ApplicationsUsing int64
	Applications      []string
}

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

func (s *Service) sealed(ctx context.Context, app GitHubApp) (SealedApp, error) {
	return s.repo.GetSealed(ctx, app.ID, app.UserID)
}

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

type appEvent struct {
	Action       string `json:"action"`
	Installation *struct {
		ID      int64 `json:"id"`
		Account *struct {
			Login string `json:"login"`
		} `json:"account"`
	} `json:"installation"`
}

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

func verifyAppSignature(provided, secret string, body []byte) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal([]byte(provided), []byte(hex.EncodeToString(mac.Sum(nil))))
}

func (s *Service) VerifyPush(header http.Header, body []byte) (uuid.UUID, bool) {
	return s.VerifyDelivery(header, body)
}

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
