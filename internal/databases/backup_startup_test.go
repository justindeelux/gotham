package databases

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// hangingContainers simulates a dead agent: ListFresh blocks until the
// caller's context ends, like a gRPC dial to an unresolvable address.
type hangingContainers struct {
	containers.ContainerService
	entered chan struct{}
	once    sync.Once
}

func (h *hangingContainers) ListFresh(ctx context.Context, _ uuid.UUID) ([]containers.Container, error) {
	h.once.Do(func() { close(h.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h *hangingContainers) List(context.Context, uuid.UUID) ([]containers.Container, error) {
	return nil, nil
}

// gatedContainers blocks the first ListFresh until the test releases it,
// simulating a stalled agent list during which a job starts.
type gatedContainers struct {
	containers.ContainerService
	release chan struct{}
	listed  []containers.Container
	mu      sync.Mutex
	removes []string
}

func (g *gatedContainers) ListFresh(ctx context.Context, _ uuid.UUID) ([]containers.Container, error) {
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]containers.Container(nil), g.listed...), nil
}

func (g *gatedContainers) List(context.Context, uuid.UUID) ([]containers.Container, error) {
	return nil, nil
}

func (g *gatedContainers) Remove(_ context.Context, _ uuid.UUID, containerID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removes = append(g.removes, containerID)
	return nil
}

// TestSweepSkipsJobStartedMidList is the JUS-51 race regression: a job that
// claims its lease and writes its run row while the sweep's list is stalled
// must not have its fresh container removed once the list returns.
func TestSweepSkipsJobStartedMidList(t *testing.T) {
	backups := newFakeBackupRepository()
	serverID := uuid.New()
	backups.serverIDs = []uuid.UUID{serverID}
	gated := &gatedContainers{release: make(chan struct{})}

	manager := NewBackupService(BackupConfig{
		Repository:         backups,
		DatabaseRepository: newFakeRepository(),
		Containers:         gated,
		Secret:             testSecret,
		Logger:             discardLogger(),
		LocalDir:           t.TempDir(),
		JobTimeout:         time.Second,
		SchedulerInterval:  time.Hour,
		DisableScheduler:   true,
	})
	t.Cleanup(func() { _ = manager.Close() })

	databaseID := uuid.New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.sweepJobContainers()
	}()

	// The job starts while the list is stalled: lease first, then the run
	// row (the order startRun guarantees), then its container appears.
	if !manager.claim(databaseID) {
		t.Fatal("could not claim the database lease")
	}
	run := backups.seedBackup(Backup{DatabaseID: databaseID, Status: BackupRunning})
	gated.mu.Lock()
	gated.listed = []containers.Container{{ID: "fresh-job", Labels: map[string]string{
		labelManaged:    "true",
		labelRole:       roleBackup,
		labelDatabaseID: databaseID.String(),
		labelBackupID:   run.ID.String(),
	}}}
	gated.mu.Unlock()
	close(gated.release)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("sweep did not finish after the list returned")
	}
	gated.mu.Lock()
	defer gated.mu.Unlock()
	for _, id := range gated.removes {
		if id == "fresh-job" {
			t.Fatal("sweep removed a job container that started mid-sweep")
		}
	}
}

// TestStartupSweepDoesNotBlockConstructor is the JUS-51 regression: the
// constructor must return while the node sweep is still stalled on a dead
// agent, so HTTP startup never waits on agent I/O.
func TestStartupSweepDoesNotBlockConstructor(t *testing.T) {
	backups := newFakeBackupRepository()
	backups.serverIDs = []uuid.UUID{uuid.New()}
	hanging := &hangingContainers{entered: make(chan struct{})}

	done := make(chan BackupService, 1)
	go func() {
		done <- NewDefaultBackupService(BackupConfig{
			Repository:         backups,
			DatabaseRepository: newFakeRepository(),
			Containers:         hanging,
			Secret:             testSecret,
			Logger:             discardLogger(),
			LocalDir:           t.TempDir(),
			JobTimeout:         time.Second,
			SchedulerInterval:  time.Hour,
			DisableScheduler:   true,
		})
	}()

	select {
	case svc := <-done:
		if svc == nil {
			t.Fatal("expected a service, got nil")
		}
		_ = svc.Close()
	case <-time.After(10 * time.Second):
		t.Fatal("NewDefaultBackupService blocked on the dead-agent sweep")
	}

	select {
	case <-hanging.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the background sweep never reached the dead agent")
	}
}
