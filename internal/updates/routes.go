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
	HTMLURL     string     `json:"html_url,omitempty"`
	Asset       string     `json:"asset,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	// LastUpdate is the durable outcome of the most recent update attempt, so a
	// failed or rolled-back update is visible instead of silently "successful".
	LastUpdate *Status `json:"last_update,omitempty"`
}

// changelogEntryBody is one entry of the GET /v1/updates/changelog body. Notes
// and the release link stay admin-gated like check notes; non-admins get the
// version metadata only.
type changelogEntryBody struct {
	Version     string     `json:"version"`
	Channel     string     `json:"channel,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	HTMLURL     string     `json:"html_url,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// changelogResponse is the GET /v1/updates/changelog body: every release newer
// than current newest-first (bounded), the running version's own entry when up
// to date, or an empty list for an unknown current version.
type changelogResponse struct {
	Current string               `json:"current"`
	Entries []changelogEntryBody `json:"entries"`
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
	svc     Service
	sched   Scheduler // nil when svc has no schedule surface
	isAdmin func(*http.Request) bool
	logger  *slog.Logger
}

// Mount registers the self-update endpoints on r:
//
//	GET  /v1/updates/check   (any authenticated caller)
//	GET  /v1/updates/changelog (any authenticated caller; notes admin-gated)
//	POST /v1/updates/apply   (platform operator only)
//	GET  /v1/updates/schedule (any authenticated caller)
//	PUT  /v1/updates/schedule (platform operator only)
//
// auth is the server's RequireAuth + read-scope chain (the check is a read);
// adminAuth is the server's RequireAuth + RequirePlatformAdmin chain, because
// applying an update replaces the whole runtime and must not be reachable from
// a plain session. isAdmin
// reports whether the (already authenticated) caller is a platform operator, so
// release notes and filesystem-path details are only exposed to operators. A
// nil svc or FEATURE_UPDATES=false mounts nothing, so the control plane can call
// Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, adminAuth func(http.Handler) http.Handler, isAdmin func(*http.Request) bool, svc Service) {
	if svc == nil || !Enabled() {
		return
	}
	if isAdmin == nil {
		isAdmin = func(*http.Request) bool { return false }
	}
	h := &handler{svc: svc, isAdmin: isAdmin, logger: slog.Default()}
	h.sched, _ = svc.(Scheduler)
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/updates/check", h.check)
		protected.Get("/v1/updates/changelog", h.changelog)
		if h.sched != nil {
			protected.Get("/v1/updates/schedule", h.getSchedule)
		}
	})
	r.Group(func(admin chi.Router) {
		admin.Use(adminAuth)
		admin.Post("/v1/updates/apply", h.apply)
		if h.sched != nil {
			admin.Put("/v1/updates/schedule", h.putSchedule)
		}
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
		response.Asset = release.AssetName
		published := release.PublishedAt
		response.PublishedAt = &published
	}
	last, err := h.svc.LastStatus()
	if err != nil {
		h.logger.Warn("updates: could not read the update status", "error", err)
	}
	admin := h.isAdmin(r)
	if release != nil && admin {
		response.Notes = release.Notes
		response.HTMLURL = httpsLink(release.HTMLURL)
	}
	if last != nil {
		status := *last
		if !admin {
			// Never expose filesystem paths (the wrapper's detail) or other
			// operator-only context to a plain session.
			status.Detail = ""
		}
		response.LastUpdate = &status
	}
	writeJSON(w, http.StatusOK, response)
}

// changelog serves GET /v1/updates/changelog. Release bodies and links stay
// admin-gated like check notes; everyone else gets version metadata only. An
// unknown current version (dev builds) yields an empty list, not an error.
func (h *handler) changelog(w http.ResponseWriter, r *http.Request) {
	entries, err := h.svc.Changelog(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	admin := h.isAdmin(r)
	response := changelogResponse{Current: h.svc.Current(), Entries: []changelogEntryBody{}}
	for _, entry := range entries {
		body := changelogEntryBody{Version: entry.Version, Channel: entry.Channel}
		if entry.PublishedAt.IsZero() {
			// Omit the zero time so the viewer does not render year-1 dates.
		} else {
			published := entry.PublishedAt
			body.PublishedAt = &published
		}
		if admin {
			body.Notes = entry.Notes
			body.HTMLURL = httpsLink(entry.HTMLURL)
		}
		response.Entries = append(response.Entries, body)
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

// getSchedule serves GET /v1/updates/schedule. The raw check error is
// operator-only because it can carry release-server detail.
func (h *handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	state := h.sched.ScheduleState()
	if !h.isAdmin(r) {
		state.LastCheckError = ""
	}
	writeJSON(w, http.StatusOK, state)
}

// putSchedule serves PUT /v1/updates/schedule.
func (h *handler) putSchedule(w http.ResponseWriter, r *http.Request) {
	var schedule Schedule
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schedule); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return
	}
	state, err := h.sched.SetSchedule(r.Context(), schedule)
	var invalid *ScheduleError
	if errors.As(err, &invalid) {
		writeJSON(w, http.StatusUnprocessableEntity, validationBody{Message: "invalid update schedule", Fields: invalid.Fields})
		return
	}
	if err != nil {
		h.logger.Error("updates: could not save the schedule", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// validationBody is the 422 body for an invalid schedule.
type validationBody struct {
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
}

// writeServiceError maps update sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoPublicKey):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "self-update is not configured"})
	case errors.Is(err, ErrUpdatePending):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a previous update is still pending; confirm or reset it before applying another"})
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
