package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// maxBodyBytes bounds a delivery body. Push notifications are small; anything
// larger is refused before it is parsed.
const maxBodyBytes = 1 << 20 // 1 MiB

// Delivery statuses reported by Receive.
const (
	// StatusQueued means a deployment was started for this delivery.
	StatusQueued = "queued"
	// StatusDuplicate means the commit SHA (or delivery ID) was already
	// handled: an anti-spam no-op, not an error.
	StatusDuplicate = "duplicate"
	// StatusIgnored means the delivery verified but asks for no build (ping,
	// tag push, another branch, deleted ref).
	StatusIgnored = "ignored"
	// StatusSkipped means the delivery verified but the deploy service refused
	// it without failing (a deployment is already running).
	StatusSkipped = "skipped"
	// StatusDeleted means a closed pull request tore its preview down.
	StatusDeleted = "deleted"
)

// Installer is the slice of providers.ProviderService the hook lifecycle
// needs. Declaring it here keeps this package off the provider service's
// repository and credential handling, and lets tests fake the Git host.
type Installer interface {
	// CreateWebhook installs a push hook and returns the provider's hook ID.
	CreateWebhook(ctx context.Context, target providers.HookTarget, hook providers.Webhook) (string, error)
	// DeleteWebhook removes a hook; an already-removed hook is a success.
	DeleteWebhook(ctx context.Context, target providers.HookTarget, hookID string) error
}

// Deployer is the slice of the deploy service a verified delivery triggers.
type Deployer interface {
	// DeploySystem queues a deployment for an application without an
	// authenticated caller — the webhook has already established which
	// application it may build.
	DeploySystem(ctx context.Context, appID uuid.UUID) (deploy.Deployment, error)
}

// Delivery is the outcome of one webhook delivery, returned as the response
// body so the Git host's delivery log shows why a push did or did not build.
type Delivery struct {
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	DeploymentID string `json:"deployment_id,omitempty"`
	// Host is the preview host a pull request delivery created or removed.
	Host string `json:"host,omitempty"`
}

// Config wires a Service. Store (or an explicit Repository) plus Installer and
// Deployer are required for anything beyond tests; Secret seals stored hook
// secrets and Logger defaults to slog.Default. Limit and Burst tune the
// delivery rate limiter and fall back to their defaults when unset.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is
	// set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Installer creates and removes hooks on the Git host.
	Installer Installer
	// Deployer queues deployments for verified deliveries.
	Deployer Deployer
	// Provisioner clones a preview sibling from a base application and tears
	// it down. nil (or FEATURE_PREVIEWS=false) makes every pull request
	// delivery a no-op.
	Provisioner PreviewProvisioner
	// Commenter posts the preview badge comment on the pull request. Best
	// effort; nil disables comments only.
	Commenter Commenter
	// Secret is the key providers.SealSecret seals hook secrets with.
	Secret string
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Limit is the delivery refill rate per client IP, Burst its bucket size.
	Limit rate.Limit
	Burst int
	// Now overrides the clock (tests). Defaults to time.Now.
	Now func() time.Time
	// SweepInterval is the orphan sweep period. Zero selects
	// defaultPreviewSweepInterval. The sweep is orphan-only: it never deletes
	// a preview that is still bound to a live sibling application.
	SweepInterval time.Duration
}

// Service is the webhook domain service: it installs and removes hooks and
// turns verified push deliveries into deployments. It is safe for concurrent
// use.
type Service struct {
	repo        Repository
	installer   Installer
	deployer    Deployer
	provisioner PreviewProvisioner
	commenter   Commenter
	logger      *slog.Logger
	limiter     *deliveryLimiter
	now         func() time.Time
	sweepEvery  time.Duration

	// Sweep lifecycle (StartPreviews/Close).
	sweepOnce sync.Once
	sweepStop context.CancelFunc
	sweepDone chan struct{}
	closeOnce sync.Once
}

// NewService builds a Service from cfg.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	repo := cfg.Repository
	if repo == nil && cfg.Store != nil {
		repo = newStoreRepository(cfg.Store, cfg.Secret)
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
		repo:        repo,
		installer:   cfg.Installer,
		deployer:    cfg.Deployer,
		provisioner: cfg.Provisioner,
		commenter:   cfg.Commenter,
		logger:      logger,
		limiter:     newDeliveryLimiter(cfg.Limit, cfg.Burst),
		now:         now,
		sweepEvery:  interval,
	}
}

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil when any dependency is missing, so the control plane can call
// Mount unconditionally.
func NewDefaultService(cfg Config) *Service {
	if cfg.Repository == nil && cfg.Store == nil {
		return nil
	}
	if cfg.Installer == nil || cfg.Deployer == nil {
		return nil
	}
	return NewService(cfg)
}

