package updates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// maxBodyBytes bounds an apply request body (it only carries a channel name).
const maxBodyBytes = 1 << 16

// checkResponse is the GET /v1/updates/check body.
type checkResponse struct {
	Current     string     `json:"current"`
	Available   bool       `json:"available"`
	Version     string     `json:"version,omitempty"`
	Channel     string     `json:"channel,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	Asset       string     `json:"asset,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	// LastUpdate is the durable outcome of the most recent update attempt, so a
	// failed or rolled-back update is visible instead of silently "successful".
	LastUpdate *Status `json:"last_update,omitempty"`
}

// applyRequest is the optional POST /v1/updates/apply body.
type applyRequest struct {
	Channel string `json:"channel,omitempty"`
}

// applyResponse is the POST /v1/updates/apply body.
type applyResponse struct {
	Applied bool   `json:"applied"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
	Staged  bool   `json:"staged"`
	Restart bool   `json:"restart"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the self-update routes for one Service.
type handler struct {
	svc    Service
	logger *slog.Logger
}

// Mount registers the self-update endpoints on r:
//
//	GET  /v1/updates/check   (any authenticated caller)
//	POST /v1/updates/apply   (platform operator only)
//
// auth is the server's RequireAuth chain; adminAuth is the server's
// RequireAuth + RequirePlatformAdmin chain, because applying an update replaces
// the whole runtime and must not be reachable from a plain session. A nil svc
// or FEATURE_UPDATES=false mounts nothing, so the control plane can call Mount
// unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, adminAuth func(http.Handler) http.Handler, svc Service) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/updates/check", h.check)
	})
	r.Group(func(admin chi.Router) {
		admin.Use(adminAuth)
		admin.Post("/v1/updates/apply", h.apply)
	})
}

// check serves GET /v1/updates/check.
func (h *handler) check(w http.ResponseWriter, r *http.Request) {
	release, err := h.svc.Check(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := checkResponse{Current: h.svc.Current(), Available: release != nil}
	if release != nil {
		response.Version = release.Version
		response.Channel = release.Channel
		response.Notes = release.Notes
		response.Asset = release.AssetName
		published := release.PublishedAt
		response.PublishedAt = &published
	}
	if last, err := h.svc.LastStatus(); err != nil {
		h.logger.Warn("updates: could not read the update status", "error", err)
	} else {
		response.LastUpdate = last
	}
	writeJSON(w, http.StatusOK, response)
}

// apply serves POST /v1/updates/apply.
func (h *handler) apply(w http.ResponseWriter, r *http.Request) {
	var request applyRequest
	if err := decodeOptionalBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return
	}
	result, err := h.svc.Apply(r.Context(), Channel(strings.ToLower(strings.TrimSpace(request.Channel))))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, applyResponse{
		Applied: result.Applied,
		Version: result.Version,
		Message: result.Message,
		Staged:  result.Staged,
		Restart: result.Restart,
	})
}

// writeServiceError maps update sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoPublicKey):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "self-update is not configured"})
	case errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, errorBody{Message: "release server timed out"})
	case errors.Is(err, ErrHTTP),
		errors.Is(err, ErrDownload),
		errors.Is(err, ErrNoRelease),
		errors.Is(err, ErrAssetNotFound),
		errors.Is(err, ErrManifest),
		errors.Is(err, ErrChecksumMismatch),
		errors.Is(err, ErrBadURL):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "release server error"})
	default:
		h.logger.Error("updates: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// decodeOptionalBody decodes a JSON body into dst, tolerating an empty body.
func decodeOptionalBody(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
