package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store"
)

func replayDeployFixture(t *testing.T) (*store.Store, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated GOTHAM_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	env, node := seedProjectEnvironment(t, ctx, st)
	user, err := st.CreateUser(ctx, uuid.NewString()+"@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM request_receipts WHERE principal_id=$1", principal)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", principal)
	})
	return st, principal, env, node
}
func deployReplayContext(principal uuid.UUID, op string, target uuid.UUID, key string) context.Context {
	ctx := context.Background()
	return requestreplay.With(ctx, &requestreplay.Request{Principal: principal, Operation: op, Target: target, Key: key})
}

func TestReplayApplicationCanonicalSecretsAndMove(t *testing.T) {
	st, principal, env, node := replayDeployFixture(t)
	cfg := Config{Store: st, Secret: testSecretKey, Logger: discardLogger()}
	first := NewService(cfg)
	defer first.Close()
	input := CreateApplicationInput{Name: " image ", EnvironmentID: env, ServerID: node, SourceType: SourceImage, ImageRef: "busybox:latest", RegistryUsername: " user ", RegistryPassword: " registry-secret ", Env: []EnvEntry{{Key: " B ", Value: "two"}, {Key: "A", Value: "secret: app-secret "}}, Storage: []Storage{{Name: "data", ContainerPath: "/data"}}}
	ctx := deployReplayContext(principal, "create_application", uuid.Nil, "create")
	app, err := first.CreateApplication(ctx, principal, input)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(cfg)
	defer restarted.Close()
	input.Name = "image"
	input.RegistryUsername = "user"
	input.RegistryPassword = "registry-secret"
	input.Env = []EnvEntry{{Key: "A", Value: "secret:app-secret"}, {Key: "B", Value: "two"}}
	replayCtx := deployReplayContext(principal, "create_application", uuid.Nil, "create")
	again, err := restarted.CreateApplication(replayCtx, principal, input)
	if err != nil || again.ID != app.ID || !requestreplay.Replayed(replayCtx) {
		t.Fatalf("replay %v %v", again.ID, err)
	}
	input.RegistryPassword = "changed"
	if _, err := restarted.CreateApplication(deployReplayContext(principal, "create_application", uuid.Nil, "create"), principal, input); !errors.Is(err, requestreplay.ErrConflict) {
		t.Fatalf("secret change: %v", err)
	}
	input.RegistryPassword = "registry-secret"
	input.Env[0].Value = "secret:changed"
	if _, err := restarted.CreateApplication(deployReplayContext(principal, "create_application", uuid.Nil, "create"), principal, input); !errors.Is(err, requestreplay.ErrConflict) {
		t.Fatalf("config change: %v", err)
	}
	input.Env[0].Value = "secret:app-secret"
	var projectID uuid.UUID
	if err := st.DB.QueryRow(context.Background(), "SELECT project_id FROM environments WHERE id=$1", env).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	moved := uuid.New()
	if _, err := st.DB.Exec(context.Background(), "INSERT INTO environments(id,project_id,name) VALUES($1,$2,'moved')", moved, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(context.Background(), "UPDATE applications SET environment_id=$1 WHERE id=$2", moved, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.CreateApplication(deployReplayContext(principal, "create_application", uuid.Nil, "create"), principal, input); !errors.Is(err, requestreplay.ErrMissing) {
		t.Fatalf("move: %v", err)
	}
	if _, err := st.DB.Exec(context.Background(), "DELETE FROM applications WHERE id=$1", app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.CreateApplication(deployReplayContext(principal, "create_application", uuid.Nil, "create"), principal, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted app: %v", err)
	}
}

func TestReplayDeployCrashWindowsAndRestart(t *testing.T) {
	st, principal, env, node := replayDeployFixture(t)
	repo := newStoreRepository(st, testSecretKey)
	for _, state := range []State{StateQueued, StateCloning, StateBuilding, StatePushing, StateStarting, StateRunning, StateFailed} {
		t.Run(string(state), func(t *testing.T) {
			app, err := repo.CreateApplication(context.Background(), Application{ID: uuid.New(), UserID: principal, EnvironmentID: env, ServerID: node, Name: uuid.NewString(), SourceType: SourceImage, ImageRef: "busybox:latest"}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			c := deployReplayContext(principal, "deploy", app.ID, "crash")
			if err := requestreplay.Fingerprint(c, "", struct{}{}); err != nil {
				t.Fatal(err)
			}
			accepted, err := repo.CreateDeployment(c, Deployment{ApplicationID: app.ID, Kind: KindDeploy, State: state})
			if err != nil {
				t.Fatal(err)
			}
			svc := NewService(Config{Store: st, Secret: testSecretKey, Logger: discardLogger()})
			defer svc.Close()
			if _, err := st.DB.Exec(context.Background(), "UPDATE applications SET image_ref='' WHERE id=$1", app.ID); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				c = deployReplayContext(principal, "deploy", app.ID, "crash")
				got, err := svc.Deploy(c, principal, app.ID)
				if err != nil || got.ID != accepted.ID || !requestreplay.Replayed(c) {
					t.Fatalf("restart replay %+v %v", got, err)
				}
				if state != StateRunning && got.State != StateFailed {
					t.Fatalf("boot policy: %s", got.State)
				}
			}
			if len(svc.queue) != 0 {
				t.Fatal("replay enqueued work")
			}
			stored, err := repo.ListDeployments(context.Background(), app.ID)
			if err != nil || len(stored) != 1 {
				t.Fatalf("stored %d %v", len(stored), err)
			}
		})
	}
}

type replayPreviousFailure struct{ *storeRepository }

func (r replayPreviousFailure) ListDeployments(context.Context, uuid.UUID) ([]Deployment, error) {
	return nil, errors.New("previous lookup failed")
}

type replayEnrichmentFailure struct{ *storeRepository }

func (r replayEnrichmentFailure) CreateApplication(ctx context.Context, app Application, env []EnvVar, secrets []Secret, storages []Storage) (Application, error) {
	_, err := r.storeRepository.CreateApplication(ctx, app, env, secrets, storages)
	if err != nil {
		return Application{}, err
	}
	return Application{}, errors.New("postcommit enrichment failed")
}

func TestReplayDeployPostcommitFailures(t *testing.T) {
	st, principal, env, node := replayDeployFixture(t)
	repo := newStoreRepository(st, testSecretKey)
	for _, previousFailure := range []bool{false, true} {
		app, err := repo.CreateApplication(context.Background(), Application{ID: uuid.New(), UserID: principal, EnvironmentID: env, ServerID: node, Name: uuid.NewString(), SourceType: SourceImage, ImageRef: "busybox:latest"}, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var repository Repository = repo
		if previousFailure {
			repository = replayPreviousFailure{repo}
		}
		svc := NewService(Config{Repository: repository, Secret: testSecretKey, Logger: discardLogger()})
		defer svc.Close()
		svc.startOnce.Do(func() {})
		svc.queue = make(chan job)
		svc.baseCancel()
		ctx := deployReplayContext(principal, "deploy", app.ID, "failed")
		if _, err := svc.Deploy(ctx, principal, app.ID); err == nil {
			t.Fatal("expected postcommit failure")
		}
		result := requestreplay.ResultFrom(ctx)
		if result == nil {
			t.Fatal("lost accepted identity")
		}
		ctx = deployReplayContext(principal, "deploy", app.ID, "failed")
		got, err := svc.Deploy(ctx, principal, app.ID)
		if err != nil || got.ID != result.ResourceID || got.State != StateFailed {
			t.Fatalf("failed replay %+v %v", got, err)
		}
	}
}

type replayHookOutcomeFailure struct{ *storeRepository }

func (r replayHookOutcomeFailure) SaveRequestHook(context.Context, string) error {
	return errors.New("outcome persistence failed")
}

type replayHookFailure struct {
	*fakeHookLifecycle
	err           error
	cancelRequest func()
}

func (h replayHookFailure) InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) error {
	if err := h.fakeHookLifecycle.InstallHook(ctx, userID, appID, r); err != nil {
		return err
	}
	if h.cancelRequest != nil {
		h.cancelRequest()
	}
	return h.err
}

func TestReplayHookAndPostcommitResponseLoss(t *testing.T) {
	st, principal, env, node := replayDeployFixture(t)
	for _, mode := range []string{"installed", "timeout", "canceled", "request_canceled", "definite_failure", "before_hook", "outcome_loss"} {
		t.Run(mode, func(t *testing.T) {
			hook := &fakeHookLifecycle{}
			stall := &stallingHookLifecycle{}
			var cancelRequest context.CancelFunc
			var lifecycle HookLifecycle = hook
			if mode == "timeout" {
				lifecycle = stall
			}
			switch mode {
			case "canceled":
				lifecycle = replayHookFailure{fakeHookLifecycle: hook, err: errors.Join(errors.New("provider response unavailable"), context.Canceled)}
			case "request_canceled":
				lifecycle = replayHookFailure{fakeHookLifecycle: hook, err: errors.New("provider response unavailable"), cancelRequest: func() { cancelRequest() }}
			case "definite_failure":
				lifecycle = replayHookFailure{fakeHookLifecycle: hook, err: errors.New("provider refused hook installation")}
			}
			repo := newStoreRepository(st, testSecretKey)
			var repository Repository = repo
			if mode == "before_hook" {
				repository = replayEnrichmentFailure{repo}
			}
			if mode == "outcome_loss" {
				repository = replayHookOutcomeFailure{repo}
			}
			svc := NewService(Config{Repository: repository, Secret: testSecretKey, Logger: discardLogger(), HookTimeout: 20 * time.Millisecond, Hooks: func() HookLifecycle { return lifecycle }})
			defer svc.Close()
			router := chi.NewRouter()
			Mount(router, func(h http.Handler) http.Handler { return h }, func(context.Context) (uuid.UUID, bool) { return principal, true }, svc)
			body := `{"name":"hook-` + mode + `","environment_id":"` + env.String() + `","server_id":"` + node.String() + `","provider":"github","source_type":"github_app","repo":"acme/repo","clone_url":"https://github.com/acme/repo.git"}`
			send := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/v1/applications", strings.NewReader(body))
				r.Header.Set("Idempotency-Key", mode)
				r = r.WithContext(r.Context())
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				cancelRequest = cancel
				r = r.WithContext(ctx)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				return w
			}
			first := send()
			if first.Code != 201 {
				t.Fatalf("create %d %s", first.Code, first.Body.String())
			}
			var before struct {
				Receipt requestreplay.Result `json:"receipt"`
			}
			if err := json.Unmarshal(first.Body.Bytes(), &before); err != nil {
				t.Fatal(err)
			}
			again := send()
			if again.Code != 201 {
				t.Fatalf("replay %d %s", again.Code, again.Body.String())
			}
			var after struct {
				Receipt requestreplay.Result `json:"receipt"`
			}
			if err := json.Unmarshal(again.Body.Bytes(), &after); err != nil {
				t.Fatal(err)
			}
			if !after.Receipt.Replayed || after.Receipt.ResourceID != before.Receipt.ResourceID {
				t.Fatal("lost identity")
			}
			switch mode {
			case "installed":
				if hook.installCalls != 1 || after.Receipt.Hook != "installed" {
					t.Fatalf("hook repeated/outcome %+v calls=%d", after, hook.installCalls)
				}
			case "timeout":
				if stall.installCalls != 1 || before.Receipt.Hook != "not_confirmed" || after.Receipt.Hook != "not_confirmed" || !strings.Contains(first.Body.String(), "not confirmed") || !strings.Contains(again.Body.String(), "not confirmed") {
					t.Fatalf("timeout %+v calls=%d", after, stall.installCalls)
				}
			case "canceled", "request_canceled":
				if hook.installCalls != 1 || before.Receipt.Hook != "not_confirmed" || after.Receipt.Hook != "not_confirmed" || !strings.Contains(first.Body.String(), "not confirmed") || !strings.Contains(again.Body.String(), "not confirmed") {
					t.Fatal("canceled hook was asserted not installed or repeated")
				}
			case "definite_failure":
				if hook.installCalls != 1 || before.Receipt.Hook != "warning" || after.Receipt.Hook != "warning" || !strings.Contains(again.Body.String(), "not installed") {
					t.Fatal("definite hook failure lost its warning")
				}
			case "outcome_loss":
				if hook.installCalls != 1 || after.Receipt.Hook != "not_confirmed" || !strings.Contains(again.Body.String(), "not confirmed") {
					t.Fatal("accepted hook outcome was asserted or repeated")
				}
			case "before_hook":
				if hook.installCalls != 0 || after.Receipt.Hook != "not_confirmed" || !strings.Contains(again.Body.String(), "not confirmed") {
					t.Fatal("unknown hook outcome lost")
				}
			}
		})
	}
}
