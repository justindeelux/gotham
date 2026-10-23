package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const fileFreeReserveDivisor = 20

const fileProgressInterval = 250 * time.Millisecond

var fileRecvIdleTimeout = 2 * time.Minute

var fileDiskUsage = func(dir string) (free, total uint64) {
	return freeBytes(dir), totalBytes(dir)
}

var fileFreeBytesOn = func(f *os.File) uint64 {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &stat); err != nil {
		return 0
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

var renameWithin = func(root *fsroot.Root, src, dst string, overwrite bool) error {
	if overwrite {
		return root.Rename(src, dst)
	}
	return root.RenameExclusive(src, dst)
}

type writeRecv struct {
	msg *agentv1.WriteRequest
	err error
}

func (s *FileServer) Write(stream grpc.ClientStreamingServer[agentv1.WriteRequest, agentv1.WriteResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return streamRecvError("write", err)
	}
	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "write: the first message must be a header")
	}
	root, err := s.root(header.GetRoot())
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "write: root %q is read-only", header.GetRoot())
	}
	rel, err := fsroot.Clean(header.GetPath())
	if err != nil || rel == "." {
		return status.Errorf(codes.InvalidArgument, "write: invalid path %q", header.GetPath())
	}
	if err := guardTrash(root, "write", rel); err != nil {
		return err
	}
	size := header.GetSize()
	if size < 0 {
		return status.Error(codes.InvalidArgument, "write: size must not be negative")
	}
	if s.maxUpload > 0 && size > s.maxUpload {
		return status.Errorf(codes.ResourceExhausted, "write: %d bytes exceeds the %d byte upload limit", size, s.maxUpload)
	}
	mode := os.FileMode(header.GetMode()) & 0o777
	if mode == 0 {
		mode = 0o644
	}
	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "write: the filesystem is low on free space")
	}

	tmp, tmpRel, err := root.CreateTemp(path.Dir(rel), 0o600)
	if err != nil {
		return fileError("write", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = root.Remove(tmpRel)
	}()

	hasher := sha256.New()
	var received int64
	var digest string

	parts := make(chan writeRecv, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			msg, recvErr := stream.Recv()
			select {
			case parts <- writeRecv{msg: msg, err: recvErr}:
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
receive:
	for {
		select {
		case <-stream.Context().Done():
			return status.FromContextError(stream.Context().Err()).Err()
		case <-idle.C:
			return status.Errorf(codes.DeadlineExceeded, "write: no data for %s; aborting the upload", fileRecvIdleTimeout)
		case result := <-parts:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(fileRecvIdleTimeout)
			if errors.Is(result.err, io.EOF) {
				break receive
			}
			if result.err != nil {
				return streamRecvError("write", result.err)
			}
			switch part := result.msg.GetPart().(type) {
			case *agentv1.WriteRequest_Data:
				received += int64(len(part.Data))
				if received > size || (s.maxUpload > 0 && received > s.maxUpload) {
					return status.Errorf(codes.InvalidArgument, "write: the body exceeds the declared size of %d bytes", size)
				}
				if _, writeErr := tmp.Write(part.Data); writeErr != nil {
					return fileError("write", writeErr)
				}
				_, _ = hasher.Write(part.Data)
			case *agentv1.WriteRequest_Commit:
				digest = strings.TrimSpace(part.Commit.GetSha256())
			case *agentv1.WriteRequest_Header:
				return status.Error(codes.InvalidArgument, "write: unexpected second header")
			default:
				return status.Error(codes.InvalidArgument, "write: empty message")
			}
		}
	}
	if received != size {
		return status.Errorf(codes.Aborted, "write: received %d of %d bytes; the upload was truncated", received, size)
	}
	if digest != "" {
		if sum := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(sum, digest) {
			return status.Error(codes.Aborted, "write: the body digest does not match the committed digest")
		}
	}
	if err := tmp.Sync(); err != nil {
		return fileError("write", err)
	}
	if err := tmp.Close(); err != nil {
		return fileError("write", err)
	}

	response, err := s.commitWrite(root, rel, tmpRel, mode, header.GetExpectEtag(), header.GetOverwrite(), size)
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}

func (s *FileServer) commitWrite(root *fsroot.Root, rel, tmpRel string, mode os.FileMode, expect string, overwrite bool, size int64) (*agentv1.WriteResponse, error) {
	unlock := s.lockPaths(root.Name(), rel)
	defer unlock()

	existing, statErr := root.Stat(rel)
	created := errors.Is(statErr, fsroot.ErrNotFound)
	writeMode := "rename"
	switch {
	case created:
		if expect != "" {
			return nil, status.Error(codes.Aborted, "write: the target no longer exists")
		}
		if err := root.Chmod(tmpRel, mode); err != nil {
			return nil, fileError("write", err)
		}
		if err := root.RenameExclusive(tmpRel, rel); err != nil {
			if errors.Is(err, fsroot.ErrExists) {
				return nil, status.Error(codes.AlreadyExists, "write: the target already exists")
			}
			return nil, fileError("write", err)
		}
	case statErr != nil:
		return nil, fileError("write", statErr)
	default:
		if existing.Kind != fsroot.KindRegular {
			return nil, status.Errorf(codes.FailedPrecondition, "write: %q is not a regular file", rel)
		}
		if expect != "" && existing.Etag != expect {
			return nil, status.Error(codes.FailedPrecondition, "write: the file changed since it was read")
		}
		if expect == "" && !overwrite {
			return nil, status.Error(codes.AlreadyExists, "write: the target already exists")
		}
		if existing.UID == uint32(os.Geteuid()) && existing.Nlink == 1 {
			if now, err := root.Stat(rel); err != nil || now.Etag != existing.Etag {
				return nil, status.Error(codes.Aborted, "write: the file changed during the upload")
			}
			if err := root.Chmod(tmpRel, existing.Mode.Perm()); err != nil {
				return nil, fileError("write", err)
			}
			if err := root.Rename(tmpRel, rel); err != nil {
				return nil, fileError("write", err)
			}
		} else {
			if err := s.overwriteInPlace(root, rel, tmpRel, existing.Etag, size); err != nil {
				return nil, err
			}
			writeMode = "in_place"
		}
	}

	entry, err := root.Stat(rel)
	if err != nil {
		return nil, fileError("write", err)
	}
	return &agentv1.WriteResponse{Entry: fileEntry(entry), Created: created, WriteMode: writeMode}, nil
}

func (s *FileServer) overwriteInPlace(root *fsroot.Root, rel, tmpRel, expectEtag string, incoming int64) error {
	src, _, err := root.OpenRegular(tmpRel)
	if err != nil {
		return fileError("write", err)
	}
	defer func() { _ = src.Close() }()

	dst, entry, err := root.OpenWrite(rel)
	if err != nil {
		return fileError("write", err)
	}
	defer func() { _ = dst.Close() }()
	if entry.Etag != expectEtag {
		return status.Error(codes.Aborted, "write: the file changed during the upload")
	}
	if free := fileFreeBytesOn(dst); free < uint64(incoming) {
		return status.Errorf(codes.ResourceExhausted, "write: %d bytes do not fit in %d free bytes", incoming, free)
	}
	if incoming > entry.Size {
		if err := preallocate(dst, incoming); err != nil {
			if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
				return status.Errorf(codes.ResourceExhausted, "write: %d bytes do not fit", incoming)
			}
			if !errors.Is(err, syscall.EOPNOTSUPP) && !errors.Is(err, syscall.ENOSYS) && !errors.Is(err, syscall.EINVAL) {
				return fileError("write", err)
			}
		}
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return fileError("write", err)
	}
	written, err := io.Copy(dst, src)
	if err != nil {
		return fileError("write", err)
	}
	if written != incoming {
		return status.Errorf(codes.Aborted, "write: staging holds %d bytes, want %d", written, incoming)
	}
	if incoming < entry.Size {
		if err := dst.Truncate(incoming); err != nil {
			return fileError("write", err)
		}
	}
	if err := dst.Sync(); err != nil {
		return fileError("write", err)
	}
	return nil
}

func (s *FileServer) Mkdir(_ context.Context, req *agentv1.MkdirRequest) (*agentv1.MkdirResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if !root.Writable() {
		return nil, status.Errorf(codes.PermissionDenied, "mkdir: root %q is read-only", req.GetRoot())
	}
	rel, err := fsroot.Clean(req.GetPath())
	if err != nil || rel == "." {
		return nil, status.Errorf(codes.InvalidArgument, "mkdir: invalid path %q", req.GetPath())
	}
	if err := guardTrash(root, "mkdir", rel); err != nil {
		return nil, err
	}
	mode := os.FileMode(req.GetMode()) & 0o777
	if mode == 0 {
		mode = 0o755
	}
	entry, err := root.Mkdir(rel, mode)
	if err != nil {
		return nil, fileError("mkdir", err)
	}
	return &agentv1.MkdirResponse{Entry: fileEntry(entry)}, nil
}

func (s *FileServer) Chmod(_ context.Context, req *agentv1.ChmodRequest) (*agentv1.ChmodResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if !root.Writable() {
		return nil, status.Errorf(codes.PermissionDenied, "chmod: root %q is read-only", req.GetRoot())
	}
	rel, err := fsroot.Clean(req.GetPath())
	if err != nil || rel == "." {
		return nil, status.Errorf(codes.InvalidArgument, "chmod: invalid path %q", req.GetPath())
	}
	if err := guardTrash(root, "chmod", rel); err != nil {
		return nil, err
	}
	if req.Mode == nil {
		return nil, status.Error(codes.InvalidArgument, "chmod: mode is required")
	}
	mode := req.GetMode()
	if mode&^uint32(0o777) != 0 {
		return nil, status.Errorf(codes.InvalidArgument, "chmod: mode %#o carries bits above the 0o777 permission mask", mode)
	}
	refuseHardlink := root.Name() != RootSystem
	if _, err := root.ChmodEntry(rel, os.FileMode(mode), refuseHardlink); err != nil {
		return nil, fileError("chmod", err)
	}
	return &agentv1.ChmodResponse{}, nil
}

func (s *FileServer) Rename(ctx context.Context, req *agentv1.RenameRequest) (*agentv1.RenameResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if !root.Writable() {
		return nil, status.Errorf(codes.PermissionDenied, "rename: root %q is read-only", req.GetRoot())
	}
	src, err := fsroot.Clean(req.GetSource())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "rename: invalid source %q", req.GetSource())
	}
	dst, err := fsroot.Clean(req.GetDestination())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "rename: invalid destination %q", req.GetDestination())
	}
	if err := guardTrash(root, "rename", src); err != nil {
		return nil, err
	}
	if err := guardTrash(root, "rename", dst); err != nil {
		return nil, err
	}
	if src == "." || dst == "." || src == dst || isWithin(src, dst) {
		return nil, status.Errorf(codes.InvalidArgument, "rename: cannot move %q to %q", req.GetSource(), req.GetDestination())
	}
	if s.protectsAppDir(root, src) || s.protectsAppDir(root, dst) {
		return nil, status.Error(codes.PermissionDenied, "rename: an application directory cannot be renamed or replaced")
	}
	unlock := s.lockPaths(root.Name(), src, dst)
	defer unlock()
	if err := s.moveEntry(ctx, root, src, dst, req.GetOverwrite()); err != nil {
		return nil, fileError("rename", err)
	}
	entry, err := root.Stat(dst)
	if err != nil {
		return nil, fileError("rename", err)
	}
	return &agentv1.RenameResponse{Entry: fileEntry(entry)}, nil
}

