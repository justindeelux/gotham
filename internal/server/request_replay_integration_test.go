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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/projects"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

func TestReplayBackedMutationAuthorization(t *testing.T) {
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated GOTHAM_TEST_DSN required")
	}
	t.Setenv("FEATURE_TEAMS", "true")
	t.Setenv("FEATURE_APPLICATIONS", "true")
	ctx := context.Background()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	user, err := st.CreateUser(ctx, uuid.NewString()+"@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := uuid.UUID(user.ID.Bytes)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM request_receipts WHERE principal_id=$1", principal)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", principal)
	}()
	tokenSvc := auth.NewAPITokenService(st, logger)
	token, err := tokenSvc.Create(ctx, principal, "replay", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	readToken, err := tokenSvc.Create(ctx, principal, "read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: "replay"})
	if err != nil {
		t.Fatal(err)
	}
	teamID := uuid.UUID(team.ID.Bytes)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM applications WHERE team_id=$1", teamID)
		_, _ = pool.Exec(ctx, "DELETE FROM teams WHERE id=$1", teamID)
	}()
	if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{TeamID: team.ID, UserID: user.ID, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	s, _ := newScopeTestServer(t)
	s.tokens = tokenSvc
	s.teamService = teams.NewService(teams.Config{Store: st, Logger: logger})
	svc := deploy.NewService(deploy.Config{Store: st, Secret: "stable-replay-secret", Logger: logger})
	defer svc.Close()
	projectSvc := projects.NewService(projects.Config{Store: st, Counter: projects.StoreCounter{Store: st}, Logger: logger})
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		projects.Mount(api, s.withTeam(), UserIDFromContext, projectSvc)
		deploy.Mount(api, s.withTeam(), UserIDFromContext, svc)
	})
	send := func(path, key, body, bearer, team, contract string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set(TeamHeader, team)
		if contract != "" {
			req.Header.Set(mcpContractHeader, contract)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	create := func(path, key, body string) *httptest.ResponseRecorder {
		return send(path, key, body, token.Token, teamID.String(), "1")
	}
	decode := func(rec *httptest.ResponseRecorder, status int) map[string]json.RawMessage {
		t.Helper()
		if rec.Code != status {
			t.Fatalf("status %d want %d: %s", rec.Code, status, rec.Body.String())
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	path := "/api/v1/mcp/projects"
	for _, tc := range []struct {
		name, key, token, team, contract string
		status                           int
	}{
		{"read", "k", readToken.Token, teamID.String(), "1", 403},
		{"jwt", "k", "valid-token", teamID.String(), "1", 403},
		{"missing-key", "", token.Token, teamID.String(), "1", 400},
		{"bad-key", "bad key", token.Token, teamID.String(), "1", 400},
		{"missing-team", "k", token.Token, "", "1", 400},
		{"foreign-team", "k", token.Token, uuid.NewString(), "1", 403},
		{"missing-contract", "k", token.Token, teamID.String(), "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decode(send(path, tc.key, `{"name":"blocked"}`, tc.token, tc.team, tc.contract), tc.status)
		})
	}
	body := decode(create(path, "project", `{"name":" shop ","description":""}`), 201)
	var receipt requestreplay.Result
	if err := json.Unmarshal(body["receipt"], &receipt); err != nil || receipt.Replayed {
		t.Fatalf("first receipt %+v %v", receipt, err)
	}
	var project struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body["project"], &project)
	var envs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(body["environments"], &envs)
	if len(envs) != 1 || envs[0].Name != "production" {
		t.Fatalf("production %s", body["environments"])
	}
	again := decode(create(path, "project", `{"name":"shop"}`), 201)
	var replay requestreplay.Result
	_ = json.Unmarshal(again["receipt"], &replay)
	if !replay.Replayed || receipt.ResourceID != replay.ResourceID || receipt.RelatedID != replay.RelatedID {
		t.Fatal("project replay identity changed")
	}
	decode(create(path, "project", `{"name":"different"}`), 409)
	envPath := path + "/" + project.ID + "/environments"
	decode(create(envPath, "staging", `{"name":"staging"}`), 201)
	decode(create(envPath, "staging", `{"name":" staging "}`), 201)
	node, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "replay-node", Ip: "127.0.0.1", Port: 22, SshUser: "root", TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM applications WHERE server_id=$1", node.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM servers WHERE id=$1", node.ID)
	}()
	appPath := "/api/v1/mcp/applications"
	appBody := `{"name":"image","source_type":"image","image_ref":"busybox:latest","server_id":"` + uuid.UUID(node.ID.Bytes).String() + `","environment_id":"` + envs[0].ID + `","env":[{"key":"PASSWORD","value":"secret:private-password"}]}`
	a := decode(create(appPath, "app", appBody), 201)
	var app struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(a["application"], &app)
	a = decode(create(appPath, "app", appBody), 201)
	_ = json.Unmarshal(a["receipt"], &replay)
	if !replay.Replayed {
		t.Fatal("app was not replayed")
	}
	decode(create(appPath, "app", strings.Replace(appBody, "private-password", "changed-secret", 1)), 409)
	deployPath := appPath + "/" + app.ID + "/deploy"
	d := decode(create(deployPath, "deploy", ``), 202)
	_ = json.Unmarshal(d["receipt"], &receipt)
	d = decode(create(deployPath, "deploy", `{}`), 202)
	_ = json.Unmarshal(d["receipt"], &replay)
	if !replay.Replayed || receipt.ResourceID != replay.ResourceID {
		t.Fatal("deploy identity changed")
	}
	decode(create(deployPath, "invalid", `{"force":true}`), 400)
	// Current membership and revocation are read again before any receipt access.
	if _, err := pool.Exec(ctx, "UPDATE team_members SET role='read_only' WHERE team_id=$1 AND user_id=$2", teamID, principal); err != nil {
		t.Fatal(err)
	}
	decode(create(path, "project", `{"name":"shop"}`), 403)
	if _, err := pool.Exec(ctx, "UPDATE team_members SET role='admin' WHERE team_id=$1 AND user_id=$2", teamID, principal); err != nil {
		t.Fatal(err)
	}
	decode(create(path, "project", `{"name":"shop"}`), 201)
	if err := tokenSvc.Revoke(ctx, principal, token.ID); err != nil {
		t.Fatal(err)
	}
	decode(create(path, "project", `{"name":"shop"}`), 401)
	// The original no-header path still accepts ordinary callers without MCP headers.
	decode(send("/api/v1/projects", "", `{"name":"legacy"}`, readToken.Token, teamID.String(), ""), 403)
	legacy, err := tokenSvc.Create(ctx, principal, "legacy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	response := decode(send("/api/v1/projects", "", `{"name":"legacy"}`, legacy.Token, teamID.String(), ""), 201)
	if _, ok := response["receipt"]; ok {
		t.Fatal("legacy response changed")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM team_members WHERE team_id=$1 AND user_id=$2", teamID, principal); err != nil {
		t.Fatal(err)
	}
	decode(send(path, "project", `{"name":"shop"}`, legacy.Token, teamID.String(), "1"), 403)
}
