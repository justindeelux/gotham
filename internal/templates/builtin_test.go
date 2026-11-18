package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	builtin "github.com/justindeelux/gotham/templates"

	"github.com/justindeelux/gotham/composeguard"
	"github.com/justindeelux/gotham/internal/services"
)

// builtinSamples holds render values that the generated ones below cannot
// satisfy. A template that needs such a value gets its own per-batch test
// file (builtin_samples_<batch>_test.go) adding an entry here from func init(),
// so adding a template never edits this file.
var builtinSamples = map[string]map[string]any{
	"wordpress": {
		"domain":           "blog.example.test",
		"db_password":      "wp$secret",
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

func TestBuiltinCatalog(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	seen := make(map[string]bool, len(catalog.List()))
	for _, template := range catalog.List() {
		if seen[template.Slug] {
			t.Errorf("duplicate template slug %q", template.Slug)
		}
		seen[template.Slug] = true
		if template.Name == "" || template.Icon == "" || template.Description == "" || len(template.Fields) == 0 {
			t.Errorf("template %s is missing metadata: %+v", template.Slug, template)
		}
		if template.Homepage == "" || len(template.Category) == 0 {
			t.Errorf("template %s is missing its homepage or category: %+v", template.Slug, template)
		}
	}
	for _, slug := range []string{"n8n", "nextcloud", "uptime-kuma", "wordpress"} {
		if !seen[slug] {
			t.Errorf("template %s missing from the catalog", slug)
		}
	}
	for slug := range builtinSamples {
		if _, ok := catalog.Get(slug); !ok {
			t.Errorf("template %s missing from the catalog", slug)
		}
	}
}

func TestBuiltinRender(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, template := range catalog.List() {
		t.Run(template.Slug, func(t *testing.T) {
			values := builtinValues(template)
			result, err := catalog.Render(template.Slug, values)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if len(result.Spec.Domains) != 0 {
				t.Errorf("compose labels still declare domains: %+v", result.Spec.Domains)
			}
			routes := make(map[string]services.DomainRoute, len(result.Domains))
			for _, route := range result.Domains {
				if route.Port == 0 {
					t.Errorf("route = %+v, want a container port", route)
				}
				routes[route.Service] = route
			}
			wantRoutes := 0
			for _, route := range template.Routes {
				domain, _ := values[route.DomainField].(string)
				if domain == "" {
					continue
				}
				wantRoutes++
				got, ok := routes[route.Service]
				if !ok {
					t.Errorf("route for %s is missing, want domain %q", route.Service, domain)
					continue
				}
				if got.Domain != domain {
					t.Errorf("route domain = %q, want %q", got.Domain, domain)
				}
			}
			if len(result.Domains) != wantRoutes {
				t.Errorf("domains = %+v, want %d routes", result.Domains, wantRoutes)
			}
			for _, volume := range result.Spec.NamedVolumes {
				if strings.HasPrefix(volume, services.ReservedVolumePrefix) {
					t.Errorf("template uses the managed database volume namespace: %s", volume)
				}
			}
			for _, mount := range result.Spec.Mounts {
				if !mount.Named {
					t.Errorf("mount %+v is not a named volume", mount)
				}
			}
			if _, err := services.Render(result.ComposeYAML, result.Env); err != nil {
				t.Fatalf("services.Render: %v", err)
			}
			for key, value := range result.Env {
				if strings.Contains(result.ComposeYAML, value) {
					t.Errorf("secret %q leaked into the rendered document", key)
				}
				if !strings.Contains(result.ComposeYAML, "${"+key+"}") {
					t.Errorf("secret %q is not referenced as ${%s}", key, key)
				}
			}
		})
	}
}

// builtinValues returns the render values for a template: a sample entry wins,
// otherwise the field default, otherwise a value derived from the field
// metadata. Sample keys are passed through so a typo fails Render.
func builtinValues(template Template) map[string]any {
	sample := builtinSamples[template.Slug]
	values := make(map[string]any, len(template.Fields)+len(sample))
	for key, value := range sample {
		values[key] = value
	}
	for _, field := range template.Fields {
		if _, ok := values[field.Key]; ok {
			continue
		}
		if field.Default != nil {
			values[field.Key] = *field.Default
			continue
		}
		values[field.Key] = generatedValue(field)
	}
	return values
}

func generatedValue(field Field) string {
	switch field.Type {
	case FieldText, FieldSecret:
		fixed := "app.example.test"
		if field.Type == FieldSecret {
			fixed = "sample-secret"
		}
		for _, candidate := range []string{fixed, "app", "a"} {
			if _, err := field.check(candidate); err == nil {
				return candidate
			}
		}
		return fixed
	case FieldNumber:
		if field.Min != nil {
			return strconv.FormatInt(*field.Min, 10)
		}
		return "0"
	case FieldSelect:
		return field.Options[0]
	case FieldBool:
		return "true"
	}
	return ""
}

func TestBuiltinExpectedValues(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}

	t.Run("wordpress", func(t *testing.T) {
		result, err := catalog.Render("wordpress", builtinSamples["wordpress"])
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if got := strings.Join(result.Spec.Services, ","); got != "mysql,wordpress" {
			t.Errorf("services = %s", got)
		}
		if route := result.Domains[0]; route.Service != "wordpress" || route.Port != 80 {
			t.Errorf("route = %+v", route)
		}
		if strings.Contains(result.ComposeYAML, "gotham.domain") {
			t.Errorf("the rendered document still carries a gotham.domain label:\n%s", result.ComposeYAML)
		}
		if got := strings.Join(result.Spec.NamedVolumes, ","); got != "mysql_data,wordpress_data" {
			t.Errorf("volumes = %s", got)
		}
		doc := decodeMap(t, result.ComposeYAML)
		if got := envValue(t, doc, "wordpress", "WORDPRESS_DB_PASSWORD"); got != "${db_password}" {
			t.Errorf("WORDPRESS_DB_PASSWORD = %q, want the reference", got)
		}
		if got := envValue(t, doc, "mysql", "MYSQL_ROOT_PASSWORD"); got != "${db_root_password}" {
			t.Errorf("MYSQL_ROOT_PASSWORD = %q, want the reference", got)
		}
		if got := envValue(t, doc, "mysql", "MYSQL_DATABASE"); got != "wordpress" {
			t.Errorf("MYSQL_DATABASE = %q", got)
		}
		if got := result.Env["db_password"]; got != "wp$secret" {
			t.Errorf("Env[db_password] = %q", got)
		}
		if got := result.Env["db_root_password"]; got != "root-secret" {
			t.Errorf("Env[db_root_password] = %q", got)
		}
	})

	t.Run("nextcloud", func(t *testing.T) {
		result, err := catalog.Render("nextcloud", builtinSamples["nextcloud"])
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if got := strings.Join(result.Spec.Services, ","); got != "nextcloud,postgres,redis" {
			t.Errorf("services = %s", got)
		}
		doc := decodeMap(t, result.ComposeYAML)
		if got := envValue(t, doc, "nextcloud", "NEXTCLOUD_ADMIN_USER"); got != "admin" {
			t.Errorf("NEXTCLOUD_ADMIN_USER = %q", got)
		}
		if got := envValue(t, doc, "nextcloud", "NEXTCLOUD_TRUSTED_DOMAINS"); got != "cloud.example.test" {
			t.Errorf("NEXTCLOUD_TRUSTED_DOMAINS = %q", got)
		}
		command, ok := serviceMap(t, doc, "redis")["command"].([]any)
		if !ok || len(command) != 3 || command[2] != "${redis_password}" {
			t.Errorf("redis command = %v, want the password reference", serviceMap(t, doc, "redis")["command"])
		}
		if got := result.Env["redis_password"]; got != "redis-secret" {
			t.Errorf("Env[redis_password] = %q", got)
		}
	})

	t.Run("n8n", func(t *testing.T) {
		result, err := catalog.Render("n8n", builtinSamples["n8n"])
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if route := result.Domains[0]; route.Port != 5678 {
			t.Errorf("route = %+v", route)
		}
		doc := decodeMap(t, result.ComposeYAML)
		for key, want := range map[string]string{
			"N8N_HOST":                "${GOTHAM_DOMAIN:-localhost}",
			"WEBHOOK_URL":             "https://${GOTHAM_DOMAIN:-localhost}/",
			"N8N_LOG_LEVEL":           "info",
			"N8N_DIAGNOSTICS_ENABLED": "true",
			"EXECUTIONS_DATA_MAX_AGE": "336",
			"GENERIC_TIMEZONE":        "UTC",
			"N8N_ENCRYPTION_KEY":      "${encryption_key}",
		} {
			if got := envValue(t, doc, "n8n", key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		env := map[string]string{"GOTHAM_DOMAIN": ""}
		for key, value := range result.Env {
			env[key] = value
		}
		deployed, err := services.Render(result.ComposeYAML, env)
		if err != nil {
			t.Fatalf("services.Render without a domain: %v", err)
		}
		final := decodeMap(t, deployed.ComposeYAML)
		if got := envValue(t, final, "n8n", "N8N_HOST"); got != "localhost" {
			t.Errorf("N8N_HOST without a domain = %q, want the localhost fallback", got)
		}
		if got := envValue(t, final, "n8n", "WEBHOOK_URL"); got != "https://localhost/" {
			t.Errorf("WEBHOOK_URL without a domain = %q, want the localhost fallback", got)
		}
		if got := result.Env["encryption_key"]; got != "enc-secret" {
			t.Errorf("Env[encryption_key] = %q", got)
		}
	})

	t.Run("uptime-kuma", func(t *testing.T) {
		result, err := catalog.Render("uptime-kuma", builtinSamples["uptime-kuma"])
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if route := result.Domains[0]; route.Service != "uptime-kuma" || route.Port != 3001 {
			t.Errorf("route = %+v", route)
		}
		doc := decodeMap(t, result.ComposeYAML)
		if got := envValue(t, doc, "uptime-kuma", "TZ"); got != "UTC" {
			t.Errorf("TZ = %q", got)
		}
	})
}

func TestBuiltinOptionalDomain(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, slug := range []string{"wordpress", "n8n", "uptime-kuma"} {
		t.Run(slug, func(t *testing.T) {
			values := map[string]any{}
			for key, value := range builtinSamples[slug] {
				if key != "domain" {
					values[key] = value
				}
			}
			result, err := catalog.Render(slug, values)
			if err != nil {
				t.Fatalf("Render without a domain: %v", err)
			}
			if len(result.Domains) != 0 {
				t.Errorf("domains = %+v, want none", result.Domains)
			}
			if _, err := services.Render(result.ComposeYAML, result.Env); err != nil {
				t.Fatalf("services.Render: %v", err)
			}
		})
	}
	t.Run("nextcloud stays required", func(t *testing.T) {
		values := map[string]any{}
		for key, value := range builtinSamples["nextcloud"] {
			if key != "domain" {
				values[key] = value
			}
		}
		if _, err := catalog.Render("nextcloud", values); err == nil {
			t.Fatal("Render without a domain succeeded, want a required-field error")
		}
	})
}

func TestBuiltinSecretFieldsAreNotBakedIn(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, template := range catalog.List() {
		for _, field := range template.Fields {
			if field.Type == FieldSecret && (field.Default != nil || !field.Required) {
				t.Errorf("%s: secret field %q has a default or is optional", template.Slug, field.Key)
			}
		}
	}
}

func TestBuiltinGeneratableFieldsAreTextOrSecret(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, template := range catalog.List() {
		for _, field := range template.Fields {
			if field.Generate && field.Type != FieldSecret && field.Type != FieldText {
				t.Errorf("%s: generatable field %q is not text or secret", template.Slug, field.Key)
			}
		}
	}
}

func TestNewDefaultService(t *testing.T) {
	t.Setenv(services.FeatureEnv, "")
	svc := NewDefaultService(nil)
	if svc == nil {
		t.Fatal("NewDefaultService = nil")
	}
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	if got, want := len(svc.List()), len(catalog.List()); got != want {
		t.Fatalf("catalog = %d templates, want %d", got, want)
	}
	if _, err := svc.Render("wordpress", builtinSamples["wordpress"]); err != nil {
		t.Fatalf("Render(wordpress): %v", err)
	}

	t.Setenv(services.FeatureEnv, "false")
	if svc := NewDefaultService(nil); svc != nil {
		t.Fatal("NewDefaultService is not nil with FEATURE_SERVICES=false")
	}
}

func TestBuiltinTemplatesPassServiceConfinement(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for slug, values := range builtinSamples {
		t.Run(slug, func(t *testing.T) {
			result, err := catalog.Render(slug, values)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			rendered, _, err := services.RenderService(result.ComposeYAML, result.Env, nil)
			if err != nil {
				t.Fatalf("RenderService: %v", err)
			}
			if err := composeguard.CheckNoInterpolation(rendered.ComposeYAML); err != nil {
				t.Fatalf("CheckNoInterpolation: %v", err)
			}
			opts := composeguard.Options{
				AppID:              uuid.NewString(),
				ManagedRoot:        t.TempDir(),
				ServiceRouteLabels: true,
			}
			if err := composeguard.Validate(rendered.ComposeYAML, opts); err != nil {
				t.Fatalf("the built-in template does not pass the service guard: %v", err)
			}
		})
	}
}
