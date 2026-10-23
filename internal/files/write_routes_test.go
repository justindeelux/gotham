package files

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
)

func newRouterWithService(t *testing.T, svc FileService, teamID uuid.UUID) http.Handler {
	t.Helper()
	t.Setenv(FeatureEnv, "true")
	r := chi.NewRouter()
	Mount(r, scopeAuth(teams.RoleOwner, teamID), svc, nil)
	return r
}

func scopeAuth(role teams.Role, teamID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := teams.Scope{UserID: uuid.New(), TeamID: teamID, Role: role}
			next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
		})
	}
}

func newRealRouter(t *testing.T, role teams.Role, teamID uuid.UUID, client FileClient) (http.Handler, *servers.Server) {
	t.Helper()
	t.Setenv(FeatureEnv, "true")
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	svc := NewService(Config{
		Registry: registry,
		Dial:     func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
	})
	r := chi.NewRouter()
	Mount(r, scopeAuth(role, teamID), svc, nil)
	return r, server
}

func doWrite(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func buildMultipart(t *testing.T, files []struct{ name, content string }) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, file := range files {
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := io.WriteString(part, file.content); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return buf.String(), writer.FormDataContentType()
}

func TestPutContentRoute(t *testing.T) {
	id := uuid.New()
	base := "/v1/servers/" + id.String() + "/files/content?root=volumes&path=a.txt"

	t.Run("happy path passes the etag and body", func(t *testing.T) {
		svc := &fakeService{writeResult: WriteResult{Entry: Entry{Name: "a.txt", Etag: `"v2"`}, WriteMode: "rename"}}
		h := newTestRouter(svc)
		rec := doWrite(t, h, http.MethodPut, base, "hello", map[string]string{"If-Match": `"v1"`})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		if svc.seenWrite == nil || svc.seenWrite.ExpectEtag != `"v1"` || svc.seenWrite.Overwrite {
			t.Fatalf("seen write = %+v", svc.seenWrite)
		}
		if string(svc.writeBody) != "hello" {
			t.Fatalf("body = %q", svc.writeBody)
		}
	})

	t.Run("missing If-Match is 428", func(t *testing.T) {
		h := newTestRouter(&fakeService{})
		rec := doWrite(t, h, http.MethodPut, base, "hello", nil)
		if rec.Code != http.StatusPreconditionRequired {
			t.Fatalf("status = %d, want 428", rec.Code)
		}
	})

	t.Run("overwrite bypasses the precondition", func(t *testing.T) {
		svc := &fakeService{writeResult: WriteResult{Entry: Entry{Name: "a.txt"}}}
		h := newTestRouter(svc)
		rec := doWrite(t, h, http.MethodPut, base+"&overwrite=true", "hello", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		if svc.seenWrite == nil || !svc.seenWrite.Overwrite || svc.seenWrite.ExpectEtag != "" {
			t.Fatalf("seen write = %+v", svc.seenWrite)
		}
	})

	t.Run("etag conflict is 409", func(t *testing.T) {
		h := newTestRouter(&fakeService{writeErr: ErrConflict})
		rec := doWrite(t, h, http.MethodPut, base, "hello", map[string]string{"If-Match": `"stale"`})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", rec.Code)
		}
	})
}

type countingReader struct {
	n int
	r io.Reader
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestPutContentAuthorizesBeforeBody(t *testing.T) {
	id := uuid.New()
	path := "/v1/servers/" + id.String() + "/files/content?root=volumes&path=a.txt"

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"feature off", ErrDisabled, http.StatusServiceUnavailable},
		{"foreign team", ErrNotFound, http.StatusNotFound},
		{"read-only caller", ErrForbidden, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestRouter(&fakeService{authorizeErr: tc.err})
			body := &countingReader{r: strings.NewReader(strings.Repeat("x", 4096))}
			req := httptest.NewRequest(http.MethodPut, path, body)
			req.Header.Set("If-Match", `"v1"`)
			req.ContentLength = -1
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if body.n != 0 {
				t.Fatalf("the request body was read (%d bytes) before authorization", body.n)
			}
		})
	}
}

func TestUploadPartialFailure(t *testing.T) {
	id := uuid.New()
	body, contentType := buildMultipart(t, []struct{ name, content string }{
		{"a.txt", "hello"},
		{"b.txt", "world"},
	})
	svc := &fakeService{
		writeHook: func(req WriteRequest) (WriteResult, error) {
			if req.Path == "b.txt" {
				return WriteResult{}, ErrConflict
			}
			return WriteResult{Entry: Entry{Name: req.Path, Etag: `"e"`}}, nil
		},
	}
	h := newTestRouter(svc)
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/upload?root=volumes&path=", body,
		map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var envelope uploadEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if len(envelope.Results) != 2 {
		t.Fatalf("results = %+v", envelope.Results)
	}
	if envelope.Results[0].Name != "a.txt" || envelope.Results[0].Entry == nil {
		t.Fatalf("first result = %+v", envelope.Results[0])
	}
	if envelope.Results[1].Name != "b.txt" || envelope.Results[1].Error != "conflict" {
		t.Fatalf("second result = %+v", envelope.Results[1])
	}
}

