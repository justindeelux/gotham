package webhooks

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

type fakeRepository struct {
	mu sync.Mutex

	app    Application
	hook   *Hook
	secret string
	target *Target

	claims   map[string]Event
	released []uuid.UUID

	previews       map[string]Preview
	previewListErr error

	reservations   map[string]DeliveryReservation
	claimStoreErr  error
	promoteErr     error
	promoteErrAt   int
	promoteCalls   int
	unreserveErr   error
	clearErr       error
	orphanErr      error
	orphanAppErr   error
	markSiblingErr error
	markClosingErr error
	markClosedErr  error
	orphanPreviews []Preview
	orphanApps     []uuid.UUID
	now            func() time.Time

	getAppErr    error
	createErr    error
	targetsErr   error
	claimErr     error
	linkErr      error
	releaseErr   error
	upsertErr    error
	getPrevErr   error
	createCalls  int
	deleteCalls  int
	targetsCalls int
}

func (r *fakeRepository) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func newFakeRepository() *fakeRepository {
	return newFakeRepositoryFor(providers.NameGitHub)
}

func newFakeRepositoryFor(provider string) *fakeRepository {
	return &fakeRepository{
		app: Application{
			ID:         uuid.New(),
			UserID:     uuid.New(),
			TeamID:     uuid.New(),
			Provider:   provider,
			Repo:       "octo/gotham",
			Branch:     "main",
			CloneURL:   "https://github.com/octo/gotham.git",
			Name:       "gotham",
			BaseDomain: "apps.example.com",
		},
		claims:       make(map[string]Event),
		previews:     make(map[string]Preview),
		reservations: make(map[string]DeliveryReservation),
	}
}

const testHookSecret = "hook-secret"

func (r *fakeRepository) withTarget() *fakeRepository {
	r.mu.Lock()
	defer r.mu.Unlock()
	secret := testHookSecret
	r.secret = secret
	r.hook = &Hook{
		ID:            uuid.New(),
		ApplicationID: r.app.ID,
		Provider:      r.app.Provider,
		Repo:          r.app.Repo,
		HookID:        "4242",
		URL:           "https://cp.example/api/v1/webhooks/github",
		CreatedAt:     time.Now().UTC(),
	}
	r.target = &Target{
		ApplicationID: r.app.ID,
		UserID:        r.app.UserID,
		TeamID:        r.app.TeamID,
		Provider:      r.app.Provider,
		Repo:          r.app.Repo,
		Branch:        r.app.Branch,
		CloneURL:      r.app.CloneURL,
		Name:          r.app.Name,
		BaseDomain:    r.app.BaseDomain,
		HookID:        r.hook.HookID,
		Secret:        secret,
		URL:           r.hook.URL,
	}
	return r
}

func (r *fakeRepository) GetApplication(_ context.Context, appID uuid.UUID) (Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getAppErr != nil {
		return Application{}, r.getAppErr
	}
	if appID != r.app.ID {
		return Application{}, ErrNotFound
	}
	return r.app, nil
}

func (r *fakeRepository) GetWebhook(_ context.Context, appID uuid.UUID) (Hook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hook == nil || r.hook.ApplicationID != appID {
		return Hook{}, ErrNotFound
	}
	return *r.hook, nil
}

func (r *fakeRepository) CreateWebhook(_ context.Context, hook Hook, secret string) (Hook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	if r.createErr != nil {
		return Hook{}, r.createErr
	}
	if r.hook != nil {
		return Hook{}, ErrConflict
	}
	hook.ID = uuid.New()
	hook.CreatedAt = time.Now().UTC()
	stored := hook
	r.hook = &stored
	r.secret = secret
	r.target = &Target{
		ApplicationID: hook.ApplicationID,
		UserID:        r.app.UserID,
		TeamID:        r.app.TeamID,
		Provider:      hook.Provider,
		Repo:          hook.Repo,
		Branch:        r.app.Branch,
		CloneURL:      r.app.CloneURL,
		Name:          r.app.Name,
		BaseDomain:    r.app.BaseDomain,
		HookID:        hook.HookID,
		Secret:        secret,
		URL:           hook.URL,
	}
	return hook, nil
}

func (r *fakeRepository) DeleteWebhook(_ context.Context, appID uuid.UUID) (Hook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	if r.hook == nil || r.hook.ApplicationID != appID {
		return Hook{}, ErrNotFound
	}
	hook := *r.hook
	r.hook, r.target = nil, nil
	return hook, nil
}

