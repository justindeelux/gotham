package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestListPrimaryApplicationDomainsScoped(t *testing.T) {
	if err := store.ProbeOnce(context.Background(), testDSN()); err != nil {
		if testDSNExplicit() {
			t.Fatalf("open store: %v", err)
		}
		t.Skipf("no database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, testDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, testDSN(), store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("jus233-scope-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	envID, serverID := seedEnvColumns(t, ctx, st, pool)

	create := func(name, domain string) sqlc.Application {
		app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
			UserID: user.ID, ServerID: serverID, EnvironmentID: envID,
			Name: name, Provider: "github", Repo: "acme/demo",
			CloneUrl: "https://github.com/acme/demo.git", Branch: "main", BuildPack: "dockerfile",
		})
		if err != nil {
			t.Fatalf("CreateApplication(%s): %v", name, err)
		}
		if _, err := st.CreateApplicationDomain(ctx, sqlc.CreateApplicationDomainParams{
			ApplicationID: app.ID, Domain: domain, IsPrimary: true, Port: 3000, Protocol: "http",
		}); err != nil {
			t.Fatalf("CreateApplicationDomain(%s): %v", domain, err)
		}
		return app
	}
	first := create("scope-one", fmt.Sprintf("one-%d.example.com", suffix))
	second := create("scope-two", fmt.Sprintf("two-%d.example.com", suffix))

	rows, err := st.ListPrimaryApplicationDomains(ctx, []pgtype.UUID{first.ID})
	if err != nil {
		t.Fatalf("ListPrimaryApplicationDomains: %v", err)
	}
	if len(rows) != 1 || rows[0].ApplicationID != first.ID {
		t.Fatalf("rows = %#v, want only the requested application", rows)
	}
	if rows[0].Domain != fmt.Sprintf("one-%d.example.com", suffix) {
		t.Fatalf("domain = %q, want the first application's primary", rows[0].Domain)
	}

	both, err := st.ListPrimaryApplicationDomains(ctx, []pgtype.UUID{first.ID, second.ID})
	if err != nil {
		t.Fatalf("ListPrimaryApplicationDomains(both): %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("rows = %#v, want both requested applications", both)
	}
	if empty, err := st.ListPrimaryApplicationDomains(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty ids = %#v / %v, want no rows and no error", empty, err)
	}
}
