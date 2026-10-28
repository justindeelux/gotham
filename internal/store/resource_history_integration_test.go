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

func resourceTestScope(t *testing.T, ctx context.Context, st *store.Store) (appID, serverID pgtype.UUID) {
	t.Helper()
	userID, envID, serverID := seedImageScope(t, ctx, st)
	appID = pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := st.DB.Exec(ctx, `INSERT INTO applications (id, user_id, environment_id, server_id, name, source_type)
		VALUES ($1, $2, $3, $4, $5, 'image')`,
		appID, userID, envID, serverID, fmt.Sprintf("rh-app-%d", time.Now().UnixNano())); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	dbID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := st.DB.Exec(ctx, `INSERT INTO databases (id, user_id, environment_id, server_id, name, engine, status)
		VALUES ($1, $2, $3, $4, $5, 'postgres', 'running')`,
		dbID, userID, envID, serverID, fmt.Sprintf("rh-db-%d", time.Now().UnixNano())); err != nil {
		t.Fatalf("insert database: %v", err)
	}
	return appID, serverID
}

func resourceSampleRow(serverID, appID pgtype.UUID, at time.Time, replica string, cpu, rx float64) sqlc.InsertResourceSamplesParams {
	return sqlc.InsertResourceSamplesParams{
		ServerID:         serverID,
		ResourceType:     "application",
		ResourceID:       appID,
		ApplicationID:    appID,
		Service:          "web",
		DeploymentID:     "deploy-1",
		Replica:          replica,
		ContainerID:      "container-" + replica,
		ObservedAt:       pgtype.Timestamptz{Time: at, Valid: true},
		CpuCores:         &cpu,
		NetRxBps:         &rx,
		MemLimitBytes:    ptrInt64(1024),
		ExpectedReplicas: 2,
		ObservedReplicas: 2,
		CoveragePartial:  false,
	}
}

func ptrInt64(value int64) *int64 { return &value }

func openIntegrationStore(t *testing.T, ctx context.Context) *store.Store {
	t.Helper()
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
	return store.New(pool)
}

