package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/config"
)

// stubPinger is a deterministic Pinger for tests.
type stubPinger struct {
	err error
}

func (s stubPinger) Ping(context.Context) error {
	return s.err
}

// newTestServer builds a Server with injected pingers, avoiding live services.
func newTestServer(t *testing.T, db, redis Pinger) *Server {
	t.Helper()

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s, err := New(cfg, logger, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = db
	s.redis = redis
	return s
}

// TestNewResolvesSecretKey pins the wiring invariant behind the empty-key fix:
// a server constructed without GOTHAM_SECRET_KEY still holds a non-empty
// credential key, so no sealing site can fall back to SHA-256("").
func TestNewResolvesSecretKey(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})
	if s.secretKey == "" {
		t.Fatal("server resolved an empty credential secret key")
	}
}

// getHealthz performs GET /healthz against the server handler.
func getHealthz(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// decodeHealth decodes the /healthz JSON body.
func decodeHealth(t *testing.T, rec *httptest.ResponseRecorder) healthResponse {
	t.Helper()

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestHealthzHealthy(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})

	rec := getHealthz(t, s)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	body := decodeHealth(t, rec)
	want := healthResponse{Status: statusOK, DB: statusOK, Redis: statusOK}
	if body != want {
		t.Errorf("body = %+v, want %+v", body, want)
	}
}

func TestHealthzDegraded(t *testing.T) {
	tests := map[string]struct {
		db    error
		redis error
		want  healthResponse
	}{
		"postgres down": {
			db:    errors.New("connection refused"),
			redis: nil,
			want:  healthResponse{Status: "degraded", DB: statusDown, Redis: statusOK},
		},
		"redis down": {
			db:    nil,
			redis: errors.New("connection refused"),
			want:  healthResponse{Status: "degraded", DB: statusOK, Redis: statusDown},
		},
		"both down": {
			db:    errors.New("connection refused"),
			redis: errors.New("connection refused"),
			want:  healthResponse{Status: "degraded", DB: statusDown, Redis: statusDown},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t, stubPinger{err: tc.db}, stubPinger{err: tc.redis})

			rec := getHealthz(t, s)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
			}

			body := decodeHealth(t, rec)
			if body != tc.want {
				t.Errorf("body = %+v, want %+v", body, tc.want)
			}
		})
	}
}

func TestNewRejectsNilDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := New(nil, logger, nil, nil, nil, nil, nil); err == nil {
		t.Error("New(nil, logger) = nil error, want error")
	}
	if _, err := New(&config.Config{}, nil, nil, nil, nil, nil, nil); err == nil {
		t.Error("New(cfg, nil) = nil error, want error")
	}
}

// TestNewWarnsOnBroadTrustedProxy pins M1: an overly broad trusted prefix logs
// a warning that any host inside it can spoof the forwarded client address.
func TestNewWarnsOnBroadTrustedProxy(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := &config.Config{Values: config.Values{Server: config.Server{
		Addr:           "127.0.0.1",
		Port:           0,
		TrustedProxies: []string{"10.0.0.0/8"},
	}}}

	s, err := New(cfg, logger, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	if !strings.Contains(buf.String(), "overly broad prefix") {
		t.Errorf("log = %q, want an overly broad prefix warning", buf.String())
	}
}

// TestNewWarnsOnMissingTrustedProxyForOAuth pins M2: an OAuth-enabled
// deployment with no trusted proxies logs an upgrade warning.
func TestNewWarnsOnMissingTrustedProxyForOAuth(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := &config.Config{Values: config.Values{
		Server: config.Server{Addr: "127.0.0.1", Port: 0},
		OAuth: config.OAuth{GitHub: config.OAuthGitHub{
			RedirectURL: "https://gotham.example/api/v1/auth/oauth/github/callback",
		}},
	}}

	s, err := New(cfg, logger, nil, &fakeOAuthService{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	if !strings.Contains(buf.String(), "no trusted proxies configured") {
		t.Errorf("log = %q, want a missing trusted-proxy warning", buf.String())
	}
}
