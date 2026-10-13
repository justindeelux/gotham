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

// downloadWriteTimeout bounds one chunk write to a stalled client. It is reset
// before every chunk, so a progressing (however slow) transfer is never cut and
// only a client that stops reading is dropped.
const downloadWriteTimeout = 60 * time.Second

// rootsEnvelope wraps the root list.
type rootsEnvelope struct {
	Roots []Root `json:"roots"`
}

// statEnvelope wraps a single entry.
type statEnvelope struct {
	Entry Entry `json:"entry"`
}

// ticketEnvelope carries a freshly minted download ticket.
type ticketEnvelope struct {
	Ticket string `json:"ticket"`
}

// errorBody is the JSON body returned for failures. Code is a stable,
// machine-readable token the browser maps to a localized message, so the
// server's English text is never shown; it is absent when only status matters.
type errorBody struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// handler serves the file routes for one FileService.
type handler struct {
	svc    FileService
	logger *slog.Logger
	// spool stages a streamed upload body to a temp file (the agent's write
	// header must declare an exact size, which a multipart part or a chunked
	// PUT does not carry). It bounds concurrent staging, checks free space and
	// sweeps stale files; see spool.go.
	spool *spooler
}

// Mount registers the file endpoints on r:
//
//	GET    /v1/servers/{id}/files/roots
//	GET    /v1/servers/{id}/files/list?root=&path=&cursor=
//	GET    /v1/servers/{id}/files/stat?root=&path=
//	GET    /v1/servers/{id}/files/content?root=&path=
//	PUT    /v1/servers/{id}/files/content?root=&path=          (If-Match / ?overwrite)
//	POST   /v1/servers/{id}/files/upload?root=&path=&overwrite=
//	POST   /v1/servers/{id}/files/mkdir
//	POST   /v1/servers/{id}/files/create
//	POST   /v1/servers/{id}/files/rename
//	POST   /v1/servers/{id}/files/move
//	POST   /v1/servers/{id}/files/copy
//	POST   /v1/servers/{id}/files/chmod
//	POST   /v1/servers/{id}/files/compress
//	POST   /v1/servers/{id}/files/extract
//	DELETE /v1/servers/{id}/files/entry?root=&path=&recursive=
//	POST   /v1/servers/{id}/files/download-ticket?root=&path=
//	GET    /v1/servers/{id}/files/download?ticket=
//
// auth wraps the group: the server passes its team chain (RequireAuth +
// RequireTeam + the write gate), and the service authorizes the target node's
// team and role before any agent read. The download route is deliberately
// outside the auth group: the single-use ticket is its credential, because a
// native browser download cannot attach the bearer token. logger is the
// control plane's configured logger; a nil logger falls back to slog.Default().
// A nil svc mounts nothing, so the control plane can call Mount unconditionally;
// the routes are mounted whenever the service exists, and answer 503 ErrDisabled
// while FEATURE_FILES is off.
//
// The ticket rides the query string. The control plane's request logger records
// r.URL.Path only, never the query, so the ticket never reaches the access log.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc FileService, logger *slog.Logger) {
	if svc == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{svc: svc, logger: logger, spool: newSpooler("", 0, logger)}
	r.Get("/v1/servers/{id}/files/download", h.download)
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/servers/{id}/files/roots", h.roots)
		protected.Get("/v1/servers/{id}/files/list", h.list)
		protected.Get("/v1/servers/{id}/files/stat", h.stat)
		protected.Get("/v1/servers/{id}/files/content", h.content)
		protected.Put("/v1/servers/{id}/files/content", h.putContent)
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
		protected.Post("/v1/servers/{id}/files/download-ticket", h.issueTicket)
	})
}

// roots serves GET .../files/roots.
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

// list serves GET .../files/list.
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

// stat serves GET .../files/stat.
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

// content serves GET .../files/content.
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

// issueTicket serves POST .../files/download-ticket.
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

// download serves GET .../files/download?ticket=.
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
	// A file body can hold secrets and the ticket URL is capability-bearing, so
	// neither the browser nor an intermediary may cache the response.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	// The response controller extends the write deadline per chunk and flushes,
	// so the browser starts writing the file before the stream ends and a
	// stalled client is dropped instead of pinning a connection. Both calls are
	// no-ops on a recorder that does not support deadlines (the handler tests).
	controller := http.NewResponseController(w)
	var written int64
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, open := <-download.Chunks:
			if !open {
				// The stream ended. A terminal error, or a byte count that does
				// not match the Content-Length already sent, cannot become a JSON
				// response, so the connection is aborted: the browser marks the
				// short body as a failed download rather than a complete file.
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

// writeError maps service sentinels to HTTP responses. The body carries a
// stable code token the browser maps to a localized message; the English
// message is a fallback and is never shown by the UI.
func (h *handler) writeError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found", Code: "not_found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "forbidden", Code: "forbidden"})
	case errors.Is(err, ErrValidation) && errors.As(err, &maxBytes):
		// A body that overran the ceiling while being read (a lying or chunked
		// Content-Length) is 413, not 400.
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "file is too large to edit", Code: "too_large"})
	case errors.Is(err, ErrValidation):
		// A fixed message: the underlying error can carry raw gRPC/agent text
		// (mapRPCError wraps the agent error), which must not reach the client.
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request", Code: "invalid"})
	case errors.Is(err, ErrInvalidArchive):
		// A truncated or corrupt archive is a 400 with its own copy, not the
		// generic "invalid request" and never a 500.
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "archive is invalid or truncated", Code: "invalid_archive"})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "conflict", Code: "conflict"})
	case errors.Is(err, ErrArchiveEntry):
		// A refused archive member (link/device/FIFO/unsupported type) is a
		// conflict-shaped refusal but not an existing-name clash, so it carries
		// its own copy instead of the generic "conflict".
		writeJSON(w, http.StatusConflict, errorBody{Message: "archive contains an entry that cannot be extracted", Code: "archive_entry"})
	case errors.Is(err, ErrTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "file is too large to edit", Code: "too_large"})
	case errors.Is(err, ErrArchiveLimit):
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Message: "archive exceeds the node's archive limits", Code: "archive_limit"})
	case errors.Is(err, ErrNotEditable):
		writeJSON(w, http.StatusUnsupportedMediaType, errorBody{Message: "file is not editable text", Code: "not_editable"})
	case errors.Is(err, ErrBusy):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: "node is busy", Code: "busy"})
	case errors.Is(err, ErrInsufficientStorage):
		writeJSON(w, http.StatusInsufficientStorage, errorBody{Message: "not enough free space", Code: "insufficient_storage"})
	case errors.Is(err, ErrAgentOutdated):
		writeJSON(w, http.StatusNotImplemented, errorBody{Message: "update the node agent", Code: "agent_outdated"})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "agent unavailable", Code: "agent_unavailable"})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "files are disabled", Code: "disabled"})
	default:
		h.logger.Error("files: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error", Code: "error"})
	}
}

// serverIDParam parses the {id} path parameter, answering 400 on a bad UUID.
func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

// contentDisposition builds an RFC 6266 attachment header: an ASCII fallback
// plus the UTF-8 filename*. The name is reduced to its base component so a
// surprising entry name cannot inject header content.
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

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
