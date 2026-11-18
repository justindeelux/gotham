package templates

import (
	"testing"

	"github.com/google/uuid"

	builtin "github.com/justindeelux/gotham/templates"

	"github.com/justindeelux/gotham/composeguard"
	"github.com/justindeelux/gotham/internal/services"
)

// testBatch222Services are the templates this batch adds. Generated values
// render every one of them, so none needs a builtinSamples override.
var testBatch222Services = []string{
	"nocodb",
	"odoo",
	"organizr",
	"paperless",
	"penpot",
	"plane",
	"reactive-resume",
	"readeck",
	"redmine",
	"ryot",
	"shlink",
	"silverbullet",
	"slash",
	"stirling-pdf",
	"teable",
	"tolgee",
	"triliumnext",
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
