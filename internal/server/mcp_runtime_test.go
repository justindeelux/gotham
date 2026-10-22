package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/teams"
)

type runtimeScopeService struct {
	deploy.DeployService
	team  uuid.UUID
	calls int
}

func (s *runtimeScopeService) GetApplicationRuntime(ctx context.Context, userID, appID uuid.UUID) (deploy.ApplicationRuntime, error) {
	s.calls++
	scope := teams.ScopeFor(ctx, userID)
	if scope.TeamID != s.team {
		return deploy.ApplicationRuntime{}, deploy.ErrNotFound
	}
	return deploy.ApplicationRuntime{ApplicationID: appID, Coverage: "complete", Containers: []deploy.RuntimeContainer{}}, nil
}
func TestMCPRuntimeReadScopes(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	teamA, teamB, app := uuid.New(), uuid.New(), uuid.New()
	membership := newFakeTeamService()
	membership.allow(teamA, testUserID, teams.RoleReadOnly)
	membership.allow(teamB, testUserID, teams.RoleAdmin)
	s.teamService = membership
	svc := &runtimeScopeService{team: teamA}
	router := chi.NewRouter()
	deploy.Mount(router, s.withTeam(), UserIDFromContext, svc)
	read := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	for _, test := range []struct {
		token string
		team  uuid.UUID
		want  int
	}{{read, teamA, 200}, {read, teamB, 404}, {read, uuid.New(), 403}, {deployToken, teamA, 200}, {"", teamA, 401}} {
		req := httptest.NewRequest("GET", "/v1/applications/"+app.String()+"/runtime", nil)
		req.Header.Set("Authorization", "Bearer "+test.token)
		req.Header.Set(TeamHeader, test.team.String())
		req.Header.Set(mcpContractHeader, "1")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != test.want {
			t.Fatalf("runtime scope status=%d want=%d body=%s", recorder.Code, test.want, recorder.Body.String())
		}
		if recorder.Code == 200 && (recorder.Header().Get(mcpTeamHeader) != teamA.String() || recorder.Header().Get(mcpContractHeader) != "1") {
			t.Fatal("runtime read omitted strict acknowledgement")
		}
	}
	if svc.calls != 3 {
		t.Fatalf("unauthorized calls reached domain: %d", svc.calls)
	}
}
