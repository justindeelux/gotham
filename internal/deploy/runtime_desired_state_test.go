package deploy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingRuntimeMonitor struct {
	mu      sync.Mutex
	desired string
}

func (m *recordingRuntimeMonitor) SetDesiredState(_ context.Context, _ string, _ uuid.UUID, desired string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.desired = desired
	return nil
}

func (m *recordingRuntimeMonitor) SetSettings(context.Context, string, uuid.UUID, *bool, *int) error {
	return nil
}

func (m *recordingRuntimeMonitor) CloseResource(context.Context, string, uuid.UUID, string) {}

func (m *recordingRuntimeMonitor) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.desired
}

func TestAcceptedDeployPathsClearIntentionalStop(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, svc *Service, app Application) error
	}{
		{
			name: "deploy",
			run: func(t *testing.T, svc *Service, app Application) error {
				_, err := svc.Deploy(context.Background(), app.UserID, app.ID)
				return err
			},
		},
		{
			name: "webhook deploy system",
			run: func(t *testing.T, svc *Service, app Application) error {
				_, err := svc.DeploySystem(context.Background(), app.ID)
				return err
			},
		},
		{
			name: "rollback",
			run: func(t *testing.T, svc *Service, app Application) error {
				repo := svc.repo.(*fakeRepository)
				target := seedDeployment(t, repo, app, Deployment{
					Kind: KindDeploy, State: StateRunning, ImageTag: "app:1",
				})
				_, err := svc.Rollback(context.Background(), app.UserID, app.ID, target.ID)
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			app := testImageApplication(t, userID)
			repo := &fakeRepository{app: app}
			svc := newTestService(t, repo)
			monitor := &recordingRuntimeMonitor{desired: "stopped"}
			svc.runtimeMonitor = monitor

			if err := tc.run(t, svc, app); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := monitor.last(); got != "running" {
				t.Fatalf("%s left desired state %q, want running", tc.name, got)
			}
		})
	}
}
