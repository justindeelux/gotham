package databases

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

const testSecret = "gotham-test-secret"

func TestMain(m *testing.M) {
	_ = os.Unsetenv(FeatureEnv)
	os.Exit(m.Run())
}

func ptr[T any](v T) *T { return &v }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeRepository struct {
	mu        sync.Mutex
	databases map[uuid.UUID]Database
	order     []uuid.UUID
	secrets   map[uuid.UUID][]Secret
	servers   map[uuid.UUID]bool

	createErr          error
	getErr             error
	listErr            error
	updateErr          error
	softDeleteErr      error
	secretErr          error
	deleteSecretsErr   error
	portErr            error
	serverErr          error
	serverMissing      bool
	environments       map[uuid.UUID]EnvironmentRef
	resolveErr         error
	beforeTargetUpdate func(Database)

	expiredListCalls int

	secretFailAt int
	secretWrites int
	secretFail   error

	statusErr    error
	statusErrFor Status

	softDeleteContainerID string

	afterCreate func(Database)
}

var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		databases: make(map[uuid.UUID]Database),
		secrets:   make(map[uuid.UUID][]Secret),
		servers:   make(map[uuid.UUID]bool),
	}
}

func (r *fakeRepository) seedServer() uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := uuid.New()
	r.servers[id] = true
	return id
}

func (r *fakeRepository) seed(database Database) Database {
	r.mu.Lock()
	defer r.mu.Unlock()
	if database.ID == uuid.Nil {
		database.ID = uuid.New()
	}
	if database.Status == "" {
		database.Status = StatusRunning
	}
	r.databases[database.ID] = database
	r.order = append(r.order, database.ID)
	return database
}

func (r *fakeRepository) live(database Database) bool {
	return database.DeletedAt.IsZero()
}

func (r *fakeRepository) present(databaseID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.databases[databaseID]
	return ok
}

func (r *fakeRepository) expiredCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.expiredListCalls
}

func (r *fakeRepository) CreateDatabase(_ context.Context, database Database) (Database, error) {
	r.mu.Lock()
	if r.createErr != nil {
		r.mu.Unlock()
		return Database{}, r.createErr
	}
	for _, id := range r.order {
		existing := r.databases[id]
		if existing.EnvironmentID == database.EnvironmentID && existing.Name == database.Name && r.live(existing) {
			r.mu.Unlock()
			return Database{}, ErrConflict
		}
	}
	if database.ID == uuid.Nil {
		database.ID = uuid.New()
	}
	r.databases[database.ID] = database
	r.order = append(r.order, database.ID)
	hook := r.afterCreate
	r.mu.Unlock()
	if hook != nil {
		hook(database)
	}
	return database, nil
}

func (r *fakeRepository) GetDatabase(_ context.Context, databaseID uuid.UUID) (Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return Database{}, r.getErr
	}
	database, ok := r.databases[databaseID]
	if !ok || !r.live(database) {
		return Database{}, ErrNotFound
	}
	return database, nil
}

func (r *fakeRepository) ListDatabases(_ context.Context, userID uuid.UUID) ([]Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	live := make([]Database, 0, len(r.order))
	for i := len(r.order) - 1; i >= 0; i-- {
		database := r.databases[r.order[i]]
		if database.UserID == userID && r.live(database) {
			live = append(live, database)
		}
	}
	return live, nil
}

func (r *fakeRepository) updateError() error { return r.updateErr }

func (r *fakeRepository) liveRow(databaseID uuid.UUID) (Database, bool) {
	stored, ok := r.databases[databaseID]
	if !ok || !r.live(stored) {
		return Database{}, false
	}
	return stored, true
}

