package e2e

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// p7Harness is the Phase 7 acceptance fixture shared by the BE-7.1 compose
// acceptance and the BE-7.2 template acceptance: the real control plane
// service stack (store -> services.Service -> mTLS agent ComposeServer ->
// `docker compose` CLI -> Docker) plus the Phase 6 proxy service on the same
// node, against the local Docker daemon and the dev Postgres database.
//
// Every prerequisite is fatal, never skipped: the caller is already behind the
// GOTHAM_E2E gate, so a missing Docker, compose plugin or Postgres must fail
// instead of skipping green.
type p7Harness struct {
	engine   *agent.DockerClient
	compose  services.ServiceService
	proxy    proxy.ProxyService
	userID   uuid.UUID
	serverID uuid.UUID
	envID    uuid.UUID
	logger   *slog.Logger

	// suffix is the per-run identifier every fixture name carries.
	suffix string

	// createdIDs are the containers this test owns and must remove.
	createdIDs []string
}

// newP7Harness boots the harness. It also refuses to run when a fixed-name
// proxy container already exists (the Phase 6 R4 convention): that container
// may be live state this run cannot prove it owns.
func newP7Harness(t *testing.T, ctx context.Context) *p7Harness {
	t.Helper()
	logger := testLogger(t)
	h := &p7Harness{logger: logger}

	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client for %s: %v", e2eDockerSock(), err)
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	version, err := engine.Version(versionCtx)
	versionCancel()
	if err != nil {
		t.Fatalf("docker daemon unreachable at %s: %v", e2eDockerSock(), err)
	}
	t.Logf("docker %s at %s", version, e2eDockerSock())
	h.engine = engine

	// The compose plugin is the feature's only node prerequisite.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker CLI not found: %v", err)
	}
	if output, err := runDocker(ctx, "compose", "version"); err != nil {
		t.Fatalf("docker compose plugin unavailable (%v): %s", err, output)
	}

	dsn := e2eDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("Postgres/migrations unavailable at %s: %v", dsn, err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Postgres unavailable at %s: %v", dsn, err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	suffix := uuid.New().String()[:8]
	h.suffix = suffix
	userRow, err := st.CreateUser(ctx, "p7-e2e-"+suffix+"@example.com", nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	h.userID = uuid.UUID(userRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userRow.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p7-e2e-" + suffix,
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	h.serverID = uuid.UUID(serverRow.ID.Bytes)
	// Services require an environment since PE-2; the harness calls the
	// service directly without a team scope, so the personal team
	// (ID = user ID, created with the account) scopes it.
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TeamID: userRow.ID, Name: "p7-e2e-" + suffix,
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
	h.envID = uuid.UUID(environment.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	// The node agent with the real ComposeServer rooted in a temp directory
	// (macOS temp paths carry /var -> /private/var symlinks the agent's
	// no-follow traversal rejects, so the fixture is canonicalized).
	nodeID := "p7-e2e-" + suffix
	composeRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	composeAgent := agent.NewComposeServer(agent.ComposeServerConfig{
		Root:       composeRoot,
		DockerHost: e2eDockerSock(),
		Logger:     logger,
	})
	// The node also serves the Phase 6 proxy service: the service domains are
	// routed through the real Traefik on this node.
	proxyConfigDir := p6CanonicalTempDir(t)
	proxyAcmeDir := proxyConfigDir + "/acme"
	if err := os.MkdirAll(proxyAcmeDir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	proxyAgent := agent.NewProxyServer(agent.ProxyServerConfig{Root: proxyConfigDir, Logger: logger})
	agentAddr, authority := startLocalAgentWithProxyRoot(t, ctx, engine, nodeID, proxyConfigDir,
		agent.WithProxyService(proxyAgent),
		agent.WithComposeService(composeAgent))

	// R4 convention from the Phase 6 test: a pre-existing fixed-name proxy
	// container may be live state this run cannot prove it owns.
	if existing := p6FindContainer(t, ctx, engine); existing != "" {
		t.Fatalf("a gotham-traefik container already exists (%s); refusing to remove a possibly live proxy", existing)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		for _, id := range h.createdIDs {
			if err := engine.Remove(cleanupCtx, id); err != nil {
				t.Logf("cleanup container %s: %v", id, err)
			}
		}
	})
	// On failure, keep the diagnosis close: the mounted configuration and the
	// proxy logs are dumped before cleanup (the Phase 6 convention).
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		current := p6FindContainer(t, context.Background(), engine)
		if current == "" {
			t.Log("traefik container no longer exists for the failure dump")
			return
		}
		if logs, err := runDocker(context.Background(), "logs", current); err == nil {
			t.Logf("traefik logs:\n%s", logs)
		}
		if listing, err := runDocker(context.Background(), "exec", current, "ls", "-la", proxy.TraefikDynamicDir); err == nil {
			t.Logf("container dynamic dir:\n%s", listing)
		}
		if data, err := os.ReadFile(proxyConfigDir + "/dynamic/gotham.yml"); err == nil {
			t.Logf("host dynamic config:\n%s", data)
		} else {
			t.Logf("read host dynamic config: %v", err)
		}
	})
	registry := p6Registry{server: &servers.Server{ID: h.serverID, IP: "127.0.0.1", NodeID: &nodeID}}
	containerService := containers.NewService(containers.Config{
		Registry: registry,
		Cache:    containers.NopCache{},
		Dial: func(dialCtx context.Context, _ *servers.Server) (containers.DockerClient, error) {
			return servers.DialDockerClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger: logger,
	})
	t.Cleanup(func() { _ = containerService.Close() })

	// Linux nodes reach published ports through the bridge gateway; Docker
	// Desktop for macOS needs host.docker.internal (the Phase 6 convention).
	backendHost := proxy.DefaultBackendHost
	if runtime.GOOS == "darwin" {
		backendHost = "host.docker.internal"
	}
	h.proxy = proxy.NewService(proxy.Config{
		Store:       st,
		Containers:  containerService,
		Services:    services.NewProxySource(st),
		BackendHost: backendHost,
		Dial: func(dialCtx context.Context, id uuid.UUID) (proxy.AgentClient, error) {
			if id != h.serverID {
				return nil, servers.ErrNotFound
			}
			return servers.DialProxyClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger:    logger,
		ConfigDir: proxyConfigDir,
		AcmeDir:   proxyAcmeDir,
	})
	traefikPullCtx, traefikPullCancel := context.WithTimeout(ctx, p7PollTimeout)
	if err := engine.PullImage(traefikPullCtx, proxy.TraefikImage); err != nil {
		traefikPullCancel()
		t.Fatalf("pull %s: %v", proxy.TraefikImage, err)
	}
	traefikPullCancel()

	h.compose = services.NewService(services.Config{
		Store:  st,
		Logger: logger,
		Proxy:  h.proxy,
		Dial: func(dialCtx context.Context, id uuid.UUID) (services.ComposeAgent, error) {
			if id != h.serverID {
				return nil, servers.ErrNotFound
			}
			client, err := servers.DialComposeClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
			if err != nil {
				return nil, err
			}
			return services.NewGRPCComposeAgent(client), nil
		},
	})
	return h
}

// trackContainer registers a container this test owns for cleanup.
func (h *p7Harness) trackContainer(id string) {
	if id != "" {
		h.createdIDs = append(h.createdIDs, id)
	}
}
