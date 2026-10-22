package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/teams"
)

func TestMCPStrictContextAndReads(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	teamA, teamB := uuid.New(), uuid.New()
	membership := newFakeTeamService()
	membership.allow(teamA, testUserID, teams.RoleReadOnly)
	membership.allow(teamB, testUserID, teams.RoleAdmin)
	s.teamService = membership
	read := mustCreateToken(t, tokens, auth.ScopeRead)
	request := func(method, path, team, token, contract string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(TeamHeader, team)
		req.Header.Set(mcpContractHeader, contract)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	for _, team := range []uuid.UUID{teamA, teamB} {
		for _, path := range []string{"/api/v1/mcp/context", "/api/v1/servers"} {
			rec := request("GET", path, team.String(), read, "1")
			if rec.Code != 200 || rec.Header().Get(mcpContractHeader) != "1" || rec.Header().Get(mcpTeamHeader) != team.String() {
				t.Fatalf("strict response %s: %d %s %+v", path, rec.Code, rec.Body.String(), rec.Header())
			}
			if path == "/api/v1/mcp/context" {
				var body struct {
					TeamID       uuid.UUID `json:"team_id"`
					Capabilities []string  `json:"capabilities"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.TeamID != team || len(body.Capabilities) != 2 {
					t.Fatalf("context %+v %v", body, err)
				}
			}
		}
	}
	for _, tc := range []struct {
		name, team, token, contract string
		want                        int
	}{
		{"missing token", teamA.String(), "", "1", 401},
		{"invalid token", teamA.String(), "gt1_invalid", "1", 401},
		{"foreign team", uuid.NewString(), read, "1", 403},
		{"nil team", uuid.Nil.String(), read, "1", 400},
		{"missing team", "", read, "1", 400},
		{"invalid team", "bad", read, "1", 400},
		{"missing contract", teamA.String(), read, "", 400},
		{"wrong contract", teamA.String(), read, "2", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request("GET", "/api/v1/mcp/context", tc.team, tc.token, tc.contract)
			if rec.Code != tc.want {
				t.Fatalf("%d want %d body %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	if rec := request("POST", "/api/v1/servers", teamA.String(), read, "1"); rec.Code != 403 {
		t.Fatalf("read mutation=%d", rec.Code)
	}
	deploy := mustCreateToken(t, tokens, auth.ScopeDeploy)
	if rec := request("POST", "/api/v1/servers", teamB.String(), deploy, "1"); rec.Code != 405 {
		t.Fatalf("legacy mutation with strict header=%d", rec.Code)
	}
	delete(membership.roles, [2]uuid.UUID{teamA, testUserID})
	for _, path := range []string{"/api/v1/mcp/context", "/api/v1/servers"} {
		if rec := request("GET", path, teamA.String(), read, "1"); rec.Code != 403 {
			t.Fatalf("removed member=%d", rec.Code)
		}
	}
	for _, feature := range []string{"false", "true"} {
		t.Setenv(teams.FeatureEnv, feature)
		if feature == "true" {
			s.teamService = nil
		}
		for _, path := range []string{"/api/v1/mcp/context", "/api/v1/servers"} {
			if rec := request("GET", path, teamB.String(), read, "1"); rec.Code != 503 || rec.Header().Get(mcpTeamHeader) != "" {
				t.Fatalf("unavailable teams=%d headers=%+v", rec.Code, rec.Header())
			}
		}
	}
}

func TestMCPRevokedToken(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	team := uuid.New()
	membership := newFakeTeamService()
	membership.allow(team, testUserID, teams.RoleReadOnly)
	s.teamService = membership
	token, err := tokens.Create(context.Background(), testUserID, "mcp", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	send := func() int {
		req := httptest.NewRequest("GET", "/api/v1/mcp/context", nil)
		req.Header.Set("Authorization", "Bearer "+token.Token)
		req.Header.Set(TeamHeader, team.String())
		req.Header.Set(mcpContractHeader, "1")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if send() != 200 {
		t.Fatal("initial context failed")
	}
	if err := tokens.Revoke(context.Background(), testUserID, token.ID); err != nil {
		t.Fatal(err)
	}
	if got := send(); got != 401 {
		t.Fatalf("revoked token=%d", got)
	}
}
