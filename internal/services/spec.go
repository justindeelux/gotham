package services

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/proxy"
)

const (
	ProjectPrefix              = "gotham-"
	LabelDomain                = "gotham.domain"
	LabelDomainPort            = "gotham.domain.port"
	DefaultDomainPort    int32 = 80
	MaxComposeYAML             = 1 << 20
	MaxEnvVars                 = 256
	ReservedVolumePrefix       = "gotham-db-"
	deployHistoryLimit         = 50
)

const MaxComposeServices = 256

var composeServiceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

var namedVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

type ComposeSpec struct {
	Services     []string
	Domains      []DomainRoute
	NamedVolumes []string
	Mounts       []StorageMount
}

type RenderedSpec struct {
	ComposeYAML string
	Spec        ComposeSpec
}

func ProjectName(id uuid.UUID) string {
	return ProjectPrefix + id.String()
}

func Render(document string, env map[string]string) (RenderedSpec, error) {
	rendered, err := Interpolate(document, env)
	if err != nil {
		return RenderedSpec{}, err
	}
	spec, err := Parse(rendered)
	if err != nil {
		return RenderedSpec{}, err
	}
	return RenderedSpec{ComposeYAML: rendered, Spec: spec}, nil
}

func Validate(document string, env map[string]string) error {
	_, err := Render(document, env)
	return err
}

func Interpolate(document string, env map[string]string) (string, error) {
	if strings.TrimSpace(document) == "" {
		return "", fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	if len(document) > MaxComposeYAML {
		return "", fmt.Errorf("%w: compose document exceeds %d bytes", ErrValidation, MaxComposeYAML)
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(document), &node); err != nil {
		return "", fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	remaining := MaxComposeYAML
	if err := interpolateNode(&node, env, &remaining); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	return buf.String(), nil
}

func interpolateNode(node *yaml.Node, env map[string]string, remaining *int) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			if err := interpolateNode(child, env, remaining); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return nil
		}
		rendered, err := interpolateString(node.Value, env, *remaining)
		if err != nil {
			return err
		}
		*remaining -= len(rendered)
		node.Value = rendered
		node.Tag = "!!str"
		node.Style = 0
	}
	return nil
}

