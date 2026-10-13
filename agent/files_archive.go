package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fileArchiveRatioCap is the zip-bomb guard: the total uncompressed bytes of
// an archive must stay under this multiple of the archive's own size. A
// legitimate archive of already-compressed data sits near 1; an archive that
// expands a few bytes into gigabytes trips it long before the byte cap does.
const fileArchiveRatioCap = 200

// fileArchiveDepth bounds how deep a compress walk or extract path may nest. A
// healthy tree is far shallower; the bound turns a pathological archive into an
// explicit error instead of unbounded recursion.
const fileArchiveDepth = 1024

// fileArchiveDirMode is the mode a directory created implicitly by an extract
// gets when the archive carries no mode for it.
const fileArchiveDirMode os.FileMode = 0o755

// archiveFormatFromName returns the archive format the file's extension
// selects. An unknown or missing extension reports ok=false, so an extract
// never guesses how to unpack a body.
func archiveFormatFromName(name string) (agentv1.ArchiveFormat, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ, true
	case strings.HasSuffix(lower, ".tar"):
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR, true
	case strings.HasSuffix(lower, ".zip"):
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, true
	default:
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED, false
	}
}

// archiveCounter enforces the archive limits: a maximum entry count, a maximum
// total uncompressed byte count, and a compression-ratio ceiling relative to
// the archive's own size. archiveBytes is 0 for compress, which disables the
// ratio check (the input is not compressed).
type archiveCounter struct {
	maxEntries   int64
	maxBytes     int64
	archiveBytes int64
	entries      int64
	bytes        int64
	// skipped counts entries a compress left out (symlinks, special files).
	skipped int
}

// addEntry accounts for one archive entry of the given uncompressed size. It is
// always called with the header's declared size before any byte is written, so
// an over-limit archive is refused before it can fill the disk.
func (c *archiveCounter) addEntry(size int64) error {
	if size < 0 {
		return fmt.Errorf("%w: archive entry has a negative size", fsroot.ErrInvalid)
	}
	c.entries++
	if c.entries > c.maxEntries {
		return fmt.Errorf("%w: archive has more than %d entries", fsroot.ErrTooLarge, c.maxEntries)
	}
	c.bytes += size
	if c.bytes > c.maxBytes {
		return fmt.Errorf("%w: archive expands beyond %d bytes", fsroot.ErrTooLarge, c.maxBytes)
	}
	if c.archiveBytes > 0 && c.bytes > c.archiveBytes && c.bytes/c.archiveBytes > fileArchiveRatioCap {
		return fmt.Errorf("%w: archive expands by more than %d:1", fsroot.ErrTooLarge, fileArchiveRatioCap)
	}
	return nil
}

// archiveProgress streams throttled Compress/Extract progress and a final done
// message carrying the skipped-entry count.
type archiveProgress struct {
	stream    grpc.ServerStreamingServer[agentv1.Progress]
	path      string
	bytesDone int64
	last      time.Time
}

// add records n completed bytes and emits a progress message at most every
// fileProgressInterval, so a large tree does not flood the stream.
func (p *archiveProgress) add(name string, n int64) error {
	p.bytesDone += n
	now := time.Now()
	if now.Sub(p.last) < fileProgressInterval {
		return nil
	}
	p.last = now
	return p.stream.Send(&agentv1.Progress{Path: name, BytesDone: p.bytesDone, BytesTotal: -1})
}

// done sends the terminal progress message.
func (p *archiveProgress) done(skipped uint64) error {
	return p.stream.Send(&agentv1.Progress{
		Path: p.path, BytesDone: p.bytesDone, BytesTotal: -1, Done: true, Skipped: skipped,
	})
}

