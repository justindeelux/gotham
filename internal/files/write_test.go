package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func writeEntry() *agentv1.WriteResponse {
	return &agentv1.WriteResponse{
		Entry:     &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2, Mode: 0o644, Etag: `"v2"`},
		Created:   false,
		WriteMode: "rename",
	}
}

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
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)

	result, err := svc.Write(scopeCtx("", projectID), server.ID, WriteRequest{
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

func TestWriteShortBodyNotCommitted(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)

	_, err := svc.Write(scopeCtx("", projectID), server.ID, WriteRequest{
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

func TestWriteBodyErrorCancelsStream(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)

	_, err := svc.Write(scopeCtx("", projectID), server.ID, WriteRequest{
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

type rejectingFileServer struct {
	agentv1.UnimplementedFileServiceServer
	err error
}

func (s rejectingFileServer) Write(grpc.ClientStreamingServer[agentv1.WriteRequest, agentv1.WriteResponse]) error {
	return s.err
}

func startRejectingAgent(t *testing.T, reject error) agentv1.FileServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	agentv1.RegisterFileServiceServer(srv, rejectingFileServer{err: reject})
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		<-serveErr
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		<-serveErr
	})
	return agentv1.NewFileServiceClient(conn)
}

func TestWriteHeaderRejectionMapsAgentStatus(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	t.Setenv(FeatureEnv, "true")

	cases := []struct {
		name  string
		agent error
		want  error
	}{
		{"read-only root", status.Error(codes.PermissionDenied, "write: root is read-only"), ErrForbidden},
		{"invalid header", status.Error(codes.InvalidArgument, "write: invalid path"), ErrValidation},
		{"over the node limit", status.Error(codes.ResourceExhausted, "write: 500 bytes exceeds the 100 byte upload limit"), ErrTooLarge},
		{"low disk", status.Error(codes.ResourceExhausted, "write: the filesystem is low on free space"), ErrInsufficientStorage},
		{"transfer slots full", status.Error(codes.ResourceExhausted, "file service is busy: too many concurrent transfers"), ErrBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := startRejectingAgent(t, tc.agent)
			svc := NewService(Config{
				Registry: registry,
				Dial:     func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
			})
			const size = 4 << 20
			body := strings.NewReader(strings.Repeat("x", size))
			_, err := svc.Write(scopeCtx("", projectID), server.ID, WriteRequest{
				Root: "volumes", Path: "a.txt", Size: size,
			}, body)
			if err == nil {
				t.Fatal("a header-time rejection must fail the write")
			}
			if errors.Is(err, io.EOF) {
				t.Fatalf("the raw io.EOF leaked instead of the agent status: %v", err)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWriteExceedsUploadCeiling(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{}
	t.Setenv(FeatureEnv, "true")
	svc := NewService(Config{
		Registry:       registry,
		Dial:           func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		MaxUploadBytes: 4,
	})

	_, err := svc.Write(scopeCtx("", projectID), server.ID, WriteRequest{
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
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx("", projectID)

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

func TestWriteRejectsBadEtagAndParentSegment(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{writeResp: writeEntry()}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx("", projectID)

	cases := map[string]WriteRequest{
		"invalid UTF-8 etag":  {Root: "volumes", Path: "a.txt", ExpectEtag: "\xff\xfe", Size: 0},
		"parent segment":      {Root: "volumes", Path: "a/../b", Size: 0},
		"parent segment root": {Root: "..", Path: "a", Size: 0},
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

func TestMkdirRenameCopyDelete(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{
		mkdirResp:  &agentv1.MkdirResponse{Entry: &agentv1.FileEntry{Name: "d", Kind: agentv1.FileKind_FILE_KIND_DIRECTORY, Mode: 0o755}},
		renameResp: &agentv1.RenameResponse{Entry: &agentv1.FileEntry{Name: "b", Etag: `"r"`}},
		copyMsgs:   []*agentv1.Progress{{Path: "b", BytesDone: 3, Done: true}},
		statResp:   &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "b", Etag: `"c"`}},
		deleteMsgs: []*agentv1.Progress{{Path: "a", Done: true}},
	}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx("", projectID)

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

	if err := svc.Delete(ctx, server.ID, "volumes", "a", true, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !client.seenDelete.GetRecursive() {
		t.Fatalf("delete request = %+v", client.seenDelete)
	}
}

func TestWriteRPCErrorMapping(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	ctx := scopeCtx("", projectID)

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
			chmodErr:  status.Error(tc.code, "x"),
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
		if _, err := svc.Chmod(ctx, server.ID, "volumes", "a", 0o600); !errors.Is(err, tc.want) {
			t.Errorf("chmod %s = %v, want %v", tc.code, err, tc.want)
		}
		if _, err := svc.Copy(ctx, server.ID, "volumes", "a", "b", false); !errors.Is(err, tc.want) {
			t.Errorf("copy %s = %v, want %v", tc.code, err, tc.want)
		}
		if err := svc.Delete(ctx, server.ID, "volumes", "a", false, false); !errors.Is(err, tc.want) {
			t.Errorf("delete %s = %v, want %v", tc.code, err, tc.want)
		}
	}
}

func TestAuditLogsActorAndNeverBody(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
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
	ctx := context.Background()

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

func TestChmod(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{
			Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Mode: 0o600, Etag: `"c"`,
		}},
	}
	svc := newTestService(t, registry, client)

	entry, err := svc.Chmod(scopeCtx("", projectID), server.ID, "volumes", "a.txt", 0o600)
	if err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if client.seenChmod.GetMode() != 0o600 || client.seenChmod.GetPath() != "a.txt" || client.seenChmod.GetRoot() != "volumes" {
		t.Fatalf("chmod request = %+v", client.seenChmod)
	}
	if entry.Mode != 0o600 || entry.Etag != `"c"` {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestChmodRejectsModeBitsBeforeDial(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx("", projectID)

	for _, mode := range []uint32{0o4755, 0o2755, 0o1755, 0o7777, 0o10000} {
		if _, err := svc.Chmod(ctx, server.ID, "volumes", "a", mode); !errors.Is(err, ErrValidation) {
			t.Errorf("Chmod(%#o) = %v, want ErrValidation", mode, err)
		}
	}
	if client.seenChmod != nil {
		t.Fatal("an invalid mode reached the agent")
	}
}
