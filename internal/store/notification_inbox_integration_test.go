package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestNotificationInboxVisibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	st := scratchStore(t)
	suffix := time.Now().UnixNano()
	owner, err := st.CreateFirstUser(ctx, fmt.Sprintf("inbox-owner-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := st.CreateUser(ctx, fmt.Sprintf("inbox-member-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	stranger, err := st.CreateUser(ctx, fmt.Sprintf("inbox-stranger-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	grantedAt := base.Add(15 * time.Minute)
	grantedProject, otherProject := uuid.New(), uuid.New()
	for _, project := range []struct {
		id   uuid.UUID
		name string
	}{{grantedProject, "granted"}, {otherProject, "other"}} {
		if _, err := st.DB.Exec(ctx, `INSERT INTO projects(id, name) VALUES($1, $2)`, project.id, project.name); err != nil {
			t.Fatalf("insert project: %v", err)
		}
	}

	beforeGrant := uuid.New()
	afterGrant := uuid.New()
	otherProjectRow := uuid.New()
	instanceWide := uuid.New()
	rows := []struct {
		id        uuid.UUID
		projectID uuid.UUID
		createdAt time.Time
	}{
		{beforeGrant, grantedProject, base},
		{afterGrant, grantedProject, base.Add(20 * time.Minute)},
		{otherProjectRow, otherProject, base.Add(10 * time.Minute)},
		{instanceWide, uuid.Nil, base.Add(30 * time.Minute)},
	}
	for _, row := range rows {
		var project any
		if row.projectID != uuid.Nil {
			project = row.projectID
		}
		if _, err := st.DB.Exec(ctx,
			`INSERT INTO notifications(id, event_key, resource_type, resource_id, project_id, name, host, error, created_at)
			 VALUES($1, 'deploy_failure', 'application', $2, $3, 'shop-web', 'shop.example.com', 'boom', $4)`,
			row.id, uuid.New(), project, row.createdAt); err != nil {
			t.Fatalf("insert notification: %v", err)
		}
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO project_members(project_id, user_id, created_at) VALUES($1, $2, $3)`,
		grantedProject, member.ID, grantedAt); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`UPDATE users SET notifications_seen_at = $2 WHERE id = $1`,
		member.ID, base.Add(-time.Minute)); err != nil {
		t.Fatalf("reset member marker: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`UPDATE users SET notifications_seen_at = $2 WHERE id = $1`,
		owner.ID, base.Add(10*time.Minute)); err != nil {
		t.Fatalf("reset owner marker: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`UPDATE users SET notifications_seen_at = $2 WHERE id = $1`,
		stranger.ID, base.Add(-time.Minute)); err != nil {
		t.Fatalf("reset stranger marker: %v", err)
	}

	list := func(userID pgtype.UUID) []uuid.UUID {
		t.Helper()
		found, err := st.ListVisibleNotifications(ctx, sqlc.ListVisibleNotificationsParams{ID: userID, Limit: 100})
		if err != nil {
			t.Fatalf("list notifications: %v", err)
		}
		ids := make([]uuid.UUID, 0, len(found))
		for _, row := range found {
			ids = append(ids, row.ID.Bytes)
		}
		return ids
	}
	unread := func(userID pgtype.UUID) int64 {
		t.Helper()
		count, err := st.CountUnreadNotifications(ctx, userID)
		if err != nil {
			t.Fatalf("count unread: %v", err)
		}
		return count
	}
	seenAt := func(userID pgtype.UUID) time.Time {
		t.Helper()
		var marker time.Time
		if err := st.DB.QueryRow(ctx, `SELECT notifications_seen_at FROM users WHERE id=$1`, userID).Scan(&marker); err != nil {
			t.Fatalf("read seen marker: %v", err)
		}
		return marker
	}
	assertIDs := func(what string, got []uuid.UUID, want ...uuid.UUID) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s = %v, want %v", what, got, want)
			}
		}
	}

	assertIDs("owner inbox", list(owner.ID), instanceWide, afterGrant, otherProjectRow, beforeGrant)
	assertIDs("member inbox", list(member.ID), afterGrant)
	assertIDs("ungranted member inbox", list(stranger.ID))
	if got := unread(owner.ID); got != 2 {
		t.Errorf("owner unread at the marker = %d, want 2", got)
	}
	if got := unread(member.ID); got != 1 {
		t.Errorf("member unread = %d, want 1", got)
	}
	if got := unread(stranger.ID); got != 0 {
		t.Errorf("ungranted member unread = %d, want 0", got)
	}

	memberID := pgtype.UUID{Bytes: member.ID.Bytes, Valid: true}
	if err := st.MarkNotificationsSeen(ctx, sqlc.MarkNotificationsSeenParams{
		ID:      memberID,
		Column2: pgtype.Timestamptz{Time: base.Add(20 * time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	if got := unread(member.ID); got != 0 {
		t.Errorf("unread at the marker = %d, want 0", got)
	}
	if err := st.MarkNotificationsSeen(ctx, sqlc.MarkNotificationsSeenParams{
		ID:      memberID,
		Column2: pgtype.Timestamptz{Time: base.Add(-30 * time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("mark backwards: %v", err)
	}
	if got := seenAt(member.ID); !got.Equal(base.Add(20 * time.Minute)) {
		t.Errorf("marker after a backwards move = %s, want it unchanged", got)
	}
	before := time.Now()
	if err := st.MarkNotificationsSeen(ctx, sqlc.MarkNotificationsSeenParams{
		ID:      memberID,
		Column2: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("mark into the future: %v", err)
	}
	if got := seenAt(member.ID); got.After(time.Now()) || got.Before(before.Add(-2*time.Second)) {
		t.Errorf("marker = %s, want it clamped to now", got)
	}

	if err := st.RevokeProject(ctx, grantedProject, member.ID.Bytes); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	assertIDs("revoked member inbox", list(member.ID))

	if _, err := st.DB.Exec(ctx,
		`INSERT INTO project_members(project_id, user_id, created_at) VALUES($1, $2, $3)`,
		grantedProject, member.ID, base.Add(time.Hour)); err != nil {
		t.Fatalf("re-grant: %v", err)
	}
	assertIDs("member granted after the events inbox", list(member.ID))
}
