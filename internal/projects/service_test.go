package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestNewServiceRequiresCounter is the PE-2 wiring guard: the resource
// counter is a required constructor argument, so forgetting to wire the real
// counter fails fast instead of silently reporting zero resources.
func TestNewServiceRequiresCounter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewService(nil counter) did not panic")
		}
	}()
	NewService(Config{Repository: newFakeRepository(), Logger: discardLogger()})
}

// TestZeroResourceCounterReadsZero documents the PE-1 stub: every count is
// zero until PE-2 replaces it.
func TestZeroResourceCounterReadsZero(t *testing.T) {
	var counter ResourceCounter = ZeroResourceCounter{}
	projectCounts, err := counter.CountProjectResources(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("count project: %v", err)
	}
	environmentCounts, err := counter.CountEnvironmentResources(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("count environment: %v", err)
	}
	for _, counts := range []ResourceCounts{projectCounts, environmentCounts} {
		if counts != (ResourceCounts{}) {
			t.Fatalf("stub counts = %+v, want zeros", counts)
		}
	}
}

// TestServiceCreateProjectStartsWithProduction asserts the contract's create
// shape: one project plus its production environment.
func TestServiceCreateProjectStartsWithProduction(t *testing.T) {
	repo := newFakeRepository()
	svc, ctx := newTestService(repo, ZeroResourceCounter{}, uuid.New(), uuid.New(), teams.RoleAdmin)

	project, environment, err := svc.CreateProject(ctx, uuid.New(), "  Shop  ", "storefront")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if project.Name != "Shop" {
		t.Errorf("name = %q, want the trimmed value", project.Name)
	}
	if project.Description != "storefront" {
		t.Errorf("description = %q", project.Description)
	}
	if project.EnvironmentCount != 1 {
		t.Errorf("environment_count = %d, want 1", project.EnvironmentCount)
	}
	if environment.Name != ProductionEnvironment || environment.ProjectID != project.ID {
		t.Errorf("environment = %+v, want production of the project", environment)
	}
	if project.Resources != (ResourceCounts{}) {
		t.Errorf("resources = %+v, want zeros behind the stub", project.Resources)
	}
}