// Compress builds one archive from a set of sources inside a single writable
// root and writes it to a destination inside the same root. Only regular files
// and directories are stored; a symbolic link or special file is skipped and
// reported in the final progress message. The archive is staged in a temporary
// file in the destination's own directory and renamed into place only once it
// is complete, so a failed or cancelled compress never leaves a partial archive
// and an unwritten destination is never replaced.
func (s *FileServer) Compress(req *agentv1.CompressRequest, stream grpc.ServerStreamingServer[agentv1.Progress]) error {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "compress: root %q is read-only", req.GetRoot())
	}
	format := req.GetFormat()
	switch format {
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ:
	default:
		return status.Error(codes.InvalidArgument, "compress: an archive format is required")
	}
	dst, err := fsroot.Clean(req.GetDestination())
	if err != nil || dst == "." {
		return status.Errorf(codes.InvalidArgument, "compress: invalid destination %q", req.GetDestination())
	}
	if s.protectsAppDir(root, dst) {
		return status.Error(codes.PermissionDenied, "compress: an application directory cannot be replaced")
	}
	sources := make([]string, 0, len(req.GetSources()))
	for _, raw := range req.GetSources() {
		rel, err := fsroot.Clean(raw)
		if err != nil || rel == "." {
			return status.Errorf(codes.InvalidArgument, "compress: invalid source %q", raw)
		}
		if rel == dst || isWithin(rel, dst) {
			return status.Errorf(codes.InvalidArgument, "compress: destination %q is inside source %q", req.GetDestination(), raw)
		}
		sources = append(sources, rel)
	}
	if len(sources) == 0 {
		return status.Error(codes.InvalidArgument, "compress: at least one source is required")
	}
	if parent, err := root.Stat(path.Dir(dst)); err != nil {
		return fileError("compress", err)
	} else if parent.Kind != fsroot.KindDirectory {
		return status.Errorf(codes.InvalidArgument, "compress: destination directory of %q is not a directory", dst)
	}
	if _, err := root.Stat(dst); err == nil {
		if !req.GetOverwrite() {
			return status.Errorf(codes.AlreadyExists, "compress: %q already exists", dst)
		}
	} else if !errors.Is(err, fsroot.ErrNotFound) {
		return fileError("compress", err)
	}

	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "compress: the filesystem is low on free space")
	}

	tmp, tmpRel, err := root.CreateTemp(path.Dir(dst), 0o600)
	if err != nil {
		return fileError("compress", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = root.Remove(tmpRel)
	}()

	counter := &archiveCounter{maxEntries: s.archiveMaxEntries, maxBytes: s.archiveMaxBytes}
	progress := &archiveProgress{stream: stream, path: dst}
	sink, closeSink, err := newArchiveSink(tmp, format)
	if err != nil {
		return fileError("compress", err)
	}
	for _, source := range sources {
		if err := s.compressEntry(stream.Context(), root, sink, source, path.Base(source), counter, progress, 0); err != nil {
			_ = closeSink()
			return fileError("compress", err)
		}
	}
	if err := closeSink(); err != nil {
		return fileError("compress", err)
	}
	if err := tmp.Sync(); err != nil {
		return fileError("compress", err)
	}
	if err := tmp.Close(); err != nil {
		return fileError("compress", err)
	}
	if req.GetOverwrite() {
		err = root.Rename(tmpRel, dst)
	} else {
		err = root.RenameExclusive(tmpRel, dst)
	}
	if err != nil {
		return fileError("compress", err)
	}
	return progress.done(uint64(counter.skipped))
}

// compressEntry stores one source (a file, a directory tree or a symlink) into
// the archive. rel is the source's path inside the root and name is the entry
// name the archive carries (the base name for a top-level source, then the
// source-relative path below it). A symbolic link, special file or other
// non-regular entry is skipped and counted, never followed. depth bounds the
// recursion.
func (s *FileServer) compressEntry(ctx context.Context, root *fsroot.Root, sink archiveSink, rel, name string, counter *archiveCounter, progress *archiveProgress, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > fileArchiveDepth {
		return fmt.Errorf("%w: archive nesting exceeds %d levels", fsroot.ErrTooLarge, fileArchiveDepth)
	}
	entry, err := root.Stat(rel)
	if err != nil {
		return err
	}
	switch entry.Kind {
	case fsroot.KindDirectory:
		if err := counter.addEntry(0); err != nil {
			return err
		}
		if err := sink.addDir(name, entry); err != nil {
			return err
		}
		return root.Names(rel, func(child string) error {
			return s.compressEntry(ctx, root, sink, path.Join(rel, child), path.Join(name, child), counter, progress, depth+1)
		})
	case fsroot.KindRegular:
		if err := counter.addEntry(entry.Size); err != nil {
			return err
		}
		file, _, err := root.OpenRegular(rel)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		if err := sink.addFile(name, entry, file); err != nil {
			return err
		}
		return progress.add(name, entry.Size)
	default:
		// A symlink, socket, device or FIFO is never stored or followed.
		counter.skipped++
		return nil
	}
}

