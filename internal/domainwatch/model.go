// Package domainwatch probes managed application domains from the control
// plane. It reuses the persistent incident lifecycle and the existing
// certificate-expiry observation; it does not replace agent-pushed host or
// container health.
package domainwatch

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
)

// Detector is the stable incident detector key.
const DetectorReachability = "domain_reachability"

// ResourceDomain is the incident resource type shown by the Needs attention
// surface.
const ResourceDomain = "domain"

// Persisted outcomes. Unknown gaps are never persisted as a sample.
const (
	OutcomeHealthy     = "healthy"
	OutcomeFailed      = "failed"
	OutcomeUnsupported = "unsupported"
	OutcomeUnknown     = "unknown"
)

// Status values shown by the API and UI.
const (
	StatusHealthy     = "healthy"
	StatusFailing     = "failing"
	StatusUnknown     = "unknown"
	StatusDisabled    = "disabled"
	StatusUnsupported = "unsupported"
)

// Failure reasons kept as separate evidence.
const (
	ReasonTimeout            = "timeout"
	ReasonDNS                = "dns_failure"
	ReasonTLS                = "tls_failure"
	ReasonTransport          = "transport_failure"
	ReasonUnexpectedStatus   = "unexpected_status"
	ReasonUnsafeRedirect     = "unsafe_redirect"
	ReasonBlockedDestination = "blocked_destination"
)

const (
	DefaultPath      = "/"
	DefaultMinStatus = 200
	DefaultMaxStatus = 399
	DefaultTimeout   = 5 * time.Second
	MinTimeout       = 1 * time.Second
	MaxTimeout       = 30 * time.Second

	// ProbeInterval and ProbeJitter keep every enabled domain on its own
	// roughly-one-minute schedule, independent of UI activity.
	ProbeInterval = 60 * time.Second
	ProbeJitter   = 10 * time.Second

	// StartupGrace covers a fresh process; ConfigGrace covers a domain or
	// setting change. Neither window contributes healthy or failed time.
	StartupGrace = 120 * time.Second
	ConfigGrace  = 120 * time.Second

	// maxValidGap bounds the time one completed probe may account for. It is
	// larger than one jittered interval (50–70s) but smaller than the
	// 120-second startup/configuration grace, so a gap that spans grace,
	// downtime or a restart is unknown time rather than observed uptime.
	maxValidGap = 90 * time.Second

	Retention         = 30 * 24 * time.Hour
	CoverageThreshold = 0.9

	bodyLimit      = 64 << 10
	maxRedirects   = 3
	maxConcurrency = 4
	maxEvidenceLen = 240
	maxPathLen     = 512
	maxHistoryRows = 12000

	window24h = 24 * time.Hour
	window7d  = 7 * 24 * time.Hour
	maxPoints = 168
)

// Settings are the owner-controlled knobs for one managed domain.
type Settings struct {
	Enabled     bool
	Path        string
	ExpectedMin int
	ExpectedMax int
	Timeout     time.Duration
	UpdatedAt   time.Time
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:     true,
		Path:        DefaultPath,
		ExpectedMin: DefaultMinStatus,
		ExpectedMax: DefaultMaxStatus,
		Timeout:     DefaultTimeout,
	}
}

// Target is one enabled managed domain bound to its application.
type Target struct {
	DomainID        uuid.UUID
	ApplicationID   uuid.UUID
	ApplicationName string
	Domain          string
	Protocol        string
	ProjectID       uuid.UUID
	ServerID        uuid.UUID
	CreatedAt       time.Time
	DomainUpdatedAt time.Time
	Settings        Settings
	SettingsUpdated time.Time
	// Deployment evidence: a release in flight, and the release that just
	// finished, are expected transitions rather than target failures.
	DeploymentState    string
	DeploymentStarted  *time.Time
	DeploymentFinished *time.Time
}

// URL is the derived probe URL. It is built only from the managed domain and
// the configured path; no client-supplied host or port is accepted.
func (t Target) URL() (string, error) {
	return buildURL(t)
}

// Result is one probe outcome. Detail is a short sanitized explanation; no
// response body, token or query string is ever kept.
type Result struct {
	Outcome    string
	Reason     string
	StatusCode int
	Latency    time.Duration
	Detail     string
	At         time.Time
}

