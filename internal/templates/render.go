package templates

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/services"
)

// RenderResult is one rendered template: the compose document plus the parsed
// view the control plane reports (service names, domain routes, named volumes,
// mounts).
type RenderResult struct {
	// ComposeYAML is the document with every placeholder resolved, ready to
	// be created and deployed through the services surface unchanged.
	ComposeYAML string
	// Env carries the values of the template's secret fields, keyed by field
	// key. Secret values are never written into the document: it references
	// them as ${field}, so the services pipeline's interpolation and
	// redaction boundary (BE-7.1) covers them from create through deploy,
	// including every error and the deploy history. The caller must pass Env
	// to services.Create alongside ComposeYAML.
	Env map[string]string
	// Spec is the services view of ComposeYAML.
	Spec services.ComposeSpec
}

// Render validates values against the template's form and renders the compose
// document. Actually rendering a document is the same computation as
// validating one, so there is no separate validate-only entry point.
func (c *Catalog) Render(slug string, values map[string]any) (RenderResult, error) {
	template, ok := c.bySlug[slug]
	if !ok {
		return RenderResult{}, notFound(slug)
	}
	return template.Render(values)
}

// Render validates values against the template's fields and substitutes them
// into the compose document. Validation is strict: an unknown field, a missing
// required value, a wrong type or a value failing its own rules is an error,
// and the result is parsed with services.Parse before it is returned.
//
// Secret fields are rendered as ${field} references and returned in Env, so a
// secret never enters the document, an error message or the deploy history;
// the services pipeline substitutes and redacts it exactly like a service
// environment value. Every error this method returns is additionally passed
// through services.RedactError with the supplied secret values (raw and
// dollar-escaped forms), so no error can echo a secret even if a future
// change places one in a parse-visible position.
func (t Template) Render(values map[string]any) (RenderResult, error) {
	known := make(map[string]bool, len(t.Fields))
	for _, field := range t.Fields {
		known[field.Key] = true
	}
	unknown := make([]string, 0)
	for key := range values {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return RenderResult{}, fmt.Errorf("%w: template %s: unknown field %q", ErrValidation, t.Slug, unknown[0])
	}

	env := make(map[string]string)
	secrets := make(map[string]string)
	secretFields := make(map[string]bool)
	resolved := make(map[string]string, len(t.Fields))
	for _, field := range t.Fields {
		value, err := field.resolve(values)
		if err != nil {
			return RenderResult{}, services.RedactError(fmt.Errorf("template %s: %w", t.Slug, err), secrets)
		}
		resolved[field.Key] = value
		if field.Type == FieldSecret {
			env[field.Key] = value
			secrets[field.Key] = value
			secretFields[field.Key] = true
		}
	}

	node, err := decodeDocument(t.compose)
	if err != nil {
		return RenderResult{}, services.RedactError(
			fmt.Errorf("%w: template %s: compose.yaml: %v", ErrValidation, t.Slug, err), secrets)
	}
	// The rendered size is enforced while the document is built, not after:
	// a small document with repeated references would otherwise allocate an
	// oversized string before any limit applies (the services pipeline
	// enforces its own limit the same way).
	remaining := services.MaxComposeYAML
	err = walkScalars(node, func(value string) (string, error) {
		if !strings.Contains(value, "{{") {
			if len(value) > remaining {
				return "", documentTooLarge()
			}
			remaining -= len(value)
			return value, nil
		}
		return substitute(value, &remaining, func(key string) (string, error) {
			fieldValue, ok := resolved[key]
			if !ok {
				return "", fmt.Errorf("%w: unknown field %q", ErrValidation, key)
			}
			if secretFields[key] {
				// A secret never enters the document: the reference is
				// resolved (and redacted) by the services pipeline.
				return "${" + key + "}", nil
			}
			// Escape for the services pipeline's own ${VAR} interpolation:
			// the literal value reaches the container unchanged, and a value
			// can never be read as a reference.
			return escapeDollar(fieldValue), nil
		})
	})
	if err != nil {
		return RenderResult{}, services.RedactError(fmt.Errorf("template %s: %w", t.Slug, err), secrets)
	}

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return RenderResult{}, services.RedactError(fmt.Errorf("template %s: render compose document: %w", t.Slug, err), secrets)
	}
	if err := encoder.Close(); err != nil {
		return RenderResult{}, services.RedactError(fmt.Errorf("template %s: render compose document: %w", t.Slug, err), secrets)
	}
	document := buffer.String()
	if len(document) > services.MaxComposeYAML {
		return RenderResult{}, services.RedactError(fmt.Errorf("template %s: %w", t.Slug, documentTooLarge()), secrets)
	}
	spec, err := services.Parse(document)
	if err != nil {
		return RenderResult{}, services.RedactError(fmt.Errorf("template %s: %w", t.Slug, err), secrets)
	}
	return RenderResult{ComposeYAML: document, Env: env, Spec: spec}, nil
}

