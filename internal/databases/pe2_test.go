package databases

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestCreateRequiresEnvironment pins the contract: no environment is a 400,
// a foreign one a 404.
func TestCreateRequiresEnvironment(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{runID: "container-1"})
	userID := uuid.New()

	if _, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: repo.seedServer(),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing environment err = %v, want ErrValidation", err)
	}

	repo.resolveErr = ErrNotFound
	envID, _ := repo.seedEnvironment()
	if _, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: repo.seedServer(),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign environment err = %v, want ErrNotFound", err)
	}
}

// TestUpdateMovesEnvironment pins the move: same-team relocation works and a
// name the target already holds is a 409.
func TestUpdateMovesEnvironment(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{runID: "container-1"})
	owner := uuid.New()
	envA, _ := repo.seedEnvironment()
	envB, _ := repo.seedEnvironment()
	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envA, ServerID: repo.seedServer(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	moved, err := svc.Update(context.Background(), owner, created.ID,
		UpdateRequest{Name: ptr("orders"), EnvironmentID: &envB})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.EnvironmentID != envB {
		t.Fatalf("environment = %s, want %s", moved.EnvironmentID, envB)
	}

	if _, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "taken", Engine: EnginePostgres, EnvironmentID: envA, ServerID: repo.seedServer(),
	}); err != nil {
		t.Fatalf("Create(taken): %v", err)
	}
	if _, err := svc.Update(context.Background(), owner, moved.ID,
		UpdateRequest{Name: ptr("taken"), EnvironmentID: &envA}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("colliding move err = %v, want ErrNameConflict", err)
	}
}

// TestUpdateRefusesServerChangeWhileBusy pins the 409 guard: a node change
// while a backup holds the database is refused, and succeeds once released.
func TestUpdateRefusesServerChangeWhileBusy(t *testing.T) {
	repo := newFakeRepository()
	leases := NewJobLeases()
	svc := NewService(Config{Repository: repo, Containers: &fakeContainers{runID: "container-1"}, Secret: testSecret, Leases: leases, Logger: discardLogger()})
	owner := uuid.New()
	envID, _ := repo.seedEnvironment()
	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: repo.seedServer(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	other := repo.seedServer()

	if !leases.Claim(created.ID, JobLeaseBackup) {
		t.Fatal("claim backup lease")
	}
	if _, err := svc.Update(context.Background(), owner, created.ID,
		UpdateRequest{Name: ptr("orders"), ServerID: &other}); !errors.Is(err, ErrDeployInFlight) {
		t.Fatalf("server change under backup err = %v, want ErrDeployInFlight", err)
	}
	leases.Release(created.ID)

	moved, err := svc.Update(context.Background(), owner, created.ID,
		UpdateRequest{Name: ptr("orders"), ServerID: &other})
	if err != nil {
		t.Fatalf("server change: %v", err)
	}
	if moved.ServerID != other {
		t.Fatalf("server = %s, want %s", moved.ServerID, other)
	}
}

// TestListFilters scopes the list to one environment or project.
func TestListFilters(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{runID: "container-1"})
	owner := uuid.New()
	envA, projectA := repo.seedEnvironment()
	envB, _ := repo.seedEnvironment()
	for _, tc := range []struct {
		name string
		env  uuid.UUID
	}{
		{"a", envA},
		{"b", envB},
	} {
		if _, _, err := svc.Create(context.Background(), owner, CreateRequest{
			Name: tc.name, Engine: EnginePostgres, EnvironmentID: tc.env, ServerID: repo.seedServer(),
		}); err != nil {
			t.Fatalf("Create(%s): %v", tc.name, err)
		}
	}

	byEnv, err := svc.List(context.Background(), owner, DatabaseFilter{EnvironmentID: envA})
	if err != nil {
		t.Fatalf("list by environment: %v", err)
	}
	if len(byEnv) != 1 || byEnv[0].Name != "a" {
		t.Fatalf("by environment = %+v, want only a", byEnv)
	}
	byProject, err := svc.List(context.Background(), owner, DatabaseFilter{ProjectID: projectA})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if len(byProject) != 1 || byProject[0].Name != "a" {
		t.Fatalf("by project = %+v, want only a", byProject)
	}
}
