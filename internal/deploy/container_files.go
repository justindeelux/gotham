package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ContainerFileAction is one exec-based change to a container's filesystem.
type ContainerFileAction string

const (
	ContainerFileActionMkdir  ContainerFileAction = "mkdir"
	ContainerFileActionRename ContainerFileAction = "rename"
	ContainerFileActionMove   ContainerFileAction = "move"
	ContainerFileActionCopy   ContainerFileAction = "copy"
	ContainerFileActionDelete ContainerFileAction = "delete"
	ContainerFileActionChmod  ContainerFileAction = "chmod"
)

const (
	// containerFileMaxBytes is the largest file the control plane accepts or
	// serves in one request; the agent enforces the same cap.
	containerFileMaxBytes = 64 << 20
	// containerFileTextBytes is the largest file the in-app editor loads.
	containerFileTextBytes = 1 << 20
)

// ContainerFileEntry is one path inside an application container.
type ContainerFileEntry struct {
	Path          string     `json:"path"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Size          int64      `json:"size"`
	Mode          uint32     `json:"mode"`
	Uid           uint32     `json:"uid"`
	Gid           uint32     `json:"gid"`
	ModifiedAt    *time.Time `json:"modified_at,omitempty"`
	SymlinkTarget string     `json:"symlink_target,omitempty"`
}

// ContainerFileListing is one page of a container directory.
type ContainerFileListing struct {
	Directory ContainerFileEntry   `json:"directory"`
	Entries   []ContainerFileEntry `json:"entries"`
	Truncated bool                 `json:"truncated"`
	Cursor    string               `json:"cursor,omitempty"`
}

// ContainerFileCapabilities reports what a container's filesystem allows now.
type ContainerFileCapabilities struct {
	Readable      bool   `json:"readable"`
	ExecAvailable bool   `json:"exec_available"`
	ExecBlockedBy string `json:"exec_blocked_by,omitempty"`
	State         string `json:"state"`
}

// ContainerFileChange is one exec-based action.
type ContainerFileChange struct {
	Action      ContainerFileAction
	Path        string
	Destination string
	Mode        uint32
	Overwrite   bool
}

// ContainerFileService is the slice of the deploy service the container file
// routes use. It is separate from DeployService so a server without container
// files can still mount the rest of the application API.
type ContainerFileService interface {
	ContainerFileCapabilities(ctx context.Context, userID, appID uuid.UUID, containerID string) (ContainerFileCapabilities, error)
	ListContainerFiles(ctx context.Context, userID, appID uuid.UUID, containerID, directory, cursor string) (ContainerFileListing, error)
	StatContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string) (ContainerFileEntry, error)
	ReadContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string, limit int64, dst io.Writer) (ContainerFileEntry, error)
	DownloadContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string, dst io.Writer) (ContainerFileEntry, error)
	WriteContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string, size int64, src io.Reader) error
	ChangeContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID string, change ContainerFileChange) error
}

var _ ContainerFileService = (*Service)(nil)

// containerFileConn is one checked container and the node that hosts it.
type containerFileConn struct {
	node      ContainerFileNode
	container ApplicationContainer
	close     func() error
}

// containerFileTarget applies the same rules to every container file call:
// the application is visible to the caller, no backup is running, the
// container belongs to the application (GOTHAM-209), and the node implements
// the file RPCs.
func (s *Service) containerFileTarget(ctx context.Context, userID, appID uuid.UUID, containerID string) (*containerFileConn, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	if s.containers == nil {
		return nil, fmt.Errorf("%w: container operations are not configured", ErrAgentUnavailable)
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return nil, err
	}
	if app.UserID != userID {
		return nil, ErrForbidden
	}
	if s.refuseDuringBackup(ctx, app.ID) {
		return nil, ErrBackupInFlight
	}
	owned, err := s.applicationContainers(ctx, app)
	if err != nil {
		return nil, err
	}
	for _, candidate := range owned {
		if candidate.ID != containerID {
			continue
		}
		node, dialErr := s.dialNode(ctx, app.ServerID)
		if dialErr != nil {
			return nil, dialErr
		}
		files, ok := node.(ContainerFileNode)
		if !ok {
			_ = node.Close()
			return nil, fmt.Errorf("%w: this node agent does not support container files; update the node agent", ErrAgentUnavailable)
		}
		return &containerFileConn{node: files, container: candidate, close: node.Close}, nil
	}
	return nil, fmt.Errorf("%w: container %s is not part of this application", ErrContainerNotFound, containerID)
}

func (c *containerFileConn) Close() {
	if c != nil && c.close != nil {
		_ = c.close()
	}
}

func (s *Service) ContainerFileCapabilities(ctx context.Context, userID, appID uuid.UUID, containerID string) (ContainerFileCapabilities, error) {
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return ContainerFileCapabilities{}, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	return conn.node.ContainerFileCapabilities(callCtx, containerID)
}

func (s *Service) ListContainerFiles(ctx context.Context, userID, appID uuid.UUID, containerID, directory, cursor string) (ContainerFileListing, error) {
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return ContainerFileListing{}, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	return conn.node.ListContainerFiles(callCtx, containerID, directory, cursor)
}

func (s *Service) StatContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string) (ContainerFileEntry, error) {
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return ContainerFileEntry{}, err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	return conn.node.StatContainerFile(callCtx, containerID, filePath)
}

// ReadContainerFile streams one file's bytes, up to limit. A directory is
// refused: it has to be downloaded as an archive instead. The limit is checked
// before anything is written, so the caller can still answer with an error.
func (s *Service) ReadContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string, limit int64, dst io.Writer) (ContainerFileEntry, error) {
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return ContainerFileEntry{}, err
	}
	defer conn.Close()
	entry, err := conn.node.StatContainerFile(ctx, containerID, filePath)
	if err != nil {
		return ContainerFileEntry{}, err
	}
	if entry.Kind != "file" && entry.Kind != "symlink" {
		return ContainerFileEntry{}, fmt.Errorf("%w: %q is not a file", ErrValidation, filePath)
	}
	if limit <= 0 || limit > containerFileMaxBytes {
		limit = containerFileMaxBytes
	}
	if entry.Size > limit {
		return entry, fmt.Errorf("%w: %q is larger than %d bytes", ErrTooLarge, filePath, limit)
	}
	if err := conn.node.ReadContainerFile(ctx, containerID, filePath, false, dst); err != nil {
		return entry, err
	}
	return entry, nil
}

// DownloadContainerFile streams one file's bytes, or the recursive Docker
// archive of a directory.
func (s *Service) DownloadContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string, dst io.Writer) (ContainerFileEntry, error) {
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return ContainerFileEntry{}, err
	}
	defer conn.Close()
	entry, err := conn.node.StatContainerFile(ctx, containerID, filePath)
	if err != nil {
		return ContainerFileEntry{}, err
	}
	if entry.Kind == "file" && entry.Size > containerFileMaxBytes {
		return entry, fmt.Errorf("%w: %q is larger than %d bytes", ErrTooLarge, filePath, int64(containerFileMaxBytes))
	}
	if err := conn.node.ReadContainerFile(ctx, containerID, filePath, entry.Kind == "directory", dst); err != nil {
		return entry, err
	}
	return entry, nil
}

// WriteContainerFile stores one file, keeping the owner and mode the agent
// reports for an existing file.
func (s *Service) WriteContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID, filePath string, size int64, src io.Reader) error {
	if filePath == "" {
		return fmt.Errorf("%w: a path is required", ErrValidation)
	}
	if size < 0 || size > containerFileMaxBytes {
		return fmt.Errorf("%w: a file is limited to %d bytes", ErrTooLarge, int64(containerFileMaxBytes))
	}
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.node.WriteContainerFile(ctx, containerID, filePath, size, src)
}

func (s *Service) ChangeContainerFile(ctx context.Context, userID, appID uuid.UUID, containerID string, change ContainerFileChange) error {
	if strings.TrimSpace(change.Path) == "" {
		return fmt.Errorf("%w: a path is required", ErrValidation)
	}
	conn, err := s.containerFileTarget(ctx, userID, appID, containerID)
	if err != nil {
		return err
	}
	defer conn.Close()
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	return conn.node.ChangeContainerFile(callCtx, containerID, change)
}

type containerFileChangeRequest struct {
	Path        string `json:"path"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
	Mode        uint32 `json:"mode"`
}

