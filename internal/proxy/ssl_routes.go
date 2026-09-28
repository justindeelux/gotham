package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxSSLBodyBytes bounds SSL request bodies (they are tiny; a DNS token is at
// most a few KB).
const maxSSLBodyBytes = 1 << 20 // 1 MiB

// dnsProviderResponse is the wire representation of a DNS provider. The
// sealed credential never appears here: only whether one is set.
type dnsProviderResponse struct {
	ID             string    `json:"id"`
	Provider       string    `json:"provider"`
	Name           string    `json:"name,omitempty"`
	Zones          []string  `json:"zones"`
	Enabled        bool      `json:"enabled"`
	CredentialsSet bool      `json:"credentials_set"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// dnsProviderEnvelope wraps a single provider.
type dnsProviderEnvelope struct {
	Provider dnsProviderResponse `json:"provider"`
}

// dnsProviderListEnvelope wraps a provider list.
type dnsProviderListEnvelope struct {
	Providers []dnsProviderResponse `json:"providers"`
}

// createDNSProviderRequest is the body of POST /v1/proxy/dns-providers.
// Credential is write-only.
type createDNSProviderRequest struct {
	Provider   string   `json:"provider"`
	Name       string   `json:"name,omitempty"`
	Zones      []string `json:"zones"`
	Credential string   `json:"credential"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

// updateDNSProviderRequest is the body of PATCH /v1/proxy/dns-providers/{id}.
type updateDNSProviderRequest struct {
	Provider   *string   `json:"provider,omitempty"`
	Name       *string   `json:"name,omitempty"`
	Zones      *[]string `json:"zones,omitempty"`
	Credential *string   `json:"credential,omitempty"`
	Enabled    *bool     `json:"enabled,omitempty"`
}

// certificateResponse is the wire representation of a certificate config.
// Status and NotAfter are additive (BE-6.3): they are computed on read from
// the owning node's ACME storage and omitted when no status service is
// configured, so existing consumers are unaffected. NotAfter is only present
// when Status is "present".
type certificateResponse struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	Domain        string     `json:"domain"`
	Enabled       bool       `json:"enabled"`
	Challenge     string     `json:"challenge"`
	DNSProviderID string     `json:"dns_provider_id,omitempty"`
	Wildcard      bool       `json:"wildcard"`
	Status        string     `json:"status,omitempty"`
	NotAfter      *time.Time `json:"not_after,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// certificateEnvelope wraps a single certificate config.
type certificateEnvelope struct {
	Certificate certificateResponse `json:"certificate"`
}

// certificateListEnvelope wraps a certificate list.
type certificateListEnvelope struct {
	Certificates []certificateResponse `json:"certificates"`
}

// createCertificateRequest is the body of POST /v1/proxy/certificates. The
// recorded domain always comes from the application, never from the body.
type createCertificateRequest struct {
	ApplicationID string `json:"application_id"`
	Enabled       *bool  `json:"enabled,omitempty"`
	Challenge     string `json:"challenge,omitempty"`
	DNSProviderID string `json:"dns_provider_id,omitempty"`
	Wildcard      bool   `json:"wildcard,omitempty"`
}

// updateCertificateRequest is the body of PATCH /v1/proxy/certificates/{id}.
type updateCertificateRequest struct {
	Enabled       *bool   `json:"enabled,omitempty"`
	Challenge     *string `json:"challenge,omitempty"`
	DNSProviderID *string `json:"dns_provider_id,omitempty"`
	Wildcard      *bool   `json:"wildcard,omitempty"`
}

// createDNSProvider serves POST /v1/proxy/dns-providers.
func (h *handler) createDNSProvider(w http.ResponseWriter, r *http.Request) {
	var req createDNSProviderRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	provider, err := h.dns.CreateProvider(r.Context(), CreateDNSProviderInput{
		Provider:   DNSProviderType(strings.TrimSpace(req.Provider)),
		Name:       req.Name,
		Zones:      req.Zones,
		Credential: req.Credential,
		Enabled:    req.Enabled,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dnsProviderEnvelope{Provider: newDNSProviderResponse(provider)})
}

// listDNSProviders serves GET /v1/proxy/dns-providers.
func (h *handler) listDNSProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.dns.ListProviders(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]dnsProviderResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, newDNSProviderResponse(provider))
	}
	writeJSON(w, http.StatusOK, dnsProviderListEnvelope{Providers: response})
}

// getDNSProvider serves GET /v1/proxy/dns-providers/{id}.
func (h *handler) getDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "provider")
	if !ok {
		return
	}
	provider, err := h.dns.GetProvider(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dnsProviderEnvelope{Provider: newDNSProviderResponse(provider)})
}

// updateDNSProvider serves PATCH /v1/proxy/dns-providers/{id}.
func (h *handler) updateDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "provider")
	if !ok {
		return
	}
	var req updateDNSProviderRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	in := UpdateDNSProviderInput{Name: req.Name, Zones: req.Zones, Credential: req.Credential, Enabled: req.Enabled}
	if req.Provider != nil {
		providerType := DNSProviderType(strings.TrimSpace(*req.Provider))
		in.Provider = &providerType
	}
	provider, err := h.dns.UpdateProvider(r.Context(), id, in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dnsProviderEnvelope{Provider: newDNSProviderResponse(provider)})
}

// deleteDNSProvider serves DELETE /v1/proxy/dns-providers/{id}.
func (h *handler) deleteDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "provider")
	if !ok {
		return
	}
	if err := h.dns.DeleteProvider(r.Context(), id); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// createCertificate serves POST /v1/proxy/certificates.
func (h *handler) createCertificate(w http.ResponseWriter, r *http.Request) {
	var req createCertificateRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	applicationID, err := uuid.Parse(strings.TrimSpace(req.ApplicationID))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid application_id"})
		return
	}
	providerID, ok := parseOptionalUUID(w, req.DNSProviderID, "invalid dns_provider_id")
	if !ok {
		return
	}
	certificate, err := h.certs.CreateCertificate(r.Context(), CreateCertificateInput{
		ApplicationID: applicationID,
		Enabled:       req.Enabled,
		Challenge:     ChallengeMode(strings.TrimSpace(req.Challenge)),
		DNSProviderID: providerID,
		Wildcard:      req.Wildcard,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, certificateEnvelope{Certificate: newCertificateResponse(certificate, CertificateStatusObservation{})})
}

// listCertificates serves GET /v1/proxy/certificates. The node-observed
// status of every intent is computed on read; an unreachable node yields
// unknown and never turns the list into an error.
func (h *handler) listCertificates(w http.ResponseWriter, r *http.Request) {
	certificates, err := h.certs.ListCertificates(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	statuses := h.certificateStatuses(r, certificates)
	response := make([]certificateResponse, 0, len(certificates))
	for _, certificate := range certificates {
		response = append(response, newCertificateResponse(certificate, statuses[certificate.ID]))
	}
	writeJSON(w, http.StatusOK, certificateListEnvelope{Certificates: response})
}

// getCertificate serves GET /v1/proxy/certificates/{id}, including the
// node-observed status.
func (h *handler) getCertificate(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "certificate")
	if !ok {
		return
	}
	certificate, err := h.certs.GetCertificate(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	statuses := h.certificateStatuses(r, []DomainCertificate{certificate})
	writeJSON(w, http.StatusOK, certificateEnvelope{Certificate: newCertificateResponse(certificate, statuses[certificate.ID])})
}

// updateCertificate serves PATCH /v1/proxy/certificates/{id}.
func (h *handler) updateCertificate(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "certificate")
	if !ok {
		return
	}
	var req updateCertificateRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	in := UpdateCertificateInput{Enabled: req.Enabled, Wildcard: req.Wildcard}
	if req.Challenge != nil {
		mode := ChallengeMode(strings.TrimSpace(*req.Challenge))
		in.Challenge = &mode
	}
	if req.DNSProviderID != nil {
		providerID, ok := parseOptionalUUID(w, *req.DNSProviderID, "invalid dns_provider_id")
		if !ok {
			return
		}
		in.DNSProviderID = &providerID
	}
	certificate, err := h.certs.UpdateCertificate(r.Context(), id, in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, certificateEnvelope{Certificate: newCertificateResponse(certificate, CertificateStatusObservation{})})
}

// deleteCertificate serves DELETE /v1/proxy/certificates/{id}.
func (h *handler) deleteCertificate(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "certificate")
	if !ok {
		return
	}
	if err := h.certs.DeleteCertificate(r.Context(), id); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sslPathID parses the {id} path parameter, answering 400 on garbage.
func sslPathID(w http.ResponseWriter, r *http.Request, resource string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid " + resource + " id"})
		return uuid.Nil, false
	}
	return id, true
}

// parseOptionalUUID parses an optional UUID string, answering 400 on garbage.
func parseOptionalUUID(w http.ResponseWriter, raw, message string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: message})
		return uuid.Nil, false
	}
	return id, true
}

// writeServiceError maps the SSL/redirect service sentinels to HTTP responses.
// Only actionable service messages reach the client; unknown failures are
// generic and logged server-side (a credential never enters either path).
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
	case errors.Is(err, ErrSecret):
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: err.Error()})
	default:
		h.logger.Error("proxy: service request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// decodeRequiredBody decodes a required JSON object body into dst. It enforces
// the request size bound over the whole body, rejects an empty body and JSON
// null, rejects any content after the first JSON value (a second value or
// garbage), and keeps strict unknown-field rejection.
func decodeRequiredBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "request body is required"})
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSSLBodyBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return false
	}
	if len(body) > maxSSLBodyBytes {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "request body is too large"})
		return false
	}
	trimmed := bytes.TrimSpace(body)
	switch {
	case len(trimmed) == 0:
		writeJSON(w, http.StatusBadRequest, apiError{Message: "request body is required"})
		return false
	case bytes.Equal(trimmed, []byte("null")):
		writeJSON(w, http.StatusBadRequest, apiError{Message: "request body must be a JSON object"})
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return false
	}
	// Only trailing whitespace may follow the first value; anything else is a
	// second value or garbage and fails the request.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "request body must contain a single JSON value"})
		return false
	}
	return true
}

// newDNSProviderResponse maps a provider onto its wire shape, dropping the
// sealed credential.
func newDNSProviderResponse(provider DNSProvider) dnsProviderResponse {
	return dnsProviderResponse{
		ID:             provider.ID.String(),
		Provider:       string(provider.Provider),
		Name:           provider.Name,
		Zones:          append([]string{}, provider.Zones...),
		Enabled:        provider.Enabled,
		CredentialsSet: provider.SealedCredential != "",
		CreatedAt:      provider.CreatedAt,
		UpdatedAt:      provider.UpdatedAt,
	}
}

// certificateStatuses observes the given certificates when a status service
// is configured; without one the map is empty and the response omits status.
func (h *handler) certificateStatuses(r *http.Request, certificates []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	if h.status == nil {
		return nil
	}
	return h.status.CertificateStatuses(r.Context(), certificates)
}

// newCertificateResponse maps a certificate config onto its wire shape. The
// observation is zero (or omitted) when no status service is configured;
// NotAfter is only rendered for a present status.
func newCertificateResponse(certificate DomainCertificate, observation CertificateStatusObservation) certificateResponse {
	response := certificateResponse{
		ID:            certificate.ID.String(),
		ApplicationID: certificate.ApplicationID.String(),
		Domain:        certificate.Domain,
		Enabled:       certificate.Enabled,
		Challenge:     string(certificate.Challenge),
		Wildcard:      certificate.Wildcard,
		CreatedAt:     certificate.CreatedAt,
		UpdatedAt:     certificate.UpdatedAt,
	}
	if certificate.DNSProviderID != uuid.Nil {
		response.DNSProviderID = certificate.DNSProviderID.String()
	}
	if observation.Status != "" {
		response.Status = string(observation.Status)
	}
	if observation.Status == CertificateStatusPresent && !observation.NotAfter.IsZero() {
		notAfter := observation.NotAfter.UTC()
		response.NotAfter = &notAfter
	}
	return response
}
