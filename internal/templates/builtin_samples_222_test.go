package templates

import (
	"testing"

	"github.com/google/uuid"

	builtin "github.com/justindeelux/gotham/templates"

	"github.com/justindeelux/gotham/composeguard"
	"github.com/justindeelux/gotham/internal/services"
)

// Values for fields whose pattern rejects the generic generated secret.
const (
	testBatch222Secret32  = "0123456789abcdef0123456789abcdef"
	testBatch222Hex64     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testBatch222Base64Key = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
)

func init() {
	for slug, values := range map[string]map[string]any{
		"orbit": {"app_key": testBatch222Base64Key},
		"plane": {
			"db_password":       testBatch222Secret32,
			"secret_key":        testBatch222Secret32,
			"live_secret":       testBatch222Secret32,
			"minio_password":    testBatch222Secret32,
			"rabbitmq_password": testBatch222Secret32,
		},
		"reactive-resume": {
			"db_password":       testBatch222Secret32,
			"auth_secret":       testBatch222Secret32,
			"encryption_secret": testBatch222Secret32,
		},
		"redmine": {
			"db_password":     testBatch222Secret32,
			"secret_key_base": testBatch222Hex64,
		},
		"snipe-it": {
			"app_key":          "base64:" + testBatch222Base64Key,
			"db_password":      testBatch222Secret32,
			"db_root_password": testBatch222Secret32,
		},
		"teable": {
			"db_password":    testBatch222Secret32,
			"redis_password": testBatch222Secret32,
			"secret_key":     testBatch222Secret32,
		},
		"tolgee": {
			"admin_password": testBatch222Secret32,
			"jwt_secret":     testBatch222Secret32,
			"db_password":    testBatch222Secret32,
		},
		"twenty": {
			"db_password":    testBatch222Secret32,
			"app_secret":     testBatch222Secret32,
			"encryption_key": testBatch222Secret32,
		},
		"vikunja": {"jwt_secret": testBatch222Secret32},
	} {
		builtinSamples[slug] = values
	}
}

// testBatch222Services are the templates this batch adds.
var testBatch222Services = []string{
	"nocodb",
	"odoo",
	"orbit",
	"organizr",
	"pairdrop",
	"paperless",
	"penpot",
	"plane",
	"rallly",
	"reactive-resume",
	"readeck",
	"redlib",
	"redmine",
	"ryot",
	"shlink",
	"silverbullet",
	"slash",
	"snipe-it",
	"stirling-pdf",
	"teable",
	"tolgee",
	"triliumnext",
	"twenty",
	"vikunja",
	"weblate",
	"yamtrack",
}

func TestBatch222PassesServiceConfinement(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, slug := range testBatch222Services {
		t.Run(slug, func(t *testing.T) {
			template, ok := catalog.Get(slug)
			if !ok {
				t.Fatalf("template %s is missing from the catalog", slug)
			}
			result, err := catalog.Render(slug, builtinValues(template))
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
				t.Fatalf("composeguard.Validate: %v", err)
			}
		})
	}
}
