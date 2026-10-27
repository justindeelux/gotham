package incidents_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/store"
)

const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

type countingNotifier struct {
	deliveries []incidents.Delivery
}

func (n *countingNotifier) NotifyIncident(_ context.Context, delivery incidents.Delivery) error {
	n.deliveries = append(n.deliveries, delivery)
	return nil
}

func scratchStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	name := fmt.Sprintf("inc%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)
	parsed.Path = "/" + name
	scratchDSN := parsed.String()
	if err := store.Migrate(ctx, scratchDSN, store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		admin, err := pgx.Connect(cleanupCtx, dsn)
		if err != nil {
			t.Logf("reconnect for drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`"`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})
	return store.New(pool)
}

func TestReportPersistsAcrossRestartAndHonoursGrants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	suffix := time.Now().UnixNano()
	owner, err := st.CreateFirstUser(ctx, fmt.Sprintf("inc-owner-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := st.CreateUser(ctx, fmt.Sprintf("inc-member-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	granted, other := uuid.New(), uuid.New()
	for _, project := range []struct {
		id   uuid.UUID
		name string
	}{{granted, "granted"}, {other, "other"}} {
		if _, err := st.DB.Exec(ctx, `INSERT INTO projects(id, name) VALUES($1,$2)`, project.id, project.name); err != nil {
			t.Fatalf("insert project: %v", err)
		}
	}
	if err := st.GrantProject(ctx, granted, uuid.UUID(member.ID.Bytes), uuid.UUID(owner.ID.Bytes)); err != nil {
		t.Fatalf("grant: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	notifier := &countingNotifier{}
	svc := incidents.NewService(incidents.Config{
		Store: st, Notifier: notifier, Logger: logger,
		Now: func() time.Time { return now },
	})
	open := incidents.Observation{
		Identity: incidents.Identity("app.health", "application", uuid.New()),
		Detector: "app.health", ProjectID: granted, ScopeKind: incidents.ScopeProject,
		ResourceType: "application", ResourceName: "shop",
		At: now, Valid: true, Unhealthy: true, Severity: incidents.SeverityWarning,
	}
	incident, transition, err := svc.Report(ctx, open)
	if err != nil || transition.Event != incidents.TransitionOpened {
		t.Fatalf("Report = %q %v", transition.Event, err)
	}
	if len(notifier.deliveries) != 1 {
		t.Fatalf("opening deliveries = %d", len(notifier.deliveries))
	}

	// A new service instance simulates a control-plane restart: the firing
	// row must still be active and a duplicate report must not reopen it.
	restarted := incidents.NewService(incidents.Config{
		Store: st, Notifier: &countingNotifier{}, Logger: logger,
		Now: func() time.Time { return now },
	})
	active, err := restarted.Get(ctx, uuid.UUID(owner.ID.Bytes), incident.ID)
	if err != nil || active.Lifecycle != incidents.LifecycleFiring {
		t.Fatalf("restart Get = %+v %v", active, err)
	}
	if _, tr, err := restarted.Report(ctx, open); err != nil || tr.Event != "" {
		t.Fatalf("duplicate after restart = %q %v", tr.Event, err)
	}

	// Members see and mutate only their currently granted project.
	memberID := uuid.UUID(member.ID.Bytes)
	page, err := restarted.List(ctx, memberID, incidents.ListParams{})
	if err != nil || len(page.Incidents) != 1 || page.Incidents[0].ID != incident.ID {
		t.Fatalf("member list = %+v %v", page, err)
	}
	otherObs := open
	otherObs.Identity = incidents.Identity("app.health", "application", uuid.New())
	otherObs.ProjectID = other
	otherIncident, _, err := svc.Report(ctx, otherObs)
	if err != nil {
		t.Fatalf("other Report: %v", err)
	}
	if _, err := restarted.Get(ctx, memberID, otherIncident.ID); err != incidents.ErrNotFound {
		t.Fatalf("member read unrelated project: %v", err)
	}
	if _, err := restarted.Acknowledge(ctx, memberID, otherIncident.ID); err != incidents.ErrNotFound {
		t.Fatalf("member mutated unrelated project: %v", err)
	}
	if _, err := restarted.Acknowledge(ctx, memberID, incident.ID); err != nil {
		t.Fatalf("member ack granted project: %v", err)
	}

	if err := st.RevokeProject(ctx, granted, memberID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := restarted.Get(ctx, memberID, incident.ID); err != incidents.ErrNotFound {
		t.Fatalf("revoked grant still readable: %v", err)
	}
}
