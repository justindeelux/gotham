package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	adapter "github.com/justindeelux/gotham/internal/mcp"
	"github.com/justindeelux/gotham/internal/projects"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpContractHeaderE2E = "X-Gotham-MCP-Contract"
	mcpTeamHeaderE2E     = "X-Gotham-MCP-Team-Id"
	mcpWorkflowSecret    = "gotham-mcp-workflow-secret-key"
)

// TestMCPAgentDeploymentWorkflow is the backed S8 proof: the real adapter over
// a real PostgreSQL, Redis and mTLS node agent on Docker drives inspect/create
// /deploy/poll/logs through the production strict REST aliases. It refuses the
// shared development dependency ports, so a run can never touch
// gotham-dev-postgres or gotham-dev-redis.
func TestMCPAgentDeploymentWorkflow(t *testing.T) {
	requireE2E(t)
	requireIsolatedMCPEnvironment(t)
	t.Setenv(cloneLocalEnv, "true")
	t.Setenv(teams.FeatureEnv, "true")
	t.Setenv(deploy.FeatureEnv, "")
	t.Setenv("GOTHAM_DEV_CLONE_LOCAL", "true")
	removeNewDanglingImages(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	dsn := strings.TrimSpace(os.Getenv(e2eDSNEnv))
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate isolated database: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated database: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)
	logger := testLogger(t)

	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = engine.Version(versionCtx)
	versionCancel()
	if err != nil {
		t.Fatalf("E2E requires Docker at %s: %v", e2eDockerSock(), err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: e2eRedisAddr()})
	t.Cleanup(func() { _ = rdb.Close() })
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		pingCancel()
		t.Fatalf("E2E requires Redis at %s: %v", e2eRedisAddr(), err)
	}
	pingCancel()

	// Rows: one user who belongs to two teams, plus a node owned by team A.
	user, err := st.CreateUser(ctx, fmt.Sprintf("mcp-e2e-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	teamA, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: "mcp-team-a"})
	if err != nil {
		t.Fatal(err)
	}
	teamB, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: "mcp-team-b"})
	if err != nil {
		t.Fatal(err)
	}
	teamAID, teamBID := uuid.UUID(teamA.ID.Bytes), uuid.UUID(teamB.ID.Bytes)
	for _, team := range []uuid.UUID{teamAID, teamBID} {
		if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
			TeamID: pgtype.UUID{Bytes: team, Valid: true}, UserID: user.ID, Role: string(teams.RoleAdmin),
		}); err != nil {
			t.Fatal(err)
		}
	}
	node, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "mcp-e2e-node", Ip: "127.0.0.1", Port: 22, SshUser: "root", TeamID: pgtype.UUID{Bytes: teamAID, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	nodeID := uuid.UUID(node.ID.Bytes)
	if _, err := pool.Exec(ctx, "UPDATE servers SET status='ready', last_seen=now() WHERE id=$1", node.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, query := range []string{
			"DELETE FROM applications WHERE team_id IN ($1,$2)",
			"DELETE FROM projects WHERE team_id IN ($1,$2)",
			"DELETE FROM team_members WHERE team_id IN ($1,$2)",
		} {
			if _, err := pool.Exec(cleanupCtx, query, teamAID, teamBID); err != nil {
				t.Logf("cleanup %q: %v", query, err)
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id=$1", nodeID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id IN ($1,$2)", teamAID, teamBID); err != nil {
			t.Logf("cleanup teams: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM request_receipts WHERE principal_id=$1", userID); err != nil {
			t.Logf("cleanup receipts: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id=$1", userID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	// The real agent on the local Docker daemon.
	agentCtx, agentCancel := context.WithCancel(context.Background())
	t.Cleanup(agentCancel)
	agentNodeID := "mcp-e2e-" + uuid.NewString()[:8]
	agentAddr, authority := startLocalAgentWithOptions(t, agentCtx, engine, agentNodeID)
	dialer := &localDialer{serverID: nodeID, addr: agentAddr, nodeID: agentNodeID, authority: authority}
	deploySvc := deploy.NewService(deploy.Config{
		Store: st, Secret: mcpWorkflowSecret, RedisAddr: e2eRedisAddr(),
		Logger: logger, Dial: deploy.AgentDial(dialer), Workers: 2,
	})
	t.Cleanup(func() { _ = deploySvc.Close() })
	projectSvc := projects.NewService(projects.Config{
		Store: st, Counter: projects.StoreCounter{Store: st}, Logger: logger,
	})
	serverSvc := servers.NewService(servers.Config{Store: st, Secret: mcpWorkflowSecret, Logger: logger})
	tokens := auth.NewAPITokenService(st, logger)
	teamSvc := teams.NewService(teams.Config{Store: st, Logger: logger})

	deployToken, err := tokens.Create(ctx, userID, "mcp-deploy", []string{auth.ScopeRead, auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	readToken, err := tokens.Create(ctx, userID, "mcp-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := tokens.Create(ctx, userID, "mcp-revoked", []string{auth.ScopeRead, auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Revoke(ctx, userID, revoked.ID); err != nil {
		t.Fatal(err)
	}

	h := &mcpWorkflowHarness{userID: userID, tokens: tokens, teams: teamSvc}
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		api.With(h.authorize).Get("/v1/mcp/context", h.context)
		projects.Mount(api, h.authorize, h.currentUser, projectSvc)
		deploy.Mount(api, h.authorize, h.currentUser, deploySvc)
		api.With(h.authorize).Get("/v1/servers", func(w http.ResponseWriter, r *http.Request) {
			list, err := serverSvc.List(r.Context())
			if err != nil {
				mcpWriteJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
				return
			}
			out := make([]map[string]any, 0, len(list))
			for _, item := range list {
				out = append(out, mcpNodeJSON(item))
			}
			mcpWriteJSON(w, http.StatusOK, map[string]any{"servers": out})
		})
		api.With(h.authorize).Get("/v1/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, err := uuid.Parse(chi.URLParam(r, "id"))
			if err != nil {
				mcpWriteJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid server id"})
				return
			}
			item, err := serverSvc.Get(r.Context(), id)
			if err != nil {
				mcpWriteJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
				return
			}
			mcpWriteJSON(w, http.StatusOK, map[string]any{"server": mcpNodeJSON(*item)})
		})
	})
	api := httptest.NewServer(router)
	t.Cleanup(api.Close)
	h.baseURL = api.URL

	owner := h.session(t, deployToken.Token, teamAID)
	readOnly := h.session(t, readToken.Token, teamAID)
	foreign := h.session(t, deployToken.Token, teamBID)
	if _, err := adapter.NewServer(ctx, adapter.Config{URL: api.URL, Token: revoked.Token, TeamID: teamAID}, "test", logger); err == nil {
		t.Fatal("revoked token initialized the adapter")
	}

	// Inspect the authorized node before mutating anything.
	nodes := h.call(t, owner, "nodes_list", map[string]any{"limit": 10})
	if nodes["total"] != float64(1) || len(nodes["nodes"].([]any)) != 1 {
		t.Fatalf("nodes_list=%+v", nodes)
	}
	if got := nodes["nodes"].([]any)[0].(map[string]any)["id"]; got != nodeID.String() {
		t.Fatalf("node id=%v want %s", got, nodeID)
	}
	nodeOut := h.call(t, owner, "node_overview", map[string]any{"node_id": nodeID.String()})
	if nodeOut["node"].(map[string]any)["status"] != "ready" {
		t.Fatalf("node_overview=%+v", nodeOut)
	}
	// A stopped heartbeat is reported as stale by the stored snapshot.
	if _, err := pool.Exec(ctx, "UPDATE servers SET last_seen=now()-interval '5 minutes' WHERE id=$1", nodeID); err != nil {
		t.Fatal(err)
	}
	stale := h.call(t, owner, "node_overview", map[string]any{"node_id": nodeID.String()})
	if stale["node"].(map[string]any)["stale"] != true {
		t.Fatalf("stale node=%+v", stale)
	}
	if _, err := pool.Exec(ctx, "UPDATE servers SET last_seen=now() WHERE id=$1", nodeID); err != nil {
		t.Fatal(err)
	}

	// Create project -> returned production environment -> explicit staging.
	project := h.call(t, owner, "project_create", map[string]any{"request_id": "mcp-project", "name": "mcp-shop"})
	projectReceipt := project["receipt"].(map[string]any)
	projectID := projectReceipt["resource_id"].(string)
	production := project["environments"].([]any)[0].(map[string]any)
	productionID := production["id"].(string)
	if production["name"] != "production" || projectReceipt["related_id"] != productionID {
		t.Fatalf("project_create=%+v", project)
	}
	staging := h.call(t, owner, "environment_create", map[string]any{"request_id": "mcp-staging", "project_id": projectID, "name": "staging"})
	stagingID := staging["environment"].(map[string]any)["id"].(string)

	// A public Git application on the returned environment and managed node.
	suffix := uuid.NewString()[:8]
	marker := "gotham-mcp-v1-" + suffix
	fixture := initFixture(t, "e2e/mcp-app-"+suffix)
	fixture.commit(t, marker, `FROM busybox:1.36
COPY index.html /www/index.html
EXPOSE 8080
CMD ["sh","-c","echo gotham-mcp-runtime; exec httpd -f -p 8080 -h /www"]
`)
	app := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-app", "name": "mcp-app-" + suffix,
		"environment_id": stagingID, "server_id": nodeID.String(), "source_type": "git_public",
		"clone_url": fixture.dir,
	})
	appID := app["application"].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		removeImages(t, "gotham/"+appID)
		removeLabelledContainers(t, "gotham.app_id="+appID)
	})

	// A public image application proves the second source without deploying it.
	image := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-image", "name": "mcp-image-" + suffix,
		"environment_id": productionID, "server_id": nodeID.String(), "source_type": "image",
		"image_ref": "busybox:1.36",
	})
	if image["application"].(map[string]any)["source_type"] != "image" {
		t.Fatalf("image create=%+v", image)
	}
	// A private credential never round-trips, even on the creation response.
	private := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-private-image", "name": "mcp-private-" + suffix,
		"environment_id": productionID, "server_id": nodeID.String(), "source_type": "image",
		"image_ref": "registry.example.com/private:1", "registry_username": "robot",
		"registry_password": "secret-sentinel",
	})
	if strings.Contains(fmt.Sprint(private), "secret-sentinel") {
		t.Fatal("registry password echoed by application_create")
	}

	// Read scope cannot deploy; read-only team membership cannot either.
	if _, err := pool.Exec(ctx, "UPDATE team_members SET role='read_only' WHERE team_id=$1 AND user_id=$2", teamAID, userID); err != nil {
		t.Fatal(err)
	}
	h.expectError(t, owner, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy-role"}, "forbidden")
	if _, err := pool.Exec(ctx, "UPDATE team_members SET role='admin' WHERE team_id=$1 AND user_id=$2", teamAID, userID); err != nil {
		t.Fatal(err)
	}
	h.expectError(t, readOnly, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy-read"}, "forbidden")
	h.expectError(t, foreign, "application_overview", map[string]any{"application_id": appID}, "unsupported_or_not_found")
	h.expectError(t, foreign, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy-foreign"}, "unsupported_or_not_found")

	// Deploy once and poll the exact receipt ID through the read-authorized
	// status route. A 202 is acceptance, never completion.
	accepted := h.call(t, owner, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy"})
	receipt := accepted["receipt"].(map[string]any)
	deploymentID := receipt["resource_id"].(string)
	if accepted["accepted"] != true || receipt["outcome"] == "" {
		t.Fatalf("deploy acceptance=%+v", accepted)
	}
	terminal := h.waitTerminal(t, owner, appID, deploymentID)
	if terminal["state"] != "running" || terminal["terminal"] != true {
		t.Fatalf("terminal deployment=%+v", terminal)
	}
	// Persisted build output is available after the terminal write.
	buildLogs := h.call(t, owner, "deployment_logs", map[string]any{"application_id": appID, "deployment_id": deploymentID, "tail": 100})
	if buildLogs["availability"] != "available" || buildLogs["log"] == "" {
		t.Fatalf("deployment_logs=%+v", buildLogs)
	}
	// Runtime logs come from the live container, not the build row.
	var runtimeLogs map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for {
		runtimeLogs = h.call(t, owner, "application_logs", map[string]any{"application_id": appID, "tail": 50})
		if strings.Contains(fmt.Sprint(runtimeLogs["containers"]), "gotham-mcp-runtime") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime logs missing startup line: %+v", runtimeLogs)
		}
		time.Sleep(500 * time.Millisecond)
	}
	overview := h.call(t, owner, "application_overview", map[string]any{"application_id": appID})
	if overview["application"].(map[string]any)["id"] != appID || len(overview["pipeline"].([]any)) == 0 || overview["runtime"] == nil {
		t.Fatalf("application_overview=%+v", overview)
	}
	// Replay returns the same release and creates no second job.
	replayed := h.call(t, owner, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy"})
	if replayed["receipt"].(map[string]any)["replayed"] != true || replayed["receipt"].(map[string]any)["resource_id"] != deploymentID {
		t.Fatalf("replay=%+v", replayed)
	}
	var jobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM deployments WHERE application_id=$1", appID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("replay created %d jobs (%v)", jobs, err)
	}

	// The failed half: a build that cannot finish stays failed with its
	// persisted output, and the pipeline state is not promoted to runtime.
	broken := newBrokenP4Fixture(t, "e2e/mcp-broken-"+suffix, "P4-BUILD-BOOM")
	t.Cleanup(func() { removeContainersMatchingCommand(t, "P4-BUILD-BOOM") })
	failedApp := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-failed-app", "name": "mcp-failed-" + suffix,
		"environment_id": stagingID, "server_id": nodeID.String(), "source_type": "git_public",
		"clone_url": broken.dir,
	})
	failedID := failedApp["application"].(map[string]any)["id"].(string)
	failed := h.call(t, owner, "application_deploy", map[string]any{"application_id": failedID, "request_id": "mcp-failed-deploy"})
	failedDeployment := failed["receipt"].(map[string]any)["resource_id"].(string)
	failedTerminal := h.waitTerminal(t, owner, failedID, failedDeployment)
	if failedTerminal["state"] != "failed" || failedTerminal["terminal"] != true {
		t.Fatalf("failed deploy=%+v", failedTerminal)
	}
	failedLogs := h.call(t, owner, "deployment_logs", map[string]any{"application_id": failedID, "deployment_id": failedDeployment})
	if !strings.Contains(fmt.Sprint(failedLogs["log"]), "P4-BUILD-BOOM") {
		t.Fatalf("failed build log=%+v", failedLogs)
	}
	failedOverview := h.call(t, owner, "application_overview", map[string]any{"application_id": failedID})
	pipelines := failedOverview["pipeline"].([]any)
	if len(pipelines) == 0 || pipelines[0].(map[string]any)["state"] != "failed" {
		t.Fatalf("failed pipeline state=%+v", failedOverview)
	}
}

