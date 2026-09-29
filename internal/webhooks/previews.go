package webhooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

// FeatureEnv is the kill switch for the preview surface:
// FEATURE_PREVIEWS=false makes every pull request delivery a no-op (push
// deliveries are untouched), starts no orphan sweep and mounts no preview
// routes.
const FeatureEnv = "FEATURE_PREVIEWS"

// Enabled reports whether preview deployments are on. Only an explicit false
// disables them — unset (or any other value) keeps them enabled, matching
// deploy.Enabled.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// Preview lifecycle states, mirroring the preview_deploys CHECK constraint.
const (
	// PreviewActive means the sibling application exists and its deployment
	// was queued (or is already running). The sibling's own deployments rows
	// carry the fine-grained state.
	PreviewActive = "active"
	// PreviewDeploying is reserved for a binding whose deployment is being
	// queued right now.
	PreviewDeploying = "deploying"
	// PreviewFailed means the deployment could not be queued.
	PreviewFailed = "failed"
	// PreviewDeleted means the pull request closed and the preview was torn
	// down. The row is kept as the audit trail.
	PreviewDeleted = "deleted"
)

// Pull request decisions, normalized across hosts. GitHub and Gitea use
// "opened"/"synchronize"/"synchronized"/"reopened"/"closed"; GitLab uses
// "open"/"update"/"reopen"/"close"/"merge". Everything else (edits, labels,
// reviews) is ignored.
const (
	prActionStart = "start"
	prActionClose = "close"
)

// Preview tunables. The TTL is measured from the last event (every
// synchronize refreshes it); it is a backstop for previews whose close
// delivery never arrived, not the normal teardown path.
const (
	defaultPreviewTTL           = 7 * 24 * time.Hour
	defaultPreviewSweepInterval = time.Hour
	// commentTimeout bounds one PR comment call. The comment is best effort:
	// a slow Git host must never hold the delivery open.
	commentTimeout = 5 * time.Second
)

// PreviewProvisioner is the slice of the deploy service the preview surface
// needs: clone a base application into a preview sibling, and delete a
// sibling on the system path (no caller to authorize).
type PreviewProvisioner interface {
	CreatePreviewApplication(ctx context.Context, baseAppID uuid.UUID, in deploy.PreviewApplicationInput) (deploy.Application, error)
	DeleteSystemApplication(ctx context.Context, appID uuid.UUID) error
}

// Commenter posts the preview badge comment on the pull request. It is
// optional: a nil Commenter disables comments only, previews still deploy.
type Commenter interface {
	CreatePullRequestComment(ctx context.Context, target providers.HookTarget, number int, body string) error
}

// closeCommit hides the head SHA of a teardown delivery from the commit
// dedupe index (see receivePullRequest).
func closeCommit(pr *pullRequest, action string) string {
	if action == prActionClose {
		return ""
	}
	return pr.HeadSHA
}

// normalizePullRequestAction maps the provider-specific action vocabulary onto
// the three decisions a preview makes: start (opened/synchronize/reopened),
// stop (closed/merge) or ignore.
func normalizePullRequestAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "closed", "close", "merge", "merged":
		return prActionClose
	case "opened", "open", "reopened", "reopen", "synchronize", "synchronized", "update":
		return prActionStart
	default:
		return ""
	}
}

// previewName derives the sibling application name: "<base>-pr-<n>". The
// name is what makes a preview recognizable in the applications list; it is
// unique per (user, name), which also keeps a re-created preview from
// colliding with itself.
func previewName(baseName string, number int) string {
	name := strings.TrimSpace(baseName)
	if name == "" {
		name = "app"
	}
	return fmt.Sprintf("%s-pr-%d", name, number)
}

// previewHost derives "pr-<n>-<slug(base)>.<base-domain>". The base name is
// slugified because it becomes one DNS label: lowercase letters, digits and
// hyphens only, no leading/trailing hyphen and at most 63 characters.
func previewHost(baseName, baseDomain string, number int) string {
	baseDomain = strings.TrimSpace(baseDomain)
	if baseDomain == "" {
		return ""
	}
	label := fmt.Sprintf("pr-%d", number)
	if slug := slugifyLabel(baseName); slug != "" {
		label += "-" + slug
	}
	if len(label) > 63 {
		label = strings.TrimRight(label[:63], "-")
	}
	return label + "." + baseDomain
}

