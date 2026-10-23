package containers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

func TestContainerTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()

	registry := newFakeRegistry()
	node := registry.seed()
	node.TeamID = teamB
	legacy := registry.seed()

	mock := &mockDockerClient{listResp: listResponse(), runID: "new-1"}
	cache := newFakeCache()
	svc := fixture(registry, mock, cache)

	owner := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleOwner})
	if _, err := svc.List(owner, node.ID); err != nil {
		t.Fatalf("owner List: %v", err)
	}

	stranger := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.List(stranger, node.ID); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("stranger List = %v, want ErrServerNotFound", err)
	}
	if _, _, err := svc.Logs(stranger, node.ID, "abc", false); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("stranger Logs = %v, want ErrServerNotFound", err)
	}
	if err := svc.Stop(stranger, node.ID, "abc"); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("stranger Stop = %v, want ErrServerNotFound", err)
	}
	if _, err := svc.List(stranger, uuid.New()); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("missing node List = %v, want ErrServerNotFound", err)
	}

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

	legacyCtx := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleAdmin})
	if _, err := svc.List(legacyCtx, legacy.ID); err != nil {
		t.Fatalf("legacy List: %v", err)
	}
	if err := svc.Start(legacyCtx, legacy.ID, "abc"); err != nil {
		t.Fatalf("legacy Start: %v", err)
	}
}

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
	if node.TeamID == uuid.Nil {
		t.Fatal("the fixture node must carry a team")
	}
}
