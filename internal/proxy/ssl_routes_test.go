package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeDNSProviderService is a canned DNSProviderService.
type fakeDNSProviderService struct {
	provider DNSProvider
	err      error

	createCalls int
	updateCalls int
	deleteCalls int
	lastInput   CreateDNSProviderInput
	lastUpdate  UpdateDNSProviderInput
}

func (f *fakeDNSProviderService) CreateProvider(_ context.Context, in CreateDNSProviderInput) (DNSProvider, error) {
	f.createCalls++
	f.lastInput = in
	if f.err != nil {
		return DNSProvider{}, f.err
	}
	return f.provider, nil
}

func (f *fakeDNSProviderService) ListProviders(context.Context) ([]DNSProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []DNSProvider{f.provider}, nil
}

func (f *fakeDNSProviderService) GetProvider(context.Context, uuid.UUID) (DNSProvider, error) {
	if f.err != nil {
		return DNSProvider{}, f.err
	}
	return f.provider, nil
}

func (f *fakeDNSProviderService) UpdateProvider(_ context.Context, _ uuid.UUID, in UpdateDNSProviderInput) (DNSProvider, error) {
	f.updateCalls++
	f.lastUpdate = in
	if f.err != nil {
		return DNSProvider{}, f.err
	}
	return f.provider, nil
}

func (f *fakeDNSProviderService) DeleteProvider(context.Context, uuid.UUID) error {
	f.deleteCalls++
	return f.err
}

// fakeCertificateService is a canned CertificateService.
type fakeCertificateService struct {
	certificate DomainCertificate
	err         error

	createCalls int
	updateCalls int
	deleteCalls int
	lastInput   CreateCertificateInput
	lastUpdate  UpdateCertificateInput
}

func (f *fakeCertificateService) CreateCertificate(_ context.Context, in CreateCertificateInput) (DomainCertificate, error) {
	f.createCalls++
	f.lastInput = in
	if f.err != nil {
		return DomainCertificate{}, f.err
	}
	return f.certificate, nil
}

func (f *fakeCertificateService) ListCertificates(context.Context) ([]DomainCertificate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []DomainCertificate{f.certificate}, nil
}

func (f *fakeCertificateService) GetCertificate(context.Context, uuid.UUID) (DomainCertificate, error) {
	if f.err != nil {
		return DomainCertificate{}, f.err
	}
	return f.certificate, nil
}

func (f *fakeCertificateService) UpdateCertificate(_ context.Context, _ uuid.UUID, in UpdateCertificateInput) (DomainCertificate, error) {
	f.updateCalls++
	f.lastUpdate = in
	if f.err != nil {
		return DomainCertificate{}, f.err
	}
	return f.certificate, nil
}

func (f *fakeCertificateService) DeleteCertificate(context.Context, uuid.UUID) error {
	f.deleteCalls++
	return f.err
}

// sampleProvider is the provider the wire tests render.
func sampleProvider() DNSProvider {
	return DNSProvider{
		ID:               uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		Provider:         ProviderCloudflare,
		Name:             "prod",
		Zones:            []string{"example.com"},
		Enabled:          true,
		SealedCredential: "sealed-must-not-leak",
		CreatedAt:        time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}
}

// sampleCertificate is the certificate config the wire tests render.
func sampleCertificate() DomainCertificate {
	return DomainCertificate{
		ID:            uuid.MustParse("cccccccc-0000-0000-0000-000000000003"),
		ApplicationID: uuid.MustParse("dddddddd-0000-0000-0000-000000000004"),
		Domain:        "app.example.com",
		Enabled:       true,
		Challenge:     ChallengeDNS01,
		DNSProviderID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		Wildcard:      true,
	}
}

// newSSLRoutes mounts the sync endpoint (without a service) plus the SSL CRUD
// endpoints behind a no-op auth middleware.
func newSSLRoutes(dns DNSProviderService, certs CertificateService) http.Handler {
	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, &fakeProxyService{}, dns, certs)
	return r
}

