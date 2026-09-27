package databases

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

// fakeDatabaseService is a scriptable DatabaseService for route tests: every
// method records its arguments and returns the configured value.
type fakeDatabaseService struct {
	create    Database
	creds     Credentials
	createErr error

	list    []Database
	listErr error

	get    Database
	getErr error

	credentials    Credentials
	credentialsErr error

	update    Database
	updateErr error

	deleteErr error

	lifecycle    Database
	lifecycleErr error

	seenUser     uuid.UUID
	seenDatabase uuid.UUID
	seenRequest  CreateRequest
	seenUpdate   UpdateRequest
	seenVerbs    []string
}

// Compile-time guarantee that fakeDatabaseService satisfies the route seam.
var _ DatabaseService = (*fakeDatabaseService)(nil)

func (f *fakeDatabaseService) Create(_ context.Context, userID uuid.UUID, req CreateRequest) (Database, Credentials, error) {
	f.seenUser, f.seenRequest = userID, req
	return f.create, f.creds, f.createErr
}

func (f *fakeDatabaseService) List(_ context.Context, userID uuid.UUID) ([]Database, error) {
	f.seenUser = userID
	return f.list, f.listErr
}

func (f *fakeDatabaseService) Get(_ context.Context, userID, databaseID uuid.UUID) (Database, error) {
	f.seenUser, f.seenDatabase = userID, databaseID
	return f.get, f.getErr
}

func (f *fakeDatabaseService) Credentials(_ context.Context, userID, databaseID uuid.UUID) (Credentials, error) {
	f.seenUser, f.seenDatabase = userID, databaseID
	return f.credentials, f.credentialsErr
}

func (f *fakeDatabaseService) Update(_ context.Context, userID, databaseID uuid.UUID, req UpdateRequest) (Database, error) {
	f.seenUser, f.seenDatabase, f.seenUpdate = userID, databaseID, req
	return f.update, f.updateErr
}

func (f *fakeDatabaseService) Delete(_ context.Context, userID, databaseID uuid.UUID) error {
	f.seenUser, f.seenDatabase = userID, databaseID
	return f.deleteErr
}

func (f *fakeDatabaseService) Start(_ context.Context, userID, databaseID uuid.UUID) (Database, error) {
	return f.action("start", userID, databaseID)
}

func (f *fakeDatabaseService) Stop(_ context.Context, userID, databaseID uuid.UUID) (Database, error) {
	return f.action("stop", userID, databaseID)
}

func (f *fakeDatabaseService) Restart(_ context.Context, userID, databaseID uuid.UUID) (Database, error) {
	return f.action("restart", userID, databaseID)
}

// action records a lifecycle verb and returns the scripted outcome.
func (f *fakeDatabaseService) action(verb string, userID, databaseID uuid.UUID) (Database, error) {
	f.seenVerbs = append(f.seenVerbs, verb)
	f.seenUser, f.seenDatabase = userID, databaseID
	return f.lifecycle, f.lifecycleErr
}

// passthroughAuth stands in for RequireAuth in route tests.
func passthroughAuth(next http.Handler) http.Handler { return next }

// newRouteServer mounts the database routes with a no-op auth middleware.
func newRouteServer(svc DatabaseService, userID UserIDFunc) http.Handler {
	r := chi.NewRouter()
	Mount(r, passthroughAuth, userID, svc)
	return r
}

// alwaysUser returns a UserIDFunc for one user.
func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

// databasePath renders one of the database routes.
func databasePath(databaseID uuid.UUID, suffix string) string {
	if databaseID == uuid.Nil {
		return "/v1/databases" + suffix
	}
	return "/v1/databases/" + databaseID.String() + suffix
}

