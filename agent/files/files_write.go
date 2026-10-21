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

// fileFreeReserveDivisor is the fraction of a filesystem a commit keeps free:
// a write is refused when less than 1/N of the filesystem is available, so a
// full disk does not leave the node unable to write its own state.
const fileFreeReserveDivisor = 20

// fileProgressInterval throttles Copy/Delete progress so a large tree does not
// emit one message per entry.
const fileProgressInterval = 250 * time.Millisecond

// fileRecvIdleTimeout bounds how long a Write may hold a transfer slot without
// receiving a message. It is reset on every message, so a slow but live upload
// is never cut off; only a stalled peer trips it and releases the slot. It is a
// variable so a test can lower it.
var fileRecvIdleTimeout = 2 * time.Minute

// fileDiskUsage reports (free, total) bytes on a path's filesystem. It is a
// variable so a test can simulate a full disk.
var fileDiskUsage = func(dir string) (free, total uint64) {
	return freeBytes(dir), totalBytes(dir)
}

// fileFreeBytesOn reports the free bytes on an open file's own filesystem. It
// is read from the target descriptor, not the root path, so a bind-mounted
// single file that lives on another filesystem is judged against the filesystem
// it is actually written to. It returns 0 when statfs cannot be read, which the
// in-place path treats as "unknown, refuse". It is a variable so a test can
// simulate a full disk.
var fileFreeBytesOn = func(f *os.File) uint64 {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &stat); err != nil {
		return 0
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

// renameWithin renames one entry within a root, honouring the overwrite policy.
// It is a variable so a test can force the cross-device (EXDEV) fallback in
// moveEntry without a real second mount.
var renameWithin = func(root *fsroot.Root, src, dst string, overwrite bool) error {
	if overwrite {
		return root.Rename(src, dst)
	}
	return root.RenameExclusive(src, dst)
}

// writeRecv is one asynchronously received Write message.
type writeRecv struct {
	msg *agentv1.WriteRequest
	err error
}

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
	// The staging file never survives the call: on success the target is a
	// rename of it (or its bytes are copied into an existing inode), and on any
	// failure removing it leaves the target untouched.
	defer func() {
		_ = tmp.Close()
		_ = root.Remove(tmpRel)
	}()

	hasher := sha256.New()
	var received int64
	var digest string

	// Receive on a helper goroutine so the idle timer can fire while the caller
	// is blocked in Recv; returning from the handler closes the stream, which
	// unblocks the goroutine.
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

// commitWrite decides, under the path's lock, whether to create, rename over or
// write in place, and applies the caller's conflict policy. The per-path lock
// makes the etag check and the commit atomic with respect to other FileServer
// writers, so a second writer always sees the first writer's etag.
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
		// A non-replacing rename keeps the create exclusive even if a container
		// creates the name between the stat above and the commit, and works on
		// filesystems without hard links (where a link-based create cannot).
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
		// A supplied etag that no longer matches is a failed precondition even
		// when overwrite is set: overwrite authorises replacing a target the
		// caller has not read, never silently clobbering a newer one.
		if expect != "" && existing.Etag != expect {
			return nil, status.Error(codes.FailedPrecondition, "write: the file changed since it was read")
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

// overwriteInPlace copies the staged body over an existing file, rewriting the
// same inode so hardlinks, ownership, ACLs and a bind mount of a single file
// all keep working. It re-checks the etag on the opened descriptor so a change
// by a container during the upload is a conflict, not a silent overwrite.
//
// The rewrite is deliberately not atomic (the in-place semantic exists to
// preserve the inode, owner and bind mounts), so the two gaps are closed where
// they can be: the free-space check reads the opened target's own filesystem
// (0 free means refuse unless the body is empty), and a growing file is
// fallocated to its new length before any byte is written, so an out-of-space
// failure happens with the old contents still intact. A crash or I/O error
// between the reservation and the final fsync can still leave the file
// half-written; that residue is the documented limitation, not a truncate-then-
// copy window.
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
	// free == 0 means statfs failed; refuse any non-empty body rather than
	// guessing. An empty body needs no space.
	if free := fileFreeBytesOn(dst); free < uint64(incoming) {
		return status.Errorf(codes.ResourceExhausted, "write: %d bytes do not fit in %d free bytes", incoming, free)
	}
	if incoming > entry.Size {
		if err := preallocate(dst, incoming); err != nil {
			if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
				return status.Errorf(codes.ResourceExhausted, "write: %d bytes do not fit", incoming)
			}
			// A filesystem without fallocate support: the free-space check
			// above already guarded the write, so continue.
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
	// A shrinking rewrite truncates after the write, so the old tail is never
	// exposed mid-copy; a growing one was sized by the reservation above.
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

// Chmod changes one entry's permission bits. The mode field is required: an
// absent mode is refused (InvalidArgument), never treated as 000. Only the
// 0o777 permission bits are accepted: any request carrying setuid, setgid or
// sticky (or any other bit above 0o777) is refused, and a special file or a
// symbolic link is refused — a link's target's mode is never changed. Existing
// setuid/setgid/sticky bits are preserved, not created. A directory must keep
// its owner execute bit so it stays traversable, and a hard-linked regular file
// in a confined root is refused (its inode may also be reachable outside the
// root). The entry keeps its type, so chmod-ing a managed application directory
// is allowed as long as it stays a directory (rename and delete keep their own
// app-directory guard). ChmodResponse is empty (the wired contract predates this
// step), so the control plane re-stats the entry after a successful chmod,
// exactly as Copy does.
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
	// mode is optional on the wire so an absent field is refused rather than
	// silently decoded as 000: a chmod has no safe default (unlike a create).
	if req.Mode == nil {
		return nil, status.Error(codes.InvalidArgument, "chmod: mode is required")
	}
	// Only the low 0o777 bits may change: setuid (0o4000), setgid (0o2000),
	// sticky (0o1000) and every higher bit are refused as one mask. Existing
	// special bits are preserved by fsroot.ChmodEntry, never created.
	mode := req.GetMode()
	if mode&^uint32(0o777) != 0 {
		return nil, status.Errorf(codes.InvalidArgument, "chmod: mode %#o carries bits above the 0o777 permission mask", mode)
	}
	// A hard link shares its inode with every other name, so chmod-ing it in a
	// confined root could change a file outside the root. In the
	// whole-filesystem root there is no "outside", so the check is moot.
	refuseHardlink := root.Name() != RootSystem
	if _, err := root.ChmodEntry(rel, os.FileMode(mode), refuseHardlink); err != nil {
		return nil, fileError("chmod", err)
	}
	return &agentv1.ChmodResponse{}, nil
}

// Rename renames or moves one entry within a single root, refusing an
// application directory at the top of the volumes root and any move into the
// source's own subtree. It takes the per-path locks for both names so a rename
// is serialised with writes and other renames, and refuses a destination that
// already exists unless overwrite is set.
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

// moveEntry renames within the root, refusing an existing destination when
// overwrite is false, and falling back to a guarded copy and delete when a bind
// mount inside the root makes the two paths different filesystems (EXDEV). The
// fallback acquires a transfer slot, checks free space and honours ctx. It does
// not remove an existing destination first: Copy stages the tree in dst's own
// directory and renames it into place, so a non-empty destination directory is
// refused exactly as a same-filesystem rename would be (ENOTEMPTY) and a failed
// copy leaves both the destination and the source intact.
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

// Copy copies one entry within a single root, streaming progress and refusing a
// copy into the source's own subtree. It honours the stream context so a
// cancelled copy aborts promptly.
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

// Delete removes one entry, streaming progress. By default the entry is moved
// into the root's recycle bin (ListTrash/Restore/Purge/EmptyTrash) instead of
// being removed, and a permanent delete is asked for explicitly. The root
// itself and the top-level application directories of the volumes root are
// never removed. It holds the per-path lock while the entry moves so it cannot
// race a write or rename, but it sends progress only after the lock is released,
// so a slow client cannot stall unrelated paths that share the stripe. It
// honours the stream context so a cancelled delete stops promptly.
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
		// Release the lock before any Send: a blocked client must not hold a
		// stripe that unrelated paths share.
		unlock()
		if moveErr != nil {
			return fileError("delete", moveErr)
		}
		// A recycle move is a single rename, so there is no per-entry progress
		// to stream; report the terminal done message.
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
	// Release the lock before any Send: a blocked client must not hold a stripe
	// that unrelated paths share.
	unlock()
	release()
	if walkErr != nil {
		_ = progress.flushBuffered()
		return fileError("delete", walkErr)
	}
	return progress.finish()
}

// progressWriter streams throttled Copy/Delete progress and a final done
// message. It reports the bytes of each completed entry. When buffered is
// non-nil it collects updates instead of sending them, so a caller can flush
// them after releasing a lock.
type progressWriter struct {
	stream    grpc.ServerStreamingServer[agentv1.Progress]
	path      string
	bytesDone int64
	last      time.Time
	buffered  *[]*agentv1.Progress
}

// entry records one completed entry, emitting a progress message at most every
// fileProgressInterval so a large tree does not flood the stream. While
// buffering, the throttled update is stored rather than sent.
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

// flushBuffered sends and clears any updates collected while buffering.
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

// finish flushes any buffered updates and sends the terminal progress message.
func (p *progressWriter) finish() error {
	if err := p.flushBuffered(); err != nil {
		return err
	}
	return p.stream.Send(&agentv1.Progress{Path: p.path, BytesDone: p.bytesDone, BytesTotal: -1, Done: true})
}

// lockPaths locks the striped path locks guarding every named path under a
// root, in a canonical order so two concurrent callers cannot deadlock, and
// returns a function that releases them. A rename locks both its source and
// destination; a write locks its target.
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

// pathLockIndex maps a (root, path) to a lock stripe. The hash is reduced
// modulo the stripe count as uint32 before the int conversion, so a 32-bit
// build never turns a large hash into a negative index.
func pathLockIndex(rootName, rel string, stripes int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(rootName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(rel))
	return int(h.Sum32() % uint32(stripes))
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
	free, total := fileDiskUsage(root.Path())
	if total == 0 {
		return false
	}
	return free < total/fileFreeReserveDivisor
}

// protectsAppDir reports whether rel resolves to a top-level application
// directory of the writable managed-volumes root: a name that is a canonical
// application (UUID) id whose resolved parent is the root itself. A running
// container bind-mounts that directory, so it must not be renamed or deleted.
// The decision is made on the resolved parent, never the lexical path, so a
// symlinked parent (for example "<appA>/up -> ..") cannot launder the path and
// reach a sibling application directory. Ordinary directories a user creates
// are not protected.
func (s *FileServer) protectsAppDir(root *fsroot.Root, rel string) bool {
	// wantParent is where the resolved parent of an application directory must
	// land: the root itself for the volumes root, or the volumes directory
	// inside the whole-filesystem root.
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
	// A directory is the app volume; a symlink with an app-directory name is
	// treated as protected too, so unlinking the name cannot detach a mount.
	return entry.Kind == fsroot.KindDirectory || entry.Kind == fsroot.KindSymlink
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
