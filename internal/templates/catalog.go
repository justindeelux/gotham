package templates

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	builtin "github.com/justindeelux/gotham/templates"

	"github.com/justindeelux/gotham/internal/services"
)

// Service is the template surface the HTTP layer depends on: the loaded
// catalog. It is implemented by *Catalog; the route tests can substitute their
// own implementation.
type Service interface {
	// List returns every template's metadata, sorted by slug.
	List() []Template
	// Get returns one template by slug.
	Get(slug string) (Template, bool)
	// Render validates the supplied values and renders the template's
	// compose document. The result has already passed services.Parse.
	Render(slug string, values map[string]any) (RenderResult, error)
}

// Catalog is the loaded, validated set of templates.
type Catalog struct {
	bySlug map[string]Template
	order  []string
}

// Compile-time guarantee that Catalog satisfies the route contract.
var _ Service = (*Catalog)(nil)

// NewDefaultService loads the catalog embedded in the binary: the repository's
// templates/ directory. It returns nil when the services feature is off (the
// rendered documents would have nowhere to deploy) or when a built-in template
// fails validation. A failed load is logged and leaves the surface unmounted
// (the routes are absent), it does not stop control-plane startup: a shipped
// template that fails validation is a build defect the load-time catalog tests
// catch before release, and at runtime the rest of the platform keeps working.
func NewDefaultService(logger *slog.Logger) Service {
	if !services.Enabled() {
		return nil
	}
	catalog, err := Load(builtin.FS)
	if err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("templates: built-in templates failed validation", "error", err)
		return nil
	}
	return catalog
}

// Load reads and validates every template below fsys. Each immediate
// subdirectory is one template: its name is the slug, and template.yaml plus
// compose.yaml must both be present and valid. Load fails on the first
// invalid template, so a catalog is either fully valid or absent.
func Load(fsys fs.FS) (*Catalog, error) {
	if fsys == nil {
		return nil, errors.New("templates: no template filesystem")
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("templates: read catalog: %w", err)
	}
	catalog := &Catalog{bySlug: make(map[string]Template, len(entries))}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		if !slugPattern.MatchString(slug) {
			return nil, fmt.Errorf("%w: directory %q is not a valid template slug", ErrValidation, slug)
		}
		template, err := loadTemplate(fsys, slug)
		if err != nil {
			return nil, err
		}
		catalog.bySlug[slug] = template
		catalog.order = append(catalog.order, slug)
	}
	if len(catalog.order) == 0 {
		return nil, fmt.Errorf("%w: no templates found", ErrValidation)
	}
	sort.Strings(catalog.order)
	return catalog, nil
}

// loadTemplate reads and validates one template directory: the metadata, the
// placeholder scan of compose.yaml (every placeholder names a declared field,
// every declared field is referenced) and the structural compose subset the
// deploy path requires (image-only services, no build/extends/env_file/
// include). Value-dependent checks (domains, volume names, paths) stay in the
// runtime services.Parse call, because only a render has the values.
func loadTemplate(fsys fs.FS, slug string) (Template, error) {
	metadata, err := fs.ReadFile(fsys, slug+"/template.yaml")
	if err != nil {
		return Template{}, fmt.Errorf("%w: template %s: read template.yaml: %v", ErrValidation, slug, err)
	}
	template, err := parseTemplate(slug, metadata)
	if err != nil {
		return Template{}, err
	}
	compose, err := fs.ReadFile(fsys, slug+"/compose.yaml")
	if err != nil {
		return Template{}, fmt.Errorf("%w: template %s: read compose.yaml: %v", ErrValidation, slug, err)
	}
	if len(compose) == 0 {
		return Template{}, fmt.Errorf("%w: template %s: compose.yaml is empty", ErrValidation, slug)
	}
	if len(compose) > services.MaxComposeYAML {
		return Template{}, fmt.Errorf("%w: template %s: compose.yaml exceeds %d bytes", ErrValidation, slug, services.MaxComposeYAML)
	}
	node, err := decodeDocument(string(compose))
	if err != nil {
		return Template{}, fmt.Errorf("%w: template %s: compose.yaml: %v", ErrValidation, slug, err)
	}
	if err := validatePlaceholders(slug, template, node); err != nil {
		return Template{}, err
	}
	if err := validateComposeStructure(slug, node); err != nil {
		return Template{}, err
	}
	template.compose = string(compose)
	return template, nil
}

