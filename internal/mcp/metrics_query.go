package mcp

import "time"

// Kept local to avoid importing control-plane services; parity is tested against servers.
func parseMetricsQuery(from, to time.Time, step string) (time.Duration, error) {
	bucket, window := time.Minute, 24*time.Hour
	switch step {
	case "", "1m":
	case "1h":
		bucket, window = time.Hour, 30*24*time.Hour
	case "1d":
		bucket, window = 24*time.Hour, 30*24*time.Hour
	default:
		return 0, &Error{Code: "invalid_request"}
	}
	if from.IsZero() || to.IsZero() || !from.Before(to) || to.Sub(from) > window {
		return 0, &Error{Code: "invalid_request"}
	}
	return bucket, nil
}
