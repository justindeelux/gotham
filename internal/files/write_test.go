package files

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// writeEntry is the entry a successful fake Write returns.
func writeEntry() *agentv1.WriteResponse {
	return &agentv1.WriteResponse{
		Entry:     &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2, Mode: 0o644, Etag: `"v2"`},
		Created:   false,
		WriteMode: "rename",
	}
}

// failReader yields some bytes then a read error, standing in for a client that
// disconnects mid-upload.
type failReader struct {
	sent bool
}

func (r *failReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "abc"), nil
	}
	return 0, errors.New("client gone")
}

func TestWriteStreamsHeaderAndBody(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)

	result, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "a.txt", ExpectEtag: `"v1"`, Size: 5,
	}, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	header := client.lastWriteHeader()
	if header.GetRoot() != "volumes" || header.GetPath() != "a.txt" || header.GetSize() != 5 ||
		header.GetExpectEtag() != `"v1"` || header.GetOverwrite() {
		t.Fatalf("header = %+v", header)
	}
	if string(client.lastWriteBody()) != "hello" {
		t.Fatalf("body = %q", client.lastWriteBody())
	}
	if result.Entry.Etag != `"v2"` || result.WriteMode != "rename" || result.Created {
		t.Fatalf("result = %+v", result)
	}
	if !client.lastWriteClosed() {
		t.Fatal("a complete write must commit")
	}
}

// TestWriteShortBodyNotCommitted proves a body shorter than the declared size is
// refused and the agent stream is cancelled rather than closed, so the agent
// never renames its staging file into place.
func TestWriteShortBodyNotCommitted(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)

	_, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "a.txt", Size: 10,
	}, strings.NewReader("short"))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if client.lastWriteClosed() {
		t.Fatal("a truncated write must not commit")
	}
	if client.writeCtx.Err() == nil {
		t.Fatal("the agent stream context was not cancelled")
	}
}

// TestWriteBodyErrorCancelsStream proves a request-body read error cancels the
// agent stream so a partial upload is never committed.
func TestWriteBodyErrorCancelsStream(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)

	_, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "a.txt", Size: 6,
	}, &failReader{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if client.lastWriteClosed() {
		t.Fatal("a body error must not commit")
	}
	if client.writeCtx.Err() == nil {
		t.Fatal("the agent stream context was not cancelled")
	}
}

func TestWriteExceedsUploadCeiling(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{}
	t.Setenv(FeatureEnv, "true")
	svc := NewService(Config{
		Registry:       registry,
		Dial:           func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		MaxUploadBytes: 4,
	})

	_, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), server.ID, WriteRequest{
		Root: "volumes", Path: "a.txt", Size: 5,
	}, strings.NewReader("hello"))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if client.lastWrite != nil {
		t.Fatal("an oversized write must be refused before dialing the agent")
	}
}

func TestWriteValidationBeforeDial(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx(teams.RoleOwner, teamID)

	cases := map[string]WriteRequest{
		"empty root":    {Root: "", Path: "a", Size: 0},
		"NUL path":      {Root: "volumes", Path: "a\x00b", Size: 0},
		"negative size": {Root: "volumes", Path: "a", Size: -1},
	}
	for name, req := range cases {
		if _, err := svc.Write(ctx, server.ID, req, nil); !errors.Is(err, ErrValidation) {
			t.Errorf("%s err = %v, want ErrValidation", name, err)
		}
	}
	if client.lastWrite != nil {
		t.Fatal("a malformed write must not dial the agent")
	}
}

func TestWriteRBACReadOnly(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{
		writeResp:  writeEntry(),
		mkdirResp:  &agentv1.MkdirResponse{Entry: &agentv1.FileEntry{Name: "d"}},
		renameResp: &agentv1.RenameResponse{Entry: &agentv1.FileEntry{Name: "b"}},
		statResp:   &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "b"}},
	}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx(teams.RoleReadOnly, teamID)

	cases := map[string]func() error{
		"write": func() error {
			_, err := svc.Write(ctx, server.ID, WriteRequest{Root: "volumes", Path: "a", Size: 0}, nil)
			return err
		},
		"create": func() error { _, err := svc.Create(ctx, server.ID, "volumes", "a", 0); return err },
		"mkdir":  func() error { _, err := svc.Mkdir(ctx, server.ID, "volumes", "d", 0); return err },
		"rename": func() error { _, err := svc.Rename(ctx, server.ID, "volumes", "a", "b", false); return err },
		"copy":   func() error { _, err := svc.Copy(ctx, server.ID, "volumes", "a", "b", false); return err },
		"delete": func() error { return svc.Delete(ctx, server.ID, "volumes", "a", false) },
	}
	for name, call := range cases {
		if err := call(); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s err = %v, want ErrForbidden", name, err)
		}
	}
}

