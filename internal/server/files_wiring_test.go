package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/servers"
)

// fakeFileServer is a ServerService stand-in that exposes the FileService dial
// seam. The embedded nil interface supplies the unused methods; the two used
// here (Get and DialFileClient) are overridden.
type fakeFileServer struct {
	ServerService
	server  *servers.Server
	dialErr error
}

func (f fakeFileServer) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if f.server != nil && f.server.ID == id {
		return f.server, nil
	}
	return nil, servers.ErrNotFound
}

func (f fakeFileServer) DialFileClient(context.Context, uuid.UUID, ...servers.DockerDialOption) (*servers.FileClient, error) {
	if f.dialErr != nil {
		return nil, f.dialErr
	}
	return nil, fmt.Errorf("%w: no agent in test", files.ErrAgentUnavailable)
}

// TestFilesServiceWiring proves the files package is mounted through the real
// server chain with the mTLS dial seam plugged in: fileService() is non-nil,
// the read route is mounted behind auth (401, not 404), and the service routes
// its dial through the registry's DialFileClient.
func TestFilesServiceWiring(t *testing.T) {
	t.Setenv(files.FeatureEnv, "true")

	server := &servers.Server{ID: uuid.New(), Name: "node-1", IP: "10.0.0.5", TeamID: uuid.New()}
	registry := fakeFileServer{server: server}

	cfg := &config.Config{Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, nil, nil, nil, registry, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	svc := s.fileService()
	if svc == nil {
		t.Fatal("fileService() = nil, want a mounted service")
	}

	// The dial seam is wired: a request reaches DialFileClient, which fails in
	// the test, surfacing as ErrAgentUnavailable.
	if _, err := svc.Roots(context.Background(), server.ID); !errors.Is(err, files.ErrAgentUnavailable) {
		t.Fatalf("Roots err = %v, want ErrAgentUnavailable", err)
	}

	// The authenticated read route is mounted: without a bearer token the real
	// chain answers 401, not the API 404.
	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+server.ID.String()+"/files/roots", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated roots = %d, want 401 (route mounted)", rec.Code)
	}

	// The download route is public (the ticket is its credential); an unknown
	// ticket is refused by the service, proving the route reaches it.
	rec = doRequest(t, s, http.MethodGet, "/api/v1/servers/"+server.ID.String()+"/files/download?ticket=unknown", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown ticket = %d, want 404", rec.Code)
	}
}

// TestFilesServiceWiringDisabled proves FEATURE_FILES=false disables the routes through
// the real chain: a mounted route answers 503, not 404.
func TestFilesServiceWiringDisabled(t *testing.T) {
	t.Setenv(files.FeatureEnv, "false")

	server := &servers.Server{ID: uuid.New(), TeamID: uuid.New()}
	cfg := &config.Config{Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, nil, nil, nil, fakeFileServer{server: server}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+server.ID.String()+"/files/download?ticket=unknown", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled download = %d, want 503", rec.Code)
	}
}

// TestFilesServiceNilWithoutRegistry proves the service stays absent when no
// node registry is wired, so Mount is a no-op.
func TestFilesServiceNilWithoutRegistry(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})
	if svc := s.fileService(); svc != nil {
		t.Fatalf("fileService() = %v, want nil without a registry", svc)
	}
}

// TestFilesRoutesRequirePlatformAdmin proves the file surface is limited to
// platform operators: a plain session is refused with 403 before the file
// service runs, while an admin-scoped API token reaches it.
func TestFilesRoutesRequirePlatformAdmin(t *testing.T) {
	t.Setenv(files.FeatureEnv, "true")
	t.Setenv(PlatformAdminsEnv, "")

	server := &servers.Server{ID: uuid.New(), Name: "node-1", TeamID: uuid.New()}
	cfg := &config.Config{Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := newFakeTokenService()
	s, err := New(cfg, logger, newFakeAuthService(), nil, tokens, fakeFileServer{server: server}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}

	url := "/api/v1/servers/" + server.ID.String() + "/files/roots"
	if rec := doRequest(t, s, http.MethodGet, url, "", "Bearer valid-token"); rec.Code != http.StatusForbidden {
		t.Fatalf("plain session roots = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	adminToken, err := tokens.Create(context.Background(), testUserID, "admin", []string{"admin"})
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	rec := doRequest(t, s, http.MethodGet, url, "", "Bearer "+adminToken.Token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("admin token roots = %d, want past the platform gate (body %s)", rec.Code, rec.Body.String())
	}
}
