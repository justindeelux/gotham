package deploy

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

func TestBuildRunRequest(t *testing.T) {
	const secretKey = "test-secret-key"
	userID := uuid.New()
	app := testApplication(userID)
	dep := Deployment{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Kind:          KindDeploy,
		State:         StateStarting,
		ImageTag:      "gotham/app:tag",
		RegistryImage: "127.0.0.1:5000/gotham/app:tag",
	}
	envVars, secrets := testEnv(t)
	storages := []Storage{{Name: "data", HostPath: "", ContainerPath: "/var/lib/app"}}

	req, err := buildRunRequest(app, dep, envVars, secrets, storages, secretKey)
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}

	if req.Image != dep.RegistryImage {
		t.Errorf("Image = %q, want registry image %q", req.Image, dep.RegistryImage)
	}
	if !strings.HasPrefix(req.Name, "gotham-demo-app-") {
		t.Errorf("Name = %q, want gotham-demo-app- prefix", req.Name)
	}
	// buildEnv sorts by key so the payload is deterministic regardless of
	// whether a value came from an env var, a secret or the PORT default.
	if want := []string{"API_TOKEN=hunter2", "FOO=bar", "PORT=3000"}; !equalStrings(req.Env, want) {
		t.Errorf("Env = %v, want %v", req.Env, want)
	}
	if want := []string{"8080:3000"}; !equalStrings(req.Ports, want) {
		t.Errorf("Ports = %v, want %v", req.Ports, want)
	}
	if want := []string{filepath.Join(managedVolumeRoot(), app.ID.String(), "data") + ":/var/lib/app"}; !equalStrings(req.Volumes, want) {
		t.Errorf("Volumes = %v, want %v", req.Volumes, want)
	}
	if req.Labels[labelManaged] != "true" {
		t.Errorf("managed label = %q, want true", req.Labels[labelManaged])
	}
	if req.Labels[labelAppID] != app.ID.String() {
		t.Errorf("app_id label = %q, want %q", req.Labels[labelAppID], app.ID)
	}
	if req.Labels[labelDeploymentID] != dep.ID.String() {
		t.Errorf("deployment_id label = %q, want %q", req.Labels[labelDeploymentID], dep.ID)
	}
	if req.Labels[portsLabel] != "8080:3000" {
		t.Errorf("ports label = %q, want 8080:3000", req.Labels[portsLabel])
	}
}

func TestBuildRunRequestImageFallback(t *testing.T) {
	app := testApplication(uuid.New())
	dep := Deployment{ID: uuid.New(), ImageTag: "gotham/app:tag"}

	req, err := buildRunRequest(app, dep, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}
	if req.Image != "gotham/app:tag" {
		t.Errorf("Image = %q, want local tag fallback", req.Image)
	}
}

func TestBuildRunRequestPorts(t *testing.T) {
	app := testApplication(uuid.New())
	dep := Deployment{ID: uuid.New(), ImageTag: "img"}

	app.HostPort = 0
	req, err := buildRunRequest(app, dep, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}
	if want := []string{"3000"}; !equalStrings(req.Ports, want) {
		t.Errorf("Ports = %v, want %v", req.Ports, want)
	}
	// A bare container port is still declared: containers.portsFromLabels
	// reads the label back to surface it in the API.
	if req.Labels[portsLabel] != "3000" {
		t.Errorf("ports label = %q, want 3000", req.Labels[portsLabel])
	}

	app.Port = 0
	app.HostPort = 0
	req, err = buildRunRequest(app, dep, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}
	if len(req.Ports) != 0 {
		t.Errorf("Ports = %v, want none", req.Ports)
	}
}

func TestBuildRunRequestSecretWinsOverEnv(t *testing.T) {
	const secretKey = "test-secret-key"
	app := testApplication(uuid.New())
	dep := Deployment{ID: uuid.New(), ImageTag: "img"}

	sealed, err := providers.SealSecret(secretKey, "from-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	req, err := buildRunRequest(app, dep,
		[]EnvVar{{Key: "TOKEN", Value: "from-env"}},
		[]Secret{{Key: "TOKEN", Ciphertext: sealed}}, nil, secretKey)
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}
	if want := []string{"PORT=3000", "TOKEN=from-secret"}; !equalStrings(req.Env, want) {
		t.Errorf("Env = %v, want the secret to win: %v", req.Env, want)
	}
}