type containerFileWriteEnvelope struct {
	Path string `json:"path"`
}

// containerFileHandler serves the container-scoped file routes.
type containerFileHandler struct {
	svc    ContainerFileService
	userID UserIDFunc
	logger *slog.Logger
}

// MountContainerFiles adds the per-container file routes to an application
// router. The caller passes the same authorization the application terminal
// uses, so file access never exceeds terminal access.
func MountContainerFiles(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ContainerFileService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &containerFileHandler{svc: svc, userID: userID, logger: slog.Default()}
	base := "/v1/applications/{id}/containers/{containerID}/files"
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get(base, h.list)
		protected.Get(base+"/capabilities", h.capabilities)
		protected.Get(base+"/stat", h.stat)
		protected.Get(base+"/content", h.read)
		protected.Put(base+"/content", h.write)
		protected.Get(base+"/download", h.download)
		protected.Put(base+"/upload", h.upload)
		protected.Post(base+"/mkdir", h.mkdir)
		protected.Post(base+"/rename", h.rename)
		protected.Post(base+"/move", h.move)
		protected.Post(base+"/copy", h.copy)
		protected.Put(base+"/chmod", h.chmod)
		protected.Delete(base, h.delete)
	})
}

func (h *containerFileHandler) target(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, string, bool) {
	userID, ok := h.userID(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, uuid.Nil, "", false
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, "", false
	}
	containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "a container is required"})
		return uuid.Nil, uuid.Nil, "", false
	}
	return userID, appID, containerID, true
}

