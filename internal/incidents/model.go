package incidents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Lifecycle is the persisted condition state. Pending is being confirmed,
// firing is actionable, resolved keeps the occurrence in history.
type Lifecycle string

const (
	LifecyclePending  Lifecycle = "pending"
	LifecycleFiring   Lifecycle = "firing"
	LifecycleResolved Lifecycle = "resolved"
)

func (l Lifecycle) Valid() bool {
	switch l {
	case LifecyclePending, LifecycleFiring, LifecycleResolved:
		return true
	default:
		return false
	}
}

// Severity is independent of lifecycle: a firing incident can be warning or
// critical, and acknowledgement never changes it.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) Valid() bool {
	return s == SeverityWarning || s == SeverityCritical
}

// Coverage describes the evidence behind an incident. It is independent of
// severity and lifecycle, so an active incident can still have unknown
// coverage while its source is unreachable.
type Coverage string

const (
	CoverageFresh    Coverage = "fresh"
	CoveragePartial  Coverage = "partial"
	CoverageStale    Coverage = "stale"
	CoverageUnknown  Coverage = "unknown"
	CoverageExpected Coverage = "expected"
)

// Scope kinds accepted by the lifecycle.
const (
	ScopeNode    = "node"
	ScopeProject = "project"
	ScopeGlobal  = "global"
)

// Transitions are the durable delivery intents this package hands to the
// notification layer.
const (
	TransitionOpened          = "opened"
	TransitionSeverityChanged = "severity_changed"
	TransitionRecovered       = "recovered"
	TransitionReminder        = "reminder"
)

// Delivery states of the local handoff, stored on the incident and its intent.
const (
	DeliveryNone       = "none"
	DeliveryPending    = "pending"
	DeliverySending    = "sending"
	DeliveryDispatched = "dispatched"
	DeliveryFailed     = "failed"
	DeliverySuppressed = "suppressed"
)

type Incident struct {
	ID        uuid.UUID
	Identity  string
	Detector  string
	ScopeKind string
	ScopeID   uuid.UUID

	NodeID    uuid.UUID
	ProjectID uuid.UUID

	ResourceType string
	ResourceID   uuid.UUID
	ResourceName string

	Severity  Severity
	Lifecycle Lifecycle
	Coverage  Coverage

	FirstObservedAt   time.Time
	LastObservedAt    time.Time
	LastValidAt       *time.Time
	ConfirmedAt       *time.Time
	RecoveryStartedAt *time.Time
	ResolvedAt        *time.Time
	CloseReason       string

	AcknowledgedAt *time.Time
	AcknowledgedBy uuid.UUID

	SnoozedUntil        *time.Time
	SnoozeNotifiedUntil *time.Time

	DeliveryState        string
	DeliveryHold         string
	DeliveryEvent        string
	DeliveryAttempts     int
	DeliveryError        string
	LastNotifiedSeverity Severity

	Evidence string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (i Incident) Active() bool { return i.Lifecycle != LifecycleResolved }

// Observation is one detector report. At is the detector's evidence time; a
// caller that has no trustworthy time leaves it zero and the service clock is
// used.
type Observation struct {
	Identity  string
	Detector  string
	ScopeKind string
	ScopeID   uuid.UUID
	NodeID    uuid.UUID
	ProjectID uuid.UUID

	ResourceType string
	ResourceID   uuid.UUID
	ResourceName string

	At       time.Time
	Severity Severity
	Evidence string

	// Valid is false for an invalid or stale report. It never counts as
	// healthy and interrupts both confirmation and recovery timing.
	Valid bool
	// Expected marks intended stop/restart/deploy work (or an equivalent
	// grace window) supplied by the detector. It also interrupts timing
	// instead of opening or recovering an incident.
	Expected bool
	// Unhealthy is the detector verdict when no numeric thresholds are used.
	Unhealthy bool
	// Value with TriggerThreshold/ClearThreshold gives independent trigger
	// and clear thresholds. Values between them are indeterminate.
	Value            *float64
	TriggerThreshold *float64
	ClearThreshold   *float64

	ConfirmFor time.Duration
	RecoverFor time.Duration
	// MaxGap bounds the evidence gap that still counts as continuous.
	MaxGap time.Duration

	// SuppressDelivery lets a producer keep the incident durable while a
	// legacy notification remains the single user-visible delivery (the node
	// connectivity path).
	SuppressDelivery bool
	SuppressReason   string
}

// Delivery is the durable handoff the notification layer renders and routes.
type Delivery struct {
	IncidentID   uuid.UUID
	Transition   string
	Severity     Severity
	Detector     string
	Name         string
	Host         string
	Evidence     string
	ResourceType string
	ResourceID   uuid.UUID
	ProjectID    uuid.UUID
	NodeID       uuid.UUID
	At           time.Time
}

// Notifier performs channel selection and rendering. It must be cheap: the
// local intent is already committed, so an error only leaves it retryable.
type Notifier interface {
	NotifyIncident(context.Context, Delivery) error
}

// Identity builds the stable detector identity. Container IDs and other
// occurrence evidence never belong in it; pass only application/database IDs
// and service or filesystem naming parts.
func Identity(detector, resourceType string, resourceID uuid.UUID, parts ...string) string {
	segments := make([]string, 0, len(parts)+3)
	segments = append(segments, strings.TrimSpace(detector), strings.TrimSpace(resourceType))
	if resourceID != uuid.Nil {
		segments = append(segments, resourceID.String())
	}
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return strings.Join(segments, "|")
}

var (
	// ErrNotFound deliberately covers both a missing incident and one the
	// caller may not see.
	ErrNotFound   = errors.New("incidents: not found")
	ErrForbidden  = errors.New("incidents: forbidden")
	ErrValidation = errors.New("incidents: validation")
)

const (
	DefaultMaxGap    = 90 * time.Second
	DefaultRetention = 30 * 24 * time.Hour
	DefaultPageSize  = 50
	MaxPageSize      = 100
	MaxHistory       = 200
)

func (o Observation) validate() error {
	if strings.TrimSpace(o.Identity) == "" {
		return fmt.Errorf("%w: identity is required", ErrValidation)
	}
	if strings.TrimSpace(o.Detector) == "" {
		return fmt.Errorf("%w: detector is required", ErrValidation)
	}
	if o.Severity != "" && !o.Severity.Valid() {
		return fmt.Errorf("%w: severity must be warning or critical", ErrValidation)
	}
	if o.ConfirmFor < 0 || o.RecoverFor < 0 || o.MaxGap < 0 {
		return fmt.Errorf("%w: durations must not be negative", ErrValidation)
	}
	return nil
}

func (o Observation) maxGap() time.Duration {
	if o.MaxGap > 0 {
		return o.MaxGap
	}
	return DefaultMaxGap
}

func (o Observation) scope() string {
	switch {
	case o.ScopeKind != "":
		return o.ScopeKind
	case o.NodeID != uuid.Nil:
		return ScopeNode
	case o.ProjectID != uuid.Nil:
		return ScopeProject
	default:
		return ScopeGlobal
	}
}