// Extract unpacks one archive inside a single writable root. Every write goes
// through fsroot (os.Root), so a name that escapes the root — by "..", an
// absolute path or a symlinked parent — is refused. Symbolic-link, hard-link,
// device and FIFO entries are refused, setuid/setgid/sticky bits are stripped,
// and the entry count, total uncompressed size and compression ratio are
// bounded. An existing file is refused unless overwrite is set; an existing
// directory is reused, never removed.
func (s *FileServer) Extract(req *agentv1.ExtractRequest, stream grpc.ServerStreamingServer[agentv1.Progress]) error {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return err
	}
	if !root.Writable() {
		return status.Errorf(codes.PermissionDenied, "extract: root %q is read-only", req.GetRoot())
	}
	src, err := fsroot.Clean(req.GetSource())
	if err != nil || src == "." {
		return status.Errorf(codes.InvalidArgument, "extract: invalid source %q", req.GetSource())
	}
	format, ok := archiveFormatFromName(src)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "extract: %q is not a .zip, .tar or .tar.gz archive", req.GetSource())
	}
	dst, err := fsroot.Clean(req.GetDestination())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "extract: invalid destination %q", req.GetDestination())
	}
	if entry, err := root.Stat(dst); err != nil {
		return fileError("extract", err)
	} else if entry.Kind != fsroot.KindDirectory {
		return status.Errorf(codes.InvalidArgument, "extract: destination %q is not a directory", dst)
	}
	archive, entry, err := root.OpenRegular(src)
	if err != nil {
		return fileError("extract", err)
	}
	defer func() { _ = archive.Close() }()

	release, err := s.beginTransfer()
	if err != nil {
		return err
	}
	defer release()
	if s.lowSpace(root) {
		return status.Error(codes.ResourceExhausted, "extract: the filesystem is low on free space")
	}

	counter := &archiveCounter{maxEntries: s.archiveMaxEntries, maxBytes: s.archiveMaxBytes, archiveBytes: entry.Size}
	progress := &archiveProgress{stream: stream, path: dst}
	if err := s.extractArchive(stream.Context(), root, archive, entry.Size, format, dst, req.GetOverwrite(), counter, progress); err != nil {
		return fileError("extract", err)
	}
	return progress.done(0)
}

// extractArchive dispatches to the reader for the archive format.
func (s *FileServer) extractArchive(ctx context.Context, root *fsroot.Root, archive *os.File, size int64, format agentv1.ArchiveFormat, dst string, overwrite bool, counter *archiveCounter, progress *archiveProgress) error {
	switch format {
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP:
		zr, err := zip.NewReader(archive, size)
		if err != nil {
			return fmt.Errorf("%w: reading zip: %v", fsroot.ErrInvalid, err)
		}
		for _, zf := range zr.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.extractZipEntry(ctx, root, zf, dst, overwrite, counter, progress); err != nil {
				return err
			}
		}
		return nil
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR:
		return s.extractTar(ctx, root, archive, dst, overwrite, counter, progress)
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ:
		gz, err := gzip.NewReader(archive)
		if err != nil {
			return fmt.Errorf("%w: reading gzip: %v", fsroot.ErrInvalid, err)
		}
		defer func() { _ = gz.Close() }()
		return s.extractTar(ctx, root, gz, dst, overwrite, counter, progress)
	default:
		return fmt.Errorf("%w: unsupported archive format", fsroot.ErrInvalid)
	}
}

