package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeRegistry is an in-memory Registry.
type fakeRegistry struct {
	mu     sync.Mutex
	items  map[uuid.UUID]*servers.Server
	getErr error
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{items: make(map[uuid.UUID]*servers.Server)}
}

func (f *fakeRegistry) seed(teamID uuid.UUID) *servers.Server {
	server := &servers.Server{ID: uuid.New(), Name: "node-1", IP: "10.0.0.5", Port: 22, TeamID: teamID, Status: servers.StatusReady}
	f.mu.Lock()
	f.items[server.ID] = server
	f.mu.Unlock()
	return server
}

func (f *fakeRegistry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	server, ok := f.items[id]
	if !ok {
		return nil, servers.ErrNotFound
	}
	return server, nil
}

// fakeReadStream satisfies grpc.ServerStreamingClient[ReadChunk] with a scripted
// sequence of chunks and a terminal error.
type fakeReadStream struct {
	grpc.ClientStream
	chunks [][]byte
	index  int
	err    error
}

func (s *fakeReadStream) Recv() (*agentv1.ReadChunk, error) {
	if s.index >= len(s.chunks) {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	data := s.chunks[s.index]
	s.index++
	return &agentv1.ReadChunk{Data: data}, nil
}

// fakeFileClient is a scriptable FileClient.
type fakeFileClient struct {
	mu sync.Mutex

	rootsResp  *agentv1.ListRootsResponse
	rootsErr   error
	listResp   *agentv1.ListResponse
	listErr    error
	statResp   *agentv1.StatResponse
	statErr    error
	searchResp *agentv1.SearchResponse
	searchErr  error
	seenSearch *agentv1.SearchRequest
	readBody   []byte
	readErr    error
	closes     int
	seenList   *agentv1.ListRequest

	sizeResp *agentv1.DirectorySizeResponse
	sizeErr  error
	seenSize *agentv1.DirectorySizeRequest

	writeResp  *agentv1.WriteResponse
	writeErr   error
	lastWrite  *fakeWriteStream
	writeCtx   context.Context
	mkdirResp  *agentv1.MkdirResponse
	mkdirErr   error
	renameResp *agentv1.RenameResponse
	renameErr  error
	chmodErr   error
	copyMsgs   []*agentv1.Progress
	copyErr    error
	deleteMsgs []*agentv1.Progress
	deleteErr  error
	seenRename *agentv1.RenameRequest
	seenCopy   *agentv1.CopyRequest
	seenDelete *agentv1.DeleteRequest
	seenMkdir  *agentv1.MkdirRequest
	seenChmod  *agentv1.ChmodRequest

	compressMsgs []*agentv1.Progress
	compressErr  error
	extractMsgs  []*agentv1.Progress
	extractErr   error
	seenCompress *agentv1.CompressRequest
	seenExtract  *agentv1.ExtractRequest

	trashResp      *agentv1.ListTrashResponse
	trashErr       error
	restoreResp    *agentv1.RestoreResponse
	restoreErr     error
	purgeErr       error
	emptyTrashResp *agentv1.EmptyTrashResponse
	emptyTrashErr  error
	seenRestore    *agentv1.RestoreRequest
	seenPurge      *agentv1.PurgeRequest
	seenEmpty      *agentv1.EmptyTrashRequest
}

func (f *fakeFileClient) ListRoots(context.Context, *agentv1.ListRootsRequest, ...grpc.CallOption) (*agentv1.ListRootsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rootsErr != nil {
		return nil, f.rootsErr
	}
	if f.rootsResp != nil {
		return f.rootsResp, nil
	}
	return &agentv1.ListRootsResponse{}, nil
}

func (f *fakeFileClient) List(_ context.Context, in *agentv1.ListRequest, _ ...grpc.CallOption) (*agentv1.ListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenList = in
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &agentv1.ListResponse{}, nil
}

func (f *fakeFileClient) Stat(context.Context, *agentv1.StatRequest, ...grpc.CallOption) (*agentv1.StatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statResp != nil {
		return f.statResp, nil
	}
	return &agentv1.StatResponse{}, nil
}

func (f *fakeFileClient) DirectorySize(_ context.Context, in *agentv1.DirectorySizeRequest, _ ...grpc.CallOption) (*agentv1.DirectorySizeResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenSize = in
	if f.sizeErr != nil {
		return nil, f.sizeErr
	}
	if f.sizeResp != nil {
		return f.sizeResp, nil
	}
	return &agentv1.DirectorySizeResponse{}, nil
}

func (f *fakeFileClient) Search(_ context.Context, in *agentv1.SearchRequest, _ ...grpc.CallOption) (*agentv1.SearchResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenSearch = in
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	if f.searchResp != nil {
		return f.searchResp, nil
	}
	return &agentv1.SearchResponse{}, nil
}

func (f *fakeFileClient) Read(context.Context, *agentv1.ReadRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ReadChunk], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	return &fakeReadStream{chunks: [][]byte{f.readBody}}, nil
}

