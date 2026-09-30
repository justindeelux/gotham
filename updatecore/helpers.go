package updatecore

import "time"

// defaultTimeout bounds a single metadata HTTP request (releases API, signed
// manifest, detached signature) when the caller sets no timeout.
const defaultTimeout = 10 * time.Second

// defaultArtifactTimeout bounds the release-artifact body download. The
// artifact is tens of MiB, so the short metadata bound would abort any realistic
// download: the 32 MB control-plane binary needs >26 Mbps sustained to finish in
// 10s. Keep this separate from the metadata bound.
const defaultArtifactTimeout = 10 * time.Minute

// defaultDuration returns value when positive, otherwise fallback.
func defaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
