package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// fakeContainerFileNode serves the container file RPCs on top of mockNode, so
// the dialed node satisfies both Node and ContainerFileNode.
type fakeContainerFileNode struct {
	*mockNode

	mu           sync.Mutex
	capabilities ContainerFileCapabilities
	listing      ContainerFileListing
	entry        ContainerFileEntry
	content      []byte
	archive      []byte
	written      []byte
	writtenPath  string
	changes      []ContainerFileChange
	listPath     string
	listCursor   string
	err          error
	closed       bool
}

func newFakeContainerFileNode() *fakeContainerFileNode {
	return &fakeContainerFileNode{
		mockNode: &mockNode{},
		entry:    ContainerFileEntry{Path: "/etc/hosts", Name: "hosts", Kind: "file", Size: 10, Mode: 0o644},
		content:  []byte("127.0.0.1\n"),
	}
}

func (f *fakeContainerFileNode) ContainerFileCapabilities(context.Context, string) (ContainerFileCapabilities, error) {
	return f.capabilities, f.err
}

func (f *fakeContainerFileNode) ListContainerFiles(_ context.Context, _, path, cursor string) (ContainerFileListing, error) {
	f.mu.Lock()
	f.listPath, f.listCursor = path, cursor
	f.mu.Unlock()
	return f.listing, f.err
}

func (f *fakeContainerFileNode) StatContainerFile(_ context.Context, _, path string) (ContainerFileEntry, error) {
	if f.err != nil {
		return ContainerFileEntry{}, f.err
	}
	entry := f.entry
	entry.Path = path
	return entry, nil
}

func (f *fakeContainerFileNode) ReadContainerFile(_ context.Context, _, _ string, archive bool, dst io.Writer) error {
	if f.err != nil {
		return f.err
	}
	data := f.content
	if archive {
		data = f.archive
	}
	_, err := dst.Write(data)
	return err
}

func (f *fakeContainerFileNode) WriteContainerFile(_ context.Context, _, path string, _ int64, src io.Reader) error {
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.writtenPath = path
	f.written = data
	f.mu.Unlock()
	return f.err
}

func (f *fakeContainerFileNode) ChangeContainerFile(_ context.Context, _ string, change ContainerFileChange) error {
	f.mu.Lock()
	f.changes = append(f.changes, change)
	f.mu.Unlock()
	return f.err
}

func (f *fakeContainerFileNode) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func containerFileTestService(t *testing.T, userID uuid.UUID, app Application, node *fakeContainerFileNode) *Service {
	t.Helper()
	svc := newTestService(t, &fakeRepository{app: app})
	svc.containers = &fakeApplicationContainers{listed: []containers.Container{
		{ID: "mine", State: "running", Labels: map[string]string{labelAppID: app.ID.String()}},
		{ID: "foreign", State: "running", Labels: map[string]string{labelAppID: uuid.New().String()}},
	}}
	svc.dial = func(context.Context, uuid.UUID) (Node, error) { return node, nil }
	_ = userID
	return svc
}

func TestContainerFilesRejectForeignContainer(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	svc := containerFileTestService(t, userID, app, node)

	_, err := svc.ListContainerFiles(context.Background(), userID, app.ID, "foreign", "/", "")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("err = %v, want ErrContainerNotFound", err)
	}
	if node.listPath != "" {
		t.Errorf("the node was asked for %q, want no call", node.listPath)
	}
}

