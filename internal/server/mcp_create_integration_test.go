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
	adapter "github.com/justindeelux/gotham/internal/mcp"
	"github.com/justindeelux/gotham/internal/projects"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCreationBacked(t *testing.T) {
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
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: "mcp-create"})
	if err != nil {
		t.Fatal(err)
	}
	teamID := uuid.UUID(team.ID.Bytes)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM applications WHERE team_id=$1", teamID)
		_, _ = pool.Exec(ctx, "DELETE FROM servers WHERE team_id=$1", teamID)
		_, _ = pool.Exec(ctx, "DELETE FROM teams WHERE id=$1", teamID)
	}()
	if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{TeamID: team.ID, UserID: user.ID, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewAPITokenService(st, logger)
	token, err := tokens.Create(ctx, principal, "create", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	read, err := tokens.Create(ctx, principal, "read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newScopeTestServer(t)
	s.tokens = tokens
	s.teamService = teams.NewService(teams.Config{Store: st, Logger: logger})
	svc := deploy.NewService(deploy.Config{Store: st, Secret: "stable-mcp-create-key", Logger: logger})
	defer svc.Close()
	projectSvc := projects.NewService(projects.Config{Store: st, Counter: projects.StoreCounter{Store: st}, Logger: logger})
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		s.mountMCPRoutes(api)
		projects.Mount(api, s.withTeam(), UserIDFromContext, projectSvc)
		deploy.Mount(api, s.withTeam(), UserIDFromContext, svc)
	})
	api := httptest.NewServer(router)
	defer api.Close()
	session := func(bearer string) *sdk.ClientSession {
		t.Helper()
		server, err := adapter.NewServer(ctx, adapter.Config{URL: api.URL, Token: bearer, TeamID: teamID}, "test", logger)
		if err != nil {
			t.Fatal(err)
		}
		a, b := sdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		return cs
	}
	cs := session(token.Token)
	call := func(cs *sdk.ClientSession, name string, args map[string]any, wantError bool) map[string]any {
		t.Helper()
		result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError != wantError {
			t.Fatalf("%s error=%v result=%+v", name, err, result)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secret-sentinel") {
			t.Fatal("secret exposed")
		}
		body := map[string]any{}
		raw, _ := json.Marshal(result.StructuredContent)
		_ = json.Unmarshal(raw, &body)
		return body
	}
	projectArgs := map[string]any{"request_id": "project", "name": "shop"}
	call(session(read.Token), "project_create", projectArgs, true)
	out := call(cs, "project_create", projectArgs, false)
	receipt := out["receipt"].(map[string]any)
	projectID := receipt["resource_id"].(string)
	environment := out["environments"].([]any)[0].(map[string]any)
	envID := environment["id"].(string)
	if environment["name"] != "production" || receipt["related_id"] != envID {
		t.Fatal("production identity lost")
	}
	replay := call(cs, "project_create", projectArgs, false)
	if replay["receipt"].(map[string]any)["replayed"] != true {
		t.Fatal("project not replayed")
	}
	call(cs, "project_create", map[string]any{"request_id": "duplicate", "name": "shop"}, true)
	call(cs, "environment_create", map[string]any{"request_id": "environment", "name": "staging", "project_id": projectID}, false)
	call(cs, "environment_create", map[string]any{"request_id": "environment-duplicate", "name": "staging", "project_id": projectID}, true)
	node, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "mcp-node", Ip: "127.0.0.1", Port: 22, SshUser: "root", TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	nodeID := uuid.UUID(node.ID.Bytes).String()
	base := func(source string) map[string]any {
		args := map[string]any{"request_id": source, "name": source, "source_type": source, "environment_id": envID, "server_id": nodeID}
		switch source {
		case "git_public", "git_private":
			args["clone_url"] = "https://example.com/repo.git"
		case "github_app", "gitlab_app":
			args["provider"] = strings.TrimSuffix(source, "_app")
			args["repo"] = "owner/repo"
			args["clone_url"] = "https://example.com/repo.git"
		case "dockerfile":
			args["dockerfile_content"] = "FROM busybox\nCMD [\"true\"]"
		case "compose":
			args["compose_content"] = "services:\n  web:\n    image: nginx:alpine\n"
			args["compose_service"] = "web"
		case "image":
			args["image_ref"] = "nginx:alpine"
			args["registry_username"] = "user"
			args["registry_password"] = "secret-sentinel"
		}
		return args
	}
	var imageID string
	for _, source := range []string{"git_public", "git_private", "github_app", "gitlab_app", "dockerfile", "compose", "image"} {
		t.Run(source, func(t *testing.T) {
			args := base(source)
			out := call(cs, "application_create", args, false)
			app := out["application"].(map[string]any)
			id := app["id"].(string)
			if app["environment_id"] != envID || app["server_id"] != nodeID || app["environment_name"] != "production" {
				t.Fatal("target not preserved")
			}
			again := call(cs, "application_create", args, false)
			if again["receipt"].(map[string]any)["replayed"] != true || again["application"].(map[string]any)["id"] != id {
				t.Fatal("app recreated")
			}
			if source == "git_public" {
				var branch string
				if err := pool.QueryRow(ctx, "SELECT branch FROM applications WHERE id=$1", id).Scan(&branch); err != nil || branch != "" {
					t.Fatalf("remote branch default %q %v", branch, err)
				}
			}
			if source == "image" {
				imageID = id
			}
		})
	}
	publicImage := base("image")
	publicImage["request_id"], publicImage["name"] = "public-image", "public-image"
	delete(publicImage, "registry_username")
	delete(publicImage, "registry_password")
	call(cs, "application_create", publicImage, false)
	repoCompose := base("compose")
	repoCompose["request_id"], repoCompose["name"] = "repo-compose", "repo-compose"
	delete(repoCompose, "compose_content")
	repoCompose["clone_url"], repoCompose["compose_file"] = "https://example.com/repo.git", "compose.yml"
	call(cs, "application_create", repoCompose, false)
	foreignTeam, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: "foreign-mcp"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM servers WHERE team_id=$1", foreignTeam.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM teams WHERE id=$1", foreignTeam.ID)
	}()
	foreignCtx := teams.WithScope(ctx, teams.Scope{UserID: principal, TeamID: uuid.UUID(foreignTeam.ID.Bytes), Role: teams.RoleAdmin})
	_, foreignEnv, err := projectSvc.CreateProject(foreignCtx, principal, "foreign-project", "")
	if err != nil {
		t.Fatal(err)
	}
	foreignNode, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "foreign-node", Ip: "127.0.0.1", Port: 22, SshUser: "root", TeamID: foreignTeam.ID})
	if err != nil {
		t.Fatal(err)
	}
	for field, id := range map[string]string{"environment_id": foreignEnv.ID.String(), "server_id": uuid.UUID(foreignNode.ID.Bytes).String()} {
		args := base("image")
		args["request_id"] = "cross-team-" + field
		args[field] = id
		call(cs, "application_create", args, true)
	}
	call(cs, "environment_create", map[string]any{"request_id": "foreign-project", "name": "blocked", "project_id": foreignEnv.ProjectID.String()}, true)

	var deployments, domains int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM deployments WHERE application_id IN (SELECT id FROM applications WHERE team_id=$1)", teamID).Scan(&deployments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM application_domains WHERE application_id IN (SELECT id FROM applications WHERE team_id=$1)", teamID).Scan(&domains); err != nil {
		t.Fatal(err)
	}
	if deployments != 0 || domains != 0 {
		t.Fatal("creation deployed or attached a domain")
	}
	for _, field := range []string{"environment_id", "server_id", "github_app_id"} {
		args := base("github_app")
		args["request_id"] = "foreign-" + field
		args[field] = uuid.NewString()
		call(cs, "application_create", args, true)
	}
	// Another user's connection stays foreign even when both users belong to this team.
	other, err := st.CreateUser(ctx, uuid.NewString()+"@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", other.ID) }()
	if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{TeamID: team.ID, UserID: other.ID, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateGitHubApp(ctx, sqlc.CreateGitHubAppParams{UserID: other.ID, AppID: 12345, Slug: "other", Name: "other", BaseUrl: "https://github.com", ApiBaseUrl: "https://api.github.com"})
	if err != nil {
		t.Fatal(err)
	}
	args := base("github_app")
	args["request_id"] = "other-user"
	args["github_app_id"] = uuid.UUID(connection.ID.Bytes).String()
	call(cs, "application_create", args, true)
	if _, err := pool.Exec(ctx, "UPDATE team_members SET role='read_only' WHERE team_id=$1 AND user_id=$2", teamID, principal); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"project_create", "environment_create", "application_create"} {
		args := projectArgs
		if name == "environment_create" {
			args = map[string]any{"request_id": "environment", "name": "staging", "project_id": projectID}
		}
		if name == "application_create" {
			args = base("image")
		}
		call(cs, name, args, true)
	}
	if _, err := pool.Exec(ctx, "UPDATE team_members SET role='admin' WHERE team_id=$1 AND user_id=$2", teamID, principal); err != nil {
		t.Fatal(err)
	}
	// A moved, deleted or expired target must never be replaced by replay.
	var staging string
	if err := pool.QueryRow(ctx, "SELECT id FROM environments WHERE project_id=$1 AND name='staging'", projectID).Scan(&staging); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE applications SET environment_id=$1 WHERE id=$2", staging, imageID); err != nil {
		t.Fatal(err)
	}
	call(cs, "application_create", base("image"), true)
	if _, err := pool.Exec(ctx, "DELETE FROM applications WHERE id=$1", imageID); err != nil {
		t.Fatal(err)
	}
	call(cs, "application_create", base("image"), true)
	if _, err := pool.Exec(ctx, "UPDATE request_receipts SET accepted_at=clock_timestamp()-interval '25 hours' WHERE principal_id=$1 AND request_key='git_public'", principal); err != nil {
		t.Fatal(err)
	}
	call(cs, "application_create", base("git_public"), true)
}
