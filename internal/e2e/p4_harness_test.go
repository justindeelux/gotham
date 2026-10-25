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

const (
	e2eDSNEnv      = "GOTHAM_TEST_DSN"
	defaultE2EDSN  = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	cloneLocalEnv  = "GOTHAM_DEV_CLONE_LOCAL"
	p4Secret       = "gotham-e2e-secret-key"
	p4DeployWait   = 6 * time.Minute
	p4LogWait      = 30 * time.Second
	p4PollInterval = 500 * time.Millisecond
)

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

type p4CreateApplication struct {
	EnvironmentID  string `json:"environment_id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Repo           string `json:"repo"`
	CloneURL       string `json:"clone_url"`
	SourceType     string `json:"source_type"`
	ComposeContent string `json:"compose_content"`
	ComposeFile    string `json:"compose_file"`
	ComposeService string `json:"compose_service"`
	Branch         string `json:"branch"`
	BuildPack      string `json:"build_pack"`
	ServerID       string `json:"server_id"`
}

type p4Application struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Repo          string `json:"repo"`
	CloneURL      string `json:"clone_url"`
	Branch        string `json:"branch"`
	BuildPack     string `json:"build_pack"`
	PrimaryDomain string `json:"primary_domain"`
	ServerID      string `json:"server_id"`
}

type p4ApplicationEnvelope struct {
	Application p4Application `json:"application"`
}

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

type p4DeploymentEnvelope struct {
	Deployment p4Deployment `json:"deployment"`
}

type p4DeploymentList struct {
	Deployments []p4Deployment `json:"deployments"`
}

type p4LogLine struct {
	Channel string
	Data    string
}

type p4LogBuffer struct {
	mu    sync.Mutex
	lines []p4LogLine
}

func (b *p4LogBuffer) add(channel, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, p4LogLine{Channel: channel, Data: data})
}

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

type localDialer struct {
	serverID  uuid.UUID
	addr      string
	nodeID    string
	authority *servers.Authority
}

func (d *localDialer) DialDockerClient(ctx context.Context, id uuid.UUID,
	opts ...servers.DockerDialOption) (*servers.DockerClient, error) {
	if id != d.serverID {
		return nil, servers.ErrNotFound
	}
	options := append([]servers.DockerDialOption{servers.WithDockerServerName(d.nodeID)}, opts...)
	return servers.DialDockerClient(ctx, d.addr, d.authority, options...)
}

type stubInstaller struct{}

func (stubInstaller) CreateWebhook(context.Context, providers.HookTarget, providers.Webhook) (string, error) {
	return "p4-e2e-hook", nil
}

func (stubInstaller) DeleteWebhook(context.Context, providers.HookTarget, string) error { return nil }

func newP4Harness(t *testing.T) *p4Harness {
	t.Helper()
	return newP4HarnessWithAgentOptions(t)
}

func newP4HarnessWithAgentOptions(t *testing.T, options ...agent.ServerOption) *p4Harness {
	t.Helper()
	requireE2E(t)
	removeNewDanglingImages(t)
	t.Setenv(cloneLocalEnv, "true")
	t.Setenv(deploy.GitAllowPrivateHostsEnv, "true")
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

	nodeID := "p4-e2e-" + uuid.New().String()[:8]
	agentCtx, agentCancel := context.WithCancel(context.Background())
	t.Cleanup(agentCancel)
	agentAddr, authority := startLocalAgentWithOptions(t, agentCtx, engine, nodeID, options...)

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
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: "p4-e2e",
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
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server row: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user row: %v", err)
		}
	})

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

	t.Setenv(webhooks.FeatureEnv, "true")
	hookSvc = webhooks.NewService(webhooks.Config{
		Store:       st,
		Installer:   stubInstaller{},
		Deployer:    deploySvc,
		Provisioner: deploySvc,
		Secret:      p4Secret,
		Logger:      testLogger(t),
	})

	userIDFunc := func(context.Context) (uuid.UUID, bool) { return userID, true }
	requireAuth := func(next http.Handler) http.Handler { return next }
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		deploy.Mount(r, requireAuth, userIDFunc, deploySvc)
		webhooks.Mount(r, requireAuth, userIDFunc, hookSvc)
	})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

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

func p4DSN() string {
	if dsn := strings.TrimSpace(os.Getenv(e2eDSNEnv)); dsn != "" {
		return dsn
	}
	return defaultE2EDSN
}

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

func (h *p4Harness) createApplication(t *testing.T, in p4CreateApplication) p4Application {
	t.Helper()
	var envelope p4ApplicationEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications", in, &envelope)
	if status != http.StatusCreated {
		t.Fatalf("create application: status %d: %s", status, raw)
	}
	h.removeAppArtifacts(t, envelope.Application.ID)
	return envelope.Application
}

func (h *p4Harness) addDomain(t *testing.T, appID, domain string, port int32) {
	t.Helper()
	status, body := h.api(t, http.MethodPost, "/v1/applications/"+appID+"/domains", map[string]any{
		"domain": domain, "port": port, "protocol": "http",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("add domain %s: status %d (%s)", domain, status, body)
	}
}

func (h *p4Harness) publishedHostPort(t *testing.T, appID string, appPort int32) int32 {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatalf("docker CLI: %v", err)
	}
	out, err := exec.Command(docker, "ps", "-q", "--filter", "label=gotham.app_id="+appID).CombinedOutput()
	if err != nil {
		t.Fatalf("docker ps for %s: %v: %s", appID, err, strings.TrimSpace(string(out)))
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		t.Fatalf("no container for application %s", appID)
	}
	portSpec := fmt.Sprintf("%d/tcp", appPort)
	out, err = exec.Command(docker, "port", ids[0], portSpec).CombinedOutput()
	if err != nil {
		t.Fatalf("docker port %s %s: %v: %s", ids[0], portSpec, err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		var port int32
		if _, err := fmt.Sscanf(line[idx+1:], "%d", &port); err == nil && port > 0 {
			return port
		}
	}
	t.Fatalf("no published host port for %s %s (%s)", ids[0], portSpec, strings.TrimSpace(string(out)))
	return 0
}

func (h *p4Harness) appURL(t *testing.T, appID string) string {
	t.Helper()
	return fmt.Sprintf("http://127.0.0.1:%d/index.html", h.publishedHostPort(t, appID, p4ContainerPort))
}

func (h *p4Harness) deploy(t *testing.T, appID string) p4Deployment {
	t.Helper()
	var envelope p4DeploymentEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications/"+appID+"/deploy", nil, &envelope)
	if status != http.StatusAccepted {
		t.Fatalf("deploy: status %d: %s", status, raw)
	}
	return envelope.Deployment
}

func (h *p4Harness) rollback(t *testing.T, appID string, body any) p4Deployment {
	t.Helper()
	var envelope p4DeploymentEnvelope
	status, raw := h.api(t, http.MethodPost, "/v1/applications/"+appID+"/rollback", body, &envelope)
	if status != http.StatusAccepted {
		t.Fatalf("rollback: status %d: %s", status, raw)
	}
	return envelope.Deployment
}

func (h *p4Harness) deployments(t *testing.T, appID string) []p4Deployment {
	t.Helper()
	var list p4DeploymentList
	status, raw := h.api(t, http.MethodGet, "/v1/applications/"+appID+"/deployments", nil, &list)
	if status != http.StatusOK {
		t.Fatalf("list deployments: status %d: %s", status, raw)
	}
	return list.Deployments
}

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

func (h *p4Harness) deployLogs(deploymentID string) string {
	channel := deploy.DeployChannel(h.serverID, uuid.MustParse(deploymentID))
	return h.logs.forChannel(channel)
}

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

func (h *p4Harness) removeAppArtifacts(t *testing.T, appID string) {
	t.Helper()
	t.Cleanup(func() { removeImages(t, "gotham/"+appID) })
	t.Cleanup(func() { removeLabelledContainers(t, "gotham.app_id="+appID) })
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

var (
	hostPortsMu    sync.Mutex
	hostPortsTaken = map[int32]bool{}
)

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
			if output, err := exec.Command(docker, "rmi", "-f", id).CombinedOutput(); err != nil &&
				!strings.Contains(string(output), "No such image") {
				t.Logf("cleanup: docker rmi -f %s: %v: %s", id, err, strings.TrimSpace(string(output)))
			}
		}
	})
}

func containsAny(s string, substrings ...string) bool {
	for _, substring := range substrings {
		if strings.Contains(s, substring) {
			return true
		}
	}
	return false
}
