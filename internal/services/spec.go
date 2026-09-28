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

// Constants of the Gotham compose convention.
const (
	// ProjectPrefix names every compose project: "gotham-<service uuid>". The
	// node agent re-validates the whole name against the same strict pattern,
	// so it can never become a path.
	ProjectPrefix = "gotham-"
	// LabelDomain marks one compose service as publicly routed. It is
	// validated through the Phase 6 proxy rules, so a label can never escape
	// the generated Traefik rule.
	LabelDomain = "gotham.domain"
	// LabelDomainPort is the container port the domain's route targets;
	// folded into DomainRoute.Port. DefaultDefaultDomainPort when absent.
	LabelDomainPort = "gotham.domain.port"
	// DefaultDomainPort is the container port used when a routed service
	// declares no LabelDomainPort.
	DefaultDomainPort int32 = 80
	// MaxComposeYAML bounds one stored document.
	MaxComposeYAML = 1 << 20 // 1 MiB
	// MaxEnvVars bounds the substitution environment of one service.
	MaxEnvVars = 256
	// ReservedVolumePrefix is the managed-database volume prefix (Phase 5,
	// BE-5.1). It is duplicated rather than imported because the databases
	// package keeps it internal; a compose project must never reference a
	// managed database volume.
	ReservedVolumePrefix = "gotham-db-"
	// deployHistoryLimit bounds how many deploy rows a read returns.
	deployHistoryLimit = 50
)

// maxComposeServices bounds one document's service count, matching the
// agent's validation bound.
const maxComposeServices = 256

// composeServiceNamePattern is the compose service identifier alphabet.
var composeServiceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// namedVolumePattern matches a named volume reference (no path separators, no
// leading dot).
var namedVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ComposeSpec is the control-plane view of one validated compose document: the
// parts this package must understand (routing, storage, environment shape).
// Everything else is left to the node's `docker compose config`, so the two
// layers cannot disagree about the full schema.
type ComposeSpec struct {
	// Services are the declared compose service names, sorted.
	Services []string
	// Domains are the routed services, sorted by compose service name.
	Domains []DomainRoute
	// NamedVolumes are the project's named volumes (declared top-level and
	// referenced by services), sorted.
	NamedVolumes []string
	// Mounts are every resolved service mount, sorted by service then target.
	Mounts []StorageMount
}

// RenderedSpec is a document ready for the node agent: the interpolated
// compose text plus the parsed view the control plane reports.
type RenderedSpec struct {
	// ComposeYAML is the rendered document with every literal dollar escaped
	// as `$$`, so the node's compose run performs no further interpolation.
	ComposeYAML string
	// Spec is the parsed view of the rendered document.
	Spec ComposeSpec
}

// ProjectName returns the compose project name of a service: "gotham-<id>".
func ProjectName(id uuid.UUID) string {
	return ProjectPrefix + id.String()
}

// Render interpolates document against env and validates the result. An
// unresolvable `${VAR}` without a default is an error, exactly like compose's
// own interpolation.
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

// Validate renders and parses a document without deploying it. It is the check
// Create and Update run so a stored service is always renderable.
func Validate(document string, env map[string]string) error {
	_, err := Render(document, env)
	return err
}

// Interpolate resolves `${VAR}` references in every string scalar of document
// against env and returns the rendered document.
//
// Supported forms are compose's: `$VAR`, `${VAR}`, `${VAR:-default}` and
// `${VAR:?message}` (unset or empty), `${VAR-default}` and `${VAR?message}`
// (unset only), plus `${VAR:+alt}` and `${VAR+alt}`. `$$` is a literal dollar.
// An unset variable outside a default or error form is an error, and every
// literal dollar in the output is escaped as `$$` so the node's compose run
// never interpolates a substituted value a second time. Nested references
// (`${A:-${B}}`) are not supported.
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
	// The rendered size is enforced while the document is built, not after:
	// a small document with repeated references would otherwise expand to
	// allocate an unbounded string before any limit applies.
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

// interpolateNode walks a YAML node tree and substitutes every string scalar.
// remaining is the rendered-size budget shared by every scalar; it is
// decremented as values are rendered so a document of repeated references
// stops at the document limit instead of expanding without bound.
func interpolateNode(node *yaml.Node, env map[string]string, remaining *int) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			if err := interpolateNode(child, env, remaining); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag == "" || node.Tag == "!!str" {
			rendered, err := interpolateString(node.Value, env, *remaining)
			if err != nil {
				return err
			}
			*remaining -= len(rendered)
			node.Value = rendered
			node.Tag = "!!str"
			node.Style = 0
		}
	}
	return nil
}