func (r *fakeRepository) Targets(_ context.Context, provider, repo string) ([]Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targetsCalls++
	if r.targetsErr != nil {
		return nil, r.targetsErr
	}
	if r.target == nil || r.target.Provider != provider ||
		!strings.EqualFold(r.target.Repo, repo) {
		return nil, nil
	}
	return []Target{*r.target}, nil
}

func (r *fakeRepository) ClaimEvent(_ context.Context, event Event) (Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimErr != nil {
		return Event{}, r.claimErr
	}
	key := event.CommitSHA
	if key == "" {
		key = event.DeliveryID
	}
	if key == "" {
		key = uuid.NewString()
	}
	if existing, taken := r.claims[key]; taken {
		return existing, ErrDuplicate
	}
	event.ID = uuid.New()
	event.ReceivedAt = time.Now().UTC()
	r.claims[key] = event
	return event, nil
}

func (r *fakeRepository) ReleaseEvent(ctx context.Context, eventID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.releaseErr != nil {
		return r.releaseErr
	}
	for key, event := range r.claims {
		if event.ID == eventID {
			delete(r.claims, key)
		}
	}
	r.released = append(r.released, eventID)
	return nil
}

func (r *fakeRepository) LinkEventDeployment(ctx context.Context, eventID, deploymentID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.linkErr != nil {
		return r.linkErr
	}
	for key, event := range r.claims {
		if event.ID == eventID {
			event.DeploymentID = deploymentID
			r.claims[key] = event
		}
	}
	return nil
}

func (r *fakeRepository) claimCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.claims)
}

func previewKey(appID uuid.UUID, prNumber int) string {
	return fmt.Sprintf("%s/%d", appID, prNumber)
}

func (r *fakeRepository) GetPreview(_ context.Context, appID uuid.UUID, prNumber int) (Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getPrevErr != nil {
		return Preview{}, r.getPrevErr
	}
	preview, ok := r.previews[previewKey(appID, prNumber)]
	if !ok {
		return Preview{}, ErrNotFound
	}
	return preview, nil
}

func (r *fakeRepository) GetPreviewByApplication(_ context.Context, previewAppID uuid.UUID) (Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getPrevErr != nil {
		return Preview{}, r.getPrevErr
	}
	for _, preview := range r.previews {
		if preview.PreviewApplicationID == previewAppID && preview.State != PreviewDeleted {
			return preview, nil
		}
	}
	return Preview{}, ErrNotFound
}

func (r *fakeRepository) UpsertPreview(_ context.Context, preview Preview) (Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.upsertErr != nil {
		return Preview{}, r.upsertErr
	}
	key := previewKey(preview.ApplicationID, preview.PRNumber)
	if existing, ok := r.previews[key]; ok {
		preview.ID = existing.ID
		preview.CreatedAt = existing.CreatedAt
	} else {
		preview.ID = uuid.New()
		preview.CreatedAt = time.Now().UTC()
	}
	preview.UpdatedAt = time.Now().UTC()
	preview.DeletedAt = time.Time{}
	r.previews[key] = preview
	return preview, nil
}

func (r *fakeRepository) ListPreviews(_ context.Context, appID uuid.UUID) ([]Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.previewListErr != nil {
		return nil, r.previewListErr
	}
	previews := make([]Preview, 0, len(r.previews))
	for _, preview := range r.previews {
		if preview.ApplicationID == appID {
			previews = append(previews, preview)
		}
	}
	return previews, nil
}

func (r *fakeRepository) MarkPreviewClosing(_ context.Context, previewID uuid.UUID) (Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.markClosingErr != nil {
		return Preview{}, r.markClosingErr
	}
	for key, preview := range r.previews {
		if preview.ID != previewID {
			continue
		}
		preview.State = PreviewClosing
		preview.UpdatedAt = r.clock().UTC()
		r.previews[key] = preview
		return preview, nil
	}
	return Preview{}, ErrNotFound
}

func (r *fakeRepository) MarkPreviewClosed(_ context.Context, appID uuid.UUID, prNumber int) (Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.markClosedErr != nil {
		return Preview{}, r.markClosedErr
	}
	key := previewKey(appID, prNumber)
	preview, ok := r.previews[key]
	if !ok {
		return Preview{}, ErrNotFound
	}
	now := r.clock().UTC()
	preview.State = PreviewDeleted
	preview.DeletedAt = now
	preview.UpdatedAt = now
	r.previews[key] = preview
	for reservationKey, reservation := range r.reservations {
		if reservation.ApplicationID == appID && reservation.PRNumber == prNumber {
			delete(r.reservations, reservationKey)
		}
	}
	return preview, nil
}

