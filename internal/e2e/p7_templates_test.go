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

const p7WordPressImage = "wordpress:6-apache"

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

func TestP7TemplateWordPressProduction(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newP7Harness(t, ctx)

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

	rendered, err := catalog.Render("wordpress", values)
	if err != nil {
		t.Fatalf("render wordpress: %v", err)
	}
	if len(rendered.Domains) != 1 || rendered.Domains[0].Domain != domain {
		t.Fatalf("domains = %+v, want the requested host", rendered.Domains)
	}
	if strings.Contains(rendered.ComposeYAML, "gotham.domain") {
		t.Fatalf("the rendered document still carries a gotham.domain label:\n%s", rendered.ComposeYAML)
	}
	for _, expected := range []string{
		"WORDPRESS_DB_PASSWORD: ${db_password}",
		"MYSQL_ROOT_PASSWORD: ${db_root_password}",
		"wordpress_data",
		"mysql_data",
	} {
		if !strings.Contains(rendered.ComposeYAML, expected) {
			t.Fatalf("the rendered document does not contain %q:\n%s", expected, rendered.ComposeYAML)
		}
	}
	for key, value := range rendered.Env {
		if strings.Contains(rendered.ComposeYAML, value) {
			t.Fatalf("secret %q leaked into the rendered document", key)
		}
	}
	if rendered.Env["db_password"] != values["db_password"] || rendered.Env["db_root_password"] != values["db_root_password"] {
		t.Fatalf("Env = %v, want both supplied secrets", rendered.Env)
	}

	pullCtx, pullCancel := context.WithTimeout(ctx, 10*time.Minute)
	for _, image := range []string{p7WordPressImage, "mysql:8.4"} {
		if _, err := h.engine.PullImage(pullCtx, image, "", ""); err != nil {
			pullCancel()
			t.Fatalf("pull %s: %v", image, err)
		}
	}
	pullCancel()

	created, err := h.compose.Create(ctx, h.userID, services.CreateRequest{
		Name:          "p7-wp-" + h.suffix,
		EnvironmentID: h.envID,
		ServerID:      h.serverID,
		ComposeYAML:   rendered.ComposeYAML,
		Env:           rendered.Env,
		Domains: []services.DomainInput{
			{Domain: domain, ComposeService: "wordpress", Port: 80, Protocol: "http"},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	project := services.ProjectName(created.ID)
	volumes := []string{project + "_wordpress_data", project + "_mysql_data"}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if _, err := h.compose.Delete(cleanupCtx, h.userID, created.ID); err != nil && !errors.Is(err, services.ErrNotFound) {
			t.Logf("cleanup service: %v", err)
		}
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

	p7Sync(t, ctx, h.proxy, h.serverID)
	if traefikID := p6FindContainer(t, ctx, h.engine); traefikID != "" {
		h.trackContainer(traefikID)
	}
	p7WaitForWordPress(t, domain)

	if _, err := h.compose.Delete(ctx, h.userID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

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
