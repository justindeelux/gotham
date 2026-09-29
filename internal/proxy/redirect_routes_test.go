package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeRedirectService is a canned RedirectService.
type fakeRedirectService struct {
	redirect DomainRedirect
	err      error

	createCalls           int
	updateCalls           int
	deleteCalls           int
	lastCreate            CreateRedirectInput
	lastUpdate            UpdateRedirectInput
	lastListApplicationID uuid.UUID
}

func (f *fakeRedirectService) CreateRedirect(_ context.Context, in CreateRedirectInput) (DomainRedirect, error) {
	f.createCalls++
	f.lastCreate = in
	if f.err != nil {
		return DomainRedirect{}, f.err
	}
	return f.redirect, nil
}

func (f *fakeRedirectService) ListRedirects(_ context.Context, applicationID uuid.UUID) ([]DomainRedirect, error) {
	f.lastListApplicationID = applicationID
	if f.err != nil {
		return nil, f.err
	}
	return []DomainRedirect{f.redirect}, nil
}

func (f *fakeRedirectService) GetRedirect(context.Context, uuid.UUID) (DomainRedirect, error) {
	if f.err != nil {
		return DomainRedirect{}, f.err
	}
	return f.redirect, nil
}

func (f *fakeRedirectService) UpdateRedirect(_ context.Context, _ uuid.UUID, in UpdateRedirectInput) (DomainRedirect, error) {
	f.updateCalls++
	f.lastUpdate = in
	if f.err != nil {
		return DomainRedirect{}, f.err
	}
	return f.redirect, nil
}

func (f *fakeRedirectService) DeleteRedirect(context.Context, uuid.UUID) error {
	f.deleteCalls++
	return f.err
}

// fakeCertificateStatusService is a canned CertificateStatusService.
type fakeCertificateStatusService struct {
	observations map[uuid.UUID]CertificateStatusObservation
	calls        int
	lastInput    []DomainCertificate
}

func (f *fakeCertificateStatusService) CertificateStatuses(_ context.Context, certificates []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	f.calls++
	f.lastInput = append([]DomainCertificate{}, certificates...)
	if f.observations != nil {
		return f.observations
	}
	out := make(map[uuid.UUID]CertificateStatusObservation, len(certificates))
	for _, certificate := range certificates {
		out[certificate.ID] = CertificateStatusObservation{Status: CertificateStatusUnknown}
	}
	return out
}

// sampleRedirect is the rule the wire tests render.
func sampleRedirect() DomainRedirect {
	return DomainRedirect{
		ID:            uuid.MustParse("eeeeeeee-0000-0000-0000-000000000005"),
		ApplicationID: uuid.MustParse("dddddddd-0000-0000-0000-000000000004"),
		SourceDomain:  "old.example.com",
		TargetDomain:  "new.example.com",
		Code:          RedirectCodePermanent,
		PreservePath:  true,
		Enabled:       true,
		CreatedAt:     time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}
}

// newRedirectRoutes mounts the sync endpoint plus the redirect endpoints
// behind a no-op auth middleware.
func newRedirectRoutes(svc RedirectService) http.Handler {
	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, passthroughAuth, &fakeProxyService{}, nil, nil, svc, nil)
	return r
}