// resolve returns the effective value of one field: the supplied value, or the
// field's default when absent.
func (f Field) resolve(values map[string]any) (string, error) {
	raw, present := values[f.Key]
	if !present {
		if f.Default != nil {
			return f.check(*f.Default)
		}
		return "", fmt.Errorf("%w: field %q is required", ErrValidation, f.Key)
	}
	text, err := scalarValue(raw)
	if err != nil {
		return "", fmt.Errorf("%w: field %q: %v", ErrValidation, f.Key, err)
	}
	return f.check(text)
}

// check applies the field's type and its type-specific rules, returning the
// canonical value. The validator runs it on every default at load, so a
// template default can never fail here for a loaded template.
func (f Field) check(text string) (string, error) {
	if len(text) > MaxFieldValue {
		return "", fmt.Errorf("%w: field %q exceeds %d bytes", ErrValidation, f.Key, MaxFieldValue)
	}
	switch f.Type {
	case FieldText, FieldSecret:
		if f.Required && strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("%w: field %q is required", ErrValidation, f.Key)
		}
		if f.MaxLength > 0 && utf8.RuneCountInString(text) > f.MaxLength {
			return "", fmt.Errorf("%w: field %q must be at most %d characters", ErrValidation, f.Key, f.MaxLength)
		}
		if f.compiled != nil && !f.compiled.MatchString(text) {
			return "", fmt.Errorf("%w: field %q does not match %s", ErrValidation, f.Key, f.Pattern)
		}
		return text, nil
	case FieldNumber:
		trimmed := strings.TrimSpace(text)
		value, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return "", fmt.Errorf("%w: field %q must be a whole number", ErrValidation, f.Key)
		}
		if f.Min != nil && value < *f.Min {
			return "", fmt.Errorf("%w: field %q must be at least %d", ErrValidation, f.Key, *f.Min)
		}
		if f.Max != nil && value > *f.Max {
			return "", fmt.Errorf("%w: field %q must be at most %d", ErrValidation, f.Key, *f.Max)
		}
		return strconv.FormatInt(value, 10), nil
	case FieldBool:
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true":
			return "true", nil
		case "false":
			return "false", nil
		default:
			return "", fmt.Errorf("%w: field %q must be true or false", ErrValidation, f.Key)
		}
	case FieldSelect:
		value := strings.TrimSpace(text)
		for _, option := range f.Options {
			if option == value {
				return value, nil
			}
		}
		return "", fmt.Errorf("%w: field %q must be one of %s", ErrValidation, f.Key, strings.Join(f.Options, ", "))
	default:
		return "", fmt.Errorf("%w: field %q has unsupported type %q", ErrValidation, f.Key, f.Type)
	}
}

