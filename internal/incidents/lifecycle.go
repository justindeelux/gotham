package incidents

import (
	"time"

	"github.com/google/uuid"
)

type evidenceState int

const (
	stateHealthy evidenceState = iota
	stateUnhealthy
	stateIndeterminate
	stateUnknown
	stateExpected
)

// Transition is one deterministic lifecycle step. Event is empty when the
// report changed evidence only (duplicate, pending confirmation, stale gap).
type Transition struct {
	Event  string
	Reason string
}

func (t Transition) notify() bool {
	switch t.Event {
	case TransitionOpened, TransitionSeverityChanged, TransitionRecovered:
		return true
	default:
		return false
	}
}

func classify(obs Observation) evidenceState {
	if !obs.Valid {
		return stateUnknown
	}
	if obs.Expected {
		return stateExpected
	}
	if obs.Value != nil && obs.TriggerThreshold != nil && obs.ClearThreshold != nil {
		switch {
		case *obs.Value >= *obs.TriggerThreshold:
			return stateUnhealthy
		case *obs.Value <= *obs.ClearThreshold:
			return stateHealthy
		default:
			// Between independent trigger and clear thresholds: never healthy,
			// never a new trigger. This is the anti-jitter band.
			return stateIndeterminate
		}
	}
	if obs.Unhealthy {
		return stateUnhealthy
	}
	return stateHealthy
}

// advance applies one observation to an active incident (or creates the first
// pending occurrence when inc is nil). It is pure: the caller persists the
// returned incident in one transaction with the transition and its intent.
func advance(inc *Incident, obs Observation, now time.Time) (*Incident, Transition) {
	at := obs.At
	if at.IsZero() {
		at = now
	}
	at = at.UTC()
	now = now.UTC()
	state := classify(obs)

	if inc == nil {
		return open(obs, state, at, now)
	}
	if !at.After(inc.LastObservedAt) {
		// Duplicate or out-of-order reports must not advance timing or create
		// another opening.
		return inc, Transition{}
	}

	previous := inc.Lifecycle
	gap := at.Sub(inc.LastObservedAt)

	inc.LastObservedAt = at
	inc.UpdatedAt = now
	if obs.Evidence != "" {
		inc.Evidence = obs.Evidence
	}
	if obs.ResourceName != "" {
		inc.ResourceName = obs.ResourceName
	}

	switch state {
	case stateUnhealthy:
		inc.Coverage = CoverageFresh
		inc.LastValidAt = timePtr(at)
		inc.RecoveryStartedAt = nil
		severityChanged := advanceSeverity(inc, obs, at, gap)
		if previous == LifecyclePending {
			if gap > obs.maxGap() {
				inc.FirstObservedAt = at
			}
			if at.Sub(inc.FirstObservedAt) >= obs.ConfirmFor {
				inc.Lifecycle = LifecycleFiring
				inc.ConfirmedAt = timePtr(at)
				inc.DeliveryEvent = TransitionOpened
				inc.DeliveryState = DeliveryPending
				return inc, Transition{Event: TransitionOpened}
			}
			return inc, Transition{}
		}
		inc.Lifecycle = LifecycleFiring
		if severityChanged {
			inc.DeliveryEvent = TransitionSeverityChanged
			inc.DeliveryState = DeliveryPending
			return inc, Transition{Event: TransitionSeverityChanged}
		}
		return inc, Transition{}

	case stateHealthy:
		inc.Coverage = CoverageFresh
		inc.LastValidAt = timePtr(at)
		inc.SeveritySince = nil
		if gap > obs.maxGap() || inc.RecoveryStartedAt == nil {
			// A restart or a stale gap cannot borrow recovery time: the stable
			// window restarts at the first fresh healthy report.
			inc.RecoveryStartedAt = timePtr(at)
		}
		if at.Sub(*inc.RecoveryStartedAt) < obs.RecoverFor {
			return inc, Transition{}
		}
		inc.Lifecycle = LifecycleResolved
		inc.ResolvedAt = timePtr(at)
		inc.RecoveryStartedAt = nil
		if previous == LifecyclePending {
			// A healthy report breaks the sustained condition; the next
			// unhealthy run must confirm from scratch.
			inc.FirstObservedAt = at
		}
		if previous == LifecycleFiring {
			inc.CloseReason = "recovered"
			inc.DeliveryEvent = TransitionRecovered
			inc.DeliveryState = DeliveryPending
			return inc, Transition{Event: TransitionRecovered, Reason: "recovered"}
		}
		inc.CloseReason = "cleared_before_confirmation"
		return inc, Transition{Event: "closed", Reason: inc.CloseReason}

	case stateIndeterminate:
		inc.Coverage = CoveragePartial
		inc.RecoveryStartedAt = nil
		severityChanged := advanceSeverity(inc, obs, at, gap)
		if previous == LifecyclePending {
			// Evidence that is neither healthy nor unhealthy cannot confirm a
			// sustained condition; the run restarts when evidence returns.
			inc.FirstObservedAt = at
		}
		if previous == LifecycleFiring && severityChanged {
			inc.DeliveryEvent = TransitionSeverityChanged
			inc.DeliveryState = DeliveryPending
			return inc, Transition{Event: TransitionSeverityChanged}
		}

	case stateExpected:
		inc.Coverage = CoverageExpected
		inc.RecoveryStartedAt = nil
		inc.SeveritySince = nil
		if previous == LifecyclePending {
			inc.FirstObservedAt = at
		}

	case stateUnknown:
		inc.Coverage = CoverageUnknown
		inc.RecoveryStartedAt = nil
		inc.SeveritySince = nil
		if previous == LifecyclePending {
			inc.FirstObservedAt = at
		}
	}
	return inc, Transition{}
}