func TestUploadBoundsTotalRequestBody(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := NewService(Config{
		Registry:       registry,
		Dial:           func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		MaxUploadBytes: 200,
	})
	h := newRouterWithService(t, svc, teamID)

	body, contentType := buildMultipart(t, []struct{ name, content string }{
		{"big.txt", strings.Repeat("x", 400)},
	})
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+server.ID.String()+"/files/upload?root=volumes&path=", body,
		map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var envelope uploadEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	tooLarge := false
	for _, result := range envelope.Results {
		if result.Entry != nil {
			t.Fatalf("an over-limit upload was committed: %+v", result)
		}
		if result.Error == "too_large" {
			tooLarge = true
		}
	}
	if !tooLarge {
		t.Fatalf("results = %+v, want a too_large result", envelope.Results)
	}
	if client.lastWrite != nil {
		t.Fatal("an over-limit upload reached the agent")
	}
}

func TestUploadSpoolErrorReportsEveryPart(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	t.Setenv(SpoolDirEnv, filepath.Join(blocker, "spool"))

	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := NewService(Config{
		Registry: registry,
		Dial:     func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
	})
	h := newRouterWithService(t, svc, teamID)

	body, contentType := buildMultipart(t, []struct{ name, content string }{
		{"a.txt", "hello"},
		{"b.txt", "world"},
	})
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+server.ID.String()+"/files/upload?root=volumes&path=", body,
		map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var envelope uploadEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if len(envelope.Results) != 2 {
		t.Fatalf("results = %+v, want one result per part", envelope.Results)
	}
	for _, result := range envelope.Results {
		if result.Error == "" {
			t.Fatalf("part %q was not reported as failed: %+v", result.Name, result)
		}
	}
	if client.lastWrite != nil {
		t.Fatal("a part with a staging failure reached the agent")
	}
}

func TestUploadTruncatedNotCommitted(t *testing.T) {
	id := uuid.New()
	body, contentType := buildMultipart(t, []struct{ name, content string }{
		{"a.txt", strings.Repeat("x", 64)},
	})
	truncated := body[:len(body)-40]
	svc := &fakeService{writeResult: WriteResult{Entry: Entry{Name: "a.txt"}}}
	h := newTestRouter(svc)
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/upload?root=volumes&path=", truncated,
		map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if svc.wrote() {
		t.Fatalf("a truncated upload reached the agent: %v", svc.calls)
	}
}

func TestUploadLongerThanReadTimeout(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{writeResult: WriteResult{Entry: Entry{Name: "a.txt"}}}
	h := newTestRouter(svc)

	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	contentType := writer.FormDataContentType()
	go func() {
		part, err := writer.CreateFormFile("file", "a.txt")
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		for i := 0; i < 4; i++ {
			if _, err := part.Write([]byte(strings.Repeat("x", 1024))); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			time.Sleep(80 * time.Millisecond)
		}
		_ = writer.Close()
		_ = pw.Close()
	}()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/servers/"+id.String()+"/files/upload?root=volumes&path=", pr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d (body %s)", resp.StatusCode, body)
	}
	if !svc.wrote() {
		t.Fatal("the slow upload never reached the agent")
	}
}

func TestMkdirCreateRenameMoveCopyDeleteRoutes(t *testing.T) {
	id := uuid.New()
	base := "/v1/servers/" + id.String() + "/files/"
	svc := &fakeService{
		mkdirEntry:  Entry{Name: "d", Kind: "directory"},
		createEntry: Entry{Name: "a"},
		renameEntry: Entry{Name: "b"},
		chmodEntry:  Entry{Name: "a", Mode: 0o600},
		copyEntry:   Entry{Name: "c"},
	}
	h := newTestRouter(svc)

	cases := []struct {
		name   string
		path   string
		body   string
		want   int
		record string
	}{
		{"mkdir", base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusCreated, "mkdir"},
		{"create", base + "create", `{"root":"volumes","path":"a"}`, http.StatusCreated, "create"},
		{"rename", base + "rename", `{"root":"volumes","source":"a","destination":"b"}`, http.StatusOK, "rename"},
		{"move", base + "move", `{"root":"volumes","source":"a","destination":"b"}`, http.StatusOK, "rename"},
		{"copy", base + "copy", `{"root":"volumes","source":"a","destination":"c"}`, http.StatusOK, "copy"},
		{"chmod", base + "chmod", `{"root":"volumes","path":"a","mode":384}`, http.StatusOK, "chmod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doWrite(t, h, http.MethodPost, tc.path, tc.body, nil)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	rec := doWrite(t, h, http.MethodDelete, base+"entry?root=volumes&path=a&recursive=true", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d (body %s)", rec.Code, rec.Body.String())
	}
}

