package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

	s, err := New(cfg, logger, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = db
	s.redis = redis
	return s
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

	if _, err := New(nil, logger, nil, nil); err == nil {
		t.Error("New(nil, logger) = nil error, want error")
	}
	if _, err := New(&config.Config{}, nil, nil, nil); err == nil {
		t.Error("New(cfg, nil) = nil error, want error")
	}
}
