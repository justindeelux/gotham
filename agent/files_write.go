package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fileFreeReserveDivisor is the fraction of a filesystem a commit keeps free:
// a write is refused when less than 1/N of the filesystem is available, so a
// full disk does not leave the node unable to write its own state.
const fileFreeReserveDivisor = 20

// fileProgressInterval throttles Copy/Delete progress so a large tree does not
// emit one message per entry.
const fileProgressInterval = 250 * time.Millisecond

// Write creates or replaces one file. The first message must be a WriteHeader;
// the following messages stream the body; an optional WriteCommit carries the
// body digest. The body is staged in a temporary file in the target's own
// directory and only committed once every declared byte has arrived, so a
// truncated or aborted stream never replaces the target.
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
	// The staging file never survives the call: on success the target is a
	// rename of it or a hard link to it (so unlinking the name is harmless),
	// and on any failure removing it leaves the target untouched.
	defer func() {
		_ = tmp.Close()
		_ = root.Remove(tmpRel)
	}()

	hasher := sha256.New()
	var received int64
	var digest string
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return streamRecvError("write", recvErr)
		}
		switch part := msg.GetPart().(type) {
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

	response, err := s.commitWrite(root, rel, tmpRel, mode, header.GetExpectEtag(), header.GetOverwrite())
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}

