package webhooks

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

// fakeRepository is a scriptable in-memory Repository.
type fakeRepository struct {
	mu sync.Mutex

	app    Application
	hook   *Hook
	secret string
	target *Target

	// claims keys a delivery by commit SHA (or delivery ID when the body has
	// no SHA) the way the partial unique indexes do.
	claims   map[string]Event
	released []uuid.UUID

	getAppErr    error
	createErr    error
	targetsErr   error
	claimErr     error
	linkErr      error
	releaseErr   error
	createCalls  int
	deleteCalls  int
	targetsCalls int
}

// newFakeRepository returns a repository holding one application on the main
// branch with no hook installed yet.
func newFakeRepository() *fakeRepository {
	return newFakeRepositoryFor(providers.NameGitHub)
}

// newFakeRepositoryFor returns a repository whose application is watched by
// the given provider.
func newFakeRepositoryFor(provider string) *fakeRepository {
	return &fakeRepository{
		app: Application{
			ID:       uuid.New(),
			UserID:   uuid.New(),
			Provider: provider,
			Repo:     "octo/gotham",
			Branch:   "main",
			CloneURL: "https://github.com/octo/gotham.git",
		},
		claims: make(map[string]Event),
	}
}

// testHookSecret is the signing secret every fake target carries; the tests
// sign their deliveries with the same value.
const testHookSecret = "hook-secret"

// withTarget installs the hook and delivery target of the application.
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
		Provider:      r.app.Provider,
		Repo:          r.app.Repo,
		Branch:        r.app.Branch,
		CloneURL:      r.app.CloneURL,
		HookID:        r.hook.HookID,
		Secret:        secret,
		URL:           r.hook.URL,
	}
	return r
}

// GetApplication implements Repository.
func (r *fakeRepository) GetApplication(_ context.Context, appID, userID uuid.UUID) (Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getAppErr != nil {
		return Application{}, r.getAppErr
	}
	if appID != r.app.ID || userID != r.app.UserID {
		return Application{}, ErrNotFound
	}
	return r.app, nil
}

// GetWebhook implements Repository.
func (r *fakeRepository) GetWebhook(_ context.Context, appID uuid.UUID) (Hook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hook == nil || r.hook.ApplicationID != appID {
		return Hook{}, ErrNotFound
	}
	return *r.hook, nil
}

// CreateWebhook implements Repository, keeping the secret unsealed so tests
// can read it back.
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
		Provider:      hook.Provider,
		Repo:          hook.Repo,
		Branch:        r.app.Branch,
		CloneURL:      r.app.CloneURL,
		HookID:        hook.HookID,
		Secret:        secret,
		URL:           hook.URL,
	}
	return hook, nil
}

// DeleteWebhook implements Repository.
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

// Targets implements Repository, honouring the case-insensitive repo match.
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

// ClaimEvent implements Repository with the same uniqueness the database has.
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
		key = uuid.NewString() // nothing to dedupe by: every delivery is new
	}
	if _, taken := r.claims[key]; taken {
		return Event{}, ErrDuplicate
	}
	event.ID = uuid.New()
	event.ReceivedAt = time.Now().UTC()
	r.claims[key] = event
	return event, nil
}

// ReleaseEvent implements Repository.
func (r *fakeRepository) ReleaseEvent(_ context.Context, eventID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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

// LinkEventDeployment implements Repository.
func (r *fakeRepository) LinkEventDeployment(_ context.Context, eventID, deploymentID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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

// claimCount reports how many deliveries are currently claimed.
func (r *fakeRepository) claimCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.claims)
}

// fakeInstaller records hook installations instead of calling a Git host.
type fakeInstaller struct {
	mu sync.Mutex

	created   []providers.Webhook
	targets   []providers.HookTarget
	deleted   []string
	createErr error
	deleteErr error
}

// CreateWebhook implements Installer.
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

// DeleteWebhook implements Installer.
func (f *fakeInstaller) DeleteWebhook(_ context.Context, _ providers.HookTarget, hookID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, hookID)
	return nil
}

// fakeDeployer records queued deployments instead of running the state
// machine.
type fakeDeployer struct {
	mu sync.Mutex

	deployed []uuid.UUID
	err      error
}

// DeploySystem implements Deployer.
func (f *fakeDeployer) DeploySystem(_ context.Context, appID uuid.UUID) (deploy.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return deploy.Deployment{}, f.err
	}
	f.deployed = append(f.deployed, appID)
	return deploy.Deployment{
		ID:            uuid.New(),
		ApplicationID: appID,
		Kind:          deploy.KindDeploy,
		State:         deploy.StateQueued,
	}, nil
}

// deployCount reports how many deployments were queued.
func (f *fakeDeployer) deployCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deployed)
}

// newTestService builds a Service over the given seams with a generous rate
// limit; tests that care about the limit build their own.
func newTestService(repo *fakeRepository, installer *fakeInstaller, deployer *fakeDeployer) *Service {
	return NewService(Config{
		Repository: repo,
		Installer:  installer,
		Deployer:   deployer,
		Logger:     discardLogger(),
	})
}

// discardLogger keeps operational logging out of the test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var _ Repository = (*fakeRepository)(nil)
var _ Installer = (*fakeInstaller)(nil)
var _ Deployer = (*fakeDeployer)(nil)