// TestBuildRunRequestPortDefault pins the PORT fallback: the payload defaults
// PORT to the application's container port, but an explicit env var or secret
// always wins and a zero port injects nothing. The port mapping itself never
// changes with the fallback.
func TestBuildRunRequestPortDefault(t *testing.T) {
	const secretKey = "test-secret-key"
	dep := Deployment{ID: uuid.New(), ImageTag: "img"}

	sealedPort, err := providers.SealSecret(secretKey, "9000")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	tests := []struct {
		name          string
		port          int32
		hostPort      int32
		envVars       []EnvVar
		secrets       []Secret
		wantEnv       []string
		wantPorts     []string
		wantPortLabel string
	}{
		{
			name:          "default injected when unset",
			port:          3000,
			hostPort:      8080,
			wantEnv:       []string{"PORT=3000"},
			wantPorts:     []string{"8080:3000"},
			wantPortLabel: "8080:3000",
		},
		{
			name:          "explicit env wins",
			port:          3000,
			hostPort:      8080,
			envVars:       []EnvVar{{Key: "PORT", Value: "4000"}},
			wantEnv:       []string{"PORT=4000"},
			wantPorts:     []string{"8080:3000"},
			wantPortLabel: "8080:3000",
		},
		{
			name:          "sealed secret wins over env and default",
			port:          3000,
			hostPort:      8080,
			envVars:       []EnvVar{{Key: "PORT", Value: "4000"}},
			secrets:       []Secret{{Key: "PORT", Ciphertext: sealedPort}},
			wantEnv:       []string{"PORT=9000"},
			wantPorts:     []string{"8080:3000"},
			wantPortLabel: "8080:3000",
		},
		{
			name:          "secret alone wins over default",
			port:          3000,
			hostPort:      8080,
			secrets:       []Secret{{Key: "PORT", Ciphertext: sealedPort}},
			wantEnv:       []string{"PORT=9000"},
			wantPorts:     []string{"8080:3000"},
			wantPortLabel: "8080:3000",
		},
		{
			name:          "zero port skips the injection",
			port:          0,
			hostPort:      0,
			envVars:       []EnvVar{{Key: "FOO", Value: "bar"}},
			wantEnv:       []string{"FOO=bar"},
			wantPorts:     nil,
			wantPortLabel: "",
		},
		{
			name:          "default sorts with the other variables",
			port:          3000,
			hostPort:      8080,
			envVars:       []EnvVar{{Key: "FOO", Value: "bar"}},
			wantEnv:       []string{"FOO=bar", "PORT=3000"},
			wantPorts:     []string{"8080:3000"},
			wantPortLabel: "8080:3000",
		},
		{
			name:          "bare container port mapping is unchanged",
			port:          3000,
			hostPort:      0,
			wantEnv:       []string{"PORT=3000"},
			wantPorts:     []string{"3000"},
			wantPortLabel: "3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := testApplication(uuid.New())
			app.Port = tt.port
			app.HostPort = tt.hostPort

			req, err := buildRunRequest(app, dep, tt.envVars, tt.secrets, nil, secretKey)
			if err != nil {
				t.Fatalf("buildRunRequest: %v", err)
			}
			if !equalStrings(req.Env, tt.wantEnv) {
				t.Errorf("Env = %v, want %v", req.Env, tt.wantEnv)
			}
			if !equalStrings(req.Ports, tt.wantPorts) {
				t.Errorf("Ports = %v, want %v", req.Ports, tt.wantPorts)
			}
			if got := req.Labels[portsLabel]; got != tt.wantPortLabel {
				t.Errorf("ports label = %q, want %q", got, tt.wantPortLabel)
			}
		})
	}
}

