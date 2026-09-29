package teams

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// integrationDSN points at the dev database from deploy/compose.dev.yml.
// Override with GOTHAM_TEST_DSN, e.g. to force a skip in CI with
// GOTHAM_TEST_DSN=postgres://nope.
const integrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// newTestStore opens the dev database and skips the test when it is not
// reachable, matching the other DB-backed suites.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		dsn = integrationDSN
	}
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

// TestSetMemberRoleSerializesConcurrentDemotions is the F4 regression against
// the real database: two owners demoting each other must not leave the team
// with zero owners. The team-row lock (SELECT ... FOR UPDATE) serializes the
// two policy transactions, so the second one observes the first demotion and
// is refused by the last-owner rule.
func TestSetMemberRoleSerializesConcurrentDemotions(t *testing.T) {
	st := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	newUser := func(label string) pgtype.UUID {
		email := fmt.Sprintf("be-8.2-f4-%s-%d@example.com", label, suffix)
		user, err := st.CreateUser(ctx, email, nil)
		if err != nil {
			t.Fatalf("create user %s: %v", label, err)
		}
		return user.ID
	}
	alice := newUser("a")
	bob := newUser("b")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, id := range []pgtype.UUID{alice, bob} {
			if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Logf("cleanup user: %v", err)
			}
		}
	})

	team, err := st.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:   pgUUID(uuid.New()),
		Name: fmt.Sprintf("f4-%d", suffix),
	}, alice)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteTeam(cleanupCtx, team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})
	if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: team.ID,
		UserID: bob,
		Role:   string(RoleOwner),
	}); err != nil {
		t.Fatalf("add second owner: %v", err)
	}

	svc := NewService(Config{Store: st, Logger: discardLogger()})

	// Both owners demote the other at the same time.
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, pair := range [][2]pgtype.UUID{{alice, bob}, {bob, alice}} {
		wg.Add(1)
		go func(i int, actor, target pgtype.UUID) {
			defer wg.Done()
			_, results[i] = svc.SetMemberRole(ctx,
				uuid.UUID(actor.Bytes), uuid.UUID(team.ID.Bytes), uuid.UUID(target.Bytes), RoleReadOnly)
		}(i, pair[0], pair[1])
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrLastOwner), errors.Is(err, ErrForbidden):
			// The second transaction sees the first demotion: either the
			// last-owner rule fires or the demoted actor no longer manages.
		default:
			t.Fatalf("unexpected concurrent demotion error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful demotions = %d, want exactly one (the second must hit the last-owner rule)", succeeded)
	}

	owners, err := st.CountTeamOwners(ctx, team.ID)
	if err != nil {
		t.Fatalf("count owners: %v", err)
	}
	if owners < 1 {
		t.Fatalf("owners after concurrent demotions = %d, want at least 1", owners)
	}
}
