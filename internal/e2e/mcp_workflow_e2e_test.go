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
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	adapter "github.com/justindeelux/gotham/internal/mcp"
	"github.com/justindeelux/gotham/internal/projects"
	controlplane "github.com/justindeelux/gotham/internal/server"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpContractHeaderE2E = "X-Gotham-MCP-Contract"
	mcpWorkflowSecret    = "gotham-mcp-workflow-secret-key"
)

func TestMCPAgentDeploymentWorkflow(t *testing.T) {
	requireE2E(t)
	requireIsolatedMCPEnvironment(t)
	t.Setenv(cloneLocalEnv, "true")
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

	user, err := st.CreateUser(ctx, fmt.Sprintf("mcp-e2e-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	node, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "mcp-e2e-node", Ip: "127.0.0.1", Port: 22, SshUser: "root"})
	if err != nil {
		t.Fatal(err)
	}
	nodeID := uuid.UUID(node.ID.Bytes)
	if _, err := pool.Exec(ctx, "UPDATE servers SET status='ready', last_seen=now() WHERE id=$1", node.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "mcp-legacy-node", Ip: "127.0.0.1", Port: 22, SshUser: "root"})
	if err != nil {
		t.Fatal(err)
	}
	legacyID := uuid.UUID(legacy.ID.Bytes)
	if _, err := pool.Exec(ctx, "UPDATE servers SET status='ready', last_seen=now() WHERE id=$1", legacyID); err != nil {
		t.Fatal(err)
	}
	var workflowApps []string
	suffix := uuid.NewString()[:8]
	var projectID string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM applications WHERE user_id=$1", userID); err != nil {
			t.Logf("cleanup applications: %v", err)
		}
		if projectID != "" {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM projects WHERE id=$1", projectID); err != nil {
				t.Logf("cleanup project: %v", err)
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id=$1 OR id=$2", nodeID, legacyID); err != nil {
			t.Logf("cleanup servers: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM request_receipts WHERE principal_id=$1", userID); err != nil {
			t.Logf("cleanup receipts: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id=$1", userID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
		assertMCPWorkflowClean(t, cleanupCtx, pool, userID, projectID, nodeID, legacyID, workflowApps)
	})

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

	h := &mcpWorkflowHarness{userID: userID, tokens: tokens}
	var remoteMCP http.Handler
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
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		router.Method(method, "/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			remoteMCP.ServeHTTP(w, r)
		}))
	}
	api := httptest.NewServer(router)
	t.Cleanup(api.Close)
	h.baseURL = api.URL
	remoteMCP = controlplane.NewMCPHandler(controlplane.MCPHandlerConfig{
		Tokens:  tokens,
		BaseURL: func(*http.Request) string { return api.URL },
		Version: "test",
		Logger:  logger,
	})

	owner := h.session(t, deployToken.Token)
	readOnly := h.session(t, readToken.Token)
	remoteOwner := h.remoteSession(t, api.URL, deployToken.Token)
	remoteRead := h.remoteSession(t, api.URL, readToken.Token)
	if _, err := adapter.NewServer(ctx, adapter.Config{URL: api.URL, Token: revoked.Token}, "test", logger); err == nil {
		t.Fatal("revoked token initialized the adapter")
	}

	remoteList, err := remoteOwner.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("remote tools/list: %v", err)
	}
	if len(remoteList.Tools) != 18 {
		t.Fatalf("remote tools=%d want 18", len(remoteList.Tools))
	}
	remoteReadList, err := remoteRead.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("remote read tools/list: %v", err)
	}
	if len(remoteReadList.Tools) != 14 {
		t.Fatalf("remote read tools=%d want 14", len(remoteReadList.Tools))
	}
	if _, err := remoteRead.CallTool(ctx, &sdk.CallToolParams{Name: "application_deploy", Arguments: map[string]any{}}); err == nil {
		t.Fatal("remote read token reached a mutation tool")
	}
	remoteNodes := h.call(t, remoteOwner, "nodes_list", map[string]any{"limit": 10})
	if remoteNodes["total"] != float64(2) {
		t.Fatalf("remote nodes_list=%+v", remoteNodes)
	}

	nodes := h.call(t, owner, "nodes_list", map[string]any{"limit": 10})
	if nodes["total"] != float64(2) {
		t.Fatalf("nodes_list=%+v", nodes)
	}
	ownerNodes := nodeIDSet(nodes)
	if !ownerNodes[nodeID.String()] || !ownerNodes[legacyID.String()] {
		t.Fatalf("nodes=%v want %s and %s", ownerNodes, nodeID, legacyID)
	}
	nodeOut := h.call(t, owner, "node_overview", map[string]any{"node_id": nodeID.String()})
	if nodeOut["node"].(map[string]any)["status"] != "ready" {
		t.Fatalf("node_overview=%+v", nodeOut)
	}
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

	project := h.call(t, owner, "project_create", map[string]any{"request_id": "mcp-project", "name": "mcp-shop-" + suffix})
	projectReceipt := project["receipt"].(map[string]any)
	projectID = projectReceipt["resource_id"].(string)
	production := project["environments"].([]any)[0].(map[string]any)
	productionID := production["id"].(string)
	if production["name"] != "production" || projectReceipt["related_id"] != productionID {
		t.Fatalf("project_create=%+v", project)
	}
	staging := h.call(t, owner, "environment_create", map[string]any{"request_id": "mcp-staging", "project_id": projectID, "name": "staging"})
	stagingID := staging["environment"].(map[string]any)["id"].(string)
	remoteProjects := h.call(t, remoteOwner, "projects_list", map[string]any{})
	seenProject := false
	for _, item := range remoteProjects["items"].([]any) {
		if item.(map[string]any)["id"] == projectID {
			seenProject = true
		}
	}
	if !seenProject {
		t.Fatalf("remote projects_list missing %s: %+v", projectID, remoteProjects)
	}

	marker := "gotham-mcp-v1-" + suffix
	fixture := initFixture(t, "e2e/mcp-app-"+suffix)
	fixture.commit(t, marker, `FROM busybox:1.36
COPY index.html /www/index.html
EXPOSE 8080
CMD ["sh","-c","echo gotham-mcp-runtime; echo runtime-secret:${MCP_ECHO_SECRET}; exec httpd -f -p 8080 -h /www"]
`)
	app := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-app", "name": "mcp-app-" + suffix,
		"environment_id": stagingID, "server_id": nodeID.String(), "source_type": "git_public",
		"clone_url": fixture.dir,
	})
	appID := app["application"].(map[string]any)["id"].(string)
	workflowApps = append(workflowApps, appID)
	t.Cleanup(func() {
		removeImages(t, "gotham/"+appID)
		removeLabelledContainers(t, "gotham.app_id="+appID)
	})
	h.putEnv(t, deployToken.Token, appID, "MCP_ECHO_SECRET", "secret-echo-sentinel")

	image := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-image", "name": "mcp-image-" + suffix,
		"environment_id": productionID, "server_id": nodeID.String(), "source_type": "image",
		"image_ref": "busybox:1.36",
	})
	if image["application"].(map[string]any)["source_type"] != "image" {
		t.Fatalf("image create=%+v", image)
	}
	private := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-private-image", "name": "mcp-private-" + suffix,
		"environment_id": productionID, "server_id": nodeID.String(), "source_type": "image",
		"image_ref": "registry.example.com/private:1", "registry_username": "robot",
		"registry_password": "secret-sentinel",
	})
	if strings.Contains(fmt.Sprint(private), "secret-sentinel") {
		t.Fatal("registry password echoed by application_create")
	}
	workflowApps = append(workflowApps, image["application"].(map[string]any)["id"].(string), private["application"].(map[string]any)["id"].(string))

	h.expectError(t, readOnly, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy-read"}, "forbidden")

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
	buildLogs := h.call(t, owner, "deployment_logs", map[string]any{"application_id": appID, "deployment_id": deploymentID, "tail": 100})
	if buildLogs["availability"] != "available" || buildLogs["log"] == "" {
		t.Fatalf("deployment_logs=%+v", buildLogs)
	}
	var runtimeLogs map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for {
		runtimeLogs = h.call(t, owner, "application_logs", map[string]any{"application_id": appID, "tail": 50})
		logText := fmt.Sprint(runtimeLogs["containers"])
		if strings.Contains(logText, "gotham-mcp-runtime") && strings.Contains(logText, "runtime-secret:<redacted>") {
			if strings.Contains(logText, "secret-echo-sentinel") {
				t.Fatalf("runtime log echoed a configured env value: %s", logText)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime logs missing the masked startup line: %+v", runtimeLogs)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if strings.Contains(fmt.Sprint(buildLogs), "secret-echo-sentinel") {
		t.Fatal("build log echoed a configured env value")
	}
	overview := h.call(t, owner, "application_overview", map[string]any{"application_id": appID})
	if overview["application"].(map[string]any)["id"] != appID || len(overview["pipeline"].([]any)) == 0 || overview["runtime"] == nil {
		t.Fatalf("application_overview=%+v", overview)
	}
	replayed := h.call(t, owner, "application_deploy", map[string]any{"application_id": appID, "request_id": "mcp-deploy"})
	if replayed["receipt"].(map[string]any)["replayed"] != true || replayed["receipt"].(map[string]any)["resource_id"] != deploymentID {
		t.Fatalf("replay=%+v", replayed)
	}
	var jobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM deployments WHERE application_id=$1", appID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("replay created %d jobs (%v)", jobs, err)
	}

	broken := newBrokenP4Fixture(t, "e2e/mcp-broken-"+suffix, "P4-BUILD-BOOM")
	t.Cleanup(func() { removeContainersMatchingCommand(t, "P4-BUILD-BOOM") })
	failedApp := h.call(t, owner, "application_create", map[string]any{
		"request_id": "mcp-failed-app", "name": "mcp-failed-" + suffix,
		"environment_id": stagingID, "server_id": nodeID.String(), "source_type": "git_public",
		"clone_url": broken.dir,
	})
	failedID := failedApp["application"].(map[string]any)["id"].(string)
	workflowApps = append(workflowApps, failedID)
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
	failedReplay := h.call(t, owner, "application_deploy", map[string]any{"application_id": failedID, "request_id": "mcp-failed-deploy"})
	failedReceipt := failedReplay["receipt"].(map[string]any)
	if failedReceipt["replayed"] != true || failedReceipt["resource_id"] != failedDeployment {
		t.Fatalf("failed replay=%+v", failedReplay)
	}
	var failedJobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM deployments WHERE application_id=$1", failedID).Scan(&failedJobs); err != nil || failedJobs != 1 {
		t.Fatalf("failed replay created %d jobs (%v)", failedJobs, err)
	}
	failedOverview := h.call(t, owner, "application_overview", map[string]any{"application_id": failedID})
	pipelines := failedOverview["pipeline"].([]any)
	if len(pipelines) == 0 || pipelines[0].(map[string]any)["state"] != "failed" {
		t.Fatalf("failed pipeline state=%+v", failedOverview)
	}
}

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
}