func (s *FileServer) moveEntry(ctx context.Context, root *fsroot.Root, src, dst string, overwrite bool) error {
	err := renameWithin(root, src, dst, overwrite)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "rename: the filesystem is low on free space")
	}
	if err := root.Copy(ctx, src, dst, overwrite, nil); err != nil {
		return err
	}
	return root.RemoveTree(ctx, src, nil)
}

func (s *FileServer) Copy(req *agentv1.CopyRequest, stream grpc.ServerStreamingServer[agentv1.Progress]) error {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "copy: root %q is read-only", req.GetRoot())
	}
	src, err := fsroot.Clean(req.GetSource())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "copy: invalid source %q", req.GetSource())
	}
	dst, err := fsroot.Clean(req.GetDestination())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "copy: invalid destination %q", req.GetDestination())
	}
	if err := guardTrash(root, "copy", src); err != nil {
		return err
	}
	if err := guardTrash(root, "copy", dst); err != nil {
		return err
	}
	if src == "." || dst == "." || src == dst || isWithin(src, dst) {
		return status.Errorf(codes.InvalidArgument, "copy: cannot copy %q to %q", req.GetSource(), req.GetDestination())
	}
	if s.protectsAppDir(root, dst) {
		return status.Error(codes.PermissionDenied, "copy: an application directory cannot be replaced")
	}
	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "copy: the filesystem is low on free space")
	}

	progress := &progressWriter{stream: stream, path: dst}
	if err := root.Copy(stream.Context(), src, dst, req.GetOverwrite(), progress.entry); err != nil {
		return fileError("copy", err)
	}
	return progress.finish()
}