// requireIsolatedMCPEnvironment refuses the shared development services. A
// plain `go test ./...` skips through requireE2E; an opt-in run without a
// throwaway database/Redis fails instead of silently using 5432/6379.
func requireIsolatedMCPEnvironment(t *testing.T) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(e2eDSNEnv))
	if dsn == "" {
		t.Fatalf("%s is required: start throwaway Postgres/Redis and export their DSN/addrs", e2eDSNEnv)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", e2eDSNEnv, err)
	}
	if port := parsed.Port(); port == "" || port == "5432" {
		t.Fatalf("%s must point at a throwaway Postgres port, never the shared 5432", e2eDSNEnv)
	}
	redisAddr := strings.TrimSpace(os.Getenv(e2eRedisEnv))
	host, port, err := net.SplitHostPort(redisAddr)
	if err != nil {
		t.Fatalf("%s must be host:port for a throwaway Redis: %v", e2eRedisEnv, err)
	}
	if port == "6379" && (host == "127.0.0.1" || host == "localhost") {
		t.Fatalf("%s must point at a throwaway Redis port, never the shared 6379", e2eRedisEnv)
	}
}

type mcpWorkflowHarness struct {
	baseURL string
	userID  uuid.UUID
	tokens  *auth.APITokenService
	teams   *teams.Service
}

