package deploy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestSanitizeBuildLogPassthrough(t *testing.T) {
	const raw = "cloning\nbuilding with engine dockerfile\nimage built: gotham/app:2"
	if got := sanitizeBuildLog(raw); got != raw {
		t.Errorf("sanitizeBuildLog = %q, want unchanged", got)
	}
	if got := sanitizeBuildLog(""); got != "" {
		t.Errorf("sanitizeBuildLog empty = %q, want empty", got)
	}
}

func TestSanitizeBuildLogKeepsTail(t *testing.T) {
	head := strings.Repeat("H", 1024) + "\n"
	tail := "deployment failed: boom\n"
	raw := head + strings.Repeat("x\n", maxBuildLogBytes) + tail
	got := sanitizeBuildLog(raw)
	if len(got) > maxBuildLogBytes {
		t.Fatalf("len = %d, want at most %d", len(got), maxBuildLogBytes)
	}
	if !strings.HasSuffix(got, tail) {
		t.Error("capped log does not keep the tail")
	}
	if strings.Contains(got, head) {
		t.Error("capped log keeps the head it should have dropped")
	}
	if !utf8.ValidString(got) {
		t.Error("capped log is not valid UTF-8")
	}
}

func TestSanitizeBuildLogRejectsInvalidUTF8(t *testing.T) {
	raw := "ok line\n\xff\xfe broken bytes\nlast line\n"
	got := sanitizeBuildLog(raw)
	if !utf8.ValidString(got) {
		t.Fatal("stored log is not valid UTF-8")
	}
	if !strings.Contains(got, "last line") {
		t.Errorf("sanitized log = %q, want the tail kept", got)
	}
	// An invalid sequence straddling the tail cut must not leave a partial
	// rune at the head either.
	raw = strings.Repeat("\xff", maxBuildLogBytes+10) + "tail"
	if got := sanitizeBuildLog(raw); !utf8.ValidString(got) || !strings.HasSuffix(got, "tail") {
		t.Errorf("cut log invalid or missing tail: %q", got)
	}
}

func TestLogRecorderRoundTrip(t *testing.T) {
	var rec logRecorder
	rec.record("deployment deploy accepted")
	rec.record("token <redacted> pulled")
	rec.record("deployment failed: boom")
	got := rec.finalize()
	want := "deployment deploy accepted\ntoken <redacted> pulled\ndeployment failed: boom"
	if got != want {
		t.Errorf("finalize = %q, want %q", got, want)
	}
}

func TestLogRecorderNilSafe(t *testing.T) {
	var rec *logRecorder
	rec.record("dropped")
	if got := rec.finalize(); got != "" {
		t.Errorf("nil finalize = %q, want empty", got)
	}
}

func TestLogRecorderBoundsMemory(t *testing.T) {
	var rec logRecorder
	line := strings.Repeat("x", 100)
	const lines = 10000 // ~1 MiB of input against a 256 KiB cap
	for i := 0; i < lines; i++ {
		rec.record(line)
	}
	got := rec.finalize()
	if len(got) > maxBuildLogBytes {
		t.Errorf("len = %d, want at most %d", len(got), maxBuildLogBytes)
	}
	if !strings.HasSuffix(got, line) {
		t.Error("bounded log does not keep the tail")
	}
	rec.mu.Lock()
	held := len(rec.lines)
	rec.mu.Unlock()
	// ~256 KiB / 101 B per line ≈ 2600 lines held; anything near 10000
	// means the cap only applies at finalize.
	if held > 4000 {
		t.Errorf("held lines = %d, want a bounded tail", held)
	}
}

// orderPublisher reports whether the stored log was already on the row when
// a terminal state line was published.
type orderPublisher struct {
	mu                   sync.Mutex
	repo                 *fakeRepository
	id                   uuid.UUID
	sawTerminal          bool
	storedBeforeTerminal bool
}

func (p *orderPublisher) Publish(_ context.Context, _, payload string) error {
	if !strings.Contains(payload, "→ failed") && !strings.Contains(payload, "→ running") {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sawTerminal = true
	if dep, ok := p.repo.deployment(p.id); ok && dep.BuildLog != "" {
		p.storedBeforeTerminal = true
	}
	return nil
}

func TestOrchestratorPersistsLogBeforeTerminalEvent(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	pub := &orderPublisher{repo: repo, id: dep.ID}

	node := newMockNode()
	node.stopErr = errors.New("stop refused")
	node.listed = []*agentv1.ContainerInfo{{Id: "old-container-id", State: "running", Status: "Up 1 minute"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
	})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	if !pub.sawTerminal {
		t.Fatal("no terminal state line published")
	}
	pub.mu.Lock()
	storedFirst := pub.storedBeforeTerminal
	pub.mu.Unlock()
	if !storedFirst {
		t.Error("terminal state event was published before the build log was persisted")
	}
	stored, _ := repo.deployment(dep.ID)
	if !strings.Contains(stored.BuildLog, "→") {
		t.Errorf("stored log = %q, want the state-change lines too", stored.BuildLog)
	}
}

func TestOrchestratorPersistsBuildLogOnFailure(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.stopErr = errors.New("stop refused")
	node.listed = []*agentv1.ContainerInfo{{Id: "old-container-id", State: "running", Status: "Up 1 minute"}}
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if stored.BuildLog == "" {
		t.Fatal("build log is empty, want the streamed lines stored")
	}
	for _, want := range []string{"accepted", "deployment failed"} {
		if !strings.Contains(stored.BuildLog, want) {
			t.Errorf("build log = %q, want it to contain %q", stored.BuildLog, want)
		}
	}
}

func TestRoutesDeploymentLog(t *testing.T) {
	userID, appID, deploymentID := uuid.New(), uuid.New(), uuid.New()
	logPath := "/v1/applications/" + appID.String() + "/deployments/" + deploymentID.String() + "/logs"

	t.Run("stored log", func(t *testing.T) {
		svc := &fakeDeployService{buildLog: "line one\nline two"}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logPath, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, `"log":"line one\nline two"`) {
			t.Errorf("body = %s, want the stored log", body)
		}
		if svc.seenUser != userID || svc.seenApplication != appID {
			t.Errorf("service saw user %s / app %s, want %s / %s",
				svc.seenUser, svc.seenApplication, userID, appID)
		}
		if !svc.seenRollbackSet || svc.seenRollback != deploymentID {
			t.Errorf("service saw deployment %s, want %s", svc.seenRollback, deploymentID)
		}
	})

	t.Run("missing deployment is a 404", func(t *testing.T) {
		svc := &fakeDeployService{buildLogErr: ErrNotFound}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logPath, nil))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("bad deployment id is a 400", func(t *testing.T) {
		svc := &fakeDeployService{}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/v1/applications/"+appID.String()+"/deployments/not-a-uuid/logs", nil))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("anonymous is a 401", func(t *testing.T) {
		svc := &fakeDeployService{buildLog: "x"}
		srv := newRouteServer(svc, nil)

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, logPath, nil))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}