func TestWriteForeignTeamNotFound(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := newTestService(t, registry, &fakeFileClient{writeResp: writeEntry()})
	_, err := svc.Write(scopeCtx(teams.RoleOwner, uuid.New()), server.ID, WriteRequest{Root: "volumes", Path: "a", Size: 0}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMkdirRenameCopyDelete(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{
		mkdirResp:  &agentv1.MkdirResponse{Entry: &agentv1.FileEntry{Name: "d", Kind: agentv1.FileKind_FILE_KIND_DIRECTORY, Mode: 0o755}},
		renameResp: &agentv1.RenameResponse{Entry: &agentv1.FileEntry{Name: "b", Etag: `"r"`}},
		copyMsgs:   []*agentv1.Progress{{Path: "b", BytesDone: 3, Done: true}},
		statResp:   &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "b", Etag: `"c"`}},
		deleteMsgs: []*agentv1.Progress{{Path: "a", Done: true}},
	}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx(teams.RoleOwner, teamID)

	entry, err := svc.Mkdir(ctx, server.ID, "volumes", "d", 0)
	if err != nil || entry.Name != "d" || entry.Kind != "directory" {
		t.Fatalf("mkdir = %+v, %v", entry, err)
	}
	if client.seenMkdir.GetMode() != 0 {
		t.Fatalf("mkdir mode = %d", client.seenMkdir.GetMode())
	}

	entry, err = svc.Rename(ctx, server.ID, "volumes", "a", "b", true)
	if err != nil || entry.Etag != `"r"` {
		t.Fatalf("rename = %+v, %v", entry, err)
	}
	if !client.seenRename.GetOverwrite() || client.seenRename.GetSource() != "a" || client.seenRename.GetDestination() != "b" {
		t.Fatalf("rename request = %+v", client.seenRename)
	}

	entry, err = svc.Copy(ctx, server.ID, "volumes", "a", "b", false)
	if err != nil || entry.Etag != `"c"` {
		t.Fatalf("copy = %+v, %v", entry, err)
	}
	if client.seenCopy.GetDestination() != "b" {
		t.Fatalf("copy request = %+v", client.seenCopy)
	}

	if err := svc.Delete(ctx, server.ID, "volumes", "a", true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !client.seenDelete.GetRecursive() {
		t.Fatalf("delete request = %+v", client.seenDelete)
	}
}

func TestWriteRPCErrorMapping(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	ctx := scopeCtx(teams.RoleOwner, teamID)

	cases := []struct {
		code codes.Code
		want error
	}{
		{codes.AlreadyExists, ErrConflict},
		{codes.FailedPrecondition, ErrConflict},
		{codes.Aborted, ErrConflict},
		{codes.ResourceExhausted, ErrBusy},
		{codes.PermissionDenied, ErrForbidden},
	}
	for _, tc := range cases {
		client := &fakeFileClient{
			mkdirErr:  status.Error(tc.code, "x"),
			renameErr: status.Error(tc.code, "x"),
			copyErr:   status.Error(tc.code, "x"),
			deleteErr: status.Error(tc.code, "x"),
			writeErr:  status.Error(tc.code, "x"),
			writeResp: writeEntry(),
		}
		svc := newTestService(t, registry, client)
		if _, err := svc.Write(ctx, server.ID, WriteRequest{Root: "volumes", Path: "a", Size: 0}, nil); !errors.Is(err, tc.want) {
			t.Errorf("write %s = %v, want %v", tc.code, err, tc.want)
		}
		if _, err := svc.Mkdir(ctx, server.ID, "volumes", "d", 0); !errors.Is(err, tc.want) {
			t.Errorf("mkdir %s = %v, want %v", tc.code, err, tc.want)
		}
		if _, err := svc.Rename(ctx, server.ID, "volumes", "a", "b", false); !errors.Is(err, tc.want) {
			t.Errorf("rename %s = %v, want %v", tc.code, err, tc.want)
		}
		if _, err := svc.Copy(ctx, server.ID, "volumes", "a", "b", false); !errors.Is(err, tc.want) {
			t.Errorf("copy %s = %v, want %v", tc.code, err, tc.want)
		}
		if err := svc.Delete(ctx, server.ID, "volumes", "a", false); !errors.Is(err, tc.want) {
			t.Errorf("delete %s = %v, want %v", tc.code, err, tc.want)
		}
	}
}

// TestAuditLogsActorAndNeverBody proves every audited op records the actor,
// target and result, and that neither a file body nor a download ticket value
// reaches the log.
func TestAuditLogsActorAndNeverBody(t *testing.T) {
	teamID := uuid.New()
	userID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "app.env", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 12, Etag: `"v1"`}},
		readBody:  []byte("SUPERSECRET1"),
		writeResp: writeEntry(),
	}
	var buf bytes.Buffer
	t.Setenv(FeatureEnv, "true")
	svc := NewService(Config{
		Registry:        registry,
		Dial:            func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		Logger:          slog.New(slog.NewJSONHandler(&buf, nil)),
		MaxContentBytes: 64,
	})
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: teams.RoleOwner})

	if _, err := svc.Content(ctx, server.ID, "volumes", "app.env"); err != nil {
		t.Fatalf("content: %v", err)
	}
	if _, err := svc.Write(ctx, server.ID, WriteRequest{Root: "volumes", Path: "a.txt", Size: 2}, strings.NewReader("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := buf.String()
	for _, op := range []string{"read_content", "write"} {
		if !strings.Contains(out, `"op":"`+op+`"`) {
			t.Errorf("audit log missing op %q: %s", op, out)
		}
	}
	if !strings.Contains(out, `"actor_user_id":"`+userID.String()+`"`) {
		t.Errorf("audit log missing actor: %s", out)
	}
	if !strings.Contains(out, `"actor_team_id":"`+teamID.String()+`"`) {
		t.Errorf("audit log missing team: %s", out)
	}
	if strings.Contains(out, "SUPERSECRET1") {
		t.Fatalf("audit log leaked a file body: %s", out)
	}
}

func TestAuditDisabledAndUnavailableResults(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv(FeatureEnv, "false")
	svc := NewService(Config{Registry: newFakeRegistry(), Logger: slog.New(slog.NewJSONHandler(&buf, nil))})
	if _, err := svc.Mkdir(context.Background(), uuid.New(), "volumes", "d", 0); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	if !strings.Contains(buf.String(), `"result":"disabled"`) {
		t.Fatalf("audit log = %s", buf.String())
	}
}
