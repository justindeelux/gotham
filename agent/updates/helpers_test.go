package updates

import (
	"io"
	"log/slog"
	"testing"
)

// discardLogger returns a logger that drops every record.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testUpdater pairs an Updater with the version state its callbacks read.
type testUpdater struct {
	*Updater
	current string
}

// newTestUpdater builds an Updater over cfg with a local version value.
func newTestUpdater(t *testing.T, cfg Config) *testUpdater {
	t.Helper()
	runner := &testUpdater{current: cfg.Version}
	u, err := New(cfg, discardLogger(), func() string { return runner.current }, func(v string) { runner.current = v })
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runner.Updater = u
	return runner
}