func TestBuildRunRequestValidation(t *testing.T) {
	app := testApplication(uuid.New())

	if _, err := buildRunRequest(app, Deployment{ID: uuid.New()}, nil, nil, nil, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("missing image error = %v, want ErrValidation", err)
	}

	dep := Deployment{ID: uuid.New(), ImageTag: "img"}
	outside := []Storage{{Name: "data", HostPath: "/data/app", ContainerPath: "/var/lib/app"}}
	if _, err := buildRunRequest(app, dep, nil, nil, outside, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("out-of-root bind error = %v, want ErrValidation", err)
	}

	emptyTarget := []Storage{{Name: "data", HostPath: "", ContainerPath: ""}}
	if _, err := buildRunRequest(app, dep, nil, nil, emptyTarget, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("empty container path error = %v, want ErrValidation", err)
	}

	// A non-absolute source is a Docker named volume: it is not a host bind
	// and is namespaced to the application.
	named := []Storage{{Name: "data", HostPath: "gotham-data", ContainerPath: "/var/lib/app"}}
	req, err := buildRunRequest(app, dep, nil, nil, named, "")
	if err != nil {
		t.Fatalf("named volume error = %v, want accepted", err)
	}
	wantNamed := appNamedVolumePrefix + app.ID.String() + "-gotham-data" + ":/var/lib/app"
	if want := []string{wantNamed}; !equalStrings(req.Volumes, want) {
		t.Errorf("Volumes = %v, want %v", req.Volumes, want)
	}

	nested := []Storage{{Name: "data", HostPath: filepath.Join(managedVolumeRoot(), app.ID.String(), "a", "b"), ContainerPath: "/var/lib/app"}}
	if _, err := buildRunRequest(app, dep, nil, nil, nested, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("nested bind error = %v, want ErrValidation", err)
	}

	sealed, err := providers.SealSecret("right-key", "value")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	secrets := []Secret{{Key: "TOKEN", Ciphertext: sealed}}
	if _, err := buildRunRequest(app, dep, nil, secrets, nil, "wrong-key"); !errors.Is(err, ErrValidation) {
		t.Errorf("wrong key error = %v, want ErrValidation", err)
	}
}

func TestContainerNameIsDockerSafe(t *testing.T) {
	app := testApplication(uuid.New())
	app.Name = "My App / v2!"
	dep := Deployment{ID: uuid.MustParse("11111111-2222-3333-4444-555555555555")}

	name := containerName(app, dep)
	if !strings.HasPrefix(name, "gotham-my-app-v2-") {
		t.Errorf("containerName = %q, want sanitized gotham-my-app-v2- prefix", name)
	}
	if strings.ContainsAny(name, " /!") {
		t.Errorf("containerName = %q, contains illegal characters", name)
	}
	// A rollback reuses the application but not the deployment, so the name
	// never collides with the container it replaces.
	other := containerName(app, Deployment{ID: uuid.New()})
	if other == name {
		t.Errorf("deployment-specific names collided: %q", name)
	}
}

func TestTruncateError(t *testing.T) {
	if got := truncateError(nil); got != "" {
		t.Errorf("truncateError(nil) = %q, want empty", got)
	}
	short := truncateError(errors.New("boom"))
	if short != "boom" {
		t.Errorf("truncateError = %q, want boom", short)
	}
	long := truncateError(errors.New(strings.Repeat("x", 8000)))
	if len([]rune(long)) > 4097 {
		t.Errorf("long error was not truncated: %d runes", len([]rune(long)))
	}
	if got := truncateError(errors.New(strings.Repeat("y", 4096))); len(got) != 4096 {
		t.Errorf("truncateError kept %d runes of a 4 KiB error; want it intact", len([]rune(got)))
	}
}

// TestTruncateErrorRuneSafe is the JUS-81 round-2 regression: cutting a
// multi-byte message at the byte limit must not store invalid UTF-8.
func TestTruncateErrorRuneSafe(t *testing.T) {
	message := strings.Repeat("x", 4095) + "日本語" + strings.Repeat("y", 100)
	got := truncateError(errors.New(message))
	if !utf8.ValidString(got) {
		t.Errorf("truncateError = %q; want valid UTF-8", got)
	}
	if strings.Contains(got, "日") {
		t.Errorf("truncateError = %q; want the cut before the split rune", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncateError = %q; want the truncation marker", got)
	}
}

// equalStrings compares two string slices order-sensitively.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTruncateErrorInvalidUTF8 pins that invalid bytes are replaced instead of
// persisted, and that a bad byte early in the message does not discard the rest.
func TestTruncateErrorInvalidUTF8(t *testing.T) {
	got := truncateError(errors.New("hdr\n\xff bin\n" + strings.Repeat("x", 5000)))
	if !utf8.ValidString(got) || len(got) < 4000 {
		t.Errorf("truncateError = %d bytes valid=%v; want valid UTF-8 near the limit", len(got), utf8.ValidString(got))
	}
}
