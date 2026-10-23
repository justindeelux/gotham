package databases

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

func TestDatabaseTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice, bob := uuid.New(), uuid.New()

	repo := newFakeRepository()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	bg := context.Background()

	mineCtx := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleAdmin})
	envID, _ := repo.seedEnvironment()
	created, _, err := svc.Create(mineCtx, bob, CreateRequest{
		Name:          "bobs-db",
		Engine:        "postgres",
		EnvironmentID: envID,
		ServerID:      repo.seedServer(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TeamID != teamB {
		t.Fatalf("created database team = %s, want %s", created.TeamID, teamB)
	}

	other := repo.seed(Database{
		UserID:        alice,
		TeamID:        teamA,
		ServerID:      repo.seedServer(),
		EnvironmentID: envID,
		Name:          "alices-db",
		Engine:        "postgres",
		Status:        StatusRunning,
	})

	databases, err := svc.List(mineCtx, bob, DatabaseFilter{})
	if err != nil {
		t.Fatalf("list team B: %v", err)
	}
	if len(databases) != 1 || databases[0].ID != created.ID {
		t.Fatalf("team B list = %+v, want only its own database", databases)
	}
	aliceCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.Get(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Get = %v, want ErrNotFound", err)
	}
	if _, err := svc.Credentials(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Credentials = %v, want ErrNotFound", err)
	}
	if _, err := svc.Update(aliceCtx, alice, created.ID, UpdateRequest{Name: ptr("stolen")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Update = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Delete = %v, want ErrNotFound", err)
	}

	viewerCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleReadOnly})
	if _, err := svc.Get(viewerCtx, alice, other.ID); err != nil {
		t.Fatalf("read_only Get: %v", err)
	}
	if _, err := svc.Credentials(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Credentials = %v, want ErrForbidden (plaintext secrets need owner/admin)", err)
	}
	if _, err := svc.Update(viewerCtx, alice, other.ID, UpdateRequest{Name: ptr("renamed")}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Update = %v, want ErrForbidden", err)
	}
	if _, err := svc.Stop(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Stop = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Delete = %v, want ErrForbidden", err)
	}
}

func TestDatabaseCreateWithoutTeamContextStaysCreatorScoped(t *testing.T) {
	repo := newFakeRepository()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	legacyEnvID, _ := repo.seedEnvironment()
	created, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name:          "legacy-db",
		Engine:        "postgres",
		EnvironmentID: legacyEnvID,
		ServerID:      repo.seedServer(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TeamID != teams.PersonalTeamID(userID) {
		t.Fatalf("created database team = %s, want the personal team without a team context", created.TeamID)
	}
	databases, err := svc.List(context.Background(), userID, DatabaseFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(databases) != 1 || databases[0].ID != created.ID {
		t.Fatalf("list = %+v, want the creator's database", databases)
	}
	if _, err := svc.Get(context.Background(), uuid.New(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(other creator) = %v, want ErrNotFound", err)
	}
}

func TestDatabaseCreateRejectsForeignServer(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice := uuid.New()

	repo := newFakeRepository()
	foreignNode := repo.seedServerForTeam(teamB)
	ownNode := repo.seedServerForTeam(teamA)
	legacyNode := repo.seedServerForTeam(uuid.Nil)
	svc := newTestService(repo, &fakeContainers{runID: "container-1"})
	ctxA := teams.WithScope(context.Background(), teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})

	envID, _ := repo.seedEnvironment()
	if _, _, err := svc.Create(ctxA, alice, CreateRequest{
		Name: "foreign", Engine: "postgres", EnvironmentID: envID, ServerID: foreignNode,
	}); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("create on a foreign node = %v, want ErrServerNotFound", err)
	}
	if _, _, err := svc.Create(ctxA, alice, CreateRequest{
		Name: "own", Engine: "postgres", EnvironmentID: envID, ServerID: ownNode,
	}); err != nil {
		t.Fatalf("create on the team's node: %v", err)
	}
	if _, _, err := svc.Create(ctxA, alice, CreateRequest{
		Name: "legacy", Engine: "postgres", EnvironmentID: envID, ServerID: legacyNode,
	}); err != nil {
		t.Fatalf("create on a legacy node: %v", err)
	}
}
