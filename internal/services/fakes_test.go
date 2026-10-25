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

func TestMain(m *testing.M) {
	_ = os.Unsetenv(FeatureEnv)
	os.Exit(m.Run())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

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
	environments  map[uuid.UUID]EnvironmentRef
	resolveErr    error
	activeDeploy  map[uuid.UUID]bool
}

var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		services: make(map[uuid.UUID]Service),
		deploys:  make(map[uuid.UUID][]Deploy),
		servers:  make(map[uuid.UUID]bool),
	}
}

func (r *fakeRepository) seedServer() uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := uuid.New()
	r.servers[id] = true
	return id
}

func (r *fakeRepository) live(service Service) bool {
	return service.DeletedAt.IsZero()
}

func (r *fakeRepository) CreateService(_ context.Context, service Service) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Service{}, r.createErr
	}
	for _, id := range r.order {
		existing := r.services[id]
		if existing.EnvironmentID == service.EnvironmentID && existing.Name == service.Name && r.live(existing) {
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

func (r *fakeRepository) ListServices(_ context.Context, userID uuid.UUID) ([]Service, error) {
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

func (r *fakeRepository) UpdateServiceConfig(_ context.Context, service Service) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return Service{}, r.updateErr
	}
	existing, ok := r.services[service.ID]
	if !ok || !r.live(existing) {
		return Service{}, ErrNotFound
	}
	effectiveEnv := service.EnvironmentID
	if effectiveEnv == uuid.Nil {
		effectiveEnv = existing.EnvironmentID
	}
	for _, id := range r.order {
		other := r.services[id]
		if other.EnvironmentID == effectiveEnv && other.Name == service.Name && other.ID != service.ID && r.live(other) {
			return Service{}, ErrConflict
		}
	}
	existing.Name = service.Name
	existing.ComposeYAML = service.ComposeYAML
	existing.Env = service.Env
	if service.EnvironmentID != uuid.Nil {
		existing.EnvironmentID = service.EnvironmentID
	}
	if service.ServerID != uuid.Nil {
		existing.ServerID = service.ServerID
	}
	r.services[service.ID] = existing
	return existing, nil
}

func (r *fakeRepository) UpdateServiceStatus(_ context.Context, serviceID uuid.UUID, status Status) (Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	service, ok := r.services[serviceID]
	if !ok || !r.live(service) {
		return Service{}, ErrNotFound
	}
	service.Status = status
	r.services[serviceID] = service
	return service, nil
}

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

func (r *fakeRepository) ListServiceDeploys(_ context.Context, serviceID uuid.UUID, limit int32) ([]Deploy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deploys := r.deploys[serviceID]
	if int32(len(deploys)) > limit {
		deploys = deploys[:limit]
	}
	return append([]Deploy{}, deploys...), nil
}

func (r *fakeRepository) ServerExists(_ context.Context, serverID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverErr != nil {
		return false, r.serverErr
	}
	if !r.servers[serverID] {
		return false, nil
	}
	return true, nil
}

func (r *fakeRepository) seedEnvironment() (uuid.UUID, uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.environments == nil {
		r.environments = map[uuid.UUID]EnvironmentRef{}
	}
	envID, projectID := uuid.New(), uuid.New()
	r.environments[envID] = EnvironmentRef{ID: envID, ProjectID: projectID, Name: "env"}
	return envID, projectID
}

func (r *fakeRepository) ListServicesByEnvironment(_ context.Context, environmentID uuid.UUID) ([]Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	services := []Service{}
	for i := len(r.order) - 1; i >= 0; i-- {
		service := r.services[r.order[i]]
		if r.live(service) && service.EnvironmentID == environmentID {
			services = append(services, service)
		}
	}
	return services, nil
}

func (r *fakeRepository) ListServicesByProject(_ context.Context, projectID uuid.UUID) ([]Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	services := []Service{}
	for i := len(r.order) - 1; i >= 0; i-- {
		service := r.services[r.order[i]]
		if r.live(service) && r.environmentProject(service.EnvironmentID) == projectID {
			services = append(services, service)
		}
	}
	return services, nil
}

func (r *fakeRepository) NameInEnvironment(_ context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.order {
		service := r.services[id]
		if service.ID != exceptID && service.EnvironmentID == environmentID && service.Name == name && r.live(service) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepository) ResolveEnvironment(_ context.Context, environmentID uuid.UUID) (EnvironmentRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolveErr != nil {
		return EnvironmentRef{}, r.resolveErr
	}
	if ref, ok := r.environments[environmentID]; ok {
		return ref, nil
	}
	return EnvironmentRef{ID: environmentID}, nil
}

func (r *fakeRepository) ResolveProject(_ context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolveErr != nil {
		return uuid.Nil, r.resolveErr
	}
	return projectID, nil
}

func (r *fakeRepository) HasActiveDeploy(_ context.Context, serviceID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeDeploy[serviceID] {
		return true, nil
	}
	for _, deploy := range r.deploys[serviceID] {
		if deploy.State == DeployDeploying {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepository) HasDeploys(_ context.Context, serviceID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.deploys[serviceID]) > 0, nil
}

func (r *fakeRepository) environmentProject(environmentID uuid.UUID) uuid.UUID {
	if ref, ok := r.environments[environmentID]; ok {
		return ref.ProjectID
	}
	return uuid.Nil
}

type fakeAgent struct {
	mu sync.Mutex

	validateErr error
	upErr       error
	downErr     error
	psErr       error
	logsErr     error

	services  []string
	streamErr error
	validated []string
	ups       []fakeUp
	downs     []fakeDown
	psCalls   []string
	logCalls  []fakeLogs
	chunks    [][]byte
	closes    int
	upGate    chan struct{}
	downGate  chan struct{}
}

type fakeUp struct {
	project string
	yaml    string
	restart bool
}

type fakeDown struct {
	project string
	yaml    string
}

type fakeLogs struct {
	project string
	service string
	tail    int64
	follow  bool
}

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
	a.ups = append(a.ups, fakeUp{project: project, yaml: string(composeYAML), restart: restart})
	gate := a.upGate
	err := a.upErr
	a.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
}

func (a *fakeAgent) Down(_ context.Context, project string, composeYAML []byte) error {
	a.mu.Lock()
	a.downs = append(a.downs, fakeDown{project: project, yaml: string(composeYAML)})
	gate := a.downGate
	err := a.downErr
	a.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
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

func (a *fakeAgent) Logs(_ context.Context, project, service string, tail int64, follow bool) (LogStream, error) {
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
	stream := &fakeLogStream{chunks: chunks, streamErr: a.streamErr, close: func() {
		a.mu.Lock()
		a.closes++
		a.mu.Unlock()
	}}
	return stream, nil
}

func (a *fakeAgent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closes++
	return nil
}

func (a *fakeAgent) currentCloses() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closes
}

type fakeLogStream struct {
	chunks    <-chan []byte
	streamErr error
	close     func()
}

func (s *fakeLogStream) Chunks() <-chan []byte { return s.chunks }
func (s *fakeLogStream) Err() error            { return s.streamErr }
func (s *fakeLogStream) Close() error {
	if s.close != nil {
		s.close()
	}
	return nil
}

func (a *fakeAgent) lastUp(t *testing.T) fakeUp {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.ups) == 0 {
		t.Fatalf("no Up call recorded")
	}
	return a.ups[len(a.ups)-1]
}

type fakeRouteSync struct {
	mu        sync.Mutex
	serverIDs []uuid.UUID
	err       error
}

func (f *fakeRouteSync) SyncServer(_ context.Context, serverID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serverIDs = append(f.serverIDs, serverID)
	return f.err
}

func (f *fakeRouteSync) calls() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID{}, f.serverIDs...)
}

func nowUTC() time.Time { return time.Now().UTC() }
