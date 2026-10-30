package templates

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	MaxTemplateYAML      = 64 << 10
	MaxNameLength        = 80
	MaxDescriptionLength = 280
	MaxFields            = 32
	MaxRoutes            = 32
	MaxFieldValue        = 1024
	maxLabelLength       = 80
	maxPlaceholderLength = 200
	maxHelpLength        = 500
	maxPatternLength     = 256
	maxOptions           = 64
	maxOptionLength      = 128
	minGeneratedLength   = 16
)

var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

var fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var iconPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type FieldType string

const (
	FieldText   FieldType = "text"
	FieldSecret FieldType = "secret"
	FieldNumber FieldType = "number"
	FieldSelect FieldType = "select"
	FieldBool   FieldType = "bool"
)

type Field struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required"`
	Default     *string   `json:"default,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Help        string    `json:"help,omitempty"`
	Pattern     string    `json:"pattern,omitempty"`
	MaxLength   int       `json:"max_length,omitempty"`
	Min         *int64    `json:"min,omitempty"`
	Max         *int64    `json:"max,omitempty"`
	Options     []string  `json:"options,omitempty"`
	Generate    bool      `json:"generate,omitempty"`

	compiled *regexp.Regexp
}

// Route declares that the platform should route a compose service to a
// container port, using the text field named by DomainField as the host.
type Route struct {
	Service     string `json:"service"`
	Port        int32  `json:"port"`
	DomainField string `json:"domain_field"`
}

type Template struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Icon        string  `json:"icon"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
	Routes      []Route `json:"routes,omitempty"`

	compose string
}

type templateDocument struct {
	Name        string          `yaml:"name"`
	Icon        string          `yaml:"icon"`
	Description string          `yaml:"description"`
	Fields      []fieldDocument `yaml:"fields"`
	Routes      []routeDocument `yaml:"routes"`
}

type routeDocument struct {
	Service     string `yaml:"service"`
	Port        int    `yaml:"port"`
	DomainField string `yaml:"domain_field"`
}

type fieldDocument struct {
	Key         string   `yaml:"key"`
	Label       string   `yaml:"label"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required"`
	Default     any      `yaml:"default"`
	Placeholder string   `yaml:"placeholder"`
	Help        string   `yaml:"help"`
	Pattern     string   `yaml:"pattern"`
	MaxLength   int      `yaml:"max_length"`
	Min         *int64   `yaml:"min"`
	Max         *int64   `yaml:"max"`
	Options     []string `yaml:"options"`
	Generate    bool     `yaml:"generate"`
}

func parseTemplate(slug string, document []byte) (Template, error) {
	if !slugPattern.MatchString(slug) {
		return Template{}, fmt.Errorf("%w: %q is not a valid template slug", ErrValidation, slug)
	}
	if len(document) > MaxTemplateYAML {
		return Template{}, fmt.Errorf("%w: template %s: template.yaml exceeds %d bytes", ErrValidation, slug, MaxTemplateYAML)
	}
	var raw templateDocument
	decoder := yaml.NewDecoder(bytes.NewReader(document))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return Template{}, fmt.Errorf("%w: template %s: template.yaml: %v", ErrValidation, slug, err)
	}
	var extra any
	switch err := decoder.Decode(&extra); {
	case err == nil:
		return Template{}, fmt.Errorf("%w: template %s: template.yaml must contain exactly one document", ErrValidation, slug)
	case !errors.Is(err, io.EOF):
		return Template{}, fmt.Errorf("%w: template %s: template.yaml must contain exactly one document: %v", ErrValidation, slug, err)
	}

	name := strings.TrimSpace(raw.Name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
		return Template{}, fmt.Errorf("%w: template %s: name must be 1-%d characters", ErrValidation, slug, MaxNameLength)
	}
	icon := strings.TrimSpace(raw.Icon)
	if !iconPattern.MatchString(icon) {
		return Template{}, fmt.Errorf("%w: template %s: icon must be 1-64 characters of lowercase letters, digits or \"-\"", ErrValidation, slug)
	}
	description := strings.TrimSpace(raw.Description)
	if description == "" || utf8.RuneCountInString(description) > MaxDescriptionLength {
		return Template{}, fmt.Errorf("%w: template %s: description must be 1-%d characters", ErrValidation, slug, MaxDescriptionLength)
	}
	if len(raw.Fields) == 0 || len(raw.Fields) > MaxFields {
		return Template{}, fmt.Errorf("%w: template %s: fields must declare 1-%d entries", ErrValidation, slug, MaxFields)
	}

	fields := make([]Field, 0, len(raw.Fields))
	seen := make(map[string]bool, len(raw.Fields))
	for index, entry := range raw.Fields {
		field, err := parseField(entry)
		if err != nil {
			return Template{}, fmt.Errorf("template %s: field %d: %w", slug, index, err)
		}
		if seen[field.Key] {
			return Template{}, fmt.Errorf("%w: template %s: duplicate field %q", ErrValidation, slug, field.Key)
		}
		seen[field.Key] = true
		fields = append(fields, field)
	}
	if len(raw.Routes) > MaxRoutes {
		return Template{}, fmt.Errorf("%w: template %s: routes must declare at most %d entries", ErrValidation, slug, MaxRoutes)
	}
	routes := make([]Route, 0, len(raw.Routes))
	seenRoutes := make(map[string]bool, len(raw.Routes))
	for index, entry := range raw.Routes {
		route, err := parseRoute(entry, fields, seenRoutes)
		if err != nil {
			return Template{}, fmt.Errorf("template %s: route %d: %w", slug, index, err)
		}
		seenRoutes[route.Service] = true
		routes = append(routes, route)
	}
	return Template{Slug: slug, Name: name, Icon: icon, Description: description, Fields: fields, Routes: routes}, nil
}