// interpolateString resolves one scalar value. See Interpolate for the
// supported forms. limit is the remaining rendered-size budget: a value that
// would exceed it fails before the oversized string is built.
func interpolateString(value string, env map[string]string, limit int) (string, error) {
	var out strings.Builder
	appendValue := func(resolved string) error {
		// The budget counts the escaped form that is actually written: a
		// dollar-rich value can double in size.
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
			// A short reference must start with a name character; "$5" is a
			// literal dollar, exactly like compose treats it.
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

// resolveReference resolves the inside of a `${...}` expression: a name, an
// optional modifier and its argument.
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

// splitReference parses a `${...}` body: the variable name first (its full
// run of name characters), then the operator immediately at the name's end,
// with everything after the operator treated as the argument. Scanning the
// name first is what keeps operator characters inside an argument from being
// mistaken for the operator: `${UNSET-http://host:-fallback}` is the unset
// default `http://host:-fallback`, not a nested `:-`.
func splitReference(expression string) (name, modifier, argument string) {
	end := 0
	for end < len(expression) && isEnvKeyChar(expression[end]) {
		end++
	}
	name = expression[:end]
	rest := expression[end:]
	// Two-character operators come first so ":-" is never read as "-" with a
	// ":"-prefixed argument.
	for _, candidate := range []string{":-", ":?", ":+", "-", "?", "+"} {
		if strings.HasPrefix(rest, candidate) {
			return name, candidate, rest[len(candidate):]
		}
	}
	return name, "", rest
}

// isEnvKeyChar reports whether c may appear in an environment variable name.
func isEnvKeyChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// isEnvKeyStart reports whether c may start an environment variable name.
func isEnvKeyStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// escapeDollar doubles every literal dollar so compose's own interpolation
// renders it unchanged.
func escapeDollar(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}

// escapedLen returns the length of value's escaped rendering without building
// it.
func escapedLen(value string) int {
	return len(value) + strings.Count(value, "$")
}

// Parse validates the schema subset this package must understand and returns
// the control-plane view of the document: service names, the domain map and
// the storage mounts. Unsupported constructs that cannot work on a node the
// control plane only sends YAML to (build contexts, file includes, external
// env files, service extends) are rejected here with a clear message instead
// of failing later on the node.
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
	if len(doc.Services) > maxComposeServices {
		return ComposeSpec{}, fmt.Errorf("%w: compose document declares more than %d services", ErrValidation, maxComposeServices)
	}

	spec := ComposeSpec{Mounts: []StorageMount{}, Domains: []DomainRoute{}}
	declaredVolumes := make(map[string]bool, len(doc.Volumes))
	for name, definition := range doc.Volumes {
		// The alias a service references is validated, and so is the
		// effective Docker volume name the definition resolves to: `name:`
		// (with or without `external`) can point an innocent-looking alias at
		// a managed database volume.
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

	domains := make(map[string]string, len(serviceNames)) // domain -> compose service
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
			if !declaredVolumes[volume] {
				return ComposeSpec{}, fmt.Errorf(
					"%w: service %q references named volume %q without a top-level volumes declaration",
					ErrValidation, name, volume)
			}
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

// composeDocument is the subset of a compose document this package decodes.
// Unknown keys are ignored (the node's CLI validates them); the fields below
// exist because the control plane must either understand them or reject them.
type composeDocument struct {
	Name     string                    `yaml:"name"`
	Include  any                       `yaml:"include"`
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
	Networks map[string]any            `yaml:"networks"`
}

// composeService is the subset of a compose service this package decodes.
type composeService struct {
	Image       string `yaml:"image"`
	Build       any    `yaml:"build"`
	Extends     any    `yaml:"extends"`
	EnvFile     any    `yaml:"env_file"`
	Labels      any    `yaml:"labels"`
	Volumes     any    `yaml:"volumes"`
	Environment any    `yaml:"environment"`
}

// validateEnvironment checks the shape of a service environment: a mapping of
// scalars or a list of KEY=VALUE strings, like compose accepts. Values are
// never inspected or recorded.
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

// parseLabels normalizes compose's two label shapes (mapping or list) into one
// map. Label values are scalars converted to their string form, exactly as
// compose does.
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

// scalarString renders a YAML scalar the way compose interpolates it.
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

// domainRoute returns the routing declaration of one compose service.
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

// parseServiceMounts resolves one service's mounts and returns the named
// volumes it references. Compose accepts the short "source:target[:mode]"
// string form and the long mapping form; both are normalized here.
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
			// A bare name (no slash, no leading dot) is a named volume.
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

// parseMount normalizes one volume entry.
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

// parseShortMount parses compose's "source:target[:mode]" volume form.
func parseShortMount(service, entry string) (StorageMount, error) {
	mount := StorageMount{Service: service}
	parts := strings.Split(entry, ":")
	switch len(parts) {
	case 1:
		// An anonymous volume: the single part is the container path.
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

// validateNamedVolume rejects the managed database volume namespace; without
// it a compose project could mount (and a forced removal later delete) a
// managed database's volume.
func validateNamedVolume(name string) error {
	if strings.HasPrefix(name, ReservedVolumePrefix) {
		return fmt.Errorf("%w: volume name %q is reserved for managed databases", ErrValidation, name)
	}
	return nil
}

// effectiveVolumeNames returns every Docker volume name a top-level volume
// definition can resolve to: the alias key itself, a `name:` override and the
// `external:` name forms (`external: true` uses the key; `external: {name: …}`
// names another volume). Compose resolves the definition this way, so guarding
// only the key would let an alias mount a managed database volume.
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
		// `external: true` resolves to the key, already covered.
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

// Redact replaces every environment value of env in message with
// "<redacted>", longest first, so an error that quotes a substituted value
// never reaches an API response, a deploy row or a log line. Both the raw
// value and its dollar-escaped rendering (`x$y` is written as `x$$y` into the
// rendered document) are replaced, and no value is treated as too short to
// protect: the environment map carries no secret/non-secret distinction, so a
// short value is redacted exactly like a long one. A secret the user typed
// inline into the compose YAML is their own content and is out of scope here.
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

// RedactError redacts err's message while preserving its error chain, so
// errors.Is keeps matching the sentinel the service wrapped. It returns nil
// for a nil error so callers can chain it unconditionally.
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

// redactedError carries a redacted message over the original error chain.
type redactedError struct {
	err     error
	message string
}

// Error returns the redacted message.
func (e *redactedError) Error() string { return e.message }

// Unwrap keeps errors.Is/As working against the original error.
func (e *redactedError) Unwrap() error { return e.err }
