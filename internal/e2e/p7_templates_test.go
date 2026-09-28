package e2e

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/templates"
)

// Phase 7 (BE-7.2) production acceptance: the one-click template engine. The
// test renders the WordPress template through the production catalog, creates
// a service from the rendered document through the real BE-7.1 path
// (store -> services.Service -> mTLS agent ComposeServer -> `docker compose`
// CLI -> Docker), and proves the declared domain is served through the real
// Traefik on the node.
//
// The plan's verify line: "render WordPress from sample input -> compose
// contains the right domain/env; 1-click deploy succeeds".
//
// Gated by GOTHAM_E2E=1 like the rest of the Phase 7 acceptance.
const p7WordPressImage = "wordpress:6-apache"

// p7TemplateSamples is one valid input per built-in template, mirroring the
// catalog tests: required fields only, so the defaults are exercised too.
var p7TemplateSamples = map[string]map[string]any{
	"wordpress": {
		"domain":           "blog.example.test",
		"db_password":      "wp-secret",
		"db_root_password": "root-secret",
	},
	"nextcloud": {
		"domain":         "cloud.example.test",
		"admin_password": "admin-secret",
		"db_password":    "db-secret",
		"redis_password": "redis-secret",
	},
	"n8n": {
		"domain":         "n8n.example.test",
		"encryption_key": "enc-secret",
	},
	"uptime-kuma": {
		"domain": "status.example.test",
	},
}

// TestP7TemplateCatalogComposeConfig proves every shipped template renders to
// a document the node's own compose CLI accepts, without pulling any image:
// `docker compose config` performs the same schema validation the agent runs
// before `compose up`. The WordPress end-to-end deploy is the sibling test
// below; this one covers the templates no CI run can afford to start.
func TestP7TemplateCatalogComposeConfig(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker CLI not found: %v", err)
	}
	t.Setenv(services.FeatureEnv, "")
	catalog := templates.NewDefaultService(testLogger(t))
	if catalog == nil {
		t.Fatal("the built-in template catalog failed to load")
	}
	for slug, values := range p7TemplateSamples {
		t.Run(slug, func(t *testing.T) {
			rendered, err := catalog.Render(slug, values)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			// Secret values travel in Env; the document references them, so
			// the compose CLI needs the environment to resolve them (exactly
			// as the services pipeline supplies it at deploy time).
			for key, value := range rendered.Env {
				if strings.Contains(rendered.ComposeYAML, value) {
					t.Fatalf("secret %q leaked into the rendered document", key)
				}
				t.Setenv(key, value)
			}
			path := filepath.Join(t.TempDir(), "compose.yaml")
			if err := os.WriteFile(path, []byte(rendered.ComposeYAML), 0o600); err != nil {
				t.Fatalf("write document: %v", err)
			}
			if output, err := runDocker(ctx, "compose", "-f", path, "config", "--quiet"); err != nil {
				t.Fatalf("docker compose config rejected the document: %v: %s", err, output)
			}
		})
	}
}