func TestWriteRoutesRBAC(t *testing.T) {
	teamID := uuid.New()
	client := &fakeFileClient{}
	h, server := newRealRouter(t, teams.RoleReadOnly, teamID, client)
	base := "/v1/servers/" + server.ID.String() + "/files/"

	uploadBody, uploadType := buildMultipart(t, []struct{ name, content string }{{"a.txt", "x"}})

	cases := []struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
	}{
		{"put content", http.MethodPut, base + "content?root=volumes&path=a", "x", map[string]string{"If-Match": `"v1"`}},
		{"upload", http.MethodPost, base + "upload?root=volumes&path=", uploadBody, map[string]string{"Content-Type": uploadType}},
		{"mkdir", http.MethodPost, base + "mkdir", `{"root":"volumes","path":"d"}`, nil},
		{"create", http.MethodPost, base + "create", `{"root":"volumes","path":"a"}`, nil},
		{"rename", http.MethodPost, base + "rename", `{"root":"volumes","source":"a","destination":"b"}`, nil},
		{"move", http.MethodPost, base + "move", `{"root":"volumes","source":"a","destination":"b"}`, nil},
		{"copy", http.MethodPost, base + "copy", `{"root":"volumes","source":"a","destination":"b"}`, nil},
		{"chmod", http.MethodPost, base + "chmod", `{"root":"volumes","path":"a","mode":384}`, nil},
		{"delete", http.MethodDelete, base + "entry?root=volumes&path=a", "", nil},
		{"trash restore", http.MethodPost, base + "trash/restore", `{"root":"volumes","id":"0123456789abcdef0123456789abcdef"}`, nil},
		{"trash purge", http.MethodPost, base + "trash/purge", `{"root":"volumes","id":"0123456789abcdef0123456789abcdef"}`, nil},
		{"trash empty", http.MethodPost, base + "trash/empty", `{"root":"volumes"}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doWrite(t, h, tc.method, tc.path, tc.body, tc.headers)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	rec := doWrite(t, h, http.MethodGet, base+"trash?root=volumes", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET trash as read_only = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestWriteErrorStatusMapping(t *testing.T) {
	id := uuid.New()
	base := "/v1/servers/" + id.String() + "/files/"
	cases := []struct {
		name string
		err  error
		path string
		body string
		want int
	}{
		{"forbidden", ErrForbidden, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusForbidden},
		{"not found", ErrNotFound, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusNotFound},
		{"conflict", ErrConflict, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusConflict},
		{"too large", ErrTooLarge, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusRequestEntityTooLarge},
		{"busy", ErrBusy, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusTooManyRequests},
		{"insufficient storage", ErrInsufficientStorage, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusInsufficientStorage},
		{"disabled", ErrDisabled, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusServiceUnavailable},
		{"agent outdated", ErrAgentOutdated, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusNotImplemented},
		{"agent unavailable", ErrAgentUnavailable, base + "mkdir", `{"root":"volumes","path":"d"}`, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestRouter(&fakeService{mkdirErr: tc.err})
			rec := doWrite(t, h, http.MethodPost, tc.path, tc.body, nil)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestChmodRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{chmodEntry: Entry{Name: "a.txt", Kind: "file", Mode: 0o600, Etag: `"c"`}}
	h := newTestRouter(svc)

	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/chmod",
		`{"root":"volumes","path":"a.txt","mode":384}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var body statEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Entry.Mode != 0o600 || body.Entry.Name != "a.txt" {
		t.Fatalf("entry = %+v", body.Entry)
	}
}

func TestChmodRouteRequiresMode(t *testing.T) {
	id := uuid.New()
	base := "/v1/servers/" + id.String() + "/files/chmod"

	svc := &fakeService{}
	h := newTestRouter(svc)
	rec := doWrite(t, h, http.MethodPost, base, `{"root":"volumes","path":"a.txt"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing mode status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.called("chmod") {
		t.Fatal("a missing mode reached the service")
	}

	svc = &fakeService{chmodEntry: Entry{Name: "a.txt", Kind: "file", Mode: 0, Etag: `"c"`}}
	h = newTestRouter(svc)
	rec = doWrite(t, h, http.MethodPost, base, `{"root":"volumes","path":"a.txt","mode":0}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("explicit mode 0 status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !svc.called("chmod") {
		t.Fatal("an explicit mode 0 did not reach the service")
	}
}

func TestChmodRouteErrorMapping(t *testing.T) {
	id := uuid.New()
	base := "/v1/servers/" + id.String() + "/files/chmod"
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"forbidden", ErrForbidden, http.StatusForbidden},
		{"not found", ErrNotFound, http.StatusNotFound},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"unsupported", ErrAgentOutdated, http.StatusNotImplemented},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestRouter(&fakeService{chmodErr: tc.err})
			rec := doWrite(t, h, http.MethodPost, base, `{"root":"volumes","path":"a","mode":384}`, nil)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
