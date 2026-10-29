package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/jobguard"
)

func TestLifecycleIsRefusedDuringABackup(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	svc := newNodeService(t, repo, node)
	svc.backupActive = func(resourceID uuid.UUID) bool { return resourceID == app.ID }
	seedDeployment(t, repo, app, Deployment{State: StateRunning, ContainerID: node.containerID})

	if _, err := svc.Stop(context.Background(), userID, app.ID); !errors.Is(err, ErrBackupInFlight) {
		t.Fatalf("Stop err = %v, want ErrBackupInFlight", err)
	}
	if _, err := svc.Start(context.Background(), userID, app.ID); !errors.Is(err, ErrBackupInFlight) {
		t.Fatalf("Start err = %v, want ErrBackupInFlight", err)
	}
	if _, err := svc.DeploySystem(context.Background(), app.ID); !errors.Is(err, ErrBackupInFlight) {
		t.Fatalf("DeploySystem err = %v, want ErrBackupInFlight", err)
	}
	if node.stopCalls != 0 || node.startCalls != 0 {
		t.Fatalf("stop = %d, start = %d; a refused request must not reach the node",
			node.stopCalls, node.startCalls)
	}

	// The job's own stop/start carries an internal context and must pass.
	if _, err := svc.Stop(jobguard.WithInternal(context.Background()), userID, app.ID); err != nil {
		t.Fatalf("internal Stop: %v", err)
	}
	if node.stopCalls != 1 {
		t.Fatalf("stop = %d; want the job's own stop to reach the node", node.stopCalls)
	}
}