// CreateWebhook installs the push hook of an application and stores it. It is
// idempotent: an application that already has a hook gets the same row back,
// with the secret the installed hook was signed with — regenerating it would
// break every delivery the host already sends.
//
// callbackBase is the public origin of the delivery route without the provider
// segment (for example https://cp.example.com/api/v1/webhooks); the provider
// named by the application is appended to it.
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
		// The host now holds a hook this control plane cannot verify. Undo it
		// so a retry starts from a clean state.
		s.bestEffortDelete(ctx, app, hookID)
		return Hook{}, err
	}
	return hook, nil
}

// DeleteWebhook removes the hook of an application, first on the Git host and
// then in the database. It is idempotent: an application with no hook reports
// false and no error. A host that fails for any reason other than "already
// gone" aborts the call so the row and the host stay in step; the caller then
// retries, removes the hook on the host by hand, or acknowledges the orphan
// with ForgetWebhook (DELETE .../webhooks?force=true).
func (s *Service) DeleteWebhook(ctx context.Context, userID, appID uuid.UUID) (bool, error) {
	return s.deleteWebhook(ctx, userID, appID, false)
}

// ForgetWebhook removes the stored hook of an application WITHOUT contacting
// the Git host at all. It is the operator's explicit escape hatch when a
// provider connection is gone or its host stalls and an application must still
// be deletable — the deploy delete fails closed on a hook removal failure (the
// stored row is the only handle on the remote hook), so without this the
// application would be stuck. A blocking provider call is deliberately not
// attempted first: the stall is the reason the hatch exists. The remote hook
// is intentionally left behind; the warning names application, provider, repo
// and hook ID so it can be removed on the host later.
func (s *Service) ForgetWebhook(ctx context.Context, userID, appID uuid.UUID) (bool, error) {
	return s.deleteWebhook(ctx, userID, appID, true)
}

// deleteWebhook is the shared body of DeleteWebhook and ForgetWebhook. With
// force=false a host failure aborts before the row is touched; with force=true
// the host is not called and the stored row goes immediately, which is what
// makes the escape hatch usable while the provider is unreachable.
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
		return false, nil // already detached: deleting twice is a success
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

// InstallHook implements deploy.HookLifecycle: it installs the push hook of an
// application using the public origin of the create request, exactly like the
// explicit POST /v1/applications/{id}/webhooks route (see callbackBaseURL).
// The deploy create path calls it; a non-request caller uses that route.
func (s *Service) InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) error {
	_, err := s.CreateWebhook(ctx, userID, appID, callbackBaseURL(r))
	return err
}

// RemoveHook implements deploy.HookLifecycle: it removes the application's
// hook from the Git host and then its stored row, tolerating an application
// that never had one. The application delete fails closed, so errors are
// translated to the deploy sentinels the delete route maps to a status
// (409 for a missing connection, 502 for a provider failure): the row must
// survive so the hook can still be removed once the provider is reachable.
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

// Compile-time guarantee that Service satisfies the application-lifecycle
// hook seam.
var _ deploy.HookLifecycle = (*Service)(nil)

// application loads an application the caller may access through its active
// team: a row of another team answers ErrNotFound so application IDs cannot be
// probed, and a write needs an owner/admin role (a creator who was demoted to
// read_only or removed from the team is refused). Hook management is a
// mutation of the application's hosting, hence write=true there; the preview
// list is a read.
func (s *Service) application(ctx context.Context, userID, appID uuid.UUID, write bool) (Application, error) {
	if appID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: application id is required", ErrValidation)
	}
	app, err := s.repo.GetApplication(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	if err := teams.ScopeFor(ctx, userID).AuthorizeResource(app.TeamID, app.UserID, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return Application{}, ErrForbidden
		}
		return Application{}, ErrNotFound
	}
	return app, nil
}