// currentUser is the fixed principal every token in this fixture belongs to.
func (h *mcpWorkflowHarness) currentUser(context.Context) (uuid.UUID, bool) {
	return h.userID, true
}

// authorize mirrors the production ControlPlane chain for the MCP surface:
// bearer token -> method scope (read vs deploy) -> explicit team membership
// -> owner/admin for mutations. The strict contract and Idempotency-Key are
// enforced by the production requestreplay.RequireStrict wrapper per route.
func (h *mcpWorkflowHarness) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if bearer == "" || !strings.HasPrefix(bearer, auth.APITokenPrefix) {
			mcpWriteJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
			return
		}
		identity, err := h.tokens.Authenticate(r.Context(), bearer)
		if err != nil {
			mcpWriteJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		required := auth.ScopeRead
		if mutation {
			required = auth.ScopeDeploy
		}
		if !contains(identity.Scopes, required) {
			mcpWriteJSON(w, http.StatusForbidden, map[string]string{"message": "insufficient scope"})
			return
		}
		teamID, err := uuid.Parse(r.Header.Get("X-Team-Id"))
		if err != nil {
			mcpWriteJSON(w, http.StatusBadRequest, map[string]string{"message": "explicit team required"})
			return
		}
		role, err := h.teams.Membership(r.Context(), teamID, identity.UserID)
		if err != nil {
			mcpWriteJSON(w, http.StatusForbidden, map[string]string{"message": "not a member"})
			return
		}
		if mutation && !role.CanWrite() {
			mcpWriteJSON(w, http.StatusForbidden, map[string]string{"message": "insufficient team role"})
			return
		}
		w.Header().Set(mcpContractHeaderE2E, "1")
		w.Header().Set(mcpTeamHeaderE2E, teamID.String())
		scope := teams.Scope{UserID: identity.UserID, TeamID: teamID, Role: role}
		next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
	})
}

