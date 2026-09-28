package templates

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"

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
// fails validation, which is a build bug the load-time tests catch; the
// failure is logged so it cannot pass silently in production.
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

// loadTemplate reads and validates one template directory, including the
// placeholder scan of compose.yaml: every placeholder must name a declared
// field and every declared field must be referenced at least once.
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
	if err := validatePlaceholders(slug, template, string(compose)); err != nil {
		return Template{}, err
	}
	template.compose = string(compose)
	return template, nil
}

// validatePlaceholders walks every string scalar of the raw compose document
// without substituting anything: it proves that each {{ .field }} names a
// declared field and that no declared field is dead metadata.
func validatePlaceholders(slug string, template Template, document string) error {
	known := make(map[string]bool, len(template.Fields))
	for _, field := range template.Fields {
		known[field.Key] = true
	}
	node, err := decodeDocument(document)
	if err != nil {
		return fmt.Errorf("%w: template %s: compose.yaml: %v", ErrValidation, slug, err)
	}
	used := make(map[string]bool, len(template.Fields))
	budget := services.MaxComposeYAML
	err = walkScalars(node, func(value string) (string, error) {
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
