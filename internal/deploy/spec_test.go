package deploy

import (
	"errors"
	"strings"
	"testing"

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
	envVars, secrets := testEnv(t, secretKey)
	storages := []Storage{{Name: "data", HostPath: "/data/app", ContainerPath: "/var/lib/app"}}

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
	// whether a value came from an env var or a secret.
	if want := []string{"API_TOKEN=hunter2", "FOO=bar"}; !equalStrings(req.Env, want) {
		t.Errorf("Env = %v, want %v", req.Env, want)
	}
	if want := []string{"8080:3000"}; !equalStrings(req.Ports, want) {
		t.Errorf("Ports = %v, want %v", req.Ports, want)
	}
	if want := []string{"/data/app:/var/lib/app"}; !equalStrings(req.Volumes, want) {
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
	if want := []string{"TOKEN=from-secret"}; !equalStrings(req.Env, want) {
		t.Errorf("Env = %v, want the secret to win: %v", req.Env, want)
	}
}

func TestBuildRunRequestValidation(t *testing.T) {
	app := testApplication(uuid.New())

	if _, err := buildRunRequest(app, Deployment{ID: uuid.New()}, nil, nil, nil, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("missing image error = %v, want ErrValidation", err)
	}

	dep := Deployment{ID: uuid.New(), ImageTag: "img"}
	badStorage := []Storage{{Name: "data", HostPath: "relative/path", ContainerPath: "/var/lib/app"}}
	if _, err := buildRunRequest(app, dep, nil, nil, badStorage, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("relative volume error = %v, want ErrValidation", err)
	}

	emptyPath := []Storage{{Name: "data", HostPath: "/data", ContainerPath: ""}}
	if _, err := buildRunRequest(app, dep, nil, nil, emptyPath, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("empty container path error = %v, want ErrValidation", err)
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
	long := truncateError(errors.New(strings.Repeat("x", 4000)))
	if len([]rune(long)) > 1001 {
		t.Errorf("long error was not truncated: %d runes", len([]rune(long)))
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
