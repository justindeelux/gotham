package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/justindeelux/gotham/internal/providers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// portsLabel mirrors containers.portsLabel. The agent ListContainers contract
// carries no port bindings, so the control plane stores the mapping on the
// container and reads it back when listing; the constant is duplicated rather
// than imported to keep the runtime payload independent of the container DTOs.
const portsLabel = "gotham.ports"

// Gotham label keys applied to every application container.
const (
	labelManaged      = "gotham.managed"
	labelAppID        = "gotham.app_id"
	labelDeploymentID = "gotham.deployment_id"
)

// portEnvKey is the environment variable every PaaS platform (Heroku,
// Railway, Coolify) injects so the process binds the port the host mapping
// actually points at.
const portEnvKey = "PORT"

// invalidNameChars matches anything outside the Docker container-name alphabet
// ([a-zA-Z0-9][a-zA-Z0-9_.-]).
var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// buildRunRequest assembles the agent container-create payload for a
// deployment: image (node registry reference when the build produced one),
// environment variables plus decrypted secrets, the storage volume map, the
// port mapping and the gotham.* labels.
//
// Secrets are opened with providers.OpenSecret, the single AES-256-GCM helper
// in this codebase; plaintext exists only in the returned request. A secret
// whose key collides with a plain env var wins: Docker applies the last
// occurrence of a duplicated key, so secrets are appended after env vars.
//
// The payload also defaults PORT to the application's container port when the
// application declares one (see defaultPortEnv), so images built without a
// Dockerfile listen where the port mapping points.
func buildRunRequest(
	app Application,
	dep Deployment,
	envVars []EnvVar,
	secrets []Secret,
	storages []Storage,
	secretKey string,
) (*agentv1.CreateContainerRequest, error) {
	image := dep.RegistryImage
	if strings.TrimSpace(image) == "" {
		image = dep.ImageTag
	}
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("%w: deployment has no image", ErrValidation)
	}

	env, err := buildEnv(envVars, secrets, secretKey, defaultPortEnv(app, envVars, secrets))
	if err != nil {
		return nil, err
	}
	volumes, err := volumeSpecs(storages)
	if err != nil {
		return nil, err
	}
	ports := portSpecs(app)

	labels := map[string]string{
		labelManaged:      "true",
		labelAppID:        app.ID.String(),
		labelDeploymentID: dep.ID.String(),
	}
	if len(ports) > 0 {
		labels[portsLabel] = strings.Join(ports, ",")
	}

	return &agentv1.CreateContainerRequest{
		Image:   image,
		Name:    containerName(app, dep),
		Env:     env,
		Labels:  labels,
		Ports:   ports,
		Volumes: volumes,
	}, nil
}

// buildEnv renders env vars first and decrypted secrets second so a colliding
// key resolves to the secret. Defaults (only the PORT fallback today) fill the
// gaps: a key present in either collection always wins. The result is sorted
// by key (secrets keep their relative order over an env var of the same name)
// for a deterministic payload.
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

// volumeSpecs renders the storage map as "host:container" bind specs. Both
// paths must be absolute: a relative host path would be resolved against the
// daemon's working directory on the node.
func volumeSpecs(storages []Storage) ([]string, error) {
	if len(storages) == 0 {
		return nil, nil
	}
	specs := make([]string, 0, len(storages))
	for _, s := range storages {
		host := strings.TrimSpace(s.HostPath)
		target := strings.TrimSpace(s.ContainerPath)
		if host == "" || target == "" {
			return nil, fmt.Errorf("%w: storage %q needs a host and a container path", ErrValidation, s.Name)
		}
		if !strings.HasPrefix(host, "/") || !strings.HasPrefix(target, "/") {
			return nil, fmt.Errorf("%w: storage %q paths must be absolute", ErrValidation, s.Name)
		}
		specs = append(specs, host+":"+target)
	}
	return specs, nil
}

// defaultPortEnv returns the PORT fallback for the runtime payload: an
// image built without a Dockerfile (Railpack, buildpacks) starts whatever the
// repository declares — often its own default port — while the host mapping
// points at Application.Port, so the platform tells the process which port to
// bind, the same way Heroku, Railway and Coolify inject PORT. The default
// applies only when the application declares a port (> 0) and neither an env
// var nor a secret already defines PORT; an explicit value (including a
// sealed secret reference) always wins, and a Dockerfile app that manages
// PORT itself — or an app with no port — sees no change at all.
func defaultPortEnv(app Application, envVars []EnvVar, secrets []Secret) map[string]string {
	if app.Port <= 0 {
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
	return map[string]string{portEnvKey: strconv.Itoa(int(app.Port))}
}

// portSpecs renders the application port as a "host:container" mapping when
// the operator pinned a host port, and as a bare container port otherwise
// (the agent then lets Docker assign a free host port).
func portSpecs(app Application) []string {
	if app.Port <= 0 {
		return nil
	}
	spec := fmt.Sprintf("%d", app.Port)
	if app.HostPort > 0 {
		spec = fmt.Sprintf("%d:%d", app.HostPort, app.Port)
	}
	return []string{spec}
}

// containerName derives a Docker-safe, unique container name from the
// application and deployment: "gotham-<app>-<deploy prefix>". The deploy
// prefix keeps a rollback container from colliding with the one it replaces.
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

// truncateError bounds the message persisted on the deployments row so one
// runaway build log cannot bloat the table.
func truncateError(err error) string {
	if err == nil {
		return ""
	}
	const limit = 1000
	message := strings.TrimSpace(err.Error())
	if len(message) > limit {
		return message[:limit] + "…"
	}
	return message
}
