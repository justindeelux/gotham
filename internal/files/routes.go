package files

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const downloadWriteTimeout = 60 * time.Second

type rootsEnvelope struct {
	Roots []Root `json:"roots"`
}

type statEnvelope struct {
	Entry Entry `json:"entry"`
}

type ticketEnvelope struct {
	Ticket string `json:"ticket"`
}

type errorBody struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type handler struct {
	svc    FileService
	logger *slog.Logger
	spool  *spooler
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc FileService, logger *slog.Logger) {
	if svc == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{svc: svc, logger: logger, spool: newSpooler("", 0, 0, logger)}
	r.Get("/v1/servers/{id}/files/download", h.download)
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/servers/{id}/files/roots", h.roots)
		protected.Get("/v1/servers/{id}/files/list", h.list)
		protected.Get("/v1/servers/{id}/files/stat", h.stat)
		protected.Get("/v1/servers/{id}/files/size", h.size)
		protected.Get("/v1/servers/{id}/files/search", h.search)
		protected.Get("/v1/servers/{id}/files/content", h.content)
		protected.Put("/v1/servers/{id}/files/content", h.putContent)
		protected.Get("/v1/servers/{id}/files/history", h.history)
		protected.Get("/v1/servers/{id}/files/history/{version}", h.historyVersion)
		protected.Post("/v1/servers/{id}/files/history/{version}/restore", h.restoreVersion)
		protected.Delete("/v1/servers/{id}/files/history/{version}", h.deleteVersion)
		protected.Post("/v1/servers/{id}/files/upload", h.upload)
		protected.Post("/v1/servers/{id}/files/mkdir", h.mkdir)
		protected.Post("/v1/servers/{id}/files/create", h.create)
		protected.Post("/v1/servers/{id}/files/rename", h.rename)
		protected.Post("/v1/servers/{id}/files/move", h.move)
		protected.Post("/v1/servers/{id}/files/copy", h.copy)
		protected.Post("/v1/servers/{id}/files/chmod", h.chmod)
		protected.Post("/v1/servers/{id}/files/compress", h.compress)
		protected.Post("/v1/servers/{id}/files/extract", h.extract)
		protected.Delete("/v1/servers/{id}/files/entry", h.deleteEntry)
		protected.Get("/v1/servers/{id}/files/trash", h.trash)
		protected.Post("/v1/servers/{id}/files/trash/restore", h.restoreTrash)
		protected.Post("/v1/servers/{id}/files/trash/purge", h.purgeTrash)
		protected.Post("/v1/servers/{id}/files/trash/empty", h.emptyTrash)
		protected.Post("/v1/servers/{id}/files/download-ticket", h.issueTicket)
	})
}

func (h *handler) roots(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	roots, err := h.svc.Roots(r.Context(), serverID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if roots == nil {
		roots = []Root{}
	}
	writeJSON(w, http.StatusOK, rootsEnvelope{Roots: roots})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	listing, err := h.svc.List(r.Context(), serverID, query.Get("root"), query.Get("path"), query.Get("cursor"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if listing.Entries == nil {
		listing.Entries = []Entry{}
	}
	writeJSON(w, http.StatusOK, listing)
}

func (h *handler) stat(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	entry, err := h.svc.Stat(r.Context(), serverID, query.Get("root"), query.Get("path"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statEnvelope{Entry: entry})
}

func (h *handler) content(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	content, err := h.svc.Content(r.Context(), serverID, query.Get("root"), query.Get("path"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, content)
}

func (h *handler) issueTicket(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	ticket, err := h.svc.IssueDownloadTicket(r.Context(), serverID, query.Get("root"), query.Get("path"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ticketEnvelope{Ticket: ticket})
}

func (h *handler) download(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if strings.TrimSpace(ticket) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "ticket is required"})
		return
	}
	download, err := h.svc.Download(r.Context(), serverID, ticket)
	if err != nil {
		h.writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(download.Size, 10))
	w.Header().Set("Content-Disposition", contentDisposition(download.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	controller := http.NewResponseController(w)
	var written int64
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, open := <-download.Chunks:
			if !open {
				streamErr, hasErr := <-download.Err
				if (hasErr && streamErr != nil) || written != download.Size {
					h.logger.Error("files: download failed mid-stream",
						"server_id", serverID.String(), "error", streamErr,
						"written", written, "size", download.Size)
					panic(http.ErrAbortHandler)
				}
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(downloadWriteTimeout))
			if _, err := w.Write(chunk); err != nil {
				return
			}
			written += int64(len(chunk))
			_ = controller.Flush()
		}
	}
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found", Code: "not_found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "forbidden", Code: "forbidden"})
	case errors.Is(err, ErrValidation) && errors.As(err, &maxBytes):
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "file is too large to edit", Code: "too_large"})
	case errors.Is(err, ErrUnrepresentable):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "text cannot be represented in the save encoding", Code: "unrepresentable"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request", Code: "invalid"})
	case errors.Is(err, ErrInvalidArchive):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "archive is invalid or truncated", Code: "invalid_archive"})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "conflict", Code: "conflict"})
	case errors.Is(err, ErrArchiveEntry):
		writeJSON(w, http.StatusConflict, errorBody{Message: "archive contains an entry that cannot be extracted", Code: "archive_entry"})
	case errors.Is(err, ErrTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "file is too large to edit", Code: "too_large"})
	case errors.Is(err, ErrArchiveLimit):
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "archive exceeds the node's archive limits", Code: "archive_limit"})
	case errors.Is(err, ErrNotEditable):
		writeJSON(w, http.StatusUnsupportedMediaType, errorBody{Message: "file is not editable text", Code: "not_editable"})
	case errors.Is(err, ErrTeamBusy):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: "the team's upload staging share is in use", Code: "busy"})
	case errors.Is(err, ErrBusy):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: "too many uploads are being staged", Code: "busy"})
	case errors.Is(err, ErrInsufficientStorage):
		writeJSON(w, http.StatusInsufficientStorage, errorBody{Message: "not enough free space", Code: "insufficient_storage"})
	case errors.Is(err, ErrAgentOutdated):
		writeJSON(w, http.StatusNotImplemented, errorBody{Message: "update the node agent", Code: "agent_outdated"})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "agent unavailable", Code: "agent_unavailable"})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "files are disabled", Code: "disabled"})
	case errors.Is(err, ErrHistoryUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "file history is unavailable", Code: "history_unavailable"})
	default:
		h.logger.Error("files: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error", Code: "error"})
	}
}

func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

func contentDisposition(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" || name == string(filepath.Separator) {
		name = "download"
	}
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return `attachment; filename="` + ascii + `"; filename*=UTF-8''` + url.PathEscape(name)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
