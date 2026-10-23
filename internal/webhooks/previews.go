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

const FeatureEnv = "FEATURE_PREVIEWS"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const (
	PreviewActive    = "active"
	PreviewDeploying = "deploying"
	PreviewClosing   = "closing"
	PreviewFailed    = "failed"
	PreviewDeleted   = "deleted"
)

const (
	prActionStart = "start"
	prActionClose = "close"
)

const (
	defaultPreviewSweepInterval   = time.Hour
	commentTimeout                = 5 * time.Second
	maxLivePreviewsPerApplication = 5
	previewSweepGrace             = 15 * time.Minute
)

type PreviewProvisioner interface {
	CreatePreviewApplication(ctx context.Context, baseAppID uuid.UUID, in deploy.PreviewApplicationInput) (deploy.Application, error)
	DeleteSystemApplication(ctx context.Context, appID uuid.UUID) error
}

type Commenter interface {
	CreatePullRequestComment(ctx context.Context, target providers.HookTarget, number int, body string) error
}

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

func previewName(baseName string, number int) string {
	name := strings.TrimSpace(baseName)
	if name == "" {
		name = "app"
	}
	return fmt.Sprintf("%s-pr-%d", name, number)
}

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

func (s *Service) receivePullRequest(ctx context.Context, provider string, target Target, parsed signedDelivery) (Delivery, error) {
	if !Enabled() {
		return Delivery{Status: StatusIgnored, Reason: "previews disabled"}, nil
	}
	pr := parsed.PullRequest
	if pr == nil || pr.Number <= 0 {
		return Delivery{Status: StatusIgnored, Reason: "pull request"}, nil
	}
	if pr.Fork {
		return Delivery{Status: StatusIgnored, Reason: "fork"}, nil
	}
	if pr.BaseBranch != target.Branch {
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
			return Delivery{Status: StatusIgnored, Reason: "application has no domain"}, nil
		}
	}

	claim, err := s.repo.ClaimPreviewDelivery(ctx, PreviewClaim{
		ApplicationID: target.ApplicationID,
		PRNumber:      pr.Number,
		Kind:          reservationKind(action),
		HeadSHA:       startSHA(pr, action),
		DeliveryID:    parsed.DeliveryID,
		LiveLimit:     maxLivePreviewsPerApplication,
	})
	if err != nil {
		return Delivery{}, err
	}
	switch {
	case claim.Duplicate:
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
		if !errors.Is(err, errCloseFenceHeld) {
			s.releaseReservation(ctx, claim.Reservation)
		}
		return Delivery{}, err
	}
	return delivery, nil
}

func reservationKind(action string) string {
	if action == prActionClose {
		return ReservationClose
	}
	return ReservationStart
}

func startSHA(pr *pullRequest, action string) string {
	if action == prActionClose {
		return ""
	}
	return strings.TrimSpace(pr.HeadSHA)
}

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

	if err := promote(PreviewBindingWrite{
		HeadSHA:              previousSHA,
		PreviewApplicationID: previewAppID,
		State:                PreviewDeploying,
	}); err != nil {
		s.compensatePreview(ctx, previewAppID, provisioned)
		return Delivery{}, err
	}

	deployment, deployErr := s.deployer.DeploySystem(ctx, previewAppID)
	if errors.Is(deployErr, deploy.ErrNotFound) {
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
		if err := promote(PreviewBindingWrite{
			HeadSHA:              strings.TrimSpace(pr.HeadSHA),
			PreviewApplicationID: previewAppID,
			State:                PreviewActive,
			ConsumeLease:         true,
		}); err != nil {
			s.logger.Warn("webhooks: could not record the queued preview revision",
				"application_id", target.ApplicationID, "pr_number", pr.Number, "error", err)
			return Delivery{}, err
		}
		s.previewComment(ctx, target, pr.Number, startedComment(host))
		return Delivery{Status: StatusQueued, Reason: "preview", DeploymentID: deployment.ID.String(), Host: host}, nil
	case errors.Is(deployErr, deploy.ErrConflict):
		return Delivery{}, fmt.Errorf("%w: a deployment is already running for the preview", ErrRetryable)
	default:
		return Delivery{}, fmt.Errorf("webhooks: queue preview deployment: %w", deployErr)
	}
}

