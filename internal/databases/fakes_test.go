package databases

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/teams"
)

// testSecret is the key the suite seals credentials with. It is a fixed value
// so assertions can open ciphertext without a configured secret.
const testSecret = "gotham-test-secret"

// TestMain clears the feature flag so the suite runs with the databases
// surface enabled; individual tests override it with t.Setenv.
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
	mu        sync.Mutex
	databases map[uuid.UUID]Database
	order     []uuid.UUID
	secrets   map[uuid.UUID][]Secret
	servers   map[uuid.UUID]bool

	createErr     error
	getErr        error
	listErr       error
	updateErr     error
	softDeleteErr error
	secretErr     error
	serverErr     error
	serverMissing bool
}

// Compile-time guarantee that fakeRepository satisfies the seam.
var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		databases: make(map[uuid.UUID]Database),
		secrets:   make(map[uuid.UUID][]Secret),
		servers:   make(map[uuid.UUID]bool),
	}
}

// seedServer registers a server the service can provision onto.
func (r *fakeRepository) seedServer() uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := uuid.New()
	r.servers[id] = true
	return id
}

// seed stores a database directly, bypassing creation semantics.
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

// live reports whether a row is visible to reads (not soft-deleted).
func (r *fakeRepository) live(database Database) bool {
	return database.DeletedAt.IsZero()
}

// CreateDatabase implements Repository.
func (r *fakeRepository) CreateDatabase(_ context.Context, database Database) (Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Database{}, r.createErr
	}
	for _, id := range r.order {
		existing := r.databases[id]
		if existing.UserID == database.UserID && existing.Name == database.Name && r.live(existing) {
			return Database{}, ErrConflict
		}
	}
	if database.ID == uuid.Nil {
		database.ID = uuid.New()
	}
	r.databases[database.ID] = database
	r.order = append(r.order, database.ID)
	return database, nil
}

// GetDatabase implements Repository.
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

// ListDatabases implements Repository: the active team's databases, or the
// creator's when the scope has no team context (pre-teams behavior).
func (r *fakeRepository) ListDatabases(_ context.Context, scope teams.Scope) ([]Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	live := make([]Database, 0, len(r.order))
	for i := len(r.order) - 1; i >= 0; i-- { // newest first
		database := r.databases[r.order[i]]
		if scope.Active() {
			if database.TeamID == scope.TeamID && r.live(database) {
				live = append(live, database)
			}
			continue
		}
		if database.UserID == scope.UserID && r.live(database) {
			live = append(live, database)
		}
	}
	return live, nil
}

// UpdateDatabase implements Repository.
func (r *fakeRepository) UpdateDatabase(_ context.Context, database Database) (Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return Database{}, r.updateErr
	}
	stored, ok := r.databases[database.ID]
	if !ok || !r.live(stored) {
		return Database{}, ErrNotFound
	}
	for _, id := range r.order {
		existing := r.databases[id]
		if id != database.ID && existing.UserID == database.UserID &&
			existing.Name == database.Name && r.live(existing) {
			return Database{}, ErrConflict
		}
	}
	database.UpdatedAt = stored.UpdatedAt
	r.databases[database.ID] = database
	return database, nil
}

// SoftDeleteDatabase implements Repository.
func (r *fakeRepository) SoftDeleteDatabase(_ context.Context, databaseID uuid.UUID) (Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.softDeleteErr != nil {
		return Database{}, r.softDeleteErr
	}
	database, ok := r.databases[databaseID]
	if !ok || !r.live(database) {
		return Database{}, ErrNotFound
	}
	database.Status = StatusDeleting
	database.DeletedAt = time.Now().UTC()
	r.databases[databaseID] = database
	return database, nil
}

// CreateSecret implements Repository.
func (r *fakeRepository) CreateSecret(_ context.Context, secret Secret) (Secret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.secretErr != nil {
		return Secret{}, r.secretErr
	}
	if secret.ID == uuid.Nil {
		secret.ID = uuid.New()
	}
	r.secrets[secret.DatabaseID] = append(r.secrets[secret.DatabaseID], secret)
	return secret, nil
}

// ListSecrets implements Repository.
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

// ServerExists implements Repository.
func (r *fakeRepository) ServerExists(_ context.Context, serverID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverErr != nil {
		return false, r.serverErr
	}
	if r.serverMissing {
		return false, nil
	}
	return r.servers[serverID], nil
}

// fakeContainers is a scriptable containers.ContainerService: it records every
// call, hands back a configurable container list for health waits, and never
// reaches an agent or a Docker daemon.
type fakeContainers struct {
	mu sync.Mutex

	runs     []containers.RunOptions
	starts   int
	stops    int
	restarts int
	removes  []string
	pulls    int

	runID      string
	runErr     error
	startErr   error
	stopErr    error
	restartErr error
	removeErr  error
	listErr    error
	pullErr    error

	// listed is the container list the health wait observes. An empty state
	// means "the container is not running yet".
	listed []containers.Container

	// suppressRunning keeps Run from publishing the container as running, so
	// a test can drive the healthcheck timeout.
	suppressRunning bool

	// logs is the payload Logs hands back; logFn overrides it per container
	// so a job flow that runs several containers can script each one. logErr
	// fails the call instead. logIDs records every container asked for.
	logs   [][]byte
	logFn  func(opts containers.RunOptions) [][]byte
	logErr error
	logIDs []string
}

// Compile-time guarantee that fakeContainers satisfies the seam.
var _ containers.ContainerService = (*fakeContainers)(nil)

// setRunning publishes the container with id in the observed list.
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

func (f *fakeContainers) Start(context.Context, uuid.UUID, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return f.startErr
}

func (f *fakeContainers) Stop(context.Context, uuid.UUID, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return f.stopErr
}

func (f *fakeContainers) Restart(context.Context, uuid.UUID, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	return f.restartErr
}

func (f *fakeContainers) Remove(_ context.Context, _ uuid.UUID, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, containerID)
	return f.removeErr
}

func (f *fakeContainers) Pull(context.Context, uuid.UUID, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulls++
	return f.pullErr
}

// Run records the payload and publishes the new container as running, which is
// what the health wait looks for.
func (f *fakeContainers) Run(_ context.Context, _ uuid.UUID, opts containers.RunOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, opts)
	if f.runErr != nil {
		return "", f.runErr
	}
	id := f.runID
	if id == "" {
		id = "container-1"
	}
	if !f.suppressRunning {
		f.listed = []containers.Container{{ID: id, State: "running", Status: "Up"}}
	}
	return id, nil
}

// Logs streams the configured payload and closes the channel, mirroring the
// agent's behaviour of ending the stream when the container exits. logFn, when
// set, picks the payload from the options of the container's run — job flows
// start several containers in a row and each one answers differently.
func (f *fakeContainers) Logs(_ context.Context, _ uuid.UUID, containerID string, _ bool) (<-chan []byte, error) {
	f.mu.Lock()
	f.logIDs = append(f.logIDs, containerID)
	if f.logErr != nil {
		err := f.logErr
		f.mu.Unlock()
		return nil, err
	}
	chunks := f.logs
	fn := f.logFn
	if fn != nil && len(f.runs) > 0 {
		chunks = fn(f.runs[len(f.runs)-1])
	}
	f.mu.Unlock()

	out := make(chan []byte)
	go func() {
		defer close(out)
		for _, chunk := range chunks {
			out <- chunk
		}
	}()
	return out, nil
}

// lastRun returns the most recent run payload.
func (f *fakeContainers) lastRun() containers.RunOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) == 0 {
		return containers.RunOptions{}
	}
	return f.runs[len(f.runs)-1]
}
