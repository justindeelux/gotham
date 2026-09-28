package templates

import (
	"strings"
	"testing"

	builtin "github.com/justindeelux/gotham/templates"

	"github.com/justindeelux/gotham/internal/services"
)

// builtinSamples is one valid input per built-in template, restricted to its
// required fields so the defaults are exercised as well.
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

// TestBuiltinCatalog proves the shipped templates load, validate and list.
func TestBuiltinCatalog(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	slugs := make([]string, 0, len(catalog.List()))
	for _, template := range catalog.List() {
		slugs = append(slugs, template.Slug)
		if template.Name == "" || template.Icon == "" || template.Description == "" || len(template.Fields) == 0 {
			t.Errorf("template %s is missing metadata: %+v", template.Slug, template)
		}
	}
	want := "n8n,nextcloud,uptime-kuma,wordpress"
	if got := strings.Join(slugs, ","); got != want {
		t.Fatalf("catalog = %s, want %s", got, want)
	}
	for slug := range builtinSamples {
		if _, ok := catalog.Get(slug); !ok {
			t.Errorf("template %s missing from the catalog", slug)
		}
	}
}

// TestBuiltinRender renders every shipped template from sample input and
// proves the document is deployable through the BE-7.1 path: services.Parse
// accepts it, the services interpolation leaves it intact and the routing
// label maps to the requested domain.
func TestBuiltinRender(t *testing.T) {
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
			if len(result.Spec.Domains) != 1 {
				t.Fatalf("domains = %+v, want exactly one route", result.Spec.Domains)
			}
			route := result.Spec.Domains[0]
			if route.Domain != values["domain"] {
				t.Errorf("route domain = %q, want %v", route.Domain, values["domain"])
			}
			if route.Port == 0 {
				t.Errorf("route = %+v, want a container port", route)
			}
			for _, volume := range result.Spec.NamedVolumes {
				if strings.HasPrefix(volume, services.ReservedVolumePrefix) {
					t.Errorf("template uses the managed database volume namespace: %s", volume)
				}
			}
			if len(result.Spec.NamedVolumes) == 0 {
				t.Error("template declares no named volume; state would not survive a stop")
			}
			for _, mount := range result.Spec.Mounts {
				if !mount.Named {
					t.Errorf("mount %+v is not a named volume", mount)
				}
			}
			// The deploy path's own render must accept the document with the
			// returned environment and resolve every secret reference.
			if _, err := services.Render(result.ComposeYAML, result.Env); err != nil {
				t.Fatalf("services.Render: %v", err)
			}
			// No secret value may appear in the document: every secret field
			// is a ${field} reference and its value travels in Env.
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

// TestBuiltinExpectedValues pins the observable values the plan's acceptance
// names: the domain and the environment a sample render produces.
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
		if route := result.Spec.Domains[0]; route.Service != "wordpress" || route.Port != 80 {
			t.Errorf("route = %+v", route)
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
		if route := result.Spec.Domains[0]; route.Port != 5678 {
			t.Errorf("route = %+v", route)
		}
		doc := decodeMap(t, result.ComposeYAML)
		for key, want := range map[string]string{
			"N8N_HOST":                "n8n.example.test",
			"WEBHOOK_URL":             "https://n8n.example.test/",
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
		if got := result.Env["encryption_key"]; got != "enc-secret" {
			t.Errorf("Env[encryption_key] = %q", got)
		}
	})

	t.Run("uptime-kuma", func(t *testing.T) {
		result, err := catalog.Render("uptime-kuma", builtinSamples["uptime-kuma"])
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if route := result.Spec.Domains[0]; route.Service != "uptime-kuma" || route.Port != 3001 {
			t.Errorf("route = %+v", route)
		}
		doc := decodeMap(t, result.ComposeYAML)
		if got := envValue(t, doc, "uptime-kuma", "TZ"); got != "UTC" {
			t.Errorf("TZ = %q", got)
		}
	})
}

// TestBuiltinSecretFieldsAreNotBakedIn proves no shipped template carries a
// password default: every secret must be supplied at render time.
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

// TestNewDefaultService proves the production constructor ships the catalog
// and honors the services kill switch.
func TestNewDefaultService(t *testing.T) {
	t.Setenv(services.FeatureEnv, "")
	svc := NewDefaultService(nil)
	if svc == nil {
		t.Fatal("NewDefaultService = nil")
	}
	if len(svc.List()) != 4 {
		t.Fatalf("catalog = %d templates, want 4", len(svc.List()))
	}
	if _, err := svc.Render("wordpress", builtinSamples["wordpress"]); err != nil {
		t.Fatalf("Render(wordpress): %v", err)
	}

	t.Setenv(services.FeatureEnv, "false")
	if svc := NewDefaultService(nil); svc != nil {
		t.Fatal("NewDefaultService is not nil with FEATURE_SERVICES=false")
	}
}
