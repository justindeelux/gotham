package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	// fileTransferDir is reserved inside every writable root for received
	// bundles. Ordinary file APIs refuse it, listings hide it and the temp
	// sweep reclaims abandoned staging.
	fileTransferDir = ".gotham-transfer"

	fileTransferBundleName   = "bundle.tar.gz"
	fileTransferExtractDir   = "x"
	fileTransferMaxSources   = 1024
	fileTransferSkipSamples  = 32
	fileTransferMaxArtifacts = 64
	fileTransferStageMode    = 0o700
)

// fileTransferArtifactTTL bounds an unused artifact's staging. Read/Apply mark
// the artifact in use, so an active transfer is never expired.
var fileTransferArtifactTTL = time.Hour

// fileTransferPublishHook runs before each top-level publication. Tests use it
// to cancel mid-publish; it is nil in production.
var fileTransferPublishHook func(name string)

type transferArtifactKind int

const (
	transferArtifactPrepared transferArtifactKind = iota
	transferArtifactReceived
)

type transferArtifact struct {
	transferID string
	handle     string
	kind       transferArtifactKind
	rootName   string
	dir        string
	// stageRel is the private staging directory relative to rootName for a
	// received artifact.
	stageRel string
	// stageAbs is the private agent-owned staging directory for a prepared
	// artifact.
	stageAbs string
	// bundleAbs is the bundle file of a prepared artifact.
	bundleAbs string
	length    int64
	sha256    string
	expanded  int64
	entries   int64
	topLevel  []string
	expires   time.Time
	inUse     bool
}

func (a *transferArtifact) bundlePath(root *fsroot.Root) string {
	if a.kind == transferArtifactPrepared {
		return a.bundleAbs
	}
	return filepath.Join(root.Path(), filepath.FromSlash(path.Join(a.stageRel, fileTransferBundleName)))
}

func transferSkipReason(kind fsroot.Kind) string {
	if kind == fsroot.KindSymlink {
		return "symlink"
	}
	return "special"
}

func cleanTransferID(id string) (string, error) {
	if id == "" || len(id) > 128 {
		return "", status.Error(codes.InvalidArgument, "transfer: transfer_id is required and bounded to 128 bytes")
	}
	for i := 0; i < len(id); i++ {
		if c := id[i]; c < 0x20 || c > 0x7e {
			return "", status.Error(codes.InvalidArgument, "transfer: transfer_id contains an unsupported character")
		}
	}
	return id, nil
}

