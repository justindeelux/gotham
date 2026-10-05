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

// ptr boxes a value for the optional update fields.
func ptr[T any](v T) *T { return &v }

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
	// serverTeams maps a registered server to its team (zero = legacy node).
	serverTeams map[uuid.UUID]uuid.UUID

	createErr        error
	getErr           error
	listErr          error
	updateErr        error
	softDeleteErr    error
	secretErr        error
	deleteSecretsErr error
	portErr          error
	serverErr        error
	serverMissing    bool
	// environments holds the seeded environments ResolveEnvironment
	// answers with; resolveErr fails every environment and project
	// resolution.
	environments map[uuid.UUID]EnvironmentRef
	resolveErr   error
	// beforeTargetUpdate, when set, runs inside UpdateDatabaseTarget while
	// the fake holds its lock, so a test can observe the in-flight write
	// (e.g. which job lease the service holds around the update).
	beforeTargetUpdate func(Database)

	// expiredListCalls counts ListExpiredDatabases invocations so a lifecycle
	// test can prove the sweeper loop started (or was refused).
	expiredListCalls int

	// secretFailAt fails the Nth CreateSecret call (1-based) while earlier
	// calls succeed, modelling a partial credential write. 0 disables it.
	secretFailAt int
	secretWrites int
	secretFail   error

	// statusErr, when set, fails UpdateDatabaseStatus. statusErrFor narrows it
	// to one target status (empty = every status), so a test can fail the
	// "running" transition while the "error" transition still succeeds.
	statusErr    error
	statusErrFor Status

	// softDeleteContainerID, when set, records a container id on the row at
	// the moment it is soft-deleted, modelling a provision that persisted its
	// container id between delete's read and its soft-delete.
	softDeleteContainerID string

	// afterCreate runs after a row is stored, letting a test interleave a
	// concurrent action (a delete that races provisioning) deterministically.
	afterCreate func(Database)
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

// present reports whether a row still exists at all, including soft-deleted
// rows (GetDatabase hides those).
func (r *fakeRepository) present(databaseID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.databases[databaseID]
	return ok
}

// expiredCalls returns how many times the retention selection ran.
func (r *fakeRepository) expiredCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.expiredListCalls
}

// CreateDatabase implements Repository.
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

// updateError returns the configured write error, mirroring the shared
// repository failure hook.
func (r *fakeRepository) updateError() error { return r.updateErr }

// liveRow loads a row that is visible to reads, mirroring the SQL fence on
// deleted_at IS NULL: a soft-deleted row is ErrNotFound to every scoped write.
func (r *fakeRepository) liveRow(databaseID uuid.UUID) (Database, bool) {
	stored, ok := r.databases[databaseID]
	if !ok || !r.live(stored) {
		return Database{}, false
	}
	return stored, true
}

// UpdateDatabaseName implements Repository, scoped to the name column so it
// cannot clobber a concurrent status or container-id write.
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

// UpdateDatabaseContainer implements Repository, scoped to the container id.
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

// UpdateDatabaseStatus implements Repository, scoped to the status column.
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

// PublicPortInUse implements Repository.
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

// DeleteDatabaseSecrets implements Repository.
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

// SoftDeleteDatabase implements Repository.
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
	// Model a provisioning write that persisted its container id after the
	// delete read: the returned row carries the id at delete time.
	if r.softDeleteContainerID != "" && database.ContainerID == "" {
		database.ContainerID = r.softDeleteContainerID
	}
	database.Status = StatusDeleting
	database.DeletedAt = time.Now().UTC()
	r.databases[databaseID] = database
	return database, nil
}

// ListExpiredDatabases implements Repository: soft-deleted rows whose grace
// window ended at or before cutoff, oldest deletion first.
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

// PurgeDatabase implements Repository: hard-delete the row and its secrets.
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

// CreateSecret implements Repository. secretFailAt models a partial write: the
// first N-1 secrets are stored, the Nth fails.
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

// ServerExists implements Repository: the node must be known and actionable by
// the caller's active team.
func (r *fakeRepository) ServerExists(_ context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverErr != nil {
		return false, r.serverErr
	}
	if r.serverMissing || !r.servers[serverID] {
		return false, nil
	}
	if err := scope.AuthorizeOptionalTeam(r.serverTeams[serverID], true); err != nil {
		return false, nil
	}
	return true, nil
}

