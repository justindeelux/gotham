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

const maxBodyBytes = 1 << 16

type checkResponse struct {
	Current     string     `json:"current"`
	Available   bool       `json:"available"`
	Version     string     `json:"version,omitempty"`
	Channel     string     `json:"channel,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	HTMLURL     string     `json:"html_url,omitempty"`
	Asset       string     `json:"asset,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	LastUpdate  *Status    `json:"last_update,omitempty"`
}

type changelogEntryBody struct {
	Version     string     `json:"version"`
	Channel     string     `json:"channel,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	HTMLURL     string     `json:"html_url,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

type changelogResponse struct {
	Current string               `json:"current"`
	Entries []changelogEntryBody `json:"entries"`
}

type applyRequest struct {
	Channel string `json:"channel,omitempty"`
}

type applyResponse struct {
	Applied bool   `json:"applied"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
	Staged  bool   `json:"staged"`
	Restart bool   `json:"restart"`
}

type errorBody struct {
	Message string `json:"message"`
}

type handler struct {
	svc     Service
	sched   Scheduler
	isAdmin func(*http.Request) bool
	logger  *slog.Logger
}

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
			status.Detail = ""
		}
		response.LastUpdate = &status
	}
	writeJSON(w, http.StatusOK, response)
}

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

func (h *handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	state := h.sched.ScheduleState()
	if !h.isAdmin(r) {
		state.LastCheckError = ""
	}
	writeJSON(w, http.StatusOK, state)
}

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

type validationBody struct {
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
}

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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
