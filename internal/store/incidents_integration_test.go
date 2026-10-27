package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func incidentParams(id, project uuid.UUID, identity, lifecycle string, first, resolved time.Time) sqlc.CreateIncidentParams {
	params := sqlc.CreateIncidentParams{
		ID:              pgtype.UUID{Bytes: id, Valid: true},
		IdentityKey:     identity,
		DetectorKey:     "app.health",
		ScopeKind:       "project",
		ProjectID:       pgtype.UUID{Bytes: project, Valid: project != uuid.Nil},
		ResourceType:    "application",
		Severity:        "warning",
		Lifecycle:       lifecycle,
		Coverage:        "fresh",
		FirstObservedAt: pgtype.Timestamptz{Time: first, Valid: true},
		LastObservedAt:  pgtype.Timestamptz{Time: first, Valid: true},
		LastValidAt:     pgtype.Timestamptz{Time: first, Valid: true},
		CreatedAt:       pgtype.Timestamptz{Time: first, Valid: true},
		UpdatedAt:       pgtype.Timestamptz{Time: first, Valid: true},
		DeliveryState:   "none",
	}
	if !resolved.IsZero() {
		params.ResolvedAt = pgtype.Timestamptz{Time: resolved, Valid: true}
	}
	return params
}

func TestIncidentVisibilityGroupingAndRetention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)

	suffix := time.Now().UnixNano()
	owner, err := st.CreateFirstUser(ctx, fmt.Sprintf("inc-owner-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := st.CreateUser(ctx, fmt.Sprintf("inc-member-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	granted, other := uuid.New(), uuid.New()
	for _, project := range []struct {
		id   uuid.UUID
		name string
	}{{granted, "granted"}, {other, "other"}} {
		if _, err := st.DB.Exec(ctx, `INSERT INTO projects(id, name) VALUES($1,$2)`, project.id, project.name); err != nil {
			t.Fatalf("insert project: %v", err)
		}
	}
	if err := st.GrantProject(ctx, granted, uuid.UUID(member.ID.Bytes), uuid.UUID(owner.ID.Bytes)); err != nil {
		t.Fatalf("grant: %v", err)
	}

	base := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	grantedIncident := uuid.New()
	otherIncident := uuid.New()
	nodeIncident := uuid.New()
	oldResolved := uuid.New()
	rows := []sqlc.CreateIncidentParams{
		incidentParams(grantedIncident, granted, "app:granted", "firing", base, time.Time{}),
		incidentParams(otherIncident, other, "app:other", "firing", base, time.Time{}),
		incidentParams(oldResolved, granted, "app:old", "resolved", base, base.Add(time.Hour)),
	}
	for _, row := range rows {
		if _, err := st.CreateIncident(ctx, row); err != nil {
			t.Fatalf("create incident: %v", err)
		}
	}
	nodeID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO servers(id, name, ip, ssh_user) VALUES($1,'node-1','127.0.0.1','root')`,
		nodeID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	nodeParams := incidentParams(nodeIncident, uuid.Nil, "node:1", "firing", base, time.Time{})
	nodeParams.ScopeKind = "node"
	nodeParams.NodeID = pgtype.UUID{Bytes: nodeID, Valid: true}
	nodeParams.ResourceType = "node"
	if _, err := st.CreateIncident(ctx, nodeParams); err != nil {
		t.Fatalf("create node incident: %v", err)
	}

	// Duplicate open occurrence for the same identity is a no-op, not a second row.
	if _, err := st.CreateIncident(ctx, incidentParams(uuid.New(), granted, "app:granted", "firing", base, time.Time{})); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate open incident = %v; want no rows", err)
	}

	memberRows, err := st.ListIncidents(ctx, sqlc.ListIncidentsParams{
		IsOwner:  false,
		UserID:   member.ID,
		RowLimit: 50,
	})
	if err != nil {
		t.Fatalf("member list: %v", err)
	}
	if len(memberRows) != 1 || uuid.UUID(memberRows[0].ID.Bytes) != grantedIncident {
		t.Fatalf("member rows = %+v; want only the granted active incident", memberRows)
	}
	memberAll, err := st.ListIncidents(ctx, sqlc.ListIncidentsParams{
		IsOwner: false, UserID: member.ID, IncludeResolved: true, RowLimit: 50,
	})
	if err != nil || len(memberAll) != 2 {
		t.Fatalf("member history = %d %v; want granted active + resolved", len(memberAll), err)
	}
	ownerRows, err := st.ListIncidents(ctx, sqlc.ListIncidentsParams{IsOwner: true, RowLimit: 50})
	if err != nil || len(ownerRows) != 3 {
		t.Fatalf("owner active = %d %v; want three", len(ownerRows), err)
	}

	nodes, err := st.ListActiveNodeIncidents(ctx)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("node incidents = %d %v", len(nodes), err)
	}

	pruned, err := st.PruneResolvedIncidents(ctx, pgtype.Timestamptz{Time: base.Add(2 * time.Hour), Valid: true})
	if err != nil || pruned != 1 {
		t.Fatalf("prune = %d %v; want the one resolved row", pruned, err)
	}
	if _, err := st.GetIncident(ctx, pgtype.UUID{Bytes: grantedIncident, Valid: true}); err != nil {
		t.Fatalf("active incident pruned: %v", err)
	}
	if _, err := st.GetIncident(ctx, pgtype.UUID{Bytes: oldResolved, Valid: true}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old resolved incident survived: %v", err)
	}
}

func TestIncidentEventHistoryIsBoundedAndOrdered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)

	incidentID := uuid.New()
	projectID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO projects(id, name) VALUES($1,$2)`, projectID, "history"); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := st.CreateIncident(ctx, incidentParams(incidentID, projectID, "app:history", "firing", time.Now().UTC(), time.Time{})); err != nil {
		t.Fatalf("create incident: %v", err)
	}
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	for i := 0; i < 5; i++ {
		if err := st.CreateIncidentEvent(ctx, sqlc.CreateIncidentEventParams{
			ID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
			IncidentID: pgtype.UUID{Bytes: incidentID, Valid: true},
			Kind:       "note",
			CreatedAt:  pgtype.Timestamptz{Time: base.Add(time.Duration(i) * time.Minute), Valid: true},
		}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}
	if _, err := st.PruneIncidentEvents(ctx, sqlc.PruneIncidentEventsParams{
		IncidentID: pgtype.UUID{Bytes: incidentID, Valid: true},
		Limit:      2,
	}); err != nil {
		t.Fatalf("prune events: %v", err)
	}
	events, err := st.ListIncidentEvents(ctx, sqlc.ListIncidentEventsParams{
		IncidentID: pgtype.UUID{Bytes: incidentID, Valid: true},
		RowLimit:   10,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d; want 2 bounded rows", len(events))
	}
	if !events[0].CreatedAt.Time.After(events[1].CreatedAt.Time) {
		t.Fatalf("events are not newest first")
	}
}
