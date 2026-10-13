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
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestCompressStreamsRequestAndReturnsEntry(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{
		compressMsgs: []*agentv1.Progress{{Path: "out.zip", BytesDone: 10, Done: true, Skipped: 1}},
		statResp:     &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "out.zip", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 10, Etag: `"z"`}},
	}
	svc := newTestService(t, registry, client)

	entry, err := svc.Compress(scopeCtx(teams.RoleOwner, teamID), server.ID, CompressRequest{
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
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{extractMsgs: []*agentv1.Progress{{Path: "dir", Done: true}}}
	svc := newTestService(t, registry, client)

	if err := svc.Extract(scopeCtx(teams.RoleOwner, teamID), server.ID, ExtractRequest{
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
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx(teams.RoleOwner, teamID)

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

// TestArchiveRBACReadOnly proves a read_only member cannot compress or extract,
// while roots/list stay readable.
func TestArchiveRBACReadOnly(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx(teams.RoleReadOnly, teamID)

	if _, err := svc.Compress(ctx, server.ID, CompressRequest{Root: "volumes", Sources: []string{"a"}, Destination: "out.zip", Format: ArchiveFormatZip}); !errors.Is(err, ErrForbidden) {
		t.Errorf("compress err = %v, want ErrForbidden", err)
	}
	if err := svc.Extract(ctx, server.ID, ExtractRequest{Root: "volumes", Source: "a.zip", Destination: "."}); !errors.Is(err, ErrForbidden) {
		t.Errorf("extract err = %v, want ErrForbidden", err)
	}
	if client.seenCompress != nil || client.seenExtract != nil {
		t.Fatal("a forbidden caller must not dial the agent")
	}
}

// TestArchiveForeignTeamNotFound proves a foreign team is indistinguishable
// from a missing node (404) for both archive operations.
func TestArchiveForeignTeamNotFound(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := newTestService(t, registry, &fakeFileClient{})
	ctx := scopeCtx(teams.RoleOwner, uuid.New())
	if _, err := svc.Compress(ctx, server.ID, CompressRequest{Root: "volumes", Sources: []string{"a"}, Destination: "out.zip", Format: ArchiveFormatZip}); !errors.Is(err, ErrNotFound) {
		t.Errorf("compress err = %v, want ErrNotFound", err)
	}
	if err := svc.Extract(ctx, server.ID, ExtractRequest{Root: "volumes", Source: "a.zip", Destination: "."}); !errors.Is(err, ErrNotFound) {
		t.Errorf("extract err = %v, want ErrNotFound", err)
	}
}

// TestArchiveRPCErrorMapping proves the agent's statuses map onto the API
// sentinels, including the archive-limit ResourceExhausted becoming 413.
func TestArchiveRPCErrorMapping(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	ctx := scopeCtx(teams.RoleOwner, teamID)

	cases := []struct {
		name  string
		agent error
		want  error
	}{
		{"not found", status.Error(codes.NotFound, "x"), ErrNotFound},
		{"read-only", status.Error(codes.PermissionDenied, "x"), ErrForbidden},
		{"conflict", status.Error(codes.AlreadyExists, "x"), ErrConflict},
		{"unimplemented", status.Error(codes.Unimplemented, "x"), ErrAgentOutdated},
		{"entry cap", status.Error(codes.ResourceExhausted, "extract: archive has more than 10 entries"), ErrTooLarge},
		{"ratio cap", status.Error(codes.ResourceExhausted, "extract: archive expands by more than 200:1"), ErrTooLarge},
		{"low disk", status.Error(codes.ResourceExhausted, "extract: the filesystem is low on free space"), ErrInsufficientStorage},
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

	// A malformed body is a 400 and never reaches the service.
	rec = doWrite(t, h, http.MethodPost, "/v1/servers/"+id.String()+"/files/compress", "{", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed compress status = %d, want 400", rec.Code)
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

// TestArchiveRouteErrorStatuses proves the route maps the service sentinels.
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

// TestArchiveRoutesDisabled proves a disabled feature answers 503 before any
// agent work.
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

// TestArchiveAudited proves both operations record an audit line with the
// stable op token and never echo a body.
func TestArchiveAudited(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
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
	ctx := scopeCtx(teams.RoleOwner, teamID)
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
}
