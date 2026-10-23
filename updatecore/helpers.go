package updatecore

import "time"

const defaultTimeout = 10 * time.Second

const defaultArtifactTimeout = 10 * time.Minute

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
