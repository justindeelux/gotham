package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/webhooks"
)

// Phase 4 (QA-4.1) environment knobs. Only GOTHAM_E2E is required: the DSN
// defaults to the dev database of deploy/compose.dev.yml, Redis and the Docker
// socket to their usual local addresses. The harness runs only behind the
// requireE2E gate, so GOTHAM_E2E=1 is already an explicit opt-in: a missing
// Postgres, Redis or Docker fails the run with a clear message instead of
// skipping it green. A plain `go test ./...` (and a box without the three)
// stays green because the gate itself skips when the feature is off.
const (
	e2eDSNEnv      = "GOTHAM_TEST_DSN"
	defaultE2EDSN  = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	cloneLocalEnv  = "GOTHAM_DEV_CLONE_LOCAL"
	p4Secret       = "gotham-e2e-secret-key"
	p4DeployWait   = 6 * time.Minute
	p4LogWait      = 30 * time.Second
	p4PollInterval = 500 * time.Millisecond
)

// p4Harness is one control plane under test: the production HTTP surface
// (application deploy routes plus the public webhook delivery route) wired to
// a real PostgreSQL, a real Redis and a real node agent serving DockerService
// and BuildService over mTLS on the local Docker daemon. Nothing is faked
// except the Git host — provider APIs are unreachable in CI, so hooks are
// seeded as rows and deliveries are replayed by hand.
type p4Harness struct {
	baseURL  string
	client   *http.Client
	userID   uuid.UUID
	serverID uuid.UUID
	envID    uuid.UUID
	secret   string
	st       *store.Store
	logs     *p4LogBuffer
}

