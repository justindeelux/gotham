package deploy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestApplicationDomainUniquenessConcurrent proves the per-node normalized
// domain index rejects a second binding even when two users write
// concurrently (BE-6.1 F6). It skips when no database is reachable.
func TestApplicationDomainUniquenessConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	explicit := integrationDSNExplicit()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres/migrations are unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)
	repo := newStoreRepository(st, "integration-secret")

	suffix := time.Now().UnixNano()
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p6-domain-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	users := make([]uuid.UUID, 0, 2)
	for i := 0; i < 2; i++ {
		row, err := st.CreateUser(ctx, fmt.Sprintf("p6-domain-%d-%d@example.com", suffix, i), nil)
		if err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		userID := uuid.UUID(row.ID.Bytes)
		users = append(users, userID)
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", row.ID); err != nil {
				t.Logf("cleanup user: %v", err)
			}
		})
	}

	domain := fmt.Sprintf("race-%d.example.com", suffix)
	start := make(chan struct{})
	results := make(chan error, len(users))
	for i, userID := range users {
		go func(i int, userID uuid.UUID) {
			<-start
			_, err := repo.CreateApplication(ctx, Application{
				UserID:     userID,
				ServerID:   serverID,
				Name:       fmt.Sprintf("app-%d", i),
				CloneURL:   "https://github.com/acme/demo.git",
				Branch:     "main",
				BuildPack:  "dockerfile",
				BaseDomain: domain,
				Port:       3000,
				HostPort:   18080,
			}, nil, nil, nil)
			results <- err
		}(i, userID)
	}
	close(start)

	success, conflicts := 0, 0
	for i := 0; i < len(users); i++ {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected write error: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d, want exactly one winner and one conflict", success, conflicts)
	}
}
