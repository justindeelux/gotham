package services

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
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()

	if _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", ServerID: repo.seedServer(), ComposeYAML: testDocument, Env: testEnv,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing environment err = %v, want ErrValidation", err)
	}

	repo.resolveErr = ErrNotFound
	envID, _ := repo.seedEnvironment()
	if _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", EnvironmentID: envID, ServerID: repo.seedServer(),
		ComposeYAML: testDocument, Env: testEnv,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign environment err = %v, want ErrNotFound", err)
	}
}

// TestUpdateMovesEnvironment pins the move: same-team relocation works and a
// name the target already holds is a 409.
func TestUpdateMovesEnvironment(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	envA, _ := repo.seedEnvironment()
	envB, _ := repo.seedEnvironment()
	created, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", EnvironmentID: envA, ServerID: repo.seedServer(),
		ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	moved, err := svc.Update(context.Background(), userID, created.ID, UpdateRequest{EnvironmentID: &envB})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.EnvironmentID != envB {
		t.Fatalf("environment = %s, want %s", moved.EnvironmentID, envB)
	}

	if _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", EnvironmentID: envB, ServerID: repo.seedServer(),
		ComposeYAML: testDocument, Env: testEnv,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate in target err = %v, want ErrConflict", err)
	}
	taken, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "taken", EnvironmentID: envA, ServerID: repo.seedServer(),
		ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("Create(taken): %v", err)
	}
	_ = taken
	name := "taken"
	if _, err := svc.Update(context.Background(), userID, moved.ID,
		UpdateRequest{EnvironmentID: &envA, Name: &name}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("colliding move err = %v, want ErrNameConflict", err)
	}
}

// TestUpdateRefusesServerChangeWhileDeploying pins the 409 guard: a node
// change while a deploy is in flight is refused, and succeeds after it
// lands.
func TestUpdateRefusesServerChangeWhileDeploying(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	envID, _ := repo.seedEnvironment()
	created, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", EnvironmentID: envID, ServerID: repo.seedServer(),
		ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	other := repo.seedServer()

	repo.activeDeploy = map[uuid.UUID]bool{created.ID: true}
	if _, err := svc.Update(context.Background(), userID, created.ID,
		UpdateRequest{ServerID: &other}); !errors.Is(err, ErrDeployInFlight) {
		t.Fatalf("server change in flight err = %v, want ErrDeployInFlight", err)
	}

	repo.activeDeploy = nil
	moved, err := svc.Update(context.Background(), userID, created.ID, UpdateRequest{ServerID: &other})
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
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	envA, projectA := repo.seedEnvironment()
	envB, _ := repo.seedEnvironment()
	_ = projectA
	for _, tc := range []struct {
		name string
		env  uuid.UUID
	}{
		{"a", envA},
		{"b", envB},
	} {
		if _, err := svc.Create(context.Background(), userID, CreateRequest{
			Name: tc.name, EnvironmentID: tc.env, ServerID: repo.seedServer(),
			ComposeYAML: testDocument, Env: testEnv,
		}); err != nil {
			t.Fatalf("Create(%s): %v", tc.name, err)
		}
	}

	byEnv, err := svc.List(context.Background(), userID, ServiceFilter{EnvironmentID: envA})
	if err != nil {
		t.Fatalf("list by environment: %v", err)
	}
	if len(byEnv) != 1 || byEnv[0].Name != "a" {
		t.Fatalf("by environment = %+v, want only a", byEnv)
	}
	byProject, err := svc.List(context.Background(), userID, ServiceFilter{ProjectID: projectA})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if len(byProject) != 1 || byProject[0].Name != "a" {
		t.Fatalf("by project = %+v, want only a", byProject)
	}
}
