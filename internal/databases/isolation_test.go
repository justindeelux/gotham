package databases

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestDatabaseTeamIsolation is the Phase 8 exit criterion at the service layer:
// a member of one team can neither see nor mutate another team's databases, a
// read_only member reads but cannot mutate, and creation stamps the active
// team.
func TestDatabaseTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice, bob := uuid.New(), uuid.New()

	repo := newFakeRepository()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	bg := context.Background()

	// Creation stamps the caller's active team.
	mineCtx := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleAdmin})
	created, _, err := svc.Create(mineCtx, bob, CreateRequest{
		Name:     "bobs-db",
		Engine:   "postgres",
		ServerID: repo.seedServer(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TeamID != teamB {
		t.Fatalf("created database team = %s, want %s", created.TeamID, teamB)
	}

	other := repo.seed(Database{
		UserID:   alice,
		TeamID:   teamA,
		ServerID: repo.seedServer(),
		Name:     "alices-db",
		Engine:   "postgres",
		Status:   StatusRunning,
	})

	// Team B sees only its own database.
	databases, err := svc.List(mineCtx, bob)
	if err != nil {
		t.Fatalf("list team B: %v", err)
	}
	if len(databases) != 1 || databases[0].ID != created.ID {
		t.Fatalf("team B list = %+v, want only its own database", databases)
	}
	// The other team's database cannot be read or mutated, not even by name.
	aliceCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.Get(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Get = %v, want ErrNotFound", err)
	}
	if _, err := svc.Credentials(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Credentials = %v, want ErrNotFound", err)
	}
	if _, err := svc.Update(aliceCtx, alice, created.ID, UpdateRequest{Name: "stolen"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Update = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Delete = %v, want ErrNotFound", err)
	}

	// A read_only member of team A reads its database but cannot mutate it.
	viewerCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleReadOnly})
	if _, err := svc.Get(viewerCtx, alice, other.ID); err != nil {
		t.Fatalf("read_only Get: %v", err)
	}
	if _, err := svc.Credentials(viewerCtx, alice, other.ID); err != nil {
		t.Fatalf("read_only Credentials: %v", err)
	}
	if _, err := svc.Update(viewerCtx, alice, other.ID, UpdateRequest{Name: "renamed"}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Update = %v, want ErrForbidden", err)
	}
	if _, err := svc.Stop(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Stop = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Delete = %v, want ErrForbidden", err)
	}
}

// TestDatabaseCreateWithoutTeamContextStaysCreatorScoped keeps the pre-teams
// path honest: without a team scope the list is the creator's own rows.
func TestDatabaseCreateWithoutTeamContextStaysCreatorScoped(t *testing.T) {
	repo := newFakeRepository()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	created, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name:     "legacy-db",
		Engine:   "postgres",
		ServerID: repo.seedServer(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TeamID != uuid.Nil {
		t.Fatalf("created database team = %s, want unset without a team context", created.TeamID)
	}
	databases, err := svc.List(context.Background(), userID)
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
