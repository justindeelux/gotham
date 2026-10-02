package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestRequestLoggerRecordsRecoveredPanic pins the middleware order: the request
// logger must wrap the recoverer so a recovered panic still produces one
// structured request line with status 500.
func TestRequestLoggerRecordsRecoveredPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &Server{logger: logger}

	r := chi.NewRouter()
	s.baseMiddleware(r)
	r.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	out := buf.String()
	if !strings.Contains(out, `"msg":"http request"`) {
		t.Fatalf("no request log line for recovered panic; log = %q", out)
	}
	if !strings.Contains(out, `"status":500`) {
		t.Fatalf("request log missing status 500; log = %q", out)
	}
}
