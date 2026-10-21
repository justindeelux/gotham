package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
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
// shallower. Kept low so the per-entry ancestor work stays bounded even when
// the ancestor cache is cold.
const fileArchiveMaxNameDepth = 64

// zipCentralDirHeaderMinSize is the fixed size of one central-directory file
// header (the variable name/extra/comment fields follow). archive/zip reads
// headers until it hits a bad one, so the number of entries it materialises is
// bounded by the bytes actually present in the central directory: at most
// cdSize/46 for a declared size of cdSize. The pre-parse uses that ratio as the
// authoritative memory bound, so a crafted directory cannot make zip.NewReader
// allocate millions of headers regardless of the declared entry count.
const zipCentralDirHeaderMinSize = 46

// fileArchiveStreamOverhead is the per-entry slack a decompressed archive
// stream may add on top of the declared file bytes: tar's 512-byte header plus
// padding, and any small pax/GNU long-name records. It bounds the total
// decompressed bytes read, so metadata-only streams (repeated pax headers) and
// a lying sequence of headers cannot burn CPU past the caps.
const fileArchiveStreamOverhead = 4096

// Zip end-of-central-directory parsing. The EOCD record sits in the last
// 22+comment bytes of a zip; archive/zip searches the last 65 KiB for it (after
// a 1 KiB first pass) and accepts a comment up to 65535 bytes, so the pre-parse
// uses the same 65 KiB window and the same last-to-first rule to select exactly
// the record archive/zip will use.
const (
	zipEndOfCentralDirMinSize              = 22
	zipEndOfCentralDirTailWindow           = 65 * 1024
	zip64EndOfCentralDirLocatorSize        = 20
	zip64EndOfCentralDirRecordSize         = 56
	zipFieldMax16                   uint16 = 0xFFFF
	zipFieldMax32                   uint32 = 0xFFFFFFFF
)

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

// errArchiveStreamTooLong marks a decompressed archive stream that outgrew its
// byte budget; extractTar turns it into a "too large" refusal.
var errArchiveStreamTooLong = errors.New("archive stream exceeds its decompressed size budget")

// archiveStreamLimit is the total decompressed bytes one extract may read: the
// declared byte cap plus per-entry header/padding slack, and no more than the
// ratio ceiling when the archive is compressed. It bounds the work archive/tar
// does on headers it consumes internally (pax/GNU long names), which never
// reach addEntry.
func archiveStreamLimit(counter *archiveCounter) int64 {
	limit := counter.maxBytes + counter.maxEntries*fileArchiveStreamOverhead
	if counter.archiveBytes > 0 {
		if ratio := counter.archiveBytes * fileArchiveRatioCap; ratio < limit {
			limit = ratio
		}
	}
	return limit
}

// cappedReader fails once the underlying reader has yielded limit bytes total,
// so the decompressed (gzip) or raw tar stream is bounded on the bytes actually
// read rather than only the declared file sizes.
type cappedReader struct {
	r     io.Reader
	limit int64
	read  int64
}

