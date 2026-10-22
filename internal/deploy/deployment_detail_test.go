package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/teams"
)

func (f *fakeDeployService) GetDeployment(_ context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error) {
	f.seenUser, f.seenApplication, f.seenRollback = userID, appID, deploymentID
	return f.deploy, f.deployErr
}

func TestExactDeploymentRead(t *testing.T) {
	app := testApplication(uuid.New())
	app.TeamID = uuid.New()
	old := Deployment{ID: uuid.New(), ApplicationID: app.ID, State: StateFailed, BuildLog: "secret-log", ComposeDocument: "secret-compose"}
	repo := &fakeRepository{app: app, deployments: []Deployment{old}}
	svc := newTestService(t, repo)
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: app.UserID, TeamID: app.TeamID, Role: teams.RoleReadOnly})
	for i := 0; i < 101; i++ {
		if _, err := repo.CreateDeployment(ctx, Deployment{ApplicationID: app.ID, State: StateRunning}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.GetDeployment(ctx, app.UserID, app.ID, old.ID)
	if err != nil || got.ID != old.ID {
		t.Fatalf("old release=%+v err=%v", got, err)
	}
	for _, tc := range []struct {
		ctx      context.Context
		app, dep uuid.UUID
		want     error
	}{
		{ctx, uuid.New(), old.ID, ErrNotFound},
		{ctx, app.ID, uuid.New(), ErrNotFound},
		{ctx, app.ID, uuid.Nil, ErrValidation},
		{teams.WithScope(ctx, teams.Scope{UserID: app.UserID, TeamID: uuid.New(), Role: teams.RoleAdmin}), app.ID, old.ID, ErrNotFound},
	} {
		if _, err := svc.GetDeployment(tc.ctx, app.UserID, tc.app, tc.dep); !errors.Is(err, tc.want) {
			t.Fatalf("err=%v want=%v", err, tc.want)
		}
	}
	route := newRouteServer(&fakeDeployService{deploy: old}, func(context.Context) (uuid.UUID, bool) { return app.UserID, true })
	rr := httptest.NewRecorder()
	route.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/applications/"+app.ID.String()+"/deployments/"+old.ID.String(), nil))
	if rr.Code != 200 || strings.Contains(rr.Body.String(), "secret-") {
		t.Fatalf("response=%d %s", rr.Code, rr.Body.String())
	}
	var body deploymentEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body.Deployment.ID != old.ID.String() {
		t.Fatalf("body=%+v %v", body, err)
	}
}
