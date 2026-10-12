package files

import (
	"context"
	"errors"
	"io"
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

	rootsResp *agentv1.ListRootsResponse
	rootsErr  error
	listResp  *agentv1.ListResponse
	listErr   error
	statResp  *agentv1.StatResponse
	statErr   error
	readBody  []byte
	readErr   error
	closes    int
	seenList  *agentv1.ListRequest
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

func (f *fakeFileClient) Read(context.Context, *agentv1.ReadRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ReadChunk], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	return &fakeReadStream{chunks: [][]byte{f.readBody}}, nil
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

	t.Run("team-less server is not found", func(t *testing.T) {
		registry := newFakeRegistry()
		server := registry.seed(uuid.Nil)
		svc := newTestService(t, registry, &fakeFileClient{})
		if _, err := svc.Roots(scopeCtx(teams.RoleOwner, teamID), server.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
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

func TestMapRPCError(t *testing.T) {
	cases := []struct {
		code codes.Code
		want error
	}{
		{codes.NotFound, ErrNotFound},
		{codes.InvalidArgument, ErrValidation},
		{codes.PermissionDenied, ErrForbidden},
		{codes.FailedPrecondition, ErrConflict},
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

func TestDisabledFeature(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := NewDefaultService(registry)
	if _, err := svc.Roots(context.Background(), server.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
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
