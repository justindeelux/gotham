package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestCreateRejectsUnconfinedCompose(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()

	cases := []struct {
		name     string
		document string
	}{
		{
			name:     "privileged",
			document: "services:\n  web:\n    image: nginx:1.27\n    privileged: true\n",
		},
		{
			name:     "host bind",
			document: "services:\n  web:\n    image: nginx:1.27\n    volumes:\n      - /etc/passwd:/etc/passwd:ro\n",
		},
		{
			name:     "docker socket",
			document: "services:\n  web:\n    image: nginx:1.27\n    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), userID, CreateRequest{
				Name:          "web",
				EnvironmentID: envID,
				ServerID:      serverID,
				ComposeYAML:   tc.document,
			})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Create = %v, want ErrValidation", err)
			}
		})
	}
}

func TestGuardServiceDocument(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envManagedVolumeRoot, root)
	serviceID := uuid.New()
	inside := filepath.Join(managedVolumeRoot(), serviceID.String(), "data")

	allowed := fmt.Sprintf("services:\n  web:\n    image: nginx:1.27\n    volumes:\n      - %s:/data\n    labels:\n      gotham.domain: app.example.com\n      gotham.domain.port: \"8080\"\n", inside)
	if err := guardServiceDocument(serviceID, allowed); err != nil {
		t.Fatalf("a managed bind and service route labels were rejected: %v", err)
	}

	foreign := fmt.Sprintf("services:\n  web:\n    image: nginx:1.27\n    volumes:\n      - %s:/data\n", filepath.Join(managedVolumeRoot(), uuid.NewString()))
	if err := guardServiceDocument(serviceID, foreign); !errors.Is(err, ErrValidation) {
		t.Fatalf("a bind owned by another service = %v, want ErrValidation", err)
	}

	reserved := "services:\n  web:\n    image: nginx:1.27\n    labels:\n      gotham.app_id: " + uuid.NewString() + "\n"
	if err := guardServiceDocument(serviceID, reserved); !errors.Is(err, ErrValidation) {
		t.Fatalf("a reserved label = %v, want ErrValidation", err)
	}
}

func TestUpdateRejectsUnconfinedCompose(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	unconfined := "services:\n  web:\n    image: nginx:1.27\n    privileged: true\n"
	_, err := svc.Update(context.Background(), userID, created.ID, UpdateRequest{ComposeYAML: &unconfined})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Update = %v, want ErrValidation", err)
	}
	stored, err := repo.GetService(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if stored.ComposeYAML != created.ComposeYAML {
		t.Fatal("a rejected update changed the stored document")
	}
}

func TestDeployRejectsStoredUnconfinedCompose(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()

	legacy := Service{
		ID:            uuid.New(),
		UserID:        userID,
		ServerID:      serverID,
		EnvironmentID: envID,
		Name:          "legacy",
		Status:        StatusRunning,
		ComposeYAML:   "services:\n  web:\n    image: nginx:1.27\n    privileged: true\n",
		Env:           map[string]string{},
	}
	if _, err := repo.CreateService(context.Background(), legacy); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, _, err := svc.Deploy(context.Background(), userID, legacy.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("Deploy = %v, want ErrValidation", err)
	}
	if len(agent.validated) != 0 || len(agent.ups) != 0 {
		t.Fatal("the deploy reached the agent before validation")
	}
	if deploys, _ := repo.ListServiceDeploys(context.Background(), legacy.ID, 10); len(deploys) != 0 {
		t.Fatalf("a rejected deploy recorded %d rows", len(deploys))
	}
}