// p4CreateApplication is the POST /v1/applications payload.
type p4CreateApplication struct {
	EnvironmentID string `json:"environment_id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Repo          string `json:"repo"`
	CloneURL      string `json:"clone_url"`
	SourceType    string `json:"source_type"`
	// ComposeContent, ComposeFile and ComposeService carry the compose
	// source (GS-8); every other source leaves them empty.
	ComposeContent string `json:"compose_content"`
	ComposeFile    string `json:"compose_file"`
	ComposeService string `json:"compose_service"`
	Branch         string `json:"branch"`
	BuildPack      string `json:"build_pack"`
	BaseDomain     string `json:"base_domain"`
	Port           int32  `json:"port"`
	HostPort       int32  `json:"host_port"`
	ServerID       string `json:"server_id"`
}

// p4Application is the application half of the API wire format.
type p4Application struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Repo       string `json:"repo"`
	CloneURL   string `json:"clone_url"`
	Branch     string `json:"branch"`
	BuildPack  string `json:"build_pack"`
	BaseDomain string `json:"base_domain"`
	Port       int32  `json:"port"`
	HostPort   int32  `json:"host_port"`
	ServerID   string `json:"server_id"`
}

// p4ApplicationEnvelope wraps a single application.
type p4ApplicationEnvelope struct {
	Application p4Application `json:"application"`
}

// p4Deployment is the deployment half of the API wire format: state, image
// reference and the actionable error text of a failed run.
type p4Deployment struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	ImageTag      string `json:"image_tag"`
	RegistryImage string `json:"registry_image"`
	Error         string `json:"error"`
	Attempt       int32  `json:"attempt"`
	ContainerID   string `json:"container_id"`
	RollbackFrom  string `json:"rollback_from"`
}

// p4DeploymentEnvelope wraps a single deployment.
type p4DeploymentEnvelope struct {
	Deployment p4Deployment `json:"deployment"`
}

// p4DeploymentList wraps a deployment list (newest first).
type p4DeploymentList struct {
	Deployments []p4Deployment `json:"deployments"`
}

// p4LogLine is one deploy log event read back from Redis.
type p4LogLine struct {
	Channel string
	Data    string
}

// p4LogBuffer records every deploy log line published during a test. The
// subscription is opened when the harness starts — before any deployment
// exists — because Redis pub/sub drops messages published before a subscriber
// attaches; a per-deployment subscription would race the worker pool.
type p4LogBuffer struct {
	mu    sync.Mutex
	lines []p4LogLine
}

// add appends one line.
func (b *p4LogBuffer) add(channel, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, p4LogLine{Channel: channel, Data: data})
}

// forChannel returns every line published on channel, oldest first.
func (b *p4LogBuffer) forChannel(channel string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out strings.Builder
	for _, line := range b.lines {
		if line.Channel == channel {
			out.WriteString(line.Data)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// localDialer resolves the single node under test. It hands the deploy service
// the production mTLS dial instead of a shortcut, so a deployment reaches the
// agent exactly as it does in production.
type localDialer struct {
	serverID  uuid.UUID
	addr      string
	nodeID    string
	authority *servers.Authority
}

// DialDockerClient implements deploy.AgentDialer.
func (d *localDialer) DialDockerClient(ctx context.Context, id uuid.UUID,
	opts ...servers.DockerDialOption) (*servers.DockerClient, error) {
	if id != d.serverID {
		return nil, servers.ErrNotFound
	}
	options := append([]servers.DockerDialOption{servers.WithDockerServerName(d.nodeID)}, opts...)
	return servers.DialDockerClient(ctx, d.addr, d.authority, options...)
}

// stubInstaller stands in for the Git host in hook management. The suite
// never installs a hook through the provider API (no network, no token): it
// seeds the row instead and replays signed deliveries.
type stubInstaller struct{}

// CreateWebhook reports a hook id without touching a provider.
func (stubInstaller) CreateWebhook(context.Context, providers.HookTarget, providers.Webhook) (string, error) {
	return "p4-e2e-hook", nil
}

// DeleteWebhook is a no-op success.
func (stubInstaller) DeleteWebhook(context.Context, providers.HookTarget, string) error { return nil }

// newP4Harness boots the whole stack for one Phase 4 test: PostgreSQL (migrated),
// Docker, Redis, the local agent, the deploy and webhook services and the HTTP
// surface they are mounted on. The caller is already behind requireE2E, so a
// missing precondition fails the test instead of skipping it green.
func newP4Harness(t *testing.T) *p4Harness {
	t.Helper()
	return newP4HarnessWithAgentOptions(t)
}

// newP4HarnessWithAgentOptions is newP4Harness with extra agent services,
// such as the compose service a compose-application test needs. Existing
// suites keep the DockerService + BuildService agent unchanged.
func newP4HarnessWithAgentOptions(t *testing.T, options ...agent.ServerOption) *p4Harness {
	t.Helper()
	requireE2E(t)
	// Register the dangling-image cleanup first: t.Cleanup is LIFO, so it runs
	// after every per-application cleanup has dropped its build tags and the
	// legacy builder's intermediate layers are visible as untagged images.
	removeNewDanglingImages(t)
	// The fixture repository is a directory on this machine; production keeps
	// local clone sources disabled (see deploy.devLocalClone).
	t.Setenv(cloneLocalEnv, "true")
	// The applications surface must be on, whatever the ambient environment says.
	t.Setenv(deploy.FeatureEnv, "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. PostgreSQL: the suite proves the real schema — applications,
	// deployments and the webhook dedupe ledger. GOTHAM_E2E=1 is an explicit
	// opt-in, so an unreachable database fails. The diagnostic is deliberately
	// generic: pgx/goose errors can embed the connection string, credentials
	// included.
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

	// 2. Docker: the agent builds and runs on the local daemon.
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

	// 3. Redis: realtime deploy logs fan out through it.
	rdb := redis.NewClient(&redis.Options{Addr: e2eRedisAddr()})
	t.Cleanup(func() { _ = rdb.Close() })
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	pingErr := rdb.Ping(pingCtx).Err()
	pingCancel()
	if pingErr != nil {
		t.Fatalf("GOTHAM_E2E=1 requires a reachable Redis at %s: %v (run: docker compose -f deploy/compose.dev.yml up -d)",
			e2eRedisAddr(), pingErr)
	}

	// 4. The node: one agent serving DockerService and BuildService over mTLS,
	// plus any extra services the caller registered (ComposeService for the
	// compose-application suite). The agent outlives the setup context above
	// — its lifetime is the test's.
	nodeID := "p4-e2e-" + uuid.New().String()[:8]
	agentCtx, agentCancel := context.WithCancel(context.Background())
	t.Cleanup(agentCancel)
	agentAddr, authority := startLocalAgentWithOptions(t, agentCtx, engine, nodeID, options...)

	// 5. Rows: the caller, and the node the application will be assigned to.
	user, err := st.CreateUser(ctx, fmt.Sprintf("p4-e2e-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p4-e2e-node",
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	// The harness mounts no team middleware, so requests run creator-scoped
	// and the personal team (ID = user ID, created with the account,
	// owner by construction) scopes the environment validation.
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TeamID: pgtype.UUID{Bytes: userID, Valid: true}, Name: "p4-e2e",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, ProjectID: project.ID, Name: "production",
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	envID := uuid.UUID(environment.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		// Deleting the user cascades to its applications, deployments, hooks
		// and delivery ledger; the node row has no owner and goes separately.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server row: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user row: %v", err)
		}
	})

	// 6. The domain services, wired the way internal/server wires them: store,
	// sealing key, realtime publisher and the mTLS agent dialer. hookSvc is
	// declared first because the deploy service resolves it lazily — the
	// production wiring builds the webhook service after the deploy service
	// (it consumes it as its deployer), and the closure below mirrors that.
	// This makes every application created through the API install its hook
	// through the real lifecycle, not just the explicit webhook route.
	var hookSvc *webhooks.Service
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
		Hooks:   func() deploy.HookLifecycle { return hookSvc },
	})
	t.Cleanup(func() { _ = deploySvc.Close() })

	// The preview surface (BE-8.1) is wired exactly like production: the
	// deploy service provisions and tears down the sibling applications, and
	// the provider service would post the badge comment (absent here: the
	// suite never talks to a Git host). The flag is forced on so an ambient
	// FEATURE_PREVIEWS=false cannot disable the surface under test.
	t.Setenv(webhooks.FeatureEnv, "true")
	hookSvc = webhooks.NewService(webhooks.Config{
		Store:       st,
		Installer:   stubInstaller{},
		Deployer:    deploySvc,
		Provisioner: deploySvc,
		Secret:      p4Secret,
		Logger:      testLogger(t),
	})

	// 7. The HTTP surface: authenticated application routes and the public,
	// signature-verified delivery route under /api, as in production.
	userIDFunc := func(context.Context) (uuid.UUID, bool) { return userID, true }
	requireAuth := func(next http.Handler) http.Handler { return next }
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		deploy.Mount(r, requireAuth, userIDFunc, deploySvc)
		webhooks.Mount(r, requireAuth, userIDFunc, hookSvc)
	})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	// 8. One pattern subscription for every logs:{server}:{deployment} channel,
	// opened before the first deployment so no line can be missed. It lives on
	// its own context and is closed by the cleanup below.
	pubsub := rdb.PSubscribe(context.Background(), "logs:*:*")
	receiveCtx, receiveCancel := context.WithTimeout(ctx, 10*time.Second)
	_, receiveErr := pubsub.Receive(receiveCtx)
	receiveCancel()
	if receiveErr != nil {
		t.Fatalf("subscribe to deploy log channels: %v", receiveErr)
	}
	t.Cleanup(func() { _ = pubsub.Close() })
	buffer := &p4LogBuffer{}
	go func() {
		for message := range pubsub.Channel() {
			var event deploy.Event
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				buffer.add(message.Channel, message.Payload)
				continue
			}
			buffer.add(message.Channel, event.Data)
		}
	}()

	return &p4Harness{
		baseURL:  httpServer.URL + "/api",
		client:   &http.Client{Timeout: 30 * time.Second},
		userID:   userID,
		serverID: serverID,
		envID:    envID,
		secret:   p4Secret,
		st:       st,
		logs:     buffer,
	}
}

// p4DSN returns the DSN the Phase 4 suite migrates and tests against.
func p4DSN() string {
	if dsn := strings.TrimSpace(os.Getenv(e2eDSNEnv)); dsn != "" {
		return dsn
	}
	return defaultE2EDSN
}

// api performs one API call against the harness control plane. body is sent as
// JSON when non-nil; out is decoded only for a 2xx response, so error payloads
// come back as raw text for the failure message.
func (h *p4Harness) api(t *testing.T, method, path string, body, out any) (int, string) {
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

// createApplication creates an application through the API (201).
func (h *p4Harness) createApplication(t *testing.T, in p4CreateApplication) p4Application {
	t.Helper()
	var envelope p4ApplicationEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications", in, &envelope)
	if status != http.StatusCreated {
		t.Fatalf("create application: status %d: %s", status, raw)
	}
	// The containers this application starts are removed when the test ends.
	h.removeAppArtifacts(t, envelope.Application.ID)
	return envelope.Application
}

// deploy queues a deployment of the current revision (202).
func (h *p4Harness) deploy(t *testing.T, appID string) p4Deployment {
	t.Helper()
	var envelope p4DeploymentEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications/"+appID+"/deploy", nil, &envelope)
	if status != http.StatusAccepted {
		t.Fatalf("deploy: status %d: %s", status, raw)
	}
	return envelope.Deployment
}

// rollback queues a redeployment of a previous release (202). A nil body asks
// for the previous successful release.
func (h *p4Harness) rollback(t *testing.T, appID string, body any) p4Deployment {
	t.Helper()
	var envelope p4DeploymentEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications/"+appID+"/rollback", body, &envelope)
	if status != http.StatusAccepted {
		t.Fatalf("rollback: status %d: %s", status, raw)
	}
	return envelope.Deployment
}

// deployments lists the application's deployments, newest first.
func (h *p4Harness) deployments(t *testing.T, appID string) []p4Deployment {
	t.Helper()
	var list p4DeploymentList
	status, raw := h.api(t, http.MethodGet, "/v1/applications/"+appID+"/deployments", nil, &list)
	if status != http.StatusOK {
		t.Fatalf("list deployments: status %d: %s", status, raw)
	}
	return list.Deployments
}

// waitForTerminal polls until the named deployment reaches running or failed.
func (h *p4Harness) waitForTerminal(t *testing.T, appID, deploymentID string) p4Deployment {
	t.Helper()
	deadline := time.Now().Add(p4DeployWait)
	last := "not listed"
	for {
		for _, deployment := range h.deployments(t, appID) {
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

// deployLogs returns every log line published for a deployment so far.
func (h *p4Harness) deployLogs(deploymentID string) string {
	channel := deploy.DeployChannel(h.serverID, uuid.MustParse(deploymentID))
	return h.logs.forChannel(channel)
}

// waitForDeployLog waits until the deployment's realtime log carries want.
func (h *p4Harness) waitForDeployLog(t *testing.T, deploymentID, want string) {
	t.Helper()
	deadline := time.Now().Add(p4LogWait)
	for time.Now().Before(deadline) {
		if strings.Contains(h.deployLogs(deploymentID), want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("deploy log of %s does not contain %q; got:\n%s", deploymentID, want, h.deployLogs(deploymentID))
}

// seedWebhook stores the hook of an application directly — the provider API is
// unreachable in CI, so the row is written with the same sealed secret the
// installer would have stored, and the test signs deliveries with the plain
// value it passed in. Creating an application already auto-installed a hook
// with a random secret (the harness wires cfg.Hooks like production), so that
// row is replaced: the test must sign with the secret it knows.
func (h *p4Harness) seedWebhook(t *testing.T, app p4Application, plainSecret string) {
	t.Helper()
	sealed, err := providers.SealSecret(h.secret, plainSecret)
	if err != nil {
		t.Fatalf("seal webhook secret: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	applicationID := pgUUID(uuid.MustParse(app.ID))
	if _, err := h.st.DeleteApplicationWebhook(ctx, applicationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("clear auto-installed webhook row: %v", err)
	}
	if _, err := h.st.CreateApplicationWebhook(ctx, sqlc.CreateApplicationWebhookParams{
		ApplicationID: applicationID,
		Provider:      "github",
		Repo:          app.Repo,
		HookID:        "p4-e2e-hook",
		Secret:        sealed,
		Url:           h.baseURL + "/v1/webhooks/github",
	}); err != nil {
		t.Fatalf("seed webhook row: %v", err)
	}
}

// removeAppArtifacts registers cleanup of the containers the application
// started and of the images its builds produced, so a run never leaves either
// behind on the test box.
func (h *p4Harness) removeAppArtifacts(t *testing.T, appID string) {
	t.Helper()
	// Cleanup is LIFO: register the image pass first so the container pass runs
	// before it. Removing a tagged image while its container is still running
	// fails, and the container must be gone before the tag can be dropped.
	t.Cleanup(func() { removeImages(t, "gotham/"+appID) })
	t.Cleanup(func() { removeLabelledContainers(t, "gotham.app_id="+appID) })
}

// pgUUID converts a domain id to its nullable PostgreSQL form.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// hostPortsTaken guards freeHostPort against handing the same loopback port to
// two callers in one run: the probe listener is closed before the container
// binds the port, so without this a later caller could reserve the same free
// number and the two containers would fight over it.
var (
	hostPortsMu    sync.Mutex
	hostPortsTaken = map[int32]bool{}
)

// freeHostPort reserves and immediately releases a loopback port, returning
// its number: the application container binds it for the duration of a test.
//
// ponytail: the close→bind window is still racy against processes outside this
// test binary; per-run bookkeeping removes the self-inflicted collision, which
// is the only one the suite controls.
func freeHostPort(t *testing.T) int32 {
	t.Helper()
	hostPortsMu.Lock()
	defer hostPortsMu.Unlock()
	for attempt := 0; attempt < 50; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve host port: %v", err)
		}
		port := int32(listener.Addr().(*net.TCPAddr).Port)
		if err := listener.Close(); err != nil {
			t.Fatalf("release host port: %v", err)
		}
		if hostPortsTaken[port] {
			continue
		}
		hostPortsTaken[port] = true
		// Release the reservation when the test finishes: artifact cleanup is
		// registered later (removeAppArtifacts) so it runs first under LIFO,
		// and the port is free again by the time it is handed out twice.
		t.Cleanup(func() {
			hostPortsMu.Lock()
			defer hostPortsMu.Unlock()
			delete(hostPortsTaken, port)
		})
		return port
	}
	t.Fatalf("could not reserve a free host port after 50 attempts")
	return 0
}

// removeLabelledContainers force-removes every container carrying label.
func removeLabelledContainers(t *testing.T, label string) {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Logf("cleanup: docker CLI not found, remove containers with %s manually", label)
		return
	}
	output, err := exec.Command(docker, "ps", "-aq", "--filter", "label="+label).CombinedOutput()
	if err != nil {
		t.Logf("cleanup: docker ps --filter label=%s: %v: %s", label, err, strings.TrimSpace(string(output)))
		return
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return
	}
	args := append([]string{"rm", "-f"}, ids...)
	if removed, err := exec.Command(docker, args...).CombinedOutput(); err != nil {
		t.Logf("cleanup: docker rm %v: %v: %s", ids, err, strings.TrimSpace(string(removed)))
	}
}

// removeContainersMatchingCommand force-removes every container whose
// reported command contains marker. The legacy Docker builder leaves the
// scratch container of a failed RUN step behind without labels, so
// label-based cleanup cannot see it.
//
// `docker ps` truncates .Command, which can cut the marker off before the
// match ever runs. Resolve the ids first and inspect each container's full
// command (entrypoint path, args and configured cmd) instead, in a single
// inspect call: vanished ids only add stderr, the surviving rows still parse.
func removeContainersMatchingCommand(t *testing.T, marker string) {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Logf("cleanup: docker CLI not found, remove containers running %q manually", marker)
		return
	}
	output, err := exec.Command(docker, "ps", "-aq").CombinedOutput()
	if err != nil {
		t.Logf("cleanup: docker ps -aq: %v: %s", err, strings.TrimSpace(string(output)))
		return
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return
	}
	args := append([]string{"inspect", "--format", `{{.ID}} {{.Path}} {{join .Args " "}} {{join .Config.Cmd " "}}`}, ids...)
	inspectOut, _ := exec.Command(docker, args...).CombinedOutput()
	var matched []string
	for _, line := range strings.Split(string(inspectOut), "\n") {
		id, command, ok := strings.Cut(strings.TrimSpace(line), " ")
		// Stderr rides along in CombinedOutput; only full hex ids are
		// inspect rows, never "error: no such object" lines.
		if !ok || len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
			continue
		}
		if !strings.Contains(command, marker) {
			continue
		}
		matched = append(matched, id)
	}
	if len(matched) == 0 {
		return
	}
	args = append([]string{"rm", "-f"}, matched...)
	if removed, err := exec.Command(docker, args...).CombinedOutput(); err != nil {
		t.Logf("cleanup: docker rm %v: %v: %s", matched, err, strings.TrimSpace(string(removed)))
	}
}

// removeImages force-removes every image reference containing ref, including
// the node-registry tag a build produced.
func removeImages(t *testing.T, ref string) {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Logf("cleanup: docker CLI not found, remove images matching %s manually", ref)
		return
	}
	output, err := exec.Command(docker, "images", "--format", "{{.Repository}}:{{.Tag}}").CombinedOutput()
	if err != nil {
		t.Logf("cleanup: docker images: %v: %s", err, strings.TrimSpace(string(output)))
		return
	}
	for _, image := range strings.Fields(string(output)) {
		if !strings.Contains(image, ref) {
			continue
		}
		if removed, err := exec.Command(docker, "rmi", "-f", image).CombinedOutput(); err != nil {
			t.Logf("cleanup: docker rmi %s: %v: %s", image, err, strings.TrimSpace(string(removed)))
		}
	}
}

// danglingImageIDs returns the short ids of every untagged image the daemon
// holds.
func danglingImageIDs(docker string) (map[string]bool, error) {
	var stderr bytes.Buffer
	command := exec.Command(docker, "images", "-q", "--filter", "dangling=true")
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("docker images --filter dangling=true: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	ids := map[string]bool{}
	for _, id := range strings.Fields(string(output)) {
		ids[id] = true
	}
	return ids, nil
}

// removeNewDanglingImages registers cleanup of the dangling images this run's
// builds leave behind. removeImages drops the built tags, but the legacy
// builder's intermediate layers survive as untagged images no tag-based
// cleanup can see; only the ids that appear during the test are removed, so
// unrelated untagged images (and their searchable cache) outlive the run.
// Cleanup is LIFO: registering this before any build makes it run after every
// per-application tag has been dropped, including on failed tests.
//
// ponytail: daemon-wide id diff — a concurrent builder on the same daemon
// could lose a fresh intermediate; scope per-build if that ever bites.
func removeNewDanglingImages(t *testing.T) {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Logf("cleanup: docker CLI not found, remove dangling build images manually")
		return
	}
	before, err := danglingImageIDs(docker)
	if err != nil {
		t.Logf("cleanup: list dangling images before the run: %v", err)
		return
	}
	t.Cleanup(func() {
		after, err := danglingImageIDs(docker)
		if err != nil {
			t.Logf("cleanup: list dangling images: %v", err)
			return
		}
		for id := range after {
			if before[id] {
				continue
			}
			// One rmi can cascade-remove untagged parents, so later ids in
			// this set may already be gone; that is not a cleanup failure.
			if output, err := exec.Command(docker, "rmi", "-f", id).CombinedOutput(); err != nil &&
				!strings.Contains(string(output), "No such image") {
				t.Logf("cleanup: docker rmi -f %s: %v: %s", id, err, strings.TrimSpace(string(output)))
			}
		}
	})
}

// containsAny reports whether s carries at least one of the substrings.
func containsAny(s string, substrings ...string) bool {
	for _, substring := range substrings {
		if strings.Contains(s, substring) {
			return true
		}
	}
	return false
}
