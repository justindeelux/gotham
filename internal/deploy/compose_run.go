package deploy

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/services"
)

// Compose source limits (GS-8). The pasted text travels to the node as the
// project's compose file, so it is capped like the task requires; the
// in-repo file path is a short relative reference, never a document.
const (
	// MaxComposeBytes caps pasted compose text at 256 KiB.
	MaxComposeBytes = 256 << 10
	// MaxComposeFilePathBytes caps the in-repo compose file path.
	MaxComposeFilePathBytes = 256
)

// composeServiceNamePattern is the compose service identifier alphabet,
// mirroring the Services surface (internal/services).
var composeServiceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// ValidateComposeFilePath checks the in-repo compose file reference of a
// repo-backed compose application: a short relative path that cannot escape
// the checkout. The content itself is validated after the clone.
func ValidateComposeFilePath(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("%w: compose file path is required", ErrValidation)
	}
	if err := checkTextBytes(trimmed, "compose file path"); err != nil {
		return err
	}
	if len(trimmed) > MaxComposeFilePathBytes {
		return fmt.Errorf("%w: compose file path exceeds %d bytes", ErrValidation, MaxComposeFilePathBytes)
	}
	if filepath.IsAbs(trimmed) {
		return fmt.Errorf("%w: compose file path %q must be relative to the repository", ErrValidation, path)
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || cleaned == "/" ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: compose file path %q escapes the repository", ErrValidation, path)
	}
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == ".." {
			return fmt.Errorf("%w: compose file path %q escapes the repository", ErrValidation, path)
		}
	}
	return nil
}

// ValidateComposeService checks the web service name: structurally a compose
// service name, and a member of the document's parsed service list when the
// document is available (pasted content). Repo-backed applications pass an
// empty document: membership is enforced at deploy time, once the file is
// read from the checkout.
func ValidateComposeService(content, service string) error {
	trimmed := strings.TrimSpace(service)
	if trimmed == "" {
		return fmt.Errorf("%w: compose web service is required", ErrValidation)
	}
	if !composeServiceNamePattern.MatchString(trimmed) {
		return fmt.Errorf("%w: invalid compose service name %q", ErrValidation, service)
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	spec, err := services.Parse(content)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	for _, name := range spec.Services {
		if name == trimmed {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown compose service %q (available: %s)",
		ErrValidation, service, strings.Join(spec.Services, ", "))
}

// ComposeImages returns the sorted image references the document's services
// declare. The orchestrator pulls each one before `up`, so a redeploy is
// pull images + up -d.
func ComposeImages(content string) ([]string, error) {
	var doc composeAppDocument
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	seen := make(map[string]bool)
	var images []string
	for _, service := range doc.Services {
		image := strings.TrimSpace(service.Image)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		images = append(images, image)
	}
	sort.Strings(images)
	return images, nil
}

// composeAppDocument is the subset of a compose document the orchestrator
// decodes: service images for the pull list. Structural validation belongs
// to the confinement allowlist, not to this shape.
type composeAppDocument struct {
	Services map[string]composeAppService `yaml:"services"`
}

// composeAppService is the subset of a compose service the orchestrator
// decodes.
type composeAppService struct {
	Image string `yaml:"image"`
}

// ComposePortTarget is one domain's routing target in a compose document: the
// service that serves the host and the container port the proxy must reach.
type ComposePortTarget struct {
	Service string
	Port    int32
}

// InjectComposeDomainPorts ensures the rendered document publishes every
// domain's container port on the service it names. An existing routable
// mapping for the port is kept (the user's declaration wins); otherwise a
// bare container port is appended, letting Docker assign an ephemeral host
// port that the proxy resolves. An application with no domains changes
// nothing, and a target must name a service the document declares.
func InjectComposeDomainPorts(rendered string, targets []ComposePortTarget) (string, error) {
	if len(targets) == 0 {
		return rendered, nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		return "", fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	servicesMap, ok := doc["services"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("%w: compose document declares no services", ErrValidation)
	}
	byService := make(map[string][]string, len(targets))
	for _, target := range targets {
		if target.Port <= 0 {
			return "", fmt.Errorf("%w: compose service %q needs a container port", ErrValidation, target.Service)
		}
		if _, ok := servicesMap[target.Service]; !ok {
			return "", fmt.Errorf("%w: unknown compose service %q", ErrValidation, target.Service)
		}
		byService[target.Service] = append(byService[target.Service], strconv.Itoa(int(target.Port)))
	}
	changed := false
	for serviceName, ports := range byService {
		service, ok := servicesMap[serviceName].(map[string]any)
		if !ok {
			return "", fmt.Errorf("%w: compose service %q is not a mapping", ErrValidation, serviceName)
		}
		entries, err := composePortEntries(service["ports"])
		if err != nil {
			return "", fmt.Errorf("%w: compose service %q: %v", ErrValidation, serviceName, err)
		}
		for _, port := range ports {
			mapped := false
			for _, entry := range entries {
				if portMapped(entry, port) {
					mapped = true
					break
				}
			}
			if mapped {
				continue
			}
			entries = append(entries, port)
			changed = true
		}
		service["ports"] = entries
		servicesMap[serviceName] = service
	}
	if !changed {
		return rendered, nil
	}
	doc["services"] = servicesMap
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	return string(out), nil
}

// composePortEntries normalizes a service's ports field to its raw entries.
// An absent field means no mappings; a present non-list is rejected rather
// than silently replaced.
func composePortEntries(raw any) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("ports must be a list")
	}
	return entries, nil
}

// isLoopbackAddress reports whether a host binding is loopback-only: the
// proxy ignores loopback bindings, so a loopback-only user mapping must not
// suppress the injected routable one. An empty host binds all interfaces
// and is routable.
func isLoopbackAddress(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "127.") ||
		strings.EqualFold(trimmed, "localhost") || trimmed == "::1"
}