func TestContainerFilesRejectNonOwner(t *testing.T) {
	owner := uuid.New()
	app := testApplication(owner)
	node := newFakeContainerFileNode()
	svc := containerFileTestService(t, owner, app, node)

	_, err := svc.ListContainerFiles(context.Background(), uuid.New(), app.ID, "mine", "/", "")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestContainerFileListingPassesCursorAndClosesNode(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	node.listing = ContainerFileListing{
		Directory: ContainerFileEntry{Path: "/etc", Name: "etc", Kind: "directory"},
		Entries:   []ContainerFileEntry{{Path: "/etc/hosts", Name: "hosts", Kind: "file"}},
		Cursor:    "hosts",
		Truncated: true,
	}
	svc := containerFileTestService(t, userID, app, node)

	listing, err := svc.ListContainerFiles(context.Background(), userID, app.ID, "mine", "/etc", "ca.pem")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listing.Cursor != "hosts" || !listing.Truncated || len(listing.Entries) != 1 {
		t.Fatalf("listing = %+v", listing)
	}
	if node.listPath != "/etc" || node.listCursor != "ca.pem" {
		t.Errorf("node saw %q with cursor %q", node.listPath, node.listCursor)
	}
	if !node.closed {
		t.Error("the node connection was not closed")
	}
}

func TestContainerFilesRefuseStoppedContainer(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	svc := containerFileTestService(t, userID, app, node)
	svc.containers = &fakeApplicationContainers{listed: []containers.Container{
		{ID: "mine", State: "exited", Labels: map[string]string{labelAppID: app.ID.String()}},
	}}

	_, err := svc.ListContainerFiles(context.Background(), userID, app.ID, "mine", "/", "")
	if !errors.Is(err, ErrContainerNotRunning) {
		t.Fatalf("err = %v, want ErrContainerNotRunning", err)
	}
	if node.listPath != "" {
		t.Errorf("the node was asked for %q, want no call", node.listPath)
	}
}

func TestContainerFileReadRefusesDirectory(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	node.entry.Kind = "directory"
	svc := containerFileTestService(t, userID, app, node)

	var buffer bytes.Buffer
	if _, err := svc.ReadContainerFile(context.Background(), userID, app.ID, "mine", "/etc", containerFileTextBytes, &buffer); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("wrote %d bytes for a directory", buffer.Len())
	}
}

func TestContainerFileReadRefusesLargeFile(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	node.entry.Size = containerFileTextBytes + 1
	svc := containerFileTestService(t, userID, app, node)

	var buffer bytes.Buffer
	_, err := svc.ReadContainerFile(context.Background(), userID, app.ID, "mine", "/etc/big", containerFileTextBytes, &buffer)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("wrote %d bytes for a refused file", buffer.Len())
	}
}

func TestContainerFileDownloadUsesTheRawFileStream(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	node.content = []byte("raw")
	node.archive = []byte("tar")
	svc := containerFileTestService(t, userID, app, node)

	var file bytes.Buffer
	if _, err := svc.DownloadContainerFile(context.Background(), userID, app.ID, "mine", "/etc/hosts", &file); err != nil {
		t.Fatalf("download file: %v", err)
	}
	if file.String() != "raw" {
		t.Fatalf("file download = %q, want the raw bytes", file.String())
	}

	node.entry.Kind = "directory"
	var directory bytes.Buffer
	if _, err := svc.DownloadContainerFile(context.Background(), userID, app.ID, "mine", "/etc", &directory); err != nil {
		t.Fatalf("download directory: %v", err)
	}
	if directory.String() != "tar" {
		t.Fatalf("directory download = %q, want the archive", directory.String())
	}
}

func TestContainerFileWriteRefusesOversize(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	svc := containerFileTestService(t, userID, app, node)

	err := svc.WriteContainerFile(context.Background(), userID, app.ID, "mine", "/etc/big", containerFileMaxBytes+1, strings.NewReader("x"))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if node.writtenPath != "" {
		t.Errorf("the node was called for %q", node.writtenPath)
	}
}

