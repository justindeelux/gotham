package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/projects"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// pe7Harness is the Phase 13 (PE-7) acceptance fixture: the p4 deploy stack
// (real control plane deploy routes, Postgres, Redis, mTLS node agent on the
// local Docker daemon) plus the projects, services and databases surfaces on
// the same router, so one flow can prove project -> environment -> application
// -> deploy end to end, including the shared-variable merge and the 409
// guards. Like the p4/p7 harnesses it runs only behind requireE2E and fails
// on a missing precondition instead of skipping green.
type pe7Harness struct {
	baseURL  string
	client   *http.Client
	userID   uuid.UUID
	serverID uuid.UUID
	pool     *pgxpool.Pool
	st       *store.Store
}

// newPE7Harness boots the stack. The services surface is wired without an
// agent dialer (create/update-refusal paths never dial) and the databases
// surface with a container service whose dial always fails (the exercised
// paths — update refusal, delete of a never-provisioned row, list — never
// reach it); the only real node work is the application deploy, exactly like
// the p4 suite.
func newPE7Harness(t *testing.T) *pe7Harness {
	t.Helper()
	requireE2E(t)
	removeNewDanglingImages(t)
	t.Setenv(cloneLocalEnv, "true")
	t.Setenv(deploy.FeatureEnv, "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := p4DSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("GOTHAM_E2E=1 requires a reachable, migrated Postgres (run: docker compose -f deploy/compose.dev.yml up -d); check %s", e2eDSNEnv)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("GOTHAM_E2E=1 requires a reachable Postgres (run: docker compose -f deploy/compose.dev.yml up -d); check %s", e2eDSNEnv)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("GOTHAM_E2E=1: docker client for %s: %v", e2eDockerSock(), err)
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = engine.Version(versionCtx)
	versionCancel()
	if err != nil {
		t.Fatalf("GOTHAM_E2E=1 requires a reachable Docker daemon at %s: %v (start Docker and run: docker compose -f deploy/compose.dev.yml up -d)",
			e2eDockerSock(), err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: e2eRedisAddr()})
	t.Cleanup(func() { _ = rdb.Close() })
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	pingErr := rdb.Ping(pingCtx).Err()
	pingCancel()
	if pingErr != nil {
		t.Fatalf("GOTHAM_E2E=1 requires a reachable Redis at %s: %v (run: docker compose -f deploy/compose.dev.yml up -d)",
			e2eRedisAddr(), pingErr)
	}

	nodeID := "pe7-e2e-" + uuid.New().String()[:8]
	agentCtx, agentCancel := context.WithCancel(context.Background())
	t.Cleanup(agentCancel)
	agentAddr, authority := startLocalAgent(t, agentCtx, engine, nodeID)

	user, err := st.CreateUser(ctx, fmt.Sprintf("pe7-e2e-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "pe7-e2e-node",
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server row: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user row: %v", err)
		}
	})

	deploySvc := deploy.NewService(deploy.Config{
		Store:     st,
		Secret:    p4Secret,
		RedisAddr: e2eRedisAddr(),
		Logger:    testLogger(t),
		Dial: deploy.AgentDial(&localDialer{
			serverID:  serverID,
			addr:      agentAddr,
			nodeID:    nodeID,
			authority: authority,
		}),
		Workers: 2,
		Hooks:   func() deploy.HookLifecycle { return nil },
	})
	t.Cleanup(func() { _ = deploySvc.Close() })

	composeSvc := services.NewService(services.Config{Store: st, Logger: testLogger(t)})
	containerSvc := containers.NewService(containers.Config{
		Registry: p6Registry{server: &servers.Server{ID: serverID, IP: "127.0.0.1"}},
		Cache:    containers.NopCache{},
		Dial: func(context.Context, *servers.Server) (containers.DockerClient, error) {
			return nil, errors.New("pe7: no agent dial in this harness")
		},
		Logger: testLogger(t),
	})
	databaseSvc := databases.NewService(databases.Config{
		Store:      st,
		Containers: containerSvc,
		Secret:     p4Secret,
		Logger:     testLogger(t),
	})
	projectSvc := projects.NewService(projects.Config{
		Store:        st,
		Counter:      projects.StoreCounter{Store: st},
		Applications: deploySvc,
		Services:     composeSvc,
		Databases:    databaseSvc,
		Secret:       p4Secret,
		Logger:       testLogger(t),
	})
	serverSvc := servers.NewService(servers.Config{Store: st, Secret: p4Secret, Logger: testLogger(t)})

	userIDFunc := func(context.Context) (uuid.UUID, bool) { return userID, true }
	requireAuth := func(next http.Handler) http.Handler { return next }
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		deploy.Mount(r, requireAuth, userIDFunc, deploySvc)
		projects.Mount(r, requireAuth, userIDFunc, projectSvc)
		services.Mount(r, requireAuth, userIDFunc, composeSvc)
		databases.Mount(r, requireAuth, requireAuth, userIDFunc, databaseSvc)
		// The node registry lives in internal/server (the full HTTP
		// server, too heavy for this harness), so the delete guard is
		// exercised through the real domain service with production's
		// exact status mapping (see handleDeleteServer/writeServerError:
		// ErrConflict -> 409 with the service text).
		r.Delete("/v1/servers/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := uuid.Parse(chi.URLParam(req, "id"))
			if err != nil {
				writePE7JSON(w, http.StatusBadRequest, map[string]string{"message": "invalid server id"})
				return
			}
			if err := serverSvc.Delete(req.Context(), id); err != nil {
				switch {
				case errors.Is(err, servers.ErrNotFound):
					writePE7JSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
				case errors.Is(err, servers.ErrConflict):
					writePE7JSON(w, http.StatusConflict, map[string]string{"message": err.Error()})
				default:
					t.Logf("server delete: %v", err)
					writePE7JSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
				}
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	return &pe7Harness{
		baseURL:  httpServer.URL + "/api",
		client:   &http.Client{Timeout: 30 * time.Second},
		userID:   userID,
		serverID: serverID,
		pool:     pool,
		st:       st,
	}
}

// api performs one API call against the harness control plane.
func (h *pe7Harness) api(t *testing.T, method, path string, body, out any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s body: %v", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, h.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || out == nil {
		return response.StatusCode, string(raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s %s response (%d): %v: %s", method, path, response.StatusCode, err, raw)
	}
	return response.StatusCode, string(raw)
}

// writePE7JSON serialises payload with the given HTTP status.
func writePE7JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// containerEnv returns the environment the container was started with.
func (h *pe7Harness) containerEnv(t *testing.T, containerID string) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := runDocker(ctx, "inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", containerID)
	if err != nil {
		t.Fatalf("docker inspect %s: %v: %s", containerID, err, out)
	}
	env := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		env[key] = value
	}
	return env
}