func newCappedReader(r io.Reader, limit int64) *cappedReader {
	return &cappedReader{r: r, limit: limit}
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.read >= c.limit {
		return 0, errArchiveStreamTooLong
	}
	if int64(len(p)) > c.limit-c.read {
		p = p[:c.limit-c.read]
	}
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
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

// apply chmods every recorded directory to its archived mode (with owner rwx
// forced, so a mode that lacks owner execute — 0644, 0600, 0555 on some
// producers — never leaves an untraversable tree), deepest first, so a
// read-only parent never blocks chmod-ing its children. Errors are joined; the
// first failure does not stop the rest.
func (d *extractDirs) apply(root *fsroot.Root) error {
	// Deepest first: sort by descending segment count.
	sort.SliceStable(d.perms, func(i, j int) bool {
		return strings.Count(d.perms[i].target, "/") > strings.Count(d.perms[j].target, "/")
	})
	var errs []error
	for _, p := range d.perms {
		if err := root.Chmod(p.target, p.mode|0o700); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// extractState bounds the CPU one extract spends on path checks. It caches the
// directories it has already created or verified so an archive with many
// entries in one deep directory is linear rather than re-walking the whole
// ancestor chain per entry. Nothing ever invalidates a cached path: an archive
// cannot create a symlink, so a verified real directory stays a real directory
// for the duration of the extract.
type extractState struct {
	root *fsroot.Root
	// verified holds root-relative paths proven to be real directories.
	verified map[string]struct{}
	// statCalls counts the Stat walks the state performed, so a test can prove
	// the cache turns repeated deep paths into O(depth) work instead of
	// O(entries × depth²).
	statCalls int
}

// newExtractState builds the per-extract directory cache.
func newExtractState(root *fsroot.Root) *extractState {
	return &extractState{root: root, verified: make(map[string]struct{})}
}

// verifyAncestors refuses a symlinked or non-directory component in rel's
// parent chain under base, so an archive entry never writes through a
// pre-existing in-root symlink. base (the caller's chosen destination) is not
// itself checked. A component already cached as a real directory is skipped.
func (e *extractState) verifyAncestors(base, rel string) error {
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
		if _, ok := e.verified[cur]; ok {
			continue
		}
		e.statCalls++
		entry, err := e.root.Stat(cur)
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
		e.verified[cur] = struct{}{}
	}
	return nil
}

// ensureDir creates dir (a root-relative path) when it is not already known to
// exist, then caches the created chain so later entries under it skip the walk.
// Callers must have verified dir's ancestors first.
func (e *extractState) ensureDir(dir string, mode os.FileMode) error {
	if dir == "." || dir == "" {
		return nil
	}
	if _, ok := e.verified[dir]; ok {
		return nil
	}
	if err := e.root.MkdirAll(dir, mode); err != nil {
		return err
	}
	for cur := dir; cur != "." && cur != "" && cur != "/"; cur = path.Dir(cur) {
		if _, ok := e.verified[cur]; ok {
			break
		}
		e.verified[cur] = struct{}{}
	}
	return nil
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
	if err := guardTrash(root, "compress", dst); err != nil {
		return err
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
		if err := guardTrash(root, "compress", rel); err != nil {
			return err
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
	if err := guardTrash(root, "extract", src); err != nil {
		return err
	}
	format, ok := archiveFormatFromName(src)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "extract: %q is not a .zip, .tar or .tar.gz archive", req.GetSource())
	}
	dst, err := fsroot.Clean(req.GetDestination())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "extract: invalid destination %q", req.GetDestination())
	}
	if err := guardTrash(root, "extract", dst); err != nil {
		return err
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
	// Every format reads a compressed archive whose on-disk size is bounded
	// before any parser runs, so a hostile archive cannot make the agent
	// allocate more than the configured input cap.
	if entry.Size > s.archiveMaxInputBytes {
		return status.Errorf(codes.ResourceExhausted, "extract: archive is larger than %d bytes", s.archiveMaxInputBytes)
	}

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
	state := newExtractState(root)
	if err := s.extractArchive(stream.Context(), root, archive, entry.Size, format, dst, src, req.GetOverwrite(), counter, dirs, state, progress); err != nil {
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
func (s *FileServer) extractArchive(ctx context.Context, root *fsroot.Root, archive *os.File, size int64, format agentv1.ArchiveFormat, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, state *extractState, progress *archiveProgress) error {
	switch format {
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP:
		// archive/zip does not validate the declared entry count while reading:
		// it materialises one header per 46-byte record physically present at
		// the central directory and only compares the count modulo 2^16 at the
		// end. It also ignores the declared central-directory size and reads
		// headers from the declared offset until one is bad, so a declared size
		// that does not cover the physical directory is not a bound. The
		// pre-parse therefore reads the same end-of-central-directory record
		// archive/zip will select (zip64-aware), requires the declared central
		// directory to end exactly at that record, and bounds the header table
		// by both the declared size (dirSize/46) and the entry cap. Requiring
		// the directory to end at the record makes cdSize authoritative: a
		// shorter declared size (the memory-bomb shape) is refused here, before
		// zip.NewReader can allocate the headers.
		dir, err := zipCentralDirectoryBounds(archive, size)
		if err != nil {
			return fmt.Errorf("%w: reading zip: %v", fsroot.ErrInvalid, err)
		}
		maxBySize := dir.size / zipCentralDirHeaderMinSize
		if dir.entries > counter.maxEntries || maxBySize > counter.maxEntries {
			return fmt.Errorf("%w: archive has more than %d entries", fsroot.ErrTooLarge, counter.maxEntries)
		}
		if dir.entries > maxBySize {
			return fmt.Errorf("%w: zip central directory is inconsistent", fsroot.ErrInvalid)
		}
		if dir.offset+dir.size != dir.recordOffset {
			return fmt.Errorf("%w: zip central directory does not end at the end-of-central-directory record", fsroot.ErrInvalid)
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
			if err := s.extractZipEntry(ctx, root, zf, dst, src, overwrite, counter, dirs, state, progress); err != nil {
				return err
			}
		}
		return nil
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR:
		return s.extractTar(ctx, root, newCappedReader(archive, archiveStreamLimit(counter)), dst, src, overwrite, counter, dirs, state, progress)
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ:
		gz, err := gzip.NewReader(archive)
		if err != nil {
			return fmt.Errorf("%w: reading gzip: %v", fsroot.ErrInvalid, err)
		}
		defer func() { _ = gz.Close() }()
		return s.extractTar(ctx, root, newCappedReader(gz, archiveStreamLimit(counter)), dst, src, overwrite, counter, dirs, state, progress)
	default:
		return fmt.Errorf("%w: unsupported archive format", fsroot.ErrInvalid)
	}
}

// zipCentralDirectory describes the central directory a zip's
// end-of-central-directory record declares. recordOffset is the physical offset
// of the end-of-central-directory record the directory must end at: the zip64
// end-of-central-directory record for a zip64 archive, otherwise the EOCD
// record itself — the same offset archive/zip treats as the end of the
// directory.
type zipCentralDirectory struct {
	entries      int64
	size         int64
	offset       int64
	recordOffset int64
}

// zipCentralDirectoryBounds reads the zip end-of-central-directory record —
// exactly the one archive/zip selects, so the pre-parse and the reader agree on
// which record is authoritative — and returns the declared entry count,
// central-directory size and offset, and the physical offset the directory must
// end at. Saturated 16/32-bit fields are resolved from the zip64 records,
// matching archive/zip's fallback when the locator is absent. It reads only the
// bounded tail window and never parses entry headers, so a hostile archive
// cannot make it allocate more than the window before the caller applies its
// caps.
func zipCentralDirectoryBounds(f io.ReaderAt, size int64) (zipCentralDirectory, error) {
	var empty zipCentralDirectory
	if size < zipEndOfCentralDirMinSize {
		return empty, fmt.Errorf("file is too short to be a zip")
	}
	window := int64(zipEndOfCentralDirTailWindow)
	if size < window {
		window = size
	}
	tail := make([]byte, window)
	if _, err := f.ReadAt(tail, size-window); err != nil {
		return empty, err
	}
	idx := zipEndOfCentralDirOffset(tail)
	if idx < 0 {
		return empty, fmt.Errorf("no end of central directory record")
	}
	recordOffset := size - window + int64(idx)
	eocd := tail[idx:]
	count16 := binary.LittleEndian.Uint16(eocd[10:12])
	size32 := binary.LittleEndian.Uint32(eocd[12:16])
	offset32 := binary.LittleEndian.Uint32(eocd[16:20])
	if count16 != zipFieldMax16 && size32 != zipFieldMax32 && offset32 != zipFieldMax32 {
		return zipCentralDirectory{
			entries:      int64(count16),
			size:         int64(size32),
			offset:       int64(offset32),
			recordOffset: recordOffset,
		}, nil
	}
	// A saturated field means the real values may live in the zip64 records.
	// archive/zip falls back to the 16/32-bit fields when the locator is absent
	// or invalid, so a 0xFFFF count with no zip64 is a plain 65535-entry archive.
	dir, used, err := zip64CentralDirectoryBounds(f, recordOffset, size)
	if err != nil {
		return empty, err
	}
	if !used {
		dir = zipCentralDirectory{
			entries:      int64(count16),
			size:         int64(size32),
			offset:       int64(offset32),
			recordOffset: recordOffset,
		}
	}
	if dir.entries < 0 || dir.size < 0 || dir.offset < 0 {
		return empty, fmt.Errorf("zip central directory declares a negative size")
	}
	if dir.size > size {
		return empty, fmt.Errorf("zip central directory is larger than the file")
	}
	return dir, nil
}

// zipEndOfCentralDirOffset returns the offset of the end-of-central-directory
// record within tail, or -1 when none is found. It scans last-to-first and
// accepts the first signature whose declared comment does not run past the end
// of the window, matching archive/zip's findSignatureInBlock (which gives up on
// a truncated comment instead of walking back to an earlier signature).
func zipEndOfCentralDirOffset(tail []byte) int {
	signature := []byte{'P', 'K', 5, 6}
	for i := len(tail) - zipEndOfCentralDirMinSize; i >= 0; i-- {
		if !bytes.Equal(tail[i:i+4], signature) {
			continue
		}
		commentLen := int(binary.LittleEndian.Uint16(tail[i+20 : i+22]))
		if i+zipEndOfCentralDirMinSize+commentLen > len(tail) {
			return -1
		}
		return i
	}
	return -1
}

// zip64CentralDirectoryBounds reads the zip64 end-of-central-directory record
// the locator points at and returns its declared entry count, central-directory
// size and offset, and the record's own offset (where the directory must end).
// used is false when there is no valid locator, so the caller falls back to the
// 16/32-bit EOCD fields exactly as archive/zip does.
func zip64CentralDirectoryBounds(f io.ReaderAt, eocdOffset, size int64) (zipCentralDirectory, bool, error) {
	if eocdOffset < zip64EndOfCentralDirLocatorSize {
		return zipCentralDirectory{}, false, nil
	}
	locator := make([]byte, zip64EndOfCentralDirLocatorSize)
	if _, err := f.ReadAt(locator, eocdOffset-zip64EndOfCentralDirLocatorSize); err != nil {
		return zipCentralDirectory{}, false, err
	}
	// Match archive/zip's findDirectory64End: the locator is valid only with the
	// locator signature, disk number 0 and a single-disk marker.
	if !bytes.Equal(locator[0:4], []byte{'P', 'K', 6, 7}) ||
		binary.LittleEndian.Uint32(locator[4:8]) != 0 ||
		binary.LittleEndian.Uint32(locator[16:20]) != 1 {
		return zipCentralDirectory{}, false, nil
	}
	recordOffset := int64(binary.LittleEndian.Uint64(locator[8:16]))
	if recordOffset < 0 || recordOffset > size-zip64EndOfCentralDirRecordSize {
		return zipCentralDirectory{}, false, fmt.Errorf("zip64 end of central directory record out of range")
	}
	record := make([]byte, zip64EndOfCentralDirRecordSize)
	if _, err := f.ReadAt(record, recordOffset); err != nil {
		return zipCentralDirectory{}, false, err
	}
	if !bytes.Equal(record[0:4], []byte{'P', 'K', 6, 6}) {
		return zipCentralDirectory{}, false, fmt.Errorf("missing zip64 end of central directory record")
	}
	dir := zipCentralDirectory{
		entries:      int64(binary.LittleEndian.Uint64(record[32:40])),
		size:         int64(binary.LittleEndian.Uint64(record[40:48])),
		offset:       int64(binary.LittleEndian.Uint64(record[48:56])),
		recordOffset: recordOffset,
	}
	if dir.entries < 0 || dir.size < 0 || dir.offset < 0 {
		return zipCentralDirectory{}, false, fmt.Errorf("zip64 end of central directory declares a negative size")
	}
	return dir, true, nil
}

// extractZipEntry unpacks one zip member. Zip names may use backslashes as
// separators (Windows producers), so those are normalised for zip only.
func (s *FileServer) extractZipEntry(ctx context.Context, root *fsroot.Root, zf *zip.File, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, state *extractState, progress *archiveProgress) error {
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
		return s.extractDir(root, dst, rel, mode.Perm(), dirs, state)
	}
	if rel == "" {
		return nil
	}
	if err := counter.addEntry(int64(zf.UncompressedSize64)); err != nil {
		return err
	}
	target := path.Join(dst, rel)
	if err := state.verifyAncestors(dst, rel); err != nil {
		return err
	}
	body, err := zf.Open()
	if err != nil {
		return fmt.Errorf("%w: opening archive entry %q: %v", fsroot.ErrInvalid, zf.Name, err)
	}
	defer func() { _ = body.Close() }()
	return s.extractFile(ctx, root, target, mode.Perm(), int64(zf.UncompressedSize64), overwrite, src, body, state, progress)
}

// extractTar unpacks a tar or tar.gz body. The pax global header git archive
// and GitHub tarballs put first is skipped, and a continuation record is
// treated as a regular file, so those archives extract.
func (s *FileServer) extractTar(ctx context.Context, root *fsroot.Root, r io.Reader, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, state *extractState, progress *archiveProgress) error {
	tr := tar.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if errors.Is(err, errArchiveStreamTooLong) {
			return fmt.Errorf("%w: archive expands beyond %d bytes", fsroot.ErrTooLarge, counter.maxBytes)
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
			// A pax global/extended header carries no payload, but it still
			// occupies a header record: count it against the entry cap so a
			// stream of metadata-only records cannot bypass it. (Per-file
			// pax/GNU long-name records are consumed inside tar.Reader.Next and
			// are bounded by the stream cap instead.)
			if err := counter.addEntry(0); err != nil {
				return err
			}
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
			if err := s.extractDir(root, dst, rel, mode, dirs, state); err != nil {
				return err
			}
		case tar.TypeReg, 0, tar.TypeCont:
			if err := state.verifyAncestors(dst, rel); err != nil {
				return err
			}
			target := path.Join(dst, rel)
			if err := s.extractFile(ctx, root, target, mode, hdr.Size, overwrite, src, tr, state, progress); err != nil {
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
func (s *FileServer) extractDir(root *fsroot.Root, dst, rel string, mode os.FileMode, dirs *extractDirs, state *extractState) error {
	target := path.Join(dst, rel)
	// A member path must never land in the reserved recycle bin, exactly like
	// any other entry-addressing operation.
	if err := guardTrash(root, "extract", target); err != nil {
		return err
	}
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "extract: an application directory cannot be replaced")
	}
	if mode == 0 {
		mode = fileArchiveDirMode
	}
	// A pre-existing symlink anywhere in the target's ancestor chain (inside the
	// destination) is refused: os.Root would follow an in-root symlink and write
	// outside the requested destination.
	if err := state.verifyAncestors(dst, rel); err != nil {
		return err
	}
	// Only a directory this extract creates gets its archived mode applied: an
	// entry that lands on an existing directory must not widen (or narrow) it
	// (Claude L2).
	created := true
	if entry, statErr := root.Stat(target); statErr == nil {
		if entry.Kind != fsroot.KindDirectory {
			return fmt.Errorf("%w: %s is not a directory", fsroot.ErrInvalid, target)
		}
		created = false
	} else if !errors.Is(statErr, fsroot.ErrNotFound) {
		return statErr
	}
	if err := state.ensureDir(target, mode|0o700); err != nil {
		return err
	}
	if created {
		dirs.record(target, mode)
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
func (s *FileServer) extractFile(ctx context.Context, root *fsroot.Root, target string, mode os.FileMode, size int64, overwrite bool, src string, r io.Reader, state *extractState, progress *archiveProgress) error {
	// A member path must never land in the reserved recycle bin, exactly like
	// any other entry-addressing operation.
	if err := guardTrash(root, "extract", target); err != nil {
		return err
	}
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
		if err := state.ensureDir(dir, fileArchiveDirMode); err != nil {
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
		// The name is echoed back only as a segment count: a hostile archive can
		// carry a multi-megabyte name, which must not be embedded in the
		// returned error (and thus the gRPC status).
		return "", fmt.Errorf("%w: archive entry nests too deeply (%d segments)", fsroot.ErrTooLarge, len(segments))
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
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return size - remaining, readErr
			}
			// A short or corrupt body (a truncated tar/gzip/zip) is an invalid
			// archive, not an internal failure, so the caller answers 4xx.
			return size - remaining, fmt.Errorf("%w: archive entry is truncated or corrupt: %v", fsroot.ErrInvalid, readErr)
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
