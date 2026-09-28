package templates

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/services"
)

// routeTestServer mounts the template routes for one service behind a
// pass-through authentication wrapper.
func routeTestServer(t *testing.T, svc Service) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	Mount(router, auth, svc)
	return router
}

// doRequest serves one request against the handler and returns the recorder.
func doRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, reader)
	handler.ServeHTTP(recorder, request)
	return recorder
}

// TestRoutesList proves the catalog answer is metadata only.
func TestRoutesList(t *testing.T) {
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	recorder := doRequest(t, router, http.MethodGet, "/v1/templates", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/templates = %d: %s", recorder.Code, recorder.Body)
	}
	var list struct {
		Templates []struct {
			Slug        string `json:"slug"`
			Name        string `json:"name"`
			Icon        string `json:"icon"`
			Description string `json:"description"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Templates) != 1 || list.Templates[0].Slug != "demo" || list.Templates[0].Name != "Demo" {
		t.Fatalf("templates = %+v", list.Templates)
	}
	if strings.Contains(recorder.Body.String(), `"fields"`) {
		t.Errorf("the catalog must not carry the field schema: %s", recorder.Body)
	}
}

// TestRoutesGet proves the detail answer carries the form schema.
func TestRoutesGet(t *testing.T) {
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	recorder := doRequest(t, router, http.MethodGet, "/v1/templates/demo", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/templates/demo = %d: %s", recorder.Code, recorder.Body)
	}
	var detail struct {
		Template struct {
			Slug   string  `json:"slug"`
			Fields []Field `json:"fields"`
		} `json:"template"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Template.Slug != "demo" || len(detail.Template.Fields) != 6 {
		t.Fatalf("template = %+v", detail.Template)
	}
	secret := detail.Template.Fields[4]
	if secret.Key != "password" || secret.Type != FieldSecret || secret.Default != nil || !secret.Required {
		t.Errorf("secret field = %+v", secret)
	}
	if !strings.Contains(recorder.Body.String(), `"options":["fast","slow"]`) {
		t.Errorf("select options are not serialised: %s", recorder.Body)
	}
}

// TestRoutesNotFound proves an unknown slug is a 404 on both read and render.
func TestRoutesNotFound(t *testing.T) {
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/templates/nope", ""},
		{http.MethodPost, "/v1/templates/nope/render", `{"values":{}}`},
	} {
		recorder := doRequest(t, router, tc.method, tc.path, tc.body)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404: %s", tc.method, tc.path, recorder.Code, recorder.Body)
		}
	}
}

// TestRoutesRender proves the FE-7.1 contract: the response carries the
// validated compose document and its parsed domain map.
func TestRoutesRender(t *testing.T) {
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	recorder := doRequest(t, router, http.MethodPost, "/v1/templates/demo/render",
		`{"values":{"domain":"app.example.com","password":"p$ss"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("render = %d: %s", recorder.Code, recorder.Body)
	}
	var response struct {
		Slug        string            `json:"slug"`
		ComposeYAML string            `json:"compose_yaml"`
		Env         map[string]string `json:"env"`
		Spec        struct {
			Services []string `json:"services"`
			Domains  []struct {
				Service string `json:"service"`
				Domain  string `json:"domain"`
				Port    int32  `json:"port"`
			} `json:"domains"`
			NamedVolumes []string `json:"named_volumes"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Slug != "demo" || !strings.Contains(response.ComposeYAML, "app.example.com") {
		t.Fatalf("render response = %+v", response)
	}
	if len(response.Spec.Services) != 1 || response.Spec.Services[0] != "web" {
		t.Errorf("services = %v", response.Spec.Services)
	}
	if len(response.Spec.Domains) != 1 || response.Spec.Domains[0].Domain != "app.example.com" || response.Spec.Domains[0].Port != 80 {
		t.Errorf("domains = %+v", response.Spec.Domains)
	}
	if len(response.Spec.NamedVolumes) != 1 || response.Spec.NamedVolumes[0] != "data" {
		t.Errorf("named volumes = %v", response.Spec.NamedVolumes)
	}
	// Secrets travel in env, never in the document.
	if got := response.Env["password"]; got != "p$ss" {
		t.Errorf("env[password] = %q", got)
	}
	if strings.Contains(response.ComposeYAML, "p$ss") {
		t.Fatalf("the response document contains the secret:\n%s", response.ComposeYAML)
	}
	// The document survives the services pipeline with the returned
	// environment, which is what the gallery forwards to POST /v1/services.
	if _, err := services.Render(response.ComposeYAML, response.Env); err != nil {
		t.Fatalf("services.Render: %v", err)
	}
}

// TestRoutesRenderErrors proves validation failures are 400s with a clear
// message and malformed bodies are rejected.
func TestRoutesRenderErrors(t *testing.T) {
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	cases := map[string]struct {
		body string
		want string
	}{
		"missing required": {body: `{"values":{"domain":"app.example.com"}}`, want: "is required"},
		"unknown field":    {body: `{"values":{"domain":"a.b","password":"x","typo":1}}`, want: "unknown field"},
		"invalid value":    {body: `{"values":{"domain":"a.b","password":"x","count":"many"}}`, want: "whole number"},
		"empty body":       {body: "", want: "request body is required"},
		"not json":         {body: "values", want: "invalid request body"},
		"wrong shape":      {body: `{"values":5}`, want: "invalid request body"},
		"unknown key":      {body: `{"valuez":{}}`, want: "invalid request body"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := doRequest(t, router, http.MethodPost, "/v1/templates/demo/render", tc.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("render = %d, want 400: %s", recorder.Code, recorder.Body)
			}
			if !strings.Contains(recorder.Body.String(), tc.want) {
				t.Errorf("body = %s, want it to contain %q", recorder.Body, tc.want)
			}
		})
	}
}