// portMapped reports whether a ports entry already publishes the container
// port on a routable binding. It understands ranges ("8000-8010:8000-8010",
// where a port inside the range counts as mapped), the [ip:]host:container
// forms (a loopback host does not count: the proxy ignores loopback
// bindings, so a loopback-only user mapping must not suppress the injected
// routable one) and the long syntax (a loopback host_ip does not count).
func portMapped(entry any, port string) bool {
	switch value := entry.(type) {
	case string:
		target := value
		if index := strings.LastIndex(target, "/"); index >= 0 {
			target = target[:index]
		}
		host := ""
		rest := strings.TrimSpace(target)
		if strings.HasPrefix(rest, "[") {
			// An IPv6 host binding brackets the address: [::1]:8080:3000.
			end := strings.Index(rest, "]")
			if end < 0 {
				return false
			}
			host = rest[1:end]
			rest = strings.TrimPrefix(rest[end+1:], ":")
		}
		parts := strings.Split(rest, ":")
		container := strings.TrimSpace(parts[len(parts)-1])
		if host == "" && len(parts) >= 2 {
			host = strings.Join(parts[:len(parts)-1], ":")
		}
		if strings.Contains(container, "-") {
			return portInRange(port, container) && !isLoopbackAddress(host)
		}
		return container == port && !isLoopbackAddress(host)
	case map[string]any:
		target := strings.TrimSpace(scalarString(value["target"]))
		host := ""
		if rawHost, ok := value["host_ip"]; ok {
			host = scalarString(rawHost)
		}
		return target == port && !isLoopbackAddress(host)
	default:
		return false
	}
}

// portInRange reports whether port falls inside a "lo-hi" range. A malformed
// range is the node's compose validation to refuse, never a mapping to
// trust, so it answers false.
func portInRange(port, span string) bool {
	bounds := strings.SplitN(span, "-", 2)
	if len(bounds) != 2 {
		return false
	}
	want, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil {
		return false
	}
	lo, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
	if err != nil {
		return false
	}
	hi, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
	if err != nil {
		return false
	}
	return want >= lo && want <= hi
}

