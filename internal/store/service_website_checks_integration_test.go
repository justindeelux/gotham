package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// A service domain shares the monitor tables with application domains; the
// owner check and the delete trigger are the two schema guarantees behind it.
func TestServiceWebsiteCheckOwnerAndCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("svc-website-check-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM applications WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM services WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	envID, serverID := seedEnvColumns(t, ctx, st, pool)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        user.ID,
		ServerID:      serverID,
		EnvironmentID: envID,
		Name:          fmt.Sprintf("svc-check-app-%d", suffix),
		SourceType:    "image",
		Provider:      "image",
		Branch:        "main",
		BuildPack:     "image",
		ImageRef:      "nginx:alpine",
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	appDomain, err := st.CreateApplicationDomain(ctx, sqlc.CreateApplicationDomainParams{
		ApplicationID: app.ID,
		Domain:        fmt.Sprintf("app-%d.example.com", suffix),
		IsPrimary:     true,
		Port:          80,
		Protocol:      "https",
	})
	if err != nil {
		t.Fatalf("create application domain: %v", err)
	}
	service, err := st.CreateService(ctx, sqlc.CreateServiceParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        user.ID,
		ServerID:      serverID,
		EnvironmentID: envID,
		Name:          fmt.Sprintf("svc-check-%d", suffix),
		Status:        "running",
		ComposeYaml:   "services:\n  web:\n    image: nginx:alpine\n",
		Env:           []byte("{}"),
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	serviceDomain, err := st.CreateServiceDomain(ctx, sqlc.CreateServiceDomainParams{
		ID:             pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ServiceID:      service.ID,
		ComposeService: "web",
		Port:           80,
		Domain:         fmt.Sprintf("svc-%d.example.com", suffix),
		Protocol:       "https",
		IsPrimary:      true,
	})
	if err != nil {
		t.Fatalf("create service domain: %v", err)
	}

	insertProbeResult := func(domainID pgtype.UUID, appID, serviceID any) error {
		_, err := pool.Exec(ctx, `INSERT INTO domain_probe_results
			(domain_id, application_id, service_id, observed_at, interval_start, outcome)
			VALUES ($1, $2, $3, now(), now(), 'healthy')`, domainID, appID, serviceID)
		return err
	}
	if err := insertProbeResult(appDomain.ID, app.ID, nil); err != nil {
		t.Fatalf("application result: %v", err)
	}
	if err := insertProbeResult(serviceDomain.ID, nil, service.ID); err != nil {
		t.Fatalf("service result: %v", err)
	}
	if err := insertProbeResult(serviceDomain.ID, app.ID, service.ID); err == nil {
		t.Fatal("result with both owners was accepted")
	}
	if err := insertProbeResult(serviceDomain.ID, nil, nil); err == nil {
		t.Fatal("result without an owner was accepted")
	}

	for _, domainID := range []pgtype.UUID{appDomain.ID, serviceDomain.ID} {
		if _, err := pool.Exec(ctx, `INSERT INTO domain_monitor_settings (domain_id) VALUES ($1)`, domainID); err != nil {
			t.Fatalf("monitor settings %v: %v", domainID, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO domain_monitor_state (domain_id) VALUES ($1)`, domainID); err != nil {
			t.Fatalf("monitor state %v: %v", domainID, err)
		}
	}
	monitorRows := func(domainID pgtype.UUID) (int, int, int) {
		t.Helper()
		var settings, state, results int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM domain_monitor_settings WHERE domain_id = $1),
			(SELECT count(*) FROM domain_monitor_state WHERE domain_id = $1),
			(SELECT count(*) FROM domain_probe_results WHERE domain_id = $1)`,
			domainID).Scan(&settings, &state, &results); err != nil {
			t.Fatalf("count monitor rows: %v", err)
		}
		return settings, state, results
	}

	if _, err := pool.Exec(ctx, `DELETE FROM application_domains WHERE id = $1`, appDomain.ID); err != nil {
		t.Fatalf("delete application domain: %v", err)
	}
	if settings, state, results := monitorRows(appDomain.ID); settings+state+results != 0 {
		t.Fatalf("application domain delete kept monitor rows: %d/%d/%d", settings, state, results)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM services WHERE id = $1`, service.ID); err != nil {
		t.Fatalf("delete service: %v", err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM service_domains WHERE id = $1`, serviceDomain.ID).Scan(&left); err != nil {
		t.Fatalf("count service domains: %v", err)
	}
	if left != 0 {
		t.Fatalf("service delete kept its domain row")
	}
	if settings, state, results := monitorRows(serviceDomain.ID); settings+state+results != 0 {
		t.Fatalf("service delete kept monitor rows: %d/%d/%d", settings, state, results)
	}
}
