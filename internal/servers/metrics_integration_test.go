package servers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestMetricsRangeAndRetention is the Phase 8 exit criterion for the server
// time series: samples round-trip, the range query aggregates into epoch
// aligned buckets at the requested step, and the retention sweep deletes only
// rows older than the 30-day horizon. It runs against the dev database and
// skips when none is available.
func TestMetricsRangeAndRetention(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server := addMetricsNode(t, ctx, service, st, "metrics-range")

	// Two buckets of the 1m series, with the first bucket carrying three
	// samples so its average is observable.
	base := time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Hour)
	insertMetric(t, st, server.ID, base, 0.10)
	insertMetric(t, st, server.ID, base.Add(10*time.Second), 0.20)
	insertMetric(t, st, server.ID, base.Add(50*time.Second), 0.30)
	insertMetric(t, st, server.ID, base.Add(time.Minute), 0.60)

	// A sample well past the retention horizon, used by the sweep below.
	old := base.Add(-31 * 24 * time.Hour)
	insertMetric(t, st, server.ID, old, 0.99)

	points, err := service.Metrics(ctx, server.ID, base, base.Add(2*time.Minute), "1m")
	if err != nil {
		t.Fatalf("Metrics(1m): %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2 (%+v)", len(points), points)
	}
	if !points[0].Bucket.Equal(base) {
		t.Errorf("first bucket = %s, want %s", points[0].Bucket, base)
	}
	if math.Abs(points[0].CPUUsage-0.2) > 1e-9 {
		t.Errorf("first bucket cpu average = %v, want 0.2", points[0].CPUUsage)
	}
	if math.Abs(points[1].CPUUsage-0.6) > 1e-9 {
		t.Errorf("second bucket cpu average = %v, want 0.6", points[1].CPUUsage)
	}
	if points[0].NetRxBps != 1024 || points[0].DiskWriteBps != 4096 || points[0].ContainerCount != 1 {
		t.Errorf("first bucket io = %+v, want the inserted values", points[0])
	}

	// The 1h series has one bucket holding all four fresh samples.
	points, err = service.Metrics(ctx, server.ID, base, base.Add(2*time.Hour), "1h")
	if err != nil {
		t.Fatalf("Metrics(1h): %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("hourly points = %d, want 1", len(points))
	}
	if !points[0].Bucket.Equal(base.Truncate(time.Hour)) {
		t.Errorf("hourly bucket = %s, want %s", points[0].Bucket, base.Truncate(time.Hour))
	}
	if math.Abs(points[0].CPUUsage-0.3) > 1e-9 {
		t.Errorf("hourly cpu average = %v, want 0.3", points[0].CPUUsage)
	}

	// The 1d series aligns to midnight UTC and holds the same samples.
	points, err = service.Metrics(ctx, server.ID, base, base.Add(48*time.Hour), "1d")
	if err != nil {
		t.Fatalf("Metrics(1d): %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("daily points = %d, want 1", len(points))
	}
	if !points[0].Bucket.Equal(base.Truncate(24 * time.Hour)) {
		t.Errorf("daily bucket = %s, want %s", points[0].Bucket, base.Truncate(24*time.Hour))
	}

	// A window that holds no samples is an empty series, not an error, and an
	// empty bucket is omitted rather than invented.
	points, err = service.Metrics(ctx, server.ID, base.Add(10*time.Minute), base.Add(20*time.Minute), "1m")
	if err != nil {
		t.Fatalf("Metrics(empty window): %v", err)
	}
	if len(points) != 0 {
		t.Errorf("empty window points = %d, want 0", len(points))
	}

	// Invalid queries are validation errors, not storage errors.
	for name, call := range map[string]func() ([]MetricPoint, error){
		"inverted range": func() ([]MetricPoint, error) {
			return service.Metrics(ctx, server.ID, base.Add(time.Minute), base, "1m")
		},
		"unknown step": func() ([]MetricPoint, error) {
			return service.Metrics(ctx, server.ID, base, base.Add(time.Hour), "5m")
		},
		"oversized window": func() ([]MetricPoint, error) {
			return service.Metrics(ctx, server.ID, base, base.Add(25*time.Hour), "1m")
		},
	} {
		if _, err := call(); !errors.Is(err, ErrValidation) {
			t.Errorf("%s error = %v, want ErrValidation", name, err)
		}
	}

	// Retention removes the out-of-horizon row and nothing else.
	sweeper := NewMetricsSweeper(st, discardLogger())
	sweeper.now = func() time.Time { return base }
	deleted, err := sweeper.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted < 1 {
		t.Errorf("sweep deleted %d rows, want the out-of-horizon sample to go", deleted)
	}
	rows, err := st.ListServerMetrics(ctx, sqlc.ListServerMetricsParams{
		ServerID:   pgUUID(server.ID),
		Bucket:     pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
		RangeStart: pgtype.Timestamptz{Time: old.Add(-time.Hour), Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: base.Add(time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("ListServerMetrics after sweep: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("buckets after sweep = %d, want the one fresh bucket", len(rows))
	}
}

// TestMetricsTeamIsolation verifies the read authorization of the series: a
// node of another team answers ErrNotFound, while every member of the node's
// team — including read_only — may read it.
func TestMetricsTeamIsolation(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	alice, err := st.CreateUser(ctx, fmt.Sprintf("be-8.4-metrics-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bob, err := st.CreateUser(ctx, fmt.Sprintf("be-8.4-metrics-b-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = ANY($1)", []any{alice.ID, bob.ID}); err != nil {
			t.Logf("cleanup users: %v", err)
		}
	})

	shared, err := st.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:   pgUUID(uuid.New()),
		Name: fmt.Sprintf("metrics-%d", suffix),
	}, alice.ID)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteTeam(cleanupCtx, shared.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})
	if _, err := st.DB.Exec(ctx,
		"INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'read_only')", shared.ID, bob.ID); err != nil {
		t.Fatalf("add read_only member: %v", err)
	}

	aliceTeam := teams.WithScope(ctx, teams.Scope{
		UserID: uuid.UUID(alice.ID.Bytes),
		TeamID: uuid.UUID(shared.ID.Bytes),
		Role:   teams.RoleOwner,
	})
	node, err := service.Add(aliceTeam, uuid.UUID(alice.ID.Bytes), "metrics-team-node", "127.0.0.1", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(node.ID)); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	sampleAt := time.Now().UTC()
	insertMetric(t, st, node.ID, sampleAt, 0.5)
	from := sampleAt.Add(-time.Hour)
	to := sampleAt.Add(time.Hour)

	reader := teams.WithScope(ctx, teams.Scope{
		UserID: uuid.UUID(bob.ID.Bytes),
		TeamID: uuid.UUID(shared.ID.Bytes),
		Role:   teams.RoleReadOnly,
	})
	if points, err := service.Metrics(reader, node.ID, from, to, "1m"); err != nil {
		t.Errorf("read_only Metrics = %v, want a series", err)
	} else if len(points) != 1 {
		t.Errorf("read_only points = %d, want 1", len(points))
	}

	// The owner (a team member) reads the same series.
	if points, err := service.Metrics(aliceTeam, node.ID, from, to, "1m"); err != nil {
		t.Errorf("owner Metrics = %v, want a series", err)
	} else if len(points) != 1 {
		t.Errorf("owner points = %d, want 1", len(points))
	}

	foreign := teams.WithScope(ctx, teams.Scope{
		UserID: uuid.UUID(bob.ID.Bytes),
		TeamID: teams.PersonalTeamID(uuid.UUID(bob.ID.Bytes)),
		Role:   teams.RoleOwner,
	})
	if _, err := service.Metrics(foreign, node.ID, from, to, "1m"); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign-team Metrics = %v, want ErrNotFound", err)
	}

	if _, err := service.Metrics(aliceTeam, uuid.New(), from, to, "1m"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown node Metrics = %v, want ErrNotFound", err)
	}
}