func parseRoute(raw routeDocument, fields []Field, seen map[string]bool) (Route, error) {
	service := strings.TrimSpace(raw.Service)
	if service == "" || service != raw.Service {
		return Route{}, fmt.Errorf("%w: service must be a non-empty compose service name", ErrValidation)
	}
	if seen[service] {
		return Route{}, fmt.Errorf("%w: duplicate route for compose service %q", ErrValidation, service)
	}
	if raw.Port < 1 || raw.Port > 65535 {
		return Route{}, fmt.Errorf("%w: service %q: port must be between 1 and 65535", ErrValidation, service)
	}
	domainField := strings.TrimSpace(raw.DomainField)
	if domainField == "" || domainField != raw.DomainField || !fieldKeyPattern.MatchString(domainField) {
		return Route{}, fmt.Errorf("%w: service %q: domain_field must name a field", ErrValidation, service)
	}
	for _, field := range fields {
		if field.Key != domainField {
			continue
		}
		if field.Type != FieldText {
			return Route{}, fmt.Errorf("%w: service %q: domain_field %q must be a text field", ErrValidation, service, domainField)
		}
		return Route{Service: service, Port: int32(raw.Port), DomainField: domainField}, nil
	}
	return Route{}, fmt.Errorf("%w: service %q: domain_field %q is not declared", ErrValidation, service, domainField)
}