func TestContainerFileWriteStreamsToNode(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	svc := containerFileTestService(t, userID, app, node)

	if err := svc.WriteContainerFile(context.Background(), userID, app.ID, "mine", "/etc/app.conf", 5, strings.NewReader("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if node.writtenPath != "/etc/app.conf" || string(node.written) != "hello" {
		t.Fatalf("written %q to %q", node.written, node.writtenPath)
	}
}

func TestContainerFileChangeReachesNode(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	node := newFakeContainerFileNode()
	svc := containerFileTestService(t, userID, app, node)

	err := svc.ChangeContainerFile(context.Background(), userID, app.ID, "mine", ContainerFileChange{
		Action:      ContainerFileActionChmod,
		Path:        "/etc/hosts",
		Mode:        0o600,
		Overwrite:   true,
		Destination: "/etc/hosts",
	})
	if err != nil {
		t.Fatalf("change: %v", err)
	}
	if len(node.changes) != 1 || node.changes[0].Action != ContainerFileActionChmod || node.changes[0].Mode != 0o600 {
		t.Fatalf("changes = %+v", node.changes)
	}
}

// fakeContainerFileService serves the routes without a node.
type fakeContainerFileService struct {
	listing    ContainerFileListing
	entry      ContainerFileEntry
	content    []byte
	err        error
	seenPath   string
	seenDest   string
	seenMode   uint32
	changes    []ContainerFileChange
	writes     []string
	written    []byte
	listCursor string
}

func (f *fakeContainerFileService) ContainerFileCapabilities(context.Context, uuid.UUID, uuid.UUID, string) (ContainerFileCapabilities, error) {
	return ContainerFileCapabilities{Readable: true, ExecAvailable: true, State: "running"}, f.err
}

func (f *fakeContainerFileService) ListContainerFiles(_ context.Context, _, _ uuid.UUID, _, directory, cursor string) (ContainerFileListing, error) {
	f.seenPath, f.listCursor = directory, cursor
	return f.listing, f.err
}

func (f *fakeContainerFileService) StatContainerFile(_ context.Context, _, _ uuid.UUID, _, filePath string) (ContainerFileEntry, error) {
	f.seenPath = filePath
	return f.entry, f.err
}

func (f *fakeContainerFileService) ReadContainerFile(_ context.Context, _, _ uuid.UUID, _, _ string, _ int64, dst io.Writer) (ContainerFileEntry, error) {
	if f.err != nil {
		return ContainerFileEntry{}, f.err
	}
	_, err := dst.Write(f.content)
	return f.entry, err
}

func (f *fakeContainerFileService) DownloadContainerFile(_ context.Context, _, _ uuid.UUID, _, _ string, dst io.Writer) (ContainerFileEntry, error) {
	if f.err != nil {
		return ContainerFileEntry{}, f.err
	}
	_, err := dst.Write(f.content)
	return f.entry, err
}

func (f *fakeContainerFileService) WriteContainerFile(_ context.Context, _, _ uuid.UUID, _, filePath string, size int64, src io.Reader) error {
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	f.writes = append(f.writes, fmt.Sprintf("%s:%d", filePath, size))
	f.written = data
	return f.err
}

func (f *fakeContainerFileService) ChangeContainerFile(_ context.Context, _, _ uuid.UUID, _ string, change ContainerFileChange) error {
	f.changes = append(f.changes, change)
	f.seenPath, f.seenDest, f.seenMode = change.Path, change.Destination, change.Mode
	return f.err
}

func newContainerFileRouter(svc ContainerFileService, userID uuid.UUID) http.Handler {
	r := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	if userID == uuid.Nil {
		MountContainerFiles(r, auth, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false }, svc)
		return r
	}
	MountContainerFiles(r, auth, alwaysUser(userID), svc)
	return r
}

func containerFilesPath(appID uuid.UUID, tail string) string {
	base := "/v1/applications/" + appID.String() + "/containers/mine/files"
	if tail == "" {
		return base
	}
	if strings.HasPrefix(tail, "?") {
		return base + tail
	}
	return base + "/" + tail
}

