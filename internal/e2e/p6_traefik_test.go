package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Phase 6 (BE-6.1) smoke tunables. The Traefik image is the one the control
// plane bootstraps, so the test proves the pinned tag actually serves.
const (
	p6NginxImage   = "nginx:1.23"
	p6PullTimeout  = 3 * time.Minute
	p6PingWait     = 90 * time.Second
	p6HTTPWait     = 30 * time.Second
	p6PollInterval = 500 * time.Millisecond
)

// TestP6TraefikFileProviderRoutesToNginx proves the BE-6.1 exit path against a
// real Docker daemon:
//
//  1. the control plane's generator renders the static + dynamic documents,
//  2. the control plane pushes them to a real node agent over mTLS
//     (servers.DialProxyClient → agent.ProxyServer.WriteProxyConfig),
//  3. Traefik runs from the generated static file and watches the generated
//     dynamic file, with an Nginx container as the backend on a user-defined
//     network, and
//  4. HTTP through Traefik reaches Nginx, and a generated router redirects
//     HTTP to HTTPS.
//
// Ports are disposable (kernel-assigned) and every container, network and temp
// directory is test-scoped, so the smoke never touches other services on the
// host. Gated by GOTHAM_E2E=1; missing Docker fails (never skips as green).
func TestP6TraefikFileProviderRoutesToNginx(t *testing.T) {
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
		t.Fatalf("docker daemon unreachable at %s: %v (start Docker and run: docker compose -f deploy/compose.dev.yml up -d)",
			e2eDockerSock(), err)
	}

	suffix := uuid.New().String()[:8]
	networkName := "gotham-p6-net-" + suffix
	nginxName := "gotham-p6-nginx-" + suffix
	traefikName := "gotham-p6-traefik-" + suffix
	plainHost := "plain-" + suffix + ".example.test"
	secureHost := "secure-" + suffix + ".example.test"

	// 1. A dedicated bridge network lets Traefik resolve the Nginx container by
	// name; the docker CLI (already used for e2e cleanup) creates it.
	if output, err := runDocker(ctx, "network", "create", networkName); err != nil {
		t.Fatalf("docker network create: %v: %s", err, output)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, name := range []string{traefikName, nginxName} {
			if output, err := runDocker(cleanupCtx, "rm", "-f", name); err != nil && !strings.Contains(output, "No such container") {
				t.Logf("cleanup: docker rm -f %s: %v: %s", name, err, output)
			}
		}
		if output, err := runDocker(cleanupCtx, "network", "rm", networkName); err != nil {
			t.Logf("cleanup: docker network rm %s: %v: %s", networkName, err, output)
		}
	})

	// 2. The Nginx backend on the shared network.
	pullCtx, pullCancel := context.WithTimeout(ctx, p6PullTimeout)
	if err := engine.PullImage(pullCtx, p6NginxImage); err != nil {
		pullCancel()
		t.Fatalf("pull %s: %v", p6NginxImage, err)
	}
	pullCancel()
	nginxID, err := engine.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:    p6NginxImage,
		Name:     nginxName,
		Networks: []string{networkName},
	})
	if err != nil {
		t.Fatalf("run nginx: %v", err)
	}
	t.Logf("docker %s at %s, nginx container %s", version, e2eDockerSock(), nginxID)

	// 3. The generated configuration: one BuildConfig route (secure router on
	// websecure plus the HTTP→HTTPS redirect router on web) and one plain
	// forwarding router, so the smoke can assert both 200 and the permanent redirect without a
	// certificate.
	appID := uuid.New()
	cfg := proxy.BuildConfig([]proxy.Route{{
		AppID:  appID,
		Domain: secureHost,
		Target: "http://" + nginxName + ":80",
	}})
	const plainRouter = "e2e-plain"
	cfg.Routers[plainRouter] = proxy.Router{
		Rule:        "Host(`" + plainHost + "`)",
		Service:     plainRouter,
		EntryPoints: []string{proxy.EntryPointWeb},
	}
	cfg.Services[plainRouter] = proxy.Service{
		LoadBalancer: proxy.LoadBalancer{Servers: []proxy.Backend{{URL: "http://" + nginxName + ":80"}}},
	}
	files, err := proxy.Generate(cfg, proxy.FormatYAML)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	// 4. The node agent's ProxyService (mTLS) writes the documents into the
	// directory Traefik mounts, and pings the published ping entrypoint.
	configDir := t.TempDir()
	acmeDir := configDir + "/acme"
	if err := os.MkdirAll(acmeDir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	httpPort := freeTCPPort(t)
	pingPort := freeTCPPort(t)
	pingURL := fmt.Sprintf("http://127.0.0.1:%d/ping", pingPort)

	nodeID := "p6-e2e-" + suffix
	proxyServer := agent.NewProxyServer(agent.ProxyServerConfig{
		Root:    configDir,
		PingURL: pingURL,
		Logger:  logger,
	})
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID,
		agent.WithProxyService(proxyServer))
	client, err := servers.DialProxyClient(ctx, agentAddr, authority,
		servers.WithDockerServerName(nodeID),
	)
	if err != nil {
		t.Fatalf("dial proxy service: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close proxy client: %v", err)
		}
	}()

	write := func(verify bool) *agentv1.WriteProxyConfigResponse {
		request := &agentv1.WriteProxyConfigRequest{Verify: verify}
		for _, file := range files {
			request.Files = append(request.Files, &agentv1.ProxyConfigFile{Path: file.Name, Content: file.Content})
		}
		writeCtx, writeCancel := context.WithTimeout(ctx, 30*time.Second)
		defer writeCancel()
		response, err := client.WriteProxyConfig(writeCtx, request)
		if err != nil {
			t.Fatalf("WriteProxyConfig(verify=%t): %v", verify, err)
		}
		return response
	}

	// The static file must exist before Traefik starts; the initial write is
	// unverified because the proxy is not up yet.
	initial := write(false)
	if len(initial.GetWritten()) != 2 {
		t.Fatalf("written = %v, want the static and dynamic documents", initial.GetWritten())
	}
	if initial.GetReloaded() {
		t.Fatal("unverified write reported a reload")
	}

	// 5. Traefik from the pinned image, mounts and published entrypoints.
	traefikPullCtx, traefikPullCancel := context.WithTimeout(ctx, p6PullTimeout)
	if err := engine.PullImage(traefikPullCtx, proxy.TraefikImage); err != nil {
		traefikPullCancel()
		t.Fatalf("pull %s: %v", proxy.TraefikImage, err)
	}
	traefikPullCancel()
	traefikID, err := engine.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:    proxy.TraefikImage,
		Name:     traefikName,
		Networks: []string{networkName},
		Ports:    []string{fmt.Sprintf("%d:80", httpPort), fmt.Sprintf("%d:8080", pingPort)},
		Volumes:  []string{configDir + ":" + proxy.TraefikContainerConfigDir, acmeDir + ":" + proxy.TraefikAcmeMount},
		Labels:   map[string]string{"gotham.e2e": "p6-traefik"},
	})
	if err != nil {
		t.Fatalf("run traefik: %v", err)
	}
	t.Logf("traefik container %s on %s:%d (ping %s)", traefikID, proxy.TraefikImage, httpPort, pingURL)

	// 6. The verified write doubles as the reload confirmation: poll until
	// Traefik answers its ping, then assert the documents were accepted.
	deadline := time.Now().Add(p6PingWait)
	var confirmed *agentv1.WriteProxyConfigResponse
	for {
		confirmed = write(true)
		if confirmed.GetReloaded() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("traefik did not confirm the reload within %s: %s", p6PingWait, confirmed.GetPingError())
		}
		time.Sleep(p6PollInterval)
	}
	if pingErr := confirmed.GetPingError(); pingErr != "" {
		t.Fatalf("confirmed reload carries a ping error: %s", pingErr)
	}

	// 7. HTTP through Traefik: the plain router forwards to Nginx, the
	// generated redirect router answers 301 to HTTPS.
	baseURL := fmt.Sprintf("http://127.0.0.1:%d/", httpPort)
	status, body := getThroughProxy(t, baseURL, plainHost, p6HTTPWait)
	if status != http.StatusOK {
		t.Fatalf("GET %s (Host %s) = %d, body %q; want 200", baseURL, plainHost, status, body)
	}
	if !strings.Contains(body, "Welcome to nginx") {
		t.Fatalf("body through Traefik = %q, want the nginx welcome page", body)
	}

	status, location := redirectThroughProxy(t, baseURL, secureHost)
	if status != http.StatusMovedPermanently {
		t.Fatalf("GET (Host %s) = %d, want 301 (Traefik's permanent redirectScheme)", secureHost, status)
	}
	if want := "https://" + secureHost + "/"; location != want {
		t.Fatalf("Location = %q, want %q", location, want)
	}
}

// getThroughProxy polls the proxy until it answers, then returns the status
// and a bounded body. Traefik needs a moment after its container starts.
func getThroughProxy(t *testing.T, url, host string, wait time.Duration) (int, string) {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	deadline := time.Now().Add(wait)
	var lastStatus int
	var lastBody string
	for {
		request, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Host = host
		response, err := client.Do(request)
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("no HTTP response from %s (Host %s) within %s: %v", url, host, wait, err)
			}
			time.Sleep(p6PollInterval)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		lastStatus, lastBody = response.StatusCode, string(body)
		if response.StatusCode == http.StatusOK || time.Now().After(deadline) {
			return lastStatus, lastBody
		}
		time.Sleep(p6PollInterval)
	}
}

// redirectThroughProxy requests url through the proxy and returns the status
// and Location without following the redirect.
func redirectThroughProxy(t *testing.T, url, host string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Host = host
	response, err := (&http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}).Do(request)
	if err != nil {
		t.Fatalf("GET %s (Host %s): %v", url, host, err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, response.Header.Get("Location")
}

// freeTCPPort asks the kernel for a free port on the loopback interface.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
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
