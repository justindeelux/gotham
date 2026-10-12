package files

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// maxJSONBodyBytes bounds a JSON request body (mkdir/rename/copy payloads).
const maxJSONBodyBytes int64 = 1 << 20 // 1 MiB

// writeEnvelope is the body returned by PUT /content.
type writeEnvelope struct {
	Entry     Entry  `json:"entry"`
	Created   bool   `json:"created"`
	WriteMode string `json:"write_mode"`
}

// uploadResult is one file's outcome in a multipart upload. Error is a stable
// token (not raw agent text) the browser maps to an i18n message; a partial
// upload reports per-file successes and failures side by side.
type uploadResult struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Entry *Entry `json:"entry,omitempty"`
	Error string `json:"error,omitempty"`
}

// uploadEnvelope wraps the per-file results.
type uploadEnvelope struct {
	Results []uploadResult `json:"results"`
}

// mkdirRequest is the body of POST /mkdir and /create.
type mkdirRequest struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

// locationRequest is the body of POST /rename, /move and /copy.
type locationRequest struct {
	Root        string `json:"root"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

// limits reports the service's body ceiling and transfer budget, so the route
// layer can bound the request body and extend the read deadline to match. A
// service that does not implement it (the route test fake) keeps the defaults.
type limits interface {
	MaxUploadBytes() int64
	TransferTimeout() time.Duration
}

// requestLimits resolves the body ceiling and transfer budget for a handler.
func (h *handler) requestLimits() (int64, time.Duration) {
	if l, ok := h.svc.(limits); ok {
		return l.MaxUploadBytes(), l.TransferTimeout()
	}
	return DefaultMaxUploadBytes, defaultTransferTimeout
}

// putContent serves PUT .../files/content: the editor save. If-Match is
// required (428 when absent); ?overwrite=true force-replaces a target the
// caller has not read, and If-Match: * is treated as that force. The body is
// streamed to the agent; a request-body error cancels the agent stream so a
// partial save never commits.
func (h *handler) putContent(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	// Authorize before reading the body, so a read-only caller or a foreign
	// team is refused once (403/404) without the body being spooled, and a
	// disabled feature answers 503 without staging anything.
	if err := h.svc.AuthorizeWrite(r.Context(), serverID); err != nil {
		h.writeError(w, err)
		return
	}
	query := r.URL.Query()
	expectEtag := strings.TrimSpace(r.Header.Get("If-Match"))
	overwrite := parseBool(query.Get("overwrite"))
	if expectEtag == "*" {
		expectEtag = ""
		overwrite = true
	}
	if expectEtag == "" && !overwrite {
		writeJSON(w, http.StatusPreconditionRequired, errorBody{Message: "If-Match is required"})
		return
	}
	maxUpload, transfer := h.requestLimits()
	h.extendReadDeadline(w, transfer)
	if r.ContentLength > maxUpload {
		h.writeError(w, ErrTooLarge)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxUpload)
	size := r.ContentLength
	var cleanup func()
	if size < 0 {
		// A chunked save has no declared length, but the agent's write header
		// must state the exact size, so spool the body to learn it.
		file, written, done, err := h.spool.stage(body, maxUpload)
		if err != nil {
			h.writeError(w, err)
			return
		}
		body, size, cleanup = file, written, done
	}
	if cleanup != nil {
		defer cleanup()
	}
	result, err := h.svc.Write(r.Context(), serverID, WriteRequest{
		Root:       query.Get("root"),
		Path:       query.Get("path"),
		ExpectEtag: expectEtag,
		Overwrite:  overwrite,
		Size:       size,
	}, body)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeEnvelope(result))
}

// upload serves POST .../files/upload: a streamed multipart upload. Each part is
// spooled just long enough to learn its exact size (the agent's write header
// must declare it), then streamed to the agent. The request is never parsed
// whole (no ParseMultipartForm): one part is in flight at a time, so a partial
// multi-file upload reports a per-file result for every file.
func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	root := query.Get("root")
	dir := query.Get("path")
	overwrite := parseBool(query.Get("overwrite"))
	maxUpload, transfer := h.requestLimits()
	h.extendReadDeadline(w, transfer)

	// Authorize before reading the body, so a read_only caller is refused with
	// 403 once rather than per file (and the body is never spooled for them).
	if err := h.svc.AuthorizeWrite(r.Context(), serverID); err != nil {
		h.writeError(w, err)
		return
	}

	// Bound the whole request body, not only each part, so a multipart with
	// many parts cannot stream unbounded bytes through the control plane. The
	// per-part staging ceiling below still bounds one file's size.
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "expected multipart/form-data"})
		return
	}

	results := make([]uploadResult, 0)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// The request body failed before the next part could be read, so
			// the remaining parts are unreachable: record one request-level
			// failure instead of dropping them silently.
			h.logger.Warn("files: upload request truncated", "server_id", serverID.String(), "error", err)
			results = append(results, uploadResult{Error: uploadBodyError(err)})
			break
		}
		name := partBaseName(part.FileName())
		rel := joinRel(dir, name)
		result := uploadResult{Name: name, Path: rel}
		if name == "" || name == "." || name == ".." {
			result.Error = "invalid"
			results = append(results, result)
			continue
		}
		file, size, cleanup, err := h.spool.stage(part, maxUpload)
		if err != nil {
			result.Error = uploadBodyError(err)
			results = append(results, result)
			// One unreadable or oversized part must not drop the parts that
			// follow it: keep reading and report each one.
			continue
		}
		written, writeErr := h.svc.Write(r.Context(), serverID, WriteRequest{
			Root:      root,
			Path:      rel,
			Overwrite: overwrite,
			Size:      size,
		}, file)
		cleanup()
		if writeErr != nil {
			result.Error = publicError(writeErr)
		} else {
			entry := written.Entry
			result.Entry = &entry
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, uploadEnvelope{Results: results})
}

// mkdir serves POST .../files/mkdir (exclusive create).
func (h *handler) mkdir(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req mkdirRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entry, err := h.svc.Mkdir(r.Context(), serverID, req.Root, req.Path, req.Mode)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, statEnvelope{Entry: entry})
}

// create serves POST .../files/create: an exclusive empty-file create.
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req mkdirRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entry, err := h.svc.Create(r.Context(), serverID, req.Root, req.Path, req.Mode)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, statEnvelope{Entry: entry})
}

// rename serves POST .../files/rename.
func (h *handler) rename(w http.ResponseWriter, r *http.Request) {
	h.locate(w, r, "rename")
}

// move serves POST .../files/move: an alias of rename (moves are single-root).
func (h *handler) move(w http.ResponseWriter, r *http.Request) {
	h.locate(w, r, "rename")
}

// copy serves POST .../files/copy.
func (h *handler) copy(w http.ResponseWriter, r *http.Request) {
	h.locate(w, r, "copy")
}

// locate decodes a locationRequest and dispatches to the rename or copy
// service method.
func (h *handler) locate(w http.ResponseWriter, r *http.Request, op string) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req locationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var (
		entry Entry
		err   error
	)
	switch op {
	case "copy":
		entry, err = h.svc.Copy(r.Context(), serverID, req.Root, req.Source, req.Destination, req.Overwrite)
	default:
		entry, err = h.svc.Rename(r.Context(), serverID, req.Root, req.Source, req.Destination, req.Overwrite)
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statEnvelope{Entry: entry})
}

// deleteEntry serves DELETE .../files/entry.
func (h *handler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	if err := h.svc.Delete(r.Context(), serverID, query.Get("root"), query.Get("path"), parseBool(query.Get("recursive"))); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// extendReadDeadline pushes the connection's read deadline out by window so a
// long upload is not cut by the 30 s server ReadTimeout. It is a no-op on a
// ResponseWriter that does not support deadlines (the handler tests).
func (h *handler) extendReadDeadline(w http.ResponseWriter, window time.Duration) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(window))
}

// uploadBodyError renders a staging or request-body error as a stable per-file
// result token. An overrun of the total request body (http.MaxBytesError) is
// "too_large"; a sentinel staging error reuses publicError; anything else is a
// request-level "truncated".
func uploadBodyError(err error) string {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return "too_large"
	}
	switch {
	case errors.Is(err, ErrTooLarge), errors.Is(err, ErrBusy),
		errors.Is(err, ErrInsufficientStorage), errors.Is(err, ErrValidation):
		return publicError(err)
	default:
		return "truncated"
	}
}

// decodeJSON decodes a bounded JSON body, answering 400 on a malformed one.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

// partBaseName reduces a multipart filename to its base component, stripping a
// Windows path too, so an upload cannot name a destination outside the target
// directory.
func partBaseName(name string) string {
	return path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
}

// joinRel joins a directory and a base name with a forward slash.
func joinRel(dir, name string) string {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

// parseBool parses a query flag, treating anything but an explicit true as
// false.
func parseBool(raw string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && b
}

// publicError renders a service error as a stable token for the upload result
// array, never echoing raw agent text.
func publicError(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrValidation):
		return "invalid"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrInsufficientStorage):
		return "insufficient_storage"
	case errors.Is(err, ErrAgentOutdated):
		return "agent_outdated"
	case errors.Is(err, ErrAgentUnavailable):
		return "agent_unavailable"
	case errors.Is(err, ErrDisabled):
		return "disabled"
	default:
		return "error"
	}
}
