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

type logsScopeService struct {
	deploy.DeployService
	team  uuid.UUID
	calls int
}

func (s *logsScopeService) GetApplicationLogs(ctx context.Context, userID, appID uuid.UUID, _ deploy.LogOptions) (deploy.ApplicationLogs, error) {
	s.calls++
	if teams.ScopeFor(ctx, userID).TeamID != s.team {
		return deploy.ApplicationLogs{}, deploy.ErrNotFound
	}
	return deploy.ApplicationLogs{ApplicationID: appID, Source: "runtime", Containers: []deploy.ContainerLog{}}, nil
}
func (s *logsScopeService) GetDeploymentLogs(ctx context.Context, userID, appID, depID uuid.UUID, _ deploy.LogOptions) (deploy.DeploymentLogs, error) {
	s.calls++
	if teams.ScopeFor(ctx, userID).TeamID != s.team {
		return deploy.DeploymentLogs{}, deploy.ErrNotFound
	}
	return deploy.DeploymentLogs{ApplicationID: appID, DeploymentID: depID, Source: "persisted_build"}, nil
}
func TestMCPLogReadScopes(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	teamA, teamB, app, dep := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	membership := newFakeTeamService()
	membership.allow(teamA, testUserID, teams.RoleReadOnly)
	membership.allow(teamB, testUserID, teams.RoleAdmin)
	s.teamService = membership
	svc := &logsScopeService{team: teamA}
	router := chi.NewRouter()
	deploy.Mount(router, s.withTeam(), UserIDFromContext, svc)
	read := mustCreateToken(t, tokens, auth.ScopeRead)
	for _, path := range []string{"/v1/applications/" + app.String() + "/logs", "/v1/applications/" + app.String() + "/deployments/" + dep.String() + "/logs?tail=100"} {
		for _, tc := range []struct {
			token string
			team  uuid.UUID
			want  int
		}{{read, teamA, 200}, {read, teamB, 404}, {read, uuid.New(), 403}, {"", teamA, 401}} {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set(TeamHeader, tc.team.String())
			req.Header.Set(mcpContractHeader, "1")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("log read scope status=%d want=%d body=%s", rr.Code, tc.want, rr.Body.String())
			}
			if rr.Code == 200 && (rr.Header().Get(mcpTeamHeader) != teamA.String() || rr.Header().Get(mcpContractHeader) != "1") {
				t.Fatal("missing strict read acknowledgment")
			}
		}
	}
	if svc.calls != 4 {
		t.Fatalf("unauthorized calls reached domain: %d", svc.calls)
	}
}