func (r *fakeRepository) ListClosingPreviews(_ context.Context, before time.Time) ([]Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.orphanErr != nil {
		return nil, r.orphanErr
	}
	closing := make([]Preview, 0)
	for _, preview := range r.previews {
		if preview.State == PreviewClosing && preview.UpdatedAt.Before(before) {
			closing = append(closing, preview)
		}
	}
	return closing, nil
}

func (r *fakeRepository) PurgeExpiredPreviewReservations(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unreserveErr != nil {
		return 0, r.unreserveErr
	}
	now := r.clock()
	purged := 0
	for key, reservation := range r.reservations {
		if !reservation.ExpiresAt.IsZero() && !reservation.ExpiresAt.After(now) {
			delete(r.reservations, key)
			purged++
		}
	}
	return purged, nil
}

func ReservationKey(reservation DeliveryReservation) string {
	if reservation.Kind == ReservationClose {
		return fmt.Sprintf("%s/%d/close", reservation.ApplicationID, reservation.PRNumber)
	}
	return fmt.Sprintf("%s/%d/%s", reservation.ApplicationID, reservation.PRNumber, reservation.HeadSHA)
}

func claimReservationKey(claim PreviewClaim) string {
	return ReservationKey(DeliveryReservation{
		ApplicationID: claim.ApplicationID, PRNumber: claim.PRNumber,
		Kind: claim.Kind, HeadSHA: claim.HeadSHA,
	})
}

const leaseTTL = 15 * time.Minute

func (r *fakeRepository) ClaimPreviewDelivery(_ context.Context, claim PreviewClaim) (PreviewClaimResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimStoreErr != nil {
		return PreviewClaimResult{}, r.claimStoreErr
	}
	now := r.clock()
	for key, reservation := range r.reservations {
		if reservation.ApplicationID != claim.ApplicationID || reservation.PRNumber != claim.PRNumber {
			continue
		}
		if !reservation.ExpiresAt.IsZero() && !reservation.ExpiresAt.After(now) {
			delete(r.reservations, key)
		}
	}

	var result PreviewClaimResult
	binding, hasBinding := r.previews[previewKey(claim.ApplicationID, claim.PRNumber)]
	if hasBinding {
		current := binding
		result.Binding = &current
	}
	closeInFlight := r.hasLiveCloseLocked(claim.ApplicationID, claim.PRNumber, now)

	reserve := func() {
		key := claimReservationKey(claim)
		if _, taken := r.reservations[key]; taken {
			result.Duplicate = true
			return
		}
		result.Approved = true
		result.Reservation = DeliveryReservation{
			ID:            uuid.New(),
			ApplicationID: claim.ApplicationID,
			PRNumber:      claim.PRNumber,
			Kind:          claim.Kind,
			HeadSHA:       claim.HeadSHA,
			DeliveryID:    claim.DeliveryID,
			ReceivedAt:    now.UTC(),
			ExpiresAt:     now.Add(leaseTTL).UTC(),
		}
		r.reservations[key] = result.Reservation
	}

	switch claim.Kind {
	case ReservationClose:
		if !hasBinding || binding.State == PreviewDeleted {
			for key, reservation := range r.reservations {
				if reservation.ApplicationID == claim.ApplicationID &&
					reservation.PRNumber == claim.PRNumber && reservation.Kind == ReservationClose {
					delete(r.reservations, key)
				}
			}
		}
		reserve()
	case ReservationStart:
		switch {
		case closeInFlight:
			result.Retryable = true
		case hasBinding && binding.State == PreviewClosing:
			result.Retryable = true
		case hasBinding && binding.State != PreviewDeleted && binding.HeadSHA == claim.HeadSHA:
			result.Duplicate = true
		default:
			live := hasBinding && binding.State != PreviewDeleted
			if !live && claim.LiveLimit > 0 &&
				r.liveCountLocked(claim.ApplicationID, now, claim.PRNumber) >= claim.LiveLimit {
				result.Limit = true
				return result, nil
			}
			reserve()
		}
	default:
		return PreviewClaimResult{}, fmt.Errorf("fake: unknown preview claim kind %q", claim.Kind)
	}
	return result, nil
}

func (r *fakeRepository) liveCountLocked(appID uuid.UUID, now time.Time, excludePR int) int {
	prs := make(map[int]bool)
	for _, preview := range r.previews {
		if preview.ApplicationID == appID && preview.State != PreviewDeleted && preview.PRNumber != excludePR {
			prs[preview.PRNumber] = true
		}
	}
	for _, reservation := range r.reservations {
		if reservation.ApplicationID == appID && reservation.Kind == ReservationStart &&
			reservation.PRNumber != excludePR && reservation.ExpiresAt.After(now) {
			prs[reservation.PRNumber] = true
		}
	}
	return len(prs)
}

