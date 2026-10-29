package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeRouteService struct {
	service   Service
	deploy    Deploy
	deploys   []Deploy
	items     []ComposeContainer
	domains   []DomainView
	chunks    [][]byte
	streamErr error
	created   CreateRequest
	domainIn  DomainInput

	deployCalls int
	err         error
}

func (f *fakeRouteService) Create(_ context.Context, _ uuid.UUID, req CreateRequest) (Service, error) {
	if f.err != nil {
		return Service{}, f.err
	}
	f.created = req
	return f.service, nil
}

func (f *fakeRouteService) List(context.Context, uuid.UUID, ServiceFilter) ([]Service, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []Service{f.service}, nil
}

func (f *fakeRouteService) Get(context.Context, uuid.UUID, uuid.UUID) (Service, error) {
	if f.err != nil {
		return Service{}, f.err
	}
	return f.service, nil
}

func (f *fakeRouteService) Update(context.Context, uuid.UUID, uuid.UUID, UpdateRequest) (Service, error) {
	if f.err != nil {
		return Service{}, f.err
	}
	return f.service, nil
}

func (f *fakeRouteService) Delete(context.Context, uuid.UUID, uuid.UUID) error { return f.err }

func (f *fakeRouteService) Deploy(context.Context, uuid.UUID, uuid.UUID) (Service, Deploy, error) {
	f.deployCalls++
	if f.err != nil {
		return Service{}, Deploy{}, f.err
	}
	return f.service, f.deploy, nil
}

func (f *fakeRouteService) Stop(context.Context, uuid.UUID, uuid.UUID) (Service, error) {
	if f.err != nil {
		return Service{}, f.err
	}
	return f.service, nil
}

func (f *fakeRouteService) Restart(context.Context, uuid.UUID, uuid.UUID) (Service, error) {
	if f.err != nil {
		return Service{}, f.err
	}
	return f.service, nil
}

