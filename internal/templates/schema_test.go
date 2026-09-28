package templates

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/justindeelux/gotham/internal/services"
)

// sampleTemplateYAML exercises every field type: text with a pattern, number
// with bounds, select, bool and secret.
const sampleTemplateYAML = `name: Demo
icon: demo
description: A demo template.
fields:
  - key: domain
    label: Domain
    type: text
    required: true
    pattern: ^[a-z.]+$
    max_length: 253
  - key: count
    label: Count
    type: number
    default: 2
    min: 1
    max: 10
  - key: mode
    label: Mode
    type: select
    default: fast
    options: [fast, slow]
  - key: debug
    label: Debug
    type: bool
    default: false
  - key: password
    label: Password
    type: secret
    required: true
  - key: note
    label: Note
    type: text
    default: hello
`

// sampleComposeYAML references every field of sampleTemplateYAML.
const sampleComposeYAML = `services:
  web:
    image: nginx:1.27
    labels:
      gotham.domain: "{{ .domain }}"
    environment:
      COUNT: "{{ .count }}"
      MODE: "{{ .mode }}"
      DEBUG: "{{ .debug }}"
      PASSWORD: "{{ .password }}"
      NOTE: "{{ .note }}"
    volumes:
      - data:/data
volumes:
  data:
`

// sampleFiles returns a valid one-template catalog.
func sampleFiles() map[string]string {
	return map[string]string{
		"demo/template.yaml": sampleTemplateYAML,
		"demo/compose.yaml":  sampleComposeYAML,
	}
}

// catalogFrom loads an in-memory catalog from name -> content pairs.
func catalogFrom(files map[string]string) (*Catalog, error) {
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return Load(fsys)
}

// mustCatalog loads a valid in-memory catalog or fails the test.
func mustCatalog(t *testing.T, files map[string]string) *Catalog {
	t.Helper()
	catalog, err := catalogFrom(files)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return catalog
}

// TestLoadSample proves a valid template loads with its schema intact.
func TestLoadSample(t *testing.T) {
	catalog := mustCatalog(t, sampleFiles())

	listed := catalog.List()
	if len(listed) != 1 || listed[0].Slug != "demo" {
		t.Fatalf("List = %+v", listed)
	}
	template, ok := catalog.Get("demo")
	if !ok {
		t.Fatal("Get(demo) not found")
	}
	if template.Name != "Demo" || template.Icon != "demo" || template.Description != "A demo template." {
		t.Fatalf("metadata = %+v", template)
	}
	if len(template.Fields) != 6 {
		t.Fatalf("fields = %d, want 6", len(template.Fields))
	}
	if field := template.Fields[0]; field.Key != "domain" || field.Type != FieldText || !field.Required || field.Pattern == "" || field.MaxLength != 253 {
		t.Errorf("domain field = %+v", field)
	}
	if field := template.Fields[1]; field.Type != FieldNumber || field.Default == nil || *field.Default != "2" || field.Min == nil || *field.Min != 1 {
		t.Errorf("count field = %+v", field)
	}
	if field := template.Fields[2]; field.Type != FieldSelect || len(field.Options) != 2 || *field.Default != "fast" {
		t.Errorf("mode field = %+v", field)
	}
	if field := template.Fields[3]; field.Type != FieldBool || field.Default == nil || *field.Default != "false" {
		t.Errorf("debug field = %+v", field)
	}
	if field := template.Fields[4]; field.Type != FieldSecret || field.Default != nil || !field.Required {
		t.Errorf("password field = %+v", field)
	}
	if _, ok := catalog.Get("nope"); ok {
		t.Error("Get(nope) resolved")
	}
}

