package templates

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	builtin "github.com/justindeelux/gotham/templates"

	"github.com/justindeelux/gotham/composeguard"
	"github.com/justindeelux/gotham/internal/services"
)

func init() {
	builtinSamples["three-proxy"] = map[string]any{
		"http_port":  3128,
		"socks_port": 1080,
		"username":   "gotham",
		"password":   "proxy-secret",
	}
	builtinSamples["gost"] = map[string]any{
		"http_port":  8080,
		"socks_port": 1080,
		"username":   "gotham",
		"password":   "proxy-secret",
	}
	builtinSamples["mitmproxy"] = map[string]any{
		"proxy_port": 8080,
		"username":   "gotham",
		"password":   "web-secret",
		"domain":     "mitm.example.test",
	}
	builtinSamples["squid"] = map[string]any{
		"http_port": 3128,
		"username":  "gotham",
		"password":  "proxy-secret",
	}
	builtinSamples["dante"] = map[string]any{
		"socks_port": 1080,
		"username":   "gotham",
		"password":   "proxy-secret",
	}
}

// TestProxyTemplateDefaults renders each proxy template with its defaults and
// checks it passes the service guard: the published host ports stay out of the
// privileged band and a secret is always generated.
func TestProxyTemplateDefaults(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, slug := range []string{"three-proxy", "gost", "mitmproxy", "squid", "dante"} {
		t.Run(slug, func(t *testing.T) {
			result, err := catalog.Render(slug, nil)
			if err != nil {
				t.Fatalf("Render with defaults: %v", err)
			}
			if len(result.Env) == 0 {
				t.Fatal("the template renders no secret, so it can run unauthenticated")
			}
			rendered, _, err := services.RenderService(result.ComposeYAML, result.Env, nil)
			if err != nil {
				t.Fatalf("RenderService: %v", err)
			}
			if err := composeguard.Validate(rendered.ComposeYAML, composeguard.Options{
				AppID:              uuid.NewString(),
				ManagedRoot:        t.TempDir(),
				ServiceRouteLabels: true,
			}); err != nil {
				t.Fatalf("the template does not pass the service guard: %v", err)
			}
		})
	}
}

// TestProxyTemplateAuthByDefault pins that every proxy ships with credentials
// wired into its value positions, never as an open proxy.
func TestProxyTemplateAuthByDefault(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for slug, want := range map[string]string{
		"three-proxy": "PROXY_PASSWORD: ${password}",
		"gost":        "http://gotham:${password}@:8080",
		"mitmproxy":   "proxyauth=gotham:${password}",
		"squid":       "PROXY_PASSWORD: ${password}",
		"dante":       "PROXY_PASSWORD: ${password}",
	} {
		t.Run(slug, func(t *testing.T) {
			result, err := catalog.Render(slug, nil)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if !strings.Contains(result.ComposeYAML, want) {
				t.Errorf("compose document does not contain %q:\n%s", want, result.ComposeYAML)
			}
		})
	}
}
