package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestSnapshotMaskingPreservesShortEnvironmentValues(t *testing.T) {
	env := map[string]string{"DEBUG": "1", "BOOL": "true", "ENV": "prod", "PORT": "3000", "SHORT_SECRET": "abcde"}
	const raw = "retry 1 of 10; true prod 3000 abcde"
	for _, incomplete := range []bool{false, true} {
		if got := maskLog(raw, env, incomplete, 256); got != raw {
			t.Fatalf("incomplete=%v: short environment values corrupted logs: %q", incomplete, got)
		}
	}
	if got := maskLog("abcdef", map[string]string{"SECRET": "abcdef"}, false, 256); got != "<redacted>" {
		t.Fatalf("six-byte value was not masked: %q", got)
	}
}

func TestSnapshotCredentialFieldMasking(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"Authorization: Basic dXNlcjpwYXNz", "Authorization: <redacted>"},
		{"Authorization: Bearer private-value", "Authorization: <redacted>"},
		{"authorization=basic dXNlcjpwYXNz", "authorization=<redacted>"},
		{`{"password":"a b c"}`, `{"password":"<redacted>"}`},
		{`{"password":"a \"b\" c"}`, `{"password":"<redacted>"}`},
		{"password='a b c'", "password='<redacted>'"},
		{`{"authorization":"Basic dXNlcjpwYXNz"}`, `{"authorization":"<redacted>"}`},
		{`password="unterminated secret`, `password="<redacted>"`},
	} {
		if got := maskLog(tc.raw, nil, false, 256); got != tc.want {
			t.Errorf("maskLog(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSnapshotLegacyBuildLogQueryCompatibility(t *testing.T) {
	router := chi.NewRouter()
	user, app, dep := uuid.New(), uuid.New(), uuid.New()
	Mount(router, func(h http.Handler) http.Handler { return h }, func(context.Context) (uuid.UUID, bool) { return user, true }, &fakeDeployService{})
	for _, query := range []string{"", "?_=1", "?unknown=cache-buster", "?service=web"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/applications/"+app.String()+"/deployments/"+dep.String()+"/logs"+query, nil))
		if rr.Code != 200 || rr.Body.String() != "{\"log\":\"\"}\n" {
			t.Errorf("legacy query %q: status=%d body=%s", query, rr.Code, rr.Body.String())
		}
	}
	for _, query := range []string{"?tail=", "?max_bytes=0", "?tail=10&_=1"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/applications/"+app.String()+"/deployments/"+dep.String()+"/logs"+query, nil))
		if rr.Code != 400 {
			t.Errorf("bounded query %q: status=%d", query, rr.Code)
		}
	}
}
