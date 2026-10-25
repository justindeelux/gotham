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

const maxSSLBodyBytes = 1 << 20

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

type dnsProviderEnvelope struct {
	Provider dnsProviderResponse `json:"provider"`
}

type dnsProviderListEnvelope struct {
	Providers []dnsProviderResponse `json:"providers"`
}

type createDNSProviderRequest struct {
	Provider   string   `json:"provider"`
	Name       string   `json:"name,omitempty"`
	Zones      []string `json:"zones"`
	Credential string   `json:"credential"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

type updateDNSProviderRequest struct {
	Provider   *string   `json:"provider,omitempty"`
	Name       *string   `json:"name,omitempty"`
	Zones      *[]string `json:"zones,omitempty"`
	Credential *string   `json:"credential,omitempty"`
	Enabled    *bool     `json:"enabled,omitempty"`
}

type certificateResponse struct {
	OwnerKind     string                 `json:"owner_kind"`
	PanelState    *PanelCertificateState `json:"panel_state,omitempty"`
	ID            string                 `json:"id"`
	ApplicationID string                 `json:"application_id"`
	Domain        string                 `json:"domain"`
	Enabled       bool                   `json:"enabled"`
	Challenge     string                 `json:"challenge"`
	DNSProviderID string                 `json:"dns_provider_id,omitempty"`
	Wildcard      bool                   `json:"wildcard"`
	Status        string                 `json:"status,omitempty"`
	NotAfter      *time.Time             `json:"not_after,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type certificateEnvelope struct {
	Certificate certificateResponse `json:"certificate"`
}

type certificateListEnvelope struct {
	Certificates []certificateResponse `json:"certificates"`
}

type createCertificateRequest struct {
	ApplicationID string `json:"application_id"`
	Domain        string `json:"domain,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
	Challenge     string `json:"challenge,omitempty"`
	DNSProviderID string `json:"dns_provider_id,omitempty"`
	Wildcard      bool   `json:"wildcard,omitempty"`
}

type updateCertificateRequest struct {
	Domain        *string `json:"domain,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
	Challenge     *string `json:"challenge,omitempty"`
	DNSProviderID *string `json:"dns_provider_id,omitempty"`
	Wildcard      *bool   `json:"wildcard,omitempty"`
}

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
		Domain:        strings.TrimSpace(req.Domain),
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

func (h *handler) updateCertificate(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "certificate")
	if !ok {
		return
	}
	var req updateCertificateRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	in := UpdateCertificateInput{Domain: req.Domain, Enabled: req.Enabled, Wildcard: req.Wildcard}
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

func sslPathID(w http.ResponseWriter, r *http.Request, resource string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid " + resource + " id"})
		return uuid.Nil, false
	}
	return id, true
}

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

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, apiError{Message: "forbidden"})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
	case errors.Is(err, ErrSecret):
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: err.Error()})
	default:
		h.logger.Error("proxy: service request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

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
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "request body must contain a single JSON value"})
		return false
	}
	return true
}

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

func (h *handler) certificateStatuses(r *http.Request, certificates []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	if h.status == nil {
		return nil
	}
	return h.status.CertificateStatuses(r.Context(), certificates)
}

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
	response.OwnerKind = "application"
	if certificate.ApplicationID == uuid.Nil {
		response.OwnerKind = "control_plane"
		state := panelCertificateState(observation, certificate.Enabled)
		response.PanelState = &state
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
