package incidents

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type IncidentView struct {
	ID           uuid.UUID `json:"id"`
	Detector     string    `json:"detector"`
	Scope        string    `json:"scope"`
	ScopeID      string    `json:"scope_id,omitempty"`
	NodeID       string    `json:"node_id,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	ResourceType string    `json:"resource_type,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	ResourceName string    `json:"resource_name,omitempty"`

	Severity  Severity  `json:"severity"`
	Lifecycle Lifecycle `json:"lifecycle"`
	Coverage  Coverage  `json:"coverage"`

	FirstObservedAt time.Time  `json:"first_observed_at"`
	LastObservedAt  time.Time  `json:"last_observed_at"`
	LastValidAt     *time.Time `json:"last_valid_at,omitempty"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	CloseReason     string     `json:"close_reason,omitempty"`

	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	SnoozedUntil   *time.Time `json:"snoozed_until,omitempty"`

	DeliveryState    string `json:"delivery_state"`
	DeliveryHold     string `json:"delivery_hold,omitempty"`
	DeliveryEvent    string `json:"delivery_event,omitempty"`
	DeliveryAttempts int    `json:"delivery_attempts"`
	DeliveryError    string `json:"delivery_error,omitempty"`

	Evidence     string `json:"evidence,omitempty"`
	GroupedUnder string `json:"grouped_under,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Page struct {
	Incidents  []IncidentView `json:"incidents"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type EventView struct {
	ID            uuid.UUID `json:"id"`
	Kind          string    `json:"kind"`
	FromLifecycle Lifecycle `json:"from_lifecycle,omitempty"`
	ToLifecycle   Lifecycle `json:"to_lifecycle,omitempty"`
	FromSeverity  Severity  `json:"from_severity,omitempty"`
	ToSeverity    Severity  `json:"to_severity,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	ActorID       string    `json:"actor_id,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type DeliveryView struct {
	ID         uuid.UUID `json:"id"`
	Transition string    `json:"transition"`
	Severity   Severity  `json:"severity"`
	State      string    `json:"state"`
	Attempts   int       `json:"attempts"`
	LastError  string    `json:"last_error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func view(inc Incident, grouping map[uuid.UUID]Incident, showGrouping bool) IncidentView {
	coverage := inc.Coverage
	groupedUnder := ""
	if inc.NodeID != uuid.Nil {
		if nodeIncident, ok := grouping[inc.NodeID]; ok && nodeIncident.ID != inc.ID && nodeIncident.Active() {
			// A symptom cannot claim fresh coverage while its node is
			// unavailable; grouping never resolves it or invents a cause.
			coverage = CoverageUnknown
			if showGrouping {
				groupedUnder = nodeIncident.ID.String()
			}
		}
	}
	return IncidentView{
		ID:               inc.ID,
		Detector:         inc.Detector,
		Scope:            inc.ScopeKind,
		ScopeID:          uuidString(inc.ScopeID),
		NodeID:           uuidString(inc.NodeID),
		ProjectID:        uuidString(inc.ProjectID),
		ResourceType:     inc.ResourceType,
		ResourceID:       uuidString(inc.ResourceID),
		ResourceName:     inc.ResourceName,
		Severity:         inc.Severity,
		Lifecycle:        inc.Lifecycle,
		Coverage:         coverage,
		FirstObservedAt:  inc.FirstObservedAt,
		LastObservedAt:   inc.LastObservedAt,
		LastValidAt:      inc.LastValidAt,
		ConfirmedAt:      inc.ConfirmedAt,
		ResolvedAt:       inc.ResolvedAt,
		CloseReason:      inc.CloseReason,
		AcknowledgedAt:   inc.AcknowledgedAt,
		AcknowledgedBy:   uuidString(inc.AcknowledgedBy),
		SnoozedUntil:     inc.SnoozedUntil,
		DeliveryState:    inc.DeliveryState,
		DeliveryHold:     displayHold(inc, coverage, groupedUnder),
		DeliveryEvent:    inc.DeliveryEvent,
		DeliveryAttempts: inc.DeliveryAttempts,
		DeliveryError:    inc.DeliveryError,
		Evidence:         inc.Evidence,
		GroupedUnder:     groupedUnder,
		CreatedAt:        inc.CreatedAt,
		UpdatedAt:        inc.UpdatedAt,
	}
}

func displayHold(inc Incident, coverage Coverage, groupedUnder string) string {
	if inc.DeliveryHold != "" {
		return inc.DeliveryHold
	}
	if inc.AcknowledgedAt != nil {
		return "acknowledged"
	}
	if inc.SnoozedUntil != nil {
		return "snoozed until " + inc.SnoozedUntil.UTC().Format(time.RFC3339)
	}
	if groupedUnder != "" {
		return "node unavailable"
	}
	switch coverage {
	case CoverageExpected:
		return "expected maintenance"
	case CoverageUnknown, CoverageStale:
		return "coverage " + string(coverage)
	}
	return ""
}

func eventView(event Event) EventView {
	return EventView{
		ID:            event.ID,
		Kind:          event.Kind,
		FromLifecycle: event.FromLifecycle,
		ToLifecycle:   event.ToLifecycle,
		FromSeverity:  event.FromSeverity,
		ToSeverity:    event.ToSeverity,
		Reason:        event.Reason,
		ActorID:       uuidString(event.ActorID),
		Detail:        event.Detail,
		CreatedAt:     event.CreatedAt,
	}
}

func deliveryView(intent DeliveryIntent) DeliveryView {
	return DeliveryView{
		ID:         intent.ID,
		Transition: intent.Transition,
		Severity:   intent.Severity,
		State:      intent.State,
		Attempts:   intent.Attempts,
		LastError:  intent.LastError,
		CreatedAt:  intent.CreatedAt,
	}
}

func uuidString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func encodeCursor(cursor Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursor.At.UTC().Format(time.RFC3339Nano) + "|" + cursor.ID.String()))
}

func decodeCursor(raw string) (*Cursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor", ErrValidation)
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: invalid cursor", ErrValidation)
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor", ErrValidation)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor", ErrValidation)
	}
	return &Cursor{At: at, ID: id}, nil
}