// TestPE7ProjectsFlow proves the Phase 13 happy path end to end against real
// infrastructure: create project (production comes with it) -> add an
// environment -> create an application on the node -> deploy -> running, with
// the project < environment < application variable merge visible in the
// deployed container; then the move and the 409 guards (environment delete
// with resources, service/database server change, server delete with live
// resources).
func TestPE7ProjectsFlow(t *testing.T) {
	h := newPE7Harness(t)
	suffix := uuid.New().String()[:8]
	ctx := context.Background()

	// 1. The project arrives with its production environment in one tx.
	var createdProject struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
		Environments []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"environments"`
	}
	status, raw := h.api(t, http.MethodPost, "/v1/projects", map[string]string{"name": "pe7-" + suffix}, &createdProject)
	if status != http.StatusCreated {
		t.Fatalf("create project: status %d: %s", status, raw)
	}
	if len(createdProject.Environments) != 1 || createdProject.Environments[0].Name != "production" {
		t.Fatalf("create project returned %d environments, want exactly [production]: %s", len(createdProject.Environments), raw)
	}
	projectID := createdProject.Project.ID
	productionID := createdProject.Environments[0].ID

	// 2. A second environment for the move later.
	var createdEnv struct {
		Environment struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"environment"`
	}
	status, raw = h.api(t, http.MethodPost, "/v1/projects/"+projectID+"/environments", map[string]string{"name": "staging"}, &createdEnv)
	if status != http.StatusCreated {
		t.Fatalf("create environment: status %d: %s", status, raw)
	}
	stagingID := createdEnv.Environment.ID

	// 3. Shared variables at both scopes. Secrets must never come back.
	putVariables := func(path string, variables []map[string]any) {
		t.Helper()
		var out any
		status, raw := h.api(t, http.MethodPut, path, map[string]any{"variables": variables}, &out)
		if status != http.StatusOK {
			t.Fatalf("PUT %s: status %d: %s", path, status, raw)
		}
	}
	putVariables("/v1/projects/"+projectID+"/variables", []map[string]any{
		{"key": "PE7_SHARED", "value": "project", "secret": false},
		{"key": "PE7_PROJ_ONLY", "value": "p", "secret": false},
		{"key": "PE7_WIN", "value": "project", "secret": false},
		{"key": "PE7_PSEC", "value": "project-secret", "secret": true},
	})
	putVariables("/v1/environments/"+productionID+"/variables", []map[string]any{
		{"key": "PE7_SHARED", "value": "environment", "secret": false},
		{"key": "PE7_ENV_ONLY", "value": "e", "secret": false},
		{"key": "PE7_WIN", "value": "environment", "secret": false},
		{"key": "PE7_ESEC", "value": "env-secret", "secret": true},
	})
	for _, path := range []string{"/v1/projects/" + projectID + "/variables", "/v1/environments/" + productionID + "/variables"} {
		status, raw := h.api(t, http.MethodGet, path, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d: %s", path, status, raw)
		}
		if strings.Contains(raw, "project-secret") || strings.Contains(raw, "env-secret") {
			t.Fatalf("GET %s leaks a secret value: %s", path, raw)
		}
	}

	// 4. The application in production, with its own env: it wins every
	// overlap, including over a shared secret (plain over sealed).
	fixture := newP4Fixture(t, "e2e/pe7-app-"+suffix, "gotham-pe7-v1-"+suffix)
	hostPort := freeHostPort(t)
	var createdApp struct {
		Application p4Application `json:"application"`
	}
	status, raw = h.api(t, http.MethodPost, "/v1/applications", p4CreateApplication{
		EnvironmentID: productionID,
		Name:          "pe7-app-" + suffix,
		Provider:      "github",
		Repo:          fixture.repo,
		CloneURL:      fixture.dir,
		Branch:        "main",
		BuildPack:     "dockerfile",
		Port:          p4ContainerPort,
		HostPort:      hostPort,
		ServerID:      h.serverID.String(),
	}, &createdApp)
	if status != http.StatusCreated {
		t.Fatalf("create application: status %d: %s", status, raw)
	}
	appID := createdApp.Application.ID
	t.Cleanup(func() { removeImages(t, "gotham/"+appID) })
	t.Cleanup(func() { removeLabelledContainers(t, "gotham.app_id="+appID) })
	// Resource rows go before the harness server rows (registered earlier,
	// so they run later): on a mid-flow failure the node row's RESTRICT
	// references are gone by the time its delete runs.
	t.Cleanup(func() { h.deleteAppRow(t, appID) })
	status, raw = h.api(t, http.MethodPut, "/v1/applications/"+appID+"/env", map[string]any{"env": []map[string]string{
		{"key": "PE7_WIN", "value": "application"},
		{"key": "PE7_APP_ONLY", "value": "a"},
		{"key": "PE7_PSEC", "value": "plain-wins"},
	}}, nil)
	if status != http.StatusOK {
		t.Fatalf("PUT application env: status %d: %s", status, raw)
	}

	// 5. Deploy and prove it runs.
	queued := h.deployApp(t, appID)
	running := h.waitAppTerminal(t, appID, queued.ID)
	if running.State != "running" {
		t.Fatalf("deployment state = %q, want running (error: %s)", running.State, running.Error)
	}
	if running.ContainerID == "" {
		t.Fatal("running deployment has no container")
	}
	waitForHTTPBody(t, fmt.Sprintf("http://127.0.0.1:%d/index.html", hostPort), fixture.marker)

	// 6. The merged precedence is what the container actually started with:
	// project < environment < application, secrets materialised in plaintext
	// only in the node payload.
	env := h.containerEnv(t, running.ContainerID)
	for key, want := range map[string]string{
		"PE7_SHARED":    "environment",
		"PE7_PROJ_ONLY": "p",
		"PE7_ENV_ONLY":  "e",
		"PE7_WIN":       "application",
		"PE7_APP_ONLY":  "a",
		"PE7_ESEC":      "env-secret",
		"PE7_PSEC":      "plain-wins",
	} {
		if env[key] != want {
			t.Fatalf("container env %s = %q, want %q", key, env[key], want)
		}
	}

	// 7. The resources envelope lists the application in its environment.
	var resources struct {
		Applications []struct {
			ID            string `json:"id"`
			EnvironmentID string `json:"environment_id"`
			ServerID      string `json:"server_id"`
			ServerName    string `json:"server_name"`
		} `json:"applications"`
	}
	status, raw = h.api(t, http.MethodGet, "/v1/environments/"+productionID+"/resources", nil, &resources)
	if status != http.StatusOK {
		t.Fatalf("GET resources: status %d: %s", status, raw)
	}
	if len(resources.Applications) != 1 || resources.Applications[0].ID != appID {
		t.Fatalf("resources envelope lists %d applications, want the one: %s", len(resources.Applications), raw)
	}

	// 8. Move the application to staging.
	var moved struct {
		Application p4Application `json:"application"`
	}
	status, raw = h.api(t, http.MethodPut, "/v1/applications/"+appID, map[string]string{"environment_id": stagingID}, &moved)
	if status != http.StatusOK {
		t.Fatalf("move application: status %d: %s", status, raw)
	}
	var got struct {
		Application struct {
			ID            string `json:"id"`
			EnvironmentID string `json:"environment_id"`
		} `json:"application"`
	}
	status, raw = h.api(t, http.MethodGet, "/v1/applications/"+appID, nil, &got)
	if status != http.StatusOK {
		t.Fatalf("get moved application: status %d: %s", status, raw)
	}
	if got.Application.EnvironmentID != stagingID {
		t.Fatalf("moved application environment = %q, want %q", got.Application.EnvironmentID, stagingID)
	}

	// 9. An environment with resources cannot be deleted; empty it first via
	// the API, then the delete succeeds.
	if status, raw := h.api(t, http.MethodDelete, "/v1/environments/"+stagingID, nil, nil); status != http.StatusConflict {
		t.Fatalf("DELETE non-empty environment: status %d, want 409: %s", status, raw)
	} else if !strings.Contains(raw, "still has resources") {
		t.Fatalf("DELETE non-empty environment body = %q, want the still-has-resources refusal", raw)
	}

	// 10. A deployed service cannot change node: seed one deploy row (the
	// service itself is created through the real API, no agent needed) and
	// move the attempt to a second node.
	secondServer, err := h.st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: "pe7-e2e-node-2", Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create second server: %v", err)
	}
	secondServerID := uuid.UUID(secondServer.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := h.pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", secondServer.ID); err != nil {
			t.Logf("cleanup second server row: %v", err)
		}
	})
	var createdService struct {
		Service struct {
			ID string `json:"id"`
		} `json:"service"`
	}
	status, raw = h.api(t, http.MethodPost, "/v1/services", map[string]any{
		"name": "pe7-svc-" + suffix, "environment_id": stagingID,
		"server_id": h.serverID.String(), "compose_yaml": "services:\n  web:\n    image: nginx:1.23\n",
	}, &createdService)
	if status != http.StatusCreated {
		t.Fatalf("create service: status %d: %s", status, raw)
	}
	serviceID := createdService.Service.ID
	if _, err := h.st.CreateServiceDeploy(ctx, sqlc.CreateServiceDeployParams{
		ID: pgUUID(uuid.New()), ServiceID: pgUUID(uuid.MustParse(serviceID)),
		State: string(services.DeployRunning), ComposeYaml: "services:\n  web:\n    image: nginx:1.23\n",
	}); err != nil {
		t.Fatalf("seed service deploy: %v", err)
	}
	t.Cleanup(func() { h.deleteServiceRows(t, serviceID) })
	if status, raw := h.api(t, http.MethodPatch, "/v1/services/"+serviceID,
		map[string]string{"server_id": secondServerID.String()}, nil); status != http.StatusConflict {
		t.Fatalf("PATCH service server: status %d, want 409: %s", status, raw)
	} else if !strings.Contains(raw, "a deployed service cannot change server") {
		t.Fatalf("PATCH service server body = %q, want the pinned refusal", raw)
	}

	// 11. A database cannot change node once created: seed the row (status
	// creating, no container — provisioning never ran) and try the move.
	dbRow, err := h.st.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID: pgUUID(uuid.New()), UserID: pgUUID(h.userID), ServerID: pgUUID(h.serverID),
		EnvironmentID: pgUUID(uuid.MustParse(stagingID)), Name: "pe7-db-" + suffix,
		Engine: "postgres", Status: "creating", StoragePath: "gotham-db-pe7-" + suffix,
		TeamID: pgUUID(h.userID),
	})
	if err != nil {
		t.Fatalf("seed database: %v", err)
	}
	dbID := uuid.UUID(dbRow.ID.Bytes).String()
	t.Cleanup(func() { h.deleteDatabaseRow(t, dbID) })
	if status, raw := h.api(t, http.MethodPatch, "/v1/databases/"+dbID,
		map[string]string{"server_id": secondServerID.String()}, nil); status != http.StatusConflict {
		t.Fatalf("PATCH database server: status %d, want 409: %s", status, raw)
	} else if !strings.Contains(raw, "a database cannot change server once created") {
		t.Fatalf("PATCH database server body = %q, want the pinned refusal", raw)
	}

	// 12. The node still holds the application, the service and the database:
	// deleting it is refused with the blocking list.
	if status, raw := h.api(t, http.MethodDelete, "/v1/servers/"+h.serverID.String(), nil, nil); status != http.StatusConflict {
		t.Fatalf("DELETE live server: status %d, want 409: %s", status, raw)
	} else if !strings.Contains(raw, "server still has resources") {
		t.Fatalf("DELETE live server body = %q, want the still-has-resources refusal", raw)
	}

	// 13. Tear down through the API: the application (its container goes
	// with it), the never-provisioned database (no container to stop), then
	// the seeded service rows at the store (never deployed, so no node
	// state). The emptied environment deletes; the last one is refused.
	if status, raw := h.api(t, http.MethodDelete, "/v1/applications/"+appID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("DELETE application: status %d: %s", status, raw)
	}
	if status, raw := h.api(t, http.MethodDelete, "/v1/databases/"+dbID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("DELETE database: status %d: %s", status, raw)
	}
	if _, err := h.st.SoftDeleteService(ctx, pgUUID(uuid.MustParse(serviceID))); err != nil {
		t.Fatalf("soft-delete service: %v", err)
	}
	if status, raw := h.api(t, http.MethodDelete, "/v1/environments/"+stagingID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("DELETE emptied environment: status %d: %s", status, raw)
	}
	if status, raw := h.api(t, http.MethodDelete, "/v1/environments/"+productionID, nil, nil); status != http.StatusConflict {
		t.Fatalf("DELETE last environment: status %d, want 409: %s", status, raw)
	} else if !strings.Contains(raw, "at least one environment") {
		t.Fatalf("DELETE last environment body = %q, want the last-environment refusal", raw)
	}
	if status, raw := h.api(t, http.MethodDelete, "/v1/projects/"+projectID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("DELETE emptied project: status %d: %s", status, raw)
	}

	t.Logf("pe7 flow ok: project=%s app=%s service=%s db=%s", projectID, appID, serviceID, dbID)
}

