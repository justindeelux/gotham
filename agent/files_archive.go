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
	"sort"
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

// fileArchiveDepth bounds how deep a compress walk may nest. A healthy tree is
// far shallower; the bound turns a pathological tree into an explicit error
// instead of unbounded recursion, and keeps the per-level directory descriptor
// count well under the process's file-descriptor limit.
const fileArchiveDepth = 128

// fileArchiveMaxNameDepth bounds how many path segments one archive entry name
// may carry on extract. A name with thousands of segments makes MkdirAll walk
// each ancestor (O(n²) syscalls) and recurses deeply; a healthy archive is far
// shallower.
const fileArchiveMaxNameDepth = 128

// fileArchiveDirMode is the mode a directory created implicitly by an extract
// gets when the archive carries no mode for it. Explicit directory entries are
// created with at least owner rwx (so their children can be written) and then
// chmod-ed to their archived mode once every entry is unpacked.
const fileArchiveDirMode os.FileMode = 0o755

// fileArchiveFileMode is the default mode of a freshly built archive when the
// destination did not already exist. A temp file is created 0600 for privacy
// and chmod-ed to this before the atomic rename, so the archive is readable by
// other users and containers as an ordinary file would be.
const fileArchiveFileMode os.FileMode = 0o644

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
// an over-limit archive is refused before it can fill the disk. The byte total
// is checked without adding first, so a declared size near MaxInt64 cannot wrap
// the accumulator past the cap (a wrapping add would defeat both the byte and
// the ratio guard).
func (c *archiveCounter) addEntry(size int64) error {
	if size < 0 {
		return fmt.Errorf("%w: archive entry has a negative size", fsroot.ErrInvalid)
	}
	c.entries++
	if c.entries > c.maxEntries {
		return fmt.Errorf("%w: archive has more than %d entries", fsroot.ErrTooLarge, c.maxEntries)
	}
	if size > c.maxBytes || c.bytes > c.maxBytes-size {
		return fmt.Errorf("%w: archive expands beyond %d bytes", fsroot.ErrTooLarge, c.maxBytes)
	}
	c.bytes += size
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

// extractDirs records the archived mode of each explicit directory entry so it
// can be applied after every entry is unpacked. A read-only directory mode
// (0555, common in module caches and distro tarballs) would otherwise make the
// writes into that directory fail mid-extract.
type extractDirs struct {
	perms []dirPerm
}

// dirPerm is one recorded directory and the mode the archive declared for it.
type dirPerm struct {
	target string
	mode   os.FileMode
}

// record remembers one directory's archived mode.
func (d *extractDirs) record(target string, mode os.FileMode) {
	d.perms = append(d.perms, dirPerm{target: target, mode: mode})
}

// apply chmods every recorded directory to its archived mode, deepest first, so
// a read-only parent never blocks chmod-ing its children. Errors are joined;
// the first failure does not stop the rest.
func (d *extractDirs) apply(root *fsroot.Root) error {
	// Deepest first: sort by descending segment count.
	sort.SliceStable(d.perms, func(i, j int) bool {
		return strings.Count(d.perms[i].target, "/") > strings.Count(d.perms[j].target, "/")
	})
	var errs []error
	for _, p := range d.perms {
		if err := root.Chmod(p.target, p.mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
		// Two sources with the same base name — or one nested inside another —
		// would store duplicate or overlapping entry names, which extract
		// cannot tell apart. Refuse rather than emit an ambiguous archive.
		for _, existing := range sources {
			if existing == rel || isWithin(existing, rel) || isWithin(rel, existing) ||
				path.Base(existing) == path.Base(rel) {
				return status.Errorf(codes.InvalidArgument, "compress: sources %q and %q overlap", existing, rel)
			}
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
	// The destination is refused when it lies on or inside a source once both
	// are resolved through any in-root symlinks: the lexical check above misses
	// a destination routed through a symlinked parent into a source tree, which
	// would make the archive enumerate its own growing temp file.
	if resolvedDst, rerr := root.Resolve(path.Dir(dst)); rerr == nil {
		resolvedDst = path.Join(resolvedDst, path.Base(dst))
		for _, source := range sources {
			resolvedSrc, serr := root.Resolve(source)
			if serr != nil {
				continue
			}
			if resolvedDst == resolvedSrc || isWithin(resolvedSrc, resolvedDst) {
				return status.Errorf(codes.InvalidArgument, "compress: destination %q resolves inside source %q", req.GetDestination(), source)
			}
		}
	}
	// The output keeps the previous file's mode when overwriting, or 0644 for a
	// fresh archive: a CreateTemp 0600 would otherwise make the archive
	// unreadable by other users and containers.
	mode := fileArchiveFileMode
	if existing, err := root.Stat(dst); err == nil {
		if existing.Kind == fsroot.KindDirectory {
			return status.Errorf(codes.InvalidArgument, "compress: destination %q is a directory", dst)
		}
		if !req.GetOverwrite() {
			return status.Errorf(codes.AlreadyExists, "compress: %q already exists", dst)
		}
		if existing.Kind == fsroot.KindRegular {
			mode = existing.Mode
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
	tmpInfo, err := tmp.Stat()
	if err != nil {
		return fileError("compress", err)
	}

	counter := &archiveCounter{maxEntries: s.archiveMaxEntries, maxBytes: s.archiveMaxBytes}
	progress := &archiveProgress{stream: stream, path: dst}
	sink, closeSink, err := newArchiveSink(tmp, format)
	if err != nil {
		return fileError("compress", err)
	}
	for _, source := range sources {
		if err := s.compressEntry(stream.Context(), root, sink, source, path.Base(source), tmpInfo, counter, progress, 0); err != nil {
			_ = closeSink()
			return fileError("compress", err)
		}
	}
	if err := closeSink(); err != nil {
		return fileError("compress", err)
	}
	if err := tmp.Chmod(mode.Perm()); err != nil {
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
// non-regular entry is skipped and counted, never followed; so is the live temp
// file when a symlinked source path reaches it. depth bounds the recursion.
func (s *FileServer) compressEntry(ctx context.Context, root *fsroot.Root, sink archiveSink, rel, name string, tmpInfo os.FileInfo, counter *archiveCounter, progress *archiveProgress, depth int) error {
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
			return s.compressEntry(ctx, root, sink, path.Join(rel, child), path.Join(name, child), tmpInfo, counter, progress, depth+1)
		})
	case fsroot.KindRegular:
		file, opened, err := root.OpenRegular(rel)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		// A source reached through a symlinked parent can physically contain
		// the destination directory, so the walk reaches the live temp file. It
		// must never be stored: the archive would read its own growing output.
		if info, err := file.Stat(); err == nil && tmpInfo != nil && os.SameFile(info, tmpInfo) {
			counter.skipped++
			return nil
		}
		// Charge and describe the file from the opened descriptor, not the
		// earlier Lstat: a leaf swapped between the two would otherwise be
		// sized by the stale stat.
		if err := counter.addEntry(opened.Size); err != nil {
			return err
		}
		if err := sink.addFile(ctx, name, opened, file); err != nil {
			return err
		}
		return progress.add(name, opened.Size)
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
//
// Extract is not transactional: a failure part-way leaves the entries already
// unpacked in place (only the file being written is staged and removed). A
// refused or failed entry aborts the whole extract; the caller should treat a
// failure as a partial result.
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
	dirs := &extractDirs{}
	if err := s.extractArchive(stream.Context(), root, archive, entry.Size, format, dst, src, req.GetOverwrite(), counter, dirs, progress); err != nil {
		return fileError("extract", err)
	}
	// Apply the archived directory modes last, so a read-only directory never
	// blocks writes into its children.
	if err := dirs.apply(root); err != nil {
		return fileError("extract", err)
	}
	return progress.done(0)
}

// extractArchive dispatches to the reader for the archive format. src is the
// archive's own path so an entry that would overwrite the archive being read is
// refused.
func (s *FileServer) extractArchive(ctx context.Context, root *fsroot.Root, archive *os.File, size int64, format agentv1.ArchiveFormat, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, progress *archiveProgress) error {
	switch format {
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP:
		// An archive's own size is bounded before the central directory is
		// parsed: zip.NewReader materialises every entry header into memory, so
		// an oversized archive would otherwise be read before any cap fires.
		if size > counter.maxBytes {
			return fmt.Errorf("%w: archive is larger than %d bytes", fsroot.ErrTooLarge, counter.maxBytes)
		}
		zr, err := zip.NewReader(archive, size)
		if err != nil {
			return fmt.Errorf("%w: reading zip: %v", fsroot.ErrInvalid, err)
		}
		if int64(len(zr.File)) > counter.maxEntries {
			return fmt.Errorf("%w: archive has more than %d entries", fsroot.ErrTooLarge, counter.maxEntries)
		}
		for _, zf := range zr.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.extractZipEntry(ctx, root, zf, dst, src, overwrite, counter, dirs, progress); err != nil {
				return err
			}
		}
		return nil
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR:
		return s.extractTar(ctx, root, archive, dst, src, overwrite, counter, dirs, progress)
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ:
		gz, err := gzip.NewReader(archive)
		if err != nil {
			return fmt.Errorf("%w: reading gzip: %v", fsroot.ErrInvalid, err)
		}
		defer func() { _ = gz.Close() }()
		return s.extractTar(ctx, root, gz, dst, src, overwrite, counter, dirs, progress)
	default:
		return fmt.Errorf("%w: unsupported archive format", fsroot.ErrInvalid)
	}
}

// extractZipEntry unpacks one zip member. Zip names may use backslashes as
// separators (Windows producers), so those are normalised for zip only.
func (s *FileServer) extractZipEntry(ctx context.Context, root *fsroot.Root, zf *zip.File, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, progress *archiveProgress) error {
	rel, err := archiveEntryPath(zf.Name, true)
	if err != nil {
		return err
	}
	mode := zf.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return fmt.Errorf("%w: archive entry %q is a symbolic link", fsroot.ErrSpecial, zf.Name)
	case mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0:
		return fmt.Errorf("%w: archive entry %q is a special file", fsroot.ErrSpecial, zf.Name)
	}
	if strings.HasSuffix(zf.Name, "/") || mode.IsDir() {
		if err := counter.addEntry(0); err != nil {
			return err
		}
		if rel == "" {
			return nil
		}
		return s.extractDir(root, dst, rel, mode.Perm(), dirs)
	}
	if rel == "" {
		return nil
	}
	if err := counter.addEntry(int64(zf.UncompressedSize64)); err != nil {
		return err
	}
	target := path.Join(dst, rel)
	if err := refuseSymlinkedAncestors(root, dst, rel); err != nil {
		return err
	}
	body, err := zf.Open()
	if err != nil {
		return fmt.Errorf("%w: opening archive entry %q: %v", fsroot.ErrInvalid, zf.Name, err)
	}
	defer func() { _ = body.Close() }()
	return s.extractFile(ctx, root, target, mode.Perm(), int64(zf.UncompressedSize64), overwrite, src, body, progress)
}

// extractTar unpacks a tar or tar.gz body. The pax global header git archive
// and GitHub tarballs put first is skipped, and a continuation record is
// treated as a regular file, so those archives extract.
func (s *FileServer) extractTar(ctx context.Context, root *fsroot.Root, r io.Reader, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, progress *archiveProgress) error {
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
		rel, err := archiveEntryPath(hdr.Name, false)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("%w: archive entry %q is a link", fsroot.ErrSpecial, hdr.Name)
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return fmt.Errorf("%w: archive entry %q is a device or FIFO", fsroot.ErrSpecial, hdr.Name)
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// Metadata records (pax global/extended headers) carry no payload
			// the tar reader has not already consumed.
			continue
		}
		// Count every accepted entry before acting on it, so a directory (size
		// 0) or a name that cleans to nothing cannot bypass the entry cap.
		size := int64(0)
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == 0 || hdr.Typeflag == tar.TypeCont {
			size = hdr.Size
		}
		if err := counter.addEntry(size); err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		mode := os.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := s.extractDir(root, dst, rel, mode, dirs); err != nil {
				return err
			}
		case tar.TypeReg, 0, tar.TypeCont:
			if err := refuseSymlinkedAncestors(root, dst, rel); err != nil {
				return err
			}
			target := path.Join(dst, rel)
			if err := s.extractFile(ctx, root, target, mode, hdr.Size, overwrite, src, tr, progress); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: archive entry %q has an unsupported type", fsroot.ErrSpecial, hdr.Name)
		}
	}
}

// extractDir creates one archive directory, reusing an existing directory and
// refusing to replace anything else. The directory is created with owner rwx so
// its children can be written even when the archive declared a read-only mode;
// the archived mode is applied once the whole extract finishes.
func (s *FileServer) extractDir(root *fsroot.Root, dst, rel string, mode os.FileMode, dirs *extractDirs) error {
	target := path.Join(dst, rel)
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "extract: an application directory cannot be replaced")
	}
	if mode == 0 {
		mode = fileArchiveDirMode
	}
	// A pre-existing symlink anywhere in the target's ancestor chain (inside the
	// destination) is refused: os.Root would follow an in-root symlink and write
	// outside the requested destination.
	if err := refuseSymlinkedAncestors(root, dst, rel); err != nil {
		return err
	}
	if err := root.MkdirAll(target, mode|0o700); err != nil {
		return err
	}
	dirs.record(target, mode)
	return nil
}

