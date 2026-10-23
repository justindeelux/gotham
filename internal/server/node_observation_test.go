package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
)

type observedRegistry struct {
	*fakeServerService
	versions map[string]servers.AgentVersion
	lookedUp []string
}

func (f *observedRegistry) List(ctx context.Context) ([]servers.Server, error) {
	items, err := f.fakeServerService.List(ctx)
	visible := make([]servers.Server, 0)
	for _, item := range items {
		if teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(item.TeamID, false) == nil {
			visible = append(visible, item)
		}
	}
	return visible, err
}
func (f *observedRegistry) Get(ctx context.Context, id uuid.UUID) (*servers.Server, error) {
	item, err := f.fakeServerService.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(item.TeamID, false) != nil {
		return nil, servers.ErrNotFound
	}
	return item, nil
}
func (f *observedRegistry) AgentVersionForNode(id string) (servers.AgentVersion, bool) {
	f.lookedUp = append(f.lookedUp, id)
	entry, ok := f.versions[id]
	return entry, ok
}

func TestAuthorizedNodeVersions(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	teamA, teamB := uuid.New(), uuid.New()
	membership := newFakeTeamService()
	membership.allow(teamA, testUserID, teams.RoleReadOnly)
	s.teamService = membership
	registry := &observedRegistry{fakeServerService: newFakeServerService(), versions: map[string]servers.AgentVersion{}}
	s.servers = registry
	nodeA, nodeB, shared, unreported := "agent-a", "agent-b", "agent-shared", "unreported"
	now := time.Now().UTC()
	var owned, foreign, legacy, noNode, noVersion uuid.UUID
	for _, tc := range []struct {
		id      *uuid.UUID
		team    *uuid.UUID
		node    *string
		version string
	}{
		{&owned, &teamA, &nodeA, "v1.2.3"}, {&foreign, &teamB, &nodeB, "foreign-version"},
		{&legacy, nil, &shared, "v2.3.4"}, {&noNode, &teamA, nil, ""}, {&noVersion, &teamA, &unreported, ""},
	} {
		*tc.id = uuid.New()
		item := servers.Server{ID: *tc.id, NodeID: tc.node, Status: servers.StatusOffline}
		if tc.team != nil {
			item.TeamID = *tc.team
		}
		registry.items[*tc.id] = item
		if tc.version != "" {
			registry.versions[*tc.node] = servers.AgentVersion{NodeID: *tc.node, Version: tc.version, At: now}
		}
	}
	deleted := uuid.New()
	registry.versions[deleted.String()] = servers.AgentVersion{Version: "deleted-version", At: now}
	token := mustCreateToken(t, tokens, auth.ScopeRead)
	read := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(TeamHeader, teamA.String())
		req.Header.Set(mcpContractHeader, "1")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := read("/api/v1/servers")
	if rec.Code != 200 {
		t.Fatalf("list=%d %s", rec.Code, rec.Body.String())
	}
	var list serverListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Servers) != 4 {
		t.Fatalf("visible=%+v", list)
	}
	for _, node := range list.Servers {
		switch node.ID {
		case owned.String(), legacy.String():
			if node.AgentVersion == nil || node.AgentVersionReportedAt == nil || !node.AgentVersionReportedAt.Equal(now) {
				t.Fatalf("missing heartbeat=%+v", node)
			}
		case noNode.String(), noVersion.String():
			if node.AgentVersion != nil || node.AgentVersionReportedAt != nil {
				t.Fatalf("invented version=%+v", node)
			}
		default:
			t.Fatalf("foreign data=%+v", node)
		}
	}
	for _, id := range []uuid.UUID{foreign, deleted} {
		if rec := read("/api/v1/servers/" + id.String()); rec.Code != 404 {
			t.Fatalf("hidden node=%d %s", rec.Code, rec.Body.String())
		}
	}
	for _, id := range registry.lookedUp {
		if id == nodeB || id == deleted.String() {
			t.Fatalf("unauthorized lookup=%s", id)
		}
	}
	if rec := read("/api/v1/servers/agents"); rec.Code != 400 {
		t.Fatalf("global route exposed=%d", rec.Code)
	}
	registry.versions = map[string]servers.AgentVersion{}
	rec = read("/api/v1/servers/" + owned.String())
	var detail serverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || detail.Server.AgentVersion != nil || detail.Server.AgentVersionReportedAt != nil {
		t.Fatalf("restart=%s", rec.Body.String())
	}
	var raw map[string]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if value, ok := raw["server"]["agent_version"]; !ok || value != nil {
		t.Fatal("unknown version must be explicit null")
	}
}
