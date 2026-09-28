package templates

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/services"
)

// renderDemo renders the sample template with the given values.
func renderDemo(t *testing.T, values map[string]any) RenderResult {
	t.Helper()
	catalog := mustCatalog(t, sampleFiles())
	result, err := catalog.Render("demo", values)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return result
}

// decodeMap parses a document into generic YAML values.
func decodeMap(t *testing.T, document string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(document), &doc); err != nil {
		t.Fatalf("decode document: %v\n%s", err, document)
	}
	return doc
}

// serviceMap returns one compose service of a decoded document.
func serviceMap(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	services, ok := doc["services"].(map[string]any)
	if !ok {
		t.Fatalf("no services in document: %v", doc)
	}
	service, ok := services[name].(map[string]any)
	if !ok {
		t.Fatalf("no service %q in document: %v", name, services)
	}
	return service
}

// envValue returns one environment value of a decoded compose service.
func envValue(t *testing.T, doc map[string]any, service, key string) string {
	t.Helper()
	environment, ok := serviceMap(t, doc, service)["environment"].(map[string]any)
	if !ok {
		t.Fatalf("service %q has no environment mapping", service)
	}
	value, ok := environment[key].(string)
	if !ok {
		t.Fatalf("environment %s of %q = %v, want a string", key, service, environment[key])
	}
	return value
}

// TestRenderSample proves defaults fill every optional field, the parsed spec
// carries the domain route, and rendering is deterministic.
func TestRenderSample(t *testing.T) {
	values := map[string]any{"domain": "app.example.com", "password": "p$ss"}
	result := renderDemo(t, values)

	if got := result.Spec.Services; len(got) != 1 || got[0] != "web" {
		t.Errorf("services = %v", got)
	}
	if len(result.Spec.Domains) != 1 {
		t.Fatalf("domains = %+v", result.Spec.Domains)
	}
	if route := result.Spec.Domains[0]; route.Service != "web" || route.Domain != "app.example.com" || route.Port != 80 {
		t.Errorf("domain = %+v", route)
	}
	if got := result.Spec.NamedVolumes; len(got) != 1 || got[0] != "data" {
		t.Errorf("named volumes = %v", got)
	}

	doc := decodeMap(t, result.ComposeYAML)
	if got := envValue(t, doc, "web", "COUNT"); got != "2" {
		t.Errorf("COUNT = %q", got)
	}
	if got := envValue(t, doc, "web", "MODE"); got != "fast" {
		t.Errorf("MODE = %q", got)
	}
	if got := envValue(t, doc, "web", "DEBUG"); got != "false" {
		t.Errorf("DEBUG = %q", got)
	}
	if got := envValue(t, doc, "web", "PASSWORD"); got != "p$$ss" {
		t.Errorf("PASSWORD = %q, want the dollar escaped for the service pipeline", got)
	}

	again := renderDemo(t, values)
	if again.ComposeYAML != result.ComposeYAML {
		t.Error("rendering is not deterministic")
	}
}

// TestRenderAcceptScalars proves JSON strings, whole numbers and booleans are
// accepted for a typed field.
func TestRenderAcceptScalars(t *testing.T) {
	result := renderDemo(t, map[string]any{
		"domain":   "app.example.com",
		"password": "secret",
		"count":    7,
		"mode":     "slow",
		"debug":    true,
	})
	doc := decodeMap(t, result.ComposeYAML)
	if got := envValue(t, doc, "web", "COUNT"); got != "7" {
		t.Errorf("COUNT = %q", got)
	}
	if got := envValue(t, doc, "web", "MODE"); got != "slow" {
		t.Errorf("MODE = %q", got)
	}
	if got := envValue(t, doc, "web", "DEBUG"); got != "true" {
		t.Errorf("DEBUG = %q", got)
	}
}