// refuseSymlinkedAncestors walks base/rel's parent chain and refuses when any
// component is a symbolic link, so an archive entry never writes through a
// pre-existing in-root symlink. base (the caller's chosen destination) is not
// itself checked.
func refuseSymlinkedAncestors(root *fsroot.Root, base, rel string) error {
	parent := path.Dir(rel)
	if parent == "." || parent == "" {
		return nil
	}
	cur := base
	for _, part := range strings.Split(parent, "/") {
		if part == "" || part == "." {
			continue
		}
		cur = path.Join(cur, part)
		entry, err := root.Stat(cur)
		if err != nil {
			if errors.Is(err, fsroot.ErrNotFound) {
				// A missing component cannot be a symlink; MkdirAll creates it.
				return nil
			}
			return err
		}
		if entry.Kind == fsroot.KindSymlink {
			return fmt.Errorf("%w: archive destination %q has a symlinked ancestor", fsroot.ErrEscape, cur)
		}
		if entry.Kind != fsroot.KindDirectory {
			return fmt.Errorf("%w: archive destination %q is not a directory", fsroot.ErrInvalid, cur)
		}
	}
	return nil
}

// extractFile writes one archive file atomically: the body is staged in a
// temporary file in the target's directory and renamed over the target only
// once it is complete, so a failure or cancellation never truncates or removes
// an existing file (with overwrite). The body is copied from exactly size bytes
// of r, so a lying header cannot stream more than it declared, and the reader is
// then drained to EOF so a zip's CRC32 — verified only at end-of-entry — is
// checked. A target that is the archive being read is refused. The
// running-app-directory guard refuses replacing a top-level application
// directory; the byte/entry caps are enforced by the caller before this runs.
func (s *FileServer) extractFile(ctx context.Context, root *fsroot.Root, target string, mode os.FileMode, size int64, overwrite bool, src string, r io.Reader, progress *archiveProgress) error {
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "extract: an application directory cannot be replaced")
	}
	if target == src {
		return fmt.Errorf("%w: archive entry %q would overwrite the archive being read", fsroot.ErrInvalid, target)
	}
	if mode == 0 {
		mode = 0o644
	}
	// A directory target cannot be replaced by a file entry; refuse it before
	// staging (a rename over a directory would otherwise surface as an internal
	// error).
	if entry, statErr := root.Stat(target); statErr == nil {
		if entry.Kind == fsroot.KindDirectory {
			return fmt.Errorf("%w: %s is a directory", fsroot.ErrExists, target)
		}
	} else if !errors.Is(statErr, fsroot.ErrNotFound) {
		return statErr
	}
	if dir := path.Dir(target); dir != "." {
		if err := root.MkdirAll(dir, fileArchiveDirMode); err != nil {
			return err
		}
	}
	tmp, tmpRel, err := root.CreateTemp(path.Dir(target), 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = root.Remove(tmpRel)
	}()
	written, copyErr := copyArchiveBody(ctx, tmp, r, size)
	if copyErr != nil {
		return copyErr
	}
	if written != size {
		return fmt.Errorf("%w: archive entry %q is truncated (%d of %d bytes)", fsroot.ErrInvalid, target, written, size)
	}
	if err := expectEntryEOF(r); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if overwrite {
		err = root.Rename(tmpRel, target)
	} else {
		err = root.RenameExclusive(tmpRel, target)
	}
	if err != nil {
		return err
	}
	return progress.add(target, written)
}

