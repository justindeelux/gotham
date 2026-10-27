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
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeRegistry struct {
	mu     sync.Mutex
	items  map[uuid.UUID]*servers.Server
	getErr error
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{items: make(map[uuid.UUID]*servers.Server)}
}

func (f *fakeRegistry) seed(_ uuid.UUID) *servers.Server {
	server := &servers.Server{ID: uuid.New(), Name: "node-1", IP: "10.0.0.5", Port: 22, Status: servers.StatusReady}
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

type fakeFileClient struct {
	mu sync.Mutex

	rootsResp  *agentv1.ListRootsResponse
	rootsErr   error
	listResp   *agentv1.ListResponse
	listErr    error
	statResp   *agentv1.StatResponse
	statErr    error
	seenStat   *agentv1.StatRequest
	searchResp *agentv1.SearchResponse
	searchErr  error
	seenSearch *agentv1.SearchRequest
	readBody   []byte
	readErr    error
	seenRead   *agentv1.ReadRequest
	closes     int
	seenList   *agentv1.ListRequest

	sizeResp *agentv1.DirectorySizeResponse
	sizeErr  error
	seenSize *agentv1.DirectorySizeRequest

	writeResp     *agentv1.WriteResponse
	writeErr      error
	writeCloseErr error
	onWriteClose  func()
	lastWrite     *fakeWriteStream
	writeCtx      context.Context
	mkdirResp     *agentv1.MkdirResponse
	mkdirErr      error
	renameResp    *agentv1.RenameResponse
	renameErr     error
	chmodErr      error
	copyMsgs      []*agentv1.Progress
	copyErr       error
	deleteMsgs    []*agentv1.Progress
	deleteErr     error
	seenRename    *agentv1.RenameRequest
	seenCopy      *agentv1.CopyRequest
	seenDelete    *agentv1.DeleteRequest
	seenMkdir     *agentv1.MkdirRequest
	seenChmod     *agentv1.ChmodRequest

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

	capResp          *agentv1.TransferCapabilitiesResponse
	capErr           error
	prepareResp      *agentv1.PrepareBundleResponse
	prepareErr       error
	seenPrepare      *agentv1.PrepareBundleRequest
	artifactChunks   [][]byte
	artifactErr      error
	seenReadArtifact *agentv1.ReadArtifactRequest
	onArtifactRecv   func()
	receiveResp      *agentv1.ReceiveArtifactResponse
	receiveErr       error
	receiveCloseErr  error
	receiveHook      func(*agentv1.ReceiveArtifactRequest) error
	lastReceive      *fakeReceiveStream
	applyEvents      []*agentv1.ApplyArtifactEvent
	applyErr         error
	applyStreamErr   error
	applyHook        func(int)
	seenApply        *agentv1.ApplyArtifactRequest
	discardErr       error
	discarded        []string
	readChunks       [][]byte
	statHook         func(*agentv1.StatRequest) (*agentv1.StatResponse, error)
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

func (f *fakeFileClient) Stat(_ context.Context, in *agentv1.StatRequest, _ ...grpc.CallOption) (*agentv1.StatResponse, error) {
	f.mu.Lock()
	if f.statHook != nil {
		hook := f.statHook
		f.mu.Unlock()
		return hook(in)
	}
	f.seenStat = in
	if f.statErr != nil {
		err := f.statErr
		f.mu.Unlock()
		return nil, err
	}
	response := f.statResp
	f.mu.Unlock()
	if response != nil {
		return response, nil
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

func (f *fakeFileClient) Read(_ context.Context, in *agentv1.ReadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ReadChunk], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenRead = in
	if f.readErr != nil {
		return nil, f.readErr
	}
	if len(f.readChunks) > 0 {
		return &fakeReadStream{chunks: f.readChunks}, nil
	}
	body := f.readBody
	if start := in.GetOffset(); start > 0 || in.GetLimit() > 0 {
		if start > int64(len(body)) {
			start = int64(len(body))
		}
		end := int64(len(body))
		if limit := in.GetLimit(); limit > 0 && start+limit < end {
			end = start + limit
		}
		body = body[start:end]
	}
	return &fakeReadStream{chunks: [][]byte{body}}, nil
}

func (f *fakeFileClient) Write(ctx context.Context, _ ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.WriteRequest, agentv1.WriteResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCtx = ctx
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	stream := &fakeWriteStream{resp: f.writeResp, closeErr: f.writeCloseErr, onClose: f.onWriteClose}
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

func (f *fakeFileClient) TransferCapabilities(context.Context, *agentv1.TransferCapabilitiesRequest, ...grpc.CallOption) (*agentv1.TransferCapabilitiesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.capErr != nil {
		return nil, f.capErr
	}
	if f.capResp != nil {
		return f.capResp, nil
	}
	return &agentv1.TransferCapabilitiesResponse{}, nil
}

func (f *fakeFileClient) PrepareBundle(_ context.Context, in *agentv1.PrepareBundleRequest, _ ...grpc.CallOption) (*agentv1.PrepareBundleResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenPrepare = in
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	if f.prepareResp != nil {
		return f.prepareResp, nil
	}
	return &agentv1.PrepareBundleResponse{TransferId: in.GetTransferId()}, nil
}

func (f *fakeFileClient) ReadArtifact(_ context.Context, in *agentv1.ReadArtifactRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ArtifactChunk], error) {
	f.mu.Lock()
	f.seenReadArtifact = in
	if f.artifactErr != nil {
		err := f.artifactErr
		f.mu.Unlock()
		return nil, err
	}
	stream := &fakeArtifactStream{chunks: f.artifactChunks, onRecv: f.onArtifactRecv}
	f.mu.Unlock()
	return stream, nil
}

func (f *fakeFileClient) ReceiveArtifact(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.ReceiveArtifactRequest, agentv1.ReceiveArtifactResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.receiveErr != nil {
		return nil, f.receiveErr
	}
	stream := &fakeReceiveStream{resp: f.receiveResp, closeErr: f.receiveCloseErr, parent: f, sendHook: f.receiveHook}
	f.lastReceive = stream
	return stream, nil
}

func (f *fakeFileClient) ApplyArtifact(_ context.Context, in *agentv1.ApplyArtifactRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ApplyArtifactEvent], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenApply = in
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	return &fakeApplyStream{events: f.applyEvents, err: f.applyStreamErr, hook: f.applyHook}, nil
}

func (f *fakeFileClient) DiscardArtifact(_ context.Context, in *agentv1.DiscardArtifactRequest, _ ...grpc.CallOption) (*agentv1.DiscardArtifactResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discarded = append(f.discarded, in.GetArtifact())
	if f.discardErr != nil {
		return nil, f.discardErr
	}
	return &agentv1.DiscardArtifactResponse{}, nil
}

type fakeArtifactStream struct {
	grpc.ClientStream
	chunks [][]byte
	index  int
	onRecv func()
}

func (s *fakeArtifactStream) Recv() (*agentv1.ArtifactChunk, error) {
	if s.onRecv != nil {
		s.onRecv()
	}
	if s.index >= len(s.chunks) {
		return nil, io.EOF
	}
	data := s.chunks[s.index]
	s.index++
	return &agentv1.ArtifactChunk{Data: data}, nil
}

type fakeReceiveStream struct {
	grpc.ClientStream
	parent    *fakeFileClient
	header    *agentv1.ReceiveArtifactHeader
	body      []byte
	resp      *agentv1.ReceiveArtifactResponse
	closeErr  error
	sendHook  func(*agentv1.ReceiveArtifactRequest) error
	closeOnce bool
}

func (s *fakeReceiveStream) Send(msg *agentv1.ReceiveArtifactRequest) error {
	switch part := msg.GetPart().(type) {
	case *agentv1.ReceiveArtifactRequest_Header:
		s.header = part.Header
	case *agentv1.ReceiveArtifactRequest_Data:
		s.body = append(s.body, part.Data...)
	}
	if s.sendHook != nil {
		return s.sendHook(msg)
	}
	return nil
}

func (s *fakeReceiveStream) CloseAndRecv() (*agentv1.ReceiveArtifactResponse, error) {
	if s.closeOnce {
		return nil, io.EOF
	}
	s.closeOnce = true
	if s.closeErr != nil {
		return nil, s.closeErr
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &agentv1.ReceiveArtifactResponse{Artifact: "dest-artifact"}, nil
}

type fakeApplyStream struct {
	grpc.ClientStream
	events []*agentv1.ApplyArtifactEvent
	index  int
	err    error
	hook   func(int)
}

func (s *fakeApplyStream) Recv() (*agentv1.ApplyArtifactEvent, error) {
	if s.hook != nil {
		s.hook(s.index)
	}
	if s.index >= len(s.events) {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (f *fakeFileClient) lastWriteHeader() *agentv1.WriteHeader {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastWrite == nil {
		return nil
	}
	return f.lastWrite.header
}

func (f *fakeFileClient) lastWriteBody() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastWrite == nil {
		return nil
	}
	return f.lastWrite.body
}

func (f *fakeFileClient) lastWriteClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWrite != nil && f.lastWrite.closed
}

type fakeWriteStream struct {
	grpc.ClientStream
	mu        sync.Mutex
	header    *agentv1.WriteHeader
	body      []byte
	commit    *agentv1.WriteCommit
	resp      *agentv1.WriteResponse
	closeErr  error
	closed    bool
	closeCall int
	onClose   func()
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
		s.commit = part.Commit
	}
	return nil
}

func (s *fakeWriteStream) CloseAndRecv() (*agentv1.WriteResponse, error) {
	if s.onClose != nil {
		s.onClose()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.closeCall++
	if s.closeErr != nil {
		return nil, s.closeErr
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &agentv1.WriteResponse{}, nil
}

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

func (f *fakeFileClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

func newTestService(t *testing.T, registry Registry, client FileClient) *Service {
	t.Helper()
	t.Setenv(FeatureEnv, "true")
	return NewService(Config{
		Registry:        registry,
		Dial:            func(context.Context, *servers.Server) (FileClient, error) { return client, nil },
		MaxContentBytes: 8,
	})
}

func scopeCtx(_ ...any) context.Context {
	return context.Background()
}

func TestRootsAndListMapping(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
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
	ctx := scopeCtx("", projectID)

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

func TestListEntryOwnerMapping(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	client := &fakeFileClient{listResp: &agentv1.ListResponse{Entries: []*agentv1.FileEntry{
		{Name: "new.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Owner: "root", Group: "wheel"},
		{Name: "old.txt", Kind: agentv1.FileKind_FILE_KIND_FILE},
	}}}
	svc := newTestService(t, registry, client)

	listing, err := svc.List(scopeCtx("", projectID), server.ID, "volumes", "", "")
	if err != nil {
		t.Fatalf("list err = %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(listing.Entries))
	}
	if got := listing.Entries[0]; got.Owner != "root" || got.Group != "wheel" {
		t.Fatalf("entry[0] owner/group = %q/%q, want root/wheel", got.Owner, got.Group)
	}
	if got := listing.Entries[1]; got.Owner != "" || got.Group != "" {
		t.Fatalf("entry[1] owner/group = %q/%q, want empty", got.Owner, got.Group)
	}
}

func TestContentLimits(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	ctx := scopeCtx("", projectID)

	t.Run("over limit is a read-only tail", func(t *testing.T) {
		svc := newTestService(t, registry, &fakeFileClient{
			statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "big", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 9}},
			readBody: []byte("tail-nine"),
		})
		content, err := svc.Content(ctx, server.ID, "root", "big")
		if err != nil {
			t.Fatalf("err = %v, want a tail", err)
		}
		if !content.Truncated || content.Size != 9 || content.Content != "ail-nine" {
			t.Fatalf("content = %+v, want a truncated tail of 9 bytes", content)
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
		if content.SHA256 != "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4" {
			t.Fatalf("sha256 = %q", content.SHA256)
		}
		if content.Etag != `"v1"` || content.Content != "hi" {
			t.Fatalf("content = %+v", content)
		}
	})
}

func TestQueryValidation(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	svc := newTestService(t, registry, &fakeFileClient{})
	ctx := scopeCtx("", projectID)

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

func TestDownloadWrongServerNotBurned(t *testing.T) {
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	other := registry.seed(projectID)
	svc := newTestService(t, registry, &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2}},
		readBody: []byte("hi"),
	})
	ctx := scopeCtx("", projectID)

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
	if got := mapRPCError(status.Error(codes.FailedPrecondition, "file_manager_disabled")); !errors.Is(got, ErrDisabledOnNode) {
		t.Errorf("mapRPCError(disabled on node) = %v, want %v", got, ErrDisabledOnNode)
	}
	if got := mapRPCError(errors.New("boom")); errors.Is(got, ErrNotFound) {
		t.Errorf("unrecognised error should pass through, got %v", got)
	}
}

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

func TestDefaultServiceUsesProvidedLogger(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())

	svc := NewDefaultService(registry, logger)
	if _, err := svc.Mkdir(scopeCtx(""), server.ID, "volumes", "d", 0); err == nil {
		t.Fatal("mkdir without a dialer should fail")
	}
	if !strings.Contains(buf.String(), `"op":"mkdir"`) {
		t.Fatalf("the audit trail did not use the provided logger: %s", buf.String())
	}
}

func TestListWithoutDialerIsUnavailable(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	projectID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(projectID)
	svc := NewService(Config{Registry: registry})
	if _, err := svc.List(scopeCtx("", projectID), server.ID, "root", "", ""); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrAgentUnavailable", err)
	}
}

func (f *fakeFileClient) lastWriteStream() *fakeWriteStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWrite
}

func (f *fakeFileClient) discardedArtifacts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.discarded...)
}

func (f *fakeFileClient) seenReadRequest() *agentv1.ReadRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seenRead
}

func (f *fakeFileClient) seenPrepareRequest() *agentv1.PrepareBundleRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seenPrepare
}

func (f *fakeFileClient) lastReceiveStream() *fakeReceiveStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReceive
}

func (s *fakeWriteStream) snapshot() (*agentv1.WriteHeader, []byte, *agentv1.WriteCommit, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header, append([]byte(nil), s.body...), s.commit, s.closeCall
}

func (s *fakeReceiveStream) snapshot() (*agentv1.ReceiveArtifactHeader, []byte) {
	return s.header, append([]byte(nil), s.body...)
}
