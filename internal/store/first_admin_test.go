package store_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func scratchStore(t *testing.T) *store.Store {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	base := testDSN()
	if err := store.ProbeOnce(ctx, base); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	name := fmt.Sprintf("fa%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect for scratch database: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)

	parsed.Path = "/" + name
	dsn := parsed.String()

	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open scratch store: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		admin, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`"`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})
	return store.New(pool)
}

func TestStoreFirstUserIsOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st := scratchStore(t)

	first, err := st.CreateFirstUser(ctx, "first@example.com", nil)
	if err != nil {
		t.Fatalf("CreateFirstUser: %v", err)
	}
	if first.Role != "owner" {
		t.Fatalf("bootstrap account role = %q, want owner", first.Role)
	}

	second, err := st.CreateUser(ctx, "second@example.com", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if second.Role != "member" {
		t.Fatalf("plain CreateUser role = %q, want member", second.Role)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, id := range []any{first.ID, second.ID} {
			if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Logf("cleanup user: %v", err)
			}
		}
	})

	var legacyRole string
	if err := st.DB.QueryRow(ctx,
		`INSERT INTO users (email) VALUES ($1) RETURNING role`,
		fmt.Sprintf("legacy-%d@example.com", time.Now().UnixNano())).Scan(&legacyRole); err != nil {
		t.Fatalf("legacy-style insert: %v", err)
	}
	if legacyRole != "member" {
		t.Fatalf("column default role = %q, want member", legacyRole)
	}
}

func TestStoreRejectsSecondOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st := scratchStore(t)

	first, err := st.CreateFirstUser(ctx, "owner@example.com", nil)
	if err != nil {
		t.Fatalf("CreateFirstUser: %v", err)
	}
	second, err := st.CreateUser(ctx, "member@example.com", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, id := range []any{first.ID, second.ID} {
			if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Logf("cleanup user: %v", err)
			}
		}
	})

	if _, err := st.DB.Exec(ctx, "UPDATE users SET role = 'owner' WHERE id = $1", second.ID); err == nil {
		t.Fatal("a second owner was accepted, want the unique index to reject it")
	}
}

func TestStoreConcurrentFirstRegistrationsYieldOneOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := scratchStore(t)

	const attempts = 32
	type result struct {
		user sqlc.User
		err  error
	}
	var wg sync.WaitGroup
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			email := fmt.Sprintf("first-admin-race-%d-%d@example.com", time.Now().UnixNano(), i)
			user, err := st.CreateFirstUser(ctx, email, nil)
			results <- result{user: user, err: err}
		}(i)
	}
	wg.Wait()
	close(results)

	wins := 0
	var winner sqlc.User
	for res := range results {
		switch {
		case res.err == nil:
			wins++
			winner = res.user
		case errors.Is(res.err, store.ErrInstanceHasAccount):
		default:
			t.Fatalf("unexpected error: %v", res.err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent bootstraps succeeded %d times, want exactly 1", wins)
	}
	if winner.Role != "owner" {
		t.Fatalf("bootstrap race winner role = %q, want owner", winner.Role)
	}

	if err := st.DeleteUserAndPersonalTeam(ctx, winner.ID); err != nil {
		t.Fatalf("cleanup winner: %v", err)
	}
}