// composeNamedVolumePattern matches a compose named volume reference: one
// path segment, starting with an alphanumeric character.
var composeNamedVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// RewriteComposeManagedVolumes maps every named volume of a rendered compose
// document onto the application's managed directory. A volume used by a
// service without a top-level declaration is auto-declared; a declared one
// keeps its name and is redefined. Both become local volumes whose device
// is <root>/<appID>/<name>, so Docker stores their data where the file
// manager and backups can reach it instead of its own default mountpoint.
// An external declaration is left untouched (the confined allowlist rejects
// external volumes, so this only matters to a direct caller of the rewrite).
func RewriteComposeManagedVolumes(rendered, root string, appID uuid.UUID) (string, error) {
	if strings.TrimSpace(rendered) == "" {
		return "", fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	if appID == uuid.Nil {
		return "", fmt.Errorf("%w: compose managed volumes need an application id", ErrValidation)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		return "", fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	used := map[string]bool{}
	services, _ := doc["services"].(map[string]any)
	for _, rawService := range services {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		entries, ok := service["volumes"].([]any)
		if !ok {
			continue
		}
		for _, entry := range entries {
			name, ok := composeVolumeSource(entry)
			if !ok {
				continue
			}
			if err := validateComposeVolumeName(name); err != nil {
				return "", err
			}
			used[name] = true
		}
	}
	declared := map[string]any{}
	if raw, ok := doc["volumes"]; ok && raw != nil {
		var isMapping bool
		declared, isMapping = raw.(map[string]any)
		if !isMapping {
			return "", fmt.Errorf("%w: top-level volumes is not a mapping", ErrValidation)
		}
	}
	names := make([]string, 0, len(used)+len(declared))
	for name := range used {
		names = append(names, name)
	}
	for name := range declared {
		if !used[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if isExternalVolume(declared[name]) {
			continue
		}
		if err := validateComposeVolumeName(name); err != nil {
			return "", err
		}
		declared[name] = map[string]any{
			"driver": "local",
			"driver_opts": map[string]any{
				"type":   "none",
				"o":      "bind",
				"device": filepath.Join(appVolumeDir(root, appID), name),
			},
		}
	}
	if len(declared) > 0 {
		doc["volumes"] = declared
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	return string(out), nil
}

// composeVolumeSource returns the named-volume source of one service mount,
// or false for an anonymous volume, a host bind or an unparseable entry.
func composeVolumeSource(entry any) (string, bool) {
	source := ""
	switch mount := entry.(type) {
	case string:
		parts := strings.Split(mount, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return "", false
		}
		source = parts[0]
	case map[string]any:
		mountType := strings.ToLower(strings.TrimSpace(scalarString(mount["type"])))
		if mountType != "" && mountType != "volume" {
			return "", false
		}
		source = scalarString(mount["source"])
	default:
		return "", false
	}
	if source == "" || strings.HasPrefix(source, "/") ||
		strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") {
		return "", false
	}
	return source, true
}

// validateComposeVolumeName keeps a rewritten name a safe single path
// segment outside the managed-database namespace.
func validateComposeVolumeName(name string) error {
	if !composeNamedVolumePattern.MatchString(name) {
		return fmt.Errorf("%w: volume source %q is not a valid named volume", ErrValidation, name)
	}
	if strings.HasPrefix(name, "gotham-db-") {
		return fmt.Errorf("%w: volume name %q is reserved for managed databases", ErrValidation, name)
	}
	return nil
}

// isExternalVolume reports whether a top-level volume declaration is
// external. The rewrite never changes such a volume; a confined deploy
// rejects it before reaching here.
func isExternalVolume(definition any) bool {
	mapping, ok := definition.(map[string]any)
	if !ok {
		return false
	}
	external, ok := mapping["external"]
	if !ok || external == nil {
		return false
	}
	if flag, ok := external.(bool); ok {
		return flag
	}
	return true
}

// rewriteManagedBindsForPreview rewrites managed bind prefixes that name
// another application directory to the preview's own directory, so a
// preview inherits the document shape without ever mounting (or writing)
// the base application's data. Named volumes need no rewrite: compose
// prefixes them with the project name, which already differs per preview.
func rewriteManagedBindsForPreview(content, root string, previewID uuid.UUID) string {
	prefix := regexp.QuoteMeta(filepath.Clean(root)) + `/[0-9a-fA-F-]{36}/`
	replacement := filepath.Clean(root) + "/" + previewID.String() + "/"
	return regexp.MustCompile(prefix).ReplaceAllString(content, replacement)
}

// teardownStubDocument is the minimal valid document a best-effort teardown
// falls back to when the stored document no longer resolves: `down
// --remove-orphans` removes the project's containers whatever services the
// document declares, so the sidecars are still reached.
func teardownStubDocument() string {
	return "services:\n  gotham-teardown:\n    image: scratch\n"
}

// composeProjectEnv assembles the substitution environment of a compose
// deploy: the application's plain env vars and opened secrets, layered over
// the shared scopes exactly like a container start, plus the PORT default.
// The map is what services.Render substitutes into the document; secret
// values never reach a stored row, a log line or an error (see Render and
// services.Redact at the call sites).
func composeProjectEnv(envVars []EnvVar, secrets []Secret, project, environment []SharedVariable, secretKey string, port int32) (map[string]string, error) {
	envVars, secrets = mergeSharedVariables(project, environment, envVars, secrets)
	env := make(map[string]string, len(envVars)+len(secrets)+1)
	for _, v := range envVars {
		if v.Key == "" {
			continue
		}
		if err := checkTextBytes(v.Value, fmt.Sprintf("environment variable %q value", v.Key)); err != nil {
			return nil, err
		}
		env[v.Key] = v.Value
	}
	for _, s := range secrets {
		if s.Key == "" {
			continue
		}
		value, err := providers.OpenSecret(secretKey, s.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: open secret %s: %v", ErrValidation, s.Key, err)
		}
		if err := checkTextBytes(value, fmt.Sprintf("secret %q value", s.Key)); err != nil {
			return nil, err
		}
		env[s.Key] = value
	}
	for key, value := range defaultPortEnv(port, envVars, secrets) {
		if _, ok := env[key]; !ok {
			env[key] = value
		}
	}
	return env, nil
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