func (h *containerFileHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	listing, err := h.svc.ListContainerFiles(r.Context(), userID, appID, containerID,
		strings.TrimSpace(r.URL.Query().Get("path")), strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (h *containerFileHandler) capabilities(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	capabilities, err := h.svc.ContainerFileCapabilities(r.Context(), userID, appID, containerID)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, capabilities)
}

func (h *containerFileHandler) stat(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	entry, err := h.svc.StatContainerFile(r.Context(), userID, appID, containerID, strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (h *containerFileHandler) read(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "a path is required"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	sink := &containerFileSink{writer: w}
	if _, err := h.svc.ReadContainerFile(r.Context(), userID, appID, containerID, filePath, containerFileTextBytes, sink); err != nil {
		h.failStream(w, err, sink.written)
	}
}

func (h *containerFileHandler) write(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "a path is required"})
		return
	}
	if !h.writeBody(w, r, userID, appID, containerID, filePath) {
		return
	}
	writeJSON(w, http.StatusOK, containerFileWriteEnvelope{Path: filePath})
}

func (h *containerFileHandler) upload(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	directory := strings.TrimSpace(r.URL.Query().Get("path"))
	name := path.Base(strings.TrimSpace(r.URL.Query().Get("name")))
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "a valid file name is required"})
		return
	}
	filePath := path.Join(directory, name)
	if !h.writeBody(w, r, userID, appID, containerID, filePath) {
		return
	}
	writeJSON(w, http.StatusOK, containerFileWriteEnvelope{Path: filePath})
}

func (h *containerFileHandler) writeBody(w http.ResponseWriter, r *http.Request, userID, appID uuid.UUID, containerID, filePath string) bool {
	if r.ContentLength < 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "a content length is required"})
		return false
	}
	if r.ContentLength > containerFileMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{
			Message: fmt.Sprintf("a file is limited to %d bytes", int64(containerFileMaxBytes)),
		})
		return false
	}
	body := http.MaxBytesReader(w, r.Body, containerFileMaxBytes)
	if err := h.svc.WriteContainerFile(r.Context(), userID, appID, containerID, filePath, r.ContentLength, body); err != nil {
		h.fail(w, err)
		return false
	}
	return true
}

func (h *containerFileHandler) download(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "a path is required"})
		return
	}
	entry, err := h.svc.StatContainerFile(r.Context(), userID, appID, containerID, filePath)
	if err != nil {
		h.fail(w, err)
		return
	}
	if entry.Kind == "file" && entry.Size > containerFileMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{
			Message: fmt.Sprintf("a download is limited to %d bytes", int64(containerFileMaxBytes)),
		})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name := entry.Name
	contentType := "application/octet-stream"
	if entry.Kind == "directory" {
		name += ".tar"
		contentType = "application/x-tar"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(name))
	sink := &containerFileSink{writer: w}
	if _, err := h.svc.DownloadContainerFile(r.Context(), userID, appID, containerID, filePath, sink); err != nil {
		h.failStream(w, err, sink.written)
	}
}

