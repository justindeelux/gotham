package databases

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

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

type gatedContainers struct {
	containers.ContainerService
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	listed  []containers.Container
	mu      sync.Mutex
	removes []string
}

func (g *gatedContainers) ListFresh(ctx context.Context, _ uuid.UUID) ([]containers.Container, error) {
	g.once.Do(func() { close(g.entered) })
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

func TestSweepSkipsJobStartedMidList(t *testing.T) {
	backups := newFakeBackupRepository()
	serverID := uuid.New()
	backups.serverIDs = []uuid.UUID{serverID}
	gated := &gatedContainers{release: make(chan struct{}), entered: make(chan struct{})}

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

	select {
	case <-gated.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("sweep never reached ListFresh")
	}
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