// commitWrite decides, under the path's lock, whether to create, rename over or
// write in place, and applies the caller's conflict policy. The per-path lock
// makes the etag check and the commit atomic with respect to other FileServer
// writers, so a second writer always sees the first writer's etag.
func (s *FileServer) commitWrite(root *fsroot.Root, rel, tmpRel string, mode os.FileMode, expect string, overwrite bool) (*agentv1.WriteResponse, error) {
	lock := s.pathLock(root.Name(), rel)
	lock.Lock()
	defer lock.Unlock()

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
		// Link, not rename, so the create stays exclusive even if a container
		// creates the name between the stat above and the commit.
		if err := root.Link(tmpRel, rel); err != nil {
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
		if expect != "" && existing.Etag != expect && !overwrite {
			return nil, status.Error(codes.Aborted, "write: the file changed since it was read")
		}
		if expect == "" && !overwrite {
			return nil, status.Error(codes.AlreadyExists, "write: the target already exists")
		}
		if existing.UID == uint32(os.Geteuid()) && existing.Nlink == 1 {
			// An agent-owned, single-link file is safe to replace by rename.
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
			// A foreign, hard-linked or bind-mounted file must keep its inode,
			// owner and ACLs, so its bytes are overwritten in place.
			if err := s.overwriteInPlace(root, rel, tmpRel, existing.Etag); err != nil {
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

// overwriteInPlace copies the staged body over an existing file, truncating and
// rewriting the same inode so hardlinks, ownership, ACLs and a bind mount of a
// single file all keep working. It re-checks the etag on the opened descriptor
// so a change by a container during the upload is a conflict, not a silent
// overwrite.
func (s *FileServer) overwriteInPlace(root *fsroot.Root, rel, tmpRel, expectEtag string) error {
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
	if err := dst.Truncate(0); err != nil {
		return fileError("write", err)
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return fileError("write", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fileError("write", err)
	}
	if err := dst.Sync(); err != nil {
		return fileError("write", err)
	}
	return nil
}

// Mkdir creates one directory. Creation is exclusive: an existing name is a
// conflict.
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

// Rename renames or moves one entry within a single root, refusing an
// application directory at the top of the volumes root and any move into the
// source's own subtree.
func (s *FileServer) Rename(_ context.Context, req *agentv1.RenameRequest) (*agentv1.RenameResponse, error) {
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
	if src == "." || dst == "." || src == dst || isWithin(src, dst) {
		return nil, status.Errorf(codes.InvalidArgument, "rename: cannot move %q to %q", req.GetSource(), req.GetDestination())
	}
	if s.protectsAppDir(root, src) || s.protectsAppDir(root, dst) {
		return nil, status.Error(codes.PermissionDenied, "rename: an application directory cannot be renamed or replaced")
	}
	if _, err := root.Stat(src); err != nil {
		return nil, fileError("rename", err)
	}
	if _, err := root.Stat(dst); err == nil {
		if !req.GetOverwrite() {
			return nil, status.Errorf(codes.AlreadyExists, "rename: %q already exists", dst)
		}
	} else if !errors.Is(err, fsroot.ErrNotFound) {
		return nil, fileError("rename", err)
	}
	if err := s.moveEntry(root, src, dst, req.GetOverwrite()); err != nil {
		return nil, fileError("rename", err)
	}
	entry, err := root.Stat(dst)
	if err != nil {
		return nil, fileError("rename", err)
	}
	return &agentv1.RenameResponse{Entry: fileEntry(entry)}, nil
}

// moveEntry renames within the root, falling back to a copy and delete when a
// bind mount inside the root makes the two paths different filesystems (EXDEV).
func (s *FileServer) moveEntry(root *fsroot.Root, src, dst string, overwrite bool) error {
	err := root.Rename(src, dst)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if copyErr := root.Copy(src, dst, overwrite, nil); copyErr != nil {
		return copyErr
	}
	return root.RemoveTree(src, nil)
}

// Copy copies one entry within a single root, streaming progress and refusing a
// copy into the source's own subtree.
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
	if err := root.Copy(src, dst, req.GetOverwrite(), progress.entry); err != nil {
		return fileError("copy", err)
	}
	return progress.finish()
}

// Delete removes one entry, recursively when asked, streaming progress. The
// root itself and the top-level application directories of the volumes root are
// never removed.
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
	if s.protectsAppDir(root, rel) {
		return status.Error(codes.PermissionDenied, "delete: an application directory cannot be deleted")
	}
	if _, err := root.Stat(rel); err != nil {
		return fileError("delete", err)
	}
	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()

	progress := &progressWriter{stream: stream, path: rel}
	if req.GetRecursive() {
		if err := root.RemoveTree(rel, progress.entry); err != nil {
			return fileError("delete", err)
		}
	} else if err := root.Remove(rel); err != nil {
		return fileError("delete", err)
	}
	return progress.finish()
}

// progressWriter streams throttled Copy/Delete progress and a final done
// message. It reports the bytes of each completed entry.
type progressWriter struct {
	stream    grpc.ServerStreamingServer[agentv1.Progress]
	path      string
	bytesDone int64
	last      time.Time
}

// entry records one completed entry, emitting a progress message at most every
// fileProgressInterval so a large tree does not flood the stream.
func (p *progressWriter) entry(rel string, e fsroot.Entry) error {
	if e.Kind == fsroot.KindRegular {
		p.bytesDone += e.Size
	}
	now := time.Now()
	if now.Sub(p.last) < fileProgressInterval {
		return nil
	}
	p.last = now
	return p.stream.Send(&agentv1.Progress{Path: rel, BytesDone: p.bytesDone, BytesTotal: -1})
}

// finish sends the terminal progress message.
func (p *progressWriter) finish() error {
	return p.stream.Send(&agentv1.Progress{Path: p.path, BytesDone: p.bytesDone, BytesTotal: -1, Done: true})
}

// pathLock returns the striped mutex guarding a (root, path) commit.
func (s *FileServer) pathLock(rootName, rel string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(rootName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(rel))
	return &s.locks[int(h.Sum32())%len(s.locks)]
}

// beginTransfer acquires a transfer slot or refuses when the node already has
// the maximum number in flight.
func (s *FileServer) beginTransfer() (func(), error) {
	select {
	case s.transfers <- struct{}{}:
		return func() { <-s.transfers }, nil
	default:
		return nil, status.Error(codes.ResourceExhausted, "file service is busy: too many concurrent transfers")
	}
}

// lowSpace reports whether committing the write would eat into the filesystem's
// free-space reserve.
func (s *FileServer) lowSpace(root *fsroot.Root) bool {
	total := totalBytes(root.Path())
	if total == 0 {
		return false
	}
	return freeBytes(root.Path()) < total/fileFreeReserveDivisor
}

// protectsAppDir reports whether rel names a top-level application directory
// of the writable managed-volumes root: a directory whose name is a canonical
// application (UUID) id. A running container bind-mounts that directory, so it
// must not be renamed or deleted. Ordinary directories a user creates are not
// protected.
func (s *FileServer) protectsAppDir(root *fsroot.Root, rel string) bool {
	if s.volumeRootPath == "" || root.Path() != s.volumeRootPath {
		return false
	}
	clean := path.Clean(rel)
	if clean == "." || strings.Contains(clean, "/") {
		return false
	}
	id, err := parseAppID(clean)
	if err != nil || id.String() != clean {
		return false
	}
	entry, err := root.Stat(clean)
	return err == nil && entry.Kind == fsroot.KindDirectory
}

// streamRecvError maps a client-stream receive error: an early EOF is a
// malformed request, a gRPC status is passed through, and anything else is a
// filesystem error.
func streamRecvError(action string, err error) error {
	if errors.Is(err, io.EOF) {
		return status.Errorf(codes.InvalidArgument, "%s: the stream ended before a header", action)
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return fileError(action, err)
}

// isWithin reports whether child lies strictly inside the parent subtree of a
// root-relative path.
func isWithin(parent, child string) bool {
	return strings.HasPrefix(child, parent+"/")
}