func (h *containerFileHandler) mkdir(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, ContainerFileActionMkdir)
}

func (h *containerFileHandler) rename(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, ContainerFileActionRename)
}

func (h *containerFileHandler) move(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, ContainerFileActionMove)
}

func (h *containerFileHandler) copy(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, ContainerFileActionCopy)
}

func (h *containerFileHandler) chmod(w http.ResponseWriter, r *http.Request) {
	h.change(w, r, ContainerFileActionChmod)
}

func (h *containerFileHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := h.svc.ChangeContainerFile(r.Context(), userID, appID, containerID, ContainerFileChange{
		Action: ContainerFileActionDelete,
		Path:   filePath,
	}); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, containerFileWriteEnvelope{Path: filePath})
}

func (h *containerFileHandler) change(w http.ResponseWriter, r *http.Request, action ContainerFileAction) {
	userID, appID, containerID, ok := h.target(w, r)
	if !ok {
		return
	}
	var request containerFileChangeRequest
	if !decodeBody(w, r, &request) {
		return
	}
	if action == ContainerFileActionChmod {
		if request.Mode == 0 || request.Mode&^uint32(0o7777) != 0 {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "a mode between 0000 and 7777 is required"})
			return
		}
	}
	if action == ContainerFileActionRename || action == ContainerFileActionMove || action == ContainerFileActionCopy {
		if strings.TrimSpace(request.Destination) == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "a destination is required"})
			return
		}
	}
	err := h.svc.ChangeContainerFile(r.Context(), userID, appID, containerID, ContainerFileChange{
		Action:      action,
		Path:        strings.TrimSpace(request.Path),
		Destination: strings.TrimSpace(request.Destination),
		Mode:        request.Mode,
		Overwrite:   request.Overwrite,
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, containerFileWriteEnvelope{Path: strings.TrimSpace(request.Path)})
}

func (h *containerFileHandler) fail(w http.ResponseWriter, err error) {
	status, message := containerFileStatus(err)
	if status == http.StatusInternalServerError && h.logger != nil {
		h.logger.Warn("deploy: container file request failed", "error", err)
	}
	writeJSON(w, status, errorBody{Message: message})
}

// failStream answers an error before anything was written and aborts the
// response once the body started, so a client never saves a truncated file as
// a complete download.
func (h *containerFileHandler) failStream(w http.ResponseWriter, err error, written int64) {
	if written == 0 {
		h.fail(w, err)
		return
	}
	if h.logger != nil {
		h.logger.Warn("deploy: container file stream failed mid-response", "error", err, "written", written)
	}
	panic(http.ErrAbortHandler)
}

// containerFileSink counts the bytes a streamed body already wrote.
type containerFileSink struct {
	writer  io.Writer
	written int64
}

func (s *containerFileSink) Write(p []byte) (int, error) {
	n, err := s.writer.Write(p)
	s.written += int64(n)
	return n, err
}

func containerFileStatus(err error) (int, string) {
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, "the file is too large"
	case errors.Is(err, ErrDisabled):
		return http.StatusServiceUnavailable, "applications are disabled on this server"
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, "only the owner can browse an application's containers"
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "the application or container was not found"
	case errors.Is(err, ErrContainerNotFound):
		return http.StatusNotFound, "the container is not part of this application"
	case errors.Is(err, ErrServerNotFound):
		return http.StatusNotFound, "the node was not found"
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, ErrValidation):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, ErrConflict), errors.Is(err, ErrBackupInFlight), errors.Is(err, ErrDeployInFlight):
		return http.StatusConflict, err.Error()
	case errors.Is(err, ErrAgentUnavailable):
		return http.StatusServiceUnavailable, "the node agent is unavailable"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

func contentDisposition(name string) string {
	cleaned := strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(name)
	return `attachment; filename="` + cleaned + `"`
}
