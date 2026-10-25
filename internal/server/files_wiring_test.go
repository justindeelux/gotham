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

	if _, err := svc.Roots(context.Background(), server.ID); !errors.Is(err, files.ErrAgentUnavailable) {
		t.Fatalf("Roots err = %v, want ErrAgentUnavailable", err)
	}

	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+server.ID.String()+"/files/roots", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated roots = %d, want 401 (route mounted)", rec.Code)
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/servers/"+server.ID.String()+"/files/download?ticket=unknown", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown ticket = %d, want 404", rec.Code)
	}
}

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

func TestFilesServiceNilWithoutRegistry(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})
	if svc := s.fileService(); svc != nil {
		t.Fatalf("fileService() = %v, want nil without a registry", svc)
	}
}

func TestFilesRoutesRequireOwner(t *testing.T) {
	t.Setenv(files.FeatureEnv, "true")

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

	memberToken, err := tokens.Create(context.Background(), testUserID, "admin", []string{"admin"})
	if err != nil {
		t.Fatalf("create member admin token: %v", err)
	}
	if rec := doRequest(t, s, http.MethodGet, url, "", "Bearer "+memberToken.Token); rec.Code != http.StatusForbidden {
		t.Fatalf("member-owned admin token roots = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	ownerToken, err := tokens.Create(context.Background(), testOwnerID, "admin", []string{"admin"})
	if err != nil {
		t.Fatalf("create owner admin token: %v", err)
	}
	rec := doRequest(t, s, http.MethodGet, url, "", "Bearer "+ownerToken.Token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("owner admin token roots = %d, want past the owner gate (body %s)", rec.Code, rec.Body.String())
	}
}
