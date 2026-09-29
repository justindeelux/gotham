package containers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestContainerTeamIsolation is the F1 regression: a node's containers are only
// reachable by its own team, before any agent or cache access, and a read_only
// member can list but not mutate. A foreign node and a missing node answer the
// same 404 so the API is not an existence oracle.
func TestContainerTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()

	registry := newFakeRegistry()
	node := registry.seed()
	node.TeamID = teamB
	legacy := registry.seed() // TeamID left unset: the pre-teams shared node

	mock := &mockDockerClient{listResp: listResponse(), runID: "new-1"}
	cache := newFakeCache()
	svc := fixture(registry, mock, cache)

	// Warm the cache for the team node: the authorization check must run
	// before the cache is consulted, so a stranger never gets a cached list.
	owner := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleOwner})
	if _, err := svc.List(owner, node.ID); err != nil {
		t.Fatalf("owner List: %v", err)
	}

	// A member of another team cannot see the node, cached or not.
	stranger := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.List(stranger, node.ID); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("stranger List = %v, want ErrServerNotFound", err)
	}
	if _, err := svc.Logs(stranger, node.ID, "abc", false); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("stranger Logs = %v, want ErrServerNotFound", err)
	}
	if err := svc.Stop(stranger, node.ID, "abc"); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("stranger Stop = %v, want ErrServerNotFound", err)
	}
	// A missing node answers exactly like a foreign one.
	if _, err := svc.List(stranger, uuid.New()); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("missing node List = %v, want ErrServerNotFound", err)
	}

	// A read_only member reads the node but cannot mutate it.
	viewer := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleReadOnly})
	if _, err := svc.List(viewer, node.ID); err != nil {
		t.Fatalf("read_only List: %v", err)
	}
	if err := svc.Start(viewer, node.ID, "abc"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Start = %v, want ErrForbidden", err)
	}
	if err := svc.Stop(viewer, node.ID, "abc"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Stop = %v, want ErrForbidden", err)
	}
	if err := svc.Restart(viewer, node.ID, "abc"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Restart = %v, want ErrForbidden", err)
	}
	if err := svc.Remove(viewer, node.ID, "abc"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Remove = %v, want ErrForbidden", err)
	}
	if err := svc.Pull(viewer, node.ID, "nginx:latest"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Pull = %v, want ErrForbidden", err)
	}
	if _, err := svc.Run(viewer, node.ID, RunOptions{Image: "nginx:latest", Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Run = %v, want ErrForbidden", err)
	}

	// Legacy nodes stay shared: no team owns them, so any authenticated team
	// may read them (the documented pre-teams residual) and an admin may act.
	legacyCtx := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleAdmin})
	if _, err := svc.List(legacyCtx, legacy.ID); err != nil {
		t.Fatalf("legacy List: %v", err)
	}
	if err := svc.Start(legacyCtx, legacy.ID, "abc"); err != nil {
		t.Fatalf("legacy Start: %v", err)
	}
}

// TestContainerRequestWithoutScopeKeepsLegacyBehavior guards the compatibility
// path: background callers (backup jobs) hold no team scope, and their node
// access must keep working.
func TestContainerRequestWithoutScopeKeepsLegacyBehavior(t *testing.T) {
	registry := newFakeRegistry()
	node := registry.seed()
	node.TeamID = uuid.New()
	svc := fixture(registry, &mockDockerClient{listResp: listResponse()}, newFakeCache())

	if _, err := svc.List(context.Background(), node.ID); err != nil {
		t.Fatalf("List without a scope: %v", err)
	}
	if err := svc.Stop(context.Background(), node.ID, "abc"); err != nil {
		t.Fatalf("Stop without a scope: %v", err)
	}
	// The node is a team node, so only the missing scope kept the call alive.
	if node.TeamID == uuid.Nil {
		t.Fatal("the fixture node must carry a team")
	}
}
