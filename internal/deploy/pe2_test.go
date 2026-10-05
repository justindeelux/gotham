package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// routeMessage decodes the {"message": ...} error body.
func routeMessage(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return decoded.Message
}

// TestCreateApplicationRequiresEnvironment pins the contract: no environment
// is a 400, a foreign one a 404.
func TestCreateApplicationRequiresEnvironment(t *testing.T) {
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	userID := uuid.New()
	serverID := uuid.New()

	in := validCreateInput(serverID)
	in.EnvironmentID = uuid.Nil
	if _, err := svc.CreateApplication(context.Background(), userID, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing environment err = %v, want ErrValidation", err)
	}

	repo.resolveErr = ErrNotFound
	in.EnvironmentID = uuid.New()
	if _, err := svc.CreateApplication(context.Background(), userID, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign environment err = %v, want ErrNotFound", err)
	}
}

// TestUpdateApplicationMovesEnvironment pins the move: same-team relocation
// works, a name the target already holds is a 409, a foreign target a 404.
func TestUpdateApplicationMovesEnvironment(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	envA, envB := uuid.New(), uuid.New()
	app.EnvironmentID = envA
	repo := &fakeRepository{app: app}
	repo.environments = map[uuid.UUID]EnvironmentRef{
		envA: {ID: envA, Name: "production"},
		envB: {ID: envB, Name: "staging"},
	}
	svc := newTestService(t, repo)

	moved, err := svc.UpdateApplication(context.Background(), userID, app.ID,
		UpdateApplicationInput{EnvironmentID: &envB})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.EnvironmentID != envB {
		t.Fatalf("environment = %s, want %s", moved.EnvironmentID, envB)
	}

	// A sibling with the same name in the target blocks the move.
	repo.apps = append(repo.apps, Application{ID: uuid.New(), UserID: userID, EnvironmentID: envA, Name: "taken"})
	taken := "taken"
	back, err := svc.UpdateApplication(context.Background(), userID, moved.ID,
		UpdateApplicationInput{EnvironmentID: &envA, Name: &taken})
	_ = back
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("colliding move err = %v, want ErrNameConflict", err)
	}

	// A foreign environment answers 404.
	repo.resolveErr = ErrNotFound
	if _, err := svc.UpdateApplication(context.Background(), userID, moved.ID,
		UpdateApplicationInput{EnvironmentID: &envA}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign move err = %v, want ErrNotFound", err)
	}
}

// TestCreatePreviewApplicationInheritsEnvironment pins decision 4: a preview
// copies the environment and the server of its base application.
func TestCreatePreviewApplicationInheritsEnvironment(t *testing.T) {
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	envID, serverID := uuid.New(), uuid.New()
	baseID := uuid.New()
	repo.apps = []Application{{
		ID: baseID, UserID: uuid.New(), TeamID: uuid.New(),
		ServerID: serverID, EnvironmentID: envID,
		Name: "base", Provider: "github", Repo: "acme/demo",
		CloneURL: "https://github.com/acme/demo.git", Branch: "main", BuildPack: "dockerfile",
	}}

	preview, err := svc.CreatePreviewApplication(context.Background(), baseID, PreviewApplicationInput{
		Name: "base-pr-9", Branch: "feat/x", BaseDomain: "pr-9.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if preview.EnvironmentID != envID || preview.ServerID != serverID {
		t.Fatalf("preview env/server = %s/%s, want %s/%s",
			preview.EnvironmentID, preview.ServerID, envID, serverID)
	}
	if !preview.IsPreview {
		t.Error("preview is not marked is_preview")
	}
}

// TestListApplicationsFilters pins the ?environment_id= and ?project_id=
// filters: each scopes the list, both together are a 400, a foreign ID a
// 404.
func TestListApplicationsFilters(t *testing.T) {
	userID := uuid.New()
	envA, projectA := uuid.New(), uuid.New()
	repo := &fakeRepository{
		apps: []Application{
			{ID: uuid.New(), UserID: userID, EnvironmentID: envA, Name: "in-env"},
			{ID: uuid.New(), UserID: userID, Name: "elsewhere"},
		},
		environments: map[uuid.UUID]EnvironmentRef{envA: {ID: envA, ProjectID: projectA}},
	}
	svc := newTestService(t, repo)

	byEnv, err := svc.ListApplications(context.Background(), userID, ApplicationFilter{EnvironmentID: envA})
	if err != nil {
		t.Fatalf("list by environment: %v", err)
	}
	if len(byEnv) != 1 || byEnv[0].Name != "in-env" {
		t.Fatalf("by environment = %+v, want only in-env", byEnv)
	}

	byProject, err := svc.ListApplications(context.Background(), userID, ApplicationFilter{ProjectID: projectA})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if len(byProject) != 1 || byProject[0].Name != "in-env" {
		t.Fatalf("by project = %+v, want only in-env", byProject)
	}

	if _, err := svc.ListApplications(context.Background(), userID,
		ApplicationFilter{EnvironmentID: envA, ProjectID: projectA}); !errors.Is(err, ErrValidation) {
		t.Fatalf("both filters err = %v, want ErrValidation", err)
	}

	repo.resolveErr = ErrNotFound
	if _, err := svc.ListApplications(context.Background(), userID,
		ApplicationFilter{EnvironmentID: uuid.New()}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign filter err = %v, want ErrNotFound", err)
	}
}

// TestMoveRefusedWhilePreviewsOpen pins F5: moving the base application to
// another environment, or changing its node, answers 409 while live
// previews exist.
func TestMoveRefusedWhilePreviewsOpen(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.EnvironmentID = uuid.New()
	repo := &fakeRepository{
		app:          app,
		livePreviews: map[uuid.UUID]bool{app.ID: true},
		environments: map[uuid.UUID]EnvironmentRef{app.EnvironmentID: {ID: app.EnvironmentID}},
	}
	svc := newTestService(t, repo)

	otherEnv := uuid.New()
	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
		UpdateApplicationInput{EnvironmentID: &otherEnv}); !errors.Is(err, ErrPreviewsOpen) {
		t.Fatalf("move with open previews err = %v, want ErrPreviewsOpen", err)
	}
	otherServer := uuid.New()
	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
		UpdateApplicationInput{ServerID: &otherServer}); !errors.Is(err, ErrPreviewsOpen) {
		t.Fatalf("server change with open previews err = %v, want ErrPreviewsOpen", err)
	}

	delete(repo.livePreviews, app.ID)
	moved, err := svc.UpdateApplication(context.Background(), userID, app.ID,
		UpdateApplicationInput{EnvironmentID: &otherEnv})
	if err != nil {
		t.Fatalf("move after previews closed: %v", err)
	}
	if moved.EnvironmentID != otherEnv {
		t.Fatalf("environment = %s, want %s", moved.EnvironmentID, otherEnv)
	}
}