func (s *FileServer) Delete(req *agentv1.DeleteRequest, stream grpc.ServerStreamingServer[agentv1.Progress]) error {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "delete: root %q is read-only", req.GetRoot())
	}
	rel, err := fsroot.Clean(req.GetPath())
	if err != nil || rel == "." {
		return status.Errorf(codes.InvalidArgument, "delete: invalid path %q", req.GetPath())
	}
	if err := guardTrash(root, "delete", rel); err != nil {
		return err
	}
	if s.protectsAppDir(root, rel) {
		return status.Error(codes.PermissionDenied, "delete: an application directory cannot be deleted")
	}
	if _, err := root.Stat(rel); err != nil {
		return fileError("delete", err)
	}
	if !req.GetPermanent() {
		unlock := s.lockPaths(root.Name(), rel)
		moveErr := s.trashEntry(stream.Context(), root, rel, req.GetRecursive())
		unlock()
		if moveErr != nil {
			return fileError("delete", moveErr)
		}
		return (&progressWriter{stream: stream, path: rel}).finish()
	}
	unlock := s.lockPaths(root.Name(), rel)
	release, err := s.beginTransfer()
	if err != nil {
		unlock()
		return err
	}
	var buffered []*agentv1.Progress
	progress := &progressWriter{stream: stream, path: rel, buffered: &buffered}
	var walkErr error
	if req.GetRecursive() {
		walkErr = root.RemoveTree(stream.Context(), rel, progress.entry)
	} else {
		walkErr = root.Remove(rel)
	}
	unlock()
	release()
	if walkErr != nil {
		_ = progress.flushBuffered()
		return fileError("delete", walkErr)
	}
	return progress.finish()
}

