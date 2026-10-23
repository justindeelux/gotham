package updates

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const (
	updateBackoffSeedBase = 5 * time.Minute
	updateBackoffMax      = time.Hour
)

type updateBackoff struct {
	store  *updatecore.StatusStore
	logger *slog.Logger
}

func newUpdateBackoff(store *updatecore.StatusStore, logger *slog.Logger) *updateBackoff {
	if logger == nil {
		logger = slog.Default()
	}
	return &updateBackoff{store: store, logger: logger}
}

func (b *updateBackoff) blocked(version string) bool {
	count, at, ok := b.read(version)
	if !ok || count < 1 || at.IsZero() {
		return false
	}
	return time.Now().Before(at.Add(backoffDelay(count)))
}

func (b *updateBackoff) record(version string, at time.Time) {
	count := 1
	if prev, _, ok := b.read(version); ok && prev >= 1 {
		count = prev + 1
	}
	b.write(version, count, at)
	b.logger.Info("updates: backing off a failed update",
		"version", version, "failures", count, "retry_in", backoffDelay(count).String())
}

func (b *updateBackoff) seedFromStatus(status *updatecore.Status, current string) {
	if b == nil || status == nil {
		return
	}
	switch status.Result {
	case updatecore.StatusOK:
		b.clear()
		return
	case updatecore.StatusRolledBack, updatecore.StatusRollbackFailed:
	default:
		return
	}
	if !newerVersion(status.Version, current) {
		return
	}
	prev, at, ok := b.read(status.Version)
	if ok && prev >= 1 && !at.IsZero() && !status.At.After(at) {
		return
	}
	count := 1
	if ok && prev >= 1 {
		count = prev + 1
	}
	b.write(status.Version, count, status.At)
	b.logger.Warn("updates: seeded a backoff from a durable rollback",
		"version", status.Version, "result", status.Result, "failures", count)
}

func (b *updateBackoff) clear() {
	if b == nil || b.store == nil {
		return
	}
	_ = b.store.Remove()
}

func (b *updateBackoff) read(version string) (count int, at time.Time, ok bool) {
	if b == nil || b.store == nil {
		return 0, time.Time{}, false
	}
	status, err := b.store.Read()
	if err != nil || status == nil || status.Version != version {
		return 0, time.Time{}, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(status.Detail))
	if err != nil || n < 0 {
		n = 0
	}
	return n, status.At, true
}

func (b *updateBackoff) write(version string, count int, at time.Time) {
	if b == nil || b.store == nil {
		return
	}
	if err := b.store.Write(updatecore.Status{
		Result: "backoff", Version: version, Detail: strconv.Itoa(count), At: at,
	}); err != nil {
		b.logger.Warn("updates: could not persist the failed-update count", "error", err)
	}
}

func backoffDelay(count int) time.Duration {
	delay := updateBackoffSeedBase
	for i := 1; i < count && delay < updateBackoffMax; i++ {
		delay *= 2
	}
	if delay > updateBackoffMax {
		delay = updateBackoffMax
	}
	return delay
}

func newerVersion(offered, current string) bool {
	o, err := updatecore.ParseVersion(offered)
	if err != nil {
		return false
	}
	c, err := updatecore.ParseVersion(current)
	if err != nil {
		return false
	}
	return o.Compare(c) > 0
}

func (b *updateBackoff) active() (version string, until time.Time, ok bool) {
	if b == nil || b.store == nil {
		return "", time.Time{}, false
	}
	status, err := b.store.Read()
	if err != nil || status == nil || status.At.IsZero() {
		return "", time.Time{}, false
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(status.Detail))
	if convErr != nil || count < 1 {
		return "", time.Time{}, false
	}
	until = status.At.Add(backoffDelay(count))
	if !time.Now().Before(until) {
		return "", time.Time{}, false
	}
	return status.Version, until, true
}
