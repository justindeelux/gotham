package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testConfig(origin string) Config {
	return Config{URL: origin, Token: "gt1_test-secret", TeamID: uuid.MustParse("11111111-1111-4111-8111-111111111111")}
}

func acknowledge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(contractHeader, "1")
	w.Header().Set(teamHeader, r.Header.Get("X-Team-Id"))
}

func TestConfig(t *testing.T) {
	for _, origin := range []string{"https://example.com", "http://127.0.0.1:8000", "http://[::1]:8000/"} {
		if _, err := NewClient(testConfig(origin)); err != nil {
			t.Fatalf("valid origin: %v", err)
		}
	}
	for _, origin := range []string{"", "https://user:secret@example.com", "http://localhost", "http://example.com", "ftp://127.0.0.1", "https://example.com/a", "https://example.com?secret", "https://example.com#secret", "https://example.com?", "https://example.com#"} {
		if _, err := NewClient(testConfig(origin)); err == nil {
			t.Errorf("accepted unsafe origin %q", origin)
		}
	}
	for _, token := range []string{"", "gt1_", "eyJhbGciOi.secret", "gt1_secret\n", "gt1_ secret", strings.Repeat("x", 4097)} {
		cfg := testConfig("https://example.com")
		cfg.Token = token
		if _, err := NewClient(cfg); err == nil {
			t.Error("accepted invalid credential")
		} else if len(token) > 4 && strings.Contains(err.Error(), token) {
			t.Error("credential leaked")
		}
	}
	cfg := testConfig("https://example.com")
	cfg.TeamID = uuid.Nil
	if _, err := NewClient(cfg); err == nil {
		t.Error("accepted nil team")
	}
	_, err := ConfigFromEnv(func(string) string { return "secret" })
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsafe config error")
	}
}

func TestRESTBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ack    string
		code   string
	}{
		{"valid", 200, `{"value":1}`, "valid", ""},
		{"missing ack", 200, `{"value":1}`, "", "contract_mismatch"},
		{"wrong team", 200, `{"value":1}`, "wrong", "contract_mismatch"},
		{"wrong version", 200, `{"value":1}`, "version", "contract_mismatch"},
		{"unauthorized", 401, "gt1_test-secret", "", "unauthorized"},
		{"forbidden", 403, "gt1_test-secret", "", "forbidden"},
		{"not found", 404, "secret", "", "unsupported_or_not_found"},
		{"html error", 500, "<html>gt1_test-secret</html>", "", "upstream_error"},
		{"invalid JSON", 200, "gt1_test-secret", "valid", "invalid_response"},
		{"trailing JSON", 200, `{} {}`, "valid", "invalid_response"},
		{"trailing garbage", 200, `{} secret`, "valid", "invalid_response"},
		{"oversize", 200, strings.Repeat(" ", maxBody) + `{}`, "valid", "response_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer gt1_test-secret" || r.Header.Get(contractHeader) != "1" || r.Header.Get("X-Team-Id") != testConfig("").TeamID.String() {
					t.Error("missing fixed authority")
				}
				if tc.ack != "" {
					acknowledge(w, r)
				}
				if tc.ack == "wrong" {
					w.Header().Set(teamHeader, uuid.NewString())
				}
				if tc.ack == "version" {
					w.Header().Set(contractHeader, "2")
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer api.Close()
			c, err := NewClient(testConfig(api.URL))
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				Value int `json:"value"`
			}
			err = c.readJSON(context.Background(), "/api/v1/servers", &out)
			if tc.code == "" {
				if err != nil || out.Value != 1 {
					t.Fatalf("result %+v %v", out, err)
				}
				return
			}
			var safe *Error
			if !errors.As(err, &safe) || safe.Code != tc.code || strings.Contains(err.Error(), c.config.Token) {
				t.Fatalf("error=%v want %s", err, tc.code)
			}
			if out.Value != 0 {
				t.Fatal("projected data before acknowledgement")
			}
		})
	}
}

func TestRedirectsNeverFollow(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/secret")
				w.WriteHeader(status)
			}))
			defer api.Close()
			c, _ := NewClient(testConfig(api.URL))
			err := c.readJSON(context.Background(), "/api/v1/servers", new(any))
			if err == nil || err.Error() != "redirect_refused" || calls != 1 {
				t.Fatalf("redirect: %v calls=%d", err, calls)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer api.Close()
	c, _ := NewClient(testConfig(api.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.readJSON(ctx, "/api/v1/servers", new(any)); err == nil || err.Error() != "cancelled" {
		t.Fatalf("cancel: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.readJSON(ctx, "/api/v1/servers", new(any)); err == nil || err.Error() != "deadline_exceeded" {
		t.Fatalf("deadline: %v", err)
	}
}

func TestHealthRequiresCurrentContext(t *testing.T) {
	authorized := true
	healthCalls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/mcp/context" {
			if !authorized {
				w.WriteHeader(401)
				return
			}
			acknowledge(w, r)
			_, _ = fmt.Fprintf(w, `{"team_id":%q,"capabilities":["strict_reads"]}`, r.Header.Get("X-Team-Id"))
			return
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Team-Id") != "" || r.Header.Get(contractHeader) != "" {
			t.Error("public health received authority headers")
		}
		healthCalls++
		w.WriteHeader(503)
		_, _ = fmt.Fprint(w, `{"status":"degraded","db":"error","redis":"ok"}`)
	}))
	defer api.Close()
	c, _ := NewClient(testConfig(api.URL))
	health, err := c.Health(context.Background())
	if err != nil || health.Status != "degraded" {
		t.Fatalf("503 health=%+v %v", health, err)
	}
	authorized = false
	if _, err = c.Health(context.Background()); err == nil || err.Error() != "unauthorized" || healthCalls != 1 {
		t.Fatalf("health auth=%v calls=%d", err, healthCalls)
	}
}

func TestUnsafeAndOversizedPaths(t *testing.T) {
	c, _ := NewClient(testConfig("https://example.com"))
	for _, path := range []string{"https://other.example", "//other.example", "/../secret", "/./secret", "/api/%2e%2e/admin", "/api/%2E%2E/admin", "/api/%2e/admin", "/api/a%2f..%2fadmin", "/api/%zz", "/api/#fragment", "/api/\\secret", "/" + strings.Repeat("x", 8192)} {
		if err := c.readJSON(context.Background(), path, new(any)); err == nil || err.Error() != "invalid_request" {
			t.Fatalf("path boundary=%v", err)
		}
	}
}

func TestCrossOriginRedirectDoesNotForwardCredential(t *testing.T) {
	received := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	c, _ := NewClient(testConfig(origin.URL))
	if err := c.readJSON(context.Background(), "/api/v1/projects", new(any)); err == nil || err.Error() != "redirect_refused" {
		t.Fatalf("redirect=%v", err)
	}
	if received {
		t.Fatal("forwarded credential to redirect target")
	}
}

func TestPathSegmentsAllowEmbeddedDots(t *testing.T) {
	for _, path := range []string{"/api/v1/projects/a..b", "/api/v1/projects/a%2E%2Eb", "/api/v1/projects/a%2Fb", "/api/v1/projects?name=.."} {
		t.Run(path, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != path {
					t.Errorf("path=%s want %s", r.URL.RequestURI(), path)
				}
				acknowledge(w, r)
				_, _ = fmt.Fprint(w, `{}`)
			}))
			defer api.Close()
			c, _ := NewClient(testConfig(api.URL))
			if err := c.readJSON(context.Background(), path, new(any)); err != nil {
				t.Fatalf("valid path: %v", err)
			}
		})
	}
}