// TestServiceNameValidation checks the 1-64 contract rule on both create
// paths and renames.
func TestServiceNameValidation(t *testing.T) {
	repo := newFakeRepository()
	userID, teamID := uuid.New(), uuid.New()
	svc, ctx := newTestService(repo, ZeroResourceCounter{}, userID, teamID, teams.RoleAdmin)

	for _, name := range []string{"", "   ", strings.Repeat("x", 65)} {
		if _, _, err := svc.CreateProject(ctx, userID, name, ""); !errors.Is(err, ErrValidation) {
			t.Errorf("CreateProject(%q) = %v, want ErrValidation", name, err)
		}
		if _, err := svc.CreateEnvironment(ctx, userID, uuid.New(), name); !errors.Is(err, ErrValidation) {
			t.Errorf("CreateEnvironment(%q) = %v, want ErrValidation", name, err)
		}
	}
	if _, _, err := svc.CreateProject(ctx, userID, strings.Repeat("x", 64), ""); err != nil {
		t.Errorf("CreateProject(64 chars) = %v, want success", err)
	}

	project, _, err := svc.CreateProject(ctx, userID, "ok", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.UpdateProject(ctx, userID, project.ID, ptr(""), nil); !errors.Is(err, ErrValidation) {
		t.Errorf("UpdateProject(empty) = %v, want ErrValidation", err)
	}
	if _, err := svc.UpdateEnvironment(ctx, userID, project.ID, "   "); !errors.Is(err, ErrValidation) {
		t.Errorf("UpdateEnvironment(blank) = %v, want ErrValidation", err)
	}
}

// TestServiceDuplicateNamesConflict checks case-insensitive uniqueness in
// both scopes.
func TestServiceDuplicateNamesConflict(t *testing.T) {
	repo := newFakeRepository()
	userID, teamID := uuid.New(), uuid.New()
	svc, ctx := newTestService(repo, ZeroResourceCounter{}, userID, teamID, teams.RoleAdmin)

	first, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, _, err := svc.CreateProject(ctx, userID, "SHOP", ""); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("duplicate project = %v, want ErrProjectExists", err)
	}
	second, _, err := svc.CreateProject(ctx, userID, "Blog", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.UpdateProject(ctx, userID, second.ID, ptr("shop"), nil); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("rename onto existing = %v, want ErrProjectExists", err)
	}

	if _, err := svc.CreateEnvironment(ctx, userID, first.ID, "PRODUCTION"); !errors.Is(err, ErrEnvironmentExists) {
		t.Fatalf("duplicate environment = %v, want ErrEnvironmentExists", err)
	}
	staging, err := svc.CreateEnvironment(ctx, userID, first.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if _, err := svc.UpdateEnvironment(ctx, userID, staging.ID, "Staging"); err != nil {
		t.Fatalf("rename onto itself with different case = %v, want success", err)
	}
	qa, err := svc.CreateEnvironment(ctx, userID, first.ID, "qa")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if _, err := svc.UpdateEnvironment(ctx, userID, qa.ID, "STAGING"); !errors.Is(err, ErrEnvironmentExists) {
		t.Fatalf("rename onto existing = %v, want ErrEnvironmentExists", err)
	}
}

// TestServiceViewerCannotMutate checks the CanWrite rule at the service
// layer (the route gate pre-filters the same way).
func TestServiceViewerCannotMutate(t *testing.T) {
	repo := newFakeRepository()
	owner, teamID := uuid.New(), uuid.New()
	adminSvc, adminCtx := newTestService(repo, ZeroResourceCounter{}, owner, teamID, teams.RoleAdmin)
	project, _, err := adminSvc.CreateProject(adminCtx, owner, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	viewerID := uuid.New()
	svc, ctx := newTestService(repo, ZeroResourceCounter{}, viewerID, teamID, teams.RoleReadOnly)
	if _, _, err := svc.CreateProject(ctx, viewerID, "Other", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer create = %v, want ErrForbidden", err)
	}
	if _, err := svc.UpdateProject(ctx, viewerID, project.ID, ptr("x"), nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer rename = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteProject(ctx, viewerID, project.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer delete = %v, want ErrForbidden", err)
	}
	if _, err := svc.CreateEnvironment(ctx, viewerID, project.ID, "staging"); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer env create = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteEnvironment(ctx, viewerID, project.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer env delete = %v, want ErrForbidden", err)
	}
	// Reads stay open.
	if _, err := svc.ListProjects(ctx, viewerID); err != nil {
		t.Errorf("viewer list = %v, want success", err)
	}
	if _, _, err := svc.GetProject(ctx, viewerID, project.ID); err != nil {
		t.Errorf("viewer get = %v, want success", err)
	}
}

// TestServiceCrossTeamIsNotFound checks team isolation: a foreign ID is
// indistinguishable from a missing one.
func TestServiceCrossTeamIsNotFound(t *testing.T) {
	repo := newFakeRepository()
	owner, teamID := uuid.New(), uuid.New()
	adminSvc, adminCtx := newTestService(repo, ZeroResourceCounter{}, owner, teamID, teams.RoleAdmin)
	project, env, err := adminSvc.CreateProject(adminCtx, owner, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	strangerSvc, strangerCtx := newTestService(repo, ZeroResourceCounter{}, uuid.New(), uuid.New(), teams.RoleAdmin)
	if _, _, err := strangerSvc.GetProject(strangerCtx, uuid.New(), project.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team get = %v, want ErrNotFound", err)
	}
	if _, err := strangerSvc.UpdateProject(strangerCtx, uuid.New(), project.ID, ptr("x"), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team rename = %v, want ErrNotFound", err)
	}
	if err := strangerSvc.DeleteProject(strangerCtx, uuid.New(), project.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team delete = %v, want ErrNotFound", err)
	}
	if _, err := strangerSvc.CreateEnvironment(strangerCtx, uuid.New(), project.ID, "staging"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team env create = %v, want ErrNotFound", err)
	}
	if _, err := strangerSvc.UpdateEnvironment(strangerCtx, uuid.New(), env.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team env rename = %v, want ErrNotFound", err)
	}
	if err := strangerSvc.DeleteEnvironment(strangerCtx, uuid.New(), env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team env delete = %v, want ErrNotFound", err)
	}
}

// TestServiceDeleteRules checks the refusal order: last environment first,
// then resources.
func TestServiceDeleteRules(t *testing.T) {
	repo := newFakeRepository()
	counter := newFakeCounter()
	userID, teamID := uuid.New(), uuid.New()
	svc, ctx := newTestService(repo, counter, userID, teamID, teams.RoleAdmin)

	project, production, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// The only environment cannot go, even empty.
	if err := svc.DeleteEnvironment(ctx, userID, production.ID); !errors.Is(err, ErrLastEnvironment) {
		t.Fatalf("delete last = %v, want ErrLastEnvironment", err)
	}
	staging, err := svc.CreateEnvironment(ctx, userID, project.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	// A project with two empty environments deletes fine.
	if err := svc.DeleteProject(ctx, userID, project.ID); err != nil {
		t.Fatalf("delete empty project = %v, want success", err)
	}

	project, production, err = svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	staging, err = svc.CreateEnvironment(ctx, userID, project.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	counter.environments[staging.ID] = ResourceCounts{Applications: 2}
	counter.projects[project.ID] = ResourceCounts{Applications: 2}
	if err := svc.DeleteEnvironment(ctx, userID, staging.ID); !errors.Is(err, ErrEnvironmentNotEmpty) {
		t.Fatalf("delete nonempty env = %v, want ErrEnvironmentNotEmpty", err)
	}
	if err := svc.DeleteProject(ctx, userID, project.ID); !errors.Is(err, ErrProjectNotEmpty) {
		t.Fatalf("delete nonempty project = %v, want ErrProjectNotEmpty", err)
	}
	// Draining the counts unblocks both deletes.
	counter.environments[staging.ID] = ResourceCounts{}
	counter.projects[project.ID] = ResourceCounts{}
	if err := svc.DeleteEnvironment(ctx, userID, staging.ID); err != nil {
		t.Fatalf("delete drained env = %v, want success", err)
	}
	if err := svc.DeleteEnvironment(ctx, userID, production.ID); !errors.Is(err, ErrLastEnvironment) {
		t.Fatalf("delete last = %v, want ErrLastEnvironment", err)
	}
	if err := svc.DeleteProject(ctx, userID, project.ID); err != nil {
		t.Fatalf("delete drained project = %v, want success", err)
	}
}

func ptr(s string) *string { return &s }