func (f *fakeRouteService) Containers(context.Context, uuid.UUID, uuid.UUID) ([]ComposeContainer, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func (f *fakeRouteService) Logs(context.Context, uuid.UUID, uuid.UUID, string, int64, bool) (LogStream, error) {
	if f.err != nil {
		return nil, f.err
	}
	chunks := make(chan []byte, len(f.chunks))
	for _, chunk := range f.chunks {
		chunks <- chunk
	}
	close(chunks)
	return &fakeLogStream{chunks: chunks, streamErr: f.streamErr}, nil
}

func (f *fakeRouteService) Deploys(context.Context, uuid.UUID, uuid.UUID) ([]Deploy, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.deploys, nil
}

func (f *fakeRouteService) ListDomains(context.Context, uuid.UUID, uuid.UUID) ([]DomainView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.domains, nil
}

func (f *fakeRouteService) AddDomain(_ context.Context, _, serviceID uuid.UUID, in DomainInput) (ServiceDomain, error) {
	if f.err != nil {
		return ServiceDomain{}, f.err
	}
	f.domainIn = in
	return ServiceDomain{
		ID:             uuid.New(),
		ServiceID:      serviceID,
		ComposeService: in.ComposeService,
		Port:           in.Port,
		Domain:         in.Domain,
		Protocol:       in.Protocol,
	}, nil
}

func (f *fakeRouteService) UpdateDomain(_ context.Context, _, serviceID, domainID uuid.UUID, in DomainInput) (ServiceDomain, error) {
	if f.err != nil {
		return ServiceDomain{}, f.err
	}
	f.domainIn = in
	return ServiceDomain{
		ID:             domainID,
		ServiceID:      serviceID,
		ComposeService: in.ComposeService,
		Port:           in.Port,
		Domain:         in.Domain,
		Protocol:       in.Protocol,
	}, nil
}

func (f *fakeRouteService) RemoveDomain(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return f.err
}

var _ ServiceService = (*fakeRouteService)(nil)

func TestRoutesServiceDomains(t *testing.T) {
	service := sampleService()
	fake := &fakeRouteService{
		service: service,
		domains: []DomainView{{
			ServiceDomain: ServiceDomain{
				ID: uuid.New(), ServiceID: service.ID, Domain: "app.example.com",
				ComposeService: "web", Port: 80, Protocol: "https",
			},
			Source: SourceManaged, Status: DomainStatusRouted,
		}},
	}
	router := routeTestServer(t, fake, service.UserID)
	base := "/v1/services/" + service.ID.String() + "/domains"

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, base, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET domains = %d: %s", recorder.Code, recorder.Body)
	}
	var list domainListEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Domains) != 1 || list.Domains[0].Source != SourceManaged || list.Domains[0].Status != DomainStatusRouted {
		t.Fatalf("list = %+v", list.Domains)
	}

	body := `{"domain":"new.example.com","compose_service":"web","port":8080,"protocol":"https"}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, base, strings.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST domains = %d: %s", recorder.Code, recorder.Body)
	}
	if fake.domainIn.ComposeService != "web" || fake.domainIn.Port != 8080 {
		t.Fatalf("domain input = %+v", fake.domainIn)
	}

	domainID := uuid.New()
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, base+"/"+domainID.String(), strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT domain = %d: %s", recorder.Code, recorder.Body)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, base+"/"+domainID.String(), nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DELETE domain = %d: %s", recorder.Code, recorder.Body)
	}
}

func TestRoutesServiceDomainErrors(t *testing.T) {
	service := sampleService()
	base := "/v1/services/" + service.ID.String() + "/domains"
	body := `{"domain":"app.example.com","compose_service":"web","port":80}`

	for _, tc := range []struct {
		err  error
		code int
	}{
		{ErrDomainConflict, http.StatusConflict},
		{ErrValidation, http.StatusBadRequest},
		{ErrNotFound, http.StatusNotFound},
	} {
		fake := &fakeRouteService{service: service, err: tc.err}
		router := routeTestServer(t, fake, service.UserID)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, base, strings.NewReader(body)))
		if recorder.Code != tc.code {
			t.Errorf("%v = %d, want %d: %s", tc.err, recorder.Code, tc.code, recorder.Body)
		}
	}
}

func routeTestServer(t *testing.T, svc ServiceService, userID uuid.UUID) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	userIDFunc := func(context.Context) (uuid.UUID, bool) {
		if userID == uuid.Nil {
			return uuid.Nil, false
		}
		return userID, true
	}
	Mount(router, auth, userIDFunc, svc)
	return router
}

func sampleService() Service {
	return Service{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		ServerID:      uuid.New(),
		EnvironmentID: uuid.New(),
		Name:          "wordpress",
		Status:        StatusRunning,
		ComposeYAML:   testDocument,
		Env:           testEnv,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

func TestRoutesCreatePassesTemplate(t *testing.T) {
	service := sampleService()
	service.TemplateSlug = "wordpress"
	fake := &fakeRouteService{service: service}
	router := routeTestServer(t, fake, service.UserID)

	body := `{"name":"wordpress","environment_id":"` + service.EnvironmentID.String() + `","server_id":"` + service.ServerID.String() + `","compose_yaml":"services:\n  web:\n    image: nginx\n","template":"wordpress"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services", strings.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /v1/services = %d: %s", recorder.Code, recorder.Body)
	}
	if fake.created.TemplateSlug != "wordpress" {
		t.Errorf("template passed to the service = %q, want wordpress", fake.created.TemplateSlug)
	}
	var created serviceEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Service.Template != "wordpress" {
		t.Errorf("template in the response = %q, want wordpress", created.Service.Template)
	}
}

