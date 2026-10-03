package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
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
	hasPassword    map[uuid.UUID]bool
	failAdd        error
	validateResult *servers.ValidationResult
	validateErr    error
	lastAuth       servers.ValidateAuth
	lastPassword   string
	metrics        []servers.MetricPoint
	metricsErr     error
}

func newFakeServerService() *fakeServerService {
	return &fakeServerService{items: make(map[uuid.UUID]servers.Server), hasPassword: make(map[uuid.UUID]bool)}
}

func (f *fakeServerService) Add(_ context.Context, _ uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID, password string) (*servers.Server, error) {
	if f.failAdd != nil {
		return nil, f.failAdd
	}
	if sshKeyID != uuid.Nil && password != "" {
		return nil, fmt.Errorf("%w: provide either ssh_key_id or password, not both", servers.ErrValidation)
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
	if password != "" {
		server.HasPassword = true
		f.mu.Lock()
		f.lastPassword = password
		f.mu.Unlock()
	}

	f.mu.Lock()
	f.items[server.ID] = *server
	if password != "" {
		f.hasPassword[server.ID] = true
	}
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
	delete(f.hasPassword, id)
	return nil
}

// Update applies a PATCH edit with the same merge semantics as the domain
// service: nil leaves a field unchanged, a key ID of uuid.Nil detaches the
// key, and a password sets (or, when empty, forgets) the secret.
func (f *fakeServerService) Update(_ context.Context, id uuid.UUID, params servers.UpdateParams) (*servers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	item, ok := f.items[id]
	if !ok {
		return nil, servers.ErrNotFound
	}
	addressChanged := false
	if params.Name != nil {
		if *params.Name == "" {
			return nil, fmt.Errorf("%w: name is required", servers.ErrValidation)
		}
		item.Name = *params.Name
	}
	if params.IP != nil {
		if *params.IP == "" {
			return nil, fmt.Errorf("%w: ip is required", servers.ErrValidation)
		}
		if *params.IP != item.IP {
			addressChanged = true
		}
		item.IP = *params.IP
	}
	if params.Port != nil {
		if *params.Port < 1 || *params.Port > 65535 {
			return nil, fmt.Errorf("%w: port must be between 1 and 65535", servers.ErrValidation)
		}
		if *params.Port != item.Port {
			addressChanged = true
		}
		item.Port = *params.Port
	}
	if params.SSHUser != nil {
		if *params.SSHUser == "" {
			return nil, fmt.Errorf("%w: ssh_user is required", servers.ErrValidation)
		}
		if *params.SSHUser != item.SSHUser {
			addressChanged = true
		}
		item.SSHUser = *params.SSHUser
	}
	if params.SSHKeyID != nil {
		if *params.SSHKeyID == uuid.Nil {
			item.SSHKeyID = nil
		} else {
			keyID := *params.SSHKeyID
			item.SSHKeyID = &keyID
		}
		delete(f.hasPassword, id)
		addressChanged = true
	}
	if params.Password != nil {
		if *params.Password == "" {
			delete(f.hasPassword, id)
		} else {
			f.lastPassword = *params.Password
			f.hasPassword[id] = true
		}
		item.SSHKeyID = nil
		addressChanged = true
	}
	if addressChanged {
		item.HostKeyFingerprint = nil
		item.Status = servers.StatusPending
	}
	item.HasPassword = f.hasPassword[id]
	f.items[id] = item
	return &item, nil
}

func (f *fakeServerService) Validate(_ context.Context, id uuid.UUID, auth servers.ValidateAuth) (*servers.ValidationResult, error) {
	f.mu.Lock()
	f.lastAuth = auth
	f.mu.Unlock()

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

func (f *fakeServerService) ResetHostKey(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	item, ok := f.items[id]
	if !ok {
		return nil, servers.ErrNotFound
	}
	item.HostKeyFingerprint = nil
	f.items[id] = item
	return &item, nil
}

func (f *fakeServerService) AddPrivateKey(_ context.Context, name, privateKeyPEM string) (*servers.PrivateKey, error) {
	if name == "" || privateKeyPEM == "" {
		return nil, fmt.Errorf("%w: name and private_key are required", servers.ErrValidation)
	}
	return &servers.PrivateKey{ID: uuid.New(), Name: name, CreatedAt: time.Now().UTC()}, nil
}

// Metrics returns the canned series, or the canned error, of the fake.
func (f *fakeServerService) Metrics(_ context.Context, id uuid.UUID, _, _ time.Time, _ string) ([]servers.MetricPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.metricsErr != nil {
		return nil, f.metricsErr
	}
	if _, ok := f.items[id]; !ok {
		return nil, servers.ErrNotFound
	}
	return f.metrics, nil
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

func TestValidateServerRouteForwardsPassphrase(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-pw")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/"+id.String()+"/validate",
		`{"passphrase":"s3cret"}`, authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	fake.mu.Lock()
	got := fake.lastAuth.Passphrase
	fake.mu.Unlock()
	if got != "s3cret" {
		t.Errorf("passphrase forwarded to the service = %q, want s3cret", got)
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

func TestResetServerHostKeyRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "web-8")

	fingerprint := "SHA256:abc"
	fake.mu.Lock()
	item := fake.items[id]
	item.HostKeyFingerprint = &fingerprint
	fake.items[id] = item
	fake.mu.Unlock()

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/servers/"+id.String()+"/host-key", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body serverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Server.HostKeyFingerprint != nil {
		t.Errorf("host_key_fingerprint = %q, want null after reset", *body.Server.HostKeyFingerprint)
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/servers/"+uuid.New().String()+"/host-key", "", authHeader)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown status = %d, want 404", rec.Code)
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

	server, err := fake.Add(context.Background(), testUserID, name, "10.0.0.9", 22, "root", uuid.Nil, "")
	if err != nil {
		t.Fatalf("seed server: %v", err)
	}
	return server.ID
}

// setTeam stamps a seeded server with a team, which is what the container
// routes authorize against.
func (f *fakeServerService) setTeam(id uuid.UUID, teamID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	server, ok := f.items[id]
	if !ok {
		panic("server not found")
	}
	server.TeamID = teamID
	f.items[id] = server
}

// compile-time assertion that the fake satisfies the interface.
var _ ServerService = (*fakeServerService)(nil)

func TestCreateServerRouteWithPassword(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers",
		`{"name":"pw-1","ip":"10.0.0.5","port":22,"ssh_user":"root","password":"s3cret-node-pw"}`, authHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var body serverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Server.HasPassword {
		t.Errorf("has_password = false, want true")
	}
	if body.Server.SSHKeyID != nil {
		t.Errorf("ssh_key_id = %v, want null for password auth", *body.Server.SSHKeyID)
	}

	// The secret must never appear in the response body.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if strings.Contains(rec.Body.String(), "s3cret-node-pw") {
		t.Errorf("response body echoes the password: %s", rec.Body.String())
	}

	// Key and password together are rejected.
	rec = doRequest(t, s, http.MethodPost, "/api/v1/servers",
		`{"name":"pw-2","ip":"10.0.0.6","ssh_user":"root","ssh_key_id":"`+uuid.New().String()+`","password":"s3cret"}`, authHeader)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("key+password status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("validation error echoes the password: %s", rec.Body.String())
	}
}

func TestUpdateServerRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "edit-1")

	// Pin a host key first: an address change must reset it.
	fingerprint := "SHA256:abc"
	fake.mu.Lock()
	item := fake.items[id]
	item.HostKeyFingerprint = &fingerprint
	item.Status = servers.StatusReady
	fake.items[id] = item
	fake.mu.Unlock()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/servers/"+id.String(),
		`{"name":"edit-1-renamed","ip":"10.0.0.99"}`, authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body serverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Server.Name != "edit-1-renamed" || body.Server.IP != "10.0.0.99" {
		t.Errorf("server = %+v, want renamed address", body.Server)
	}
	if body.Server.HostKeyFingerprint != nil {
		t.Errorf("host_key_fingerprint = %q, want null after address change", *body.Server.HostKeyFingerprint)
	}
	if body.Server.Status != servers.StatusPending {
		t.Errorf("status = %q, want pending after address change", body.Server.Status)
	}

	// A name-only edit keeps the pin.
	fake.mu.Lock()
	item = fake.items[id]
	item.HostKeyFingerprint = &fingerprint
	item.Status = servers.StatusReady
	fake.items[id] = item
	fake.mu.Unlock()

	rec = doRequest(t, s, http.MethodPatch, "/api/v1/servers/"+id.String(),
		`{"name":"edit-1-again"}`, authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Server.HostKeyFingerprint == nil || *body.Server.HostKeyFingerprint != fingerprint {
		t.Errorf("host_key_fingerprint = %+v, want pin kept on name-only edit", body.Server.HostKeyFingerprint)
	}

	// Setting a password switches the auth mode and resets the pin.
	rec = doRequest(t, s, http.MethodPatch, "/api/v1/servers/"+id.String(),
		`{"password":"new-node-pw"}`, authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Server.HasPassword {
		t.Errorf("has_password = false, want true after password edit")
	}
	if strings.Contains(rec.Body.String(), "new-node-pw") {
		t.Errorf("response body echoes the password: %s", rec.Body.String())
	}
}

func TestUpdateServerRouteValidation(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "edit-2")

	tests := []struct {
		name   string
		target string
		body   string
		want   int
	}{
		{"empty name", "/api/v1/servers/" + id.String(), `{"name":""}`, http.StatusBadRequest},
		{"empty ip", "/api/v1/servers/" + id.String(), `{"ip":""}`, http.StatusBadRequest},
		{"bad port", "/api/v1/servers/" + id.String(), `{"port":70000}`, http.StatusBadRequest},
		{"empty user", "/api/v1/servers/" + id.String(), `{"ssh_user":""}`, http.StatusBadRequest},
		{"bad key id", "/api/v1/servers/" + id.String(), `{"ssh_key_id":"nope"}`, http.StatusBadRequest},
		{"bad id", "/api/v1/servers/not-a-uuid", `{"name":"x"}`, http.StatusBadRequest},
		{"unknown server", "/api/v1/servers/" + uuid.New().String(), `{"name":"x"}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPatch, tt.target, tt.body, authHeader)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	// The route requires authentication.
	if rec := doRequest(t, s, http.MethodPatch, "/api/v1/servers/"+id.String(), `{"name":"x"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestServerMetricsRoute(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "metrics-1")

	bucket := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	fake.metrics = []servers.MetricPoint{{
		Bucket:         bucket,
		CPUUsage:       0.25,
		MemUsage:       0.5,
		DiskUsage:      0.75,
		NetRxBps:       1024,
		NetTxBps:       512,
		DiskReadBps:    2048,
		DiskWriteBps:   4096,
		ContainerCount: 2,
	}}

	rec := doRequest(t, s, http.MethodGet,
		"/api/v1/servers/"+id.String()+"/metrics?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z&step=1m",
		"", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body metricsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Step != "1m" {
		t.Errorf("step = %q, want 1m", body.Step)
	}
	if len(body.Points) != 1 {
		t.Fatalf("points = %d, want 1", len(body.Points))
	}
	point := body.Points[0]
	if !point.Bucket.Equal(bucket) {
		t.Errorf("bucket = %s, want %s", point.Bucket, bucket)
	}
	if point.CPUUsage != 0.25 || point.MemUsage != 0.5 || point.DiskUsage != 0.75 {
		t.Errorf("usage = %+v", point)
	}
	if point.NetRxBps != 1024 || point.DiskWriteBps != 4096 || point.ContainerCount != 2 {
		t.Errorf("io = %+v", point)
	}

	// The step defaults to 1m when the query names none.
	rec = doRequest(t, s, http.MethodGet,
		"/api/v1/servers/"+id.String()+"/metrics?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z",
		"", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("default step status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Step != "1m" {
		t.Errorf("default step = %q, want 1m", body.Step)
	}
}

func TestServerMetricsRouteValidation(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "metrics-2")
	base := "/api/v1/servers/" + id.String() + "/metrics"

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{"missing from", base + "?to=2026-09-29T13:00:00Z", http.StatusBadRequest},
		{"missing to", base + "?from=2026-09-29T11:00:00Z", http.StatusBadRequest},
		{"invalid from", base + "?from=yesterday&to=2026-09-29T13:00:00Z", http.StatusBadRequest},
		{"invalid to", base + "?from=2026-09-29T11:00:00Z&to=soon", http.StatusBadRequest},
		{"bad id", "/api/v1/servers/not-a-uuid/metrics?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z", http.StatusBadRequest},
		{"unknown server", "/api/v1/servers/" + uuid.New().String() + "/metrics?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodGet, tt.target, "", authHeader)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	// A domain validation failure (bad step, inverted or oversized range) maps
	// to 400.
	fake.metricsErr = fmt.Errorf("%w: step must be one of 1m, 1h or 1d", servers.ErrValidation)
	rec := doRequest(t, s, http.MethodGet, base+"?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z&step=7m", "", authHeader)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("domain validation status = %d, want 400", rec.Code)
	}

	// The route requires authentication.
	if rec := doRequest(t, s, http.MethodGet, base+"?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestServerMetricsRouteDisabled(t *testing.T) {
	t.Setenv(servers.FeatureEnv, "false")

	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)
	id := seedServer(t, fake, "metrics-3")

	rec := doRequest(t, s, http.MethodGet,
		"/api/v1/servers/"+id.String()+"/metrics?from=2026-09-29T11:00:00Z&to=2026-09-29T13:00:00Z",
		"", authHeader)
	if rec.Code != http.StatusNotFound {
		t.Errorf("metrics status = %d, want 404 when FEATURE_METRICS=false", rec.Code)
	}

	// The rest of the servers surface stays mounted.
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+id.String(), "", authHeader); rec.Code != http.StatusOK {
		t.Errorf("server detail status = %d, want 200 with metrics disabled", rec.Code)
	}
}
