package deploy

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// maxBuildLogBytes caps the persisted build log (JUS-84). The tail is kept:
// the failure that ended the run is at the end.
const maxBuildLogBytes = 256 << 10 // 256 KiB

// logRecorder tees streamed lines for persistence. Lines are stored exactly
// as emitted, so the stored log carries the same redaction the call sites
// already applied to the stream (compose secrets, clone credentials). The
// buffer is a rolling tail: old lines are dropped while recording, so a noisy
// build cannot grow memory without bound (finalize only re-caps the tail cut).
type logRecorder struct {
	mu    sync.Mutex
	lines []string
	size  int // bytes held, counting one separator per line
}

// record appends one emitted line, dropping the oldest lines past the cap.
func (r *logRecorder) record(line string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	r.size += len(line) + 1
	// ponytail: whole lines are dropped, so the cut is rune-safe by construction.
	for len(r.lines) > 1 && r.size > maxBuildLogBytes {
		r.size -= len(r.lines[0]) + 1
		r.lines = r.lines[1:]
	}
}

// finalize joins the recorded lines and caps the result for storage.
func (r *logRecorder) finalize() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return sanitizeBuildLog(strings.Join(r.lines, "\n"))
}

// sanitizeBuildLog makes raw log output safe for the text column: valid
// UTF-8 (Postgres rejects the rest) capped at maxBuildLogBytes, keeping the
// tail. The tail cut lands on a rune boundary.
func sanitizeBuildLog(raw string) string {
	clean := strings.ToValidUTF8(raw, "\uFFFD")
	if len(clean) <= maxBuildLogBytes {
		return clean
	}
	cut := clean[len(clean)-maxBuildLogBytes:]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[1:] // drop only a leading partial rune
	}
	return cut
}
