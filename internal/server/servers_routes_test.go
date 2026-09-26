package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/servers"
)

// fakeServerService is an in-memory ServerService for handler tests.
type fakeServerService struct {
	mu             sync.Mutex
	items          map[uuid.UUID]servers.Server
	failAdd        error
	validateResult *servers.ValidationResult
	validateErr    error
}

func newFakeServerService() *fakeServerService {
	return &fakeServerService{items: make(map[uuid.UUID]servers.Server)}
}

func (f *fakeServerService) Add(_ context.Context, _ uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID) (*servers.Server, error) {
	if f.failAdd != nil {
		return nil, f.failAdd
	}
	server := &servers.Server{
		ID:        uuid.New(),
		Name:      name,
		IP:        ip,
		Port:      port,
		SSHUser:   sshUser,
		Status:    servers.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if sshKeyID != uuid.Nil {
		keyID := sshKeyID
		server.SSHKeyID = &keyID
	}

	f.mu.Lock()
	f.items[server.ID] = *server
	f.mu.Unlock()
	return server, nil
}

func (f *fakeServerService) List(context.Context) ([]servers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	list := make([]servers.Server, 0, len(f.items))
	for _, item := range f.items {
		list = append(list, item)
	}
	return list, nil
}

func (f *fakeServerService) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	item, ok := f.items[id]
	if !ok {
		return nil, servers.ErrNotFound
	}
	return &item, nil
}

func (f *fakeServerService) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.items[id]; !ok {
		return servers.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func (f *fakeServerService) Validate(_ context.Context, id uuid.UUID, _ servers.ValidateAuth) (*servers.ValidationResult, error) {
	if f.validateErr != nil {
		return f.validateResult, f.validateErr
	}
	if f.validateResult != nil {
		return f.validateResult, nil
	}

	f.mu.Lock()
	item, ok := f.items[id]
	f.mu.Unlock()
	if !ok {
		return nil, servers.ErrNotFound
	}
	item.Status = servers.StatusReady
	return &servers.ValidationResult{
		Checks: []servers.CheckResult{
			{Name: "docker", OK: true, Detail: "24.0.7"},
			{Name: "cpu", OK: true, Detail: "Linux x86_64"},
			{Name: "ram", OK: true, Detail: "8192000 bytes"},
			{Name: "disk", OK: true, Detail: "20480000 bytes"},
		},
		Server: &item,
	}, nil
}

func (f *fakeServerService) AddPrivateKey(_ context.Context, name, privateKeyPEM string) (*servers.PrivateKey, error) {
	if name == "" || privateKeyPEM == "" {
		return nil, fmt.Errorf("%w: name and private_key are required", servers.ErrValidation)
	}
	return &servers.PrivateKey{ID: uuid.New(), Name: name, CreatedAt: time.Now().UTC()}, nil
}

// newServerRoutesTestServer builds a Server backed by the fake server service.
func newServerRoutesTestServer(t *testing.T, fake ServerService) *Server {
	t.Helper()

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s, err := New(cfg, logger, newFakeAuthService(), nil, nil, fake, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s
}

// authHeader is the bearer token the fake auth service accepts.
const authHeader = "Bearer valid-token"

func TestCreateServerRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers",
		`{"name":"web-1","ip":"10.0.0.5","port":22,"ssh_user":"root"}`, authHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var body serverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Server.ID == "" || body.Server.Name != "web-1" {
		t.Errorf("server = %+v", body.Server)
	}
	if body.Server.Status != servers.StatusPending {
		t.Errorf("status = %q, want %q", body.Server.Status, servers.StatusPending)
	}

	// A validation failure from the service maps to 400.
	fake.failAdd = fmt.Errorf("%w: name is required", servers.ErrValidation)
	rec = doRequest(t, s, http.MethodPost, "/api/v1/servers", `{"ip":"10.0.0.6","ssh_user":"root"}`, authHeader)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("validation status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	// Missing auth is rejected before the handler.
	rec = doRequest(t, s, http.MethodPost, "/api/v1/servers", `{"name":"x","ip":"1.2.3.4","ssh_user":"root"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestListServersRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	seedServer(t, fake, "web-2")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body serverListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(body.Servers))
	}
}

func TestGetServerRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-3")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+id.String(), "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/servers/"+uuid.New().String(), "", authHeader)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown status = %d, want 404", rec.Code)
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/servers/not-a-uuid", "", authHeader)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad id status = %d, want 400", rec.Code)
	}
}

func TestDeleteServerRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-4")

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/servers/"+id.String(), "", authHeader)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/servers/"+id.String(), "", authHeader)
	if rec.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", rec.Code)
	}
}

func TestValidateServerRouteSuccess(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-5")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/"+id.String()+"/validate", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body validateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Checks) != 4 {
		t.Fatalf("checks = %d, want 4", len(body.Checks))
	}
	if body.Server == nil || body.Server.Status != servers.StatusReady {
		t.Errorf("server = %+v, want status ready", body.Server)
	}
}

func TestValidateServerRouteCheckFailure(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-6")

	fake.validateResult = &servers.ValidationResult{
		Checks: []servers.CheckResult{
			{Name: "docker", OK: false, Detail: "docker is not installed or not on PATH"},
			{Name: "cpu", OK: true, Detail: "Linux x86_64"},
		},
		Server: &servers.Server{ID: id, Name: "web-6", Status: servers.StatusError},
	}
	fake.validateErr = fmt.Errorf("%w: docker check failed", servers.ErrValidation)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/"+id.String()+"/validate", "", authHeader)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
	}

	var body validateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Checks) != 2 || body.Message == "" {
		t.Errorf("body = %+v, want checks and a message", body)
	}
}

func TestValidateServerRouteNoCredentials(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-7")

	fake.validateErr = fmt.Errorf("%w: server has no SSH key", servers.ErrNoCredentials)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/"+id.String()+"/validate", "", authHeader)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePrivateKeyRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/private-keys",
		`{"name":"deploy-key","private_key":"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n"}`, authHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	var body privateKeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID == "" || body.Name != "deploy-key" {
		t.Errorf("body = %+v", body)
	}

	// Empty input maps to 400.
	rec = doRequest(t, s, http.MethodPost, "/api/v1/private-keys", `{"name":"","private_key":""}`, authHeader)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty input status = %d, want 400", rec.Code)
	}
}

// seedServer inserts one server into the fake and returns its ID.
func seedServer(t *testing.T, fake *fakeServerService, name string) uuid.UUID {
	t.Helper()

	server, err := fake.Add(context.Background(), testUserID, name, "10.0.0.9", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("seed server: %v", err)
	}
	return server.ID
}

// compile-time assertion that the fake satisfies the interface.
var _ ServerService = (*fakeServerService)(nil)
