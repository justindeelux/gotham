package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type versionedFakeServerService struct {
	ServerService
	version string
}

func (f *versionedFakeServerService) Version() string { return f.version }

func newVersionTestServer(t *testing.T, version string) *Server {
	t.Helper()
	return newServerRoutesTestServer(t, &versionedFakeServerService{
		ServerService: newFakeServerService(),
		version:       version,
	})
}

func decodeVersion(t *testing.T, rec *httptest.ResponseRecorder) versionResponse {
	t.Helper()
	var body versionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestGetVersionRoute(t *testing.T) {
	s := newVersionTestServer(t, "1.2.3")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/version", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if body := decodeVersion(t, rec); body.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", body.Version, "1.2.3")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

func TestGetVersionRouteUnauthenticated(t *testing.T) {
	s := newVersionTestServer(t, "1.2.3")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/version", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (body %s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestGetVersionRouteDevBuild(t *testing.T) {
	for _, version := range []string{"dev", ""} {
		s := newVersionTestServer(t, version)

		rec := doRequest(t, s, http.MethodGet, "/api/v1/version", "", authHeader)
		if rec.Code != http.StatusOK {
			t.Fatalf("version %q: status = %d, want %d (body %s)", version, rec.Code, http.StatusOK, rec.Body.String())
		}
		if body := decodeVersion(t, rec); body.Version != "dev" {
			t.Errorf("version %q: body = %q, want %q", version, body.Version, "dev")
		}
	}
}