func TestRouteContainerFilesRequireUser(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.Nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), ""), nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRouteContainerFilesList(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeContainerFileService{listing: ContainerFileListing{
		Directory: ContainerFileEntry{Path: "/", Kind: "directory"},
		Entries:   []ContainerFileEntry{{Path: "/etc", Name: "etc", Kind: "directory", Mode: 0o755}},
	}}
	srv := newContainerFileRouter(svc, userID)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(appID, "?path=/&cursor=etc"), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.seenPath != "/" || svc.listCursor != "etc" {
		t.Errorf("service saw path %q cursor %q", svc.seenPath, svc.listCursor)
	}
	var listing ContainerFileListing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Mode != 0o755 {
		t.Errorf("entries = %+v", listing.Entries)
	}
}

func TestRouteContainerFilesForeignContainerIs404(t *testing.T) {
	svc := &fakeContainerFileService{err: fmt.Errorf("%w: foreign", ErrContainerNotFound)}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "?path=/"), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRouteContainerFilesNotRunningCarriesTheStableCode(t *testing.T) {
	svc := &fakeContainerFileService{err: fmt.Errorf("%w: exited", ErrContainerNotRunning)}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "?path=/"), nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "not_running" {
		t.Fatalf("code = %q, want not_running (body %s)", body.Code, rec.Body.String())
	}
}

func TestRouteContainerFilesForbiddenIs403(t *testing.T) {
	svc := &fakeContainerFileService{err: ErrForbidden}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "?path=/"), nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRouteContainerFilesContentRoundTrip(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeContainerFileService{entry: ContainerFileEntry{Path: "/etc/hosts", Kind: "file", Size: 10}, content: []byte("127.0.0.1\n")}
	srv := newContainerFileRouter(svc, userID)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(appID, "content?path=/etc/hosts"), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "127.0.0.1\n" {
		t.Fatalf("read = %d %q", rec.Code, rec.Body.String())
	}

	body := strings.NewReader("hello")
	request := httptest.NewRequest(http.MethodPut, containerFilesPath(appID, "content?path=/etc/hosts"), body)
	request.ContentLength = 5
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("write = %d %q", rec.Code, rec.Body.String())
	}
	if len(svc.writes) != 1 || svc.writes[0] != "/etc/hosts:5" || string(svc.written) != "hello" {
		t.Fatalf("writes = %v (%q)", svc.writes, svc.written)
	}
}

func TestRouteContainerFilesContentRequiresLength(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.New())
	request := httptest.NewRequest(http.MethodPut, containerFilesPath(uuid.New(), "content?path=/etc/hosts"), strings.NewReader("x"))
	request.ContentLength = -1
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRouteContainerFilesUploadUsesDirectoryAndName(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.New())
	body := strings.NewReader("abc")
	request := httptest.NewRequest(http.MethodPut, containerFilesPath(uuid.New(), "upload?path=/etc&name=app.conf"), body)
	request.ContentLength = 3
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.writes) != 1 || svc.writes[0] != "/etc/app.conf:3" {
		t.Fatalf("writes = %v", svc.writes)
	}
}

func TestRouteContainerFilesUploadRejectsTraversalName(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.New())
	body := strings.NewReader("abc")
	request := httptest.NewRequest(http.MethodPut, containerFilesPath(uuid.New(), "upload?path=/etc&name=.."), body)
	request.ContentLength = 3
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(svc.writes) != 0 {
		t.Fatalf("writes = %v", svc.writes)
	}
}