func (r *fakeRepository) WritePreviewBinding(_ context.Context, write PreviewBindingWrite) (PreviewBindingWriteResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promoteCalls++
	if r.promoteErr != nil && (r.promoteErrAt == 0 || r.promoteCalls == r.promoteErrAt) {
		return PreviewBindingWriteResult{}, r.promoteErr
	}
	now := r.clock()
	lease, ok := r.reservationByIDLocked(write.ReservationID)
	if !ok || !lease.ExpiresAt.After(now) ||
		lease.Kind != ReservationStart ||
		lease.ApplicationID != write.ApplicationID ||
		lease.PRNumber != write.PRNumber ||
		lease.HeadSHA != write.LeaseHeadSHA {
		return PreviewBindingWriteResult{Refused: BindingRefusedLease}, nil
	}

	if r.hasLiveCloseLocked(write.ApplicationID, write.PRNumber, now) {
		return PreviewBindingWriteResult{Refused: BindingRefusedClosing}, nil
	}

	key := previewKey(write.ApplicationID, write.PRNumber)
	binding, hasBinding := r.previews[key]
	if hasBinding && binding.State == PreviewClosing {
		return PreviewBindingWriteResult{Refused: BindingRefusedClosing}, nil
	}
	live := hasBinding && binding.State != PreviewDeleted
	if !live && write.LiveLimit > 0 &&
		r.liveCountLocked(write.ApplicationID, now, write.PRNumber) >= write.LiveLimit {
		return PreviewBindingWriteResult{Refused: BindingRefusedLimit}, nil
	}

	stored := Preview{
		ID:                   uuid.New(),
		ApplicationID:        write.ApplicationID,
		TeamID:               write.TeamID,
		Provider:             write.Provider,
		Repo:                 write.Repo,
		PRNumber:             write.PRNumber,
		Branch:               write.Branch,
		HeadSHA:              write.HeadSHA,
		PreviewApplicationID: write.PreviewApplicationID,
		Host:                 write.Host,
		State:                write.State,
		CreatedAt:            now.UTC(),
		UpdatedAt:            now.UTC(),
	}
	if hasBinding {
		stored.ID = binding.ID
		stored.CreatedAt = binding.CreatedAt
	}
	r.previews[key] = stored
	if write.ConsumeLease {
		for reservationKey, reservation := range r.reservations {
			if reservation.ID == write.ReservationID {
				delete(r.reservations, reservationKey)
			}
		}
	}
	return PreviewBindingWriteResult{Binding: &stored}, nil
}

func (r *fakeRepository) hasCloseMarker(appID uuid.UUID, prNumber int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, reservation := range r.reservations {
		if reservation.ApplicationID == appID && reservation.PRNumber == prNumber &&
			reservation.Kind == ReservationClose {
			return true
		}
	}
	return false
}

func (r *fakeRepository) hasLiveCloseLocked(appID uuid.UUID, prNumber int, now time.Time) bool {
	for _, reservation := range r.reservations {
		if reservation.ApplicationID == appID && reservation.PRNumber == prNumber &&
			reservation.Kind == ReservationClose && reservation.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}

func (r *fakeRepository) reservationByIDLocked(id uuid.UUID) (DeliveryReservation, bool) {
	for _, reservation := range r.reservations {
		if reservation.ID == id {
			return reservation, true
		}
	}
	return DeliveryReservation{}, false
}

func (r *fakeRepository) ReleasePreviewDelivery(_ context.Context, reservationID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unreserveErr != nil {
		return r.unreserveErr
	}
	for key, reservation := range r.reservations {
		if reservation.ID == reservationID {
			delete(r.reservations, key)
		}
	}
	return nil
}

func (r *fakeRepository) ClearPreviewDeliveries(_ context.Context, appID uuid.UUID, prNumber int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clearErr != nil {
		return r.clearErr
	}
	for key, reservation := range r.reservations {
		if reservation.ApplicationID == appID && reservation.PRNumber == prNumber {
			delete(r.reservations, key)
		}
	}
	return nil
}

func (r *fakeRepository) countLiveForTest(appID uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.liveCountLocked(appID, r.clock(), 0)
}

func (r *fakeRepository) ListOrphanedPreviews(_ context.Context) ([]Preview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.orphanErr != nil {
		return nil, r.orphanErr
	}
	if r.orphanPreviews != nil {
		return append([]Preview{}, r.orphanPreviews...), nil
	}
	orphans := make([]Preview, 0)
	for _, preview := range r.previews {
		if preview.State == PreviewDeleted || preview.PreviewApplicationID != uuid.Nil {
			continue
		}
		orphans = append(orphans, preview)
	}
	return orphans, nil
}

func (r *fakeRepository) ListOrphanedPreviewApplications(_ context.Context, _ time.Time) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.orphanAppErr != nil {
		return nil, r.orphanAppErr
	}
	return append([]uuid.UUID{}, r.orphanApps...), nil
}

