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

const (
	MaxComposeBytes         = 256 << 10
	MaxComposeFilePathBytes = 256
)

var composeServiceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

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

type composeAppDocument struct {
	Services map[string]composeAppService `yaml:"services"`
}

type composeAppService struct {
	Image string `yaml:"image"`
}

type ComposePortTarget struct {
	Service string
	Port    int32
}

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

func isLoopbackAddress(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "127.") ||
		strings.EqualFold(trimmed, "localhost") || trimmed == "::1"
}

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

var composeNamedVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

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

func validateComposeVolumeName(name string) error {
	if !composeNamedVolumePattern.MatchString(name) {
		return fmt.Errorf("%w: volume source %q is not a valid named volume", ErrValidation, name)
	}
	if strings.HasPrefix(name, "gotham-db-") {
		return fmt.Errorf("%w: volume name %q is reserved for managed databases", ErrValidation, name)
	}
	return nil
}

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

func rewriteManagedBindsForPreview(content, root string, previewID uuid.UUID) string {
	prefix := regexp.QuoteMeta(filepath.Clean(root)) + `/[0-9a-fA-F-]{36}/`
	replacement := filepath.Clean(root) + "/" + previewID.String() + "/"
	return regexp.MustCompile(prefix).ReplaceAllString(content, replacement)
}

func teardownStubDocument() string {
	return "services:\n  gotham-teardown:\n    image: scratch\n"
}

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