// extractZipEntry unpacks one zip member.
func (s *FileServer) extractZipEntry(ctx context.Context, root *fsroot.Root, zf *zip.File, dst string, overwrite bool, counter *archiveCounter, progress *archiveProgress) error {
	rel, err := archiveEntryPath(zf.Name)
	if err != nil {
		return err
	}
	if rel == "" {
		return nil
	}
	mode := zf.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return fmt.Errorf("%w: archive entry %q is a symbolic link", fsroot.ErrSpecial, zf.Name)
	case mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0:
		return fmt.Errorf("%w: archive entry %q is a special file", fsroot.ErrSpecial, zf.Name)
	}
	target := path.Join(dst, rel)
	if strings.HasSuffix(zf.Name, "/") || mode.IsDir() {
		return s.extractDir(root, target, mode.Perm())
	}
	if err := counter.addEntry(int64(zf.UncompressedSize64)); err != nil {
		return err
	}
	body, err := zf.Open()
	if err != nil {
		return fmt.Errorf("%w: opening archive entry %q: %v", fsroot.ErrInvalid, zf.Name, err)
	}
	defer func() { _ = body.Close() }()
	return s.extractFile(ctx, root, target, mode.Perm(), int64(zf.UncompressedSize64), overwrite, body, progress)
}

// extractTar unpacks a tar or tar.gz body.
func (s *FileServer) extractTar(ctx context.Context, root *fsroot.Root, r io.Reader, dst string, overwrite bool, counter *archiveCounter, progress *archiveProgress) error {
	tr := tar.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: reading tar: %v", fsroot.ErrInvalid, err)
		}
		rel, err := archiveEntryPath(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("%w: archive entry %q is a link", fsroot.ErrSpecial, hdr.Name)
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return fmt.Errorf("%w: archive entry %q is a device or FIFO", fsroot.ErrSpecial, hdr.Name)
		}
		if rel == "" {
			continue
		}
		target := path.Join(dst, rel)
		mode := os.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := s.extractDir(root, target, mode); err != nil {
				return err
			}
		case tar.TypeReg, 0:
			if err := counter.addEntry(hdr.Size); err != nil {
				return err
			}
			if err := s.extractFile(ctx, root, target, mode, hdr.Size, overwrite, tr, progress); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: archive entry %q has an unsupported type", fsroot.ErrSpecial, hdr.Name)
		}
	}
}

// extractDir creates one archive directory, reusing an existing directory and
// refusing to replace anything else.
func (s *FileServer) extractDir(root *fsroot.Root, target string, mode os.FileMode) error {
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "extract: an application directory cannot be replaced")
	}
	if mode == 0 {
		mode = fileArchiveDirMode
	}
	return root.MkdirAll(target, mode)
}

// extractFile writes one archive file. The body is copied from exactly size
// bytes of r, so a lying header cannot stream more than it declared. A partial
// file is removed on any failure so a failed extract leaves nothing behind. The
// running-app-directory guard refuses replacing a top-level application
// directory of the managed-volumes root; the byte/entry caps are enforced by the
// caller before this runs.
func (s *FileServer) extractFile(ctx context.Context, root *fsroot.Root, target string, mode os.FileMode, size int64, overwrite bool, r io.Reader, progress *archiveProgress) error {
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "extract: an application directory cannot be replaced")
	}
	if mode == 0 {
		mode = 0o644
	}
	if dir := path.Dir(target); dir != "." {
		if err := root.MkdirAll(dir, fileArchiveDirMode); err != nil {
			return err
		}
	}
	out, err := root.CreateFile(target, mode, overwrite)
	if err != nil {
		return err
	}
	written, copyErr := copyArchiveBody(ctx, out, r, size)
	if copyErr != nil {
		_ = out.Close()
		_ = root.Remove(target)
		return copyErr
	}
	if written != size {
		_ = out.Close()
		_ = root.Remove(target)
		return fmt.Errorf("%w: archive entry %q is truncated (%d of %d bytes)", fsroot.ErrInvalid, target, written, size)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = root.Remove(target)
		return err
	}
	if err := out.Close(); err != nil {
		_ = root.Remove(target)
		return err
	}
	// The process umask may have masked group/other bits; set the archive's exact
	// permission bits (setuid/setgid/sticky already stripped by Perm).
	if err := root.Chmod(target, mode); err != nil {
		return err
	}
	return progress.add(target, written)
}

