package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Phase 6 (BE-6.1) production acceptance. The test drives the real control
// plane service surface (store -> proxy.SyncService -> containers.Service ->
// mTLS agent -> Docker) against the local Docker daemon and the dev Postgres
// database, and asserts the generated HTTP routes with real requests through
// the bootstrapped Traefik:
//
//   - bootstrap with the production port specs (including the loopback ping),
//   - pinned and ephemeral (HostPort 0) application endpoints,
//   - pending/invalid/disabled rows isolated as diagnostics,
//   - route update and removal (the old route stops),
//   - configuration history and fast revert,
//   - repair of a pre-fix container and native restart policy,
//   - an invalid document write that must NOT be presented as accepted.
//
// p6BaseURL is the production gateway served by the bootstrapped Traefik.
const p6BaseURL = "http://127.0.0.1:80/"

const (
	p6NginxImage   = "nginx:1.23"
	p6PullTimeout  = 3 * time.Minute
	p6HTTPWait     = 60 * time.Second
	p6PollInterval = 500 * time.Millisecond
	p6SyncTimeout  = 2 * time.Minute
)

// TestP6ProxySyncProduction is gated by GOTHAM_E2E=1. Docker must be reachable
// (fatal, never skipped as green); Postgres follows the suite convention and
// skips when unreachable.
func TestP6ProxySyncProduction(t *testing.T) {
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

	// GOTHAM_E2E=1 is an explicit opt-in to the production acceptance run: a
	// missing prerequisite fails the test instead of skipping it green.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker CLI not found: %v", err)
	}
	dsn := e2eDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("Postgres/migrations unavailable at %s: %v (run: docker compose -f deploy/compose.dev.yml up -d)", dsn, err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Postgres unavailable at %s: %v", dsn, err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	suffix := uuid.New().String()[:8]
	userRow, err := st.CreateUser(ctx, "p6-e2e-"+suffix+"@example.com", nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userRow.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p6-e2e-" + suffix,
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

	// The node agent: DockerService + the real ProxyService rooted in a temp
	// directory that the CP mounts into Traefik (a relocated node layout using
	// the same production code paths).
	nodeID := "p6-e2e-" + suffix
	// macOS temp paths contain OS-level symlinks (/var -> /private/var); the
	// agent's no-follow traversal from "/" needs canonical fixtures.
	configDir := p6CanonicalTempDir(t)
	acmeDir := configDir + "/acme"
	if err := os.MkdirAll(acmeDir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	proxyAgent := agent.NewProxyServer(agent.ProxyServerConfig{Root: configDir, Logger: logger})
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID,
		agent.WithProxyService(proxyAgent))

	registry := p6Registry{server: &servers.Server{ID: serverID, IP: "127.0.0.1", NodeID: &nodeID}}
	// R4: a pre-existing fixed-name container may be a live proxy this test
	// cannot prove it owns, so refuse to run instead of deleting it. Only
	// container IDs created by this run are cleaned up.
	if existing := p6FindContainer(t, ctx, engine); existing != "" {
		t.Fatalf("a gotham-traefik container already exists (%s); refusing to remove a possibly live proxy", existing)
	}
	createdIDs := []string{}
	trackCreated := func(id string) string {
		if id != "" {
			createdIDs = append(createdIDs, id)
		}
		return id
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		for _, id := range createdIDs {
			if err := engine.Remove(cleanupCtx, id); err != nil {
				t.Logf("cleanup container %s: %v", id, err)
			}
		}
	})
	containerService := containers.NewService(containers.Config{
		Registry: registry,
		Cache:    containers.NopCache{},
		Dial: func(dialCtx context.Context, _ *servers.Server) (containers.DockerClient, error) {
			return servers.DialDockerClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger: logger,
	})
	t.Cleanup(func() { _ = containerService.Close() })

	// Linux nodes (production and CI) reach published ports through the
	// bridge gateway. Docker Desktop for macOS does not route Docker-assigned
	// (ephemeral) ports through 172.17.0.1, so the desktop equivalent is used
	// locally; the production default stays in the generated configuration.
	backendHost := proxy.DefaultBackendHost
	if runtime.GOOS == "darwin" {
		backendHost = "host.docker.internal"
	}
	t.Logf("E2E backend host %s (production default %s)", backendHost, proxy.DefaultBackendHost)
	proxyService := proxy.NewService(proxy.Config{
		Store:       st,
		Containers:  containerService,
		BackendHost: backendHost,
		Dial: func(dialCtx context.Context, id uuid.UUID) (proxy.AgentClient, error) {
			if id != serverID {
				return nil, servers.ErrNotFound
			}
			return servers.DialProxyClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger:    logger,
		ConfigDir: configDir,
		AcmeDir:   acmeDir,
	})

	pullCtx, pullCancel := context.WithTimeout(ctx, p6PullTimeout)
	if err := engine.PullImage(pullCtx, p6NginxImage); err != nil {
		pullCancel()
		t.Fatalf("pull %s: %v", p6NginxImage, err)
	}
	pullCancel()

	pinnedHostPort := freeTCPPort(t)
	pinnedDomain := "pinned-" + suffix + ".example.test"
	ephemeralDomain := "ephemeral-" + suffix + ".example.test"
	pendingDomain := "pending-" + suffix + ".example.test"
	invalidDomain := "invalid domain " + suffix
	disabledDomain := "disabled-" + suffix + ".example.test"

	pinnedContainer := p6RunNginx(t, ctx, engine, "p6-app-pinned-"+suffix, fmt.Sprintf("%d:80", pinnedHostPort))
	ephemeralContainer := p6RunNginx(t, ctx, engine, "p6-app-ephemeral-"+suffix, "80")
	invalidContainer := p6RunNginx(t, ctx, engine, "p6-app-invalid-"+suffix, "80")
	disabledContainer := p6RunNginx(t, ctx, engine, "p6-app-disabled-"+suffix, "80")

	pinnedApp := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "pinned-"+suffix, pinnedDomain, pinnedHostPort, false)
	p6CreateRunningDeployment(t, ctx, pool, pinnedApp, pinnedContainer)
	ephemeralApp := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "ephemeral-"+suffix, ephemeralDomain, 0, false)
	p6CreateRunningDeployment(t, ctx, pool, ephemeralApp, ephemeralContainer)
	// Pending: a domain without a running deployment.
	p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "pending-"+suffix, pendingDomain, freeTCPPort(t), false)
	// Invalid legacy domain and a migration-disabled duplicate: both must be
	// isolated, not block the healthy rows.
	invalidApp := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "invalid-"+suffix, invalidDomain, freeTCPPort(t), false)
	p6CreateRunningDeployment(t, ctx, pool, invalidApp, invalidContainer)
	disabledApp := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "disabled-"+suffix, disabledDomain, freeTCPPort(t), true)
	p6CreateRunningDeployment(t, ctx, pool, disabledApp, disabledContainer)

	syncCtx, syncCancel := context.WithTimeout(ctx, p6SyncTimeout)
	err = proxyService.SyncServer(syncCtx, serverID)
	syncCancel()
	if !errors.Is(err, proxy.ErrPartialSync) {
		t.Fatalf("first sync err = %v, want ErrPartialSync", err)
	}
	var partial *proxy.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("first sync err = %v, want diagnostics", err)
	}
	if len(partial.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v, want pending + invalid + disabled", partial.Diagnostics)
	}
	t.Logf("first sync diagnostics: %v", partial.Diagnostics)

	traefikID := trackCreated(p6WaitForContainer(t, ctx, engine))
	// On failure, keep the diagnosis close: the proxy logs, the mounted
	// configuration and the container's mounts are dumped before cleanup.
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
		if data, err := os.ReadFile(configDir + "/dynamic/gotham.yml"); err == nil {
			t.Logf("host dynamic config:\n%s", data)
		} else {
			t.Logf("read host dynamic config: %v", err)
		}
		if mounts, err := runDocker(context.Background(), "inspect", "--format", "{{json .Mounts}}", current); err == nil {
			t.Logf("mounts:\n%s", mounts)
		}
	})
	p6AssertTraefikBootstrap(t, traefikID)

	if body := p6ExpectHTTP(t, pinnedDomain, http.StatusOK); !strings.Contains(body, "Welcome to nginx") {
		t.Fatalf("pinned route body = %q, want nginx", body)
	}
	p6ExpectHTTP(t, ephemeralDomain, http.StatusOK)
	p6ExpectHTTP(t, pendingDomain, http.StatusNotFound)
	p6ExpectHTTP(t, disabledDomain, http.StatusNotFound)

	// Route update: renaming the domain must move the route.
	renamedDomain := "renamed-" + suffix + ".example.test"
	if _, err := pool.Exec(ctx, "UPDATE applications SET base_domain = $2 WHERE id = $1", pgType(pinnedApp), renamedDomain); err != nil {
		t.Fatalf("update domain: %v", err)
	}
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync after update: %v", err)
	}
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)
	p6ExpectHTTP(t, pinnedDomain, http.StatusNotFound)

	// Removal: deleting the ephemeral application must stop its old route even
	// while pending/invalid/disabled rows remain on the node.
	if _, err := pool.Exec(ctx, "DELETE FROM applications WHERE id = $1", pgType(ephemeralApp)); err != nil {
		t.Fatalf("delete ephemeral app: %v", err)
	}
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync after delete: %v", err)
	}
	p6ExpectHTTP(t, ephemeralDomain, http.StatusNotFound)
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)

	// History and fast revert: reverting restores the version recorded before
	// the update/removal pair, bringing the ephemeral route back and keeping
	// the renamed one.
	var versionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM proxy_config_versions WHERE server_id = $1", serverRow.ID).Scan(&versionCount); err != nil {
		t.Fatalf("count config versions: %v", err)
	}
	if versionCount < 3 {
		t.Fatalf("config versions = %d, want at least 3 recorded syncs", versionCount)
	}
	revertCtx, revertCancel := context.WithTimeout(ctx, p6SyncTimeout)
	err = proxyService.RevertServer(revertCtx, serverID)
	revertCancel()
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	p6ExpectHTTP(t, ephemeralDomain, http.StatusOK)
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)

	// Repair: a container created before the host-IP binding fix (no loopback
	// ping) is replaced by the sync and the proxy comes back verified.
	currentID := p6FindContainer(t, ctx, engine)
	if currentID == "" {
		t.Fatal("managed proxy container disappeared before the repair step")
	}
	traefikID = currentID
	if err := engine.Remove(ctx, traefikID); err != nil {
		t.Fatalf("remove traefik: %v", err)
	}
	legacyID, err := engine.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:   proxy.TraefikImage,
		Name:    proxy.TraefikContainerName,
		Labels:  map[string]string{"gotham.managed": "true", "gotham.component": "proxy"},
		Ports:   []string{"80:80", "443:443"},
		Volumes: []string{configDir + ":" + proxy.TraefikContainerConfigDir, acmeDir + ":" + proxy.TraefikAcmeMount},
	})
	if err != nil {
		t.Fatalf("run legacy traefik: %v", err)
	}
	trackCreated(legacyID)
	t.Logf("legacy traefik %s without the loopback binding", legacyID)
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync must repair the legacy container: %v", err)
	}
	traefikID = trackCreated(p6WaitForContainer(t, ctx, engine))
	if traefikID == legacyID {
		t.Fatal("legacy container was not recreated")
	}
	p6AssertTraefikBootstrap(t, traefikID)
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)

	// Restart: the native policy is present and a restart serves again without
	// any control-plane action.
	if policy := p6Inspect(t, traefikID, "{{.HostConfig.RestartPolicy.Name}}"); policy != proxy.TraefikRestartPolicy {
		t.Fatalf("restart policy = %q, want %q", policy, proxy.TraefikRestartPolicy)
	}
	if err := engine.Restart(ctx, traefikID); err != nil {
		t.Fatalf("restart traefik: %v", err)
	}
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)

	// R5 convergence: owned drift (correct ports, wrong mount / missing
	// policy label / wrong image) is recreated, while an unowned same-name
	// container is never removed and fails the sync with an actionable
	// conflict.
	p6AssertRepairsDrift(t, ctx, engine, proxyService, serverID, configDir, acmeDir, &traefikID, &createdIDs, renamedDomain)

	// A malformed dynamic document must not be presented as accepted: the
	// verified write only proves the ping answered, and Traefik keeps serving
	// the last valid configuration.
	client, err := servers.DialProxyClient(ctx, agentAddr, authority, servers.WithDockerServerName(nodeID))
	if err != nil {
		t.Fatalf("dial proxy service: %v", err)
	}
	badWriteCtx, badWriteCancel := context.WithTimeout(ctx, 30*time.Second)
	badResponse, err := client.WriteProxyConfig(badWriteCtx, &agentv1.WriteProxyConfigRequest{
		Files: []*agentv1.ProxyConfigFile{{
			Path:    proxy.DynamicFileName(proxy.FormatYAML),
			Content: []byte("http: [this is not valid\n"),
		}},
		Verify: true,
	})
	badWriteCancel()
	if err != nil {
		t.Fatalf("invalid write: %v", err)
	}
	_ = client.Close()
	if !badResponse.GetReloaded() {
		t.Fatalf("invalid write ping failed: %s", badResponse.GetPingError())
	}
	// Wait for the file provider to actually reject the malformed document
	// before asserting the previous route survives: the verified write is a
	// ping signal, not configuration acceptance.
	rejection := p6WaitForLog(t, ctx, traefikID, []string{"error", "parse"}, []string{"error", "yaml"}, []string{"error", "gotham.yml"}, []string{"error", "unmarshal"})
	t.Logf("traefik rejected the malformed document: %s", rejection)
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)
	t.Log("invalid document kept the previous route serving; the verified write is a ping signal, not acceptance")
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("restore after invalid write: %v", err)
	}
	p6ExpectHTTP(t, renamedDomain, http.StatusOK)
}

