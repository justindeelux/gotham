package templates

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/services"
)

// maxBodyBytes bounds a render request body. A form validates at most
// MaxFields values of at most MaxFieldValue each, so the bound is generous.
const maxBodyBytes = 64 << 10 // 64 KiB

// templateSummary is the catalog entry: metadata only, no form schema.
type templateSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
}

// templateDetail is one template's metadata plus its form schema.
type templateDetail struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Icon        string  `json:"icon"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

// specResponse is the parsed view of a rendered document, as returned to the
// caller (camel-free, snake_case like the rest of the API).
type specResponse struct {
	Services     []string                `json:"services"`
	Domains      []services.DomainRoute  `json:"domains"`
	NamedVolumes []string                `json:"named_volumes"`
	Mounts       []services.StorageMount `json:"mounts"`
}

// templateListEnvelope wraps the catalog.
type templateListEnvelope struct {
	Templates []templateSummary `json:"templates"`
}

// templateEnvelope wraps one template.
type templateEnvelope struct {
	Template templateDetail `json:"template"`
}

// renderRequest is the body of POST /v1/templates/{slug}/render. Values are
// keyed by field key; a scalar may be a string, a whole number or a boolean.
type renderRequest struct {
	Values map[string]any `json:"values"`
}

// renderResponse is the FE-7.1 contract: the validated compose document, the
// environment its secret references resolve against, and the parsed view.
// Deployment creates a service with compose_yaml and env through the existing
// services endpoints (see the package README).
type renderResponse struct {
	Slug        string            `json:"slug"`
	ComposeYAML string            `json:"compose_yaml"`
	Env         map[string]string `json:"env"`
	Spec        specResponse      `json:"spec"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the template routes for one Service.
type handler struct {
	svc    Service
	logger *slog.Logger
}

// Mount registers the authenticated template endpoints under /api:
//
//	GET  /v1/templates
//	GET  /v1/templates/{slug}
//	POST /v1/templates/{slug}/render
//
// auth wraps the group: the server passes the same admin-scoped wrapper the
// services routes use, because a rendered document is deployed through the
// service surface and mutates node state there. The surface itself is
// stateless and read-only (a render touches no user row and executes nothing),
// so no user accessor is needed.
//
// FEATURE_SERVICES=false mounts nothing (404), matching services.Mount; when
// the flag is flipped off after mounting, every call answers 503 through the
// per-request check, matching the service operations. A nil service mounts
// nothing either, so the control plane can call Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc Service) {
	if svc == nil || !services.Enabled() {
		return
	}
	h := &handler{svc: svc, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/templates", h.list)
		protected.Get("/v1/templates/{slug}", h.get)
		protected.Post("/v1/templates/{slug}/render", h.render)
	})
}

// enabled reports whether the services feature (and with it the template
// surface) is on, answering 503 when it is not: a runtime
// FEATURE_SERVICES=false behaves like the service operations, while the flag
// already off at startup unmounted the routes entirely (404).
func (h *handler) enabled(w http.ResponseWriter) bool {
	if services.Enabled() {
		return true
	}
	h.writeError(w, ErrDisabled)
	return false
}

// list serves GET .../templates: the catalog metadata.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	if !h.enabled(w) {
		return
	}
	listed := h.svc.List()
	response := make([]templateSummary, 0, len(listed))
	for _, template := range listed {
		response = append(response, templateSummary{
			Slug:        template.Slug,
			Name:        template.Name,
			Icon:        template.Icon,
			Description: template.Description,
		})
	}
	writeJSON(w, http.StatusOK, templateListEnvelope{Templates: response})
}

// get serves GET .../templates/{slug}: metadata plus the field schema.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	if !h.enabled(w) {
		return
	}
	template, ok := h.svc.Get(chi.URLParam(r, "slug"))
	if !ok {
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
		return
	}
	writeJSON(w, http.StatusOK, templateEnvelope{Template: newTemplateDetail(template)})
}

// render serves POST .../templates/{slug}/render.
func (h *handler) render(w http.ResponseWriter, r *http.Request) {
	if !h.enabled(w) {
		return
	}
	slug := chi.URLParam(r, "slug")
	var req renderRequest
	if !decodeBody(w, r, &req) {
		return
	}
	result, err := h.svc.Render(slug, req.Values)
	if err != nil {
		h.writeError(w, err)
		return
	}
	env := result.Env
	if env == nil {
		env = map[string]string{}
	}
	writeJSON(w, http.StatusOK, renderResponse{
		Slug:        slug,
		ComposeYAML: result.ComposeYAML,
		Env:         env,
		Spec: specResponse{
			Services:     result.Spec.Services,
			Domains:      result.Spec.Domains,
			NamedVolumes: result.Spec.NamedVolumes,
			Mounts:       result.Spec.Mounts,
		},
	})
}

// newTemplateDetail maps a domain template to its wire representation.
func newTemplateDetail(template Template) templateDetail {
	fields := make([]Field, 0, len(template.Fields))
	fields = append(fields, template.Fields...)
	return templateDetail{
		Slug:        template.Slug,
		Name:        template.Name,
		Icon:        template.Icon,
		Description: template.Description,
		Fields:      fields,
	}
}

// writeError maps template sentinels (and the services validation the render
// inherits) to HTTP responses.
func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrValidation), errors.Is(err, services.ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "services are disabled"})
	default:
		h.logger.Error("templates: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// decodeBody decodes a required JSON body into dst. An empty or malformed body
// answers 400, a body over the limit answers 413, and trailing content after
// the single JSON value is rejected: the endpoint consumes exactly one object.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	// Read one byte past the limit so an over-limit body is detected instead
	// of being silently truncated into a valid prefix.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(body) > maxBodyBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "request body is too large"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "request body is required"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	// Require EOF after the single JSON value: trailing garbage or a second
	// document is not a valid request.
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
