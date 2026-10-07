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
