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

type deploymentScopeService struct {
	deploy.DeployService
	team   uuid.UUID
	reads  int
	writes int
}

func (s *deploymentScopeService) GetDeployment(ctx context.Context, userID, appID, depID uuid.UUID) (deploy.Deployment, error) {
	s.reads++
	if teams.ScopeFor(ctx, userID).TeamID != s.team {
		return deploy.Deployment{}, deploy.ErrNotFound
	}
	return deploy.Deployment{ID: depID, ApplicationID: appID, State: deploy.StateFailed}, nil
}
func (s *deploymentScopeService) Deploy(_ context.Context, _, _ uuid.UUID) (deploy.Deployment, error) {
	s.writes++
	return deploy.Deployment{}, nil
}
func TestMCPExactDeploymentScopes(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	teamA, teamB, app, dep := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	membership := newFakeTeamService()
	membership.allow(teamA, testUserID, teams.RoleReadOnly)
	membership.allow(teamB, testUserID, teams.RoleAdmin)
	s.teamService = membership
	svc := &deploymentScopeService{team: teamA}
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) { deploy.Mount(api, s.withTeam(), UserIDFromContext, svc) })
	read := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeRead, auth.ScopeDeploy)
	for _, tc := range []struct {
		method, suffix, token string
		team                  uuid.UUID
		want                  int
	}{
		{"GET", "/deployments/" + dep.String(), read, teamA, 200},
		{"GET", "/deployments/" + dep.String(), read, teamB, 404},
		{"GET", "/deployments/" + dep.String(), read, uuid.New(), 403},
		{"GET", "/deployments/" + dep.String(), "", teamA, 401},
		{"POST", "/deploy", read, teamA, 403},
		{"POST", "/deploy", read, teamB, 403},
		{"POST", "/deploy", deployToken, teamA, 403},
		{"POST", "/deploy", deployToken, teamB, 202},
	} {
		prefix := "/api/v1/applications/"
		if tc.method == "POST" {
			prefix = "/api/v1/mcp/applications/"
		}
		req := httptest.NewRequest(tc.method, prefix+app.String()+tc.suffix, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set(TeamHeader, tc.team.String())
		req.Header.Set(mcpContractHeader, "1")
		req.Header.Set("Idempotency-Key", "stable")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Fatalf("%s %s status=%d want=%d body=%s", tc.method, tc.suffix, rr.Code, tc.want, rr.Body.String())
		}
		if rr.Code >= 200 && rr.Code < 300 && (rr.Header().Get(mcpTeamHeader) != tc.team.String() || rr.Header().Get(mcpContractHeader) != "1") {
			t.Fatal("missing strict acknowledgment")
		}
	}
	if svc.reads != 2 || svc.writes != 1 {
		t.Fatalf("domain reads=%d writes=%d", svc.reads, svc.writes)
	}
}