func TestRoutesCreateAndRead(t *testing.T) {
	service := sampleService()
	fake := &fakeRouteService{service: service}
	router := routeTestServer(t, fake, service.UserID)

	createBody := `{"name":"wordpress","environment_id":"` + service.EnvironmentID.String() + `","server_id":"` + service.ServerID.String() + `","compose_yaml":"services:\n  web:\n    image: nginx\n","env":{"A":"b"}}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services", strings.NewReader(createBody)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /v1/services = %d: %s", recorder.Code, recorder.Body)
	}
	var created serviceEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Service.ComposeProject != ProjectName(service.ID) || created.Service.ComposeYAML == "" {
		t.Errorf("created = %+v", created.Service)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/services = %d", recorder.Code)
	}
	var list serviceListEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Services) != 1 {
		t.Fatalf("list = %+v", list.Services)
	}
	if list.Services[0].ComposeYAML != "" {
		t.Errorf("list responses must omit the document")
	}
	if len(list.Services[0].Domains) != 1 || list.Services[0].Domains[0].Domain != "app.example.com" {
		t.Errorf("domains = %+v", list.Services[0].Domains)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services/"+service.ID.String(), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/services/{id} = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services/not-a-uuid", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("GET bad id = %d, want 400", recorder.Code)
	}
}

func TestRoutesCreateWithDeployStartsTheDeploy(t *testing.T) {
	service := sampleService()
	fake := &fakeRouteService{service: service}
	router := routeTestServer(t, fake, service.UserID)
	body := `{"name":"wordpress","environment_id":"` + service.EnvironmentID.String() + `","server_id":"` + service.ServerID.String() + `","compose_yaml":"services:\n  web:\n    image: nginx\n","deploy":true}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services", strings.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /v1/services = %d: %s", recorder.Code, recorder.Body)
	}
	if fake.deployCalls != 1 {
		t.Fatalf("deploy calls = %d, want 1", fake.deployCalls)
	}

	fake = &fakeRouteService{service: service}
	router = routeTestServer(t, fake, service.UserID)
	body = `{"name":"wordpress","environment_id":"` + service.EnvironmentID.String() + `","server_id":"` + service.ServerID.String() + `","compose_yaml":"services:\n  web:\n    image: nginx\n"}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services", strings.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /v1/services = %d: %s", recorder.Code, recorder.Body)
	}
	if fake.deployCalls != 0 {
		t.Fatalf("deploy calls = %d, want none without deploy: true", fake.deployCalls)
	}
}

func TestRoutesCreateRejections(t *testing.T) {
	service := sampleService()
	newRouter := func() (http.Handler, *fakeRouteService) {
		fake := &fakeRouteService{service: service}
		return routeTestServer(t, fake, service.UserID), fake
	}
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{"missing server", `{"name":"wordpress","environment_id":"` + service.EnvironmentID.String() + `","compose_yaml":"services:\n  web:\n    image: nginx\n"}`, "server is required"},
		{"missing environment", `{"name":"wordpress","server_id":"` + service.ServerID.String() + `","compose_yaml":"services:\n  web:\n    image: nginx\n"}`, "environment is required"},
		{"invalid server", `{"name":"wordpress","environment_id":"` + service.EnvironmentID.String() + `","server_id":"nope","compose_yaml":"x"}`, "invalid server id"},
		{"invalid environment", `{"name":"wordpress","environment_id":"nope","server_id":"` + service.ServerID.String() + `","compose_yaml":"x"}`, "invalid environment id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, _ := newRouter()
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services", strings.NewReader(tc.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", recorder.Code, recorder.Body.String())
			}
			var decoded struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Message != tc.message {
				t.Errorf("message = %q, want %q", decoded.Message, tc.message)
			}
		})
	}
}

func TestRoutesLifecycleAndLists(t *testing.T) {
	service := sampleService()
	fake := &fakeRouteService{
		service: service,
		deploy: Deploy{
			ID: uuid.New(), ServiceID: service.ID, State: DeployRunning,
			CreatedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
		},
		deploys: []Deploy{{ID: uuid.New(), ServiceID: service.ID, State: DeployFailed, Error: "boom"}},
		items:   []ComposeContainer{{Service: "web", ContainerID: "abc", State: "running"}},
		chunks:  [][]byte{[]byte("line one\n"), []byte("line two\n")},
	}
	router := routeTestServer(t, fake, service.UserID)

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/v1/services/" + service.ID.String() + "/deploy", http.StatusAccepted},
		{http.MethodPost, "/v1/services/" + service.ID.String() + "/stop", http.StatusOK},
		{http.MethodPost, "/v1/services/" + service.ID.String() + "/restart", http.StatusOK},
		{http.MethodGet, "/v1/services/" + service.ID.String() + "/deploys", http.StatusOK},
		{http.MethodGet, "/v1/services/" + service.ID.String() + "/containers", http.StatusOK},
		{http.MethodDelete, "/v1/services/" + service.ID.String(), http.StatusNoContent},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.want {
			t.Errorf("%s %s = %d, want %d: %s", tc.method, tc.path, recorder.Code, tc.want, recorder.Body)
		}
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/v1/services/"+service.ID.String()+"/logs?service=web&tail=10&follow=false", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("logs = %d: %s", recorder.Code, recorder.Body)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "line two") {
		t.Errorf("logs body = %q", body)
	}
	if recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("logs content type = %q", recorder.Header().Get("Content-Type"))
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/v1/services/"+service.ID.String()+"/logs?tail=nope", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("bad tail = %d, want 400", recorder.Code)
	}
}

func TestRoutesErrorMapping(t *testing.T) {
	cases := map[string]int{
		"not found":  http.StatusNotFound,
		"validation": http.StatusBadRequest,
		"conflict":   http.StatusConflict,
		"disabled":   http.StatusServiceUnavailable,
		"agent":      http.StatusBadGateway,
		"deploy":     http.StatusBadGateway,
	}
	sentinels := map[string]error{
		"not found":  ErrNotFound,
		"validation": ErrValidation,
		"conflict":   ErrConflict,
		"disabled":   ErrDisabled,
		"agent":      ErrAgentUnavailable,
		"deploy":     ErrDeployFailed,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			service := sampleService()
			fake := &fakeRouteService{service: service, err: sentinels[name]}
			router := routeTestServer(t, fake, service.UserID)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services/"+service.ID.String()+"/deploy", nil))
			if recorder.Code != want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, want, recorder.Body)
			}
		})
	}
}

func TestRoutesLogsFirstReadFailure(t *testing.T) {
	service := sampleService()
	router := routeTestServer(t, &fakeRouteService{service: service, err: ErrAgentUnavailable}, service.UserID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services/"+service.ID.String()+"/logs", nil))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", recorder.Code, recorder.Body)
	}
}

func TestRoutesLogsTerminalFailureKeepsBody(t *testing.T) {
	service := sampleService()
	fake := &fakeRouteService{
		service:   service,
		chunks:    [][]byte{[]byte("partial\n")},
		streamErr: errors.New("stream died"),
	}
	router := routeTestServer(t, fake, service.UserID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services/"+service.ID.String()+"/logs", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "partial\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestRoutesLogsTerminalErrorIsRedactedInLogs(t *testing.T) {
	secret := testEnv["PASSWORD"]
	repo := newFakeRepository()
	agent := &fakeAgent{
		chunks:    [][]byte{[]byte("partial\n")},
		streamErr: fmt.Errorf("%w: logs: invalid value %s", ErrDeployFailed, secret),
	}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	var captured bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	router := routeTestServer(t, svc, userID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services/"+created.ID.String()+"/logs", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body)
	}
	if body := recorder.Body.String(); body != "partial\n" {
		t.Fatalf("body = %q, want the application log content untouched", body)
	}
	if strings.Contains(captured.String(), secret) {
		t.Fatalf("the handler log leaked the environment value: %s", captured.String())
	}
	if !strings.Contains(captured.String(), "<redacted>") {
		t.Fatalf("the handler log does not show the redaction: %s", captured.String())
	}
}

func TestRoutesRequireAuth(t *testing.T) {
	service := sampleService()
	router := routeTestServer(t, &fakeRouteService{service: service}, uuid.Nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMountDisabled(t *testing.T) {
	service := sampleService()
	for name, build := range map[string]func() ServiceService{
		"nil service": func() ServiceService { return nil },
		"flag off": func() ServiceService {
			t.Setenv(FeatureEnv, "false")
			return &fakeRouteService{service: service}
		},
	} {
		t.Run(name, func(t *testing.T) {
			router := routeTestServer(t, build(), service.UserID)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/services", nil))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (nothing mounted)", recorder.Code)
			}
		})
	}
}

func TestDecodeBodyRejectsUnknownFields(t *testing.T) {
	service := sampleService()
	router := routeTestServer(t, &fakeRouteService{service: service}, service.UserID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/services",
		strings.NewReader(`{"name":"x","server_id":"`+service.ServerID.String()+`","compose_yaml":"y","typo":1}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