// deleteAppRow removes the application through the API (its container goes
// with it), falling back to a row delete when the API path already failed.
// Best-effort: it runs as a cleanup, so it never fails the test.
func (h *pe7Harness) deleteAppRow(t *testing.T, appID string) {
	t.Helper()
	if status, _ := h.api(t, http.MethodDelete, "/v1/applications/"+appID, nil, nil); status == http.StatusNoContent || status == http.StatusNotFound {
		return
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	if _, err := h.pool.Exec(cleanupCtx, "DELETE FROM applications WHERE id = $1", pgUUID(uuid.MustParse(appID))); err != nil {
		t.Logf("cleanup application row: %v", err)
	}
}

// deleteServiceRows hard-deletes the seeded service and its deploy rows. The
// service never deployed, so no node state exists; a hard delete (not the
// soft delete) also drops the server_id RESTRICT reference.
func (h *pe7Harness) deleteServiceRows(t *testing.T, serviceID string) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	id := pgUUID(uuid.MustParse(serviceID))
	if _, err := h.pool.Exec(cleanupCtx, "DELETE FROM service_deploys WHERE service_id = $1", id); err != nil {
		t.Logf("cleanup service deploy rows: %v", err)
	}
	if _, err := h.pool.Exec(cleanupCtx, "DELETE FROM services WHERE id = $1", id); err != nil {
		t.Logf("cleanup service row: %v", err)
	}
}

