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

type Service interface {
	List() []Template
	Get(slug string) (Template, bool)
	Render(slug string, values map[string]any) (RenderResult, error)
}

type Catalog struct {
	bySlug map[string]Template
	order  []string
}

var _ Service = (*Catalog)(nil)

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
	composeServices, err := validateComposeStructure(slug, node)
	if err != nil {
		return Template{}, err
	}
	for _, route := range template.Routes {
		if _, ok := composeServices[route.Service]; !ok {
			return Template{}, fmt.Errorf("%w: template %s: route names compose service %q, which compose.yaml does not declare",
				ErrValidation, slug, route.Service)
		}
	}
	template.compose = string(compose)
	return template, nil
}

func validatePlaceholders(slug string, template Template, node *yaml.Node) error {
	known := make(map[string]bool, len(template.Fields))
	for _, field := range template.Fields {
		known[field.Key] = true
	}
	used := make(map[string]bool, len(template.Fields))
	for _, route := range template.Routes {
		used[route.DomainField] = true
	}
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

type composeStructure struct {
	Include  any                       `yaml:"include"`
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image   string `yaml:"image"`
	Build   any    `yaml:"build"`
	Extends any    `yaml:"extends"`
	EnvFile any    `yaml:"env_file"`
}

func validateComposeStructure(slug string, node *yaml.Node) (map[string]struct{}, error) {
	var document composeStructure
	if err := node.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: template %s: compose.yaml: %v", ErrValidation, slug, err)
	}
	if document.Include != nil {
		return nil, fmt.Errorf("%w: template %s: include is not supported: the agent only receives this document", ErrValidation, slug)
	}
	if len(document.Services) == 0 {
		return nil, fmt.Errorf("%w: template %s: compose document declares no services", ErrValidation, slug)
	}
	if len(document.Services) > services.MaxComposeServices {
		return nil, fmt.Errorf("%w: template %s: compose document declares more than %d services",
			ErrValidation, slug, services.MaxComposeServices)
	}
	names := make(map[string]struct{}, len(document.Services))
	for name, service := range document.Services {
		if service.Build != nil {
			return nil, fmt.Errorf("%w: template %s: service %q uses build, which needs a build context the control plane cannot ship; push an image instead",
				ErrValidation, slug, name)
		}
		if service.Extends != nil {
			return nil, fmt.Errorf("%w: template %s: service %q uses extends, which references a file the node does not have", ErrValidation, slug, name)
		}
		if service.EnvFile != nil {
			return nil, fmt.Errorf("%w: template %s: service %q uses env_file, which references a file the node does not have", ErrValidation, slug, name)
		}
		if strings.TrimSpace(service.Image) == "" {
			return nil, fmt.Errorf("%w: template %s: service %q declares no image", ErrValidation, slug, name)
		}
		names[name] = struct{}{}
	}
	return names, nil
}

func (c *Catalog) List() []Template {
	listed := make([]Template, 0, len(c.order))
	for _, slug := range c.order {
		listed = append(listed, c.bySlug[slug])
	}
	return listed
}

func (c *Catalog) Get(slug string) (Template, bool) {
	template, ok := c.bySlug[slug]
	return template, ok
}