type progressWriter struct {
	stream    grpc.ServerStreamingServer[agentv1.Progress]
	path      string
	bytesDone int64
	last      time.Time
	buffered  *[]*agentv1.Progress
}

func (p *progressWriter) entry(rel string, e fsroot.Entry) error {
	if e.Kind == fsroot.KindRegular {
		p.bytesDone += e.Size
	}
	now := time.Now()
	if now.Sub(p.last) < fileProgressInterval {
		return nil
	}
	p.last = now
	msg := &agentv1.Progress{Path: rel, BytesDone: p.bytesDone, BytesTotal: -1}
	if p.buffered != nil {
		*p.buffered = append(*p.buffered, msg)
		return nil
	}
	return p.stream.Send(msg)
}

func (p *progressWriter) flushBuffered() error {
	if p.buffered == nil {
		return nil
	}
	for _, msg := range *p.buffered {
		if err := p.stream.Send(msg); err != nil {
			return err
		}
	}
	*p.buffered = nil
	return nil
}

func (p *progressWriter) finish() error {
	if err := p.flushBuffered(); err != nil {
		return err
	}
	return p.stream.Send(&agentv1.Progress{Path: p.path, BytesDone: p.bytesDone, BytesTotal: -1, Done: true})
}

func (s *FileServer) lockPaths(rootName string, rels ...string) func() {
	indices := make([]int, 0, len(rels))
	seen := make(map[int]struct{}, len(rels))
	for _, rel := range rels {
		idx := pathLockIndex(rootName, rel, len(s.locks))
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		s.locks[idx].Lock()
	}
	return func() {
		for i := len(indices) - 1; i >= 0; i-- {
			s.locks[indices[i]].Unlock()
		}
	}
}

func pathLockIndex(rootName, rel string, stripes int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(rootName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(rel))
	return int(h.Sum32() % uint32(stripes))
}

func (s *FileServer) beginTransfer() (func(), error) {
	select {
	case s.transfers <- struct{}{}:
		return func() { <-s.transfers }, nil
	default:
		return nil, status.Error(codes.ResourceExhausted, "file service is busy: too many concurrent transfers")
	}
}

func (s *FileServer) lowSpace(root *fsroot.Root) bool {
	free, total := fileDiskUsage(root.Path())
	if total == 0 {
		return false
	}
	return free < total/fileFreeReserveDivisor
}

func (s *FileServer) protectsAppDir(root *fsroot.Root, rel string) bool {
	wantParent := "."
	switch {
	case s.volumeRootPath != "" && root.Path() == s.volumeRootPath:
	case s.volumeRootRel != "" && root.Name() == RootSystem:
		resolved, err := root.Resolve(s.volumeRootRel)
		if err != nil {
			return false
		}
		wantParent = resolved
	default:
		return false
	}
	clean := path.Clean(rel)
	if clean == "." {
		return false
	}
	base := path.Base(clean)
	id, err := uuid.Parse(base)
	if err != nil || id.String() != base {
		return false
	}
	parent, err := root.Resolve(path.Dir(clean))
	if err != nil || parent != wantParent {
		return false
	}
	entry, err := root.Stat(clean)
	if err != nil {
		return false
	}
	return entry.Kind == fsroot.KindDirectory || entry.Kind == fsroot.KindSymlink
}

func streamRecvError(action string, err error) error {
	if errors.Is(err, io.EOF) {
		return status.Errorf(codes.InvalidArgument, "%s: the stream ended before a header", action)
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return fileError(action, err)
}

func isWithin(parent, child string) bool {
	return strings.HasPrefix(child, parent+"/")
}