func TestResourceHistoryRollupsAndRetention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st := openIntegrationStore(t, ctx)
	appID, serverID := resourceTestScope(t, ctx, st)

	base := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	var rows []sqlc.InsertResourceSamplesParams
	for minute := 0; minute < 5; minute++ {
		at := base.Add(time.Duration(minute) * time.Minute)
		rows = append(rows,
			resourceSampleRow(serverID, appID, at, "1", 0.2, 100),
			resourceSampleRow(serverID, appID, at, "2", 0.3, 200),
		)
	}
	// One replica disappears from the last bucket; coverage must say so.
	partialAt := base.Add(5 * time.Minute)
	partial := resourceSampleRow(serverID, appID, partialAt, "1", 0.5, 500)
	partial.CoveragePartial = true
	partial.ObservedReplicas = 1
	rows = append(rows, partial)
	if err := st.InsertResourceSamples(ctx, rows); err != nil {
		t.Fatalf("InsertResourceSamples: %v", err)
	}

	if _, err := st.UpsertResourceMinuteRollups(ctx, base, base.Add(6*time.Minute)); err != nil {
		t.Fatalf("UpsertResourceMinuteRollups: %v", err)
	}
	points, err := st.ListResourceRollupPoints(ctx, sqlc.ListResourceRollupPointsParams{
		ResourceType: "application",
		ResourceID:   appID,
		Tier:         "minute",
		RangeStart:   pgtype.Timestamptz{Time: base, Valid: true},
		RangeEnd:     pgtype.Timestamptz{Time: base.Add(6 * time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("ListResourceRollupPoints: %v", err)
	}
	if len(points) != 6*2 {
		t.Fatalf("minute rows = %d; want 12 (6 buckets x 2 metrics)", len(points))
	}
	cpu := points[0]
	if cpu.Samples != 2 || cpu.Replicas != 2 || cpu.CoveragePartial {
		t.Errorf("complete bucket = %+v; want 2 samples, 2 replicas, complete", cpu)
	}
	if cpu.ValueSum != 0.5 {
		t.Errorf("cpu sum = %v; want 0.5", cpu.ValueSum)
	}
	foundPartial := false
	for _, point := range points {
		if point.CoveragePartial && point.Replicas == 1 {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Error("partial replica coverage was not retained in the rollup")
	}

	if _, err := st.UpsertResourceHourRollups(ctx, base, base.Add(6*time.Minute)); err != nil {
		t.Fatalf("UpsertResourceHourRollups: %v", err)
	}
	hours, err := st.ListResourceRollupPoints(ctx, sqlc.ListResourceRollupPointsParams{
		ResourceType: "application",
		ResourceID:   appID,
		Tier:         "hour",
		RangeStart:   pgtype.Timestamptz{Time: base.Truncate(time.Hour), Valid: true},
		RangeEnd:     pgtype.Timestamptz{Time: base.Truncate(time.Hour).Add(time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("ListResourceRollupPoints hour: %v", err)
	}
	sums := map[string]float64{}
	for _, point := range hours {
		sums[point.Metric] += point.ValueSum
	}
	if sums["cpu_cores"] != 3 {
		t.Errorf("hourly CPU sum = %v; want 3 (five complete buckets plus the partial one)", sums["cpu_cores"])
	}

	// Retention removes only the expired tier; the active incident state and
	// the app itself are untouched.
	old := base.Add(-25 * time.Hour)
	if err := st.InsertResourceSamples(ctx, []sqlc.InsertResourceSamplesParams{
		resourceSampleRow(serverID, appID, old, "1", 0.1, 10),
	}); err != nil {
		t.Fatal(err)
	}
	if deleted, err := st.DeleteResourceSamplesBefore(ctx, time.Now().Add(-24*time.Hour)); err != nil || deleted == 0 {
		t.Fatalf("delete raw = %d, %v; want at least one", deleted, err)
	}
	if deleted, err := st.DeleteResourceRollupsBefore(ctx, "minute", base.Add(6*time.Minute)); err != nil || deleted == 0 {
		t.Errorf("delete minute = %d, %v; want expired rows removed", deleted, err)
	}
	if remaining, err := st.ListResourceRollupPoints(ctx, sqlc.ListResourceRollupPointsParams{
		ResourceType: "application", ResourceID: appID, Tier: "minute",
		RangeStart: pgtype.Timestamptz{Time: base, Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: base.Add(6 * time.Minute), Valid: true},
	}); err != nil || len(remaining) != 0 {
		t.Errorf("minute rows after retention = %d, %v; want 0", len(remaining), err)
	}
	if remaining, err := st.ListResourceRollupPoints(ctx, sqlc.ListResourceRollupPointsParams{
		ResourceType: "application", ResourceID: appID, Tier: "hour",
		RangeStart: pgtype.Timestamptz{Time: base.Truncate(time.Hour), Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: base.Truncate(time.Hour).Add(time.Hour), Valid: true},
	}); err != nil || len(remaining) == 0 {
		t.Errorf("hour rows after minute retention = %d, %v; want them kept", len(remaining), err)
	}
	if _, err := st.DeleteResourceRollupsBefore(ctx, "hour", base.Add(6*time.Minute)); err != nil {
		t.Errorf("delete hour: %v", err)
	}
	if _, err := st.GetApplication(ctx, appID); err != nil {
		t.Errorf("application disappeared with history retention: %v", err)
	}

	if err := st.DeleteResourceHistory(ctx, "application", appID); err != nil {
		t.Fatalf("DeleteResourceHistory: %v", err)
	}
	exists, err := st.ResourceHistoryExists(ctx, "application", appID)
	if err != nil || exists {
		t.Errorf("history still exists after delete: %v %v", exists, err)
	}
}

func TestResourceHistoryStorageFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("storage fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	st := openIntegrationStore(t, ctx)
	appID, serverID := resourceTestScope(t, ctx, st)

	// A small three-replica fixture: one day at the 30-second cadence.
	const replicas = 3
	const samples = 24 * 60 * 2
	end := time.Now().UTC().Truncate(30 * time.Second)
	start := end.Add(-time.Duration(samples) * 30 * time.Second)
	rows := make([]sqlc.InsertResourceSamplesParams, 0, replicas*samples)
	for replica := 0; replica < replicas; replica++ {
		for index := 0; index < samples; index++ {
			cpu := 0.1 + float64(replica)*0.05
			rx := float64(index % 1000)
			rows = append(rows, resourceSampleRow(serverID, appID, start.Add(time.Duration(index)*30*time.Second), fmt.Sprintf("%d", replica+1), cpu, rx))
		}
	}
	started := time.Now()
	if err := st.InsertResourceSamples(ctx, rows); err != nil {
		t.Fatalf("InsertResourceSamples: %v", err)
	}
	writeElapsed := time.Since(started)
	if _, err := st.UpsertResourceMinuteRollups(ctx, start, end); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if _, err := st.UpsertResourceHourRollups(ctx, start, end); err != nil {
		t.Fatalf("hour rollup: %v", err)
	}

	var rawBytes, rawCount, rollupBytes, rollupCount int64
	if err := st.DB.QueryRow(ctx, `SELECT coalesce(sum(pg_column_size(r)), 0), count(*) FROM resource_samples r WHERE resource_id=$1`, appID).Scan(&rawBytes, &rawCount); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, `SELECT coalesce(sum(pg_column_size(r)), 0), count(*) FROM resource_metric_rollups r WHERE resource_id=$1`, appID).Scan(&rollupBytes, &rollupCount); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture: %d replicas x %d samples = %d rows in %s; raw=%d bytes (%.1f B/row), rollups=%d rows %d bytes (%.1f B/row)",
		replicas, samples, rawCount, writeElapsed, rawBytes, float64(rawBytes)/float64(rawCount),
		rollupCount, rollupBytes, float64(rollupBytes)/float64(maxInt64(rollupCount, 1)))
	if rawCount != int64(replicas*samples) {
		t.Errorf("raw rows = %d; want %d", rawCount, replicas*samples)
	}
	if rollupCount == 0 {
		t.Error("fixture produced no rollups")
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
