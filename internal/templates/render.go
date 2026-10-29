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

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/services"
)

type RenderResult struct {
	ComposeYAML string
	Env         map[string]string
	Spec        services.ComposeSpec
	// Domains holds the managed routes the template declares through its
	// routes list. A route whose domain field is empty is omitted.
	Domains []services.DomainRoute
}

func (c *Catalog) Render(slug string, values map[string]any) (RenderResult, error) {
	template, ok := c.bySlug[slug]
	if !ok {
		return RenderResult{}, notFound(slug)
	}
	return template.Render(values)
}

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
				return "${" + key + "}", nil
			}
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
	domains := make([]services.DomainRoute, 0, len(t.Routes))
	for _, route := range t.Routes {
		host := proxy.NormalizeDomain(resolved[route.DomainField])
		if host == "" {
			continue
		}
		if err := proxy.ValidateDomain(host); err != nil {
			return RenderResult{}, services.RedactError(
				fmt.Errorf("%w: field %q: %v", ErrValidation, route.DomainField, err), secrets)
		}
		domains = append(domains, services.DomainRoute{Service: route.Service, Port: route.Port, Domain: host})
	}
	return RenderResult{ComposeYAML: document, Env: env, Spec: spec, Domains: domains}, nil
}

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

func documentTooLarge() error {
	return fmt.Errorf("%w: rendered compose document exceeds %d bytes", ErrValidation, services.MaxComposeYAML)
}

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

func escapeDollar(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}
