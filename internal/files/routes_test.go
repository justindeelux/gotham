package files

import (
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
)

// fakeService is a scriptable FileService for route tests.
type fakeService struct {
	mu sync.Mutex

	roots       []Root
	rootsErr    error
	listing     Listing
	listErr     error
	entry       Entry
	statErr     error
	content     Content
	contentErr  error
	ticket      string
	ticketErr   error
	download    *Download
	downloadErr error
	calls       []string

	writeResult  WriteResult
	writeErr     error
	writeHook    func(WriteRequest) (WriteResult, error)
	seenWrite    *WriteRequest
	writeBody    []byte
	authorizeErr error
	mkdirEntry   Entry
	mkdirErr     error
	createEntry  Entry
	createErr    error
	renameEntry  Entry
	renameErr    error
	chmodEntry   Entry
	chmodErr     error
	copyEntry    Entry
	copyErr      error
	deleteErr    error
}

func (f *fakeService) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

// wrote reports whether the service's Write was called, safe to read after a
// handler ran on another goroutine.
func (f *fakeService) wrote() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call == "write" {
			return true
		}
	}
	return false
}

func (f *fakeService) Roots(_ context.Context, _ uuid.UUID) ([]Root, error) {
	f.record("roots")
	return f.roots, f.rootsErr
}

func (f *fakeService) List(_ context.Context, _ uuid.UUID, _, _, _ string) (Listing, error) {
	f.record("list")
	return f.listing, f.listErr
}

func (f *fakeService) Stat(_ context.Context, _ uuid.UUID, _, _ string) (Entry, error) {
	f.record("stat")
	return f.entry, f.statErr
}

func (f *fakeService) Content(_ context.Context, _ uuid.UUID, _, _ string) (Content, error) {
	f.record("content")
	return f.content, f.contentErr
}

func (f *fakeService) IssueDownloadTicket(_ context.Context, _ uuid.UUID, _, _ string) (string, error) {
	f.record("ticket")
	return f.ticket, f.ticketErr
}

func (f *fakeService) Download(_ context.Context, _ uuid.UUID, _ string) (*Download, error) {
	f.record("download")
	return f.download, f.downloadErr
}

func (f *fakeService) AuthorizeWrite(_ context.Context, _ uuid.UUID) error {
	f.record("authorize")
	return f.authorizeErr
}

func (f *fakeService) Write(_ context.Context, _ uuid.UUID, req WriteRequest, body io.Reader) (WriteResult, error) {
	f.record("write")
	f.mu.Lock()
	f.seenWrite = &req
	f.mu.Unlock()
	if body != nil {
		data, _ := io.ReadAll(body)
		f.mu.Lock()
		f.writeBody = data
		f.mu.Unlock()
	}
	if f.writeHook != nil {
		return f.writeHook(req)
	}
	return f.writeResult, f.writeErr
}

func (f *fakeService) Mkdir(_ context.Context, _ uuid.UUID, _, _ string, _ uint32) (Entry, error) {
	f.record("mkdir")
	return f.mkdirEntry, f.mkdirErr
}

func (f *fakeService) Create(_ context.Context, _ uuid.UUID, _, _ string, _ uint32) (Entry, error) {
	f.record("create")
	return f.createEntry, f.createErr
}

func (f *fakeService) Rename(_ context.Context, _ uuid.UUID, _, _, _ string, _ bool) (Entry, error) {
	f.record("rename")
	return f.renameEntry, f.renameErr
}

func (f *fakeService) Copy(_ context.Context, _ uuid.UUID, _, _, _ string, _ bool) (Entry, error) {
	f.record("copy")
	return f.copyEntry, f.copyErr
}

func (f *fakeService) Chmod(_ context.Context, _ uuid.UUID, _, _ string, _ uint32) (Entry, error) {
	f.record("chmod")
	return f.chmodEntry, f.chmodErr
}

func (f *fakeService) Delete(_ context.Context, _ uuid.UUID, _, _ string, _ bool) error {
	f.record("delete")
	return f.deleteErr
}

// passthroughAuth stands in for RequireAuth in route tests.
func passthroughAuth(next http.Handler) http.Handler { return next }

// newTestRouter mounts the file routes with the fake service.
func newTestRouter(svc FileService) http.Handler {
	r := chi.NewRouter()
	Mount(r, passthroughAuth, svc, nil)
	return r
}

func doRequest(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRootsListStatRoutes(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{
		roots:   []Root{{Name: "volumes", Path: "/volumes", Writable: true, FreeBytes: 7}},
		listing: Listing{Entries: []Entry{{Name: "a", Kind: "file", Etag: `"e"`}}, Truncated: true, Cursor: "c"},
		entry:   Entry{Name: "a", Kind: "file"},
	}
	h := newTestRouter(svc)

	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/roots")
	if rec.Code != http.StatusOK {
		t.Fatalf("roots status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var roots rootsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
		t.Fatalf("decode roots: %v", err)
	}
	if len(roots.Roots) != 1 || roots.Roots[0].Name != "volumes" {
		t.Fatalf("roots = %+v", roots.Roots)
	}

	rec = doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/list?root=volumes&path=sub&cursor=cur")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listing Listing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !listing.Truncated || listing.Cursor != "c" || len(listing.Entries) != 1 {
		t.Fatalf("listing = %+v", listing)
	}

	rec = doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/stat?root=volumes&path=a")
	if rec.Code != http.StatusOK {
		t.Fatalf("stat status = %d", rec.Code)
	}
	var stat statEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &stat); err != nil {
		t.Fatalf("decode stat: %v", err)
	}
	if stat.Entry.Name != "a" {
		t.Fatalf("stat = %+v", stat)
	}

	rec = doRequest(t, h, http.MethodGet, "/v1/servers/not-a-uuid/files/roots")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad id status = %d, want 400", rec.Code)
	}
}

func TestContentRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{content: Content{Content: "hi", Etag: `"e"`, SHA256: "abc", Size: 2}}
	h := newTestRouter(svc)

	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/content?root=volumes&path=a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var body Content
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if body.Content != "hi" || body.SHA256 != "abc" {
		t.Fatalf("content = %+v", body)
	}
}

func TestErrorStatusMapping(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"forbidden", ErrForbidden, http.StatusForbidden},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"conflict", ErrConflict, http.StatusConflict},
		{"too large", ErrTooLarge, http.StatusRequestEntityTooLarge},
		{"not editable", ErrNotEditable, http.StatusUnsupportedMediaType},
		{"busy", ErrBusy, http.StatusTooManyRequests},
		{"outdated", ErrAgentOutdated, http.StatusNotImplemented},
		{"unavailable", ErrAgentUnavailable, http.StatusBadGateway},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
		{"other", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestRouter(&fakeService{listErr: tc.err})
			rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/list?root=volumes")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Message == "" {
				t.Fatalf("empty error message")
			}
		})
	}
}

func TestDownloadTicketRoute(t *testing.T) {
	id := uuid.New()
	h := newTestRouter(&fakeService{ticket: "tok"})

	rec := doRequest(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/download-ticket?root=volumes&path=a")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var body ticketEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	if body.Ticket != "tok" {
		t.Fatalf("ticket = %q", body.Ticket)
	}
}

func TestDownloadRequiresTicket(t *testing.T) {
	id := uuid.New()
	h := newTestRouter(&fakeService{})
	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/download")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestDownloadStreamsFirstChunkBeforeEnd asserts a download chunk reaches the
// client before the agent stream ends: the native download must start writing
// while the source is still producing.
func TestDownloadStreamsFirstChunkBeforeEnd(t *testing.T) {
	chunks := make(chan []byte)
	release := make(chan struct{})
	streamErr := make(chan error)

	go func() {
		chunks <- []byte("first")
		<-release
		chunks <- []byte("second")
		close(chunks)
		close(streamErr)
	}()

	id := uuid.New()
	h := newTestRouter(&fakeService{download: &Download{Name: "a.txt", Size: 11, Chunks: chunks, Err: streamErr}})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/servers/" + id.String() + "/files/download?ticket=t")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.ContentLength != 11 {
		t.Fatalf("content-length = %d, want 11", resp.ContentLength)
	}
	if disposition := resp.Header.Get("Content-Disposition"); !strings.Contains(disposition, `filename="a.txt"`) {
		t.Errorf("content-disposition = %q", disposition)
	}
	if cacheControl := resp.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Errorf("cache-control = %q, want no-store", cacheControl)
	}

	first := make([]byte, len("first"))
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatalf("read first chunk: %v", err)
	}
	if string(first) != "first" {
		t.Fatalf("first = %q", first)
	}
	// The source is still producing; the client already holds the first chunk.
	close(release)
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if string(rest) != "second" {
		t.Fatalf("rest = %q", rest)
	}
}

// TestDownloadAbortsOnMidStreamError asserts a terminal agent error after the
// headers were sent aborts the connection, so the browser marks the download as
// failed instead of accepting a short body.
// TestDownloadAbortsOnShortBody asserts a clean-end stream that produced fewer
// bytes than the Content-Length already sent aborts the connection, so the
// client sees a failed download instead of a truncated "success".
func TestDownloadAbortsOnShortBody(t *testing.T) {
	chunks := make(chan []byte)
	streamErr := make(chan error)
	go func() {
		chunks <- []byte("part")
		close(chunks)
		close(streamErr)
	}()

	id := uuid.New()
	h := newTestRouter(&fakeService{download: &Download{Name: "a.txt", Size: 100, Chunks: chunks, Err: streamErr}})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/servers/" + id.String() + "/files/download?ticket=t")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.ContentLength != 100 {
		t.Fatalf("content-length = %d, want 100", resp.ContentLength)
	}
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatalf("expected a truncated-body error, got none")
	}
}

// TestValidationErrorMessageIsGeneric asserts the 400 body never echoes the raw
// gRPC/agent error text a validation failure can carry.
func TestValidationErrorMessageIsGeneric(t *testing.T) {
	id := uuid.New()
	h := newTestRouter(&fakeService{
		listErr: fmt.Errorf("%w: rpc error: code = InvalidArgument desc = /etc/shadow: no such file", ErrValidation),
	})
	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/list?root=volumes")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Message != "invalid request" {
		t.Fatalf("message = %q, want %q", body.Message, "invalid request")
	}
}

func TestDownloadAbortsOnMidStreamError(t *testing.T) {
	chunks := make(chan []byte)
	streamErr := make(chan error, 1)
	go func() {
		chunks <- []byte("part")
		close(chunks)
		streamErr <- errors.New("agent died")
		close(streamErr)
	}()

	id := uuid.New()
	h := newTestRouter(&fakeService{download: &Download{Name: "a.txt", Size: 100, Chunks: chunks, Err: streamErr}})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/servers/" + id.String() + "/files/download?ticket=t")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatalf("expected a truncated-body error, got none")
	}
}
