package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestServiceTeamIsolation is the Phase 8 exit criterion at the service layer:
// a member of one team can neither see nor mutate another team's compose
// services, a read_only member reads but cannot mutate, and creation stamps the
// active team.
func TestServiceTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice, bob := uuid.New(), uuid.New()

	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	bg := context.Background()

	// Creation stamps the caller's active team.
	mineCtx := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleAdmin})
	created, err := svc.Create(mineCtx, bob, CreateRequest{
		Name:        "bobs-service",
		ServerID:    repo.seedServer(),
		ComposeYAML: testDocument,
		Env:         testEnv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TeamID != teamB {
		t.Fatalf("created service team = %s, want %s", created.TeamID, teamB)
	}

	other, err := svc.Create(teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner}), alice, CreateRequest{
		Name:        "alices-service",
		ServerID:    repo.seedServer(),
		ComposeYAML: testDocument,
		Env:         testEnv,
	})
	if err != nil {
		t.Fatalf("Create (team A): %v", err)
	}
	if other.TeamID != teamA {
		t.Fatalf("team A service team = %s, want %s", other.TeamID, teamA)
	}

	// Team B sees only its own service.
	all, err := svc.List(mineCtx, bob)
	if err != nil {
		t.Fatalf("list team B: %v", err)
	}
	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("team B list = %+v, want only its own service", all)
	}

	// The other team's service cannot be read or mutated, not even by name.
	aliceCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.Get(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Get = %v, want ErrNotFound", err)
	}
	if _, err := svc.Deploys(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Deploys = %v, want ErrNotFound", err)
	}
	if _, err := svc.Update(aliceCtx, alice, created.ID, UpdateRequest{Name: ptr("stolen")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Update = %v, want ErrNotFound", err)
	}
	if _, _, err := svc.Deploy(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Deploy = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(aliceCtx, alice, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Delete = %v, want ErrNotFound", err)
	}

	// A read_only member of team A reads but cannot mutate.
	viewerCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleReadOnly})
	if _, err := svc.Get(viewerCtx, alice, other.ID); err != nil {
		t.Fatalf("read_only Get: %v", err)
	}
	if _, err := svc.Deploys(viewerCtx, alice, other.ID); err != nil {
		t.Fatalf("read_only Deploys: %v", err)
	}
	if _, err := svc.Update(viewerCtx, alice, other.ID, UpdateRequest{Name: ptr("renamed")}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Update = %v, want ErrForbidden", err)
	}
	if _, _, err := svc.Deploy(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Deploy = %v, want ErrForbidden", err)
	}
	if _, err := svc.Stop(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Stop = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(viewerCtx, alice, other.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Delete = %v, want ErrForbidden", err)
	}
	if stream, err := svc.Logs(viewerCtx, alice, other.ID, "", 10, false); errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Logs = %v, want read access", err)
	} else if stream != nil {
		_ = stream.Close()
	}
}

// ptr returns a pointer to v, for the optional update fields.
func ptr[T any](v T) *T {
	return &v
}
