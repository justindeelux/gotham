package services

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMain clears the feature flag so the suite runs with the services surface
// enabled; individual tests override it with t.Setenv.
func TestMain(m *testing.M) {
	_ = os.Unsetenv(FeatureEnv)
	os.Exit(m.Run())
}

// discardLogger keeps the service's operational logging out of the test
// output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeRepository is a scriptable in-memory Repository. Rows live in insertion
// order so lists stay deterministic, and soft deletes flip a flag instead of
// dropping the row — exactly the visibility rule the SQL queries implement.
type fakeRepository struct {
	mu       sync.Mutex
	services map[uuid.UUID]Service
	order    []uuid.UUID
	deploys  map[uuid.UUID][]Deploy
	servers  map[uuid.UUID]bool

	createErr     error
	getErr        error
	listErr       error
	updateErr     error
	softDeleteErr error
	deployErr     error
	serverErr     error
}

// Compile-time guarantee that fakeRepository satisfies the seam.
var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		services: make(map[uuid.UUID]Service),
		deploys:  make(map[uuid.UUID][]Deploy),
		servers:  make(map[uuid.UUID]bool),
	}
}

// seedServer registers a server the service can deploy onto.
func (r *fakeRepository) seedServer() uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := uuid.New()
	r.servers[id] = true
	return id
}

// live reports whether a row is visible to reads (not soft-deleted).
func (r *fakeRepository) live(service Service) bool {
	return service.DeletedAt.IsZero()
}

// CreateService implements Repository.
func (r *fakeRepository) CreateService(_ context.Context, service Service) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Service{}, r.createErr
	}
	for _, id := range r.order {
		existing := r.services[id]
		if existing.UserID == service.UserID && existing.Name == service.Name && r.live(existing) {
			return Service{}, ErrConflict
		}
	}
	if service.ID == uuid.Nil {
		service.ID = uuid.New()
	}
	r.services[service.ID] = service
	r.order = append(r.order, service.ID)
	return service, nil
}

// GetService implements Repository.
func (r *fakeRepository) GetService(_ context.Context, serviceID uuid.UUID) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return Service{}, r.getErr
	}
	service, ok := r.services[serviceID]
	if !ok || !r.live(service) {
		return Service{}, ErrNotFound
	}
	return service, nil
}

// ListServicesByUser implements Repository.
func (r *fakeRepository) ListServicesByUser(_ context.Context, userID uuid.UUID) ([]Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	services := []Service{}
	for i := len(r.order) - 1; i >= 0; i-- {
		service := r.services[r.order[i]]
		if service.UserID == userID && r.live(service) {
			services = append(services, service)
		}
	}
	return services, nil
}

// UpdateService implements Repository.
func (r *fakeRepository) UpdateService(_ context.Context, service Service) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return Service{}, r.updateErr
	}
	if _, ok := r.services[service.ID]; !ok || !r.live(r.services[service.ID]) {
		return Service{}, ErrNotFound
	}
	r.services[service.ID] = service
	return service, nil
}

// SoftDeleteService implements Repository.
func (r *fakeRepository) SoftDeleteService(_ context.Context, serviceID uuid.UUID) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.softDeleteErr != nil {
		return Service{}, r.softDeleteErr
	}
	service, ok := r.services[serviceID]
	if !ok || !r.live(service) {
		return Service{}, ErrNotFound
	}
	service.DeletedAt = nowUTC()
	service.Status = StatusDeleting
	r.services[serviceID] = service
	return service, nil
}

// CreateServiceDeploy implements Repository.
func (r *fakeRepository) CreateServiceDeploy(_ context.Context, deploy Deploy) (Deploy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deployErr != nil {
		return Deploy{}, r.deployErr
	}
	if deploy.ID == uuid.Nil {
		deploy.ID = uuid.New()
	}
	r.deploys[deploy.ServiceID] = append([]Deploy{deploy}, r.deploys[deploy.ServiceID]...)
	return deploy, nil
}

// UpdateServiceDeploy implements Repository.
func (r *fakeRepository) UpdateServiceDeploy(_ context.Context, deploy Deploy) (Deploy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deploys := r.deploys[deploy.ServiceID]
	for i, existing := range deploys {
		if existing.ID == deploy.ID {
			deploys[i] = deploy
			r.deploys[deploy.ServiceID] = deploys
			return deploy, nil
		}
	}
	return Deploy{}, ErrNotFound
}

// ListServiceDeploys implements Repository.
func (r *fakeRepository) ListServiceDeploys(_ context.Context, serviceID uuid.UUID, limit int32) ([]Deploy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deploys := r.deploys[serviceID]
	if int32(len(deploys)) > limit {
		deploys = deploys[:limit]
	}
	return append([]Deploy{}, deploys...), nil
}

// ServerExists implements Repository.
func (r *fakeRepository) ServerExists(_ context.Context, serverID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverErr != nil {
		return false, r.serverErr
	}
	return r.servers[serverID], nil
}

// fakeAgent is a scriptable ComposeAgent recording every call.
type fakeAgent struct {
	mu sync.Mutex

	validateErr error
	upErr       error
	downErr     error
	psErr       error
	logsErr     error

	services  []string
	validated []string
	ups       []fakeUp
	downs     []string
	psCalls   []string
	logCalls  []fakeLogs
	chunks    [][]byte
}

type fakeUp struct {
	project string
	yaml    string
	restart bool
}

type fakeLogs struct {
	project string
	service string
	tail    int64
	follow  bool
}

// Compile-time guarantee that fakeAgent satisfies the seam.
var _ ComposeAgent = (*fakeAgent)(nil)

func (a *fakeAgent) Validate(_ context.Context, project string, composeYAML []byte) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.validated = append(a.validated, project)
	if a.validateErr != nil {
		return nil, a.validateErr
	}
	if a.services == nil {
		return []string{"web", "worker"}, nil
	}
	return a.services, nil
}

func (a *fakeAgent) Up(_ context.Context, project string, composeYAML []byte, restart bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ups = append(a.ups, fakeUp{project: project, yaml: string(composeYAML), restart: restart})
	return a.upErr
}

func (a *fakeAgent) Down(_ context.Context, project string, composeYAML []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.downs = append(a.downs, project)
	return a.downErr
}

func (a *fakeAgent) Ps(_ context.Context, project string) ([]ComposeContainer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.psCalls = append(a.psCalls, project)
	if a.psErr != nil {
		return nil, a.psErr
	}
	return []ComposeContainer{{Service: "web", State: "running"}}, nil
}

func (a *fakeAgent) Logs(_ context.Context, project, service string, tail int64, follow bool) (<-chan []byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logCalls = append(a.logCalls, fakeLogs{project: project, service: service, tail: tail, follow: follow})
	if a.logsErr != nil {
		return nil, a.logsErr
	}
	chunks := make(chan []byte, len(a.chunks))
	for _, chunk := range a.chunks {
		chunks <- chunk
	}
	close(chunks)
	return chunks, nil
}

// lastUp returns the most recent Up call.
func (a *fakeAgent) lastUp(t *testing.T) fakeUp {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.ups) == 0 {
		t.Fatalf("no Up call recorded")
	}
	return a.ups[len(a.ups)-1]
}

// nowUTC is a tiny indirection so tests do not import time just for a
// timestamp.
func nowUTC() time.Time { return time.Now().UTC() }