func (f *fakeFileClient) Write(ctx context.Context, _ ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.WriteRequest, agentv1.WriteResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCtx = ctx
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	stream := &fakeWriteStream{resp: f.writeResp}
	f.lastWrite = stream
	return stream, nil
}

func (f *fakeFileClient) Mkdir(_ context.Context, in *agentv1.MkdirRequest, _ ...grpc.CallOption) (*agentv1.MkdirResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenMkdir = in
	if f.mkdirErr != nil {
		return nil, f.mkdirErr
	}
	if f.mkdirResp != nil {
		return f.mkdirResp, nil
	}
	return &agentv1.MkdirResponse{}, nil
}

func (f *fakeFileClient) Rename(_ context.Context, in *agentv1.RenameRequest, _ ...grpc.CallOption) (*agentv1.RenameResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenRename = in
	if f.renameErr != nil {
		return nil, f.renameErr
	}
	if f.renameResp != nil {
		return f.renameResp, nil
	}
	return &agentv1.RenameResponse{}, nil
}

func (f *fakeFileClient) Chmod(_ context.Context, in *agentv1.ChmodRequest, _ ...grpc.CallOption) (*agentv1.ChmodResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenChmod = in
	if f.chmodErr != nil {
		return nil, f.chmodErr
	}
	return &agentv1.ChmodResponse{}, nil
}

func (f *fakeFileClient) Copy(_ context.Context, in *agentv1.CopyRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenCopy = in
	if f.copyErr != nil {
		return nil, f.copyErr
	}
	return &fakeProgressStream{msgs: f.copyMsgs}, nil
}

func (f *fakeFileClient) Delete(_ context.Context, in *agentv1.DeleteRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenDelete = in
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &fakeProgressStream{msgs: f.deleteMsgs}, nil
}

func (f *fakeFileClient) Compress(_ context.Context, in *agentv1.CompressRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenCompress = in
	if f.compressErr != nil {
		return nil, f.compressErr
	}
	return &fakeProgressStream{msgs: f.compressMsgs}, nil
}

func (f *fakeFileClient) Extract(_ context.Context, in *agentv1.ExtractRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenExtract = in
	if f.extractErr != nil {
		return nil, f.extractErr
	}
	return &fakeProgressStream{msgs: f.extractMsgs}, nil
}

func (f *fakeFileClient) ListTrash(_ context.Context, _ *agentv1.ListTrashRequest, _ ...grpc.CallOption) (*agentv1.ListTrashResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.trashErr != nil {
		return nil, f.trashErr
	}
	if f.trashResp != nil {
		return f.trashResp, nil
	}
	return &agentv1.ListTrashResponse{}, nil
}

func (f *fakeFileClient) Restore(_ context.Context, in *agentv1.RestoreRequest, _ ...grpc.CallOption) (*agentv1.RestoreResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenRestore = in
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.restoreResp != nil {
		return f.restoreResp, nil
	}
	return &agentv1.RestoreResponse{}, nil
}

func (f *fakeFileClient) Purge(_ context.Context, in *agentv1.PurgeRequest, _ ...grpc.CallOption) (*agentv1.PurgeResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenPurge = in
	if f.purgeErr != nil {
		return nil, f.purgeErr
	}
	return &agentv1.PurgeResponse{}, nil
}

func (f *fakeFileClient) EmptyTrash(_ context.Context, in *agentv1.EmptyTrashRequest, _ ...grpc.CallOption) (*agentv1.EmptyTrashResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenEmpty = in
	if f.emptyTrashErr != nil {
		return nil, f.emptyTrashErr
	}
	if f.emptyTrashResp != nil {
		return f.emptyTrashResp, nil
	}
	return &agentv1.EmptyTrashResponse{}, nil
}

// lastWriteHeader returns the header the fake Write stream received.
func (f *fakeFileClient) lastWriteHeader() *agentv1.WriteHeader {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastWrite == nil {
		return nil
	}
	return f.lastWrite.header
}

// lastWriteBody returns the body bytes the fake Write stream received.
func (f *fakeFileClient) lastWriteBody() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastWrite == nil {
		return nil
	}
	return f.lastWrite.body
}