func (r *fakeRepository) UpdateDatabaseName(ctx context.Context, databaseID uuid.UUID, name string) (Database, error) {
	if err := ctx.Err(); err != nil {
		return Database{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.updateError(); err != nil {
		return Database{}, err
	}
	stored, ok := r.liveRow(databaseID)
	if !ok {
		return Database{}, ErrNotFound
	}
	for _, id := range r.order {
		existing := r.databases[id]
		if id != databaseID && existing.EnvironmentID == stored.EnvironmentID &&
			existing.Name == name && r.live(existing) {
			return Database{}, ErrConflict
		}
	}
	stored.Name = name
	stored.UpdatedAt = time.Now().UTC()
	r.databases[databaseID] = stored
	return stored, nil
}

func (r *fakeRepository) UpdateDatabaseContainer(ctx context.Context, databaseID uuid.UUID, containerID string) (Database, error) {
	if err := ctx.Err(); err != nil {
		return Database{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.updateError(); err != nil {
		return Database{}, err
	}
	stored, ok := r.liveRow(databaseID)
	if !ok {
		return Database{}, ErrNotFound
	}
	stored.ContainerID = containerID
	stored.UpdatedAt = time.Now().UTC()
	r.databases[databaseID] = stored
	return stored, nil
}

func (r *fakeRepository) UpdateDatabaseStatus(ctx context.Context, databaseID uuid.UUID, status Status) (Database, error) {
	if err := ctx.Err(); err != nil {
		return Database{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.updateError(); err != nil {
		return Database{}, err
	}
	if r.statusErr != nil && (r.statusErrFor == "" || r.statusErrFor == status) {
		return Database{}, r.statusErr
	}
	stored, ok := r.liveRow(databaseID)
	if !ok {
		return Database{}, ErrNotFound
	}
	stored.Status = status
	stored.UpdatedAt = time.Now().UTC()
	r.databases[databaseID] = stored
	return stored, nil
}

func (r *fakeRepository) PublicPortInUse(_ context.Context, serverID uuid.UUID, publicPort int32) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.portErr != nil {
		return false, r.portErr
	}
	for _, id := range r.order {
		existing := r.databases[id]
		if r.live(existing) && existing.ServerID == serverID && existing.PublicPort == publicPort {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepository) DeleteDatabaseSecrets(ctx context.Context, databaseID uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteSecretsErr != nil {
		return r.deleteSecretsErr
	}
	delete(r.secrets, databaseID)
	return nil
}

func (r *fakeRepository) SoftDeleteDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error) {
	if err := ctx.Err(); err != nil {
		return Database{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.softDeleteErr != nil {
		return Database{}, r.softDeleteErr
	}
	database, ok := r.databases[databaseID]
	if !ok || !r.live(database) {
		return Database{}, ErrNotFound
	}
	if r.softDeleteContainerID != "" && database.ContainerID == "" {
		database.ContainerID = r.softDeleteContainerID
	}
	database.Status = StatusDeleting
	database.DeletedAt = time.Now().UTC()
	r.databases[databaseID] = database
	return database, nil
}

func (r *fakeRepository) ListExpiredDatabases(_ context.Context, cutoff time.Time) ([]Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expiredListCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	expired := make([]Database, 0)
	for _, id := range r.order {
		database := r.databases[id]
		if !database.DeletedAt.IsZero() && !database.DeletedAt.After(cutoff) {
			expired = append(expired, database)
		}
	}
	return expired, nil
}

func (r *fakeRepository) PurgeDatabase(_ context.Context, databaseID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.softDeleteErr != nil {
		return r.softDeleteErr
	}
	delete(r.secrets, databaseID)
	delete(r.databases, databaseID)
	for i, id := range r.order {
		if id == databaseID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return nil
}

func (r *fakeRepository) CreateSecret(_ context.Context, secret Secret) (Secret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secretWrites++
	if err := r.secretErr; err != nil {
		return Secret{}, err
	}
	if r.secretFailAt > 0 && r.secretWrites >= r.secretFailAt {
		if r.secretFail != nil {
			return Secret{}, r.secretFail
		}
		return Secret{}, errors.New("databases: secret write failed")
	}
	if secret.ID == uuid.Nil {
		secret.ID = uuid.New()
	}
	r.secrets[secret.DatabaseID] = append(r.secrets[secret.DatabaseID], secret)
	return secret, nil
}

func (r *fakeRepository) ListSecrets(_ context.Context, databaseID uuid.UUID) ([]Secret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secretErr != nil {
		return nil, r.secretErr
	}
	stored := r.secrets[databaseID]
	secrets := make([]Secret, len(stored))
	copy(secrets, stored)
	return secrets, nil
}

func (r *fakeRepository) ServerExists(_ context.Context, serverID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverErr != nil {
		return false, r.serverErr
	}
	if r.serverMissing || !r.servers[serverID] {
		return false, nil
	}
	return true, nil
}

func (r *fakeRepository) UpdateDatabaseTarget(_ context.Context, database Database) (Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeTargetUpdate != nil {
		r.beforeTargetUpdate(database)
	}
	if err := r.updateError(); err != nil {
		return Database{}, err
	}
	stored, ok := r.databases[database.ID]
	if !ok || !r.live(stored) {
		return Database{}, ErrNotFound
	}
	effectiveEnv := database.EnvironmentID
	if effectiveEnv == uuid.Nil {
		effectiveEnv = stored.EnvironmentID
	}
	for _, id := range r.order {
		existing := r.databases[id]
		if id != database.ID && existing.EnvironmentID == effectiveEnv &&
			existing.Name == database.Name && r.live(existing) {
			return Database{}, ErrConflict
		}
	}
	stored.Name = database.Name
	if database.EnvironmentID != uuid.Nil {
		stored.EnvironmentID = database.EnvironmentID
	}
	if database.ServerID != uuid.Nil {
		stored.ServerID = database.ServerID
	}
	stored.UpdatedAt = time.Now().UTC()
	r.databases[database.ID] = stored
	return stored, nil
}

func (r *fakeRepository) ListDatabasesByEnvironment(_ context.Context, environmentID uuid.UUID) ([]Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	databases := []Database{}
	for i := len(r.order) - 1; i >= 0; i-- {
		database := r.databases[r.order[i]]
		if r.live(database) && database.EnvironmentID == environmentID {
			databases = append(databases, database)
		}
	}
	return databases, nil
}

func (r *fakeRepository) ListDatabasesByProject(_ context.Context, projectID uuid.UUID) ([]Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	databases := []Database{}
	for i := len(r.order) - 1; i >= 0; i-- {
		database := r.databases[r.order[i]]
		if r.live(database) && r.environmentProject(database.EnvironmentID) == projectID {
			databases = append(databases, database)
		}
	}
	return databases, nil
}

func (r *fakeRepository) NameInEnvironment(_ context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.order {
		database := r.databases[id]
		if id != exceptID && database.EnvironmentID == environmentID && database.Name == name && r.live(database) {
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

func (r *fakeRepository) environmentProject(environmentID uuid.UUID) uuid.UUID {
	if ref, ok := r.environments[environmentID]; ok {
		return ref.ProjectID
	}
	return uuid.Nil
}

type fakeContainers struct {
	mu sync.Mutex

	runs          []containers.RunOptions
	starts        int
	stops         int
	restarts      int
	removes       []string
	pulls         int
	removeServers []uuid.UUID
	calls         []string
	afterRun      func()

	volumeRemoves []string
	volumeErr     error

	runID      string
	runErr     error
	startErr   error
	stopErr    error
	restartErr error
	removeErr  error
	listErr    error
	pullErr    error

	listed     []containers.Container
	freshLists int

	suppressRunning bool

	logs         [][]byte
	logFn        func(opts containers.RunOptions) [][]byte
	logErr       error
	logStreamErr error
	logIDs       []string
}

var _ containers.ContainerService = (*fakeContainers)(nil)

func (f *fakeContainers) setRunning(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = []containers.Container{{ID: id, State: "running", Status: "Up"}}
}

func (f *fakeContainers) List(context.Context, uuid.UUID) ([]containers.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	list := make([]containers.Container, len(f.listed))
	copy(list, f.listed)
	return list, nil
}

func (f *fakeContainers) ListFresh(context.Context, uuid.UUID) ([]containers.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.freshLists++
	if f.listErr != nil {
		return nil, f.listErr
	}
	list := make([]containers.Container, len(f.listed))
	copy(list, f.listed)
	return list, nil
}

func (f *fakeContainers) Start(_ context.Context, _ uuid.UUID, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "start")
	f.starts++
	f.setState(containerID, "running")
	return f.startErr
}

func (f *fakeContainers) Stop(_ context.Context, _ uuid.UUID, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.setState(containerID, "exited")
	return f.stopErr
}

func (f *fakeContainers) setState(containerID, state string) {
	for i := range f.listed {
		if f.listed[i].ID == containerID {
			f.listed[i].State = state
			return
		}
	}
}

func (f *fakeContainers) Restart(context.Context, uuid.UUID, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	return f.restartErr
}

func (f *fakeContainers) Remove(ctx context.Context, serverID uuid.UUID, containerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "remove")
	f.removes = append(f.removes, containerID)
	f.removeServers = append(f.removeServers, serverID)
	return f.removeErr
}

func (f *fakeContainers) RemoveVolume(_ context.Context, _ uuid.UUID, volumeName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.volumeErr != nil {
		return f.volumeErr
	}
	f.volumeRemoves = append(f.volumeRemoves, volumeName)
	return nil
}

func (f *fakeContainers) Pull(context.Context, uuid.UUID, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "pull")
	f.pulls++
	return f.pullErr
}

func (f *fakeContainers) Run(_ context.Context, _ uuid.UUID, opts containers.RunOptions) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "run")
	f.runs = append(f.runs, opts)
	runErr := f.runErr
	id := f.runID
	if id == "" {
		id = "container-1"
	}
	if runErr == nil && !f.suppressRunning {
		f.listed = []containers.Container{{ID: id, State: "running", Status: "Up"}}
	}
	hook := f.afterRun
	f.mu.Unlock()
	if runErr != nil {
		return "", runErr
	}
	if hook != nil {
		hook()
	}
	return id, nil
}

func (f *fakeContainers) Logs(_ context.Context, _ uuid.UUID, containerID string, _ bool) (<-chan []byte, <-chan error, error) {
	f.mu.Lock()
	f.logIDs = append(f.logIDs, containerID)
	if f.logErr != nil {
		err := f.logErr
		f.mu.Unlock()
		return nil, nil, err
	}
	chunks := f.logs
	fn := f.logFn
	if fn != nil && len(f.runs) > 0 {
		chunks = fn(f.runs[len(f.runs)-1])
	}
	streamFailure := f.logStreamErr
	f.mu.Unlock()

	out := make(chan []byte)
	streamErr := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(streamErr)
		for _, chunk := range chunks {
			out <- chunk
		}
		if streamFailure != nil {
			streamErr <- streamFailure
		}
	}()
	return out, streamErr, nil
}

func (f *fakeContainers) lastRun() containers.RunOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) == 0 {
		return containers.RunOptions{}
	}
	return f.runs[len(f.runs)-1]
}
