package builds

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// excludedContextEntries are skipped wherever they appear in a build context.
var excludedContextEntries = map[string]bool{
	".git":      true,
	".DS_Store": true,
	"__MACOSX":  true,
}

// buildContextTar packs dir into an uncompressed tar build context and appends
// the extra files (path -> contents), which let an engine synthesise a
// Dockerfile next to the sources. Symlinks and other non-regular files are
// skipped; VCS and OS metadata are excluded.
func buildContextTar(dir string, extra map[string][]byte) ([]byte, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: stat %s: %v", ErrValidation, dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrValidation, dir)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if excludedContextEntries[entry.Name()] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Name:     rel + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			})
		}
		fileInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !fileInfo.Mode().IsRegular() {
			return nil
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     rel,
			Mode:     int64(fileInfo.Mode().Perm()),
			Size:     fileInfo.Size(),
			ModTime:  fileInfo.ModTime(),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return nil, fmt.Errorf("builds: pack context %s: %w", dir, err)
	}

	for _, name := range sortedKeys(extra) {
		content := extra[name]
		if err := tw.WriteHeader(&tar.Header{
			Name:     filepath.ToSlash(name),
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