func (h *mcpWorkflowHarness) currentUser(context.Context) (uuid.UUID, bool) {
	return h.userID, true
}

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
		w.Header().Set(mcpContractHeaderE2E, "1")
		next.ServeHTTP(w, r)
	})
}

func (h *mcpWorkflowHarness) context(w http.ResponseWriter, _ *http.Request) {
	mcpWriteJSON(w, http.StatusOK, map[string]any{
		"capabilities": []string{"context", "strict_reads", "strict_mutations", "request_replay"},
	})
}

func (h *mcpWorkflowHarness) session(t *testing.T, token string) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	server, err := adapter.NewServer(ctx, adapter.Config{URL: h.baseURL, Token: token}, "test", testLogger(t))
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

func (h *mcpWorkflowHarness) remoteSession(t *testing.T, baseURL, token string) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "remote-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             baseURL + "/mcp",
		HTTPClient:           &http.Client{Transport: mcpBearerTransport{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("remote connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type mcpBearerTransport struct {
	token string
}

func (t mcpBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
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

func nodeIDSet(out map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, item := range out["nodes"].([]any) {
		ids[item.(map[string]any)["id"].(string)] = true
	}
	return ids
}

func (h *mcpWorkflowHarness) putEnv(t *testing.T, token, appID, key, value string) {
	t.Helper()
	body := fmt.Sprintf(`{"env":[{"key":%q,"value":%q}]}`, key, value)
	request, err := http.NewRequest(http.MethodPut, h.baseURL+"/api/v1/applications/"+appID+"/env", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		var raw json.RawMessage
		_ = json.NewDecoder(response.Body).Decode(&raw)
		t.Fatalf("put env: status=%d body=%s", response.StatusCode, raw)
	}
}

func assertMCPWorkflowClean(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, projectID string, nodeID, legacyID uuid.UUID, appIDs []string) {
	t.Helper()
	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{"applications", "SELECT count(*) FROM applications WHERE user_id=$1", []any{userID}},
		{"projects", "SELECT count(*) FROM projects WHERE id=$1", []any{projectID}},
		{"servers", "SELECT count(*) FROM servers WHERE id IN ($1,$2)", []any{nodeID, legacyID}},
		{"users", "SELECT count(*) FROM users WHERE id=$1", []any{userID}},
		{"request_receipts", "SELECT count(*) FROM request_receipts WHERE principal_id=$1", []any{userID}},
	}
	for _, check := range checks {
		var count int
		if err := pool.QueryRow(ctx, check.query, check.args...).Scan(&count); err != nil {
			t.Errorf("cleanup check %s: %v", check.name, err)
			continue
		}
		if count != 0 {
			t.Errorf("cleanup leaked %d %s row(s)", count, check.name)
		}
	}
	for _, appID := range appIDs {
		if output, err := exec.Command("docker", "ps", "-aq", "--filter", "label=gotham.app_id="+appID).Output(); err == nil && strings.TrimSpace(string(output)) != "" {
			t.Errorf("cleanup left containers for application %s: %s", appID, strings.TrimSpace(string(output)))
		}
		if output, err := exec.Command("docker", "images", "-q", "gotham/"+appID).Output(); err == nil && strings.TrimSpace(string(output)) != "" {
			t.Errorf("cleanup left image gotham/%s", appID)
		}
	}
}
