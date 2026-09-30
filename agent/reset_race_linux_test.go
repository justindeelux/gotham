//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TestWriteRetryMarkerChmodRace is R4-M1: a path-based chmod on the temp file
// can be redirected to a symlink target swapped in by the directory owner. The
// mode is set on the file descriptor, so the swap cannot redirect it. The
// attacker watches the directory with inotify (a polling attacker cannot win the
// short window) and replaces the temp file with a symlink to a 0600 victim; the
// test fails if the victim ever becomes world-readable.
func TestWriteRetryMarkerChmodRace(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("ROOT SECRET CONFIG\n"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		t.Skipf("inotify unavailable: %v", err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, dir, unix.IN_CREATE); err != nil {
		t.Skipf("inotify watch unavailable: %v", err)
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		buf := make([]byte, 64<<10)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, err := unix.Read(fd, buf)
			if err != nil || n <= 0 {
				continue
			}
			for off := 0; off+unix.SizeofInotifyEvent <= n; {
				event := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
				nameLen := int(event.Len)
				end := off + unix.SizeofInotifyEvent + nameLen
				if end > n {
					break
				}
				name := strings.TrimRight(string(buf[off+unix.SizeofInotifyEvent:end]), "\x00")
				if strings.HasPrefix(name, ".update.retry.") {
					target := filepath.Join(dir, name)
					// The link name must not itself match the temp prefix, or
					// the attacker would loop on its own IN_CREATE events.
					link := filepath.Join(dir, "attacker-"+name)
					_ = os.Remove(link)
					if err := os.Symlink(victim, link); err == nil {
						_ = os.Rename(link, target)
					}
				}
				off = end
			}
		}
	}()

	retry := filepath.Join(dir, "update.retry")
	deadline := time.Now().Add(20 * time.Second)
	for i := 0; i < 50000 && time.Now().Before(deadline); i++ {
		if err := writeRetryMarker(retry); err != nil {
			t.Fatalf("writeRetryMarker: %v", err)
		}
		info, err := os.Stat(victim)
		if err != nil {
			t.Fatalf("stat victim: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("iteration %d: victim mode %o — chmod followed a swapped symlink", i, perm)
		}
		_ = os.Remove(retry)
	}
}