// TestP7TemplateWordPressProduction is gated by GOTHAM_E2E=1.
func TestP7TemplateWordPressProduction(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newP7Harness(t, ctx)

	// The production constructor the HTTP wiring uses (FEATURE_SERVICES is
	// forced on, whatever the ambient environment says).
	t.Setenv(services.FeatureEnv, "")
	catalog := templates.NewDefaultService(h.logger)
	if catalog == nil {
		t.Fatal("the built-in template catalog failed to load")
	}

	domain := "p7-wp-" + h.suffix + ".example.test"
	values := map[string]any{
		"domain":           domain,
		"db_password":      "wp-" + h.suffix + "$secret",
		"db_root_password": "root-" + h.suffix,
	}

	// 1. The render: the document carries the requested domain and the
	// template's named volumes, references the secrets as ${field}, and has
	// already passed the BE-7.1 schema check.
	rendered, err := catalog.Render("wordpress", values)
	if err != nil {
		t.Fatalf("render wordpress: %v", err)
	}
	if len(rendered.Spec.Domains) != 1 || rendered.Spec.Domains[0].Domain != domain {
		t.Fatalf("domains = %+v, want the requested host", rendered.Spec.Domains)
	}
	for _, expected := range []string{
		"gotham.domain: " + domain,
		"WORDPRESS_DB_PASSWORD: ${db_password}",
		"MYSQL_ROOT_PASSWORD: ${db_root_password}",
		"wordpress_data",
		"mysql_data",
	} {
		if !strings.Contains(rendered.ComposeYAML, expected) {
			t.Fatalf("the rendered document does not contain %q:\n%s", expected, rendered.ComposeYAML)
		}
	}
	// The secrets travel in the returned environment, never in the document.
	for key, value := range rendered.Env {
		if strings.Contains(rendered.ComposeYAML, value) {
			t.Fatalf("secret %q leaked into the rendered document", key)
		}
	}
	if rendered.Env["db_password"] != values["db_password"] || rendered.Env["db_root_password"] != values["db_root_password"] {
		t.Fatalf("Env = %v, want both supplied secrets", rendered.Env)
	}

	// 2. Pull first with a generous timeout: the deploy below runs compose up.
	pullCtx, pullCancel := context.WithTimeout(ctx, 10*time.Minute)
	for _, image := range []string{p7WordPressImage, "mysql:8.4"} {
		if err := h.engine.PullImage(pullCtx, image); err != nil {
			pullCancel()
			t.Fatalf("pull %s: %v", image, err)
		}
	}
	pullCancel()

	// 3. The gallery flow: a service is created from the rendered document and
	// its environment, then deployed through the existing services surface.
	created, err := h.compose.Create(ctx, h.userID, services.CreateRequest{
		Name:        "p7-wp-" + h.suffix,
		ServerID:    h.serverID,
		ComposeYAML: rendered.ComposeYAML,
		Env:         rendered.Env,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	project := services.ProjectName(created.ID)
	volumes := []string{project + "_wordpress_data", project + "_mysql_data"}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		// A failure between Deploy and Delete leaves the project up; stop it
		// first so the volumes can actually be removed. A service the test
		// already deleted is not an error.
		if err := h.compose.Delete(cleanupCtx, h.userID, created.ID); err != nil && !errors.Is(err, services.ErrNotFound) {
			t.Logf("cleanup service: %v", err)
		}
		// The project is down by now; remove the fixture's own volumes so
		// nothing leaks.
		for _, volume := range volumes {
			if output, err := runDocker(cleanupCtx, "volume", "rm", volume); err != nil {
				t.Logf("cleanup volume %s: %v: %s", volume, err, output)
			}
		}
	})

	deployed, deploy, err := h.compose.Deploy(ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if deployed.Status != services.StatusRunning || deploy.State != services.DeployRunning {
		t.Fatalf("deployed = %+v, deploy = %+v", deployed, deploy)
	}
	containers := p7WaitForRunning(t, ctx, h.compose, h.userID, created.ID)
	t.Logf("containers: %+v", containers)

	// 4. The domain label becomes a real Traefik route on this node and the
	// WordPress instance answers there.
	p7Sync(t, ctx, h.proxy, h.serverID)
	if traefikID := p6FindContainer(t, ctx, h.engine); traefikID != "" {
		h.trackContainer(traefikID)
	}
	p7WaitForWordPress(t, domain)

	// 5. Delete stops the project; the cleanup above removes the volumes.
	if err := h.compose.Delete(ctx, h.userID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// p7WaitForWordPress polls the routed domain until it proves WordPress is the
// backend: the root request either serves the installer markup directly (200
// with the WordPress page) or redirects to the WordPress installer
// (/wp-admin/install.php), whose page must then carry the WordPress markup.
// A bare 302 to anywhere else, or any other status - including the 500 of an
// unreachable database - is retried until the deadline.
func p7WaitForWordPress(t *testing.T, domain string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	var lastStatus int
	var lastBody string
	for {
		status, location, body := p7GetRouted(t, domain, "/")
		lastStatus, lastBody = status, body
		switch {
		case status == http.StatusOK && strings.Contains(body, "WordPress"):
			t.Logf("the WordPress domain served the installer page (200) through Traefik")
			return
		case status == http.StatusFound && strings.Contains(location, "/wp-admin/install.php"):
			installerStatus, _, installerBody := p7GetRouted(t, domain, "/wp-admin/install.php")
			lastStatus, lastBody = installerStatus, installerBody
			if installerStatus == http.StatusOK && strings.Contains(installerBody, "WordPress") {
				t.Logf("the WordPress domain redirected to the installer (%s) and served it (200) through Traefik", location)
				return
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("the WordPress domain never proved the installer: last status %d (body %.300s)", lastStatus, lastBody)
}

// p7GetRouted performs one GET through the local Traefik gateway with the
// domain as the Host header, without following redirects. It returns the
// status, the Location header and the bounded body.
func p7GetRouted(t *testing.T, domain, path string) (int, string, string) {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequest(http.MethodGet, p6BaseURL+strings.TrimPrefix(path, "/"), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Host = domain
	response, err := client.Do(request)
	if err != nil {
		return 0, "", ""
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	_ = response.Body.Close()
	return response.StatusCode, response.Header.Get("Location"), string(body)
}