// TestHeartbeatSkipsMetricsWhenDisabled verifies the kill switch: with
// FEATURE_METRICS=false the heartbeat still updates the servers row but appends
// no time-series sample.
func TestHeartbeatSkipsMetricsWhenDisabled(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodeID := uniqueNodeID("node-metrics-disabled")
	if _, err := service.RegisterNode(ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux"}); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, row.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	t.Setenv(FeatureEnv, "false")
	sentAt := time.Now().UTC()
	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{
		CpuUsage: 0.42,
		SentAt:   timestamppb.New(sentAt),
	}); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	updated, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID after heartbeat: %v", err)
	}
	if updated.CpuUsage == nil || *updated.CpuUsage != 0.42 {
		t.Errorf("cpu_usage = %v, want 0.42 (the servers row must still update)", updated.CpuUsage)
	}

	var count int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM server_metrics WHERE server_id = $1", row.ID).Scan(&count); err != nil {
		t.Fatalf("count metrics: %v", err)
	}
	if count != 0 {
		t.Errorf("metrics rows = %d, want 0 with FEATURE_METRICS=false", count)
	}
}

// addMetricsNode creates a node for the metrics tests and deletes it on
// cleanup (its samples cascade).
func addMetricsNode(t *testing.T, ctx context.Context, service *ServerService, st *store.Store, name string) *Server {
	t.Helper()
	server, err := service.Add(ctx, uuid.New(), name, "127.0.0.1", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("add node: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(server.ID)); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})
	return server
}

// insertMetric stores one sample directly, without going through the heartbeat
// path, so the range test controls the timestamps exactly.
func insertMetric(t *testing.T, st *store.Store, serverID uuid.UUID, at time.Time, cpu float64) {
	t.Helper()
	err := st.InsertServerMetric(context.Background(), sqlc.InsertServerMetricParams{
		ServerID:       pgUUID(serverID),
		RecordedAt:     pgtype.Timestamptz{Time: at, Valid: true},
		CpuUsage:       cpu,
		MemUsage:       0.5,
		DiskUsage:      0.25,
		NetRxBps:       1024,
		NetTxBps:       512,
		DiskReadBps:    2048,
		DiskWriteBps:   4096,
		ContainerCount: 1,
	})
	if err != nil {
		t.Fatalf("insert metric: %v", err)
	}
}