// p6Registry resolves the one seeded node.
type p6Registry struct {
	server *servers.Server
}

// Get returns the seeded server or ErrNotFound.
func (r p6Registry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if r.server == nil || r.server.ID != id {
		return nil, servers.ErrNotFound
	}
	return r.server, nil
}

// e2eDSN resolves the database DSN the suite uses.
func e2eDSN() string {
	if dsn := strings.TrimSpace(os.Getenv(e2eDSNEnv)); dsn != "" {
		return dsn
	}
	return defaultE2EDSN
}

// p6Sync runs one sync and tolerates the diagnostics of the mixed rows.
func p6Sync(svc *proxy.SyncService, serverID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.Background(), p6SyncTimeout)
	defer cancel()
	if err := svc.SyncServer(ctx, serverID); err != nil && !errors.Is(err, proxy.ErrPartialSync) {
		return err
	}
	return nil
}

// p6RunNginx starts an nginx container publishing the given spec. The agent
// creates the container before starting it and returns the created ID together
// with a start error (a port conflict, for example), so cleanup is registered
// for any returned ID before the error is handled and a failed start cannot
// leak the container. Removal of this owned container fails the test, like
// every other owned-resource cleanup.
func p6RunNginx(t *testing.T, ctx context.Context, engine *agent.DockerClient, name, portSpec string) string {
	t.Helper()
	id, err := engine.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image: p6NginxImage,
		Name:  name,
		Ports: []string{portSpec},
	})
	if id != "" {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if err := engine.Remove(cleanupCtx, id); err != nil {
				t.Errorf("cleanup container %s: %v", name, err)
			}
		})
	}
	if err != nil {
		t.Fatalf("run %s: %v", name, err)
	}
	return id
}

