package updates

import (
	"io"
	"log/slog"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type testUpdater struct {
	*Updater
	current string
}

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
