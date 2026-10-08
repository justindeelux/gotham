package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// testImageApplication returns an image-source application with a sealed
// private-registry credential.
func testImageApplication(t *testing.T, userID uuid.UUID) Application {
	t.Helper()
	sealed, err := providers.SealSecret(testSecretKey, "s3cret-token")
	if err != nil {
		t.Fatalf("seal registry credential: %v", err)
	}
	app := testApplication(userID)
	app.Provider = ""
	app.Repo = ""
	app.CloneURL = ""
	app.SourceType = SourceImage
	app.Branch = ""
	app.BuildPack = ""
	app.ImageRef = "registry.example.com/team/app:1.2"
	app.RegistryUsername = "robot"
	app.RegistryPasswordCiphertext = sealed
	return app
}

func TestOrchestratorImageDeployPullsAndRuns(t *testing.T) {
	app := testImageApplication(t, uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	src := &fakeSource{}
	node := newMockNode()
	pub := &recordPublisher{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     src,
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	// No repository to clone and nothing to build: the pushing step (where
	// the pull happens) follows queued directly.
	want := []State{StateQueued, StatePushing, StateStarting, StateRunning}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running", stored.State)
	}
	if src.calls != 0 {
		t.Errorf("clone calls = %d, want 0 (an image source has no repository)", src.calls)
	}
	if node.buildCalls != 0 {
		t.Errorf("build calls = %d, want 0 (an image source is prebuilt)", node.buildCalls)
	}
	if node.pullCalls != 1 {
		t.Fatalf("pull calls = %d, want 1", node.pullCalls)
	}
	if len(node.pullImages) != 1 || node.pullImages[0] != app.ImageRef {
		t.Errorf("pulled = %v, want [%s]", node.pullImages, app.ImageRef)
	}
	if len(node.pullUsers) != 1 || node.pullUsers[0] != "robot" {
		t.Errorf("pull usernames = %v, want [robot]", node.pullUsers)
	}
	if len(node.pullPasswords) != 1 || node.pullPasswords[0] != "s3cret-token" {
		t.Errorf("pull passwords not passed through to the node pull")
	}
	if stored.ImageTag != app.ImageRef {
		t.Errorf("image tag = %q, want the pulled reference", stored.ImageTag)
	}
	if stored.RegistryImage != app.ImageRef {
		t.Errorf("registry image = %q, want the pulled reference", stored.RegistryImage)
	}
	if stored.Digest != node.digest {
		t.Errorf("digest = %q, want the resolved %q", stored.Digest, node.digest)
	}
	req := node.lastRequest()
	if req == nil {
		t.Fatal("no container request recorded")
	}
	if req.Image != app.ImageRef {
		t.Errorf("container image = %q, want the pulled reference", req.Image)
	}
	for _, event := range pub.payloads() {
		if strings.Contains(event.Data, "s3cret-token") || strings.Contains(event.Data, "robot") {
			t.Errorf("published event carries the credential: %q", event.Data)
		}
	}
	if strings.Contains(stored.Error, "s3cret-token") {
		t.Errorf("stored error carries the credential: %q", stored.Error)
	}
}

func TestOrchestratorImageRedeployPullsAgain(t *testing.T) {
	app := testImageApplication(t, uuid.New())
	repo := &fakeRepository{app: app}
	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})

	first := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	o.run(context.Background(), job{app: app, dep: first})

	// The tag moved upstream: the next pull resolves a new digest, which the
	// redeploy records on its own row.
	node.digest = "sha256:moveddigest"
	second := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	o.run(context.Background(), job{app: app, dep: second})

	if node.pullCalls != 2 {
		t.Errorf("pull calls = %d, want 2 (a redeploy pulls again)", node.pullCalls)
	}
	stored, ok := repo.deployment(second.ID)
	if !ok {
		t.Fatal("second deployment row is gone")
	}
	if stored.Digest != "sha256:moveddigest" {
		t.Errorf("digest = %q, want the re-resolved digest", stored.Digest)
	}
}

func TestOrchestratorImageRollbackUsesPinnedDigest(t *testing.T) {
	app := testImageApplication(t, uuid.New())
	repo := &fakeRepository{app: app}
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	pinned := "registry.example.com/team/app@" + digest
	dep := seedDeployment(t, repo, app, Deployment{
		Kind:          KindRollback,
		ImageTag:      app.ImageRef,
		RegistryImage: pinned,
		Digest:        digest,
		RollbackFrom:  uuid.New(),
	})
	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running", stored.State)
	}
	if len(node.pullImages) != 1 || node.pullImages[0] != pinned {
		t.Errorf("pulled = %v, want the digest-pinned [%s]", node.pullImages, pinned)
	}
	if stored.Digest != node.digest && stored.Digest != digest {
		t.Errorf("digest = %q, want the resolved or pinned digest", stored.Digest)
	}
	req := node.lastRequest()
	if req == nil {
		t.Fatal("no container request recorded")
	}
	if req.Image != pinned {
		t.Errorf("container image = %q, want the pinned reference", req.Image)
	}
}

