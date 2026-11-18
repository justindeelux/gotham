package servers

import "sync"

// LogOnce remembers the last error message it reported so a persistent
// condition is logged once instead of on every poll. Call Report with err on
// failure and nil on success: the next failure is reported again.
//
// The zero value is ready to use.
type LogOnce struct {
	mu   sync.Mutex
	last string
}

// Report records err and reports whether the caller should log it: true when
// the message differs from the previous one, false for a repeat.
func (l *LogOnce) Report(err error) bool {
	message := ""
	if err != nil {
		message = err.Error()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := message != l.last
	l.last = message
	return changed && err != nil
}
