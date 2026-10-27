//go:build darwin

package stats

// Darwin has no Linux-style proc/sysfs magic numbers on the candidate paths the
// agent is configured with, so no candidate is treated as pseudo.
func pseudoFilesystem(int64) bool { return false }
