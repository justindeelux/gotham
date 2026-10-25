package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/clientip"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
)

const maxBodyBytes = 1 << 20

const (
	StatusQueued    = "queued"
	StatusDuplicate = "duplicate"
	StatusIgnored   = "ignored"
	StatusDeleted   = "deleted"
)

type Installer interface {
	CreateWebhook(ctx context.Context, target providers.HookTarget, hook providers.Webhook) (string, error)
	DeleteWebhook(ctx context.Context, target providers.HookTarget, hookID string) error
}

type Deployer interface {
	DeploySystem(ctx context.Context, appID uuid.UUID) (deploy.Deployment, error)
}

type AppEventHandler interface {
	VerifyDelivery(header http.Header, body []byte) (uuid.UUID, bool)
	HandleAppEvent(ctx context.Context, appID uuid.UUID, event string, body []byte) error
}

type AppPushTarget struct {
	ApplicationID uuid.UUID
	Branch        string
}

type AppPushHandler interface {
	VerifyPush(header http.Header, body []byte) (uuid.UUID, bool)
	PushTargets(ctx context.Context, appID uuid.UUID, repo string) ([]AppPushTarget, error)
}

type Delivery struct {
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	DeploymentID string `json:"deployment_id,omitempty"`
	Host         string `json:"host,omitempty"`
}

type Config struct {
	Store           *store.Store
	Repository      Repository
	Installer       Installer
	Deployer        Deployer
	Provisioner     PreviewProvisioner
	Commenter       Commenter
	Secret          string
	AppEvents       AppEventHandler
	AppPush         AppPushHandler
	Logger          *slog.Logger
	Limit           rate.Limit
	Burst           int
	TrustedProxies  []netip.Prefix
	ControlPlaneURL func(ctx context.Context) string
	Now             func() time.Time
	SweepInterval   time.Duration
}

type Service struct {
	repo            Repository
	installer       Installer
	deployer        Deployer
	provisioner     PreviewProvisioner
	commenter       Commenter
	appEvents       AppEventHandler
	appPush         AppPushHandler
	logger          *slog.Logger
	limiter         *deliveryLimiter
	trusted         []netip.Prefix
	controlPlaneURL func(ctx context.Context) string
	now             func() time.Time
	sweepEvery      time.Duration

	sweepOnce sync.Once
	sweepStop context.CancelFunc
	sweepDone chan struct{}
	closeOnce sync.Once
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	repo := cfg.Repository
	if repo == nil && cfg.Store != nil {
		repo = newStoreRepository(cfg.Store, cfg.Secret, logger)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	interval := cfg.SweepInterval
	if interval <= 0 {
		interval = defaultPreviewSweepInterval
	}
	return &Service{
		repo:            repo,
		installer:       cfg.Installer,
		deployer:        cfg.Deployer,
		provisioner:     cfg.Provisioner,
		commenter:       cfg.Commenter,
		appEvents:       cfg.AppEvents,
		appPush:         cfg.AppPush,
		logger:          logger,
		limiter:         newDeliveryLimiter(cfg.Limit, cfg.Burst),
		trusted:         cfg.TrustedProxies,
		controlPlaneURL: cfg.ControlPlaneURL,
		now:             now,
		sweepEvery:      interval,
	}
}

func NewDefaultService(cfg Config) *Service {
	if cfg.Repository == nil && cfg.Store == nil {
		return nil
	}
	if cfg.Installer == nil || cfg.Deployer == nil {
		return nil
	}
	return NewService(cfg)
}

func (s *Service) CreateWebhook(ctx context.Context, userID, appID uuid.UUID, callbackBase string) (Hook, error) {
	if s == nil || s.repo == nil || s.installer == nil {
		return Hook{}, errors.New("webhooks: service is not configured")
	}
	if err := validateCallbackURL(callbackBase); err != nil {
		return Hook{}, err
	}

	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Hook{}, err
	}
	if !isSupported(app.Provider) {
		return Hook{}, fmt.Errorf("%w: application provider %q has no webhook support", ErrValidation, app.Provider)
	}
	if strings.TrimSpace(app.Repo) == "" {
		return Hook{}, fmt.Errorf("%w: application has no repository", ErrValidation)
	}
	callbackURL := strings.TrimRight(callbackBase, "/") + "/" + app.Provider

	existing, err := s.repo.GetWebhook(ctx, appID)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Hook{}, err
	}

	secret, err := newHookSecret()
	if err != nil {
		return Hook{}, err
	}
	hookID, err := s.installer.CreateWebhook(ctx, providers.HookTarget{
		UserID:   app.UserID,
		Provider: app.Provider,
		CloneURL: app.CloneURL,
		Repo:     app.Repo,
	}, providers.Webhook{
		URL:    callbackURL,
		Secret: secret,
		Events: s.hookEvents(),
	})
	if err != nil {
		return Hook{}, mapProviderError(app.Provider, err)
	}

	hook, err := s.repo.CreateWebhook(ctx, Hook{
		ApplicationID: appID,
		Provider:      app.Provider,
		Repo:          app.Repo,
		HookID:        hookID,
		URL:           callbackURL,
	}, secret)
	if err != nil {
		s.bestEffortDelete(ctx, app, hookID)
		return Hook{}, err
	}
	return hook, nil
}