// p6CreateApplication inserts an application row with a direct SQL write so
// the test can seed legacy values (invalid domain, disabled duplicate) the
// API validation would reject, and returns its id.
func p6CreateApplication(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, serverID pgtype.UUID, name, domain string, hostPort int32, disabled bool) uuid.UUID {
	t.Helper()
	var id pgtype.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO applications (user_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port, base_domain_disabled)
		 VALUES ($1, $2, $3, 'https://github.com/acme/demo.git', 'main', 'dockerfile', $4, 80, $5, $6)
		 RETURNING id`,
		userID, serverID, name, domain, hostPort, disabled).Scan(&id)
	if err != nil {
		t.Fatalf("insert application %s: %v", name, err)
	}
	return uuid.UUID(id.Bytes)
}

// p6CreateRunningDeployment records a running deployment with a container id.
func p6CreateRunningDeployment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appID uuid.UUID, containerID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO deployments (application_id, kind, state, image_tag, container_id)
		 VALUES ($1, 'deploy', 'running', 'p6-e2e', $2)`,
		pgType(appID), containerID); err != nil {
		t.Fatalf("insert deployment for %s: %v", appID, err)
	}
}

// p6WaitForContainer returns the id of the managed proxy container on the
// node.
func p6WaitForContainer(t *testing.T, ctx context.Context, engine *agent.DockerClient) string {
	t.Helper()
	deadline := time.Now().Add(p6HTTPWait)
	for {
		listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		listed, err := engine.ListContainers(listCtx, true)
		cancel()
		if err == nil {
			for _, container := range listed {
				if container.GetName() == proxy.TraefikContainerName {
					return container.GetId()
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("container %q not found on the node", proxy.TraefikContainerName)
		}
		time.Sleep(p6PollInterval)
	}
}

// p6AssertTraefikBootstrap checks the production bindings, the read-only
// config mount and the native restart policy of a freshly bootstrapped node.
func p6AssertTraefikBootstrap(t *testing.T, traefikID string) {
	t.Helper()
	ports := p6Inspect(t, traefikID, "{{range $p, $conf := .NetworkSettings.Ports}}{{$p}}={{range $conf}}{{.HostIp}}:{{.HostPort}} {{end}}{{end}}")
	for _, want := range []string{"80/tcp", "443/tcp", "8080/tcp", "127.0.0.1:8080"} {
		if !strings.Contains(ports, want) {
			t.Fatalf("published ports = %q, want %s", ports, want)
		}
	}
	mounts := p6Inspect(t, traefikID, "{{json .Mounts}}")
	if !strings.Contains(mounts, `"/etc/traefik"`) || !strings.Contains(mounts, `"RW":false`) {
		t.Fatalf("config mount not read-only: %s", mounts)
	}
	if policy := p6Inspect(t, traefikID, "{{.HostConfig.RestartPolicy.Name}}"); policy != proxy.TraefikRestartPolicy {
		t.Fatalf("restart policy = %q, want %q", policy, proxy.TraefikRestartPolicy)
	}
}

// p6Inspect renders one docker inspect template for the container.
func p6Inspect(t *testing.T, containerID, format string) string {
	t.Helper()
	output, err := runDocker(context.Background(), "inspect", "--format", format, containerID)
	if err != nil {
		t.Fatalf("docker inspect %s: %v: %s", containerID, err, output)
	}
	return output
}

// p6ExpectHTTP polls a route through Traefik until it answers want (or fails).
func p6ExpectHTTP(t *testing.T, host string, want int) string {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	deadline := time.Now().Add(p6HTTPWait)
	var lastStatus int
	var lastBody string
	for {
		request, err := http.NewRequest(http.MethodGet, p6BaseURL, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Host = host
		response, err := client.Do(request)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
			_ = response.Body.Close()
			lastStatus, lastBody = response.StatusCode, string(body)
			if response.StatusCode == want || time.Now().After(deadline) {
				break
			}
		} else if time.Now().After(deadline) {
			break
		}
		time.Sleep(p6PollInterval)
	}
	if lastStatus != want {
		t.Fatalf("GET (Host %s) = %d, want %d (body %.200s)", host, lastStatus, want, lastBody)
	}
	return lastBody
}

// freeTCPPort asks the kernel for a free loopback port.
func freeTCPPort(t *testing.T) int32 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return int32(listener.Addr().(*net.TCPAddr).Port)
}

// runDocker runs one docker CLI command and returns its combined output.
func runDocker(ctx context.Context, args ...string) (string, error) {
	binary, err := exec.LookPath("docker")
	if err != nil {
		return "", err
	}
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// pgType converts a uuid.UUID for pgx parameters.
func pgType(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// p6CanonicalTempDir returns a temp directory with OS-level symlinks resolved
// (macOS /var -> /private/var), matching the agent's no-follow traversal from
// the trusted root.
func p6CanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return dir
}

// p6FindContainer returns the managed proxy container's id, or "" when absent.
func p6FindContainer(t *testing.T, ctx context.Context, engine *agent.DockerClient) string {
	t.Helper()
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	listed, err := engine.ListContainers(listCtx, true)
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	for _, container := range listed {
		if container.GetName() == proxy.TraefikContainerName {
			return container.GetId()
		}
	}
	return ""
}

// p6WaitForLog polls the proxy's logs until one line contains every keyword of
// one group (case-insensitive), returning the matching line.
func p6WaitForLog(t *testing.T, ctx context.Context, containerID string, groups ...[]string) string {
	t.Helper()
	deadline := time.Now().Add(p6HTTPWait)
	var logs string
	for {
		output, err := runDocker(ctx, "logs", containerID)
		if err == nil {
			logs = output
			for _, line := range strings.Split(output, "\n") {
				lower := strings.ToLower(line)
				for _, group := range groups {
					matches := true
					for _, keyword := range group {
						if !strings.Contains(lower, keyword) {
							matches = false
							break
						}
					}
					if matches {
						return line
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no rejection log within %s; traefik logs:\n%s", p6HTTPWait, logs)
		}
		time.Sleep(p6PollInterval)
	}
}

// p6AssertRepairsDrift proves the managed-container convergence rules (R5):
// owned drift (wrong mount, missing policy, wrong image) is repaired by
// recreation, and an unowned same-name container is never removed — the sync
// fails with an actionable conflict until the operator resolves it.
func p6AssertRepairsDrift(
	t *testing.T,
	ctx context.Context,
	engine *agent.DockerClient,
	svc *proxy.SyncService,
	serverID uuid.UUID,
	configDir, acmeDir string,
	traefikID *string,
	createdIDs *[]string,
	domain string,
) {
	t.Helper()

	baseRequest := func() *agentv1.CreateContainerRequest {
		return &agentv1.CreateContainerRequest{
			Image: proxy.TraefikImage,
			Name:  proxy.TraefikContainerName,
			Labels: map[string]string{
				"gotham.managed":              "true",
				"gotham.component":            "proxy",
				"gotham.proxy.config_dir":     configDir,
				"gotham.proxy.acme_dir":       acmeDir,
				"gotham.proxy.restart_policy": proxy.TraefikRestartPolicy,
			},
			Ports:   append([]string{}, proxy.TraefikPorts...),
			Volumes: []string{configDir + ":" + proxy.TraefikContainerConfigDir + ":ro", acmeDir + ":" + proxy.TraefikAcmeMount},
		}
	}
	replace := func(request *agentv1.CreateContainerRequest) {
		t.Helper()
		if err := engine.Remove(ctx, *traefikID); err != nil {
			t.Fatalf("remove current traefik: %v", err)
		}
		id, err := engine.RunImage(ctx, request)
		if err != nil {
			t.Fatalf("run drift fixture: %v", err)
		}
		*createdIDs = append(*createdIDs, id)
		*traefikID = id
	}

	drifts := map[string]func(*agentv1.CreateContainerRequest){
		"wrong mount": func(r *agentv1.CreateContainerRequest) {
			r.Volumes = []string{configDir + "/elsewhere:" + proxy.TraefikContainerConfigDir + ":ro", acmeDir + ":" + proxy.TraefikAcmeMount}
		},
		"missing policy label": func(r *agentv1.CreateContainerRequest) {
			delete(r.Labels, "gotham.proxy.restart_policy")
		},
		"wrong image": func(r *agentv1.CreateContainerRequest) {
			r.Image = p6NginxImage
		},
	}
	for name, mutate := range drifts {
		t.Run(name, func(t *testing.T) {
			request := baseRequest()
			mutate(request)
			replace(request)

			if err := p6Sync(svc, serverID); err != nil {
				t.Fatalf("sync must repair %s drift: %v", name, err)
			}
			newID := p6WaitForContainer(t, ctx, engine)
			if newID == *traefikID {
				t.Fatalf("%s drift was not repaired", name)
			}
			*createdIDs = append(*createdIDs, newID)
			*traefikID = newID
			p6AssertTraefikBootstrap(t, newID)
			p6ExpectHTTP(t, domain, http.StatusOK)
		})
	}

	t.Run("unowned same-name container", func(t *testing.T) {
		request := baseRequest()
		request.Labels = nil
		replace(request)

		err := p6Sync(svc, serverID)
		if !errors.Is(err, proxy.ErrConflict) {
			t.Fatalf("unowned container sync err = %v, want ErrConflict", err)
		}
		if current := p6FindContainer(t, ctx, engine); current != *traefikID {
			t.Fatalf("unowned container was replaced (%s -> %s)", *traefikID, current)
		}

		// The fixture belongs to this run: remove it and let the managed proxy
		// converge again.
		if err := engine.Remove(ctx, *traefikID); err != nil {
			t.Fatalf("remove unowned fixture: %v", err)
		}
		if err := p6Sync(svc, serverID); err != nil {
			t.Fatalf("restore after unowned conflict: %v", err)
		}
		newID := p6WaitForContainer(t, ctx, engine)
		*createdIDs = append(*createdIDs, newID)
		*traefikID = newID
		p6AssertTraefikBootstrap(t, newID)
		p6ExpectHTTP(t, domain, http.StatusOK)
	})
}
