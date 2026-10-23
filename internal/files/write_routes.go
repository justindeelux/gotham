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

const maxJSONBodyBytes int64 = 1 << 20

type writeEnvelope struct {
	Entry     Entry  `json:"entry"`
	Created   bool   `json:"created"`
	WriteMode string `json:"write_mode"`
}

type uploadResult struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Entry *Entry `json:"entry,omitempty"`
	Error string `json:"error,omitempty"`
}

type uploadEnvelope struct {
	Results []uploadResult `json:"results"`
}

type mkdirRequest struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

type chmodRequest struct {
	Root string  `json:"root"`
	Path string  `json:"path"`
	Mode *uint32 `json:"mode"`
}

type locationRequest struct {
	Root        string `json:"root"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

type limits interface {
	MaxUploadBytes() int64
	TransferTimeout() time.Duration
}

func (h *handler) requestLimits() (int64, time.Duration) {
	if l, ok := h.svc.(limits); ok {
		return l.MaxUploadBytes(), l.TransferTimeout()
	}
	return DefaultMaxUploadBytes, defaultTransferTimeout
}

func (h *handler) putContent(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
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
		Encoding:   query.Get("encoding"),
		FromEditor: true,
	}, body)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeEnvelope(result))
}

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

	if err := h.svc.AuthorizeWrite(r.Context(), serverID); err != nil {
		h.writeError(w, err)
		return
	}

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

func (h *handler) chmod(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req chmodRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Mode == nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "mode is required"})
		return
	}
	entry, err := h.svc.Chmod(r.Context(), serverID, req.Root, req.Path, *req.Mode)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statEnvelope{Entry: entry})
}

func (h *handler) rename(w http.ResponseWriter, r *http.Request) {
	h.locate(w, r, "rename")
}

func (h *handler) move(w http.ResponseWriter, r *http.Request) {
	h.locate(w, r, "rename")
}

func (h *handler) copy(w http.ResponseWriter, r *http.Request) {
	h.locate(w, r, "copy")
}

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

func (h *handler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	if err := h.svc.Delete(r.Context(), serverID, query.Get("root"), query.Get("path"), parseBool(query.Get("recursive")), parseBool(query.Get("permanent"))); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) extendReadDeadline(w http.ResponseWriter, window time.Duration) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(window))
}

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

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

func partBaseName(name string) string {
	return path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
}

func joinRel(dir, name string) string {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

func parseBool(raw string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && b
}

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