func (h *mcpWorkflowHarness) context(w http.ResponseWriter, r *http.Request) {
	scope, _ := teams.FromContext(r.Context())
	mcpWriteJSON(w, http.StatusOK, map[string]any{
		"team_id":      scope.TeamID,
		"capabilities": []string{"context", "strict_reads", "strict_mutations", "request_replay"},
	})
}

func (h *mcpWorkflowHarness) session(t *testing.T, token string, team uuid.UUID) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	server, err := adapter.NewServer(ctx, adapter.Config{URL: h.baseURL, Token: token, TeamID: team}, "test", testLogger(t))
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

func (h *mcpWorkflowHarness) raw(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport: %v", name, err)
	}
	return result
}

func (h *mcpWorkflowHarness) call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result := h.raw(t, cs, name, args)
	if result.IsError {
		t.Fatalf("%s: %+v", name, result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatalf("%s: missing structured content", name)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "gt1_") {
		t.Fatalf("%s echoed a bearer token", name)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h *mcpWorkflowHarness) expectError(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, code string) {
	t.Helper()
	result := h.raw(t, cs, name, args)
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("%s: error=%v content=%+v", name, result.IsError, result.Content)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || text.Text != code {
		t.Fatalf("%s: code=%+v want %s", name, result.Content, code)
	}
}

func (h *mcpWorkflowHarness) waitTerminal(t *testing.T, cs *sdk.ClientSession, appID, deploymentID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(p4DeployWait)
	last := "not observed"
	for {
		result := h.raw(t, cs, "deployment_status", map[string]any{"application_id": appID, "deployment_id": deploymentID})
		if !result.IsError {
			raw, _ := json.Marshal(result.StructuredContent)
			var out map[string]any
			_ = json.Unmarshal(raw, &out)
			if out["terminal"] == true {
				return out
			}
			last = fmt.Sprint(out["state"])
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment %s did not reach a terminal state (last %q)", deploymentID, last)
		}
		time.Sleep(p4PollInterval)
	}
}

func mcpWriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func mcpNodeJSON(item servers.Server) map[string]any {
	return map[string]any{
		"id": item.ID.String(), "name": item.Name, "status": item.Status,
		"os": item.OS, "arch": item.Arch, "docker_version": item.DockerVersion,
		"total_mem": item.TotalMem, "total_disk": item.TotalDisk,
		"cpu_usage": item.CPUUsage, "mem_usage": item.MemUsage,
		"disk_usage": item.DiskUsage, "container_count": item.ContainerCount,
		"last_seen": item.LastSeen,
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