func (s *Service) DeleteWebhook(ctx context.Context, userID, appID uuid.UUID) (bool, error) {
	return s.deleteWebhook(ctx, userID, appID, false)
}

func (s *Service) ForgetWebhook(ctx context.Context, userID, appID uuid.UUID) (bool, error) {
	return s.deleteWebhook(ctx, userID, appID, true)
}

func (s *Service) deleteWebhook(ctx context.Context, userID, appID uuid.UUID, force bool) (bool, error) {
	if s == nil || s.repo == nil || s.installer == nil {
		return false, errors.New("webhooks: service is not configured")
	}

	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return false, err
	}
	hook, err := s.repo.GetWebhook(ctx, appID)
	switch {
	case errors.Is(err, ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}

	if force {
		if hook.HookID != "" {
			s.logger.Warn("webhooks: forgetting the stored hook without contacting the Git host; remove it there by hand",
				"application_id", appID, "provider", app.Provider, "repo", app.Repo,
				"hook_id", hook.HookID)
		}
	} else if hook.HookID != "" {
		if err := s.installer.DeleteWebhook(ctx, providers.HookTarget{
			UserID:   app.UserID,
			Provider: app.Provider,
			CloneURL: app.CloneURL,
			Repo:     app.Repo,
		}, hook.HookID); err != nil {
			return false, mapProviderError(app.Provider, err)
		}
	}
	if _, err := s.repo.DeleteWebhook(ctx, appID); err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	return true, nil
}

func (s *Service) InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) error {
	_, err := s.CreateWebhook(ctx, userID, appID, s.callbackBaseURL(r))
	return err
}

func (s *Service) RemoveHook(ctx context.Context, userID, appID uuid.UUID) error {
	_, err := s.DeleteWebhook(ctx, userID, appID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotConnected):
		return fmt.Errorf("%w: %v", deploy.ErrNotConnected, err)
	case errors.Is(err, ErrProvider):
		return fmt.Errorf("%w: %v", deploy.ErrProvider, err)
	default:
		return err
	}
}

var _ deploy.HookLifecycle = (*Service)(nil)

func (s *Service) application(ctx context.Context, _ uuid.UUID, appID uuid.UUID, _ bool) (Application, error) {
	if appID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: application id is required", ErrValidation)
	}
	app, err := s.repo.GetApplication(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	return app, nil
}

func (s *Service) Receive(ctx context.Context, provider string, r *http.Request) (Delivery, error) {
	if s == nil || s.repo == nil || s.deployer == nil {
		return Delivery{}, errors.New("webhooks: service is not configured")
	}
	if !isSupported(provider) {
		return Delivery{}, fmt.Errorf("%w: %s", ErrNotFound, provider)
	}
	if !s.limiter.allow(clientip.ClientIP(r, s.trusted)) {
		return Delivery{}, ErrRateLimited
	}

	body, err := readBody(r)
	if err != nil {
		return Delivery{}, err
	}
	if appEvent, ok := appDeliveryEvent(provider, r.Header); ok {
		if s.appEvents == nil {
			return Delivery{}, ErrUnauthorized
		}
		appID, ok := s.appEvents.VerifyDelivery(r.Header, body)
		if !ok {
			return Delivery{}, ErrUnauthorized
		}
		if err := s.appEvents.HandleAppEvent(ctx, appID, appEvent, body); err != nil {
			return Delivery{}, err
		}
		return Delivery{Status: StatusIgnored, Reason: "event"}, nil
	}
	parsed, err := parseDelivery(provider, r.Header, body)
	if err != nil {
		return Delivery{}, err
	}

	targets, err := s.repo.Targets(ctx, provider, parsed.Repository)
	if err != nil {
		return Delivery{}, err
	}
	target, ok := authorizedTarget(provider, r.Header, body, targets)
	if !ok {
		appTarget, ok := s.appPushTarget(ctx, provider, r.Header, body, parsed)
		if !ok {
			return Delivery{}, ErrUnauthorized
		}
		target = appTarget
	}

	return s.deliverToTarget(ctx, provider, parsed, target)
}

