package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// Opt-in uses only the explicitly supplied disposable database.
func TestNodeObservationBackedVisibility(t *testing.T) {
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		t.Skip("GOTHAM_TEST_DSN required for backed node observation")
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatal(err)
	}
	teamA, teamB := uuid.New(), uuid.New()
	for _, team := range []uuid.UUID{teamA, teamB} {
		if _, err := pool.Exec(ctx, "INSERT INTO teams (id,name) VALUES ($1,'observation')", team); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM teams WHERE id = ANY($1)", []uuid.UUID{teamA, teamB}) }()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := servers.NewService(servers.Config{Store: store.New(pool), Secret: "test-secret", Logger: logger})
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM servers WHERE id = ANY($1)", ids) }()
	for i, id := range ids {
		var team any
		switch i {
		case 0, 3:
			team = teamA
		case 1:
			team = teamB
		}
		node := "observation-" + id.String()
		if _, err := pool.Exec(ctx, "INSERT INTO servers (id,name,ip,ssh_user,team_id,node_id,status,last_seen) VALUES ($1,'observation','192.0.2.1','root',$2,$3,'offline',now()-interval '1 hour')", id, team, node); err != nil {
			t.Fatal(err)
		}
		if i != 3 {
			registry.RecordAgentVersion(node, "v1.2."+string(rune('0'+i)))
		}
	}
	membership := newFakeTeamService()
	membership.allow(teamA, testUserID, teams.RoleReadOnly)
	tokens := newFakeTokenService()
	s, err := New(&config.Config{}, logger, newFakeAuthService(), nil, tokens, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.closer()
	s.teamService = membership
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
	found := map[string]serverDTO{}
	for _, node := range list.Servers {
		found[node.ID] = node
	}
	for _, i := range []int{0, 2} {
		node, ok := found[ids[i].String()]
		if !ok || node.AgentVersion == nil || node.AgentVersionReportedAt == nil || node.Status != "offline" {
			t.Fatalf("authorized heartbeat=%+v", node)
		}
	}
	if _, ok := found[ids[1].String()]; ok || strings.Contains(rec.Body.String(), "v1.2.1") {
		t.Fatal("foreign version exposed")
	}
	if node := found[ids[3].String()]; node.AgentVersion != nil || node.AgentVersionReportedAt != nil {
		t.Fatal("unknown version invented")
	}
	rec = read("/api/v1/servers/" + ids[1].String())
	if rec.Code != 404 {
		t.Fatalf("foreign detail=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := pool.Exec(ctx, "DELETE FROM servers WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if rec := read("/api/v1/servers/" + ids[0].String()); rec.Code != 404 {
		t.Fatalf("deleted heartbeat exposed=%d", rec.Code)
	}
	if rec := read("/api/v1/servers/agents"); rec.Code != 403 {
		t.Fatalf("read token reached global map=%d %s", rec.Code, rec.Body.String())
	}
}