func interpolateString(value string, env map[string]string, limit int) (string, error) {
	var out strings.Builder
	appendValue := func(resolved string) error {
		if escapedLen(resolved) > limit-out.Len() {
			return fmt.Errorf("%w: rendered compose document exceeds %d bytes", ErrValidation, MaxComposeYAML)
		}
		out.WriteString(escapeDollar(resolved))
		return nil
	}
	tooLarge := func() error {
		return fmt.Errorf("%w: rendered compose document exceeds %d bytes", ErrValidation, MaxComposeYAML)
	}
	for i := 0; i < len(value); {
		if value[i] != '$' {
			if out.Len()+1 > limit {
				return "", tooLarge()
			}
			out.WriteByte(value[i])
			i++
			continue
		}
		if i+1 >= len(value) {
			if out.Len()+2 > limit {
				return "", tooLarge()
			}
			out.WriteString("$$")
			i++
			continue
		}
		switch next := value[i+1]; next {
		case '$':
			if out.Len()+2 > limit {
				return "", tooLarge()
			}
			out.WriteString("$$")
			i += 2
		case '{':
			end := strings.IndexByte(value[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("%w: unterminated ${...} reference", ErrValidation)
			}
			resolved, err := resolveReference(value[i+2:i+2+end], env)
			if err != nil {
				return "", err
			}
			if err := appendValue(resolved); err != nil {
				return "", err
			}
			i += 2 + end + 1
		default:
			if !isEnvKeyStart(value[i+1]) {
				if out.Len()+2 > limit {
					return "", tooLarge()
				}
				out.WriteString("$$")
				i++
				continue
			}
			end := i + 1
			for end < len(value) && isEnvKeyChar(value[end]) {
				end++
			}
			name := value[i+1 : end]
			resolved, ok := env[name]
			if !ok {
				return "", fmt.Errorf("%w: %s is required", ErrValidation, name)
			}
			if err := appendValue(resolved); err != nil {
				return "", err
			}
			i = end
		}
	}
	return out.String(), nil
}

func resolveReference(expression string, env map[string]string) (string, error) {
	name, modifier, argument := splitReference(expression)
	if name == "" {
		return "", fmt.Errorf("%w: empty ${...} reference", ErrValidation)
	}
	if modifier == "" && argument != "" {
		return "", fmt.Errorf("%w: unsupported reference modifier in ${%s}", ErrValidation, expression)
	}
	value, set := env[name]
	nonEmpty := set && value != ""

	switch modifier {
	case "":
		if !set {
			return "", fmt.Errorf("%w: %s is required", ErrValidation, name)
		}
		return value, nil
	case ":-":
		if nonEmpty {
			return value, nil
		}
		return argument, nil
	case "-":
		if set {
			return value, nil
		}
		return argument, nil
	case ":?":
		if nonEmpty {
			return value, nil
		}
		if argument == "" {
			argument = name + " is required and has no default"
		}
		return "", fmt.Errorf("%w: %s", ErrValidation, argument)
	case "?":
		if set {
			return value, nil
		}
		if argument == "" {
			argument = name + " is required and has no default"
		}
		return "", fmt.Errorf("%w: %s", ErrValidation, argument)
	case ":+":
		if nonEmpty {
			return argument, nil
		}
		return "", nil
	case "+":
		if set {
			return argument, nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("%w: unsupported reference modifier %q", ErrValidation, modifier)
	}
}

func splitReference(expression string) (name, modifier, argument string) {
	end := 0
	for end < len(expression) && isEnvKeyChar(expression[end]) {
		end++
	}
	name = expression[:end]
	rest := expression[end:]
	for _, candidate := range []string{":-", ":?", ":+", "-", "?", "+"} {
		if strings.HasPrefix(rest, candidate) {
			return name, candidate, rest[len(candidate):]
		}
	}
	return name, "", rest
}

func isEnvKeyChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isEnvKeyStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func escapeDollar(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}

func escapedLen(value string) int {
	return len(value) + strings.Count(value, "$")
}

func Parse(document string) (ComposeSpec, error) {
	if strings.TrimSpace(document) == "" {
		return ComposeSpec{}, fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	if len(document) > MaxComposeYAML {
		return ComposeSpec{}, fmt.Errorf("%w: compose document exceeds %d bytes", ErrValidation, MaxComposeYAML)
	}
	var doc composeDocument
	decoder := yaml.NewDecoder(strings.NewReader(document))
	if err := decoder.Decode(&doc); err != nil {
		return ComposeSpec{}, fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	if doc.Include != nil {
		return ComposeSpec{}, fmt.Errorf("%w: include is not supported: the agent only receives this document", ErrValidation)
	}
	if len(doc.Services) == 0 {
		return ComposeSpec{}, fmt.Errorf("%w: compose document declares no services", ErrValidation)
	}
	if len(doc.Services) > MaxComposeServices {
		return ComposeSpec{}, fmt.Errorf("%w: compose document declares more than %d services", ErrValidation, MaxComposeServices)
	}

	spec := ComposeSpec{Mounts: []StorageMount{}, Domains: []DomainRoute{}}
	declaredVolumes := make(map[string]bool, len(doc.Volumes))
	for name, definition := range doc.Volumes {
		if err := validateNamedVolume(name); err != nil {
			return ComposeSpec{}, err
		}
		effective, err := effectiveVolumeNames(name, definition)
		if err != nil {
			return ComposeSpec{}, err
		}
		for _, candidate := range effective {
			if err := validateNamedVolume(candidate); err != nil {
				return ComposeSpec{}, err
			}
		}
		declaredVolumes[name] = true
	}
	serviceNames := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	domains := make(map[string]string, len(serviceNames))
	volumes := make(map[string]bool, len(declaredVolumes))
	for name := range declaredVolumes {
		volumes[name] = true
	}
	for name, service := range doc.Services {
		if !composeServiceNamePattern.MatchString(name) {
			return ComposeSpec{}, fmt.Errorf("%w: invalid compose service name %q", ErrValidation, name)
		}
		if service.Build != nil {
			return ComposeSpec{}, fmt.Errorf(
				"%w: service %q uses build, which needs a build context the control plane cannot ship; push an image instead",
				ErrValidation, name)
		}
		if service.Extends != nil {
			return ComposeSpec{}, fmt.Errorf("%w: service %q uses extends, which references a file the node does not have", ErrValidation, name)
		}
		if service.EnvFile != nil {
			return ComposeSpec{}, fmt.Errorf("%w: service %q uses env_file, which references a file the node does not have", ErrValidation, name)
		}
		if strings.TrimSpace(service.Image) == "" {
			return ComposeSpec{}, fmt.Errorf("%w: service %q declares no image", ErrValidation, name)
		}
		if err := validateEnvironment(service.Environment); err != nil {
			return ComposeSpec{}, fmt.Errorf("service %q: %w", name, err)
		}
		labels, err := parseLabels(service.Labels)
		if err != nil {
			return ComposeSpec{}, fmt.Errorf("service %q: %w", name, err)
		}
		route, routed, err := domainRoute(name, labels)
		if err != nil {
			return ComposeSpec{}, err
		}
		if routed {
			if owner, taken := domains[route.Domain]; taken {
				return ComposeSpec{}, fmt.Errorf(
					"%w: services %q and %q both declare %s %s", ErrValidation, owner, name, LabelDomain, route.Domain)
			}
			domains[route.Domain] = name
			spec.Domains = append(spec.Domains, route)
		}
		mounts, referenced, err := parseServiceMounts(name, service.Volumes)
		if err != nil {
			return ComposeSpec{}, err
		}
		for _, volume := range referenced {
			volumes[volume] = true
		}
		spec.Mounts = append(spec.Mounts, mounts...)
	}

	spec.Services = serviceNames
	spec.NamedVolumes = make([]string, 0, len(volumes))
	for name := range volumes {
		spec.NamedVolumes = append(spec.NamedVolumes, name)
	}
	sort.Strings(spec.NamedVolumes)
	sort.Slice(spec.Domains, func(i, j int) bool { return spec.Domains[i].Service < spec.Domains[j].Service })
	sort.Slice(spec.Mounts, func(i, j int) bool {
		if spec.Mounts[i].Service != spec.Mounts[j].Service {
			return spec.Mounts[i].Service < spec.Mounts[j].Service
		}
		return spec.Mounts[i].Target < spec.Mounts[j].Target
	})
	return spec, nil
}

type composeDocument struct {
	Name     string                    `yaml:"name"`
	Include  any                       `yaml:"include"`
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
	Networks map[string]any            `yaml:"networks"`
}

type composeService struct {
	Image       string `yaml:"image"`
	Build       any    `yaml:"build"`
	Extends     any    `yaml:"extends"`
	EnvFile     any    `yaml:"env_file"`
	Labels      any    `yaml:"labels"`
	Volumes     any    `yaml:"volumes"`
	Environment any    `yaml:"environment"`
}

func validateEnvironment(raw any) error {
	switch value := raw.(type) {
	case nil:
		return nil
	case map[string]any:
		return nil
	case []any:
		for _, entry := range value {
			if _, ok := entry.(string); !ok {
				return fmt.Errorf("%w: environment list entries must be KEY=VALUE strings", ErrValidation)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: environment must be a mapping or a list of KEY=VALUE strings", ErrValidation)
	}
}

func parseLabels(raw any) (map[string]string, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		labels := make(map[string]string, len(value))
		for key, rawValue := range value {
			labels[key] = scalarString(rawValue)
		}
		return labels, nil
	case []any:
		labels := make(map[string]string, len(value))
		for _, entry := range value {
			text, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("%w: label list entries must be KEY=VALUE strings", ErrValidation)
			}
			key, labelValue, found := strings.Cut(text, "=")
			if !found || strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("%w: label list entries must be KEY=VALUE strings", ErrValidation)
			}
			labels[strings.TrimSpace(key)] = labelValue
		}
		return labels, nil
	default:
		return nil, fmt.Errorf("%w: labels must be a mapping or a list of KEY=VALUE strings", ErrValidation)
	}
}

func scalarString(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func domainRoute(service string, labels map[string]string) (DomainRoute, bool, error) {
	raw, ok := labels[LabelDomain]
	if !ok || strings.TrimSpace(raw) == "" {
		return DomainRoute{}, false, nil
	}
	domain := proxy.NormalizeDomain(raw)
	if err := proxy.ValidateDomain(domain); err != nil {
		return DomainRoute{}, false, fmt.Errorf("%w: service %q: %v", ErrValidation, service, err)
	}
	port := DefaultDomainPort
	if rawPort, ok := labels[LabelDomainPort]; ok {
		parsed, err := strconv.ParseInt(strings.TrimSpace(rawPort), 10, 32)
		if err != nil || parsed < 1 || parsed > 65535 {
			return DomainRoute{}, false, fmt.Errorf(
				"%w: service %q: %s must be a port between 1 and 65535", ErrValidation, service, LabelDomainPort)
		}
		port = int32(parsed)
	}
	return DomainRoute{Service: service, Domain: domain, Port: port}, true, nil
}

func parseServiceMounts(service string, raw any) ([]StorageMount, []string, error) {
	if raw == nil {
		return nil, nil, nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: service %q: volumes must be a list", ErrValidation, service)
	}
	mounts := make([]StorageMount, 0, len(entries))
	volumes := []string{}
	for _, entry := range entries {
		mount, err := parseMount(service, entry)
		if err != nil {
			return nil, nil, err
		}
		if mount.Source != "" && namedVolumePattern.MatchString(mount.Source) {
			if err := validateNamedVolume(mount.Source); err != nil {
				return nil, nil, fmt.Errorf("service %q: %w", service, err)
			}
			mount.Named = true
			volumes = append(volumes, mount.Source)
		}
		mounts = append(mounts, mount)
	}
	return mounts, volumes, nil
}

func parseMount(service string, raw any) (StorageMount, error) {
	switch entry := raw.(type) {
	case string:
		return parseShortMount(service, entry)
	case map[string]any:
		mount := StorageMount{Service: service}
		if source, ok := entry["source"]; ok {
			mount.Source = scalarString(source)
		}
		if target, ok := entry["target"]; ok {
			mount.Target = scalarString(target)
		}
		if readonly, ok := entry["read_only"].(bool); ok {
			mount.ReadOnly = readonly
		}
		if mount.Target == "" {
			return StorageMount{}, fmt.Errorf("%w: service %q: volume mount has no target", ErrValidation, service)
		}
		return mount, nil
	default:
		return StorageMount{}, fmt.Errorf("%w: service %q: volume entries must be strings or mappings", ErrValidation, service)
	}
}

func parseShortMount(service, entry string) (StorageMount, error) {
	mount := StorageMount{Service: service}
	parts := strings.Split(entry, ":")
	switch len(parts) {
	case 1:
		mount.Target = parts[0]
	case 2, 3:
		mount.Source = parts[0]
		mount.Target = parts[1]
		if len(parts) == 3 {
			mount.ReadOnly = strings.Contains(parts[2], "ro")
		}
	default:
		return StorageMount{}, fmt.Errorf("%w: service %q: volume %q has too many segments", ErrValidation, service, entry)
	}
	if strings.TrimSpace(mount.Target) == "" {
		return StorageMount{}, fmt.Errorf("%w: service %q: volume mount has no target", ErrValidation, service)
	}
	return mount, nil
}

func validateNamedVolume(name string) error {
	if strings.HasPrefix(name, ReservedVolumePrefix) {
		return fmt.Errorf("%w: volume name %q is reserved for managed databases", ErrValidation, name)
	}
	return nil
}

func effectiveVolumeNames(key string, definition any) ([]string, error) {
	names := []string{key}
	if definition == nil {
		return names, nil
	}
	mapping, ok := definition.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: volume %q definition must be a mapping", ErrValidation, key)
	}
	if rawName, ok := mapping["name"]; ok {
		name, ok := rawName.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%w: volume %q declares an invalid name", ErrValidation, key)
		}
		names = append(names, strings.TrimSpace(name))
	}
	switch external := mapping["external"].(type) {
	case nil, bool:
	case map[string]any:
		if rawName, ok := external["name"]; ok {
			name, ok := rawName.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%w: volume %q declares an invalid external name", ErrValidation, key)
			}
			names = append(names, strings.TrimSpace(name))
		}
	default:
		return nil, fmt.Errorf("%w: volume %q declares an invalid external flag", ErrValidation, key)
	}
	return names, nil
}

func Redact(message string, env map[string]string) string {
	values := make([]string, 0, len(env)*2)
	for _, value := range env {
		if value == "" {
			continue
		}
		values = append(values, value)
		if escaped := escapeDollar(value); escaped != value {
			values = append(values, escaped)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		message = strings.ReplaceAll(message, value, "<redacted>")
	}
	return message
}

func RedactError(err error, env map[string]string) error {
	if err == nil {
		return nil
	}
	message := Redact(err.Error(), env)
	if message == err.Error() {
		return err
	}
	return &redactedError{err: err, message: message}
}

type redactedError struct {
	err     error
	message string
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.err }
