package templates

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Bounds of one template definition. They exist so a malformed or hostile
// template file cannot make the catalog load or a render unbounded.
const (
	// MaxTemplateYAML bounds one template.yaml document.
	MaxTemplateYAML = 64 << 10 // 64 KiB
	// MaxNameLength bounds the display name.
	MaxNameLength = 80
	// MaxDescriptionLength bounds the card description.
	MaxDescriptionLength = 280
	// MaxFields bounds one template's form.
	MaxFields = 32
	// MaxFieldValue bounds one supplied or default field value, in bytes.
	MaxFieldValue = 1024
	// maxLabelLength bounds a field label.
	maxLabelLength = 80
	// maxPlaceholderLength bounds a field placeholder.
	maxPlaceholderLength = 200
	// maxHelpLength bounds a field help text.
	maxHelpLength = 500
	// maxPatternLength bounds a field validation pattern.
	maxPatternLength = 256
	// maxOptions bounds a select field's option count.
	maxOptions = 64
	// maxOptionLength bounds one select option.
	maxOptionLength = 128
)

// slugPattern is the template directory alphabet.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// fieldKeyPattern is the field key alphabet: it is also the environment-variable
// shape, so a template author never fights quoting.
var fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// iconPattern is the icon key alphabet the gallery resolves against its icon set.
var iconPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// FieldType is the input type of one form field.
type FieldType string

const (
	// FieldText — a free-form string.
	FieldText FieldType = "text"
	// FieldSecret — a free-form string the UI masks; never has a default.
	FieldSecret FieldType = "secret"
	// FieldNumber — a whole number, optionally bounded by Min/Max.
	FieldNumber FieldType = "number"
	// FieldSelect — one of a fixed Options set.
	FieldSelect FieldType = "select"
	// FieldBool — true or false.
	FieldBool FieldType = "bool"
)

// Field is one form field of a template. Default is the value used when the
// caller supplies none; the validator requires it on every non-required
// field, so a render with no input is always possible for optional values.
// Type-specific keys are only valid for their type: Pattern and MaxLength for
// text/secret, Min and Max for number, Options for select.
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

	// compiled is Pattern anchored to a full-value match. Unexported: the
	// HTTP representation never exposes it.
	compiled *regexp.Regexp
}

// Template is one validated template: catalog metadata, the form schema and
// the compose document with {{ .field }} placeholders.
type Template struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Icon        string  `json:"icon"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`

	// compose is the raw compose.yaml with unresolved placeholders.
	compose string
}

// templateDocument is the template.yaml wire shape. Unknown keys are rejected
// so a typo cannot silently drop configuration.
type templateDocument struct {
	Name        string          `yaml:"name"`
	Icon        string          `yaml:"icon"`
	Description string          `yaml:"description"`
	Fields      []fieldDocument `yaml:"fields"`
}

// fieldDocument is one field entry of template.yaml.
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
}

// parseTemplate validates one template.yaml document and returns the template
// definition (without the compose document, which loadTemplate adds).
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
	if err := decoder.Decode(&extra); err == nil && extra != nil {
		return Template{}, fmt.Errorf("%w: template %s: template.yaml must contain exactly one document", ErrValidation, slug)
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
	return Template{Slug: slug, Name: name, Icon: icon, Description: description, Fields: fields}, nil
}

// parseField validates one field entry: known type, type-appropriate
// validation keys, a default that itself passes the field's own rules, and the
// secret constraints (a secret is required and is never pre-filled).
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

// rejectSurrounding rejects a value the YAML decoder kept but that carries
// surrounding whitespace, which would silently change a key or an option.
func rejectSurrounding(raw, trimmed, what string) error {
	if raw != trimmed {
		return fmt.Errorf("%w: %s must not carry surrounding whitespace", ErrValidation, what)
	}
	return nil
}

// scalarDefault converts a YAML scalar default to its canonical string form.
// Mappings, sequences and null are rejected: a form field has a scalar value.
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