// slugifyLabel turns an application name into a DNS-label-safe slug: lower
// case, [a-z0-9-] only, no runs of hyphens, no leading/trailing hyphen.
func slugifyLabel(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	return b.String()
}

// receivePullRequest handles a verified pull_request delivery: it finds or
// creates the preview of the base application and either queues a deployment
// of the PR head branch or tears the preview down. Any untriggering PR (an
// unwatched base branch, a metadata-only action, a repository without an
// application) is acknowledged as ignored.
//
// The delivery is claimed in webhook_events exactly like a push, so a
// redelivered PR event cannot start a second deployment.
func (s *Service) receivePullRequest(ctx context.Context, provider string, target Target, parsed signedDelivery) (Delivery, error) {
	if !Enabled() {
		return Delivery{Status: StatusIgnored, Reason: "previews disabled"}, nil
	}
	pr := parsed.PullRequest
	if pr == nil || pr.Number <= 0 {
		return Delivery{Status: StatusIgnored, Reason: "pull request"}, nil
	}
	if !strings.EqualFold(pr.BaseBranch, target.Branch) {
		return Delivery{Status: StatusIgnored, Reason: "branch"}, nil
	}
	action := normalizePullRequestAction(pr.Action)
	if action == "" {
		return Delivery{Status: StatusIgnored, Reason: "action"}, nil
	}
	if s.provisioner == nil {
		return Delivery{Status: StatusIgnored, Reason: "previews disabled"}, nil
	}

	event, err := s.repo.ClaimEvent(ctx, Event{
		ApplicationID: target.ApplicationID,
		Provider:      provider,
		Event:         parsed.Event,
		DeliveryID:    parsed.DeliveryID,
		Ref:           "refs/heads/" + pr.HeadBranch,
		// Only a start delivery dedupes by commit: a close event carries the
		// same head SHA as the synchronize that deployed it, and deduping the
		// close against that claim would leave the preview running forever.
		// Close redeliveries are made idempotent by the binding's state (and
		// by the delivery ID when the host repeats it verbatim).
		CommitSHA: closeCommit(pr, action),
	})
	switch {
	case errors.Is(err, ErrDuplicate):
		return Delivery{Status: StatusDuplicate, Reason: "delivery already handled"}, nil
	case err != nil:
		return Delivery{}, err
	}

	var delivery Delivery
	if action == prActionClose {
		delivery, err = s.closePreview(ctx, target, pr.Number)
	} else {
		delivery, err = s.openPreview(ctx, provider, target, pr)
	}
	if err != nil {
		// Nothing durable was achieved: release the claim so the host's retry
		// of this delivery is not mistaken for spam.
		s.releaseClaim(ctx, event)
		return Delivery{}, err
	}
	return delivery, nil
}

// ListPreviews returns an application's preview bindings, newest first, for a
// caller whose active team may read the application. A foreign team's
// application fails like a missing one, so application IDs cannot be probed.
func (s *Service) ListPreviews(ctx context.Context, userID, appID uuid.UUID) ([]Preview, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("webhooks: service is not configured")
	}
	if !Enabled() {
		return nil, fmt.Errorf("%w: previews are disabled", ErrNotFound)
	}
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	previews, err := s.repo.ListPreviews(ctx, appID)
	if err != nil {
		return nil, err
	}
	if previews == nil {
		return []Preview{}, nil
	}
	return previews, nil
}

