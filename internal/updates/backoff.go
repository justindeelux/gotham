package updates

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// Control-plane failed-update backoff (LOW-2). The AUTO_UPDATE loop must not
// re-download and re-apply a release that just rolled back on every interval:
// each rollback restarts the control plane, so a flat retry crash-loops. This
// mirrors the agent's persisted, seeded backoff (agent/updater.go): the failed
// attempt count is stored on disk so it escalates across the restarts that
// follow a rollback, and a *newer* release is still offered immediately.
const (
	// updateBackoffSeedBase is the first failed-attempt delay (5m); each further
	// attempt doubles it, capped at updateBackoffMax.
	updateBackoffSeedBase = 5 * time.Minute
	updateBackoffMax      = time.Hour
)

// updateBackoff persists the failed-attempt state for the version that rolled
// back. The record is a single updatecore.Status (result "backoff", Detail the
// count, At the failure time) so it reuses the hardened status store.
type updateBackoff struct {
	store  *updatecore.StatusStore
	logger *slog.Logger
}

// newUpdateBackoff builds the backoff around store, or a no-op when store is
// nil (backoff disabled).
func newUpdateBackoff(store *updatecore.StatusStore, logger *slog.Logger) *updateBackoff {
	if logger == nil {
		logger = slog.Default()
	}
	return &updateBackoff{store: store, logger: logger}
}

// blocked reports whether version is still inside its recorded backoff window.
func (b *updateBackoff) blocked(version string) bool {
	count, at, ok := b.read(version)
	if !ok || count < 1 || at.IsZero() {
		return false
	}
	return time.Now().Before(at.Add(backoffDelay(count)))
}

// record counts a failed attempt for version (a synchronous apply failure) and
// arms the exponential delay from now.
func (b *updateBackoff) record(version string, at time.Time) {
	count := 1
	if prev, _, ok := b.read(version); ok && prev >= 1 {
		count = prev + 1
	}
	b.write(version, count, at)
	b.logger.Info("updates: backing off a failed update",
		"version", version, "failures", count, "retry_in", backoffDelay(count).String())
}

// seedFromStatus seeds the persisted backoff from the durable, root-owned status
// at startup: a rolled_back/rollback_failed status for a version newer than the
// running one arms the backoff before the first auto-update tick, so a restart
// does not immediately re-apply the broken release. An unchanged failure (same
// status timestamp) keeps its escalated count instead of resetting it; an `ok`
// status clears the record.
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
		// The stored failure is at or after this rollback (the same rollback, or
		// a later synchronous failure): keep its count and time. Rewinding `at`
		// to an older rollback would let a restart retry sooner than the
		// recorded backoff allows.
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

// clear drops the record (operator reset, or a healthy update).
func (b *updateBackoff) clear() {
	if b == nil || b.store == nil {
		return
	}
	_ = b.store.Remove()
}

// read returns the recorded count and failure time when the stored record names
// version.
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

// write persists the failed-attempt count and failure time for version.
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

// backoffDelay is the exponential failed-attempt delay base * 2^(count-1),
// capped at updateBackoffMax.
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

// newerVersion reports whether offered is a strictly newer release than current.
// Unparsable versions fail closed (false).
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
