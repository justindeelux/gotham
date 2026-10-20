package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Test-only user plumbing: the production mount receives UserIDFromContext,
// so the handler itself never depends on the server package.
type targetsUserKey struct{}

func targetsUserFrom(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(targetsUserKey{}).(uuid.UUID)
	return userID, ok
}

func targetsAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-User") == "" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), targetsUserKey{}, uuid.MustParse(r.Header.Get("X-Test-User")))))
	})
}

type fakeTargetsSource struct {
	targets Targets
	err     error
	userID  uuid.UUID
}

func (f *fakeTargetsSource) ListTargets(_ context.Context, userID uuid.UUID) (Targets, error) {
	f.userID = userID
	return f.targets, f.err
}

func targetsRouter(t *testing.T, source TargetsSourceFunc) *chi.Mux {
	t.Helper()
	router := chi.NewRouter()
	mountTargets(router, targetsAuth, source, targetsUserFrom, nil, 2*time.Second)
	return router
}

func getTargets(t *testing.T, router http.Handler, userID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/terminal/targets", nil)
	if userID != "" {
		request.Header.Set("X-Test-User", userID)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestTerminalTargetsShape(t *testing.T) {
	appID := uuid.New()
	serverID := uuid.New()
	source := &fakeTargetsSource{targets: Targets{
		Servers: []HostTarget{{ID: serverID, Name: "prod-01", Status: "ready"}},
		Applications: []ContainerTargetGroup{{
			ApplicationID:   appID,
			ApplicationName: "shop",
			ServerID:        serverID,
			Containers: []ContainerTarget{{
				Service:     "web",
				ContainerID: "a1b2c3",
				Name:        "shop-web-1",
				Image:       "nginx",
				State:       "running",
				Replica:     1,
				Label:       "web · shop-web-1",
			}},
		}},
		Errors: []TargetError{{Source: "application", ID: appID.String(), Name: "shop", Code: TargetErrorUnavailable}},
	}}
	router := targetsRouter(t, func(*http.Request) TargetsSource { return source })

	recorder := getTargets(t, router, uuid.New().String())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body targetsListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Servers) != 1 || body.Servers[0].ID != serverID.String() {
		t.Fatalf("servers = %+v", body.Servers)
	}
	if len(body.Applications) != 1 || body.Applications[0].ApplicationID != appID.String() {
		t.Fatalf("applications = %+v", body.Applications)
	}
	container := body.Applications[0].Containers[0]
	if container.ContainerID != "a1b2c3" || container.Label != "web · shop-web-1" || container.Replica != 1 {
		t.Fatalf("container = %+v", container)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != string(TargetErrorUnavailable) {
		t.Fatalf("errors = %+v", body.Errors)
	}
	if source.userID == uuid.Nil {
		t.Fatal("the handler did not pass the authenticated user")
	}
}

func TestTerminalTargetsRejectsAnonymous(t *testing.T) {
	source := &fakeTargetsSource{}
	router := targetsRouter(t, func(*http.Request) TargetsSource { return source })
	recorder := getTargets(t, router, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestTerminalTargetsSourceFailureIsGeneric(t *testing.T) {
	router := targetsRouter(t, func(*http.Request) TargetsSource {
		return &fakeTargetsSource{err: errors.New("database is down")}
	})
	recorder := getTargets(t, router, uuid.New().String())
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if body := recorder.Body.String(); body == "" || strings.Contains(body, "database") {
		t.Fatalf("body = %q, want a generic message", body)
	}
}

func TestTerminalTargetsBoundsAndIgnoresUnknownCodes(t *testing.T) {
	servers := make([]HostTarget, 60)
	applications := make([]ContainerTargetGroup, 60)
	for index := range servers {
		servers[index] = HostTarget{ID: uuid.New(), Name: "node", Status: "ready"}
	}
	for index := range applications {
		containers := make([]ContainerTarget, 60)
		for position := range containers {
			containers[position] = ContainerTarget{
				Service:     "web",
				ContainerID: uuid.NewString(),
				Name:        "web-1",
				State:       "running",
				Label:       "web · web-1",
			}
		}
		applications[index] = ContainerTargetGroup{
			ApplicationID:   uuid.New(),
			ApplicationName: "shop",
			ServerID:        uuid.New(),
			Containers:      containers,
		}
	}
	router := targetsRouter(t, func(*http.Request) TargetsSource {
		return &fakeTargetsSource{targets: Targets{
			Servers:      servers,
			Applications: applications,
			Errors: []TargetError{
				{Source: "application", Code: TargetErrorCode("raw backend text")},
			},
		}}
	})
	recorder := getTargets(t, router, uuid.New().String())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body targetsListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Servers) != maxHostTargets {
		t.Fatalf("servers = %d, want %d", len(body.Servers), maxHostTargets)
	}
	if len(body.Applications) != maxContainerApps {
		t.Fatalf("applications = %d, want %d", len(body.Applications), maxContainerApps)
	}
	if len(body.Applications[0].Containers) != maxContainersPerApp {
		t.Fatalf("containers = %d, want %d", len(body.Applications[0].Containers), maxContainersPerApp)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != string(TargetErrorInternal) {
		t.Fatalf("errors = %+v, want a sanitized internal code", body.Errors)
	}
}

func TestTerminalTargetsEmptyListsAreNotNull(t *testing.T) {
	router := targetsRouter(t, func(*http.Request) TargetsSource {
		return &fakeTargetsSource{targets: Targets{}}
	})
	recorder := getTargets(t, router, uuid.New().String())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"servers", "applications", "errors"} {
		if string(raw[key]) != "[]" {
			t.Fatalf("%s = %s, want []", key, raw[key])
		}
	}
}

func TestTerminalTargetsMountIsSkippedWithoutASource(t *testing.T) {
	router := chi.NewRouter()
	mountTargets(router, targetsAuth, nil, targetsUserFrom, nil, time.Second)
	recorder := getTargets(t, router, uuid.New().String())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