func parseField(raw fieldDocument) (Field, error) {
	key := strings.TrimSpace(raw.Key)
	if !fieldKeyPattern.MatchString(key) {
		return Field{}, fmt.Errorf("%w: key must be 1-64 characters of lowercase letters, digits or \"_\" and start with a letter", ErrValidation)
	}
	if err := rejectSurrounding(raw.Key, key, "key"); err != nil {
		return Field{}, err
	}
	field := Field{
		Key:         key,
		Label:       strings.TrimSpace(raw.Label),
		Required:    raw.Required,
		Placeholder: strings.TrimSpace(raw.Placeholder),
		Help:        strings.TrimSpace(raw.Help),
		Pattern:     strings.TrimSpace(raw.Pattern),
		MaxLength:   raw.MaxLength,
		Min:         raw.Min,
		Max:         raw.Max,
		Generate:    raw.Generate,
	}
	field.Type = FieldType(strings.TrimSpace(raw.Type))
	if field.Label == "" {
		field.Label = key
	}
	if utf8.RuneCountInString(field.Label) > maxLabelLength {
		return Field{}, fmt.Errorf("%w: field %q: label is longer than %d characters", ErrValidation, key, maxLabelLength)
	}
	if utf8.RuneCountInString(field.Placeholder) > maxPlaceholderLength {
		return Field{}, fmt.Errorf("%w: field %q: placeholder is longer than %d characters", ErrValidation, key, maxPlaceholderLength)
	}
	if utf8.RuneCountInString(field.Help) > maxHelpLength {
		return Field{}, fmt.Errorf("%w: field %q: help is longer than %d characters", ErrValidation, key, maxHelpLength)
	}

	hasPattern := field.Pattern != ""
	hasBounds := field.Min != nil || field.Max != nil
	hasOptions := len(raw.Options) > 0
	if field.Generate {
		if field.Type != FieldSecret && field.Type != FieldText {
			return Field{}, fmt.Errorf("%w: field %q: generate applies to text and secret fields only", ErrValidation, key)
		}
		if hasPattern {
			return Field{}, fmt.Errorf("%w: field %q: generated fields must not declare a pattern", ErrValidation, key)
		}
		if field.Type == FieldSecret && field.MaxLength != 0 && field.MaxLength < minGeneratedLength {
			return Field{}, fmt.Errorf("%w: field %q: generated secrets need max_length of at least %d", ErrValidation, key, minGeneratedLength)
		}
	}
	switch field.Type {
	case FieldText, FieldSecret:
		if hasBounds {
			return Field{}, fmt.Errorf("%w: field %q: min and max apply to number fields", ErrValidation, key)
		}
		if hasOptions {
			return Field{}, fmt.Errorf("%w: field %q: options apply to select fields", ErrValidation, key)
		}
		if hasPattern {
			if len(field.Pattern) > maxPatternLength {
				return Field{}, fmt.Errorf("%w: field %q: pattern is longer than %d characters", ErrValidation, key, maxPatternLength)
			}
			compiled, err := regexp.Compile("^(?:" + field.Pattern + ")$")
			if err != nil {
				return Field{}, fmt.Errorf("%w: field %q: invalid pattern: %v", ErrValidation, key, err)
			}
			field.compiled = compiled
		}
		if field.MaxLength < 0 || field.MaxLength > MaxFieldValue {
			return Field{}, fmt.Errorf("%w: field %q: max_length must be between 1 and %d", ErrValidation, key, MaxFieldValue)
		}
	case FieldNumber:
		if hasPattern || field.MaxLength != 0 || hasOptions {
			return Field{}, fmt.Errorf("%w: field %q: number fields accept min and max only", ErrValidation, key)
		}
		if field.Min != nil && field.Max != nil && *field.Min > *field.Max {
			return Field{}, fmt.Errorf("%w: field %q: min is greater than max", ErrValidation, key)
		}
	case FieldSelect:
		if hasPattern || field.MaxLength != 0 || hasBounds {
			return Field{}, fmt.Errorf("%w: field %q: select fields accept options only", ErrValidation, key)
		}
		if len(raw.Options) == 0 || len(raw.Options) > maxOptions {
			return Field{}, fmt.Errorf("%w: field %q: options must declare 1-%d entries", ErrValidation, key, maxOptions)
		}
		field.Options = make([]string, 0, len(raw.Options))
		optionSeen := make(map[string]bool, len(raw.Options))
		for _, option := range raw.Options {
			if option == "" || option != strings.TrimSpace(option) || utf8.RuneCountInString(option) > maxOptionLength {
				return Field{}, fmt.Errorf("%w: field %q: options must be non-empty, trimmed strings of at most %d characters", ErrValidation, key, maxOptionLength)
			}
			if optionSeen[option] {
				return Field{}, fmt.Errorf("%w: field %q: duplicate option %q", ErrValidation, key, option)
			}
			optionSeen[option] = true
			field.Options = append(field.Options, option)
		}
	case FieldBool:
		if hasPattern || field.MaxLength != 0 || hasBounds || hasOptions {
			return Field{}, fmt.Errorf("%w: field %q: bool fields accept no validation keys", ErrValidation, key)
		}
	default:
		return Field{}, fmt.Errorf("%w: field %q: type must be one of text, secret, number, select, bool", ErrValidation, key)
	}

	if field.Type == FieldSecret {
		if !field.Required {
			return Field{}, fmt.Errorf("%w: field %q: secret fields must be required, a secret is never pre-filled", ErrValidation, key)
		}
		if raw.Default != nil {
			return Field{}, fmt.Errorf("%w: field %q: secret fields must not declare a default", ErrValidation, key)
		}
	}
	if raw.Default != nil {
		text, err := scalarDefault(raw.Default)
		if err != nil {
			return Field{}, fmt.Errorf("%w: field %q: %v", ErrValidation, key, err)
		}
		if _, err := field.check(text); err != nil {
			return Field{}, fmt.Errorf("%w: field %q: invalid default: %v", ErrValidation, key, err)
		}
		field.Default = &text
	} else if !field.Required {
		return Field{}, fmt.Errorf("%w: field %q: a field that is not required must declare a default", ErrValidation, key)
	}
	return field, nil
}

func rejectSurrounding(raw, trimmed, what string) error {
	if raw != trimmed {
		return fmt.Errorf("%w: %s must not carry surrounding whitespace", ErrValidation, what)
	}
	return nil
}

func scalarDefault(raw any) (string, error) {
	switch value := raw.(type) {
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case int:
		return strconv.Itoa(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case uint64:
		return strconv.FormatUint(value, 10), nil
	case float64:
		if value != math.Trunc(value) || math.Abs(value) > 1<<53 {
			return "", fmt.Errorf("default must be a whole number")
		}
		return strconv.FormatInt(int64(value), 10), nil
	default:
		return "", fmt.Errorf("default must be a string, number or boolean")
	}
}