// sampleRow is the domain row the fakes hand back.
func sampleRow(userID uuid.UUID) Database {
	return Database{
		ID:          uuid.New(),
		UserID:      userID,
		ServerID:    uuid.New(),
		Name:        "orders",
		Engine:      EnginePostgres,
		Version:     "16-alpine",
		Status:      StatusRunning,
		ContainerID: "container-1",
		PublicPort:  5433,
		StoragePath: "gotham-db-00000000-0000-0000-0000-000000000000",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

// TestCreateRouteReturnsRowAndCredentials asserts the 201 payload and that the
// request fields reach the service intact.
func TestCreateRouteReturnsRowAndCredentials(t *testing.T) {
	userID := uuid.New()
	row := sampleRow(userID)
	svc := &fakeDatabaseService{
		create: row,
		creds:  Credentials{Username: "orders", Password: "pw", Database: "orders"},
	}
	srv := newRouteServer(svc, alwaysUser(userID))

	body := `{"name":"orders","engine":"postgres","version":"16-alpine","server_id":"` +
		row.ServerID.String() + `","public_port":5433}`
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, databasePath(uuid.Nil, ""), strings.NewReader(body)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var envelope createDatabaseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Database.ID != row.ID.String() || envelope.Database.Status != StatusRunning {
		t.Errorf("database = %+v", envelope.Database)
	}
	if envelope.Database.Volume != row.StoragePath {
		t.Errorf("volume = %q, want %q", envelope.Database.Volume, row.StoragePath)
	}
	if envelope.Credentials.Password != "pw" {
		t.Errorf("credentials = %+v, want the generated password", envelope.Credentials)
	}
	if svc.seenUser != userID {
		t.Errorf("user = %s, want %s", svc.seenUser, userID)
	}
	if svc.seenRequest.Name != "orders" || svc.seenRequest.Engine != EnginePostgres ||
		svc.seenRequest.Version != "16-alpine" || svc.seenRequest.ServerID != row.ServerID ||
		svc.seenRequest.PublicPort != 5433 {
		t.Errorf("request = %+v", svc.seenRequest)
	}
}

// TestCreateRouteRejections covers the 400 paths of the create body.
func TestCreateRouteRejections(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed json", body: `{"name":`},
		{name: "unknown field", body: `{"name":"db","engine":"postgres","surprise":true}`},
		{name: "invalid server id", body: `{"name":"db","engine":"postgres","server_id":"nope"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeDatabaseService{create: sampleRow(userID)}
			srv := newRouteServer(svc, alwaysUser(userID))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, databasePath(uuid.Nil, ""), strings.NewReader(tt.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	// A valid body still parses the server id it was given.
	svc := &fakeDatabaseService{create: sampleRow(userID)}
	srv := newRouteServer(svc, alwaysUser(userID))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, databasePath(uuid.Nil, ""),
		strings.NewReader(`{"name":"db","engine":"postgres","server_id":"`+serverID.String()+`"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.seenRequest.ServerID != serverID {
		t.Errorf("server_id = %s, want %s", svc.seenRequest.ServerID, serverID)
	}
}

// TestListGetAndCredentialsRoutes covers the read endpoints.
func TestListGetAndCredentialsRoutes(t *testing.T) {
	userID, databaseID := uuid.New(), uuid.New()

	t.Run("list", func(t *testing.T) {
		svc := &fakeDatabaseService{list: []Database{sampleRow(userID), sampleRow(userID)}}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, databasePath(uuid.Nil, ""), nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var envelope databaseListEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(envelope.Databases) != 2 {
			t.Errorf("databases = %d, want 2", len(envelope.Databases))
		}
	})

	t.Run("list empty renders an array", func(t *testing.T) {
		svc := &fakeDatabaseService{list: []Database{}}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, databasePath(uuid.Nil, ""), nil))
		if !strings.Contains(rec.Body.String(), `"databases":[]`) {
			t.Errorf("body = %s, want an empty array", rec.Body.String())
		}
	})

	t.Run("get", func(t *testing.T) {
		svc := &fakeDatabaseService{get: sampleRow(userID)}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, databasePath(databaseID, ""), nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var envelope databaseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if envelope.Database.Engine != EnginePostgres || envelope.Database.PublicPort != 5433 {
			t.Errorf("database = %+v", envelope.Database)
		}
		if svc.seenUser != userID || svc.seenDatabase != databaseID {
			t.Errorf("seen = %s/%s, want %s/%s", svc.seenUser, svc.seenDatabase, userID, databaseID)
		}
	})

	t.Run("credentials", func(t *testing.T) {
		svc := &fakeDatabaseService{credentials: Credentials{Username: "orders", Password: "pw", Database: "orders"}}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, databasePath(databaseID, "/credentials"), nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var envelope credentialsEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if envelope.Credentials.Password != "pw" {
			t.Errorf("credentials = %+v", envelope.Credentials)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		svc := &fakeDatabaseService{}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/databases/not-a-uuid", nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// TestUpdateAndDeleteRoutes covers rename and soft delete.
func TestUpdateAndDeleteRoutes(t *testing.T) {
	userID, databaseID := uuid.New(), uuid.New()

	t.Run("update renames", func(t *testing.T) {
		row := sampleRow(userID)
		row.Name = "warehouse"
		svc := &fakeDatabaseService{update: row}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, databasePath(databaseID, ""),
			strings.NewReader(`{"name":"warehouse"}`)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		if svc.seenUpdate.Name != "warehouse" {
			t.Errorf("update = %+v", svc.seenUpdate)
		}
	})

	t.Run("delete answers 204", func(t *testing.T) {
		svc := &fakeDatabaseService{}
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, databasePath(databaseID, ""), nil))

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q, want empty", rec.Body.String())
		}
		if svc.seenUser != userID || svc.seenDatabase != databaseID {
			t.Errorf("seen = %s/%s", svc.seenUser, svc.seenDatabase)
		}
	})
}

// TestLifecycleRoutes walks the three verbs and checks they are routed to the
// matching service method.
func TestLifecycleRoutes(t *testing.T) {
	userID, databaseID := uuid.New(), uuid.New()
	row := sampleRow(userID)

	for _, verb := range []string{"start", "stop", "restart"} {
		t.Run(verb, func(t *testing.T) {
			svc := &fakeDatabaseService{lifecycle: row}
			srv := newRouteServer(svc, alwaysUser(userID))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, databasePath(databaseID, "/"+verb), nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if len(svc.seenVerbs) != 1 || svc.seenVerbs[0] != verb {
				t.Errorf("verbs = %v, want [%s]", svc.seenVerbs, verb)
			}
			if svc.seenUser != userID || svc.seenDatabase != databaseID {
				t.Errorf("seen = %s/%s", svc.seenUser, svc.seenDatabase)
			}
		})
	}
}

// TestServiceErrorMapping pins the sentinel → HTTP status table.
func TestServiceErrorMapping(t *testing.T) {
	userID, databaseID := uuid.New(), uuid.New()

	tests := []struct {
		name   string
		status int
		err    error
		bind   func(*fakeDatabaseService)
	}{
		{name: "not found", status: http.StatusNotFound, err: ErrNotFound,
			bind: func(s *fakeDatabaseService) { s.getErr = ErrNotFound }},
		{name: "server not found", status: http.StatusNotFound, err: ErrServerNotFound,
			bind: func(s *fakeDatabaseService) { s.getErr = ErrServerNotFound }},
		{name: "validation", status: http.StatusBadRequest, err: ErrValidation,
			bind: func(s *fakeDatabaseService) { s.getErr = ErrValidation }},
		{name: "conflict", status: http.StatusConflict, err: ErrConflict,
			bind: func(s *fakeDatabaseService) { s.updateErr = ErrConflict }},
		{name: "disabled", status: http.StatusServiceUnavailable, err: ErrDisabled,
			bind: func(s *fakeDatabaseService) { s.getErr = ErrDisabled }},
		{name: "agent unavailable", status: http.StatusBadGateway, err: ErrAgentUnavailable,
			bind: func(s *fakeDatabaseService) { s.lifecycleErr = ErrAgentUnavailable }},
		{name: "healthcheck", status: http.StatusBadGateway, err: ErrHealthcheck,
			bind: func(s *fakeDatabaseService) { s.lifecycleErr = ErrHealthcheck }},
		{name: "internal", status: http.StatusInternalServerError, err: errors.New("boom"),
			bind: func(s *fakeDatabaseService) { s.getErr = errors.New("boom") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeDatabaseService{get: sampleRow(userID)}
			tt.bind(svc)
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			var req *http.Request
			switch tt.name {
			case "conflict":
				req = httptest.NewRequest(http.MethodPatch, databasePath(databaseID, ""),
					strings.NewReader(`{"name":"warehouse"}`))
			case "agent unavailable", "healthcheck":
				req = httptest.NewRequest(http.MethodPost, databasePath(databaseID, "/start"), nil)
			default:
				req = httptest.NewRequest(http.MethodGet, databasePath(databaseID, ""), nil)
			}

			srv.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body.String())
			}
		})
	}
}

// TestRoutesRequireAuthentication: every endpoint answers 401 without a user,
// and none of them reaches the service.
func TestRoutesRequireAuthentication(t *testing.T) {
	userID, databaseID := uuid.New(), uuid.New()
	svc := &fakeDatabaseService{get: sampleRow(userID), create: sampleRow(userID), lifecycle: sampleRow(userID)}
	srv := newRouteServer(svc, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false })

	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, databasePath(uuid.Nil, ""), strings.NewReader(`{"name":"db","engine":"postgres","server_id":"`+uuid.New().String()+`"}`)),
		httptest.NewRequest(http.MethodGet, databasePath(uuid.Nil, ""), nil),
		httptest.NewRequest(http.MethodGet, databasePath(databaseID, ""), nil),
		httptest.NewRequest(http.MethodGet, databasePath(databaseID, "/credentials"), nil),
		httptest.NewRequest(http.MethodPatch, databasePath(databaseID, ""), strings.NewReader(`{"name":"db"}`)),
		httptest.NewRequest(http.MethodDelete, databasePath(databaseID, ""), nil),
		httptest.NewRequest(http.MethodPost, databasePath(databaseID, "/start"), nil),
	}
	for _, req := range requests {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", req.Method, req.URL.Path, rec.Code)
		}
	}
	if len(svc.seenVerbs) != 0 {
		t.Errorf("lifecycle verbs reached the service: %v", svc.seenVerbs)
	}
	if svc.seenUser != uuid.Nil {
		t.Errorf("user = %s, want no call at all", svc.seenUser)
	}
}

// TestMountIsNoopWhenDisabled: a nil service or FEATURE_DATABASES=false mounts
// nothing, so the control plane can call Mount unconditionally.
func TestMountIsNoopWhenDisabled(t *testing.T) {
	userID := uuid.New()

	t.Run("nil service", func(t *testing.T) {
		r := chi.NewRouter()
		Mount(r, passthroughAuth, alwaysUser(userID), nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, databasePath(uuid.Nil, ""), nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("feature disabled", func(t *testing.T) {
		t.Setenv(FeatureEnv, "false")
		r := chi.NewRouter()
		Mount(r, passthroughAuth, alwaysUser(userID), &fakeDatabaseService{})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, databasePath(uuid.Nil, ""), nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
}