func (s *Service) deliverToTarget(ctx context.Context, provider string, parsed signedDelivery, target Target) (Delivery, error) {
	if isPullRequestEvent(provider, parsed.Event) {
		return s.receivePullRequest(ctx, provider, target, parsed)
	}
	if !isPushEvent(provider, parsed.Event) {
		return Delivery{Status: StatusIgnored, Reason: "event"}, nil
	}
	if parsed.Deleted {
		return Delivery{Status: StatusIgnored, Reason: "ref deleted"}, nil
	}
	branch := branchOf(parsed.Ref)
	if branch == "" {
		return Delivery{Status: StatusIgnored, Reason: "ref"}, nil
	}
	if branch != target.Branch {
		return Delivery{Status: StatusIgnored, Reason: "branch"}, nil
	}

	event, err := s.repo.ClaimEvent(ctx, Event{
		ApplicationID: target.ApplicationID,
		Provider:      provider,
		Event:         parsed.Event,
		DeliveryID:    parsed.DeliveryID,
		Ref:           parsed.Ref,
		CommitSHA:     parsed.Commit,
	})
	switch {
	case errors.Is(err, ErrDuplicate):
		if event.DeploymentID != uuid.Nil {
			return Delivery{Status: StatusDuplicate, Reason: "commit already handled"}, nil
		}
		return Delivery{}, fmt.Errorf("%w: the delivery is still being handled", ErrRetryable)
	case err != nil:
		return Delivery{}, err
	}

	deployment, err := s.deployer.DeploySystem(ctx, target.ApplicationID)
	if err != nil {
		s.releaseClaim(ctx, event)
		if errors.Is(err, deploy.ErrConflict) {
			return Delivery{}, fmt.Errorf("%w: a deployment is already running", ErrRetryable)
		}
		return Delivery{}, err
	}
	s.linkClaim(ctx, event, deployment)
	return Delivery{Status: StatusQueued, DeploymentID: deployment.ID.String()}, nil
}

func (s *Service) appPushTarget(ctx context.Context, provider string, header http.Header, body []byte, parsed signedDelivery) (Target, bool) {
	if provider != providers.NameGitHub || s.appPush == nil {
		return Target{}, false
	}
	if !isPushEvent(provider, parsed.Event) || parsed.Deleted {
		return Target{}, false
	}
	branch := branchOf(parsed.Ref)
	if branch == "" || parsed.Repository == "" {
		return Target{}, false
	}
	appID, ok := s.appPush.VerifyPush(header, body)
	if !ok {
		return Target{}, false
	}
	targets, err := s.appPush.PushTargets(ctx, appID, parsed.Repository)
	if err != nil || len(targets) == 0 {
		return Target{}, false
	}
	for _, candidate := range targets {
		if candidate.Branch == branch {
			return Target{ApplicationID: candidate.ApplicationID, Branch: candidate.Branch}, true
		}
	}
	first := targets[0]
	return Target{ApplicationID: first.ApplicationID, Branch: first.Branch}, true
}

func appDeliveryEvent(provider string, header http.Header) (string, bool) {
	if provider != providers.NameGitHub {
		return "", false
	}
	event := strings.ToLower(strings.TrimSpace(header.Get(headerGitHubEvent)))
	switch event {
	case "installation", "installation_repositories":
		return event, true
	default:
		return "", false
	}
}

func authorizedTarget(provider string, header http.Header, body []byte, targets []Target) (Target, bool) {
	for _, target := range targets {
		if verifySignature(provider, header, body, target.Secret) {
			return target, true
		}
	}
	return Target{}, false
}

const claimCleanupTimeout = 3 * time.Second

func (s *Service) releaseClaim(ctx context.Context, event Event) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimCleanupTimeout)
	defer cancel()
	if err := s.repo.ReleaseEvent(cleanupCtx, event.ID); err != nil {
		s.logger.Warn("webhooks: could not release delivery claim", "event_id", event.ID, "error", err)
	}
}

func (s *Service) linkClaim(ctx context.Context, event Event, deployment deploy.Deployment) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimCleanupTimeout)
	defer cancel()
	if err := s.repo.LinkEventDeployment(cleanupCtx, event.ID, deployment.ID); err != nil {
		s.logger.Warn("webhooks: could not link delivery to deployment",
			"event_id", event.ID, "deployment_id", deployment.ID, "error", err)
	}
}

const hookRollbackTimeout = 3 * time.Second

func (s *Service) bestEffortDelete(ctx context.Context, app Application, hookID string) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookRollbackTimeout)
	defer cancel()
	err := s.installer.DeleteWebhook(rollbackCtx, providers.HookTarget{
		UserID:   app.UserID,
		Provider: app.Provider,
		CloneURL: app.CloneURL,
		Repo:     app.Repo,
	}, hookID)
	if err != nil {
		s.logger.Warn("webhooks: could not roll back hook installation",
			"application_id", app.ID, "hook_id", hookID, "error", err)
	}
}

func mapProviderError(provider string, err error) error {
	switch {
	case errors.Is(err, providers.ErrNotConnected), errors.Is(err, providers.ErrNotFound):
		return fmt.Errorf("%w: %s", ErrNotConnected, provider)
	case errors.Is(err, providers.ErrValidation):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return fmt.Errorf("%w: %s: %v", ErrProvider, provider, err)
	}
}

func (s *Service) hookEvents() []string {
	if !Enabled() {
		return []string{"push"}
	}
	return []string{"push", "pull_request"}
}

func isSupported(provider string) bool {
	switch provider {
	case providers.NameGitHub, providers.NameGitLab, providers.NameGitea:
		return true
	default:
		return false
	}
}

func validateCallbackURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%w: invalid webhook url", ErrValidation)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: webhook url must be http or https", ErrValidation)
	}
	return nil
}

func newHookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("webhooks: generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
