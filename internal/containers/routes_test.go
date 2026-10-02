package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeService is a scriptable ContainerService for route tests.
type fakeService struct {
	mu         sync.Mutex
	containers []Container
	listErr    error
	actionErr  error
	pullErr    error
	runID      string
	runErr     error
	runSeen    RunOptions
	calls      []string
	logs       [][]byte
	logErr     error
}

func (f *fakeService) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeService) List(_ context.Context, _ uuid.UUID) ([]Container, error) {
	f.record("list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.containers, nil
}

func (f *fakeService) Start(_ context.Context, _ uuid.UUID, _ string) error {
	f.record("start")
	return f.actionErr
}

func (f *fakeService) Stop(_ context.Context, _ uuid.UUID, _ string) error {
	f.record("stop")
	return f.actionErr
}

func (f *fakeService) Restart(_ context.Context, _ uuid.UUID, _ string) error {
	f.record("restart")
	return f.actionErr
}

func (f *fakeService) Remove(_ context.Context, _ uuid.UUID, _ string) error {
	f.record("remove")
	return f.actionErr
}

func (f *fakeService) Pull(_ context.Context, _ uuid.UUID, image string) error {
	f.record("pull")
	if image == "" {
		return fmt.Errorf("%w: image is required", ErrValidation)
	}
	return f.pullErr
}

func (f *fakeService) Run(_ context.Context, _ uuid.UUID, opts RunOptions) (string, error) {
	f.record("run")
	f.mu.Lock()
	f.runSeen = opts
	f.mu.Unlock()
	if opts.Image == "" {
		return "", fmt.Errorf("%w: image is required", ErrValidation)
	}
	if f.runErr != nil {
		return "", f.runErr
	}
	return f.runID, nil
}

// Logs streams the configured payload (default: nothing) and closes the
// channel, which is what the agent does when a container's stream ends.
func (f *fakeService) Logs(_ context.Context, _ uuid.UUID, _ string, _ bool) (<-chan []byte, <-chan error, error) {
	f.record("logs")
	f.mu.Lock()
	chunks := f.logs
	err := f.logErr
	f.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	out := make(chan []byte)
	streamErr := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(streamErr)
		for _, chunk := range chunks {
			out <- chunk
		}
	}()
	return out, streamErr, nil
}

// passthroughAuth stands in for RequireAuth in route tests.
func passthroughAuth(next http.Handler) http.Handler {
	return next
}

// newTestRouter mounts the container routes with the fake service.
func newTestRouter(svc ContainerService) http.Handler {
	r := chi.NewRouter()
	Mount(r, passthroughAuth, svc)
	return r
}

func doRouteRequest(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{containers: []Container{{ID: "abc", Name: "web", Image: "nginx", State: "running", Ports: []string{}}}}
	h := newTestRouter(svc)

	rec := doRouteRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/containers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body containerListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Containers) != 1 || body.Containers[0].ID != "abc" {
		t.Errorf("body = %+v", body)
	}

	rec = doRouteRequest(t, h, http.MethodGet, "/v1/servers/not-a-uuid/containers", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad id status = %d, want 400", rec.Code)
	}

	svc.listErr = ErrServerNotFound
	rec = doRouteRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/containers", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown server status = %d, want 404", rec.Code)
	}
}

func TestActionRoutes(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{}
	h := newTestRouter(svc)

	for _, action := range []string{"start", "stop", "restart"} {
		rec := doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/abc123/"+action, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 (body %s)", action, rec.Code, rec.Body.String())
		}
		var body containerActionEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode: %v", action, err)
		}
		if body.ContainerID != "abc123" {
			t.Errorf("%s container_id = %q", action, body.ContainerID)
		}
	}

	svc.actionErr = ErrContainerNotFound
	rec := doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/missing/start", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing container status = %d, want 404", rec.Code)
	}

	svc.actionErr = ErrAgentUnavailable
	rec = doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/abc/stop", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("agent down status = %d, want 502", rec.Code)
	}
}

func TestPullRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{}
	h := newTestRouter(svc)

	rec := doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/images/pull", `{"image":"nginx:latest"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/images/pull", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty image status = %d, want 400", rec.Code)
	}

	rec = doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/images/pull", `{`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", rec.Code)
	}
}

func TestRunRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{runID: "new-id"}
	h := newTestRouter(svc)

	rec := doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/run",
		`{"image":"nginx:latest","name":"web","ports":["8080:80"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body containerActionEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ContainerID != "new-id" {
		t.Errorf("container_id = %q, want new-id", body.ContainerID)
	}
	svc.mu.Lock()
	seen := svc.runSeen
	svc.mu.Unlock()
	if seen.Image != "nginx:latest" || seen.Name != "web" || len(seen.Ports) != 1 {
		t.Errorf("run options = %+v", seen)
	}

	rec = doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/run", `{"name":"web"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing image status = %d, want 400", rec.Code)
	}

	// FX-5a: a raw run must not spoof a managed label or mount a reserved
	// named volume.
	rec = doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/run",
		`{"image":"nginx","labels":{"gotham.component":"proxy"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("reserved label status = %d, want 400", rec.Code)
	}
	rec = doRouteRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/containers/run",
		`{"image":"nginx","volumes":["gotham-db-secret:/data"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("reserved volume status = %d, want 400", rec.Code)
	}
}

func TestAuthMiddlewareEnforced(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}, &fakeService{})

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/v1/servers/"+id.String()+"/containers", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMountNilService(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, passthroughAuth, nil)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/v1/servers/"+id.String()+"/containers", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no routes mounted)", rec.Code)
	}
}

func TestInternalErrorMapping(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{listErr: errors.New("boom")}
	h := newTestRouter(svc)

	rec := doRouteRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/containers", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// Compile-time assertion that the fake satisfies the interface.
var _ ContainerService = (*fakeService)(nil)
