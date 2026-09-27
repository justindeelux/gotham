package builds

import "os"

// isRegular reports whether path exists and is a regular file.
func isRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// exists reports whether path exists, as a file or a directory.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