// lastWriteClosed reports whether CloseAndRecv was called (i.e. the stream was
// committed rather than cancelled).
func (f *fakeFileClient) lastWriteClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWrite != nil && f.lastWrite.closed
}

// fakeWriteStream is a scriptable Write client stream.
type fakeWriteStream struct {
	grpc.ClientStream
	mu     sync.Mutex
	header *agentv1.WriteHeader
	body   []byte
	resp   *agentv1.WriteResponse
	closed bool
}

func (s *fakeWriteStream) Send(msg *agentv1.WriteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch part := msg.GetPart().(type) {
	case *agentv1.WriteRequest_Header:
		s.header = part.Header
	case *agentv1.WriteRequest_Data:
		s.body = append(s.body, part.Data...)
	case *agentv1.WriteRequest_Commit:
	}
	return nil
}

func (s *fakeWriteStream) CloseAndRecv() (*agentv1.WriteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.resp != nil {
		return s.resp, nil
	}
	return &agentv1.WriteResponse{}, nil
}

// fakeProgressStream is a scriptable Copy/Delete progress stream.
type fakeProgressStream struct {
	grpc.ClientStream
	msgs  []*agentv1.Progress
	index int
	err   error
}

func (s *fakeProgressStream) Recv() (*agentv1.Progress, error) {
	if s.index >= len(s.msgs) {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	msg := s.msgs[s.index]
	s.index++
	return msg, nil
}

// Close satisfies io.Closer so the service releases the fake like a real client.
func (f *fakeFileClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

// newTestService builds a service wired to client with a small editor ceiling.
func newTestService(t *testing.T, registry Registry, client FileClient) *Service {
	t.Helper()
	t.Setenv(FeatureEnv, "true")
	return NewService(Config{
		Registry:        registry,
		Dial:            func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		MaxContentBytes: 8,
	})
}

// scopeCtx stores an active team scope on the request context.
func scopeCtx(role teams.Role, teamID uuid.UUID) context.Context {
	return teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamID, Role: role})
}

func TestResolveRBAC(t *testing.T) {
	teamID := uuid.New()
	otherTeam := uuid.New()

	t.Run("foreign team is not found", func(t *testing.T) {
		registry := newFakeRegistry()
		server := registry.seed(otherTeam)
		svc := newTestService(t, registry, &fakeFileClient{})
		if _, err := svc.Roots(scopeCtx(teams.RoleOwner, teamID), server.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("team-less legacy server is allowed", func(t *testing.T) {
		registry := newFakeRegistry()
		server := registry.seed(uuid.Nil)
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2}},
			readBody: []byte("hi"),
		})
		ctx := scopeCtx(teams.RoleOwner, teamID)
		if _, err := svc.Roots(ctx, server.ID); err != nil {
			t.Fatalf("roots err = %v, want nil", err)
		}
		if _, err := svc.Content(ctx, server.ID, "root", "a"); err != nil {
			t.Fatalf("content err = %v, want nil", err)
		}
	})

	t.Run("read_only viewer may write on a team-less legacy server", func(t *testing.T) {
		// A team-less node ignores the caller's team role, like the rest of the
		// legacy node surface: platform operators are superusers, so a viewer
		// scope still authorizes a write there (documents the operator semantics).
		registry := newFakeRegistry()
		server := registry.seed(uuid.Nil)
		svc := newTestService(t, registry, &fakeFileClient{})
		if err := svc.AuthorizeWrite(scopeCtx(teams.RoleReadOnly, teamID), server.ID); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})

	t.Run("read_only member reads but not content", func(t *testing.T) {
		registry := newFakeRegistry()
		server := registry.seed(teamID)
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2}},
		})
		ctx := scopeCtx(teams.RoleReadOnly, teamID)
		if _, err := svc.Roots(ctx, server.ID); err != nil {
			t.Fatalf("roots err = %v, want nil", err)
		}
		if _, err := svc.Content(ctx, server.ID, "root", "a"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("content err = %v, want ErrForbidden", err)
		}
		if _, err := svc.IssueDownloadTicket(ctx, server.ID, "root", "a"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("ticket err = %v, want ErrForbidden", err)
		}
	})

	t.Run("owner reads content", func(t *testing.T) {
		registry := newFakeRegistry()
		server := registry.seed(teamID)
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2}},
			readBody: []byte("hi"),
		})
		if _, err := svc.Content(scopeCtx(teams.RoleOwner, teamID), server.ID, "root", "a"); err != nil {
			t.Fatalf("content err = %v, want nil", err)
		}
	})

	t.Run("no scope keeps the pre-teams path", func(t *testing.T) {
		registry := newFakeRegistry()
		server := registry.seed(uuid.Nil)
		svc := newTestService(t, registry, &fakeFileClient{})
		if _, err := svc.Roots(context.Background(), server.ID); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})
}