func TestRouteContainerFilesDownloadDirectoryIsTar(t *testing.T) {
	svc := &fakeContainerFileService{entry: ContainerFileEntry{Path: "/etc", Name: "etc", Kind: "directory"}, content: []byte("tar")}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "download?path=/etc"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/x-tar" {
		t.Errorf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "etc.tar") {
		t.Errorf("disposition = %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestRouteContainerFilesDownloadRefusesLargeFile(t *testing.T) {
	svc := &fakeContainerFileService{entry: ContainerFileEntry{
		Path: "/var/log/app.log",
		Name: "app.log",
		Kind: "file",
		Size: containerFileMaxBytes + 1,
	}}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "download?path=/var/log/app.log"), nil))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRouteContainerFilesChanges(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	cases := []struct {
		action string
		method string
		body   string
		check  func(*testing.T, *fakeContainerFileService)
	}{
		{"mkdir", http.MethodPost, `{"path":"/etc/new"}`, func(t *testing.T, svc *fakeContainerFileService) {
			if svc.changes[0].Action != ContainerFileActionMkdir || svc.seenPath != "/etc/new" {
				t.Fatalf("mkdir change = %+v", svc.changes[0])
			}
		}},
		{"rename", http.MethodPost, `{"path":"/etc/a","destination":"/etc/b"}`, func(t *testing.T, svc *fakeContainerFileService) {
			if svc.changes[0].Action != ContainerFileActionRename || svc.seenDest != "/etc/b" {
				t.Fatalf("rename change = %+v", svc.changes[0])
			}
		}},
		{"move", http.MethodPost, `{"path":"/etc/a","destination":"/var/a","overwrite":true}`, func(t *testing.T, svc *fakeContainerFileService) {
			if svc.changes[0].Action != ContainerFileActionMove || !svc.changes[0].Overwrite {
				t.Fatalf("move change = %+v", svc.changes[0])
			}
		}},
		{"copy", http.MethodPost, `{"path":"/etc/a","destination":"/etc/b"}`, func(t *testing.T, svc *fakeContainerFileService) {
			if svc.changes[0].Action != ContainerFileActionCopy {
				t.Fatalf("copy change = %+v", svc.changes[0])
			}
		}},
		{"chmod", http.MethodPut, `{"path":"/etc/a","mode":384}`, func(t *testing.T, svc *fakeContainerFileService) {
			if svc.changes[0].Action != ContainerFileActionChmod || svc.seenMode != 0o600 {
				t.Fatalf("chmod change = %+v", svc.changes[0])
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.action, func(t *testing.T) {
			svc := &fakeContainerFileService{}
			srv := newContainerFileRouter(svc, userID)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(testCase.method, containerFilesPath(appID, testCase.action), strings.NewReader(testCase.body)))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
			}
			testCase.check(t, svc)
		})
	}
}

func TestRouteContainerFilesChmodRejectsBadMode(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, containerFilesPath(uuid.New(), "chmod"), strings.NewReader(`{"path":"/etc/a","mode":32768}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRouteContainerFilesDelete(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, containerFilesPath(uuid.New(), "?path=/etc/old"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.changes) != 1 || svc.changes[0].Action != ContainerFileActionDelete || svc.seenPath != "/etc/old" {
		t.Fatalf("changes = %+v", svc.changes)
	}
}

func TestRouteContainerFilesCapabilities(t *testing.T) {
	svc := &fakeContainerFileService{}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "capabilities"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var capabilities ContainerFileCapabilities
	if err := json.Unmarshal(rec.Body.Bytes(), &capabilities); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !capabilities.ExecAvailable || capabilities.State != "running" {
		t.Fatalf("capabilities = %+v", capabilities)
	}
}

func TestRouteContainerFilesAgentUnavailableIs503(t *testing.T) {
	svc := &fakeContainerFileService{err: fmt.Errorf("%w: no dial", ErrAgentUnavailable)}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, containerFilesPath(uuid.New(), "?path=/"), nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRouteContainerFilesTooLargeIs413(t *testing.T) {
	svc := &fakeContainerFileService{err: fmt.Errorf("%w: too big", ErrTooLarge)}
	srv := newContainerFileRouter(svc, uuid.New())
	request := httptest.NewRequest(http.MethodPut, containerFilesPath(uuid.New(), "content?path=/etc/hosts"), strings.NewReader("x"))
	request.ContentLength = 1
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, request)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRouteContainerFilesConflictIs409(t *testing.T) {
	svc := &fakeContainerFileService{err: fmt.Errorf("%w: destination exists", ErrConflict)}
	srv := newContainerFileRouter(svc, uuid.New())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, containerFilesPath(uuid.New(), "copy"),
		strings.NewReader(`{"path":"/etc/a","destination":"/etc/b"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}