// deleteDatabaseRow hard-deletes the seeded database row. It never
// provisioned, so no container or volume exists; a hard delete drops the
// server_id RESTRICT reference the soft delete would keep.
func (h *pe7Harness) deleteDatabaseRow(t *testing.T, dbID string) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	if _, err := h.pool.Exec(cleanupCtx, "DELETE FROM databases WHERE id = $1", pgUUID(uuid.MustParse(dbID))); err != nil {
		t.Logf("cleanup database row: %v", err)
	}
}

// deployApp queues a deployment of the current revision (202).
func (h *pe7Harness) deployApp(t *testing.T, appID string) p4Deployment {
	t.Helper()
	var envelope p4DeploymentEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications/"+appID+"/deploy", nil, &envelope)
	if status != http.StatusAccepted {
		t.Fatalf("deploy: status %d: %s", status, raw)
	}
	return envelope.Deployment
}

// waitAppTerminal polls until the named deployment reaches running or failed.
func (h *pe7Harness) waitAppTerminal(t *testing.T, appID, deploymentID string) p4Deployment {
	t.Helper()
	deadline := time.Now().Add(p4DeployWait)
	last := "not listed"
	for {
		var list p4DeploymentList
		status, raw := h.api(t, http.MethodGet, "/v1/applications/"+appID+"/deployments", nil, &list)
		if status != http.StatusOK {
			t.Fatalf("list deployments: status %d: %s", status, raw)
		}
		for _, deployment := range list.Deployments {
			if deployment.ID != deploymentID {
				continue
			}
			if deployment.State == "running" || deployment.State == "failed" {
				return deployment
			}
			last = deployment.State
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment %s never reached a terminal state; last observed %q", deploymentID, last)
		}
		time.Sleep(p4PollInterval)
	}
}