func (r *fakeRepository) MarkPreviewsDeletedForSibling(_ context.Context, previewAppID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.markSiblingErr != nil {
		return r.markSiblingErr
	}
	for key, preview := range r.previews {
		if preview.PreviewApplicationID != previewAppID || preview.State == PreviewDeleted {
			continue
		}
		preview.State = PreviewDeleted
		preview.DeletedAt = time.Now().UTC()
		preview.UpdatedAt = preview.DeletedAt
		r.previews[key] = preview
		for reservationKey, reservation := range r.reservations {
			if reservation.ApplicationID == preview.ApplicationID && reservation.PRNumber == preview.PRNumber {
				delete(r.reservations, reservationKey)
			}
		}
	}
	return nil
}

func (r *fakeRepository) reservationCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reservations)
}

type fakeInstaller struct {
	mu sync.Mutex

	created   []providers.Webhook
	targets   []providers.HookTarget
	deleted   []string
	createErr error
	deleteErr error
}

func (f *fakeInstaller) CreateWebhook(_ context.Context, target providers.HookTarget, hook providers.Webhook) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created = append(f.created, hook)
	f.targets = append(f.targets, target)
	return uuid.NewString(), nil
}

func (f *fakeInstaller) DeleteWebhook(_ context.Context, _ providers.HookTarget, hookID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, hookID)
	return nil
}

type fakeDeployer struct {
	mu sync.Mutex

	deployed []uuid.UUID
	err      error
	missing  map[uuid.UUID]bool

	provisioned  []deploy.PreviewApplicationInput
	provisionID  uuid.UUID
	provisionErr error
	deleted      []uuid.UUID
	deleteErr    error
}

func (f *fakeDeployer) DeploySystem(_ context.Context, appID uuid.UUID) (deploy.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return deploy.Deployment{}, f.err
	}
	if f.missing[appID] {
		return deploy.Deployment{}, deploy.ErrNotFound
	}
	f.deployed = append(f.deployed, appID)
	return deploy.Deployment{
		ID:            uuid.New(),
		ApplicationID: appID,
		Kind:          deploy.KindDeploy,
		State:         deploy.StateQueued,
	}, nil
}

func (f *fakeDeployer) CreatePreviewApplication(_ context.Context, _ uuid.UUID, in deploy.PreviewApplicationInput) (deploy.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provisionErr != nil {
		return deploy.Application{}, f.provisionErr
	}
	f.provisioned = append(f.provisioned, in)
	f.provisionID = uuid.New()
	return deploy.Application{ID: f.provisionID, Name: in.Name, Branch: in.Branch}, nil
}

func (f *fakeDeployer) DeleteSystemApplication(_ context.Context, appID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, appID)
	return nil
}

func (f *fakeDeployer) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeDeployer) deployCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deployed)
}

func (f *fakeDeployer) provisionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.provisioned)
}

func (f *fakeDeployer) deleteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deleted)
}

type fakeCommenter struct {
	mu sync.Mutex

	numbers []int
	bodies  []string
	targets []providers.HookTarget
	err     error
}

func (f *fakeCommenter) CreatePullRequestComment(_ context.Context, target providers.HookTarget, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.numbers = append(f.numbers, number)
	f.bodies = append(f.bodies, body)
	f.targets = append(f.targets, target)
	return f.err
}

func (f *fakeCommenter) commentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.numbers)
}

func newTestService(repo *fakeRepository, installer *fakeInstaller, deployer *fakeDeployer) *Service {
	return newTestServiceWith(Config{
		Repository:  repo,
		Installer:   installer,
		Deployer:    deployer,
		Provisioner: deployer,
		Logger:      discardLogger(),
	})
}

func newTestServiceWith(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	return NewService(cfg)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var _ Repository = (*fakeRepository)(nil)
var _ Installer = (*fakeInstaller)(nil)
var _ Deployer = (*fakeDeployer)(nil)