// openPreview creates (or reuses) the preview sibling of a base application
// and queues a deployment of the PR head branch. Re-deliveries of the same PR
// refresh the existing sibling instead of racing a second one.
func (s *Service) openPreview(ctx context.Context, provider string, target Target, pr *pullRequest) (Delivery, error) {
	host := previewHost(target.Name, target.BaseDomain, pr.Number)
	if host == "" {
		// A preview has no address without a base domain; deploying it would
		// only produce an unreachable container.
		return Delivery{Status: StatusIgnored, Reason: "application has no domain"}, nil
	}

	existing, err := s.repo.GetPreview(ctx, target.ApplicationID, pr.Number)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Delivery{}, err
	}
	appID := uuid.Nil
	if err == nil && existing.State != PreviewDeleted {
		appID = existing.PreviewApplicationID
	}

	var deployment deploy.Deployment
	deployErr := error(nil)
	if appID != uuid.Nil {
		deployment, deployErr = s.deployer.DeploySystem(ctx, appID)
		if errors.Is(deployErr, deploy.ErrNotFound) {
			// The sibling was removed out of band: recreate it below.
			appID, deployErr = uuid.Nil, nil
		}
	}
	if appID == uuid.Nil {
		created, err := s.provisioner.CreatePreviewApplication(ctx, target.ApplicationID, deploy.PreviewApplicationInput{
			Name:       previewName(target.Name, pr.Number),
			Branch:     pr.HeadBranch,
			BaseDomain: host,
		})
		if err != nil {
			if errors.Is(err, deploy.ErrConflict) || errors.Is(err, deploy.ErrValidation) {
				// A name or host collision is permanent for this attempt:
				// answer ignored (2xx) with the reason in the delivery log so
				// the host does not retry forever.
				return Delivery{Status: StatusIgnored, Reason: "preview application rejected"}, nil
			}
			return Delivery{}, err
		}
		appID = created.ID
		deployment, deployErr = s.deployer.DeploySystem(ctx, appID)
	}

	state := PreviewActive
	switch {
	case deployErr == nil:
		// queued (or already running: ErrConflict below)
	case errors.Is(deployErr, deploy.ErrConflict):
		deployErr = nil // a deployment is already in flight; the preview is live
	default:
		state = PreviewFailed
	}
	if _, err := s.repo.UpsertPreview(ctx, Preview{
		ApplicationID:        target.ApplicationID,
		TeamID:               target.TeamID,
		Provider:             provider,
		Repo:                 target.Repo,
		PRNumber:             pr.Number,
		Branch:               pr.HeadBranch,
		HeadSHA:              pr.HeadSHA,
		PreviewApplicationID: appID,
		Host:                 host,
		State:                state,
	}); err != nil {
		return Delivery{}, err
	}

	switch {
	case state == PreviewFailed:
		s.previewComment(ctx, target, pr.Number, failedComment(host))
		return Delivery{}, fmt.Errorf("webhooks: queue preview deployment: %w", deployErr)
	case deployErr == nil && deployment.ID != uuid.Nil:
		s.previewComment(ctx, target, pr.Number, startedComment(host))
		return Delivery{Status: StatusQueued, Reason: "preview", DeploymentID: deployment.ID.String(), Host: host}, nil
	default:
		if deployment.ID != uuid.Nil {
			return Delivery{Status: StatusSkipped, Reason: "deployment in progress", DeploymentID: deployment.ID.String(), Host: host}, nil
		}
		return Delivery{Status: StatusSkipped, Reason: "preview exists", Host: host}, nil
	}
}

// closePreview tears a preview down when its pull request closes or merges.
// The sibling application is deleted through the system path (container stop
// best effort, route refreshed), which cascades its deployments and
// configuration; the binding row stays as the audit trail.
func (s *Service) closePreview(ctx context.Context, target Target, number int) (Delivery, error) {
	preview, err := s.repo.GetPreview(ctx, target.ApplicationID, number)
	if errors.Is(err, ErrNotFound) {
		return Delivery{Status: StatusIgnored, Reason: "no preview"}, nil
	}
	if err != nil {
		return Delivery{}, err
	}
	if preview.State == PreviewDeleted {
		return Delivery{Status: StatusDuplicate, Reason: "preview already deleted"}, nil
	}
	if err := s.teardownPreview(ctx, preview); err != nil {
		return Delivery{}, err
	}
	s.previewComment(ctx, target, number, deletedComment())
	return Delivery{Status: StatusDeleted, Reason: "preview deleted", Host: preview.Host}, nil
}

// teardownPreview deletes a preview's sibling application and marks the
// binding deleted. Deleting the sibling removes its container and, through
// the proxy sync, its route.
func (s *Service) teardownPreview(ctx context.Context, preview Preview) error {
	if preview.PreviewApplicationID != uuid.Nil {
		if err := s.provisioner.DeleteSystemApplication(ctx, preview.PreviewApplicationID); err != nil {
			return err
		}
	}
	if _, err := s.repo.MarkPreviewDeleted(ctx, preview.ID); err != nil {
		return err
	}
	return nil
}

