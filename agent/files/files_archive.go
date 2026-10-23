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

const fileArchiveRatioCap = 200

const fileArchiveDepth = 128

const fileArchiveMaxNameDepth = 64

const zipCentralDirHeaderMinSize = 46

const fileArchiveStreamOverhead = 4096

const (
	zipEndOfCentralDirMinSize              = 22
	zipEndOfCentralDirTailWindow           = 65 * 1024
	zip64EndOfCentralDirLocatorSize        = 20
	zip64EndOfCentralDirRecordSize         = 56
	zipFieldMax16                   uint16 = 0xFFFF
	zipFieldMax32                   uint32 = 0xFFFFFFFF
)

const fileArchiveDirMode os.FileMode = 0o755

const fileArchiveFileMode os.FileMode = 0o644

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

type archiveCounter struct {
	maxEntries   int64
	maxBytes     int64
	archiveBytes int64
	entries      int64
	bytes        int64
	skipped      int
}

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

var errArchiveStreamTooLong = errors.New("archive stream exceeds its decompressed size budget")

func archiveStreamLimit(counter *archiveCounter) int64 {
	limit := counter.maxBytes + counter.maxEntries*fileArchiveStreamOverhead
	if counter.archiveBytes > 0 {
		if ratio := counter.archiveBytes * fileArchiveRatioCap; ratio < limit {
			limit = ratio
		}
	}
	return limit
}

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

type archiveProgress struct {
	stream    grpc.ServerStreamingServer[agentv1.Progress]
	path      string
	bytesDone int64
	last      time.Time
}

func (p *archiveProgress) add(name string, n int64) error {
	p.bytesDone += n
	now := time.Now()
	if now.Sub(p.last) < fileProgressInterval {
		return nil
	}
	p.last = now
	return p.stream.Send(&agentv1.Progress{Path: name, BytesDone: p.bytesDone, BytesTotal: -1})
}

func (p *archiveProgress) done(skipped uint64) error {
	return p.stream.Send(&agentv1.Progress{
		Path: p.path, BytesDone: p.bytesDone, BytesTotal: -1, Done: true, Skipped: skipped,
	})
}

type extractDirs struct {
	perms []dirPerm
}

type dirPerm struct {
	target string
	mode   os.FileMode
}

func (d *extractDirs) record(target string, mode os.FileMode) {
	d.perms = append(d.perms, dirPerm{target: target, mode: mode})
}

func (d *extractDirs) apply(root *fsroot.Root) error {
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

type extractState struct {
	root      *fsroot.Root
	verified  map[string]struct{}
	statCalls int
}

func newExtractState(root *fsroot.Root) *extractState {
	return &extractState{root: root, verified: make(map[string]struct{})}
}

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
		if info, err := file.Stat(); err == nil && tmpInfo != nil && os.SameFile(info, tmpInfo) {
			counter.skipped++
			return nil
		}
		if err := counter.addEntry(opened.Size); err != nil {
			return err
		}
		if err := sink.addFile(ctx, name, opened, file); err != nil {
			return err
		}
		return progress.add(name, opened.Size)
	default:
		counter.skipped++
		return nil
	}
}

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
	if err := dirs.apply(root); err != nil {
		return fileError("extract", err)
	}
	return progress.done(0)
}

func (s *FileServer) extractArchive(ctx context.Context, root *fsroot.Root, archive *os.File, size int64, format agentv1.ArchiveFormat, dst, src string, overwrite bool, counter *archiveCounter, dirs *extractDirs, state *extractState, progress *archiveProgress) error {
	switch format {
	case agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP:
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

type zipCentralDirectory struct {
	entries      int64
	size         int64
	offset       int64
	recordOffset int64
}

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

func zip64CentralDirectoryBounds(f io.ReaderAt, eocdOffset, size int64) (zipCentralDirectory, bool, error) {
	if eocdOffset < zip64EndOfCentralDirLocatorSize {
		return zipCentralDirectory{}, false, nil
	}
	locator := make([]byte, zip64EndOfCentralDirLocatorSize)
	if _, err := f.ReadAt(locator, eocdOffset-zip64EndOfCentralDirLocatorSize); err != nil {
		return zipCentralDirectory{}, false, err
	}
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
			if err := counter.addEntry(0); err != nil {
				return err
			}
			continue
		}
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

func (s *FileServer) extractDir(root *fsroot.Root, dst, rel string, mode os.FileMode, dirs *extractDirs, state *extractState) error {
	target := path.Join(dst, rel)
	if err := guardTrash(root, "extract", target); err != nil {
		return err
	}
	if s.protectsAppDir(root, target) {
		return status.Error(codes.PermissionDenied, "extract: an application directory cannot be replaced")
	}
	if mode == 0 {
		mode = fileArchiveDirMode
	}
	if err := state.verifyAncestors(dst, rel); err != nil {
		return err
	}
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

func (s *FileServer) extractFile(ctx context.Context, root *fsroot.Root, target string, mode os.FileMode, size int64, overwrite bool, src string, r io.Reader, state *extractState, progress *archiveProgress) error {
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
			return size - remaining, fmt.Errorf("%w: archive entry is truncated or corrupt: %v", fsroot.ErrInvalid, readErr)
		}
	}
	return size, nil
}

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

type archiveSink interface {
	addDir(name string, entry fsroot.Entry) error
	addFile(ctx context.Context, name string, entry fsroot.Entry, body io.Reader) error
	close() error
}

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

type zipSink struct {
	zw *zip.Writer
}

func (z *zipSink) addDir(name string, entry fsroot.Entry) error {
	header := &zip.FileHeader{Name: strings.TrimSuffix(name, "/") + "/", Method: zip.Store, Modified: entry.Modified}
	header.SetMode(entry.Mode.Perm() | os.ModeDir)
	_, err := z.zw.CreateHeader(header)
	return err
}

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

type tarSink struct {
	tw *tar.Writer
}

func (t *tarSink) addDir(name string, entry fsroot.Entry) error {
	return t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     strings.TrimSuffix(name, "/") + "/",
		Mode:     int64(entry.Mode.Perm()),
		ModTime:  entry.Modified,
	})
}

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