// TestServerChangeDomainCollisionAnswersOwn409 pins F6: a server change
// that collides on the target node's domain answers 409 with the domain
// message, not the deploy-in-flight text.
func TestServerChangeDomainCollisionAnswersOwn409(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.BaseDomain = "taken.example.com"
	repo := &fakeRepository{
		app:          app,
		updateErr:    ErrConflict, // the per-server domain index fired
		environments: map[uuid.UUID]EnvironmentRef{app.EnvironmentID: {ID: app.EnvironmentID}},
	}
	svc := newTestService(t, repo)

	other := uuid.New()
	err := func() error {
		_, err := svc.UpdateApplication(context.Background(), userID, app.ID,
			UpdateApplicationInput{ServerID: &other})
		return err
	}()
	if !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("domain collision on server change err = %v, want ErrDomainConflict", err)
	}
}

// TestServerChangeDeployCheckPassesThrough pins F6: a ListDeployments failure
// inside the in-flight check surfaces as-is, never as the 409.
func TestServerChangeDeployCheckPassesThrough(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app, listDeploymentsErr: errors.New("database down")}
	svc := newTestService(t, repo)

	other := uuid.New()
	_, err := svc.UpdateApplication(context.Background(), userID, app.ID,
		UpdateApplicationInput{ServerID: &other})
	if err == nil || errors.Is(err, ErrDeployInFlight) {
		t.Fatalf("err = %v, want the raw ListDeployments failure", err)
	}
	if err.Error() != "database down" {
		t.Fatalf("err = %v, want the raw ListDeployments failure", err)
	}
}

// TestUpdateApplicationTeamScope keeps the move inside the team even when the
// caller names an environment of another team: the repository resolves it,
// and a refusal surfaces as ErrNotFound.
func TestUpdateApplicationTeamScope(t *testing.T) {
	userID := uuid.New()
	teamID := uuid.New()
	app := testApplication(userID)
	app.TeamID = teamID
	app.EnvironmentID = uuid.New()
	repo := &fakeRepository{app: app, resolveErr: ErrNotFound}
	svc := newTestService(t, repo)
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: teams.RoleAdmin})

	foreign := uuid.New()
	if _, err := svc.UpdateApplication(ctx, userID, app.ID,
		UpdateApplicationInput{EnvironmentID: &foreign}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign environment err = %v, want ErrNotFound", err)
	}
}

// TestUpdateApplicationRouteMovesEnvironment pins the wire: environment_id
// reaches the service, and the 409 bodies match the contract exactly.
func TestUpdateApplicationRouteMovesEnvironment(t *testing.T) {
	userID := uuid.New()
	appID := uuid.New()
	envID := uuid.New()

	svc := &fakeDeployService{application: sampleApplication()}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+appID.String(),
		strings.NewReader(`{"environment_id":"`+envID.String()+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("move = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	if svc.seenUpdate.EnvironmentID == nil || *svc.seenUpdate.EnvironmentID != envID {
		t.Fatalf("service saw %+v, want the environment move", svc.seenUpdate)
	}

	svc.updateErr = ErrDeployInFlight
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+appID.String(),
		strings.NewReader(`{"server_id":"`+uuid.NewString()+`"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("server change in flight = %d, want 409", rec.Code)
	}
	if got := routeMessage(t, rec.Body.Bytes()); got != "a deploy is in progress" {
		t.Fatalf("body = %q, want the contract message", got)
	}

	svc.updateErr = ErrNameConflict
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+appID.String(),
		strings.NewReader(`{"environment_id":"`+envID.String()+`"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("colliding move = %d, want 409", rec.Code)
	}
}