// TestRoutesRequireAuth proves the auth wrapper is the only gate: a rejecting
// wrapper answers 401 on every endpoint.
func TestRoutesRequireAuth(t *testing.T) {
	router := chi.NewRouter()
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
	Mount(router, auth, mustCatalog(t, sampleFiles()))
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/templates"},
		{http.MethodGet, "/v1/templates/demo"},
		{http.MethodPost, "/v1/templates/demo/render"},
	} {
		recorder := doRequest(t, router, tc.method, tc.path, `{}`)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, recorder.Code)
		}
	}
}

// TestRoutesRenderBodyStrictness proves the endpoint consumes exactly one JSON
// object and rejects over-limit bodies: trailing garbage, a second document
// and an oversized body are all refused (only trailing whitespace is fine).
func TestRoutesRenderBodyStrictness(t *testing.T) {
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	valid := `{"values":{"domain":"app.example.com","password":"x"}}`
	cases := map[string]struct {
		body string
		want int
	}{
		"trailing garbage": {body: valid + "garbage", want: http.StatusBadRequest},
		"second document":  {body: valid + `{"unknown":true}`, want: http.StatusBadRequest},
		"second null":      {body: valid + "null", want: http.StatusBadRequest},
		"oversized body":   {body: valid + strings.Repeat(" ", maxBodyBytes), want: http.StatusRequestEntityTooLarge},
		"trailing space":   {body: valid + "\n\t  ", want: http.StatusOK},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := doRequest(t, router, http.MethodPost, "/v1/templates/demo/render", tc.body)
			if recorder.Code != tc.want {
				t.Fatalf("render = %d, want %d: %s", recorder.Code, tc.want, recorder.Body)
			}
		})
	}
}

// TestRoutesDisabledAtRuntime proves FEATURE_SERVICES=false after mounting
// answers 503 on every endpoint, exactly like the service operations; the
// startup-time flag instead unmounts the routes (404, see TestMountDisabled).
func TestRoutesDisabledAtRuntime(t *testing.T) {
	t.Setenv(services.FeatureEnv, "")
	router := routeTestServer(t, mustCatalog(t, sampleFiles()))
	if recorder := doRequest(t, router, http.MethodGet, "/v1/templates", ""); recorder.Code != http.StatusOK {
		t.Fatalf("enabled status = %d, want 200", recorder.Code)
	}
	t.Setenv(services.FeatureEnv, "false")
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/templates", ""},
		{http.MethodGet, "/v1/templates/demo", ""},
		{http.MethodPost, "/v1/templates/demo/render", `{"values":{"domain":"app.example.com","password":"x"}}`},
	} {
		recorder := doRequest(t, router, tc.method, tc.path, tc.body)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503: %s", tc.method, tc.path, recorder.Code, recorder.Body)
		}
		if !strings.Contains(recorder.Body.String(), "services are disabled") {
			t.Errorf("%s %s body = %s", tc.method, tc.path, recorder.Body)
		}
	}
}

// TestMountDisabled proves a nil service and FEATURE_SERVICES=false both mount
// nothing.
func TestMountDisabled(t *testing.T) {
	for name, build := range map[string]func(t *testing.T) Service{
		"nil service": func(t *testing.T) Service { return nil },
		"flag off": func(t *testing.T) Service {
			t.Setenv(services.FeatureEnv, "false")
			return mustCatalog(t, sampleFiles())
		},
	} {
		t.Run(name, func(t *testing.T) {
			router := routeTestServer(t, build(t))
			recorder := doRequest(t, router, http.MethodGet, "/v1/templates", "")
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (nothing mounted)", recorder.Code)
			}
		})
	}
}