// TestLoadRejectsBadDefinitions is the validator's core table: every malformed
// definition must be rejected at load, never at render.
func TestLoadRejectsBadDefinitions(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"invalid slug directory": {
			files: map[string]string{"Bad_Slug/template.yaml": sampleTemplateYAML, "Bad_Slug/compose.yaml": sampleComposeYAML},
			want:  "not a valid template slug",
		},
		"missing template.yaml": {
			files: map[string]string{"demo/compose.yaml": sampleComposeYAML},
			want:  "read template.yaml",
		},
		"missing compose.yaml": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML},
			want:  "read compose.yaml",
		},
		"invalid template yaml": {
			files: map[string]string{"demo/template.yaml": "name: [unclosed", "demo/compose.yaml": sampleComposeYAML},
			want:  "template.yaml",
		},
		"unknown metadata key": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "icon: demo", "icon: demo\nversion: 2", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "field version not found",
		},
		"missing name": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "name: Demo\n", "", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "name must be",
		},
		"bad icon": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "icon: demo", "icon: Demo Icon", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "icon must be",
		},
		"missing description": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "description: A demo template.\n", "", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "description must be",
		},
		"no fields": {
			files: map[string]string{"demo/template.yaml": "name: Demo\nicon: demo\ndescription: A demo template.\nfields: []\n", "demo/compose.yaml": sampleComposeYAML},
			want:  "fields must declare",
		},
		"unknown type": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "type: text", "type: color", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "type must be one of",
		},
		"bad field key": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "key: domain", "key: Domain", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "key must be",
		},
		"duplicate field": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "key: password", "key: domain", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "duplicate field",
		},
		"optional without default": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    default: 2\n", "", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "must declare a default",
		},
		"secret not required": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    type: secret\n    required: true\n", "    type: secret\n", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "secret fields must be required",
		},
		"secret with default": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    type: secret\n    required: true\n", "    type: secret\n    required: true\n    default: hunter2\n", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "must not declare a default",
		},
		"invalid number default": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "default: 2", "default: two", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "whole number",
		},
		"number default out of range": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "default: 2", "default: 20", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "at most 10",
		},
		"huge float default": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "default: 2", "default: 1e30", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "whole number",
		},
		"select default not an option": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "default: fast", "default: turbo", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "must be one of",
		},
		"invalid bool default": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "default: false", "default: maybe", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "must be true or false",
		},
		"text default fails pattern": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    type: text\n    required: true\n", "    type: text\n    default: UPPER\n", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "does not match",
		},
		"invalid pattern": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "pattern: ^[a-z.]+$", "pattern: ^[a-z", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "invalid pattern",
		},
		"pattern on number": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    default: 2\n    min: 1", "    default: 2\n    pattern: ^[0-9]+$\n    min: 1", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "min and max",
		},
		"options on text": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    max_length: 253\n", "    max_length: 253\n    options: [a, b]\n", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "options apply to select",
		},
		"min greater than max": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "    min: 1\n    max: 10", "    min: 11\n    max: 10", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "min is greater than max",
		},
		"duplicate option": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "options: [fast, slow]", "options: [fast, fast]", 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "duplicate option",
		},
		"empty option": {
			files: map[string]string{"demo/template.yaml": strings.Replace(sampleTemplateYAML, "options: [fast, slow]", `options: ["fast", ""]`, 1), "demo/compose.yaml": sampleComposeYAML},
			want:  "options must be non-empty",
		},
		"empty compose": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": ""},
			want:  "compose.yaml is empty",
		},
		"invalid compose yaml": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": "services: [unclosed"},
			want:  "compose.yaml",
		},
		"unknown placeholder": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "{{ .count }}", "{{ .missing }}", 1)},
			want:  `unknown field "missing"`,
		},
		"malformed placeholder": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "{{ .count }}", "{{ if .count }}", 1)},
			want:  "not a supported placeholder",
		},
		"unterminated placeholder": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "{{ .count }}", "{{ .count", 1)},
			want:  "unterminated placeholder",
		},
		"unused field": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "      COUNT: \"{{ .count }}\"\n", "", 1)},
			want:  `field "count" is never referenced`,
		},
		"no templates": {
			files: map[string]string{"README.md": "not a template"},
			want:  "no templates found",
		},
		"null trailing document": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML + "---\nnull\n", "demo/compose.yaml": sampleComposeYAML},
			want:  "must contain exactly one document",
		},
		"malformed trailing document": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML + "---\n[broken\n", "demo/compose.yaml": sampleComposeYAML},
			want:  "must contain exactly one document",
		},
		"extra trailing document": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML + "---\nnull\n---\nignored: true\n", "demo/compose.yaml": sampleComposeYAML},
			want:  "must contain exactly one document",
		},
		"build service": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "image: nginx:1.27", "build: .", 1)},
			want:  "uses build",
		},
		"extends service": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "image: nginx:1.27", "image: nginx:1.27\n    extends:\n      service: base", 1)},
			want:  "uses extends",
		},
		"env_file service": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "image: nginx:1.27", "image: nginx:1.27\n    env_file: .env", 1)},
			want:  "uses env_file",
		},
		"empty image": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": strings.Replace(sampleComposeYAML, "image: nginx:1.27", `image: ""`, 1)},
			want:  "declares no image",
		},
		"include document": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": "include:\n  - other.yaml\n" + sampleComposeYAML},
			want:  "include is not supported",
		},
		"extra compose document": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": sampleComposeYAML + "---\nnull\n"},
			want:  "exactly one document",
		},
		"no services": {
			files: map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": "name: \"{{ .domain }}\"\ncount: \"{{ .count }}\"\nmode: \"{{ .mode }}\"\ndebug: \"{{ .debug }}\"\npassword: \"{{ .password }}\"\nnote: \"{{ .note }}\"\n"},
			want:  "declares no services",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := catalogFrom(tc.files)
			if err == nil {
				t.Fatal("Load succeeded, want an error")
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

// TestLoadRejectsTooManyServices proves the load-time structural check reuses
// the deploy path's service-count bound, so an oversized catalog entry cannot
// enter the gallery and fail on every render.
func TestLoadRejectsTooManyServices(t *testing.T) {
	var compose strings.Builder
	compose.WriteString("services:\n")
	for i := 0; i <= services.MaxComposeServices; i++ {
		fmt.Fprintf(&compose, "  svc%d:\n    image: nginx:1.27\n", i)
	}
	compose.WriteString("name: \"{{ .domain }}\"\ncount: \"{{ .count }}\"\nmode: \"{{ .mode }}\"\ndebug: \"{{ .debug }}\"\npassword: \"{{ .password }}\"\nnote: \"{{ .note }}\"\n")
	files := map[string]string{"demo/template.yaml": sampleTemplateYAML, "demo/compose.yaml": compose.String()}
	_, err := catalogFrom(files)
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("Load = %v, want the service-count bound", err)
	}
}

// TestLoadAllowsValueDrivenStructure proves the load-time structural check
// stays value-independent: a placeholder in a service name or image position
// is allowed, because only the render's services.Parse can judge the
// substituted value.
func TestLoadAllowsValueDrivenStructure(t *testing.T) {
	files := sampleFiles()
	files["demo/compose.yaml"] = strings.Replace(sampleComposeYAML, "image: nginx:1.27", `image: "{{ .note }}"`, 1)
	catalog := mustCatalog(t, files)
	result, err := catalog.Render("demo", map[string]any{"domain": "app.example.com", "password": "x", "note": "nginx:1.27"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := serviceMap(t, decodeMap(t, result.ComposeYAML), "web")["image"]; got != "nginx:1.27" {
		t.Errorf("image = %v", got)
	}
}

// TestLoadRejectsOversizedTemplate proves the template.yaml size bound.
func TestLoadRejectsOversizedTemplate(t *testing.T) {
	files := sampleFiles()
	files["demo/template.yaml"] = sampleTemplateYAML + strings.Repeat("# padding\n", MaxTemplateYAML)
	if _, err := catalogFrom(files); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Load = %v, want a size error", err)
	}
}
