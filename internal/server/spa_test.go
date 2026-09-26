package server

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve is a small helper that issues a GET request through the server handler.
func serve(t *testing.T, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestSPAServesStaticAsset(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})

	matches, err := fs.Glob(webDist, webDistDir+"/"+assetsDir+"*")
	if err != nil {
		t.Fatalf("glob embedded assets: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no assets embedded under %s/%s", webDistDir, assetsDir)
	}

	target := strings.TrimPrefix(matches[0], webDistDir)
	rec := serve(t, s, target)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want %d (body %s)", target, rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Errorf("GET %s returned an empty body", target)
	}
}

func TestSPAFallbackReturnsIndex(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})

	routes := []string{"/", "/dashboard", "/apps/123/settings"}
	for _, route := range routes {
		rec := serve(t, s, route)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want %d", route, rec.Code, http.StatusOK)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want text/html", route, ct)
		}
		if !strings.Contains(rec.Body.String(), `id="app"`) {
			t.Errorf("GET %s did not return the SPA shell", route)
		}
	}
}

func TestSPAUnknownAssetIsNotFound(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})

	rec := serve(t, s, "/assets/does-not-exist.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /assets/does-not-exist.js = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAPINotFoundIsJSON(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})

	rec := serve(t, s, "/api/v1/unknown")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/unknown = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /api/v1/unknown Content-Type = %q, want application/json", ct)
	}
}

func TestSPADevProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Dev-Backend", "vite")
		_, _ = io.WriteString(w, "vite dev server")
	}))
	defer backend.Close()

	t.Setenv(webDevEnv, "1")
	t.Setenv(webDevURLEnv, backend.URL)

	s := newTestServer(t, stubPinger{}, stubPinger{})

	rec := serve(t, s, "/dashboard")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Dev-Backend"); got != "vite" {
		t.Errorf("X-Dev-Backend = %q, want %q", got, "vite")
	}
}