func TestRedirectRoutesCRUD(t *testing.T) {
	svc := &fakeRedirectService{redirect: sampleRedirect()}
	r := newRedirectRoutes(svc)

	// Create.
	body := `{"application_id":"dddddddd-0000-0000-0000-000000000004","source_domain":"old.example.com","target_domain":"new.example.com","code":302,"preserve_path":false,"enabled":false}`
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/proxy/redirects", strings.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%s)", recorder.Code, recorder.Body.String())
	}
	if svc.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", svc.createCalls)
	}
	if svc.lastCreate.ApplicationID.String() != "dddddddd-0000-0000-0000-000000000004" ||
		svc.lastCreate.Code != RedirectCodeTemporary || *svc.lastCreate.PreservePath || *svc.lastCreate.Enabled {
		t.Fatalf("create input = %#v", svc.lastCreate)
	}
	var created redirectEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Redirect.ID != sampleRedirect().ID.String() || created.Redirect.SourceDomain != "old.example.com" {
		t.Fatalf("create response = %#v", created.Redirect)
	}

	// List, unfiltered and filtered.
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/redirects", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", recorder.Code)
	}
	var listed redirectListEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Redirects) != 1 || listed.Redirects[0].Code != RedirectCodePermanent {
		t.Fatalf("list = %#v", listed.Redirects)
	}
	if svc.lastListApplicationID != uuid.Nil {
		t.Fatalf("unfiltered list application = %s, want nil", svc.lastListApplicationID)
	}

	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/redirects?application_id=dddddddd-0000-0000-0000-000000000004", nil))
	if recorder.Code != http.StatusOK || svc.lastListApplicationID.String() != "dddddddd-0000-0000-0000-000000000004" {
		t.Fatalf("filtered list status = %d, application = %s", recorder.Code, svc.lastListApplicationID)
	}

	// Get.
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/redirects/"+sampleRedirect().ID.String(), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", recorder.Code)
	}

	// Patch.
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/v1/proxy/redirects/"+sampleRedirect().ID.String(),
		strings.NewReader(`{"enabled":false,"target_domain":"elsewhere.example.com"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200 (%s)", recorder.Code, recorder.Body.String())
	}
	if svc.lastUpdate.Enabled == nil || *svc.lastUpdate.Enabled || svc.lastUpdate.TargetDomain == nil || *svc.lastUpdate.TargetDomain != "elsewhere.example.com" {
		t.Fatalf("patch input = %#v", svc.lastUpdate)
	}
	if svc.lastUpdate.SourceDomain != nil || svc.lastUpdate.Code != nil {
		t.Fatalf("patch must stay partial: %#v", svc.lastUpdate)
	}

	// Delete.
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/v1/proxy/redirects/"+sampleRedirect().ID.String(), nil))
	if recorder.Code != http.StatusNoContent || svc.deleteCalls != 1 {
		t.Fatalf("delete status = %d calls = %d, want 204/1", recorder.Code, svc.deleteCalls)
	}
}

func TestRedirectRoutesRejectBadInput(t *testing.T) {
	svc := &fakeRedirectService{redirect: sampleRedirect()}
	r := newRedirectRoutes(svc)

	cases := []struct{ name, method, path, body string }{
		{"invalid application id", http.MethodPost, "/v1/proxy/redirects", `{"application_id":"nope","source_domain":"a.example.com","target_domain":"b.example.com"}`},
		{"missing body", http.MethodPost, "/v1/proxy/redirects", ``},
		{"unknown field", http.MethodPost, "/v1/proxy/redirects", `{"application_id":"dddddddd-0000-0000-0000-000000000004","source_domain":"a.example.com","target_domain":"b.example.com","surprise":1}`},
		{"invalid filter", http.MethodGet, "/v1/proxy/redirects?application_id=nope", ``},
		{"invalid path id", http.MethodGet, "/v1/proxy/redirects/nope", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tc.body)
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body.String())
			}
		})
	}
	if svc.createCalls != 0 {
		t.Fatalf("create calls = %d, want none", svc.createCalls)
	}
}

func TestRedirectRoutesMapServiceErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{fmt.Errorf("%w: bad", ErrValidation), http.StatusBadRequest},
		{fmt.Errorf("%w: missing", ErrNotFound), http.StatusNotFound},
		{fmt.Errorf("%w: taken", ErrConflict), http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			r := newRedirectRoutes(&fakeRedirectService{err: tc.err})
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/redirects", nil))
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
		})
	}
}

func TestRedirectRoutesShareTheSyncAuthBoundary(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get("X-Test-Auth") != "ok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, req)
		})
	}, passthroughAuth, &fakeProxyService{}, nil, nil, &fakeRedirectService{redirect: sampleRedirect()}, nil)
	path := "/v1/proxy/redirects/" + sampleRedirect().ID.String()
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/v1/proxy/redirects"},
		{http.MethodGet, "/v1/proxy/redirects"},
		{http.MethodGet, path},
		{http.MethodPatch, path},
		{http.MethodDelete, path},
	} {
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest(request.method, request.path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", request.method, request.path, recorder.Code)
		}
	}
}

// TestCertificateResponseCarriesObservedStatus proves the certificate API
// surfaces the computed status: present carries not_after, unknown does not,
// and a nil status service leaves the response byte-compatible with BE-6.2
// consumers.
func TestCertificateResponseCarriesObservedStatus(t *testing.T) {
	certificate := sampleCertificate()
	expiry := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	statuses := &fakeCertificateStatusService{observations: map[uuid.UUID]CertificateStatusObservation{
		certificate.ID: {Status: CertificateStatusPresent, NotAfter: expiry},
	}}

	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, passthroughAuth, &fakeProxyService{}, nil,
		&fakeCertificateService{certificate: certificate}, nil, statuses)

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/certificates", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", recorder.Code)
	}
	if statuses.calls != 1 || len(statuses.lastInput) != 1 {
		t.Fatalf("status calls = %d input = %d, want one read for the listed certificates", statuses.calls, len(statuses.lastInput))
	}
	var listed certificateListEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if listed.Certificates[0].Status != string(CertificateStatusPresent) {
		t.Fatalf("status = %q, want present", listed.Certificates[0].Status)
	}
	if listed.Certificates[0].NotAfter == nil || !listed.Certificates[0].NotAfter.Equal(expiry) {
		t.Fatalf("not_after = %v, want %s", listed.Certificates[0].NotAfter, expiry)
	}

	// Unknown never fabricates an expiry and the list still answers 200.
	unknown := &fakeCertificateStatusService{}
	r = chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, passthroughAuth, &fakeProxyService{}, nil,
		&fakeCertificateService{certificate: certificate}, nil, unknown)
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/certificates", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unknown-node list status = %d, want 200", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "not_after") {
		t.Fatalf("unknown status fabricated an expiry: %s", recorder.Body.String())
	}
	var unknownBody certificateListEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &unknownBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if unknownBody.Certificates[0].Status != string(CertificateStatusUnknown) {
		t.Fatalf("status = %q, want unknown", unknownBody.Certificates[0].Status)
	}

	// Without a status service the response carries no status fields at all.
	r = chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, passthroughAuth, &fakeProxyService{}, nil,
		&fakeCertificateService{certificate: certificate}, nil, nil)
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/proxy/certificates", nil))
	if strings.Contains(recorder.Body.String(), `"status"`) || strings.Contains(recorder.Body.String(), "not_after") {
		t.Fatalf("nil status service leaked status fields: %s", recorder.Body.String())
	}
}