func TestRootsAndListMapping(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	modified := time.Now().UTC().Truncate(time.Second)
	client := &fakeFileClient{
		rootsResp: &agentv1.ListRootsResponse{Roots: []*agentv1.FileRoot{{Name: "volumes", Path: "/var/lib/gotham/volumes", Writable: true, FreeBytes: 42}}},
		listResp: &agentv1.ListResponse{
			Entries:   []*agentv1.FileEntry{{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 3, Mode: 0o644, Etag: `"e"`, ModifiedAt: timestamppb.New(modified)}},
			Truncated: true,
			Cursor:    "next",
		},
	}
	svc := newTestService(t, registry, client)
	ctx := scopeCtx(teams.RoleOwner, teamID)

	roots, err := svc.Roots(ctx, server.ID)
	if err != nil {
		t.Fatalf("roots err = %v", err)
	}
	if len(roots) != 1 || roots[0].Name != "volumes" || !roots[0].Writable || roots[0].FreeBytes != 42 {
		t.Fatalf("roots = %+v", roots)
	}

	listing, err := svc.List(ctx, server.ID, "volumes", "sub", "cur")
	if err != nil {
		t.Fatalf("list err = %v", err)
	}
	if !listing.Truncated || listing.Cursor != "next" || len(listing.Entries) != 1 {
		t.Fatalf("listing = %+v", listing)
	}
	entry := listing.Entries[0]
	if entry.Name != "a.txt" || entry.Kind != "file" || entry.Mode != 0o644 || entry.Etag != `"e"` || !entry.ModifiedAt.Equal(modified) {
		t.Fatalf("entry = %+v", entry)
	}
	if client.seenList.GetPath() != "sub" || client.seenList.GetCursor() != "cur" {
		t.Fatalf("agent saw %+v", client.seenList)
	}
}

func TestContentLimits(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	ctx := scopeCtx(teams.RoleOwner, teamID)

	t.Run("too large", func(t *testing.T) {
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "big", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 9}},
		})
		if _, err := svc.Content(ctx, server.ID, "root", "big"); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})

	t.Run("binary", func(t *testing.T) {
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "bin", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 3}},
			readBody: []byte{'a', 0x00, 'b'},
		})
		if _, err := svc.Content(ctx, server.ID, "root", "bin"); !errors.Is(err, ErrNotEditable) {
			t.Fatalf("err = %v, want ErrNotEditable", err)
		}
	})

	t.Run("sha256 of the body", func(t *testing.T) {
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "t", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2, Etag: `"v1"`}},
			readBody: []byte("hi"),
		})
		content, err := svc.Content(ctx, server.ID, "root", "t")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		// sha256("hi")
		if content.SHA256 != "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4" {
			t.Fatalf("sha256 = %q", content.SHA256)
		}
		if content.Etag != `"v1"` || content.Content != "hi" {
			t.Fatalf("content = %+v", content)
		}
	})
}

// TestQueryValidation is the fix-round-1 regression: invalid UTF-8 or an
// over-long root/path/cursor must fail as ErrValidation (400) before any dial,
// instead of reaching proto.Marshal and surfacing as a 500.
func TestQueryValidation(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	svc := newTestService(t, registry, &fakeFileClient{})
	ctx := scopeCtx(teams.RoleOwner, teamID)

	invalid := "\xff\xfe"
	nul := "a\x00b"
	long := strings.Repeat("a", maxFileArgBytes+1)

	cases := map[string]func() error{
		"list bad root":    func() error { _, err := svc.List(ctx, server.ID, invalid, "", ""); return err },
		"list bad path":    func() error { _, err := svc.List(ctx, server.ID, "root", invalid, ""); return err },
		"list bad cursor":  func() error { _, err := svc.List(ctx, server.ID, "root", "", invalid); return err },
		"list NUL path":    func() error { _, err := svc.List(ctx, server.ID, "root", nul, ""); return err },
		"list long path":   func() error { _, err := svc.List(ctx, server.ID, "root", long, ""); return err },
		"stat bad path":    func() error { _, err := svc.Stat(ctx, server.ID, "root", invalid); return err },
		"content bad path": func() error { _, err := svc.Content(ctx, server.ID, "root", invalid); return err },
		"ticket bad path":  func() error { _, err := svc.IssueDownloadTicket(ctx, server.ID, "root", long); return err },
		"empty root":       func() error { _, err := svc.List(ctx, server.ID, "", "", ""); return err },
	}
	for name, call := range cases {
		if err := call(); !errors.Is(err, ErrValidation) {
			t.Errorf("%s err = %v, want ErrValidation", name, err)
		}
	}
}