// expectEntryEOF reads one more byte from r, expecting end-of-entry. A zip
// reader only verifies the entry's CRC32 and its "no more than declared" check
// when it reaches EOF, so a trailing byte or a checksum error is reported here
// instead of silently accepting a corrupt or lying entry.
func expectEntryEOF(r io.Reader) error {
	var probe [1]byte
	n, err := r.Read(probe[:])
	if n == 0 && errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: archive entry is corrupt: %v", fsroot.ErrInvalid, err)
	}
	return fmt.Errorf("%w: archive entry expanded past its declared size", fsroot.ErrInvalid)
}

// archiveEntryPath validates one archive member name and returns it as a
// root-relative path. An absolute name, a ".." segment, a NUL byte or a name
// that cleans to nothing is refused; a directory root entry ("." or "./")
// returns "". backslash marks a zip name (Windows producers use "\"), which is
// normalised to "/"; a tar name keeps its backslashes, which are ordinary
// filename characters on Linux.
func archiveEntryPath(name string, backslash bool) (string, error) {
	if strings.IndexByte(name, 0) >= 0 {
		return "", fmt.Errorf("%w: archive entry %q contains a NUL byte", fsroot.ErrInvalid, name)
	}
	if backslash {
		name = strings.ReplaceAll(name, "\\", "/")
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%w: archive entry %q is absolute", fsroot.ErrInvalid, name)
	}
	segments := strings.Split(name, "/")
	if len(segments) > fileArchiveMaxNameDepth {
		return "", fmt.Errorf("%w: archive entry %q nests too deeply", fsroot.ErrTooLarge, name)
	}
	for _, segment := range segments {
		if segment == ".." {
			return "", fmt.Errorf("%w: archive entry %q escapes the destination", fsroot.ErrEscape, name)
		}
	}
	rel, err := fsroot.Clean(name)
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

// copyArchiveEntry copies exactly size bytes from src to dst, checking ctx
// between chunks so a cancelled compress stops a single large file promptly.
// The source is bounded by size, so a file that grows while it is read cannot
// push the archive past the declared entry size; the caller compares the
// returned count with size to detect a source that shrank.
func copyArchiveEntry(ctx context.Context, dst io.Writer, src io.Reader, size int64) (int64, error) {
	limited := io.LimitReader(src, size)
	buffer := make([]byte, fileChunkSize)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := limited.Read(buffer)
		if n > 0 {
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return written, writeErr
			}
			written += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

// archiveSink stores one entry into an archive under construction.
type archiveSink interface {
	addDir(name string, entry fsroot.Entry) error
	addFile(ctx context.Context, name string, entry fsroot.Entry, body io.Reader) error
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

// addFile writes a regular file with the source's mode, copying exactly the
// declared size so a source that grows during the walk cannot inflate the
// archive.
func (z *zipSink) addFile(ctx context.Context, name string, entry fsroot.Entry, body io.Reader) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: entry.Modified}
	header.SetMode(entry.Mode.Perm())
	w, err := z.zw.CreateHeader(header)
	if err != nil {
		return err
	}
	written, err := copyArchiveEntry(ctx, w, body, entry.Size)
	if err != nil {
		return err
	}
	if written != entry.Size {
		return fmt.Errorf("%w: source %q changed size during compression", fsroot.ErrConflict, name)
	}
	return nil
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
func (t *tarSink) addFile(ctx context.Context, name string, entry fsroot.Entry, body io.Reader) error {
	if err := t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     int64(entry.Mode.Perm()),
		Size:     entry.Size,
		ModTime:  entry.Modified,
	}); err != nil {
		return err
	}
	written, err := copyArchiveEntry(ctx, t.tw, body, entry.Size)
	if err != nil {
		return err
	}
	if written != entry.Size {
		return fmt.Errorf("%w: source %q changed size during compression", fsroot.ErrConflict, name)
	}
	return nil
}

func (t *tarSink) close() error { return t.tw.Close() }