func TestServiceImageSourceValidation(t *testing.T) {
	newImageService := func(t *testing.T) (*Service, *fakeRepository) {
		t.Helper()
		repo := &fakeRepository{}
		return newNodeService(t, repo, newMockNode()), repo
	}

	validImageInput := func(serverID uuid.UUID) CreateApplicationInput {
		in := validCreateInput(serverID)
		in.Provider = ""
		in.Repo = ""
		in.CloneURL = ""
		in.SourceType = SourceImage
		in.Branch = ""
		in.BuildPack = ""
		in.ImageRef = "registry.example.com/team/app:1.2"
		in.RegistryUsername = "robot"
		in.RegistryPassword = "s3cret-token"
		return in
	}

	t.Run("create stores the reference and seals the credential", func(t *testing.T) {
		svc, repo := newImageService(t)
		created, err := svc.CreateApplication(context.Background(), uuid.New(), validImageInput(uuid.New()))
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		if created.ImageRef != "registry.example.com/team/app:1.2" {
			t.Errorf("image ref = %q", created.ImageRef)
		}
		if created.RegistryUsername != "robot" {
			t.Errorf("username = %q, want robot", created.RegistryUsername)
		}
		if created.RegistryPasswordCiphertext == "" || created.RegistryPasswordCiphertext == "s3cret-token" {
			t.Errorf("password not sealed: %q", created.RegistryPasswordCiphertext)
		}
		stored := created
		for _, candidate := range repo.apps {
			if candidate.ID == created.ID {
				stored = candidate
			}
		}
		if stored.RegistryPasswordCiphertext == "" {
			t.Error("sealed credential not stored")
		}
		opened, err := providers.OpenSecret(testSecretKey, stored.RegistryPasswordCiphertext)
		if err != nil || opened != "s3cret-token" {
			t.Errorf("credential does not round-trip: %q, %v", opened, err)
		}
	})

	t.Run("create refuses a bad reference", func(t *testing.T) {
		svc, _ := newImageService(t)
		in := validImageInput(uuid.New())
		in.ImageRef = "not a ref"
		if _, err := svc.CreateApplication(context.Background(), uuid.New(), in); err == nil {
			t.Error("CreateApplication(bad ref) = nil; want error")
		} else if strings.Contains(err.Error(), "s3cret-token") {
			t.Errorf("error carries the credential: %v", err)
		}
	})

	t.Run("create refuses the internal registry scope", func(t *testing.T) {
		svc, _ := newImageService(t)
		in := validImageInput(uuid.New())
		in.ImageRef = "127.0.0.1:5000/team/app:1"
		_, err := svc.CreateApplication(context.Background(), uuid.New(), in)
		if err == nil {
			t.Error("CreateApplication(loopback) = nil; want error")
		}
	})

	t.Run("create refuses git fields on an image source", func(t *testing.T) {
		svc, _ := newImageService(t)
		for name, mutate := range map[string]func(*CreateApplicationInput){
			"branch":     func(in *CreateApplicationInput) { in.Branch = "main" },
			"build pack": func(in *CreateApplicationInput) { in.BuildPack = "dockerfile" },
			"clone URL":  func(in *CreateApplicationInput) { in.CloneURL = "https://example.com/r.git" },
		} {
			in := validImageInput(uuid.New())
			mutate(&in)
			if _, err := svc.CreateApplication(context.Background(), uuid.New(), in); err == nil {
				t.Errorf("CreateApplication(%s set) = nil; want error", name)
			}
		}
	})

	t.Run("create refuses image fields on a git source", func(t *testing.T) {
		svc, _ := newImageService(t)
		in := validCreateInput(uuid.New())
		in.SourceType = SourceGitPublic
		in.Provider = ""
		in.CloneURL = "https://example.com/r.git"
		in.ImageRef = "nginx:1"
		if _, err := svc.CreateApplication(context.Background(), uuid.New(), in); err == nil {
			t.Error("CreateApplication(image ref on git source) = nil; want error")
		}
	})

	t.Run("update rotates and clears the credential", func(t *testing.T) {
		svc, _ := newImageService(t)
		ctx := context.Background()
		user := uuid.New()
		created, err := svc.CreateApplication(ctx, user, validImageInput(uuid.New()))
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		password := "rotated-token"
		updated, err := svc.UpdateApplication(ctx, user, created.ID, UpdateApplicationInput{RegistryPassword: &password})
		if err != nil {
			t.Fatalf("rotate: %v", err)
		}
		opened, err := providers.OpenSecret(testSecretKey, updated.RegistryPasswordCiphertext)
		if err != nil || opened != "rotated-token" {
			t.Errorf("rotated credential = %q, %v", opened, err)
		}
		if updated.RegistryUsername != "robot" {
			t.Errorf("username = %q, want it preserved across a password rotation", updated.RegistryUsername)
		}
		empty := ""
		cleared, err := svc.UpdateApplication(ctx, user, created.ID, UpdateApplicationInput{RegistryPassword: &empty})
		if err != nil {
			t.Fatalf("clear: %v", err)
		}
		if cleared.RegistryPasswordCiphertext != "" {
			t.Error("empty password did not clear the sealed credential")
		}
	})

	t.Run("update refuses image fields on a git source", func(t *testing.T) {
		svc, _ := newImageService(t)
		ctx := context.Background()
		user := uuid.New()
		created, err := svc.CreateApplication(ctx, user, validCreateInput(uuid.New()))
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		ref := "nginx:1"
		if _, err := svc.UpdateApplication(ctx, user, created.ID, UpdateApplicationInput{ImageRef: &ref}); err == nil {
			t.Error("UpdateApplication(image ref on git source) = nil; want error")
		}
	})
}