// DeletePreviews tears down every live preview of one base application. The
// deploy service calls it before deleting the base row, because the preview
// siblings hang off the base application outside the deploy schema: the
// preview_deploys link cascades with the base, so the siblings must be
// removed first or their containers would be orphaned.
func (s *Service) DeletePreviews(ctx context.Context, baseAppID uuid.UUID) {
	if s == nil || s.repo == nil || s.provisioner == nil || baseAppID == uuid.Nil {
		return
	}
	previews, err := s.repo.ListPreviews(ctx, baseAppID)
	if err != nil {
		s.logger.Warn("webhooks: preview lookup failed",
			"application_id", baseAppID, "error", err)
		return
	}
	for _, preview := range previews {
		if preview.State == PreviewDeleted {
			continue
		}
		if err := s.teardownPreview(ctx, preview); err != nil {
			s.logger.Warn("webhooks: preview teardown failed",
				"preview_id", preview.ID, "application_id", baseAppID, "error", err)
		}
	}
}

// SweepPreviews tears down previews whose last activity is older than the
// configured TTL. It is the backstop for close deliveries that never arrived
// (deleted hooks, lost networks); the normal teardown path is the closed PR
// event. Per-row failures are logged and skipped so one unreachable node
// cannot block the rest of the sweep.
func (s *Service) SweepPreviews(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.provisioner == nil || !Enabled() {
		return 0, nil
	}
	stale, err := s.repo.ListStalePreviews(ctx, s.now().UTC().Add(-s.previewTTL))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, preview := range stale {
		if err := s.teardownPreview(ctx, preview); err != nil {
			s.logger.Warn("webhooks: orphaned preview teardown failed",
				"preview_id", preview.ID, "application_id", preview.ApplicationID, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		s.logger.Info("webhooks: orphaned previews removed", "count", removed)
	}
	return removed, nil
}

// StartPreviews starts the orphan sweep, exactly once. The server calls it
// with the rest of the route wiring and stops it through Close on shutdown.
// With previews disabled (or without a repository/provisioner) it is a no-op.
func (s *Service) StartPreviews() {
	if s == nil || s.repo == nil || s.provisioner == nil || !Enabled() {
		return
	}
	s.sweepOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.sweepStop = cancel
		s.sweepDone = make(chan struct{})
		go s.sweepLoop(ctx)
	})
}

// sweepLoop runs SweepPreviews on the configured interval until the context
// is cancelled.
func (s *Service) sweepLoop(ctx context.Context) {
	defer close(s.sweepDone)
	ticker := time.NewTicker(s.sweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepPreviews(ctx); err != nil {
				s.logger.Warn("webhooks: preview sweep failed", "error", err)
			}
		}
	}
}

// Close stops the orphan sweep and waits for the loop to exit. It is safe to
// call more than once, and on a service that never started one.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.sweepStop != nil {
			s.sweepStop()
		}
		if s.sweepDone != nil {
			<-s.sweepDone
		}
	})
	return nil
}

// previewComment posts the PR badge comment, best effort: a Git host that
// cannot accept a comment never fails the delivery that produced it.
func (s *Service) previewComment(ctx context.Context, target Target, number int, body string) {
	if s.commenter == nil || number <= 0 || strings.TrimSpace(body) == "" {
		return
	}
	commentCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commentTimeout)
	defer cancel()
	if err := s.commenter.CreatePullRequestComment(commentCtx, providers.HookTarget{
		UserID:   target.UserID,
		Provider: target.Provider,
		CloneURL: target.CloneURL,
		Repo:     target.Repo,
	}, number, body); err != nil {
		s.logger.Warn("webhooks: preview comment failed",
			"application_id", target.ApplicationID, "pr_number", number, "error", err)
	}
}

// startedComment is the badge comment of a queued preview.
func startedComment(host string) string {
	return "Preview deployment started: http://" + host + "\n\n" +
		"This preview updates on every push to the pull request and is removed when the pull request closes."
}

// failedComment is the badge comment of a preview whose deployment could not
// be queued.
func failedComment(host string) string {
	return "Preview deployment could not be started for http://" + host + ".\n\n" +
		"Check the application's deployment log for details."
}

// deletedComment is the badge comment of a torn-down preview.
func deletedComment() string {
	return "Preview deployment removed."
}