// Receive handles one provider delivery: rate-limit it, find the hook it was
// signed for, verify the signature in constant time, drop anything that is
// not a push to the watched branch (or was already handled), and queue a
// deployment for the rest.
//
// The returned Delivery is always a 2xx outcome; an error carries the status
// the route should answer with.
func (s *Service) Receive(ctx context.Context, provider string, r *http.Request) (Delivery, error) {
	if s == nil || s.repo == nil || s.deployer == nil {
		return Delivery{}, errors.New("webhooks: service is not configured")
	}
	if !isSupported(provider) {
		return Delivery{}, fmt.Errorf("%w: %s", ErrNotFound, provider)
	}
	if !s.limiter.allow(clientIP(r)) {
		return Delivery{}, ErrRateLimited
	}

	body, err := readBody(r)
	if err != nil {
		return Delivery{}, err
	}
	parsed, err := parseDelivery(provider, r.Header, body)
	if err != nil {
		return Delivery{}, err
	}

	targets, err := s.repo.Targets(ctx, provider, parsed.Repository)
	if err != nil {
		return Delivery{}, err
	}
	if len(targets) == 0 {
		return Delivery{}, ErrUnauthorized
	}
	target, ok := authorizedTarget(provider, r.Header, body, targets)
	if !ok {
		return Delivery{}, ErrUnauthorized
	}

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
	if !strings.EqualFold(branch, target.Branch) {
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
		return Delivery{Status: StatusDuplicate, Reason: "commit already handled"}, nil
	case err != nil:
		return Delivery{}, err
	}

	deployment, err := s.deployer.DeploySystem(ctx, target.ApplicationID)
	if err != nil {
		// Nothing was queued, so the claim must not stand: releasing it keeps
		// the anti-spam guarantee ("never queue twice") while letting the
		// host's retry of this delivery through.
		s.releaseClaim(ctx, event)
		if errors.Is(err, deploy.ErrConflict) {
			return Delivery{Status: StatusSkipped, Reason: "deployment in progress"}, nil
		}
		return Delivery{}, err
	}
	if err := s.repo.LinkEventDeployment(ctx, event.ID, deployment.ID); err != nil {
		s.logger.Warn("webhooks: could not link delivery to deployment",
			"event_id", event.ID, "deployment_id", deployment.ID, "error", err)
	}
	return Delivery{Status: StatusQueued, DeploymentID: deployment.ID.String()}, nil
}

// authorizedTarget returns the hook whose secret authenticates the delivery.
// Every candidate is probed, so several applications watching one repository
// each keep their own secret without weakening the comparison.
func authorizedTarget(provider string, header http.Header, body []byte, targets []Target) (Target, bool) {
	for _, target := range targets {
		if verifySignature(provider, header, body, target.Secret) {
			return target, true
		}
	}
	return Target{}, false
}

// releaseClaim undoes a claim whose deployment never started.
func (s *Service) releaseClaim(ctx context.Context, event Event) {
	if err := s.repo.ReleaseEvent(ctx, event.ID); err != nil {
		s.logger.Warn("webhooks: could not release delivery claim", "event_id", event.ID, "error", err)
	}
}

// hookRollbackTimeout bounds the best-effort rollback of a hook whose row
// could not be stored. It is deliberately short: the rollback runs on a
// detached context (see bestEffortDelete), so nothing else bounds it, and the
// create request budget is deploy.DefaultHookTimeout + this value — 8s + 3s =
// 11s, leaving 4s of the SPA's 15s request timeout. TestHookBudgetsStayUnderSPARequestTimeout
// pins the sum; do not raise this without shrinking the other side.
const hookRollbackTimeout = 3 * time.Second

// bestEffortDelete removes a hook the host already accepted when storing it
// failed, so a retry does not accumulate orphan hooks on the repository.
//
// It detaches from the install context: that context carries the caller's
// deadline and is exactly what may have just expired (a provider that
// answered near the timeout, a store write that failed on the expired
// context), and removal must still happen then. The detached context is
// bounded so a stalled host cannot hold the rollback open forever; the
// values (there are none used here) survive, the cancellation does not.
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

// mapProviderError turns a Git-host failure into a webhook sentinel so the
// route can answer without leaking the provider's response body.
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

// hookEvents returns the events a newly installed hook subscribes to. Previews
// add pull_request deliveries; with the feature off the hook stays push-only.
// Hooks installed before previews existed keep their push-only subscription
// until they are deleted and re-installed — the plan's documented re-install
// path, chosen over silently re-creating a hook (which would lose the secret
// deliveries are signed with).
func (s *Service) hookEvents() []string {
	if !Enabled() {
		return []string{"push"}
	}
	return []string{"push", "pull_request"}
}

// isSupported reports whether a provider name has webhook implementations.
func isSupported(provider string) bool {
	switch provider {
	case providers.NameGitHub, providers.NameGitLab, providers.NameGitea:
		return true
	default:
		return false
	}
}

// validateCallbackURL rejects a callback that is not an absolute http(s) URL.
// The host cannot post to anything else, and installing such a hook would
// leave a silently broken integration behind.
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

// newHookSecret returns a fresh 256-bit signing secret, base64url-encoded.
func newHookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("webhooks: generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// clientIP extracts the peer IP from RemoteAddr, falling back to the raw value
// when it is not in host:port form.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