func (s *Service) compensatePreview(ctx context.Context, appID uuid.UUID, provisioned bool) {
	if !provisioned || appID == uuid.Nil {
		return
	}
	if err := s.provisioner.DeleteSystemApplication(ctx, appID); err != nil {
		s.logger.Warn("webhooks: preview compensation delete failed; the sweep will pick it up",
			"application_id", appID, "error", err)
	}
}

func (s *Service) closePreview(ctx context.Context, target Target, number int, claim *PreviewClaimResult) (Delivery, error) {
	if claim.Binding == nil {
		if cerr := s.repo.ClearPreviewDeliveries(ctx, target.ApplicationID, number); cerr != nil {
			return Delivery{}, errors.Join(ErrRetryable, errCloseFenceHeld, cerr)
		}
		return Delivery{Status: StatusIgnored, Reason: "no preview"}, nil
	}
	preview := *claim.Binding
	if preview.State == PreviewDeleted {
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
		return Delivery{}, err
	}
	if _, err := s.repo.MarkPreviewClosed(ctx, target.ApplicationID, number); err != nil {
		return Delivery{}, err
	}
	s.previewComment(ctx, target, number, deletedComment())
	return Delivery{Status: StatusDeleted, Reason: "preview deleted", Host: preview.Host}, nil
}

func (s *Service) teardownPreview(ctx context.Context, preview Preview) error {
	if preview.PreviewApplicationID == uuid.Nil {
		return nil
	}
	return s.provisioner.DeleteSystemApplication(ctx, preview.PreviewApplicationID)
}

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

func (s *Service) releaseReservation(ctx context.Context, reservation DeliveryReservation) {
	if reservation.ID == uuid.Nil {
		return
	}
	if err := s.repo.ReleasePreviewDelivery(ctx, reservation.ID); err != nil {
		s.logger.Warn("webhooks: could not release preview reservation",
			"reservation_id", reservation.ID, "error", err)
	}
}

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

func startedComment(host string) string {
	return "Preview deployment started: http://" + host + "\n\n" +
		"This preview updates on every push to the pull request and is removed when the pull request closes."
}

func failedComment(host string) string {
	return "Preview deployment could not be started for http://" + host + ".\n\n" +
		"Check the application's deployment log for details."
}

func succeededComment(host string) string {
	return "Preview deployment is live: http://" + host
}

func failedDeployComment(host string) string {
	return "Preview deployment failed for http://" + host + ".\n\n" +
		"Check the application's deployment log for details."
}

func deletedComment() string {
	return "Preview deployment removed."
}

func (s *Service) DeployFinished(ctx context.Context, result deploy.DeployResult) {
	if s == nil || s.repo == nil || s.commenter == nil {
		return
	}
	if result.State != deploy.StateRunning && result.State != deploy.StateFailed {
		return
	}
	binding, err := s.repo.GetPreviewByApplication(ctx, result.ApplicationID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Warn("webhooks: preview terminal comment lookup failed",
				"application_id", result.ApplicationID, "error", err)
		}
		return
	}
	if binding.State != PreviewActive {
		return
	}
	base, err := s.repo.GetApplication(ctx, binding.ApplicationID)
	if err != nil {
		s.logger.Warn("webhooks: preview terminal comment application lookup failed",
			"application_id", binding.ApplicationID, "error", err)
		return
	}
	host := strings.TrimSpace(result.Host)
	if host == "" {
		host = binding.Host
	}
	target := Target{
		ApplicationID: base.ID,
		UserID:        base.UserID,
		TeamID:        base.TeamID,
		Provider:      base.Provider,
		Repo:          base.Repo,
		CloneURL:      base.CloneURL,
		Name:          base.Name,
		BaseDomain:    base.BaseDomain,
	}
	if result.State == deploy.StateRunning {
		s.previewComment(ctx, target, binding.PRNumber, succeededComment(host))
		return
	}
	s.previewComment(ctx, target, binding.PRNumber, failedDeployComment(host))
}
