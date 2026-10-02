package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestSessionSweepCutoffs pins the two retention thresholds.
func TestSessionSweepCutoffs(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiredBefore, revokedBefore := sessionSweepCutoffs(now)

	if want := now.Add(-7 * 24 * time.Hour); !expiredBefore.Equal(want) {
		t.Errorf("expiredBefore = %v, want %v", expiredBefore, want)
	}
	if want := now.Add(-30 * 24 * time.Hour); !revokedBefore.Equal(want) {
		t.Errorf("revokedBefore = %v, want %v", revokedBefore, want)
	}
}

// TestSessionSweeperSweepsStaleSessions drives the sweep against a real
// database: expired rows past the grace period and long-revoked rows go, while
// fresh and recently unusable rows stay. It skips without Postgres.
func TestSessionSweeperSweepsStaleSessions(t *testing.T) {
	_, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("sweep")
	cleanupUser(t, st, email)
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	now := time.Now()
	createSession := func(hash string, expires, revokedAt time.Time) {
		t.Helper()
		if _, err := st.CreateSession(ctx, sqlc.CreateSessionParams{
			UserID:            user.ID,
			RefreshHash:       hash,
			ExpiresAt:         pgtype.Timestamptz{Time: expires, Valid: true},
			CredentialVersion: user.CredentialVersion,
		}); err != nil {
			t.Fatalf("CreateSession(%s): %v", hash, err)
		}
		if !revokedAt.IsZero() {
			if _, err := st.DB.Exec(ctx,
				"UPDATE sessions SET revoked_at = $2 WHERE refresh_hash = $1", hash, revokedAt); err != nil {
				t.Fatalf("backdate revoked_at(%s): %v", hash, err)
			}
		}
	}

	createSession("fresh", now.Add(time.Hour), time.Time{})
	createSession("expired-recent", now.Add(-24*time.Hour), time.Time{})
	createSession("expired-stale", now.Add(-8*24*time.Hour), time.Time{})
	createSession("revoked-recent", now.Add(time.Hour), now.Add(-24*time.Hour))
	createSession("revoked-stale", now.Add(time.Hour), now.Add(-31*24*time.Hour))
	// A row revoked yesterday but expired long ago must survive: the 30-day
	// reuse window is measured from revocation, not expiry.
	createSession("revoked-recent-expired", now.Add(-10*24*time.Hour), now.Add(-24*time.Hour))

	sweeper := NewSessionSweeper(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sweeper.now = func() time.Time { return now }

	deleted, err := sweeper.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("Sweep deleted %d rows, want the 2 stale rows", deleted)
	}

	var remaining int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", user.ID).Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 4 {
		t.Fatalf("sessions remaining = %d, want the 4 fresh/recent rows", remaining)
	}
}