// scalarValue renders a JSON-supplied value as the string a form field carries.
// Only scalars are accepted; numbers must be whole so no precision is lost.
func scalarValue(raw any) (string, error) {
	switch value := raw.(type) {
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case int:
		return strconv.Itoa(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case float64:
		if value != math.Trunc(value) || math.Abs(value) > 1<<53 {
			return "", errors.New("must be a whole number")
		}
		return strconv.FormatInt(int64(value), 10), nil
	case json.Number:
		return value.String(), nil
	default:
		return "", errors.New("must be a string, number or boolean")
	}
}

// decodeDocument parses a compose document into its YAML node tree. The input
// must contain exactly one YAML document: a second document (or a null
// trailing one) would otherwise be dropped silently by the re-encode.
func decodeDocument(document string) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(document))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("compose document must contain exactly one document")
	}
	return &node, nil
}

// walkScalars visits every string scalar of a YAML tree and replaces it with
// the visitor's result. A non-string scalar (a number, a boolean, null) cannot
// carry a placeholder and is left untouched; aliases are skipped because their
// anchor is already visited.
func walkScalars(node *yaml.Node, visit func(string) (string, error)) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			if err := walkScalars(child, visit); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag != "" && node.Tag != "!!str" {
			return nil
		}
		rendered, err := visit(node.Value)
		if err != nil {
			return err
		}
		if rendered != node.Value {
			node.Value = rendered
			node.Tag = "!!str"
			node.Style = 0
		}
	case yaml.AliasNode:
	}
	return nil
}

// substitute replaces every {{ .field }} placeholder of one scalar with the
// value resolve returns for its key. budget is the remaining rendered-size
// budget shared by the whole document: a scalar that would exceed it fails
// before the oversized string is built, exactly like services.Interpolate. The
// syntax is deliberately the only supported one: there are no functions, no
// conditionals, no nested templates and no escaping construct, so a template
// can never execute anything.
func substitute(source string, budget *int, resolve func(key string) (string, error)) (string, error) {
	if !strings.Contains(source, "{{") {
		if strings.Contains(source, "}}") {
			return "", fmt.Errorf("%w: %q contains \"}}\" without an opening placeholder", ErrValidation, source)
		}
		if len(source) > *budget {
			return "", documentTooLarge()
		}
		*budget -= len(source)
		return source, nil
	}
	var out strings.Builder
	out.Grow(min(len(source), *budget))
	write := func(segment string) error {
		if len(segment) > *budget-out.Len() {
			return documentTooLarge()
		}
		out.WriteString(segment)
		return nil
	}
	rest := source
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			if err := write(rest); err != nil {
				return "", err
			}
			break
		}
		if err := write(rest[:start]); err != nil {
			return "", err
		}
		end := strings.Index(rest[start+2:], "}}")
		if end < 0 {
			return "", fmt.Errorf("%w: unterminated placeholder in %q", ErrValidation, source)
		}
		body := rest[start+2 : start+2+end]
		key, ok := placeholderKey(body)
		if !ok {
			return "", fmt.Errorf("%w: %q is not a supported placeholder; only {{ .field }} references are allowed", ErrValidation, "{{"+body+"}}")
		}
		value, err := resolve(key)
		if err != nil {
			return "", err
		}
		if err := write(value); err != nil {
			return "", err
		}
		rest = rest[start+2+end+2:]
	}
	*budget -= out.Len()
	return out.String(), nil
}

// documentTooLarge reports a document that exceeded the rendered-size bound.
func documentTooLarge() error {
	return fmt.Errorf("%w: rendered compose document exceeds %d bytes", ErrValidation, services.MaxComposeYAML)
}

// placeholderKey parses a placeholder body: optional whitespace around a
// single ".key" field reference.
func placeholderKey(body string) (string, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, ".") {
		return "", false
	}
	key := body[1:]
	if !fieldKeyPattern.MatchString(key) {
		return "", false
	}
	return key, true
}

// escapeDollar doubles every literal dollar, so a substituted value passes the
// services pipeline's ${VAR} interpolation unchanged.
func escapeDollar(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}
