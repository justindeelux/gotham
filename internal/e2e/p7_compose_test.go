package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Phase 7 (BE-7.1) production acceptance. The test drives the real control
// plane service surface (store -> services.Service -> mTLS agent
// ComposeServer -> `docker compose` CLI -> Docker) against the local Docker
// daemon and the dev Postgres database, and asserts the observable compose
// behavior:
//
//   - a two-service project with a named volume deploys and ps reports both
//     containers running,
//   - per-service logs stream and carry the substituted environment value,
//   - the domain label maps to the right compose service,
//   - down keeps the named volume (and its data) across stop/deploy,
//   - deleting the service keeps the volume.
//
// Gated by GOTHAM_E2E=1 like the Phase 6 acceptance: with the gate set, a
// missing prerequisite fails the test instead of skipping it green.
const (
	p7BusyboxImage = "busybox:1.36"
	p7NginxImage   = "nginx:1.23"
	p7PollTimeout  = 3 * time.Minute
)

// TestP7ComposeServiceProduction is gated by GOTHAM_E2E=1. Docker and the
// compose plugin must be reachable (fatal, never skipped as green); Postgres
// follows the suite convention and skips when unreachable.
func TestP7ComposeServiceProduction(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := testLogger(t)

	engine, err := agent.NewDockerClient(e2eDockerSock())
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
	userRow, err := st.CreateUser(ctx, "p7-e2e-"+suffix+"@example.com", nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uuid.UUID(userRow.ID.Bytes)
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
	serverID := uuid.UUID(serverRow.ID.Bytes)
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
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID,
		agent.WithProxyService(proxyAgent),
		agent.WithComposeService(composeAgent))

	// R4 convention from the Phase 6 test: a pre-existing fixed-name proxy
	// container may be live state this run cannot prove it owns.
	if existing := p6FindContainer(t, ctx, engine); existing != "" {
		t.Fatalf("a gotham-traefik container already exists (%s); refusing to remove a possibly live proxy", existing)
	}
	createdIDs := []string{}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		for _, id := range createdIDs {
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
	registry := p6Registry{server: &servers.Server{ID: serverID, IP: "127.0.0.1", NodeID: &nodeID}}
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
	proxyService := proxy.NewService(proxy.Config{
		Store:       st,
		Containers:  containerService,
		Services:    services.NewProxySource(st),
		BackendHost: backendHost,
		Dial: func(dialCtx context.Context, id uuid.UUID) (proxy.AgentClient, error) {
			if id != serverID {
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

	composeService := services.NewService(services.Config{
		Store:  st,
		Logger: logger,
		Proxy:  proxyService,
		Dial: func(dialCtx context.Context, id uuid.UUID) (services.ComposeAgent, error) {
			if id != serverID {
				return nil, servers.ErrNotFound
			}
			client, err := servers.DialComposeClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
			if err != nil {
				return nil, err
			}
			return services.NewGRPCComposeAgent(client), nil
		},
	})

	// The document exercises the label convention, env substitution and a
	// named volume. `$${MESSAGE}` escapes this control plane's interpolation
	// so the container's shell expands the substituted value at runtime.
	domain := "p7-" + suffix + ".example.test"
	message := "p7-alive-" + suffix
	document := fmt.Sprintf(`services:
  web:
    image: %s
    labels:
      gotham.domain: ${DOMAIN}
      gotham.domain.port: "80"
    ports:
      - "80"
    volumes:
      - data:/data
  worker:
    image: %s
    environment:
      MESSAGE: ${MESSAGE}
    command: ["sh", "-c", "while true; do echo $${MESSAGE}; sleep 1; done"]
volumes:
  data:
`, p7NginxImage, p7BusyboxImage)

	// The domain map maps the labeled compose service to its validated host.
	rendered, err := services.Render(document, map[string]string{"DOMAIN": domain, "MESSAGE": message})
	if err != nil {
		t.Fatalf("render sample compose: %v", err)
	}
	if len(rendered.Spec.Domains) != 1 {
		t.Fatalf("domain map = %+v, want exactly the web route", rendered.Spec.Domains)
	}
	if route := rendered.Spec.Domains[0]; route.Service != "web" || route.Domain != domain || route.Port != 80 {
		t.Fatalf("domain map = %+v, want web -> %s:80", route, domain)
	}

	pullCtx, pullCancel := context.WithTimeout(ctx, p7PollTimeout)
	for _, image := range []string{p7NginxImage, p7BusyboxImage} {
		if err := engine.PullImage(pullCtx, image); err != nil {
			pullCancel()
			t.Fatalf("pull %s: %v", image, err)
		}
	}
	pullCancel()

	created, err := composeService.Create(ctx, userID, services.CreateRequest{
		Name:        "p7-" + suffix,
		ServerID:    serverID,
		ComposeYAML: document,
		Env:         map[string]string{"DOMAIN": domain, "MESSAGE": message},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	project := services.ProjectName(created.ID)
	volume := project + "_data"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		// The project's containers are down by the end of the test; remove
		// the fixture's own volume so nothing leaks.
		if output, err := runDocker(cleanupCtx, "volume", "rm", volume); err != nil {
			t.Logf("cleanup volume %s: %v: %s", volume, err, output)
		}
	})

	deployed, deploy, err := composeService.Deploy(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if deployed.Status != services.StatusRunning || deploy.State != services.DeployRunning {
		t.Fatalf("deployed = %+v, deploy = %+v", deployed, deploy)
	}

	// ps reports both compose services running.
	containerList := p7WaitForRunning(t, ctx, composeService, userID, created.ID, 2)
	t.Logf("containers: %+v", containerList)

	// The domain label becomes a real Traefik route on this node: the
	// generated router points at the project's published backend port and a
	// Host-header request serves the nginx page.
	p7Sync(t, ctx, proxyService, serverID)
	if traefikID := p6FindContainer(t, ctx, engine); traefikID != "" {
		createdIDs = append(createdIDs, traefikID)
	}
	if body := p6ExpectHTTP(t, domain, 200); !strings.Contains(body, "Welcome to nginx") {
		t.Fatalf("the service domain served %q, want nginx", body)
	}

	// Per-service logs carry the substituted environment value.
	logs := p7ReadLogs(t, ctx, composeService, userID, created.ID, "worker")
	if !strings.Contains(logs, message) {
		t.Fatalf("worker logs do not contain %q:\n%s", message, logs)
	}

	// Data written into the named volume must survive stop and redeploy.
	p7WriteMarker(t, ctx, volume, "marker-"+suffix)
	stopped, err := composeService.Stop(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != services.StatusStopped {
		t.Fatalf("stopped status = %q", stopped.Status)
	}
	if !p7VolumeExists(t, ctx, volume) {
		t.Fatalf("down removed the named volume %s", volume)
	}
	if containers := p7ListContainers(t, ctx, composeService, userID, created.ID); len(containers) != 0 {
		t.Fatalf("down left containers behind: %+v", containers)
	}
	// Stop removes the route: the same host now answers 404 through Traefik.
	p7Sync(t, ctx, proxyService, serverID)
	p6ExpectHTTP(t, domain, 404)

	if _, _, err := composeService.Deploy(ctx, userID, created.ID); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	p7WaitForRunning(t, ctx, composeService, userID, created.ID, 2)
	if marker := p7ReadMarker(t, ctx, volume); !strings.Contains(marker, "marker-"+suffix) {
		t.Fatalf("volume data did not survive the redeploy: %q", marker)
	}
	p7Sync(t, ctx, proxyService, serverID)
	p6ExpectHTTP(t, domain, 200)

	// The deploy history snapshots every attempt.
	deploys, err := composeService.Deploys(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Deploys: %v", err)
	}
	if len(deploys) != 2 || deploys[0].State != services.DeployRunning {
		t.Fatalf("deploys = %+v", deploys)
	}

	// Restart restarts the running project in place and keeps the volume
	// data.
	restarted, err := composeService.Restart(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.Status != services.StatusRunning {
		t.Fatalf("restarted status = %q", restarted.Status)
	}
	p7WaitForRunning(t, ctx, composeService, userID, created.ID, 2)
	if marker := p7ReadMarker(t, ctx, volume); !strings.Contains(marker, "marker-"+suffix) {
		t.Fatalf("volume data did not survive the restart: %q", marker)
	}

	// Delete stops the project and keeps the data.
	if err := composeService.Delete(ctx, userID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !p7VolumeExists(t, ctx, volume) {
		t.Fatalf("delete removed the named volume %s", volume)
	}
	if _, err := composeService.Get(ctx, userID, created.ID); err == nil {
		t.Fatal("deleted service is still readable")
	}
	// The deleted service's host is no longer claimed.
	p7Sync(t, ctx, proxyService, serverID)
	p6ExpectHTTP(t, domain, 404)
}

// p7Sync runs one proxy sync, tolerating the diagnostics a stopped or deleted
// project produces (the sync still pushes the healthy configuration).
func p7Sync(t *testing.T, ctx context.Context, svc proxy.ProxyService, serverID uuid.UUID) {
	t.Helper()
	syncCtx, cancel := context.WithTimeout(ctx, p7PollTimeout)
	defer cancel()
	if err := svc.SyncServer(syncCtx, serverID); err != nil && !errors.Is(err, proxy.ErrPartialSync) {
		t.Fatalf("proxy sync: %v", err)
	}
}

// p7ListContainers reads the project's container list through the service.
func p7ListContainers(t *testing.T, ctx context.Context, svc services.ServiceService, userID, serviceID uuid.UUID) []services.ComposeContainer {
	t.Helper()
	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	containers, err := svc.Containers(listCtx, userID, serviceID)
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	return containers
}

// p7WaitForRunning polls ps until the project reports the expected number of
// running containers.
func p7WaitForRunning(t *testing.T, ctx context.Context, svc services.ServiceService, userID, serviceID uuid.UUID, want int) []services.ComposeContainer {
	t.Helper()
	deadline := time.Now().Add(p7PollTimeout)
	var last []services.ComposeContainer
	for {
		last = p7ListContainers(t, ctx, svc, userID, serviceID)
		running := 0
		for _, container := range last {
			if container.State == "running" {
				running++
			}
		}
		if running == want {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("project never reported %d running containers; last: %+v", want, last)
		}
		time.Sleep(time.Second)
	}
}

// p7ReadLogs reads one compose service's logs (tail, no follow) through the
// service and returns the collected text.
func p7ReadLogs(t *testing.T, ctx context.Context, svc services.ServiceService, userID, serviceID uuid.UUID, composeService string) string {
	t.Helper()
	logCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	stream, err := svc.Logs(logCtx, userID, serviceID, composeService, 100, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer func() { _ = stream.Close() }()
	var builder strings.Builder
	for chunk := range stream.Chunks() {
		builder.Write(chunk)
	}
	return builder.String()
}

// p7WriteMarker writes a marker file into the project's named volume through
// the Docker CLI (the suite drives Docker through the agent; the CLI is used
// only for fixture setup and cleanup, like the Phase 6 failure dumps).
func p7WriteMarker(t *testing.T, ctx context.Context, volume, content string) {
	t.Helper()
	output, err := runDocker(ctx, "run", "--rm", "-v", volume+":/data", p7BusyboxImage,
		"sh", "-c", "printf %s "+content+" > /data/marker")
	if err != nil {
		t.Fatalf("write marker: %v: %s", err, output)
	}
}

// p7ReadMarker reads the marker file back from the volume.
func p7ReadMarker(t *testing.T, ctx context.Context, volume string) string {
	t.Helper()
	output, err := runDocker(ctx, "run", "--rm", "-v", volume+":/data", p7BusyboxImage,
		"cat", "/data/marker")
	if err != nil {
		t.Fatalf("read marker: %v: %s", err, output)
	}
	return output
}

// p7VolumeExists reports whether the named volume exists on the node.
func p7VolumeExists(t *testing.T, ctx context.Context, volume string) bool {
	t.Helper()
	output, err := runDocker(ctx, "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		t.Fatalf("list volumes: %v: %s", err, output)
	}
	for _, name := range strings.Split(output, "\n") {
		if strings.TrimSpace(name) == volume {
			return true
		}
	}
	return false
}