// validatePlaceholders walks every string scalar of the raw compose document
// without substituting anything: it proves that each {{ .field }} names a
// declared field and that no declared field is dead metadata.
func validatePlaceholders(slug string, template Template, node *yaml.Node) error {
	known := make(map[string]bool, len(template.Fields))
	for _, field := range template.Fields {
		known[field.Key] = true
	}
	used := make(map[string]bool, len(template.Fields))
	budget := services.MaxComposeYAML
	err := walkScalars(node, func(value string) (string, error) {
		if _, err := substitute(value, &budget, func(key string) (string, error) {
			if !known[key] {
				return "", fmt.Errorf("%w: unknown field %q", ErrValidation, key)
			}
			used[key] = true
			return "", nil
		}); err != nil {
			return "", err
		}
		return value, nil
	})
	if err != nil {
		return fmt.Errorf("template %s: %w", slug, err)
	}
	for _, field := range template.Fields {
		if !used[field.Key] {
			return fmt.Errorf("%w: template %s: field %q is never referenced in compose.yaml", ErrValidation, slug, field.Key)
		}
	}
	return nil
}

// composeStructure is the value-independent subset of a compose document a
// template must satisfy at load: services are image-only (no build context,
// no file includes, no extends, no external env files). The rules mirror
// services.Parse, which re-checks them after substitution together with the
// value-dependent ones; a template that cannot deploy is rejected before it
// enters the gallery instead of failing on every render.
type composeStructure struct {
	Include  any                       `yaml:"include"`
	Services map[string]composeService `yaml:"services"`
}

// composeService is the subset of one service definition the structural check
// inspects.
type composeService struct {
	Image   string `yaml:"image"`
	Build   any    `yaml:"build"`
	Extends any    `yaml:"extends"`
	EnvFile any    `yaml:"env_file"`
}

// validateComposeStructure applies the deploy-path structural rules to the raw
// template document. Placeholders are allowed anywhere a value may carry them:
// service names, images and the forbidden keys are only checked for presence,
// never against substituted values, so a field-driven image is fine.
func validateComposeStructure(slug string, node *yaml.Node) error {
	var document composeStructure
	if err := node.Decode(&document); err != nil {
		return fmt.Errorf("%w: template %s: compose.yaml: %v", ErrValidation, slug, err)
	}
	if document.Include != nil {
		return fmt.Errorf("%w: template %s: include is not supported: the agent only receives this document", ErrValidation, slug)
	}
	if len(document.Services) == 0 {
		return fmt.Errorf("%w: template %s: compose document declares no services", ErrValidation, slug)
	}
	if len(document.Services) > services.MaxComposeServices {
		return fmt.Errorf("%w: template %s: compose document declares more than %d services",
			ErrValidation, slug, services.MaxComposeServices)
	}
	for name, service := range document.Services {
		if service.Build != nil {
			return fmt.Errorf("%w: template %s: service %q uses build, which needs a build context the control plane cannot ship; push an image instead",
				ErrValidation, slug, name)
		}
		if service.Extends != nil {
			return fmt.Errorf("%w: template %s: service %q uses extends, which references a file the node does not have", ErrValidation, slug, name)
		}
		if service.EnvFile != nil {
			return fmt.Errorf("%w: template %s: service %q uses env_file, which references a file the node does not have", ErrValidation, slug, name)
		}
		if strings.TrimSpace(service.Image) == "" {
			return fmt.Errorf("%w: template %s: service %q declares no image", ErrValidation, slug, name)
		}
	}
	return nil
}

// List returns the catalog metadata sorted by slug.
func (c *Catalog) List() []Template {
	listed := make([]Template, 0, len(c.order))
	for _, slug := range c.order {
		listed = append(listed, c.bySlug[slug])
	}
	return listed
}

// Get returns the template with the given slug.
func (c *Catalog) Get(slug string) (Template, bool) {
	template, ok := c.bySlug[slug]
	return template, ok
}