// seedServerForTeam registers a node owned by teamID (the zero UUID seeds a
// legacy shared node).
func (r *fakeRepository) seedServerForTeam(teamID uuid.UUID) uuid.UUID {
	id := r.seedServer()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverTeams == nil {
		r.serverTeams = map[uuid.UUID]uuid.UUID{}
	}
	r.serverTeams[id] = teamID
	return id
}

// UpdateDatabaseTarget mirrors the production write: rename, move and node
// change in one step, with the target-environment collision check.
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
	// The conflict check runs against the effective environment: an
	// unchanged placement arrives Nil (the SQL write is conditional).
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
	// Placement columns arrive Nil when the request leaves them alone (the
	// SQL write is conditional); only a set value moves the row.
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

// ListDatabasesByEnvironment implements Repository.
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

// ListDatabasesByProject implements Repository.
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

// NameInEnvironment implements Repository.
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

// ResolveEnvironment implements Repository.
func (r *fakeRepository) ResolveEnvironment(_ context.Context, environmentID, _ uuid.UUID) (EnvironmentRef, error) {
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

// ResolveProject implements Repository.
func (r *fakeRepository) ResolveProject(_ context.Context, projectID, _ uuid.UUID) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolveErr != nil {
		return uuid.Nil, r.resolveErr
	}
	return projectID, nil
}

// seedEnvironment registers an environment the service accepts on create and
// move; resolveErr fails every resolution instead.
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

// environmentProject returns the project of a seeded environment (zero for
// unknown IDs). Callers hold r.mu.
func (r *fakeRepository) environmentProject(environmentID uuid.UUID) uuid.UUID {
	if ref, ok := r.environments[environmentID]; ok {
		return ref.ProjectID
	}
	return uuid.Nil
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
	// removeServers records the server id of every Remove call, so a test can
	// prove cleanup used the real node id rather than a zeroed row.
	removeServers []uuid.UUID
	// calls records container operations in order ("pull", "run", "remove",
	// ...) so ordering (pull before run) is assertable.
	calls []string
	// afterRun runs after Run published its container, letting a test
	// interleave a delete with provisioning deterministically.
	afterRun func()

	// volumeRemoves records every RemoveVolume name; volumeErr fails the call.
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

	// listed is the container list the health wait observes. An empty state
	// means "the container is not running yet".
	listed []containers.Container
	// freshLists counts ListFresh calls (the uncached read the backup sweep
	// must use).
	freshLists int

	// suppressRunning keeps Run from publishing the container as running, so
	// a test can drive the healthcheck timeout.
	suppressRunning bool

	// logs is the payload Logs hands back; logFn overrides it per container
	// so a job flow that runs several containers can script each one. logErr
	// fails the call instead. logIDs records every container asked for.
	// logStreamErr is delivered on the streamErr channel after the chunks, so a
	// test can drive a mid-stream agent failure.
	logs         [][]byte
	logFn        func(opts containers.RunOptions) [][]byte
	logErr       error
	logStreamErr error
	logIDs       []string
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

// ListFresh serves the uncached read the backup sweep prefers, recording the
// call so a test can prove the sweep did not read the List cache.
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
	// Start makes a known container running again, so a Stop-then-Start
	// sequence (and the health wait after it) sees the real state.
	f.setState(containerID, "running")
	return f.startErr
}

func (f *fakeContainers) Stop(_ context.Context, _ uuid.UUID, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	// Observe the stop so an "observe the database after stopping it"
	// regression cannot hide behind a still-"running" listed state.
	f.setState(containerID, "exited")
	return f.stopErr
}

// setState updates the observed state of a listed container. A container that
// was never published is left alone.
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

// Run records the payload and publishes the new container as running, which is
// what the health wait looks for.
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

// Logs streams the configured payload and closes the channel, mirroring the
// agent's behaviour of ending the stream when the container exits. logFn, when
// set, picks the payload from the options of the container's run — job flows
// start several containers in a row and each one answers differently.
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

// lastRun returns the most recent run payload.
func (f *fakeContainers) lastRun() containers.RunOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) == 0 {
		return containers.RunOptions{}
	}
	return f.runs[len(f.runs)-1]
}