func TestServiceImageRollbackPinsDigest(t *testing.T) {
	repo := &fakeRepository{}
	svc := newNodeService(t, repo, newMockNode())
	ctx := context.Background()
	user := uuid.New()

	app := testImageApplication(t, user)
	repo.app = app

	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	target := seedDeployment(t, repo, app, Deployment{
		Kind:          KindDeploy,
		State:         StateRunning,
		ImageTag:      app.ImageRef,
		RegistryImage: app.ImageRef,
		Digest:        digest,
	})
	// seedDeployment marks finished; a rollback target must be running.
	_ = target

	rolled, err := svc.Rollback(ctx, user, app.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	want := "registry.example.com/team/app@" + digest
	if rolled.RegistryImage != want {
		t.Errorf("rollback image = %q, want the pinned %q", rolled.RegistryImage, want)
	}
	if rolled.RollbackFrom == uuid.Nil {
		t.Error("rollback does not reference its target")
	}
}

func TestRoutesImageApplicationRedactsCredential(t *testing.T) {
	userID := uuid.New()
	app := sampleApplication()
	app.UserID = userID
	app.Provider = ""
	app.Repo = ""
	app.CloneURL = ""
	app.SourceType = SourceImage
	app.Branch = ""
	app.BuildPack = ""
	app.ImageRef = "registry.example.com/team/app:1.2"
	app.RegistryUsername = "robot"
	app.RegistryPasswordCiphertext = "sealed-credential"
	svc := &fakeDeployService{application: app}
	srv := newRouteServer(svc, alwaysUser(userID))

	body := `{
		"name": "image app",
		"source_type": "image",
		"image_ref": "registry.example.com/team/app:1.2",
		"registry_username": "robot",
		"registry_password": "s3cret-token",
		"port": 3000,
		"environment_id": "` + uuid.New().String() + `",
		"server_id": "` + uuid.New().String() + `"
	}`
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath, strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	assertJSONKeys(t, mustJSON(t, rec.Body.Bytes(), "application"), applicationWireKeys...)
	raw := rec.Body.String()
	if strings.Contains(raw, "s3cret-token") || strings.Contains(raw, "sealed-credential") {
		t.Errorf("create response carries the credential: %s", raw)
	}
	if strings.Contains(raw, "registry_username") || strings.Contains(raw, "registry_password") {
		t.Errorf("create response names credential fields: %s", raw)
	}
	if !strings.Contains(raw, `"has_registry_credential":true`) {
		t.Errorf("create response misses the credential flag: %s", raw)
	}
	if !strings.Contains(raw, `"image_ref":"registry.example.com/team/app:1.2"`) {
		t.Errorf("create response misses the reference: %s", raw)
	}
	if svc.seenCreate.ImageRef != "registry.example.com/team/app:1.2" ||
		svc.seenCreate.RegistryUsername != "robot" ||
		svc.seenCreate.RegistryPassword != "s3cret-token" {
		t.Errorf("service saw %+v, want the image payload", svc.seenCreate)
	}

	get := httptest.NewRecorder()
	srv.ServeHTTP(get, httptest.NewRequest(http.MethodGet, applicationsPath+"/"+app.ID.String(), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200 (body %s)", get.Code, get.Body.String())
	}
	getRaw := get.Body.String()
	if strings.Contains(getRaw, "robot") || strings.Contains(getRaw, "sealed-credential") {
		t.Errorf("get response carries the credential: %s", getRaw)
	}
}

func TestServiceImageDeployQueues(t *testing.T) {
	userID := uuid.New()
	app := testImageApplication(t, userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	queued, err := svc.Deploy(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("Deploy(image source): %v", err)
	}
	if queued.State != StateQueued {
		t.Errorf("state = %s, want queued", queued.State)
	}
	if stored := listStored(t, repo, app.ID); len(stored) != 1 {
		t.Errorf("queued %d deployments, want 1", len(stored))
	}
}
