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
	// PreviewActive means the sibling application exists and its newest
	// revision was queued. The sibling's own deployments rows carry the
	// fine-grained state.
	PreviewActive = "active"
	// PreviewDeploying means the binding is reserved and a revision is being
	// queued (or a queue attempt failed and the binding awaits its next
	// delivery).
	PreviewDeploying = "deploying"
	// PreviewClosing means a close delivery owns the preview: the close intent
	// is persisted before the sibling is torn down, so a failed (or lost)
	// teardown is re-attempted by the sweep.
	PreviewClosing = "closing"
	// PreviewFailed is reserved for the previews screen contract; the current
	// lifecycle records a failed queue attempt as deploying (the sibling is
	// intact and the next delivery retries it).
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

// Preview tunables.
const (
	defaultPreviewSweepInterval = time.Hour
	// commentTimeout bounds one PR comment call. The comment is best effort:
	// a slow Git host must never hold the delivery open.
	commentTimeout = 5 * time.Second
	// maxLivePreviewsPerApplication caps how many live previews one base
	// application may have. A PR synchronize of an already-previewed PR never
	// counts against the cap (it refreshes its own binding); a new PR beyond
	// the cap is acknowledged and logged.
	maxLivePreviewsPerApplication = 5
	// previewSweepGrace keeps the orphan-application sweep away from a
	// sibling that a delivery has just provisioned but not yet bound. It is
	// far larger than the window between the clone and the binding write.
	previewSweepGrace = 15 * time.Minute
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

// normalizePullRequestAction maps the provider-specific action vocabulary onto
// the two decisions a preview makes: start (opened/synchronize/reopened) and
// close (closed/merge), or "" for everything else.
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

// receivePullRequest handles a verified pull_request delivery: it reserves the
// delivery in the preview-specific ledger (never in the push webhook_events
// ledger), then either queues a deployment of the PR head revision or tears
// the preview down. Any untriggering PR (a fork head, an unwatched base
// branch, a metadata-only action, a repository without an application) is
// acknowledged as ignored.
//
// A reservation is released on every path that did not queue a deployment, so
// a failed or refused delivery can be retried by the host; a successful start
// keeps it, which is what makes a replayed body a duplicate.
func (s *Service) receivePullRequest(ctx context.Context, provider string, target Target, parsed signedDelivery) (Delivery, error) {
	if !Enabled() {
		return Delivery{Status: StatusIgnored, Reason: "previews disabled"}, nil
	}
	pr := parsed.PullRequest
	if pr == nil || pr.Number <= 0 {
		return Delivery{Status: StatusIgnored, Reason: "pull request"}, nil
	}
	if pr.Fork {
		// A fork PR's code is not the repository the base application trusts:
		// never build it against the platform.
		return Delivery{Status: StatusIgnored, Reason: "fork"}, nil
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
	if action == prActionStart {
		if strings.TrimSpace(pr.HeadSHA) == "" {
			return Delivery{Status: StatusIgnored, Reason: "revision"}, nil
		}
		if strings.TrimSpace(pr.HeadBranch) == "" {
			return Delivery{Status: StatusIgnored, Reason: "branch"}, nil
		}
		if strings.TrimSpace(target.BaseDomain) == "" {
			// A preview has no address without a base domain; deploying it
			// would only produce an unreachable container.
			return Delivery{Status: StatusIgnored, Reason: "application has no domain"}, nil
		}
	}

	claim, err := s.repo.ClaimPreviewDelivery(ctx, PreviewClaim{
		ApplicationID: target.ApplicationID,
		PRNumber:      pr.Number,
		Kind:          reservationKind(action),
		// Only a start names a revision: a close carries no revision, and its
		// idempotency is the PR-scoped close marker plus the binding state.
		HeadSHA:    startSHA(pr, action),
		DeliveryID: parsed.DeliveryID,
		LiveLimit:  maxLivePreviewsPerApplication,
	})
	if err != nil {
		// Includes the retryable "the application vanished" case, which the
		// route answers 503 for.
		return Delivery{}, err
	}
	switch {
	case claim.Duplicate:
		// The live binding already names this head, or an earlier attempt for
		// it is still in flight.
		return Delivery{Status: StatusDuplicate, Reason: "delivery already handled"}, nil
	case claim.Retryable:
		return Delivery{}, fmt.Errorf("%w: a close transition is in progress", ErrRetryable)
	case claim.Limit:
		s.logger.Warn("webhooks: preview limit reached",
			"application_id", target.ApplicationID, "pr_number", pr.Number,
			"limit", maxLivePreviewsPerApplication)
		return Delivery{Status: StatusIgnored, Reason: "preview limit reached"}, nil
	}

	var delivery Delivery
	if action == prActionClose {
		delivery, err = s.closePreview(ctx, target, pr.Number, &claim)
	} else {
		delivery, err = s.openPreview(ctx, provider, target, pr, &claim)
	}
	if err != nil {
		// The lease must not outlive a delivery that queued nothing: a
		// redelivered body has to be able to claim again.
		s.releaseReservation(ctx, claim.Reservation)
		return Delivery{}, err
	}
	return delivery, nil
}

// reservationKind maps the normalized action onto the ledger kind.
func reservationKind(action string) string {
	if action == prActionClose {
		return ReservationClose
	}
	return ReservationStart
}

// startSHA hides the head SHA of a teardown delivery from the start
// reservation key (see receivePullRequest).
func startSHA(pr *pullRequest, action string) string {
	if action == prActionClose {
		return ""
	}
	return strings.TrimSpace(pr.HeadSHA)
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
// and queues a deployment of the PR head revision. The binding is persisted
// before the deployment is queued, so a queue failure can never leave a
// sibling outside every cleanup path (close, base delete, sweep). The quota
// and the current-head dedupe already ran atomically in the claim.
func (s *Service) openPreview(ctx context.Context, provider string, target Target, pr *pullRequest, claim *PreviewClaimResult) (Delivery, error) {
	host := previewHost(target.Name, target.BaseDomain, pr.Number)
	if host == "" {
		s.releaseReservation(ctx, claim.Reservation)
		return Delivery{Status: StatusIgnored, Reason: "application has no domain"}, nil
	}

	existing := claim.Binding
	live := existing != nil && existing.State != PreviewDeleted
	previewAppID := uuid.Nil
	previousSHA := ""
	if existing != nil {
		previousSHA = existing.HeadSHA
	}
	if live {
		previewAppID = existing.PreviewApplicationID
	}

	// promote is the fenced binding write: it is refused when the claim lease
	// has lapsed (a stale worker), when a close owns the preview, or when the
	// quota filled while this worker provisioned.
	promote := func(write PreviewBindingWrite) error {
		write.ApplicationID = target.ApplicationID
		write.PRNumber = pr.Number
		write.ReservationID = claim.Reservation.ID
		write.LeaseHeadSHA = strings.TrimSpace(pr.HeadSHA)
		write.TeamID = target.TeamID
		write.Provider = provider
		write.Repo = target.Repo
		write.Branch = pr.HeadBranch
		write.Host = host
		write.LiveLimit = maxLivePreviewsPerApplication
		result, err := s.repo.WritePreviewBinding(ctx, write)
		if err != nil {
			return err
		}
		switch result.Refused {
		case "":
			return nil
		case BindingRefusedClosing:
			return fmt.Errorf("%w: a close transition is in progress", ErrRetryable)
		case BindingRefusedLease:
			return fmt.Errorf("%w: the preview claim expired", ErrRetryable)
		default:
			return fmt.Errorf("%w: the preview limit was reached while provisioning", ErrRetryable)
		}
	}

	provisioned := false
	if previewAppID == uuid.Nil {
		created, err := s.provisioner.CreatePreviewApplication(ctx, target.ApplicationID, deploy.PreviewApplicationInput{
			Name:       previewName(target.Name, pr.Number),
			Branch:     pr.HeadBranch,
			BaseDomain: host,
		})
		if err != nil {
			// A concurrent delivery may have created the sibling and the
			// binding between our read and the clone: recover that binding.
			// Otherwise the failure is retryable (a transient store error, or
			// a name/host collision the operator can resolve) — never a
			// permanent ignore, which would strand the sibling.
			recovered, recoveryErr := s.repo.GetPreview(ctx, target.ApplicationID, pr.Number)
			if recoveryErr == nil && recovered.State != PreviewDeleted && recovered.PreviewApplicationID != uuid.Nil {
				previewAppID, previousSHA = recovered.PreviewApplicationID, recovered.HeadSHA
			} else {
				s.previewComment(ctx, target, pr.Number, failedComment(host))
				return Delivery{}, fmt.Errorf("%w: provision preview application: %v", ErrRetryable, err)
			}
		} else {
			previewAppID = created.ID
			provisioned = true
		}
	}

	// Persist the binding BEFORE queueing: a crash or a queue failure then
	// leaves a tracked sibling, and the next delivery recovers it instead of
	// hitting (and being rejected by) its unique name and host. The write is
	// fenced, so a worker whose lease lapsed cannot promote a live binding.
	if err := promote(PreviewBindingWrite{
		HeadSHA:              previousSHA,
		PreviewApplicationID: previewAppID,
		State:                PreviewDeploying,
	}); err != nil {
		// Compensation: a sibling this delivery just created must not survive
		// a binding it cannot be found through.
		s.compensatePreview(ctx, previewAppID, provisioned)
		return Delivery{}, err
	}

	deployment, deployErr := s.deployer.DeploySystem(ctx, previewAppID)
	if errors.Is(deployErr, deploy.ErrNotFound) {
		// The sibling vanished between the read and the queue (deleted out of
		// band): clear the dead link and provision a fresh sibling once. Both
		// writes stay fenced, so a close that completed in the meantime is
		// never overwritten back to active (R-1).
		if err := promote(PreviewBindingWrite{
			HeadSHA: previousSHA,
			State:   PreviewDeploying,
		}); err != nil {
			return Delivery{}, err
		}
		created, err := s.provisioner.CreatePreviewApplication(ctx, target.ApplicationID, deploy.PreviewApplicationInput{
			Name:       previewName(target.Name, pr.Number),
			Branch:     pr.HeadBranch,
			BaseDomain: host,
		})
		if err != nil {
			return Delivery{}, fmt.Errorf("%w: recreate preview application: %v", ErrRetryable, err)
		}
		previewAppID = created.ID
		if err := promote(PreviewBindingWrite{
			HeadSHA:              previousSHA,
			PreviewApplicationID: previewAppID,
			State:                PreviewDeploying,
		}); err != nil {
			s.compensatePreview(ctx, previewAppID, true)
			return Delivery{}, err
		}
		deployment, deployErr = s.deployer.DeploySystem(ctx, previewAppID)
	}

	switch {
	case deployErr == nil:
		// The revision is queued: only now does the binding name it, and only
		// now does the in-flight lease go. A replay after this point dedupes
		// against the binding's current head.
		if err := promote(PreviewBindingWrite{
			HeadSHA:              strings.TrimSpace(pr.HeadSHA),
			PreviewApplicationID: previewAppID,
			State:                PreviewActive,
			ConsumeLease:         true,
		}); err != nil {
			// The deployment is queued, but the fenced write did not record
			// the revision: surface it as retryable so the host redelivers
			// instead of losing the revision behind a 200. The next delivery
			// repairs the binding revision.
			s.logger.Warn("webhooks: could not record the queued preview revision",
				"application_id", target.ApplicationID, "pr_number", pr.Number, "error", err)
			return Delivery{}, err
		}
		s.previewComment(ctx, target, pr.Number, startedComment(host))
		return Delivery{Status: StatusQueued, Reason: "preview", DeploymentID: deployment.ID.String(), Host: host}, nil
	case errors.Is(deployErr, deploy.ErrConflict):
		// A deployment is already running for the sibling. The revision must
		// not be recorded as handled: the reservation is released (by the
		// caller) and the delivery answers retryable, so the host can
		// redeliver it once the active deployment finishes. The binding keeps
		// the revision that was actually queued.
		return Delivery{}, fmt.Errorf("%w: a deployment is already running for the preview", ErrRetryable)
	default:
		return Delivery{}, fmt.Errorf("webhooks: queue preview deployment: %w", deployErr)
	}
}

// compensatePreview deletes a sibling this delivery just provisioned when its
// binding could not be promoted (fence refusal or store failure), so the
// orphan sweep is a backstop rather than the only cleanup.
func (s *Service) compensatePreview(ctx context.Context, appID uuid.UUID, provisioned bool) {
	if !provisioned || appID == uuid.Nil {
		return
	}
	if err := s.provisioner.DeleteSystemApplication(ctx, appID); err != nil {
		s.logger.Warn("webhooks: preview compensation delete failed; the sweep will pick it up",
			"application_id", appID, "error", err)
	}
}

// closePreview tears a preview down when its pull request closes or merges.
// The close intent is persisted before the teardown, so a failed (or lost)
// teardown is re-attempted by the sweep instead of leaving the preview running
// forever. The sibling application is deleted through the system path
// (container stop best effort, route refreshed, local deploy-key rows removed
// but the shared remote key kept); the binding row stays as the audit trail.
// The close completion — binding deleted, ledger cleared — is one atomic
// operation, so a stale reservation can never block a reopen.
func (s *Service) closePreview(ctx context.Context, target Target, number int, claim *PreviewClaimResult) (Delivery, error) {
	if claim.Binding == nil {
		// Nothing to tear down. Clearing the ledger keeps a stale reservation
		// from suppressing a later reopen at the same revision.
		if cerr := s.repo.ClearPreviewDeliveries(ctx, target.ApplicationID, number); cerr != nil {
			return Delivery{}, cerr
		}
		return Delivery{Status: StatusIgnored, Reason: "no preview"}, nil
	}
	preview := *claim.Binding
	if preview.State == PreviewDeleted {
		// N2: an already-deleted close retry must still complete the ledger
		// cleanup, and a cleanup failure must surface (so the host retries)
		// instead of reporting success over a poisoned ledger.
		if _, err := s.repo.MarkPreviewClosed(ctx, target.ApplicationID, number); err != nil {
			return Delivery{}, err
		}
		return Delivery{Status: StatusDuplicate, Reason: "preview already deleted"}, nil
	}
	if preview.State != PreviewClosing {
		if _, err := s.repo.MarkPreviewClosing(ctx, preview.ID); err != nil {
			return Delivery{}, err
		}
	}
	if err := s.teardownPreview(ctx, preview); err != nil {
		// The binding stays 'closing': the host may retry, and the sweep
		// re-attempts the teardown either way.
		return Delivery{}, err
	}
	if _, err := s.repo.MarkPreviewClosed(ctx, target.ApplicationID, number); err != nil {
		return Delivery{}, err
	}
	s.previewComment(ctx, target, number, deletedComment())
	return Delivery{Status: StatusDeleted, Reason: "preview deleted", Host: preview.Host}, nil
}

// teardownPreview deletes a preview's sibling application. Deleting the
// sibling removes its container, its local deploy-key rows (the remote key
// stays: it is shared with the base application) and, through the proxy sync,
// its route. The caller completes the close with MarkPreviewClosed.
func (s *Service) teardownPreview(ctx context.Context, preview Preview) error {
	if preview.PreviewApplicationID == uuid.Nil {
		return nil
	}
	return s.provisioner.DeleteSystemApplication(ctx, preview.PreviewApplicationID)
}

// CleanupApplication runs before an application row is deleted (BE-8.1). Two
// cases:
//
//   - the application is itself a preview sibling: its binding is marked
//     deleted (the FK then clears the link), because the binding is the audit
//     trail, not the owner;
//   - the application is a base: every live preview of it is torn down, and a
//     failure is returned so the caller aborts the base delete — the base FK
//     cascades the bindings away, so deleting the base after a failed teardown
//     would leave the siblings untracked. Container stops stay best effort.
func (s *Service) CleanupApplication(ctx context.Context, appID uuid.UUID) error {
	if s == nil || s.repo == nil {
		return errors.New("webhooks: service is not configured")
	}
	if appID == uuid.Nil {
		return fmt.Errorf("%w: application id is required", ErrValidation)
	}
	if err := s.repo.MarkPreviewsDeletedForSibling(ctx, appID); err != nil {
		return err
	}
	if s.provisioner == nil {
		return nil
	}
	previews, err := s.repo.ListPreviews(ctx, appID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, preview := range previews {
		if preview.State == PreviewDeleted {
			continue
		}
		if preview.State != PreviewClosing {
			if _, err := s.repo.MarkPreviewClosing(ctx, preview.ID); err != nil {
				s.logger.Warn("webhooks: could not persist the close intent",
					"preview_id", preview.ID, "application_id", appID, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		if err := s.teardownPreview(ctx, preview); err != nil {
			s.logger.Warn("webhooks: preview teardown failed",
				"preview_id", preview.ID, "application_id", appID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if _, err := s.repo.MarkPreviewClosed(ctx, appID, preview.PRNumber); err != nil {
			s.logger.Warn("webhooks: preview close completion failed",
				"preview_id", preview.ID, "application_id", appID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// SweepPreviews is the orphan sweep, run on the configured interval. It is
// deliberately orphan-only: it never deletes a preview that is still bound to
// a live sibling application, however old it is (an open PR keeps its preview
// until it closes).
//
//   - pass 0 purges expired delivery leases, so a crashed delivery can never
//     suppress a later revision;
//   - pass 1 closes bindings whose sibling is already gone (deleted out of
//     band, or never linked at all);
//   - pass 2 re-attempts the teardown of bindings left in 'closing' by a
//     failed or lost close delivery;
//   - pass 3 deletes preview siblings that no binding references at all and
//     that are older than the grace period (a crash between the clone and the
//     binding write, or a base delete whose cleanup could not finish).
func (s *Service) SweepPreviews(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.provisioner == nil || !Enabled() {
		return 0, nil
	}
	removed := 0

	if purged, err := s.repo.PurgeExpiredPreviewReservations(ctx); err != nil {
		s.logger.Warn("webhooks: expired preview reservations could not be purged", "error", err)
	} else if purged > 0 {
		s.logger.Info("webhooks: expired preview reservations purged", "count", purged)
	}

	orphaned, err := s.repo.ListOrphanedPreviews(ctx)
	if err != nil {
		return 0, err
	}
	for _, preview := range orphaned {
		if _, err := s.repo.MarkPreviewClosed(ctx, preview.ApplicationID, preview.PRNumber); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			s.logger.Warn("webhooks: orphaned preview binding could not be closed",
				"preview_id", preview.ID, "error", err)
			continue
		}
		removed++
	}

	closing, err := s.repo.ListClosingPreviews(ctx, s.now().UTC().Add(-previewSweepGrace))
	if err != nil {
		return removed, err
	}
	for _, preview := range closing {
		if err := s.teardownPreview(ctx, preview); err != nil {
			s.logger.Warn("webhooks: closing preview teardown failed",
				"preview_id", preview.ID, "application_id", preview.ApplicationID, "error", err)
			continue
		}
		if _, err := s.repo.MarkPreviewClosed(ctx, preview.ApplicationID, preview.PRNumber); err != nil {
			s.logger.Warn("webhooks: closing preview could not be completed",
				"preview_id", preview.ID, "error", err)
			continue
		}
		removed++
	}

	siblings, err := s.repo.ListOrphanedPreviewApplications(ctx, s.now().UTC().Add(-previewSweepGrace))
	if err != nil {
		return removed, err
	}
	for _, appID := range siblings {
		if err := s.provisioner.DeleteSystemApplication(ctx, appID); err != nil {
			s.logger.Warn("webhooks: orphaned preview application delete failed",
				"application_id", appID, "error", err)
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

// releaseReservation removes a reservation whose delivery queued nothing.
func (s *Service) releaseReservation(ctx context.Context, reservation DeliveryReservation) {
	if reservation.ID == uuid.Nil {
		return
	}
	if err := s.repo.ReleasePreviewDelivery(ctx, reservation.ID); err != nil {
		s.logger.Warn("webhooks: could not release preview reservation",
			"reservation_id", reservation.ID, "error", err)
	}
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
