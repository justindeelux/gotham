package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestCompressStreamsRequestAndReturnsEntry(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{
		compressMsgs: []*agentv1.Progress{{Path: "out.zip", BytesDone: 10, Done: true, Skipped: 1}},
		statResp:     &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "out.zip", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 10, Etag: `"z"`}},
	}
	svc := newTestService(t, registry, client)

	entry, err := svc.Compress(scopeCtx("", projectID), server.ID, CompressRequest{
		Root: "volumes", Sources: []string{"a", "dir"}, Destination: "out.zip", Format: ArchiveFormatZip,
	})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if entry.Etag != `"z"` || entry.Kind != "file" {
		t.Fatalf("entry = %+v", entry)
	}
	req := client.seenCompress
	if req.GetRoot() != "volumes" || req.GetDestination() != "out.zip" ||
		req.GetFormat() != agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP ||
		len(req.GetSources()) != 2 || req.GetOverwrite() {
		t.Fatalf("compress request = %+v", req)
	}
}

func TestExtractStreamsRequest(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{extractMsgs: []*agentv1.Progress{{Path: "dir", Done: true}}}
	svc := newTestService(t, registry, client)

	if err := svc.Extract(scopeCtx("", projectID), server.ID, ExtractRequest{
		Root: "volumes", Source: "a.zip", Destination: "sub", Overwrite: true,
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	req := client.seenExtract
	if req.GetRoot() != "volumes" || req.GetSource() != "a.zip" || req.GetDestination() != "sub" || !req.GetOverwrite() {
		t.Fatalf("extract request = %+v", req)
	}
}

func TestArchiveValidationBeforeDial(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx("", projectID)

	tooMany := make([]string, maxArchiveSources+1)
	for i := range tooMany {
		tooMany[i] = "a"
	}
	cases := map[string]CompressRequest{
		"empty root":        {Root: "", Destination: "out.zip", Format: ArchiveFormatZip, Sources: []string{"a"}},
		"missing dest":      {Root: "volumes", Destination: "", Format: ArchiveFormatZip, Sources: []string{"a"}},
		"parent dest":       {Root: "volumes", Destination: "../out.zip", Format: ArchiveFormatZip, Sources: []string{"a"}},
		"bad format":        {Root: "volumes", Destination: "out.rar", Format: "rar", Sources: []string{"a"}},
		"no sources":        {Root: "volumes", Destination: "out.zip", Format: ArchiveFormatZip},
		"parent source":     {Root: "volumes", Destination: "out.zip", Format: ArchiveFormatZip, Sources: []string{"a/../b"}},
		"too many sources":  {Root: "volumes", Destination: "out.zip", Format: ArchiveFormatZip, Sources: tooMany},
		"invalid UTF8 root": {Root: "vol\xff", Destination: "out.zip", Format: ArchiveFormatZip, Sources: []string{"a"}},
	}
	for name, req := range cases {
		if _, err := svc.Compress(ctx, server.ID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s err = %v, want ErrValidation", name, err)
		}
	}
	if client.seenCompress != nil {
		t.Fatal("a malformed compress must not dial the agent")
	}

	if err := svc.Extract(ctx, server.ID, ExtractRequest{Root: "volumes", Source: "../a.zip", Destination: "."}); !errors.Is(err, ErrValidation) {
		t.Errorf("extract parent source err = %v, want ErrValidation", err)
	}
	if client.seenExtract != nil {
		t.Fatal("a malformed extract must not dial the agent")
	}
}

func TestArchiveRPCErrorMapping(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	ctx := scopeCtx("", projectID)

	cases := []struct {
		name  string
		agent error
		want  error
	}{
		{"not found", status.Error(codes.NotFound, "x"), ErrNotFound},
		{"read-only", status.Error(codes.PermissionDenied, "x"), ErrForbidden},
		{"conflict", status.Error(codes.AlreadyExists, "x"), ErrConflict},
		{"unimplemented", status.Error(codes.Unimplemented, "x"), ErrAgentOutdated},
		{"entry cap", status.Error(codes.ResourceExhausted, "extract: archive has more than 10 entries"), ErrArchiveLimit},
		{"ratio cap", status.Error(codes.ResourceExhausted, "extract: archive expands by more than 200:1"), ErrArchiveLimit},
		{"depth cap", status.Error(codes.ResourceExhausted, "extract: fsroot: directory too large: archive entry nests too deeply (65 segments)"), ErrArchiveLimit},
		{"input cap", status.Error(codes.ResourceExhausted, "extract: archive is larger than 1073741824 bytes"), ErrArchiveLimit},
		{"low disk", status.Error(codes.ResourceExhausted, "extract: the filesystem is low on free space"), ErrInsufficientStorage},
		{"no space in an archive path", status.Error(codes.ResourceExhausted, "extract: write archive/x: no space left on device"), ErrInsufficientStorage},
		{"refused entry", status.Error(codes.FailedPrecondition, `extract: archive entry "x" is a link`), ErrArchiveEntry},
		{"truncated body", status.Error(codes.InvalidArgument, `extract: archive entry is truncated or corrupt: unexpected EOF`), ErrInvalidArchive},
		{"corrupt zip", status.Error(codes.InvalidArgument, "extract: reading zip: zip: not a valid zip file"), ErrInvalidArchive},
		{"invalid destination is not an archive error", status.Error(codes.InvalidArgument, "extract: invalid destination \"../x\""), ErrValidation},
		{"busy", status.Error(codes.ResourceExhausted, "file service is busy: too many concurrent transfers"), ErrBusy},
	}
	for _, tc := range cases {
		client := &fakeFileClient{compressErr: tc.agent, extractErr: tc.agent}
		svc := newTestService(t, registry, client)
		if _, err := svc.Compress(ctx, server.ID, CompressRequest{Root: "volumes", Sources: []string{"a"}, Destination: "out.zip", Format: ArchiveFormatZip}); !errors.Is(err, tc.want) {
			t.Errorf("%s compress err = %v, want %v", tc.name, err, tc.want)
		}
		if err := svc.Extract(ctx, server.ID, ExtractRequest{Root: "volumes", Source: "a.zip", Destination: "."}); !errors.Is(err, tc.want) {
			t.Errorf("%s extract err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestCompressRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{compressEntry: Entry{Name: "out.zip", Kind: "file", Size: 10, Etag: `"z"`}}
	h := newTestRouter(svc)

	body := `{"root":"volumes","sources":["a","b"],"destination":"out.zip","format":"tar.gz","overwrite":true}`
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/compress", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("compress status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var stat statEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &stat); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stat.Entry.Name != "out.zip" {
		t.Fatalf("entry = %+v", stat.Entry)
	}
	if svc.seenCompress == nil || svc.seenCompress.Format != "tar.gz" || !svc.seenCompress.Overwrite || len(svc.seenCompress.Sources) != 2 {
		t.Fatalf("seen compress = %+v", svc.seenCompress)
	}

	rec = doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/compress", "{", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed compress status = %d, want 400", rec.Code)
	}
}

func TestIsInvalidArchiveError(t *testing.T) {
	t.Parallel()
	archive := []string{
		`extract: archive entry is truncated or corrupt: unexpected EOF`,
		`extract: reading zip: zip: not a valid zip file`,
		`extract: reading gzip: gzip: invalid header`,
		`extract: reading tar: unexpected EOF`,
		`extract: opening archive entry "x": unexpected EOF`,
		`extract: "a.rar" is not a .zip, .tar or .tar.gz archive`,
	}
	for _, message := range archive {
		if !isInvalidArchiveError(message) {
			t.Errorf("isInvalidArchiveError(%q) = false, want true", message)
		}
	}
	notArchive := []string{
		`extract: invalid destination "../x"`,
		`compress: invalid source "../x"`,
		`compress: destination "a.zip" is inside source "a"`,
		`compress: sources "a" and "b" overlap`,
	}
	for _, message := range notArchive {
		if isInvalidArchiveError(message) {
			t.Errorf("isInvalidArchiveError(%q) = true, want false", message)
		}
	}
}

func TestExtractRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{}
	h := newTestRouter(svc)

	body := `{"root":"volumes","source":"a.zip","destination":"sub","overwrite":false}`
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/extract", body, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("extract status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if svc.seenExtract == nil || svc.seenExtract.Source != "a.zip" || svc.seenExtract.Destination != "sub" {
		t.Fatalf("seen extract = %+v", svc.seenExtract)
	}
}

func TestArchiveRouteErrorStatuses(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"validation", ErrValidation, http.StatusBadRequest},
		{"forbidden", ErrForbidden, http.StatusForbidden},
		{"conflict", ErrConflict, http.StatusConflict},
		{"too large", ErrTooLarge, http.StatusRequestEntityTooLarge},
		{"archive limit", ErrArchiveLimit, http.StatusRequestEntityTooLarge},
		{"archive entry", ErrArchiveEntry, http.StatusConflict},
		{"invalid archive", ErrInvalidArchive, http.StatusBadRequest},
		{"busy", ErrBusy, http.StatusTooManyRequests},
		{"outdated", ErrAgentOutdated, http.StatusNotImplemented},
		{"unavailable", ErrAgentUnavailable, http.StatusBadGateway},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		svc := &fakeService{compressErr: tc.err, extractErr: tc.err}
		h := newTestRouter(svc)
		rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/compress", `{"root":"volumes","sources":["a"],"destination":"out.zip","format":"zip"}`, nil)
		if rec.Code != tc.want {
			t.Errorf("%s compress status = %d, want %d", tc.name, rec.Code, tc.want)
		}
		rec = doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/extract", `{"root":"volumes","source":"a.zip","destination":"."}`, nil)
		if rec.Code != tc.want {
			t.Errorf("%s extract status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}

func TestArchiveRouteErrorCodes(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrArchiveLimit, http.StatusRequestEntityTooLarge, "archive_limit"},
		{ErrArchiveEntry, http.StatusConflict, "archive_entry"},
		{ErrInvalidArchive, http.StatusBadRequest, "invalid_archive"},
	}
	for _, tc := range cases {
		svc := &fakeService{compressErr: tc.err, extractErr: tc.err}
		h := newTestRouter(svc)
		rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/compress", `{"root":"volumes","sources":["a"],"destination":"out.zip","format":"zip"}`, nil)
		if rec.Code != tc.status {
			t.Fatalf("compress status = %d, want %d", rec.Code, tc.status)
		}
		var body errorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Code != tc.code {
			t.Errorf("error code = %q, want %q", body.Code, tc.code)
		}
	}
}

func TestArchiveRoutesDisabled(t *testing.T) {
	id := uuid.New()
	t.Setenv(FeatureEnv, "false")
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := NewService(Config{Registry: registry})
	h := newTestRouter(svc)
	_ = server
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/compress", `{"root":"volumes","sources":["a"],"destination":"out.zip","format":"zip"}`, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled compress status = %d, want 503", rec.Code)
	}
}

func TestArchiveAudited(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{
		compressMsgs: []*agentv1.Progress{{Done: true}},
		statResp:     &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "out.zip"}},
	}
	var buf bytes.Buffer
	t.Setenv(FeatureEnv, "true")
	svc := NewService(Config{
		Registry: registry,
		Dial:     func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		Logger:   slog.New(slog.NewJSONHandler(&buf, nil)),
	})
	ctx := scopeCtx("", projectID)
	if _, err := svc.Compress(ctx, server.ID, CompressRequest{Root: "volumes", Sources: []string{"a"}, Destination: "out.zip", Format: ArchiveFormatZip}); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := svc.Extract(ctx, server.ID, ExtractRequest{Root: "volumes", Source: "a.zip", Destination: "."}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	out := buf.String()
	for _, op := range []string{`"op":"compress"`, `"op":"extract"`} {
		if !strings.Contains(out, op) {
			t.Errorf("audit log missing %s: %s", op, out)
		}
	}
	for _, extra := range []string{`"format":"zip"`, `"overwrite":false`, `"destination":"."`, `"sources":1`} {
		if !strings.Contains(out, extra) {
			t.Errorf("audit log missing %s: %s", extra, out)
		}
	}
}
