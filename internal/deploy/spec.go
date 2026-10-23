package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const portsLabel = "gotham.ports"

const (
	labelManaged      = "gotham.managed"
	labelAppID        = "gotham.app_id"
	labelDeploymentID = "gotham.deployment_id"
)

const portEnvKey = "PORT"

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func buildRunRequest(
	app Application,
	dep Deployment,
	envVars []EnvVar,
	secrets []Secret,
	storages []Storage,
	secretKey string,
	ports []int32,
	port int32,
) (*agentv1.CreateContainerRequest, error) {
	image := dep.RegistryImage
	if strings.TrimSpace(image) == "" {
		image = dep.ImageTag
	}
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("%w: deployment has no image", ErrValidation)
	}

	env, err := buildEnv(envVars, secrets, secretKey, defaultPortEnv(port, envVars, secrets))
	if err != nil {
		return nil, err
	}
	volumes, err := volumeSpecs(managedVolumeRoot(), app.ID, storages)
	if err != nil {
		return nil, err
	}
	specs := portSpecs(ports)

	labels := map[string]string{
		labelManaged:      "true",
		labelAppID:        app.ID.String(),
		labelDeploymentID: dep.ID.String(),
	}
	if len(specs) > 0 {
		labels[portsLabel] = strings.Join(specs, ",")
	}

	return &agentv1.CreateContainerRequest{
		Image:   image,
		Name:    containerName(app, dep),
		Env:     env,
		Labels:  labels,
		Ports:   specs,
		Volumes: volumes,
	}, nil
}

func buildEnv(envVars []EnvVar, secrets []Secret, secretKey string, defaults map[string]string) ([]string, error) {
	plain := make(map[string]string, len(envVars))
	for _, v := range envVars {
		if v.Key == "" {
			continue
		}
		plain[v.Key] = v.Value
	}
	sealed := make(map[string]string, len(secrets))
	for _, s := range secrets {
		if s.Key == "" {
			continue
		}
		value, err := providers.OpenSecret(secretKey, s.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: open secret %s: %v", ErrValidation, s.Key, err)
		}
		sealed[s.Key] = value
	}
	for key, value := range defaults {
		if _, ok := plain[key]; ok {
			continue
		}
		if _, ok := sealed[key]; ok {
			continue
		}
		plain[key] = value
	}

	keys := make([]string, 0, len(plain)+len(sealed))
	seen := map[string]bool{}
	for key := range plain {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range sealed {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := sealed[key]
		if !ok {
			value = plain[key]
		}
		env = append(env, key+"="+value)
	}
	return env, nil
}

func mergeSharedVariables(project, environment []SharedVariable, envVars []EnvVar, secrets []Secret) ([]EnvVar, []Secret) {
	plain := make(map[string]string)
	sealed := make(map[string]string)
	applyPlain := func(key, value string) {
		delete(sealed, key)
		plain[key] = value
	}
	applySealed := func(key, ciphertext string) {
		delete(plain, key)
		sealed[key] = ciphertext
	}
	for _, scope := range [][]SharedVariable{project, environment} {
		for _, variable := range scope {
			if variable.Key == "" {
				continue
			}
			if variable.Secret {
				applySealed(variable.Key, variable.Ciphertext)
			} else {
				applyPlain(variable.Key, variable.Value)
			}
		}
	}
	for _, v := range envVars {
		if v.Key == "" {
			continue
		}
		applyPlain(v.Key, v.Value)
	}
	for _, s := range secrets {
		if s.Key == "" {
			continue
		}
		applySealed(s.Key, s.Ciphertext)
	}
	keys := make([]string, 0, len(plain)+len(sealed))
	for key := range plain {
		keys = append(keys, key)
	}
	for key := range sealed {
		if _, ok := plain[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	mergedVars := make([]EnvVar, 0, len(plain))
	mergedSecrets := make([]Secret, 0, len(sealed))
	for _, key := range keys {
		if ciphertext, ok := sealed[key]; ok {
			mergedSecrets = append(mergedSecrets, Secret{Key: key, Ciphertext: ciphertext})
		} else {
			mergedVars = append(mergedVars, EnvVar{Key: key, Value: plain[key]})
		}
	}
	return mergedVars, mergedSecrets
}

func volumeSpecs(root string, appID uuid.UUID, storages []Storage) ([]string, error) {
	if len(storages) == 0 {
		return nil, nil
	}
	specs := make([]string, 0, len(storages))
	for _, s := range storages {
		target := strings.TrimSpace(s.ContainerPath)
		if target == "" || !strings.HasPrefix(target, "/") {
			return nil, fmt.Errorf("%w: storage %q needs an absolute container path", ErrValidation, s.Name)
		}
		host, err := managedHostPath(root, appID, s.Name, strings.TrimSpace(s.HostPath))
		if err != nil {
			return nil, err
		}
		specs = append(specs, host+":"+target)
	}
	return specs, nil
}

func domainTargetPorts(domains []ApplicationDomain) ([]int32, int32) {
	ports := make([]int32, 0, len(domains))
	primaryPort := int32(0)
	for _, domain := range domains {
		if domain.Disabled || domain.Port <= 0 {
			continue
		}
		ports = append(ports, domain.Port)
		if domain.IsPrimary {
			primaryPort = domain.Port
		}
	}
	if primaryPort == 0 && len(ports) > 0 {
		primaryPort = ports[0]
	}
	return ports, primaryPort
}

func defaultPortEnv(port int32, envVars []EnvVar, secrets []Secret) map[string]string {
	if port <= 0 {
		return nil
	}
	for _, v := range envVars {
		if v.Key == portEnvKey {
			return nil
		}
	}
	for _, s := range secrets {
		if s.Key == portEnvKey {
			return nil
		}
	}
	return map[string]string{portEnvKey: strconv.Itoa(int(port))}
}

func portSpecs(ports []int32) []string {
	seen := make(map[int32]bool, len(ports))
	specs := make([]string, 0, len(ports))
	for _, port := range ports {
		if port <= 0 || seen[port] {
			continue
		}
		seen[port] = true
		specs = append(specs, strconv.Itoa(int(port)))
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i] < specs[j] })
	return specs
}

func containerName(app Application, dep Deployment) string {
	name := strings.ToLower(app.Name)
	name = strings.Trim(invalidNameChars.ReplaceAllString(name, "-"), "-")
	if len(name) > 40 {
		name = strings.Trim(name[:40], "-")
	}
	deploy := dep.ID.String()[:8]
	if name == "" {
		return "gotham-" + app.ID.String()[:8] + "-" + deploy
	}
	return "gotham-" + name + "-" + deploy
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	const limit = 4096
	message := strings.ToValidUTF8(strings.TrimSpace(err.Error()), "\uFFFD")
	if len(message) > limit {
		cut := message[:limit]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		return cut + "…"
	}
	return message
}
