package updatecore

import "time"

// defaultTimeout bounds a single HTTP request when the caller sets no timeout.
const defaultTimeout = 10 * time.Second

// defaultDuration returns value when positive, otherwise fallback.
func defaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