// TestDownloadWrongServerNotBurned proves the redeem-time server check does not
// consume a ticket issued for another server: the wrong-server request is
// refused, then the legitimate server still redeems the same ticket.
func TestDownloadWrongServerNotBurned(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	other := registry.seed(teamID)
	svc := newTestService(t, registry, &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2}},
		readBody: []byte("hi"),
	})
	ctx := scopeCtx(teams.RoleOwner, teamID)

	ticket, err := svc.IssueDownloadTicket(ctx, server.ID, "root", "a")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	if _, err := svc.Download(ctx, other.ID, ticket); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong-server download err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Download(ctx, server.ID, ticket); err != nil {
		t.Fatalf("the ticket was burned by the wrong-server request: %v", err)
	}
}

func TestMapRPCError(t *testing.T) {
	cases := []struct {
		code codes.Code
		want error
	}{
		{codes.NotFound, ErrNotFound},
		{codes.InvalidArgument, ErrValidation},
		{codes.PermissionDenied, ErrForbidden},
		{codes.FailedPrecondition, ErrConflict},
		{codes.AlreadyExists, ErrConflict},
		{codes.Aborted, ErrConflict},
		{codes.ResourceExhausted, ErrBusy},
		{codes.Unimplemented, ErrAgentOutdated},
		{codes.Unavailable, ErrAgentUnavailable},
		{codes.DeadlineExceeded, ErrAgentUnavailable},
		{codes.Canceled, ErrAgentUnavailable},
	}
	for _, tc := range cases {
		if got := mapRPCError(status.Error(tc.code, "x")); !errors.Is(got, tc.want) {
			t.Errorf("mapRPCError(%s) = %v, want %v", tc.code, got, tc.want)
		}
	}
	if got := mapRPCError(errors.New("boom")); errors.Is(got, ErrNotFound) {
		t.Errorf("unrecognised error should pass through, got %v", got)
	}
}

// TestResourceExhaustedClassification is the fix-round-1 regression: an agent
// ResourceExhausted is not always "the node is busy". The message prefixes the
// agent already defines distinguish a body over the node's limit (413), low
// free space or an exhausted quota (507) and the transfer semaphore (429).
func TestResourceExhaustedClassification(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    error
	}{
		{"over the upload limit", "write: 500 bytes exceeds the 100 byte upload limit", ErrTooLarge},
		{"fsroot too large", "list: fsroot: directory too large", ErrTooLarge},
		{"low on free space", "write: the filesystem is low on free space", ErrInsufficientStorage},
		{"does not fit", "write: 500 bytes do not fit in 10 free bytes", ErrInsufficientStorage},
		{"quota", "copy: the quota is exhausted", ErrInsufficientStorage},
		{"enospc", "write: write /vol/a: no space left on device", ErrInsufficientStorage},
		{"transfer semaphore", "file service is busy: too many concurrent transfers", ErrBusy},
		{"unrecognised stays busy", "something else entirely", ErrBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapRPCError(status.Error(codes.ResourceExhausted, tc.message))
			if !errors.Is(got, tc.want) {
				t.Fatalf("mapRPCError(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

func TestDisabledFeature(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := NewDefaultService(registry, nil)
	if _, err := svc.Roots(context.Background(), server.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

// TestDefaultServiceUsesProvidedLogger is the fix-round-1 regression: the
// production default service must write its audit trail through the control
// plane's configured logger, not slog.Default.
func TestDefaultServiceUsesProvidedLogger(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())

	svc := NewDefaultService(registry, logger)
	// No dialer is wired, so the operation fails with ErrAgentUnavailable but
	// still audits through the provided logger.
	if _, err := svc.Mkdir(scopeCtx(teams.RoleOwner, server.TeamID), server.ID, "volumes", "d", 0); err == nil {
		t.Fatal("mkdir without a dialer should fail")
	}
	if !strings.Contains(buf.String(), `"op":"mkdir"`) {
		t.Fatalf("the audit trail did not use the provided logger: %s", buf.String())
	}
}

func TestListWithoutDialerIsUnavailable(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	svc := NewService(Config{Registry: registry})
	if _, err := svc.List(scopeCtx(teams.RoleOwner, teamID), server.ID, "root", "", ""); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrAgentUnavailable", err)
	}
}
