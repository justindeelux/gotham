package deploy

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAutoDomainPortPrefersDeclaredPorts(t *testing.T) {
	appID := uuid.New()
	dockerfile := testApplication(uuid.New())
	dockerfile.ID = appID
	dockerfile.SourceType = SourceDockerfile
	dockerfile.DockerfileContent = "FROM alpine\nEXPOSE 8080/tcp\n"

	if got := autoDomainPort(dockerfile, ApplicationDomain{Domain: "app.example.com"}, ""); got != 8080 {
		t.Errorf("Dockerfile EXPOSE port = %d, want 8080", got)
	}

	compose := testApplication(uuid.New())
	compose.ID = appID
	compose.SourceType = SourceCompose
	compose.ComposeService = "web"
	composeRaw := "services:\n  web:\n    image: nginx\n    ports:\n      - \"9000:3000\"\n"
	if got := autoDomainPort(compose, ApplicationDomain{Domain: "app.example.com"}, composeRaw); got != 3000 {
		t.Errorf("compose service port = %d, want 3000", got)
	}

	railpack := testApplication(uuid.New())
	railpack.ID = appID
	generated := autoDomainPort(railpack, ApplicationDomain{Domain: "app.example.com"}, "")
	if generated < 10000 || generated > 59999 {
		t.Errorf("generated port = %d, want a stable 10000-59999 port", generated)
	}
	if again := autoDomainPort(railpack, ApplicationDomain{Domain: "app.example.com"}, ""); again != generated {
		t.Errorf("generated port changed from %d to %d", generated, again)
	}
}

func TestStartContainerResolvesEmptyDomainPort(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	app.DockerfileContent = "FROM alpine\nEXPOSE 8080\n"
	app.BuildPack = "dockerfile"
	repo := &fakeRepository{app: app}
	repo.domains = []ApplicationDomain{{
		ID: uuid.New(), ApplicationID: app.ID, Domain: "app.example.com",
		IsPrimary: true, Port: 0, Protocol: ProtocolHTTP,
	}}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	request := node.lastRequest()
	if request == nil {
		t.Fatal("no container was started")
	}
	if !equalStrings(request.GetPorts(), []string{"8080"}) {
		t.Errorf("ports = %v, want the declared EXPOSE port", request.GetPorts())
	}
	if !containsEnv(request.GetEnv(), "PORT=8080") {
		t.Errorf("env = %v, want PORT=8080", request.GetEnv())
	}
	domains, err := repo.ListApplicationDomains(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	if len(domains) != 1 || domains[0].Port != 8080 {
		t.Errorf("domains = %#v, want the resolved port persisted", domains)
	}
}

func TestPendingChangesNotifyOnceUntilDeployClears(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	notifier := &recordingNotifier{}
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: "container-1"})
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Notifier:   notifier,
	})
	t.Cleanup(func() { _ = svc.Close() })

	ctx := context.Background()
	if _, err := svc.AddDomain(ctx, userID, app.ID, DomainInput{Domain: "one.example.com", Protocol: ProtocolHTTPS}); err != nil {
		t.Fatalf("add first domain: %v", err)
	}
	results := notifier.redeployResults()
	if len(results) != 1 || !equalStrings(results[0].Changes, []string{PendingChangeDomains}) {
		t.Fatalf("redeploy notifications = %#v, want one domains event", results)
	}

	if _, err := svc.AddDomain(ctx, userID, app.ID, DomainInput{Domain: "two.example.com", Protocol: ProtocolHTTPS}); err != nil {
		t.Fatalf("add second domain: %v", err)
	}
	if got := len(notifier.redeployResults()); got != 1 {
		t.Errorf("notifications = %d while pending, want still 1", got)
	}

	if _, err := svc.ReplaceEnv(ctx, userID, app.ID, []EnvEntry{{Key: "FOO", Value: "bar"}}); err != nil {
		t.Fatalf("replace env: %v", err)
	}
	results = notifier.redeployResults()
	if len(results) != 1 {
		t.Errorf("notifications = %d after a further change, want still 1", len(results))
	}

	svc.clearPendingChanges(ctx, app.ID)
	if _, err := svc.AddDomain(ctx, userID, app.ID, DomainInput{Domain: "three.example.com", Protocol: ProtocolHTTPS}); err != nil {
		t.Fatalf("add domain after clear: %v", err)
	}
	results = notifier.redeployResults()
	if len(results) != 2 {
		t.Fatalf("notifications = %d after a successful deploy and a new change, want 2", len(results))
	}
	if !equalStrings(results[1].Changes, []string{PendingChangeDomains}) {
		t.Errorf("second notification changes = %v, want domains", results[1].Changes)
	}
}