// archiveEntryPath validates one archive member name and returns it as a
// root-relative path. An absolute name, a ".." segment, a NUL byte or a name
// that cleans to nothing is refused; a directory root entry ("." or "./")
// returns "" so the caller skips it.
func archiveEntryPath(name string) (string, error) {
	if strings.IndexByte(name, 0) >= 0 {
		return "", fmt.Errorf("%w: archive entry %q contains a NUL byte", fsroot.ErrInvalid, name)
	}
	slash := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(slash, "/") {
		return "", fmt.Errorf("%w: archive entry %q is absolute", fsroot.ErrInvalid, name)
	}
	for _, segment := range strings.Split(slash, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: archive entry %q escapes the destination", fsroot.ErrEscape, name)
		}
	}
	rel, err := fsroot.Clean(slash)
	if err != nil {
		return "", fmt.Errorf("%w: archive entry %q: %v", fsroot.ErrInvalid, name, err)
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}

// copyArchiveBody copies at most size bytes from src to dst, checking ctx
// between chunks so a cancelled extract stops promptly.
func copyArchiveBody(ctx context.Context, dst io.Writer, src io.Reader, size int64) (int64, error) {
	buffer := make([]byte, fileChunkSize)
	remaining := size
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return size - remaining, err
		}
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		n, readErr := src.Read(buffer[:chunk])
		if n > 0 {
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return size - remaining, writeErr
			}
			remaining -= int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return size - remaining, nil
			}
			return size - remaining, readErr
		}
	}
	return size, nil
}

// archiveSink stores one entry into an archive under construction.
type archiveSink interface {
	addDir(name string, entry fsroot.Entry) error
	addFile(name string, entry fsroot.Entry, body io.Reader) error
	close() error
}

// newArchiveSink builds the sink for format over w and returns it with a close
// function that flushes the container (and, for tar.gz, the gzip stream).
func newArchiveSink(w io.Writer, format agentv1.ArchiveFormat) (archiveSink, func() error, error) {
	switch format {
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP:
		zw := zip.NewWriter(w)
		return &zipSink{zw: zw}, zw.Close, nil
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR:
		tw := tar.NewWriter(w)
		return &tarSink{tw: tw}, tw.Close, nil
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ:
		gw := gzip.NewWriter(w)
		tw := tar.NewWriter(gw)
		closeFn := func() error {
			if err := tw.Close(); err != nil {
				_ = gw.Close()
				return err
			}
			return gw.Close()
		}
		return &tarSink{tw: tw}, closeFn, nil
	default:
		return nil, nil, fmt.Errorf("unsupported archive format")
	}
}

// zipSink stores entries in a zip container.
type zipSink struct {
	zw *zip.Writer
}

// addDir writes a directory header with the source's mode.
func (z *zipSink) addDir(name string, entry fsroot.Entry) error {
	header := &zip.FileHeader{Name: strings.TrimSuffix(name, "/") + "/", Method: zip.Store, Modified: entry.Modified}
	header.SetMode(entry.Mode.Perm() | os.ModeDir)
	_, err := z.zw.CreateHeader(header)
	return err
}

// addFile writes a regular file with the source's mode.
func (z *zipSink) addFile(name string, entry fsroot.Entry, body io.Reader) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: entry.Modified}
	header.SetMode(entry.Mode.Perm())
	w, err := z.zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, body)
	return err
}

func (z *zipSink) close() error { return z.zw.Close() }

// tarSink stores entries in a tar (or tar.gz) container.
type tarSink struct {
	tw *tar.Writer
}

// addDir writes a directory header with the source's mode.
func (t *tarSink) addDir(name string, entry fsroot.Entry) error {
	return t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     strings.TrimSuffix(name, "/") + "/",
		Mode:     int64(entry.Mode.Perm()),
		ModTime:  entry.Modified,
	})
}

// addFile writes a regular file with the source's mode and size.
func (t *tarSink) addFile(name string, entry fsroot.Entry, body io.Reader) error {
	if err := t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     int64(entry.Mode.Perm()),
		Size:     entry.Size,
		ModTime:  entry.Modified,
	}); err != nil {
		return err
	}
	written, err := io.Copy(t.tw, body)
	if err != nil {
		return err
	}
	if written != entry.Size {
		return fmt.Errorf("%w: source %q changed size during compression", fsroot.ErrConflict, name)
	}
	return nil
}

func (t *tarSink) close() error { return t.tw.Close() }
