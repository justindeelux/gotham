package deploy

import (
	"strings"
	"sync"
	"unicode/utf8"
)

const maxBuildLogBytes = 256 << 10

type logRecorder struct {
	mu    sync.Mutex
	lines []string
	size  int
}

func (r *logRecorder) record(line string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	r.size += len(line) + 1
	for len(r.lines) > 1 && r.size > maxBuildLogBytes {
		r.size -= len(r.lines[0]) + 1
		r.lines = r.lines[1:]
	}
}

func (r *logRecorder) finalize() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return sanitizeBuildLog(strings.Join(r.lines, "\n"))
}

func sanitizeBuildLog(raw string) string {
	clean := strings.ToValidUTF8(raw, "\uFFFD")
	if len(clean) <= maxBuildLogBytes {
		return clean
	}
	cut := clean[len(clean)-maxBuildLogBytes:]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[1:]
	}
	return cut
}