func cleanTransferTopLevel(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, status.Error(codes.InvalidArgument, "transfer: top_level is required")
	}
	if len(names) > fileTransferMaxSources {
		return nil, status.Errorf(codes.ResourceExhausted, "transfer: more than %d top-level names", fileTransferMaxSources)
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || name == "." || name == ".." || strings.IndexByte(name, 0) >= 0 ||
			strings.ContainsAny(name, `/\`) || name == fileTrashDir || name == fileTransferDir {
			return nil, status.Errorf(codes.InvalidArgument, "transfer: invalid top-level name %q", name)
		}
		if _, ok := seen[name]; ok {
			return nil, status.Errorf(codes.InvalidArgument, "transfer: duplicate top-level name %q", name)
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func cleanTransferSources(root *fsroot.Root, raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, status.Error(codes.InvalidArgument, "prepare bundle: at least one source is required")
	}
	if len(raw) > fileTransferMaxSources {
		return nil, status.Errorf(codes.InvalidArgument, "prepare bundle: at most %d sources are accepted", fileTransferMaxSources)
	}
	sources := make([]string, 0, len(raw))
	for _, name := range raw {
		rel, err := fsroot.Clean(name)
		if err != nil || rel == "." {
			return nil, status.Errorf(codes.InvalidArgument, "prepare bundle: invalid source %q", name)
		}
		if err := guardTrash(root, "prepare bundle", rel); err != nil {
			return nil, err
		}
		for _, existing := range sources {
			if existing == rel || isWithin(existing, rel) || isWithin(rel, existing) ||
				path.Base(existing) == path.Base(rel) {
				return nil, status.Errorf(codes.InvalidArgument, "prepare bundle: sources %q and %q overlap", existing, rel)
			}
		}
		sources = append(sources, rel)
	}
	return sources, nil
}

func sameTopLevel(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]struct{}, len(got))
	for _, name := range got {
		seen[name] = struct{}{}
	}
	for _, name := range want {
		if _, ok := seen[name]; !ok {
			return false
		}
	}
	return true
}

func transferHandle() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("transfer: generate handle: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func validTransferDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func hashTransferFile(name string) (int64, string, error) {
	file, err := os.Open(name)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	n, err := io.Copy(hasher, file)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *FileServer) newSourceStage() (string, error) {
	if err := os.MkdirAll(s.transferRoot, fileTransferStageMode); err != nil {
		return "", err
	}
	return os.MkdirTemp(s.transferRoot, "bundle-")
}

func (s *FileServer) registerArtifact(a *transferArtifact) error {
	s.artifactsMu.Lock()
	defer s.artifactsMu.Unlock()
	total := 0
	for _, byHandle := range s.artifacts {
		total += len(byHandle)
	}
	if total >= fileTransferMaxArtifacts {
		return status.Error(codes.ResourceExhausted, "transfer: too many staged artifacts")
	}
	if s.artifacts[a.transferID] == nil {
		s.artifacts[a.transferID] = make(map[string]*transferArtifact)
	}
	s.artifacts[a.transferID][a.handle] = a
	return nil
}

// popArtifact removes one artifact from the registry and returns it. An expired
// artifact is removed from disk as a side effect.
func (s *FileServer) popArtifact(transferID, handle string) *transferArtifact {
	s.artifactsMu.Lock()
	byHandle := s.artifacts[transferID]
	a := byHandle[handle]
	if a != nil {
		delete(byHandle, handle)
		if len(byHandle) == 0 {
			delete(s.artifacts, transferID)
		}
	}
	s.artifactsMu.Unlock()
	return a
}

// acquireArtifact pins one artifact for a stream operation and extends its
// lifetime. One transfer can never address another transfer's artifact.
func (s *FileServer) acquireArtifact(transferID, handle string) (*transferArtifact, error) {
	if transferID == "" || handle == "" {
		return nil, status.Error(codes.InvalidArgument, "transfer: transfer_id and artifact are required")
	}
	now := time.Now()
	s.artifactsMu.Lock()
	a := s.artifacts[transferID][handle]
	if a != nil && (a.inUse || now.After(a.expires)) {
		busy := a.inUse
		if !busy {
			delete(s.artifacts[transferID], handle)
			if len(s.artifacts[transferID]) == 0 {
				delete(s.artifacts, transferID)
			}
		}
		s.artifactsMu.Unlock()
		if busy {
			return nil, status.Error(codes.ResourceExhausted, "transfer: the artifact is in use")
		}
		s.removeArtifactStaging(a)
		return nil, status.Error(codes.NotFound, "transfer: the artifact expired")
	}
	if a != nil {
		a.inUse = true
		a.expires = now.Add(fileTransferArtifactTTL)
	}
	s.artifactsMu.Unlock()
	if a == nil {
		return nil, status.Error(codes.NotFound, "transfer: unknown artifact")
	}
	return a, nil
}

func (s *FileServer) releaseArtifact(a *transferArtifact) {
	s.artifactsMu.Lock()
	a.inUse = false
	a.expires = time.Now().Add(fileTransferArtifactTTL)
	s.artifactsMu.Unlock()
}

// discardArtifact removes one artifact from the registry and its
// transfer-owned staging. Published entries are never touched.
func (s *FileServer) discardArtifact(a *transferArtifact) {
	if popped := s.popArtifact(a.transferID, a.handle); popped != nil {
		s.removeArtifactStaging(popped)
	} else {
		s.removeArtifactStaging(a)
	}
}

// removeArtifactStaging removes only the directory this server created for the
// artifact: agent-owned source staging or the reserved destination directory.
func (s *FileServer) removeArtifactStaging(a *transferArtifact) {
	switch a.kind {
	case transferArtifactPrepared:
		if a.stageAbs != "" && withinPath(s.transferRoot, a.stageAbs) {
			_ = os.RemoveAll(a.stageAbs)
		}
	default:
		if root, err := s.root(a.rootName); err == nil && firstComponent(a.stageRel) == fileTransferDir {
			_ = root.RemoveTree(context.Background(), a.stageRel, nil)
		}
	}
}

// sweepTransfers expires unused artifacts and reclaims source staging a
// previous process left behind. An active artifact is never removed.
func (s *FileServer) sweepTransfers(now time.Time) {
	cutoff := now.Add(-fileTempSweepAge)
	var expired []*transferArtifact
	s.artifactsMu.Lock()
	for id, byHandle := range s.artifacts {
		for handle, a := range byHandle {
			if !a.inUse && now.After(a.expires) {
				expired = append(expired, a)
				delete(byHandle, handle)
			}
		}
		if len(byHandle) == 0 {
			delete(s.artifacts, id)
		}
	}
	s.artifactsMu.Unlock()
	for _, a := range expired {
		s.removeArtifactStaging(a)
	}
	entries, err := os.ReadDir(s.transferRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(s.transferRoot, entry.Name()))
	}
}

func (s *FileServer) TransferCapabilities(context.Context, *agentv1.TransferCapabilitiesRequest) (*agentv1.TransferCapabilitiesResponse, error) {
	return &agentv1.TransferCapabilitiesResponse{
		Available:        true,
		MaxBundleBytes:   uint64(max(s.archiveMaxInputBytes, 0)),
		MaxExpandedBytes: uint64(max(s.archiveMaxBytes, 0)),
		MaxEntries:       uint64(max(s.archiveMaxEntries, 0)),
		MaxSources:       fileTransferMaxSources,
		MaxConcurrency:   uint32(cap(s.transfers)),
	}, nil
}

func (s *FileServer) PrepareBundle(ctx context.Context, req *agentv1.PrepareBundleRequest) (*agentv1.PrepareBundleResponse, error) {
	transferID, err := cleanTransferID(req.GetTransferId())
	if err != nil {
		return nil, err
	}
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	sources, err := cleanTransferSources(root, req.GetSources())
	if err != nil {
		return nil, err
	}
	release, err := s.beginTransfer()
	if err != nil {
		return nil, err
	}
	defer release()

	stageAbs, err := s.newSourceStage()
	if err != nil {
		return nil, fileError("prepare bundle", err)
	}
	removeStage := true
	defer func() {
		if removeStage {
			_ = os.RemoveAll(stageAbs)
		}
	}()
	bundleAbs := filepath.Join(stageAbs, fileTransferBundleName)
	file, err := os.OpenFile(bundleAbs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fileError("prepare bundle", err)
	}
	counter := &archiveCounter{
		maxEntries: s.archiveMaxEntries,
		maxBytes:   s.archiveMaxBytes,
		sampleCap:  fileTransferSkipSamples,
	}
	sink, closeSink, err := newArchiveSink(&cappedWriter{w: file, limit: s.archiveMaxInputBytes}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ)
	if err != nil {
		_ = file.Close()
		return nil, fileError("prepare bundle", err)
	}
	topLevel := make([]string, 0, len(sources))
	for _, source := range sources {
		entry, statErr := root.Stat(source)
		if statErr != nil {
			_ = closeSink()
			return nil, fileError("prepare bundle", statErr)
		}
		if entry.Kind != fsroot.KindDirectory && entry.Kind != fsroot.KindRegular {
			counter.skip(path.Base(source), transferSkipReason(entry.Kind))
			continue
		}
		if err := s.compressEntry(ctx, root, sink, source, path.Base(source), nil, counter, nil, 0); err != nil {
			_ = closeSink()
			return nil, fileError("prepare bundle", err)
		}
		topLevel = append(topLevel, path.Base(source))
	}
	if err := closeSink(); err != nil {
		_ = file.Close()
		return nil, fileError("prepare bundle", err)
	}
	if len(topLevel) == 0 {
		_ = file.Close()
		return nil, status.Error(codes.InvalidArgument, "prepare bundle: the selection contains nothing transferable")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, fileError("prepare bundle", err)
	}
	if err := file.Close(); err != nil {
		return nil, fileError("prepare bundle", err)
	}
	length, digest, err := hashTransferFile(bundleAbs)
	if err != nil {
		return nil, fileError("prepare bundle", err)
	}
	handle, err := transferHandle()
	if err != nil {
		return nil, fileError("prepare bundle", err)
	}
	artifact := &transferArtifact{
		transferID: transferID,
		handle:     handle,
		kind:       transferArtifactPrepared,
		stageAbs:   stageAbs,
		bundleAbs:  bundleAbs,
		length:     length,
		sha256:     digest,
		expanded:   counter.bytes,
		entries:    counter.entries,
		topLevel:   append([]string(nil), topLevel...),
		expires:    time.Now().Add(fileTransferArtifactTTL),
	}
	if err := s.registerArtifact(artifact); err != nil {
		return nil, err
	}
	removeStage = false
	return &agentv1.PrepareBundleResponse{
		TransferId:      transferID,
		Artifact:        handle,
		CompressedBytes: uint64(length),
		Sha256:          digest,
		ExpandedBytes:   uint64(counter.bytes),
		EntryCount:      uint64(counter.entries),
		TopLevel:        topLevel,
		SkippedCount:    uint64(counter.skipped),
		Skipped:         counter.skipSamples,
	}, nil
}

func (s *FileServer) ReadArtifact(req *agentv1.ReadArtifactRequest, stream grpc.ServerStreamingServer[agentv1.ArtifactChunk]) error {
	transferID, err := cleanTransferID(req.GetTransferId())
	if err != nil {
		return err
	}
	artifact, err := s.acquireArtifact(transferID, req.GetArtifact())
	if err != nil {
		return err
	}
	defer s.releaseArtifact(artifact)
	if artifact.kind != transferArtifactPrepared {
		return status.Error(codes.FailedPrecondition, "read artifact: the artifact was not prepared by this node")
	}
	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	file, err := os.Open(artifact.bundleAbs)
	if err != nil {
		return fileError("read artifact", err)
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, fileChunkSize)
	for {
		if err := stream.Context().Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			if sendErr := stream.Send(&agentv1.ArtifactChunk{Data: buffer[:n]}); sendErr != nil {
				return sendErr
			}
		}
		switch {
		case errors.Is(readErr, io.EOF):
			return nil
		case readErr != nil:
			return fileError("read artifact", readErr)
		}
	}
}

func (s *FileServer) ReceiveArtifact(stream grpc.ClientStreamingServer[agentv1.ReceiveArtifactRequest, agentv1.ReceiveArtifactResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return streamRecvError("receive artifact", err)
	}
	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "receive artifact: the first message must be a header")
	}
	transferID, err := cleanTransferID(header.GetTransferId())
	if err != nil {
		return err
	}
	root, err := s.root(header.GetRoot())
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "receive artifact: root %q is read-only", header.GetRoot())
	}
	dir, err := fsroot.Clean(header.GetDirectory())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive artifact: invalid directory %q", header.GetDirectory())
	}
	if err := guardTrash(root, "receive artifact", dir); err != nil {
		return err
	}
	if entry, statErr := root.Stat(dir); statErr != nil {
		return fileError("receive artifact", statErr)
	} else if entry.Kind != fsroot.KindDirectory {
		return status.Errorf(codes.InvalidArgument, "receive artifact: destination %q is not a directory", dir)
	}
	if header.GetLength() == 0 || header.GetLength() > uint64(s.archiveMaxInputBytes) {
		return status.Errorf(codes.ResourceExhausted, "receive artifact: length %d is outside the %d byte bundle cap", header.GetLength(), s.archiveMaxInputBytes)
	}
	length := int64(header.GetLength())
	digest := strings.ToLower(strings.TrimSpace(header.GetSha256()))
	if !validTransferDigest(digest) {
		return status.Error(codes.InvalidArgument, "receive artifact: sha256 must be a hex SHA-256 digest")
	}
	topLevel, err := cleanTransferTopLevel(header.GetTopLevel())
	if err != nil {
		return err
	}
	if header.GetExpandedBytes() > uint64(s.archiveMaxBytes) {
		return status.Errorf(codes.ResourceExhausted, "receive artifact: %d expanded bytes exceeds the %d byte cap", header.GetExpandedBytes(), s.archiveMaxBytes)
	}
	if header.GetEntryCount() > uint64(s.archiveMaxEntries) {
		return status.Errorf(codes.ResourceExhausted, "receive artifact: %d entries exceeds the %d entry cap", header.GetEntryCount(), s.archiveMaxEntries)
	}

	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "receive artifact: the filesystem is low on free space")
	}
	if err := root.MkdirAll(fileTransferDir, fileTransferStageMode); err != nil {
		return fileError("receive artifact", err)
	}
	stageRel, err := root.MkdirTemp(fileTransferDir, fileTransferStageMode)
	if err != nil {
		return fileError("receive artifact", err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = root.RemoveTree(context.Background(), stageRel, nil)
		}
	}()
	file, err := root.CreateFile(path.Join(stageRel, fileTransferBundleName), 0o600, false)
	if err != nil {
		return fileError("receive artifact", err)
	}
	if err := receiveArtifactBody(stream, file, length, digest); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fileError("receive artifact", err)
	}
	if err := file.Close(); err != nil {
		return fileError("receive artifact", err)
	}
	handle, err := transferHandle()
	if err != nil {
		return fileError("receive artifact", err)
	}
	artifact := &transferArtifact{
		transferID: transferID,
		handle:     handle,
		kind:       transferArtifactReceived,
		rootName:   root.Name(),
		dir:        dir,
		stageRel:   stageRel,
		length:     length,
		sha256:     digest,
		expanded:   int64(header.GetExpandedBytes()),
		entries:    int64(header.GetEntryCount()),
		topLevel:   topLevel,
		expires:    time.Now().Add(fileTransferArtifactTTL),
	}
	if err := s.registerArtifact(artifact); err != nil {
		return err
	}
	keepStage = true
	return stream.SendAndClose(&agentv1.ReceiveArtifactResponse{
		TransferId: transferID,
		Artifact:   handle,
		Length:     uint64(length),
		Sha256:     digest,
	})
}

type artifactRecv struct {
	msg *agentv1.ReceiveArtifactRequest
	err error
}

func receiveArtifactBody(stream grpc.ClientStreamingServer[agentv1.ReceiveArtifactRequest, agentv1.ReceiveArtifactResponse], file *os.File, length int64, digest string) error {
	hasher := sha256.New()
	var received int64
	parts := make(chan artifactRecv, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			msg, recvErr := stream.Recv()
			select {
			case parts <- artifactRecv{msg: msg, err: recvErr}:
			case <-done:
				return
			}
			if recvErr != nil {
				return
			}
		}
	}()

	idle := time.NewTimer(fileRecvIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return status.FromContextError(stream.Context().Err()).Err()
		case <-idle.C:
			return status.Errorf(codes.DeadlineExceeded, "receive artifact: no data for %s; aborting the upload", fileRecvIdleTimeout)
		case result := <-parts:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(fileRecvIdleTimeout)
			if errors.Is(result.err, io.EOF) {
				if received != length {
					return status.Errorf(codes.Aborted, "receive artifact: received %d of %d bytes; the bundle was truncated", received, length)
				}
				if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), digest) {
					return status.Error(codes.Aborted, "receive artifact: the bundle digest does not match")
				}
				return nil
			}
			if result.err != nil {
				return streamRecvError("receive artifact", result.err)
			}
			switch part := result.msg.GetPart().(type) {
			case *agentv1.ReceiveArtifactRequest_Data:
				received += int64(len(part.Data))
				if received > length {
					return status.Errorf(codes.InvalidArgument, "receive artifact: the body exceeds the declared %d bytes", length)
				}
				if _, writeErr := file.Write(part.Data); writeErr != nil {
					return fileError("receive artifact", writeErr)
				}
				_, _ = hasher.Write(part.Data)
			case *agentv1.ReceiveArtifactRequest_Header:
				return status.Error(codes.InvalidArgument, "receive artifact: unexpected second header")
			default:
				return status.Error(codes.InvalidArgument, "receive artifact: empty message")
			}
		}
	}
}

func (s *FileServer) ApplyArtifact(req *agentv1.ApplyArtifactRequest, stream grpc.ServerStreamingServer[agentv1.ApplyArtifactEvent]) error {
	transferID, err := cleanTransferID(req.GetTransferId())
	if err != nil {
		return err
	}
	artifact, err := s.acquireArtifact(transferID, req.GetArtifact())
	if err != nil {
		return err
	}
	defer s.releaseArtifact(artifact)
	if artifact.kind != transferArtifactReceived {
		return status.Error(codes.FailedPrecondition, "apply artifact: only a received bundle can be applied")
	}
	dir, err := fsroot.Clean(req.GetDirectory())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "apply artifact: invalid directory %q", req.GetDirectory())
	}
	if req.GetRoot() != artifact.rootName || dir != artifact.dir {
		return status.Error(codes.InvalidArgument, "apply artifact: the bundle belongs to another root or directory")
	}
	topLevel, err := cleanTransferTopLevel(req.GetTopLevel())
	if err != nil {
		return err
	}
	if !sameTopLevel(topLevel, artifact.topLevel) {
		return status.Error(codes.InvalidArgument, "apply artifact: top_level does not match the received selection")
	}
	root, err := s.root(artifact.rootName)
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "apply artifact: root %q is read-only", artifact.rootName)
	}
	if err := guardTrash(root, "apply artifact", dir); err != nil {
		return err
	}
	if entry, statErr := root.Stat(dir); statErr != nil {
		return fileError("apply artifact", statErr)
	} else if entry.Kind != fsroot.KindDirectory {
		return status.Errorf(codes.FailedPrecondition, "apply artifact: destination %q changed", dir)
	}

	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "apply artifact: the filesystem is low on free space")
	}

	bundle := artifact.bundlePath(root)
	length, digest, err := hashTransferFile(bundle)
	if err != nil || length != artifact.length || !strings.EqualFold(digest, artifact.sha256) {
		s.discardArtifact(artifact)
		return status.Error(codes.FailedPrecondition, "apply artifact: the staged bundle no longer matches its length and checksum")
	}
	xRel := path.Join(artifact.stageRel, fileTransferExtractDir)
	if _, err := root.Mkdir(xRel, fileTransferStageMode); err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	xAbs := filepath.Join(root.Path(), filepath.FromSlash(xRel))
	info, err := os.Stat(xAbs)
	if err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	staging, err := fsroot.Open("transfer-staging", xAbs, info, true)
	if err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	defer func() { _ = staging.Close() }()
	archive, err := os.Open(bundle)
	if err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	defer func() { _ = archive.Close() }()

	counter := &archiveCounter{
		maxEntries:   artifact.entries,
		maxBytes:     artifact.expanded,
		archiveBytes: artifact.length,
	}
	progress := &applyProgress{stream: stream}
	if err := progress.send("extract", dir); err != nil {
		s.discardArtifact(artifact)
		return err
	}
	dirs := &extractDirs{}
	state := newExtractState(staging)
	if err := s.extractArchive(stream.Context(), staging, archive, artifact.length, agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ, ".", xRel, false, counter, dirs, state, progress); err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	if counter.bytes != artifact.expanded || counter.entries != artifact.entries {
		s.discardArtifact(artifact)
		return status.Errorf(codes.FailedPrecondition, "apply artifact: archive expands to %d bytes in %d entries, want %d in %d",
			counter.bytes, counter.entries, artifact.expanded, artifact.entries)
	}
	if err := dirs.apply(staging); err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	var names []string
	if err := staging.Names(".", func(name string) error {
		names = append(names, name)
		return nil
	}); err != nil {
		s.discardArtifact(artifact)
		return fileError("apply artifact", err)
	}
	if !sameTopLevel(names, artifact.topLevel) {
		s.discardArtifact(artifact)
		return status.Error(codes.FailedPrecondition, "apply artifact: the archive does not store the prepared selection")
	}

	for _, name := range artifact.topLevel {
		if err := stream.Context().Err(); err != nil {
			s.discardArtifact(artifact)
			return status.FromContextError(err).Err()
		}
		if err := s.recheckPublish(root, dir, name); err != nil {
			s.discardArtifact(artifact)
			return err
		}
		if fileTransferPublishHook != nil {
			fileTransferPublishHook(name)
		}
		if err := stream.Context().Err(); err != nil {
			s.discardArtifact(artifact)
			return status.FromContextError(err).Err()
		}
		if err := progress.send("publish", name); err != nil {
			s.discardArtifact(artifact)
			return err
		}
		if err := root.RenameExclusive(path.Join(xRel, name), path.Join(dir, name)); err != nil {
			if errors.Is(err, fsroot.ErrExists) {
				if sendErr := progress.outcome(name, agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_SKIPPED, "destination already exists"); sendErr != nil {
					s.discardArtifact(artifact)
					return sendErr
				}
				continue
			}
			s.discardArtifact(artifact)
			return fileError("apply artifact", err)
		}
		if err := progress.outcome(name, agentv1.TransferOutcomeStatus_TRANSFER_OUTCOME_STATUS_PUBLISHED, ""); err != nil {
			s.discardArtifact(artifact)
			return err
		}
	}
	s.discardArtifact(artifact)
	return progress.send("done", ".")
}

// recheckPublish applies the destination policy again at publication time, so a
// directory swapped after preflight is reported as a conflict or refusal.
func (s *FileServer) recheckPublish(root *fsroot.Root, dir, name string) error {
	entry, err := root.Stat(dir)
	if err != nil {
		return fileError("apply artifact", err)
	}
	if entry.Kind != fsroot.KindDirectory {
		return status.Errorf(codes.FailedPrecondition, "apply artifact: destination %q changed", dir)
	}
	target := path.Join(dir, name)
	if err := guardTrash(root, "apply artifact", target); err != nil {
		return err
	}
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "apply artifact: an application directory cannot be replaced")
	}
	return nil
}

type applyProgress struct {
	stream    grpc.ServerStreamingServer[agentv1.ApplyArtifactEvent]
	bytesDone int64
	last      time.Time
}

func (p *applyProgress) add(name string, n int64) error {
	p.bytesDone += n
	now := time.Now()
	if now.Sub(p.last) < fileProgressInterval {
		return nil
	}
	p.last = now
	return p.send("extract", name)
}

func (p *applyProgress) done(uint64) error { return nil }

func (p *applyProgress) send(phase, name string) error {
	return p.stream.Send(&agentv1.ApplyArtifactEvent{Event: &agentv1.ApplyArtifactEvent_Phase{
		Phase: &agentv1.TransferPhase{Phase: phase, Path: name, BytesDone: p.bytesDone, BytesTotal: -1},
	}})
}

func (p *applyProgress) outcome(name string, status agentv1.TransferOutcomeStatus, reason string) error {
	return p.stream.Send(&agentv1.ApplyArtifactEvent{Event: &agentv1.ApplyArtifactEvent_Outcome{
		Outcome: &agentv1.TransferOutcome{Name: name, Status: status, Reason: reason},
	}})
}

func (s *FileServer) DiscardArtifact(_ context.Context, req *agentv1.DiscardArtifactRequest) (*agentv1.DiscardArtifactResponse, error) {
	transferID, err := cleanTransferID(req.GetTransferId())
	if err != nil {
		return nil, err
	}
	s.artifactsMu.Lock()
	a := s.artifacts[transferID][req.GetArtifact()]
	if a != nil && a.inUse {
		a = nil
	}
	s.artifactsMu.Unlock()
	if a == nil {
		return nil, status.Error(codes.NotFound, "discard artifact: unknown or busy artifact")
	}
	s.discardArtifact(a)
	return &agentv1.DiscardArtifactResponse{}, nil
}