// TestRenderRejectsBadValues proves strict validation: missing required values,
// unknown fields and values that fail their type's rules are all errors.
func TestRenderRejectsBadValues(t *testing.T) {
	cases := map[string]struct {
		values map[string]any
		want   string
	}{
		"missing required":     {values: map[string]any{"domain": "app.example.com"}, want: `field "password" is required`},
		"unknown field":        {values: map[string]any{"domain": "app.example.com", "password": "x", "extra": "y"}, want: `unknown field "extra"`},
		"pattern mismatch":     {values: map[string]any{"domain": "UPPER.example", "password": "x"}, want: "does not match"},
		"max length":           {values: map[string]any{"domain": strings.Repeat("a", 300), "password": "x"}, want: "at most 253 characters"},
		"value too long":       {values: map[string]any{"domain": "app.example.com", "password": strings.Repeat("x", MaxFieldValue+1)}, want: "exceeds 1024 bytes"},
		"number not a number":  {values: map[string]any{"domain": "app.example.com", "password": "x", "count": "abc"}, want: "whole number"},
		"number below min":     {values: map[string]any{"domain": "app.example.com", "password": "x", "count": 0}, want: "at least 1"},
		"number above max":     {values: map[string]any{"domain": "app.example.com", "password": "x", "count": 99}, want: "at most 10"},
		"number fractional":    {values: map[string]any{"domain": "app.example.com", "password": "x", "count": 1.5}, want: "whole number"},
		"bool not a bool":      {values: map[string]any{"domain": "app.example.com", "password": "x", "debug": "maybe"}, want: "true or false"},
		"select not an option": {values: map[string]any{"domain": "app.example.com", "password": "x", "mode": "turbo"}, want: "must be one of"},
		"null value":           {values: map[string]any{"domain": "app.example.com", "password": nil}, want: "string, number or boolean"},
		"structured value":     {values: map[string]any{"domain": "app.example.com", "password": map[string]any{"a": "b"}}, want: "string, number or boolean"},
		"empty required":       {values: map[string]any{"domain": "   ", "password": "x"}, want: `field "domain" is required`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			catalog := mustCatalog(t, sampleFiles())
			_, err := catalog.Render("demo", tc.values)
			if err == nil {
				t.Fatal("Render succeeded, want an error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("error = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestRenderRejectsOversizedDocument proves the rendered-size budget stops a
// document with repeated placeholders before the oversized string is built
// (the budget is shared across the whole document, like services.Interpolate).
func TestRenderRejectsOversizedDocument(t *testing.T) {
	repeats := services.MaxComposeYAML/MaxFieldValue + 2
	files := sampleFiles()
	files["demo/compose.yaml"] = strings.Replace(sampleComposeYAML,
		`      PASSWORD: "{{ .password }}"`,
		`      PASSWORD: "`+strings.Repeat("{{ .password }}", repeats)+`"`, 1)
	catalog := mustCatalog(t, files)
	_, err := catalog.Render("demo", map[string]any{
		"domain":   "app.example.com",
		"password": strings.Repeat("x", MaxFieldValue),
	})
	if err == nil {
		t.Fatal("Render succeeded, want an oversized-document error")
	}
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Render = %v, want ErrValidation about the size bound", err)
	}
}

// TestRenderUnknownSlug proves an absent template is ErrNotFound.
func TestRenderUnknownSlug(t *testing.T) {
	catalog := mustCatalog(t, sampleFiles())
	if _, err := catalog.Render("nope", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Render(nope) = %v, want ErrNotFound", err)
	}
}

// TestRenderEscapesValuesForServices proves a value can never be read as a
// ${VAR} reference by the service pipeline and can never inject YAML
// structure: quotes, newlines, colons and dollars stay one scalar.
func TestRenderEscapesValuesForServices(t *testing.T) {
	secret := "p$a$$b\n  injected: yes\n  - list\n${SECRET}\"quote\\\""
	result := renderDemo(t, map[string]any{"domain": "app.example.com", "password": secret})

	// The render output is valid YAML: no injected top-level key, no second
	// service, exactly one volume.
	doc := decodeMap(t, result.ComposeYAML)
	if _, ok := doc["injected"]; ok {
		t.Fatalf("the value injected a top-level key: %s", result.ComposeYAML)
	}
	if _, ok := serviceMap(t, doc, "web")["injected"]; ok {
		t.Fatalf("the value injected a service key: %s", result.ComposeYAML)
	}
	if got := envValue(t, doc, "web", "PASSWORD"); got != escapeDollar(secret) {
		t.Fatalf("PASSWORD = %q, want %q", got, escapeDollar(secret))
	}

	// The services pipeline renders the document without resolving anything
	// out of the value: every dollar is already escaped.
	rendered, err := services.Render(result.ComposeYAML, nil)
	if err != nil {
		t.Fatalf("services.Render: %v", err)
	}
	// The node's compose run unescapes $$ back to $, so the escaped form is
	// exactly what must arrive on disk.
	final := decodeMap(t, rendered.ComposeYAML)
	deployed := envValue(t, final, "web", "PASSWORD")
	if deployed != escapeDollar(secret) {
		t.Fatalf("the deployed document lost the value:\n%s", rendered.ComposeYAML)
	}
	if !strings.Contains(deployed, "$${SECRET}") {
		t.Fatalf("a ${VAR}-looking value was not escaped:\n%s", rendered.ComposeYAML)
	}
}

// TestRenderIsNotRecursive proves a value that itself looks like a placeholder
// is inserted literally once; the engine never re-scans its own output.
func TestRenderIsNotRecursive(t *testing.T) {
	result := renderDemo(t, map[string]any{"domain": "app.example.com", "password": "{{ .domain }}"})
	doc := decodeMap(t, result.ComposeYAML)
	if got := envValue(t, doc, "web", "PASSWORD"); got != "{{ .domain }}" {
		t.Fatalf("PASSWORD = %q, want the literal value", got)
	}
}

// TestLoadRejectsTemplateSyntax proves the placeholder syntax is the only
// mechanism: template actions, pipes, functions and blocks are load errors.
func TestLoadRejectsTemplateSyntax(t *testing.T) {
	for name, replacement := range map[string]string{
		"action":  "{{ if .count }}",
		"pipe":    "{{ .count | printf \"%d\" }}",
		"block":   "{{ define \"x\" }}",
		"range":   "{{ range .count }}",
		"comment": "{{/* note */}}",
		"dash":    "{{- .count }}",
		"bare":    "{{ count }}",
	} {
		t.Run(name, func(t *testing.T) {
			files := sampleFiles()
			files["demo/compose.yaml"] = strings.Replace(sampleComposeYAML, "{{ .count }}", replacement, 1)
			if _, err := catalogFrom(files); err == nil {
				t.Fatalf("Load accepted %q", replacement)
			} else if !errors.Is(err, ErrValidation) {
				t.Errorf("error = %v, want ErrValidation", err)
			}
		})
	}
}

// TestRenderPlaceholderSpacing proves the documented whitespace tolerance:
// {{.field}}, {{ .field}} and {{ .field }} are equivalent.
func TestRenderPlaceholderSpacing(t *testing.T) {
	for _, form := range []string{"{{.domain}}", "{{ .domain}}", "{{.domain }}", "{{ .domain }}"} {
		files := sampleFiles()
		files["demo/compose.yaml"] = strings.Replace(sampleComposeYAML, "{{ .domain }}", form, 1)
		catalog := mustCatalog(t, files)
		result, err := catalog.Render("demo", map[string]any{"domain": "app.example.com", "password": "x"})
		if err != nil {
			t.Fatalf("Render with %q: %v", form, err)
		}
		if !strings.Contains(result.ComposeYAML, "app.example.com") {
			t.Errorf("form %q did not substitute", form)
		}
	}
}