// Point is one bounded chart bucket.
type Point struct {
	At             time.Time `json:"at"`
	Outcome        string    `json:"outcome"`
	CoveredSeconds int64     `json:"covered_seconds"`
}

// Window is one availability window. Availability is nil when coverage is
// below the threshold, so the UI never shows a standalone percentage built on
// mostly unknown time.
type Window struct {
	Key              string   `json:"key"`
	RangeSeconds     int64    `json:"range_seconds"`
	ObservedSeconds  int64    `json:"observed_seconds"`
	HealthySeconds   int64    `json:"healthy_seconds"`
	FailedSeconds    int64    `json:"failed_seconds"`
	Coverage         float64  `json:"coverage"`
	Availability     *float64 `json:"availability,omitempty"`
	Samples          int      `json:"samples"`
	AverageLatencyMS *int     `json:"average_latency_ms,omitempty"`
	Insufficient     bool     `json:"insufficient_coverage"`
	Points           []Point  `json:"points"`
}

// View is the API representation of one domain's website check.
type View struct {
	DomainID       string     `json:"domain_id"`
	ApplicationID  string     `json:"application_id"`
	URL            string     `json:"url"`
	ProbeLocation  string     `json:"probe_location"`
	Enabled        bool       `json:"enabled"`
	Path           string     `json:"path"`
	ExpectedMin    int        `json:"expected_min"`
	ExpectedMax    int        `json:"expected_max"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason,omitempty"`
	Coverage       string     `json:"coverage"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	LastStatus     *int       `json:"last_status,omitempty"`
	LastLatencyMS  *int       `json:"last_latency_ms,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	IncidentID     string     `json:"incident_id,omitempty"`
	Windows        []Window   `json:"windows"`
}

// SettingsInput is the validated write shape.
type SettingsInput struct {
	Enabled     bool
	Path        string
	ExpectedMin int
	ExpectedMax int
	Timeout     int
}

func (in SettingsInput) validate() (Settings, error) {
	path, err := normalizePath(in.Path)
	if err != nil {
		return Settings{}, err
	}
	if in.ExpectedMin < 100 || in.ExpectedMin > 599 || in.ExpectedMax < 100 || in.ExpectedMax > 599 {
		return Settings{}, errExpectedRange
	}
	if in.ExpectedMin > in.ExpectedMax {
		return Settings{}, errExpectedRange
	}
	if in.Timeout < int(MinTimeout/time.Second) || in.Timeout > int(MaxTimeout/time.Second) {
		return Settings{}, errTimeout
	}
	return Settings{
		Enabled:     in.Enabled,
		Path:        path,
		ExpectedMin: in.ExpectedMin,
		ExpectedMax: in.ExpectedMax,
		Timeout:     time.Duration(in.Timeout) * time.Second,
	}, nil
}

func normalizePath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return DefaultPath, nil
	}
	if len(path) > maxPathLen || !strings.HasPrefix(path, "/") {
		return "", errPath
	}
	if strings.ContainsAny(path, "?#") || strings.Contains(path, "//") {
		return "", errPath
	}
	for _, r := range path {
		if r <= 0x20 || r == 0x7f || r == '\\' {
			return "", errPath
		}
	}
	return path, nil
}

// evidence renders bounded, non-secret probe evidence for the incident.
func (r Result) evidence() string {
	parts := make([]string, 0, 4)
	if r.Reason != "" {
		parts = append(parts, "reason="+r.Reason)
	}
	if r.StatusCode > 0 {
		parts = append(parts, "status="+itoa(r.StatusCode))
	}
	if r.Latency > 0 {
		parts = append(parts, "latency_ms="+itoa(int(r.Latency/time.Millisecond)))
	}
	if r.Detail != "" {
		parts = append(parts, r.Detail)
	}
	text := strings.Join(parts, " ")
	if len(text) > maxEvidenceLen {
		text = text[:maxEvidenceLen]
	}
	return text
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (t Target) incidentIdentity() string {
	return incidents.Identity(DetectorReachability, ResourceDomain, t.DomainID)
}

func (t Target) resourceName() string {
	name := strings.TrimSpace(t.ApplicationName)
	if name == "" {
		return t.Domain
	}
	return name + " / " + t.Domain
}