// advanceSeverity moves the incident between warning and critical only after
// SeverityFor has elapsed. Evidence gaps restart the window, so a single sample
// never flips the severity.
func advanceSeverity(inc *Incident, obs Observation, at time.Time, gap time.Duration) bool {
	if !obs.Severity.Valid() {
		inc.SeveritySince = nil
		return false
	}
	if obs.Severity == inc.Severity {
		inc.SeveritySince = nil
		return false
	}
	if gap > obs.maxGap() || inc.SeveritySince == nil {
		inc.SeveritySince = timePtr(at)
	}
	if obs.SeverityFor > 0 && at.Sub(*inc.SeveritySince) < obs.SeverityFor {
		return false
	}
	inc.Severity = obs.Severity
	inc.SeveritySince = nil
	return true
}

func open(obs Observation, state evidenceState, at, now time.Time) (*Incident, Transition) {
	if state != stateUnhealthy {
		return nil, Transition{}
	}
	severity := obs.Severity
	if !severity.Valid() {
		severity = SeverityWarning
	}
	scopeID := obs.ScopeID
	if scopeID == uuid.Nil {
		scopeID = obs.NodeID
	}
	if scopeID == uuid.Nil {
		scopeID = obs.ProjectID
	}
	inc := &Incident{
		ID:              uuid.New(),
		Identity:        obs.Identity,
		Detector:        obs.Detector,
		ScopeKind:       obs.scope(),
		ScopeID:         scopeID,
		NodeID:          obs.NodeID,
		ProjectID:       obs.ProjectID,
		ResourceType:    obs.ResourceType,
		ResourceID:      obs.ResourceID,
		ResourceName:    obs.ResourceName,
		Severity:        severity,
		Lifecycle:       LifecyclePending,
		Coverage:        CoverageFresh,
		FirstObservedAt: at,
		LastObservedAt:  at,
		LastValidAt:     timePtr(at),
		DeliveryState:   DeliveryNone,
		Evidence:        obs.Evidence,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if obs.ConfirmFor <= 0 {
		inc.Lifecycle = LifecycleFiring
		inc.ConfirmedAt = timePtr(at)
		inc.DeliveryEvent = TransitionOpened
		inc.DeliveryState = DeliveryPending
		return inc, Transition{Event: TransitionOpened}
	}
	return inc, Transition{}
}

func timePtr(t time.Time) *time.Time {
	t = t.UTC()
	return &t
}