// doJSON performs one JSON request against the handler.
func doJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestDNSProviderCRUDRoutes(t *testing.T) {
	service := &fakeDNSProviderService{provider: sampleProvider()}
	handler := newSSLRoutes(service, nil)

	created := doJSON(t, handler, http.MethodPost, "/v1/proxy/dns-providers",
		`{"provider":"cloudflare","name":"prod","zones":["example.com"],"credential":"secret-token"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	if service.createCalls != 1 || service.lastInput.Credential != "secret-token" {
		t.Fatalf("create input = %#v", service.lastInput)
	}
	if strings.Contains(created.Body.String(), "secret-token") || strings.Contains(created.Body.String(), "sealed-must-not-leak") {
		t.Fatalf("create response leaks credential material: %s", created.Body.String())
	}
	var envelope dnsProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if envelope.Provider.ID != sampleProvider().ID.String() || !envelope.Provider.CredentialsSet || !envelope.Provider.Enabled {
		t.Fatalf("create response = %#v", envelope.Provider)
	}

	listed := doJSON(t, handler, http.MethodGet, "/v1/proxy/dns-providers", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"providers"`) {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	fetched := doJSON(t, handler, http.MethodGet, "/v1/proxy/dns-providers/"+sampleProvider().ID.String(), "")
	if fetched.Code != http.StatusOK {
		t.Fatalf("get status = %d", fetched.Code)
	}
	patched := doJSON(t, handler, http.MethodPatch, "/v1/proxy/dns-providers/"+sampleProvider().ID.String(),
		`{"credential":"rotated"}`)
	if patched.Code != http.StatusOK || service.updateCalls != 1 || service.lastUpdate.Credential == nil || *service.lastUpdate.Credential != "rotated" {
		t.Fatalf("patch status = %d update = %#v", patched.Code, service.lastUpdate)
	}
	deleted := doJSON(t, handler, http.MethodDelete, "/v1/proxy/dns-providers/"+sampleProvider().ID.String(), "")
	if deleted.Code != http.StatusNoContent || service.deleteCalls != 1 {
		t.Fatalf("delete status = %d calls = %d", deleted.Code, service.deleteCalls)
	}
}

func TestSSLRouteMappingsAndValidation(t *testing.T) {
	providerPath := "/v1/proxy/dns-providers/" + sampleProvider().ID.String()
	oversized := `{"enabled":false}` + strings.Repeat(" ", maxSSLBodyBytes)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "create provider unknown field", method: http.MethodPost, path: "/v1/proxy/dns-providers", body: `{"provider":"cloudflare","zones":["example.com"],"credential":"t","sealed":"x"}`, status: http.StatusBadRequest},
		{name: "create provider empty body", method: http.MethodPost, path: "/v1/proxy/dns-providers", body: "", status: http.StatusBadRequest},
		{name: "bad provider id", method: http.MethodGet, path: "/v1/proxy/dns-providers/not-a-uuid", body: "", status: http.StatusBadRequest},
		{name: "certificate missing application id", method: http.MethodPost, path: "/v1/proxy/certificates", body: `{"challenge":"http-01"}`, status: http.StatusBadRequest},
		{name: "certificate bad application id", method: http.MethodPost, path: "/v1/proxy/certificates", body: `{"application_id":"nope"}`, status: http.StatusBadRequest},
		{name: "certificate bad provider id", method: http.MethodPost, path: "/v1/proxy/certificates", body: `{"application_id":"dddddddd-0000-0000-0000-000000000004","challenge":"dns-01","dns_provider_id":"nope"}`, status: http.StatusBadRequest},
		{name: "bad certificate id", method: http.MethodPatch, path: "/v1/proxy/certificates/not-a-uuid", body: `{"enabled":false}`, status: http.StatusBadRequest},
		{name: "whitespace only body", method: http.MethodPatch, path: providerPath, body: "   \n\t ", status: http.StatusBadRequest},
		{name: "json null body", method: http.MethodPatch, path: providerPath, body: `null`, status: http.StatusBadRequest},
		{name: "trailing second value", method: http.MethodPatch, path: providerPath, body: `{"enabled":false}{"enabled":true}`, status: http.StatusBadRequest},
		{name: "trailing garbage", method: http.MethodPatch, path: providerPath, body: `{"enabled":false} oops`, status: http.StatusBadRequest},
		{name: "body over the limit", method: http.MethodPatch, path: providerPath, body: oversized, status: http.StatusBadRequest},
		{name: "valid body with surrounding whitespace", method: http.MethodPatch, path: providerPath, body: "\n {\"enabled\":false} \n", status: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newSSLRoutes(&fakeDNSProviderService{provider: sampleProvider()}, &fakeCertificateService{certificate: sampleCertificate()})
			recorder := doJSON(t, handler, tc.method, tc.path, tc.body)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.status, recorder.Body.String())
			}
		})
	}

	// The service-level validation error surfaces verbatim with 400.
	handler := newSSLRoutes(&fakeDNSProviderService{err: ErrValidation}, &fakeCertificateService{certificate: sampleCertificate()})
	recorder := doJSON(t, handler, http.MethodPost, "/v1/proxy/dns-providers",
		`{"provider":"cloudflare","zones":["example.com"],"credential":"t"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d", recorder.Code)
	}
}

func TestCertificateCRUDRoutes(t *testing.T) {
	service := &fakeCertificateService{certificate: sampleCertificate()}
	handler := newSSLRoutes(nil, service)

	created := doJSON(t, handler, http.MethodPost, "/v1/proxy/certificates",
		`{"application_id":"dddddddd-0000-0000-0000-000000000004","challenge":"dns-01","dns_provider_id":"aaaaaaaa-0000-0000-0000-000000000001","wildcard":true}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	if service.lastInput.DNSProviderID != sampleProvider().ID || !service.lastInput.Wildcard {
		t.Fatalf("create input = %#v", service.lastInput)
	}
	var envelope certificateEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Certificate.Domain != "app.example.com" || envelope.Certificate.DNSProviderID == "" {
		t.Fatalf("certificate response = %#v", envelope.Certificate)
	}

	listed := doJSON(t, handler, http.MethodGet, "/v1/proxy/certificates", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"certificates"`) {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	patched := doJSON(t, handler, http.MethodPatch, "/v1/proxy/certificates/"+sampleCertificate().ID.String(),
		`{"challenge":"http-01","enabled":false}`)
	if patched.Code != http.StatusOK || service.updateCalls != 1 {
		t.Fatalf("patch status = %d calls = %d", patched.Code, service.updateCalls)
	}
	if service.lastUpdate.Challenge == nil || *service.lastUpdate.Challenge != ChallengeHTTP01 {
		t.Fatalf("patch challenge = %#v", service.lastUpdate.Challenge)
	}
	if service.lastUpdate.Enabled == nil || *service.lastUpdate.Enabled {
		t.Fatalf("patch enabled = %#v", service.lastUpdate.Enabled)
	}
	deleted := doJSON(t, handler, http.MethodDelete, "/v1/proxy/certificates/"+sampleCertificate().ID.String(), "")
	if deleted.Code != http.StatusNoContent || service.deleteCalls != 1 {
		t.Fatalf("delete status = %d calls = %d", deleted.Code, service.deleteCalls)
	}
}

func TestSSLErrorMapping(t *testing.T) {
	serverProviderID := sampleProvider().ID.String()
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "not found", err: ErrNotFound, status: http.StatusNotFound},
		{name: "conflict", err: ErrConflict, status: http.StatusConflict},
		{name: "missing deployment secret", err: fmt.Errorf("%w: refusing to store DNS provider credentials", ErrSecret), status: http.StatusServiceUnavailable},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newSSLRoutes(&fakeDNSProviderService{err: tc.err}, &fakeCertificateService{err: tc.err})
			provider := doJSON(t, handler, http.MethodGet, "/v1/proxy/dns-providers/"+serverProviderID, "")
			if provider.Code != tc.status {
				t.Fatalf("get status = %d, want %d", provider.Code, tc.status)
			}
			certificate := doJSON(t, handler, http.MethodGet, "/v1/proxy/certificates/"+sampleCertificate().ID.String(), "")
			if certificate.Code != tc.status {
				t.Fatalf("certificate status = %d, want %d", certificate.Code, tc.status)
			}
			if tc.status == http.StatusInternalServerError {
				if strings.Contains(provider.Body.String(), "boom") {
					t.Fatalf("internal error detail leaked: %s", provider.Body.String())
				}
			}
		})
	}
}

func TestMountSkipsSSLRoutesWithoutServices(t *testing.T) {
	handler := newSSLRoutes(nil, nil)
	for _, path := range []string{"/v1/proxy/dns-providers", "/v1/proxy/certificates"} {
		recorder := doJSON(t, handler, http.MethodGet, path, "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 when the SSL services are unwired", path, recorder.Code)
		}
	}
}

// TestSSLRoutesShareTheSyncAuthBoundary proves every SSL endpoint is mounted
// inside the same authenticated group as the sync route: unauthenticated
// requests never reach the services.
func TestSSLRoutesShareTheSyncAuthBoundary(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get("X-Test-Auth") != "ok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, req)
		})
	}, &fakeProxyService{}, &fakeDNSProviderService{provider: sampleProvider()}, &fakeCertificateService{certificate: sampleCertificate()})

	id := sampleProvider().ID.String()
	certificateID := sampleCertificate().ID.String()
	requests := []struct{ method, path string }{
		{http.MethodPost, "/v1/proxy/dns-providers"},
		{http.MethodGet, "/v1/proxy/dns-providers"},
		{http.MethodGet, "/v1/proxy/dns-providers/" + id},
		{http.MethodPatch, "/v1/proxy/dns-providers/" + id},
		{http.MethodDelete, "/v1/proxy/dns-providers/" + id},
		{http.MethodPost, "/v1/proxy/certificates"},
		{http.MethodGet, "/v1/proxy/certificates"},
		{http.MethodGet, "/v1/proxy/certificates/" + certificateID},
		{http.MethodPatch, "/v1/proxy/certificates/" + certificateID},
		{http.MethodDelete, "/v1/proxy/certificates/" + certificateID},
	}
	for _, tc := range requests {
		recorder := doJSON(t, r, tc.method, tc.path, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401 without auth", tc.method, tc.path, recorder.Code)
		}
	}
}
