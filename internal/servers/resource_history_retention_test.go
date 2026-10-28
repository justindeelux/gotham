package servers

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

func TestResourceSweeperKeepsCompleteBucketsWhenTheWindowAdvances(t *testing.T) {
	service, st := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, err := st.CreateUser(ctx, fmt.Sprintf("sweep-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("sweep-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	node := addMetricsNode(t, ctx, service, st, fmt.Sprintf("sweep-node-%d", time.Now().UnixNano()))
	appID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO applications (id, user_id, environment_id, server_id, name, source_type)
		VALUES ($1, $2, $3, $4, $5, 'image')`,
		pgUUID(appID), user.ID, environment.ID, pgUUID(node.ID), fmt.Sprintf("sweep-app-%d", time.Now().UnixNano())); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = st.DB.Exec(cleanupCtx, `DELETE FROM applications WHERE id=$1`, pgUUID(appID))
	})

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	oldHour := now.Add(-25 * time.Hour).Truncate(time.Hour)
	deployment := uuid.NewString()

	row := func(at time.Time, container string) sqlc.InsertResourceSamplesParams {
		cpu := 1.0
		return sqlc.InsertResourceSamplesParams{
			ServerID:         pgUUID(node.ID),
			ResourceType:     ResourceTypeApplication,
			ResourceID:       pgUUID(appID),
			ApplicationID:    pgUUID(appID),
			Service:          "web",
			DeploymentID:     deployment,
			Replica:          "1",
			ContainerID:      container,
			ObservedAt:       pgtype.Timestamptz{Time: at, Valid: true},
			CpuCores:         &cpu,
			ExpectedReplicas: 1,
			ObservedReplicas: 1,
		}
	}

	var rows []sqlc.InsertResourceSamplesParams
	// Two samples in the first two minutes of the hour that sits on the
	// 25-hour lookback edge.
	rows = append(rows,
		row(oldHour, "edge-container"),
		row(oldHour.Add(30*time.Second), "edge-container"),
		row(oldHour.Add(time.Minute), "edge-container"),
		row(oldHour.Add(90*time.Second), "edge-container"),
	)
	// Recent data around the 2-hour minute lookback edge.
	for offset := -3 * time.Hour; offset <= 0; offset += 30 * time.Second {
		rows = append(rows, row(now.Add(offset), "recent-container"))
	}
	if err := st.InsertResourceSamples(ctx, rows); err != nil {
		t.Fatalf("InsertResourceSamples: %v", err)
	}
	// Prime the minute tier for the old hour; the hourly backfill reads it.
	if _, err := st.UpsertResourceMinuteRollups(ctx, oldHour, oldHour.Add(2*time.Minute)); err != nil {
		t.Fatalf("prime minute rollups: %v", err)
	}

	sweeper := &ResourceHistorySweeper{store: st, now: func() time.Time { return now }, logger: discardLogger(), backfill: true}
	if _, err := sweeper.Sweep(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}

	minuteBucket := now.Add(-resourceMinuteLookback)
	hourBucket := oldHour
	minuteBefore := rollupRow(t, st, appID, ResourceTierMinute, minuteBucket, time.Minute)
	hourBefore := rollupRow(t, st, appID, ResourceTierHour, hourBucket, time.Hour)
	if minuteBefore.Samples != 2 {
		t.Fatalf("minute samples = %d; want 2", minuteBefore.Samples)
	}
	if hourBefore.Samples != 4 {
		t.Fatalf("hour samples = %d; want 4", hourBefore.Samples)
	}

	for step := 1; step <= 3; step++ {
		now = now.Add(30 * time.Second)
		if _, err := sweeper.Sweep(ctx); err != nil {
			t.Fatalf("sweep %d: %v", step, err)
		}
		minuteAfter := rollupRow(t, st, appID, ResourceTierMinute, minuteBucket, time.Minute)
		hourAfter := rollupRow(t, st, appID, ResourceTierHour, hourBucket, time.Hour)
		if minuteAfter != minuteBefore {
			t.Fatalf("step %d rewrote a complete minute bucket: before=%+v after=%+v", step, minuteBefore, minuteAfter)
		}
		if hourAfter != hourBefore {
			t.Fatalf("step %d rewrote a complete hour bucket: before=%+v after=%+v", step, hourBefore, hourAfter)
		}
	}
}

func rollupRow(t *testing.T, st *store.Store, appID uuid.UUID, tier string, bucket time.Time, size time.Duration) sqlc.ListResourceRollupPointsRow {
	t.Helper()
	rows, err := st.ListResourceRollupPoints(context.Background(), sqlc.ListResourceRollupPointsParams{
		ResourceType: ResourceTypeApplication,
		ResourceID:   pgUUID(appID),
		Tier:         tier,
		RangeStart:   pgtype.Timestamptz{Time: bucket, Valid: true},
		RangeEnd:     pgtype.Timestamptz{Time: bucket.Add(size), Valid: true},
	})
	if err != nil {
		t.Fatalf("ListResourceRollupPoints(%s): %v", tier, err)
	}
	for _, row := range rows {
		if row.Metric == "cpu_cores" {
			return row
		}
	}
	t.Fatalf("no %s cpu_cores row in [%s,%s)", tier, bucket, bucket.Add(size))
	return sqlc.ListResourceRollupPointsRow{}
}
