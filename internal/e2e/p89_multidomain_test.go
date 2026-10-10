package e2e

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// JUS-89 production acceptance (gated by GOTHAM_E2E=1): two domains of one
// application route through a real Traefik, and removing one domain removes
// only its routers while the survivor keeps serving.
//
// The test drives the public generator surface (BuildConfig + Generate, the
// same bytes a proxy sync pushes) into a real traefik:v3.7 container with the
// file provider, behind a backend nginx on a dedicated Docker network.
// Requests carry explicit Host headers, so no DNS or hosts entries are
// needed. The one-app-two-routes grouping itself is covered by unit tests
// (TestRoutesForServerEmitsOneRoutePerDomain); this test proves Traefik
// serves what the generator emits, including the alias router naming and the
// shared service.
func TestP89MultiDomainTraefik(t *testing.T) {
	requireE2E(t)
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker CLI is required: %v", err)
	}

	suffix := strings.ToLower(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	network := "p89-" + suffix
	backend := "p89-web-" + suffix
	gateway := "p89-traefik-" + suffix
	hostPort := "18089"
	dynamicDir := t.TempDir()

	docker := func(args ...string) {
		t.Helper()
		cmd := exec.Command("docker", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	t.Cleanup(func() {
		for _, args := range [][]string{
			{"rm", "-f", gateway},
			{"rm", "-f", backend},
			{"network", "rm", network},
		} {
			_ = exec.Command("docker", args...).Run()
		}
	})

	docker("network", "create", network)
	docker("run", "-d", "--name", backend, "--network", network, p6NginxImage)
	docker("run", "-d", "--name", gateway, "--network", network,
		"-p", "127.0.0.1:"+hostPort+":80",
		"-v", dynamicDir+":/etc/traefik/dynamic",
		"traefik:v3.7",
		"--entrypoints.web.address=:80",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true")

	appID := uuid.New()
	primary, alias := "p89a.example.com", "www.p89a.example.com"
	target := "http://" + backend + ":80"
	routes := []proxy.Route{
		{AppID: appID, Domain: primary, Target: target},
		{AppID: appID, Name: proxy.AliasRouteName(appID, alias), Domain: alias, Target: target, Service: "app-" + appID.String()},
	}
	writeDynamic := func(active []proxy.Route) {
		t.Helper()
		files, err := proxy.Generate(proxy.BuildConfig(active, nil, nil, "", ""), proxy.FormatYAML)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		for _, file := range files {
			if !strings.HasPrefix(file.Name, "dynamic/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dynamicDir, filepath.Base(file.Name)), file.Content, 0o644); err != nil {
				t.Fatalf("write dynamic config: %v", err)
			}
		}
	}
	get := func(host string) int {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+hostPort+"/", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		request.Host = host
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return -1
		}
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	waitFor := func(host string, want int) {
		t.Helper()
		deadline := time.Now().Add(p6HTTPWait)
		for {
			if got := get(host); got == want {
				return
			} else if time.Now().After(deadline) {
				t.Fatalf("host %q never answered %d (last %d)", host, want, got)
			}
			time.Sleep(p6PollInterval)
		}
	}

	// Both domains route through their own routers over the shared service.
	writeDynamic(routes)
	waitFor(primary, http.StatusOK)
	waitFor(alias, http.StatusOK)

	// Removing the alias removes only its routers: the survivor keeps its
	// 200 while the removed host falls through to Traefik's 404.
	writeDynamic(routes[:1])
	waitFor(primary, http.StatusOK)
	waitFor(alias, http.StatusNotFound)

	// The survivor's configuration is byte-identical to a single-domain app.
	single, err := proxy.Generate(proxy.BuildConfig(routes[:1], nil, nil, "", ""), proxy.FormatYAML)
	if err != nil {
		t.Fatalf("generate single: %v", err)
	}
	legacy, err := proxy.Generate(proxy.BuildConfig([]proxy.Route{
		{AppID: appID, Domain: primary, Target: target},
	}, nil, nil, "", ""), proxy.FormatYAML)
	if err != nil {
		t.Fatalf("generate legacy: %v", err)
	}
	for i := range single {
		if string(single[i].Content) != string(legacy[i].Content) {
			t.Fatalf("survivor %q differs from a single-domain app:\n%s\n---\n%s",
				single[i].Name, single[i].Content, legacy[i].Content)
		}
	}
}