func TestPendingChangesBeforeFirstDeployAreNotRecorded(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	notifier := &recordingNotifier{}
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Notifier:   notifier,
	})
	t.Cleanup(func() { _ = svc.Close() })

	if _, err := svc.ReplaceEnv(context.Background(), userID, app.ID, []EnvEntry{{Key: "FOO", Value: "bar"}}); err != nil {
		t.Fatalf("replace env: %v", err)
	}
	if got := repo.pendingChanges[app.ID]; len(got) != 0 {
		t.Errorf("pending = %v before the first deploy, want none", got)
	}
	if got := len(notifier.redeployResults()); got != 0 {
		t.Errorf("notifications = %d before the first deploy, want none", got)
	}
}

func TestPendingChangesDistinguishEnvSecretsAndStorage(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	notifier := &recordingNotifier{}
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: "container-1"})
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Notifier:   notifier,
	})
	t.Cleanup(func() { _ = svc.Close() })

	ctx := context.Background()
	if _, err := svc.ReplaceEnv(ctx, userID, app.ID, []EnvEntry{{Key: "FOO", Value: "bar"}}); err != nil {
		t.Fatalf("replace env: %v", err)
	}
	if got := repo.pendingChanges[app.ID]; !equalStrings(got, []string{PendingChangeEnvironment}) {
		t.Errorf("pending = %v, want environment", got)
	}

	if _, err := svc.ReplaceEnv(ctx, userID, app.ID, []EnvEntry{{Key: "FOO", Value: "bar"}}); err != nil {
		t.Fatalf("repeat env: %v", err)
	}
	if got := repo.pendingChanges[app.ID]; !equalStrings(got, []string{PendingChangeEnvironment}) {
		t.Errorf("pending after an identical save = %v, want unchanged", got)
	}

	entries, err := svc.GetEnv(ctx, userID, app.ID)
	if err != nil {
		t.Fatalf("get env: %v", err)
	}
	repo.mu.Lock()
	delete(repo.pendingChanges, app.ID)
	repo.mu.Unlock()
	if _, err := svc.ReplaceEnv(ctx, userID, app.ID, append(entries, EnvEntry{Key: "TOKEN", Value: "secret:top-secret"})); err != nil {
		t.Fatalf("add secret: %v", err)
	}
	if got := repo.pendingChanges[app.ID]; !equalStrings(got, []string{PendingChangeSecrets}) {
		t.Errorf("pending = %v, want secrets only", got)
	}

	repo.mu.Lock()
	delete(repo.pendingChanges, app.ID)
	repo.mu.Unlock()
	storage := Storage{Name: "data", ContainerPath: "/data"}
	if _, err := svc.ReplaceStorages(ctx, userID, app.ID, []Storage{storage}); err != nil {
		t.Fatalf("replace storages: %v", err)
	}
	if got := repo.pendingChanges[app.ID]; !equalStrings(got, []string{PendingChangeStorage}) {
		t.Errorf("pending = %v, want storage", got)
	}
	if _, err := svc.ReplaceStorages(ctx, userID, app.ID, []Storage{storage}); err != nil {
		t.Fatalf("repeat storages: %v", err)
	}
	if got := repo.pendingChanges[app.ID]; !equalStrings(got, []string{PendingChangeStorage}) {
		t.Errorf("pending after an identical storage save = %v, want unchanged", got)
	}
}

func containsEnv(env []string, want string) bool {
	for _, value := range env {
		if value == want {
			return true
		}
	}
	return false
}

func TestDockerfileExposedPortReadsDirectives(t *testing.T) {
	content := strings.Join([]string{
		"FROM alpine",
		"EXPOSE 8000-8010",
		"expose 3000/tcp 4000/udp",
		"CMD [\"true\"]",
	}, "\n")
	if got := dockerfileExposedPort(content); got != 8000 {
		t.Errorf("exposed port = %d, want the first directive's start", got)
	}
	if got := dockerfileExposedPort("EXPOSE 3000/tcp 4000/udp"); got != 3000 {
		t.Errorf("protocol-suffixed port = %d, want 3000", got)
	}
	if got := dockerfileExposedPort("FROM alpine"); got != 0 {
		t.Errorf("no EXPOSE = %d, want 0", got)
	}
}
