package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
)

// fakeListers serves canned workloads for the resources surface.
type fakeListers struct {
	applications []deploy.Application
	services     []services.Service
	databases    []databases.Database
}

func (f *fakeListers) ListApplications(context.Context, uuid.UUID, deploy.ApplicationFilter) ([]deploy.Application, error) {
	return f.applications, nil
}

func (f *fakeListers) List(_ context.Context, _ uuid.UUID, _ databases.DatabaseFilter) ([]databases.Database, error) {
	return f.databases, nil
}

// Compile-time guarantees that the canned listers satisfy the seams.
var (
	_ ApplicationLister = (*fakeListers)(nil)
	_ DatabaseLister    = (*fakeListers)(nil)
)

// fakeServiceLister adapts fakeListers to the services seam (its List takes a
// ServiceFilter, so it cannot live on the same type as the databases List).
type fakeServiceLister struct{ *fakeListers }

func (f fakeServiceLister) List(_ context.Context, _ uuid.UUID, _ services.ServiceFilter) ([]services.Service, error) {
	return f.services, nil
}

var _ ServiceLister = fakeServiceLister{}

// resourcesService wires the real service onto the in-memory repository with
// canned workloads.
func resourcesService(repo *fakeRepository, listers *fakeListers) *Service {
	return NewService(Config{
		Repository:   repo,
		Counter:      newFakeCounter(),
		Applications: listers,
		Services:     fakeServiceLister{listers},
		Databases:    listers,
		Logger:       discardLogger(),
	})
}

// TestGetEnvironmentResources returns one environment with its project and
// workloads, hides previews by default and answers 404 for foreign IDs.
func TestGetEnvironmentResources(t *testing.T) {
	repo := newFakeRepository()
	owner, teamID := uuid.New(), uuid.New()
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: owner, TeamID: teamID, Role: teams.RoleAdmin})
	svc := NewService(Config{Repository: repo, Counter: newFakeCounter(), Logger: discardLogger()})

	project, _, err := svc.CreateProject(ctx, owner, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environments, err := svc.ListEnvironments(ctx, owner, project.ID)
	if err != nil || len(environments) != 1 {
		t.Fatalf("environments = %+v, %v", environments, err)
	}
	production := environments[0]

	previewID := uuid.New()
	listers := &fakeListers{
		applications: []deploy.Application{
			{ID: uuid.New(), EnvironmentID: production.ID, Name: "web"},
			{ID: previewID, EnvironmentID: production.ID, Name: "web-pr-7", IsPreview: true},
		},
		services:  []services.Service{{ID: uuid.New(), EnvironmentID: production.ID, Name: "worker"}},
		databases: []databases.Database{{ID: uuid.New(), EnvironmentID: production.ID, Name: "db"}},
	}
	svc = resourcesService(repo, listers)

	resources, err := svc.GetEnvironmentResources(ctx, owner, production.ID, false)
	if err != nil {
		t.Fatalf("GetEnvironmentResources: %v", err)
	}
	if resources.Environment.ID != production.ID || resources.Project.ID != project.ID {
		t.Errorf("envelope = %+v / %+v, want production of Shop", resources.Environment, resources.Project)
	}
	if len(resources.Applications) != 1 || resources.Applications[0].Name != "web" {
		t.Errorf("applications = %+v, want only the non-preview", resources.Applications)
	}
	if len(resources.Services) != 1 || len(resources.Databases) != 1 {
		t.Errorf("services = %d, databases = %d, want one each", len(resources.Services), len(resources.Databases))
	}

	withPreviews, err := svc.GetEnvironmentResources(ctx, owner, production.ID, true)
	if err != nil {
		t.Fatalf("GetEnvironmentResources (previews): %v", err)
	}
	if len(withPreviews.Applications) != 2 {
		t.Errorf("applications with previews = %d, want 2", len(withPreviews.Applications))
	}

	if _, err := svc.GetEnvironmentResources(ctx, owner, uuid.New(), false); err != ErrNotFound {
		t.Errorf("unknown environment err = %v, want ErrNotFound", err)
	}
	strangerCtx := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: uuid.New(), Role: teams.RoleAdmin})
	if _, err := svc.GetEnvironmentResources(strangerCtx, uuid.New(), production.ID, false); err != ErrNotFound {
		t.Errorf("foreign environment err = %v, want ErrNotFound", err)
	}
}

// TestEnvironmentResourcesRoute walks the endpoint: the envelope shape,
// preview filtering and the 404.
func TestEnvironmentResourcesRoute(t *testing.T) {
	owner, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	adminCtx := teams.WithScope(context.Background(), teams.Scope{UserID: owner, TeamID: teamID, Role: teams.RoleAdmin})
	bootstrap := NewService(Config{Repository: repo, Counter: newFakeCounter(), Logger: discardLogger()})
	project, _, err := bootstrap.CreateProject(adminCtx, owner, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environments, err := bootstrap.ListEnvironments(adminCtx, owner, project.ID)
	if err != nil || len(environments) != 1 {
		t.Fatalf("environments = %+v, %v", environments, err)
	}
	production := environments[0]

	listers := &fakeListers{
		applications: []deploy.Application{{ID: uuid.New(), EnvironmentID: production.ID, Name: "web"}},
		services:     []services.Service{{ID: uuid.New(), EnvironmentID: production.ID, Name: "worker"}},
		databases:    []databases.Database{{ID: uuid.New(), EnvironmentID: production.ID, Name: "db"}},
	}
	handler := newRouteServer(owner, teamID, teams.RoleAdmin, resourcesService(repo, listers))

	rec := doRequest(handler, http.MethodGet, "/v1/environments/"+production.ID.String()+"/resources", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get resources = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	var envelope environmentResourcesEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Environment.ID != production.ID.String() || envelope.Project.ID != project.ID.String() {
		t.Errorf("envelope = %+v / %+v", envelope.Environment, envelope.Project)
	}
	if len(envelope.Applications) != 1 || len(envelope.Services) != 1 || len(envelope.Databases) != 1 {
		t.Errorf("workloads = %d/%d/%d, want one each",
			len(envelope.Applications), len(envelope.Services), len(envelope.Databases))
	}

	rec = doRequest(handler, http.MethodGet, "/v1/environments/"+uuid.NewString()+"/resources", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown environment = %d, want 404", rec.Code)
	}
}
