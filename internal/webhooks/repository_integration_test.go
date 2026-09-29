package webhooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// defaultIntegrationDSN points at the dev database from deploy/compose.dev.yml.
// Override with GOTHAM_TEST_DSN; a value that cannot be reached skips the test
// so CI stays green without a database.
const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// integrationDSN returns the DSN the repository integration test should use.
func integrationDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultIntegrationDSN
}

// TestStoreRepositoryRoundtrip exercises the PostgreSQL adapter — including
// secret sealing and the partial unique indexes that make dedupe work —
// against a real server. It skips when no database is reachable.
func TestStoreRepositoryRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)
	repo := newStoreRepository(st, "integration-secret")

	email := fmt.Sprintf("be-4.4-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	createdApp, err := st.CreateApplication(ctx, createApplicationParams(userID))
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(createdApp.ID.Bytes)

	// The lookup is by ID; team authorization happens in the service.
	if _, err := repo.GetApplication(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing application error = %v, want ErrNotFound", err)
	}
	app, err := repo.GetApplication(ctx, appID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if app.Repo != "Octo/Gotham" || app.Branch != "main" || app.TeamID != userID {
		t.Errorf("application = %+v, want the creator's personal team", app)
	}

	// Installing a hook seals its secret at rest and reads it back opened.
	created, err := repo.CreateWebhook(ctx, Hook{
		ApplicationID: appID,
		Provider:      "github",
		Repo:          app.Repo,
		HookID:        "4242",
		URL:           "https://cp.example/api/v1/webhooks/github",
	}, "hook-secret")
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if created.ID == uuid.Nil || created.HookID != "4242" {
		t.Errorf("created hook = %+v", created)
	}
	var storedSecret string
	if err := pool.QueryRow(ctx,
		"SELECT secret FROM application_webhooks WHERE application_id = $1", pgUUID(appID),
	).Scan(&storedSecret); err != nil {
		t.Fatalf("read raw secret: %v", err)
	}
	if storedSecret == "hook-secret" {
		t.Fatal("hook secret is stored in the clear")
	}

	if _, err := repo.CreateWebhook(ctx, Hook{
		ApplicationID: appID, Provider: "github", Repo: app.Repo, HookID: "4243",
	}, "other"); !errors.Is(err, ErrConflict) {
		t.Errorf("second hook error = %v, want ErrConflict", err)
	}

	got, err := repo.GetWebhook(ctx, appID)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.URL != "https://cp.example/api/v1/webhooks/github" {
		t.Errorf("hook = %+v", got)
	}

	// The delivery lookup matches the repository case-insensitively.
	targets, err := repo.Targets(ctx, "github", "octo/gotham")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Secret != "hook-secret" || targets[0].Branch != "main" {
		t.Fatalf("targets = %+v, want the one installed hook with an opened secret", targets)
	}
	if targets, err := repo.Targets(ctx, "gitlab", "octo/gotham"); err != nil || len(targets) != 0 {
		t.Errorf("targets for another provider = %v / %v, want none", targets, err)
	}

	// Dedupe: the same commit may be claimed once, a new commit again.
	event, err := repo.ClaimEvent(ctx, Event{
		ApplicationID: appID, Provider: "github", Event: "push",
		DeliveryID: "d1", Ref: "refs/heads/main", CommitSHA: "abc123",
	})
	if err != nil {
		t.Fatalf("ClaimEvent: %v", err)
	}
	if _, err := repo.ClaimEvent(ctx, Event{
		ApplicationID: appID, Provider: "github", Event: "push",
		DeliveryID: "d2", Ref: "refs/heads/main", CommitSHA: "abc123",
	}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("same commit error = %v, want ErrDuplicate", err)
	}
	if _, err := repo.ClaimEvent(ctx, Event{
		ApplicationID: appID, Provider: "github", Event: "push",
		DeliveryID: "d1", Ref: "refs/heads/main", CommitSHA: "def456",
	}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("same delivery id error = %v, want ErrDuplicate", err)
	}

	deploymentID := uuid.New()
	if err := repo.LinkEventDeployment(ctx, event.ID, deploymentID); err != nil {
		t.Fatalf("LinkEventDeployment: %v", err)
	}
	if err := repo.ReleaseEvent(ctx, event.ID); err != nil {
		t.Fatalf("ReleaseEvent: %v", err)
	}
	// Released: the same delivery may be claimed again, and a double release
	// is not an error.
	if _, err := repo.ClaimEvent(ctx, Event{
		ApplicationID: appID, Provider: "github", Event: "push",
		DeliveryID: "d1", Ref: "refs/heads/main", CommitSHA: "abc123",
	}); err != nil {
		t.Errorf("reclaim after release: %v", err)
	}
	if err := repo.ReleaseEvent(ctx, event.ID); err != nil {
		t.Errorf("second release: %v", err)
	}

	// Detaching the application removes its hook exactly once.
	if _, err := repo.DeleteWebhook(ctx, appID); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if _, err := repo.DeleteWebhook(ctx, appID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetWebhook(ctx, appID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetWebhook after delete error = %v, want ErrNotFound", err)
	}
}

// TestStoreRepositoryPreviewRoundtrip exercises the preview_deploys adapter
// against a real server: the unique (application, PR) pair, the deleted/
// reopened transition, the stale list, the cascade from both the base and the
// sibling application, and the enriched delivery targets.
func TestStoreRepositoryPreviewRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)
	repo := newStoreRepository(st, "integration-secret")

	email := fmt.Sprintf("be-8.1-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	base, err := st.CreateApplication(ctx, createApplicationParams(userID))
	if err != nil {
		t.Fatalf("CreateApplication(base): %v", err)
	}
	baseID := uuid.UUID(base.ID.Bytes)
	sibling, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), Name: "wh-app-pr-7", Provider: "github",
		Repo: base.Repo, CloneUrl: base.CloneUrl, Branch: "feat/x", BuildPack: "auto",
		BaseDomain: "pr-7-wh-app.example.com",
	})
	if err != nil {
		t.Fatalf("CreateApplication(sibling): %v", err)
	}
	siblingID := uuid.UUID(sibling.ID.Bytes)

	created, err := repo.UpsertPreview(ctx, Preview{
		ApplicationID: baseID, TeamID: userID, Provider: "github", Repo: base.Repo,
		PRNumber: 7, Branch: "feat/x", HeadSHA: "abc123",
		PreviewApplicationID: siblingID, Host: "pr-7-wh-app.example.com", State: PreviewActive,
	})
	if err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}
	if created.ID == uuid.Nil || created.HeadSHA != "abc123" || created.Host != "pr-7-wh-app.example.com" {
		t.Fatalf("created preview = %+v", created)
	}
	if created.CreatedAt.IsZero() || !created.DeletedAt.IsZero() {
		t.Errorf("created timestamps = %+v, want created_at set and deleted_at unset", created)
	}

	got, err := repo.GetPreview(ctx, baseID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if got.ID != created.ID || got.PreviewApplicationID != siblingID || got.PRNumber != 7 {
		t.Errorf("preview = %+v, want the stored row", got)
	}
	if _, err := repo.GetPreview(ctx, baseID, 8); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing preview error = %v, want ErrNotFound", err)
	}

	// One preview of the application; a bound preview is never an orphan.
	list, err := repo.ListPreviews(ctx, baseID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPreviews = %d / %v, want one preview", len(list), err)
	}
	if got, err := repo.CountLivePreviews(ctx, baseID); err != nil || got != 1 {
		t.Fatalf("CountLivePreviews = %d / %v, want 1", got, err)
	}
	if orphans, err := repo.ListOrphanedPreviews(ctx); err != nil || len(orphans) != 0 {
		t.Fatalf("ListOrphanedPreviews = %d / %v, want none", len(orphans), err)
	}
	if apps, err := repo.ListOrphanedPreviewApplications(ctx, time.Now().UTC().Add(time.Hour)); err != nil || len(apps) != 0 {
		t.Fatalf("ListOrphanedPreviewApplications = %d / %v, want none", len(apps), err)
	}

	// The reservation ledger: a replayed start is a duplicate, a new head is
	// allowed, a close reserves once, and clearing frees the reopen path.
	start := DeliveryReservation{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationStart, HeadSHA: "abc123", DeliveryID: "d1",
	}
	reserved, err := repo.ReservePreviewDelivery(ctx, start)
	if err != nil {
		t.Fatalf("ReservePreviewDelivery: %v", err)
	}
	if reserved.ID == uuid.Nil || reserved.DeliveryID != "d1" {
		t.Errorf("reservation = %+v", reserved)
	}
	if _, err := repo.ReservePreviewDelivery(ctx, start); !errors.Is(err, ErrDuplicate) {
		t.Errorf("replayed reservation error = %v, want ErrDuplicate", err)
	}
	// A different PR at the same head is independent (F2).
	if _, err := repo.ReservePreviewDelivery(ctx, DeliveryReservation{
		ApplicationID: baseID, PRNumber: 8, Kind: ReservationStart, HeadSHA: "abc123",
	}); err != nil {
		t.Errorf("second PR at the same head: %v, want a reservation", err)
	}
	// A new head revision of the same PR is independent.
	if _, err := repo.ReservePreviewDelivery(ctx, DeliveryReservation{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationStart, HeadSHA: "def456",
	}); err != nil {
		t.Errorf("new head reservation: %v, want one", err)
	}
	closeReservation, err := repo.ReservePreviewDelivery(ctx, DeliveryReservation{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationClose,
	})
	if err != nil {
		t.Fatalf("close reservation: %v", err)
	}
	if _, err := repo.ReservePreviewDelivery(ctx, DeliveryReservation{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationClose,
	}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("second close reservation error = %v, want ErrDuplicate", err)
	}
	if err := repo.ReleasePreviewDelivery(ctx, reserved.ID); err != nil {
		t.Fatalf("ReleasePreviewDelivery: %v", err)
	}
	if err := repo.ClearPreviewDeliveries(ctx, baseID, 7); err != nil {
		t.Fatalf("ClearPreviewDeliveries: %v", err)
	}
	if reserved.ID == closeReservation.ID {
		t.Error("distinct reservations share one id")
	}
	// After the clear the same head can reserve again (reopen, F2).
	if _, err := repo.ReservePreviewDelivery(ctx, start); err != nil {
		t.Errorf("reservation after clear: %v, want a fresh reservation", err)
	}
	if err := repo.ClearPreviewDeliveries(ctx, baseID, 7); err != nil {
		t.Fatalf("ClearPreviewDeliveries(second): %v", err)
	}

	// Closing marks the row deleted; a re-opened PR refreshes the same row
	// (the unique pair) instead of inserting a second one.
	deleted, err := repo.MarkPreviewDeleted(ctx, created.ID)
	if err != nil {
		t.Fatalf("MarkPreviewDeleted: %v", err)
	}
	if deleted.State != PreviewDeleted || deleted.DeletedAt.IsZero() {
		t.Errorf("deleted preview = %+v", deleted)
	}
	reopened, err := repo.UpsertPreview(ctx, Preview{
		ApplicationID: baseID, TeamID: userID, Provider: "github", Repo: base.Repo,
		PRNumber: 7, Branch: "feat/y", HeadSHA: "def456",
		PreviewApplicationID: siblingID, Host: "pr-7-wh-app.example.com", State: PreviewActive,
	})
	if err != nil {
		t.Fatalf("UpsertPreview(reopen): %v", err)
	}
	if reopened.ID != created.ID || reopened.Branch != "feat/y" || !reopened.DeletedAt.IsZero() {
		t.Errorf("reopened preview = %+v, want the same row refreshed", reopened)
	}

	// Deleting the sibling clears the link but keeps the binding row: the
	// FK is ON DELETE SET NULL so the audit trail survives the teardown.
	if err := st.DeleteApplication(ctx, pgUUID(siblingID)); err != nil {
		t.Fatalf("DeleteApplication(sibling): %v", err)
	}
	survivor, err := repo.GetPreview(ctx, baseID, 7)
	if err != nil {
		t.Fatalf("preview after sibling delete: %v", err)
	}
	if survivor.PreviewApplicationID != uuid.Nil {
		t.Errorf("preview still links the deleted sibling: %+v", survivor)
	}
	// The binding is now orphaned: the sweep's binding pass finds it, and its
	// application pass ignores it (it is a binding, not an application).
	orphans, err := repo.ListOrphanedPreviews(ctx)
	if err != nil || len(orphans) != 1 || orphans[0].ID != survivor.ID {
		t.Fatalf("ListOrphanedPreviews = %+v / %v, want the orphaned binding", orphans, err)
	}
	if _, err := repo.MarkPreviewDeleted(ctx, survivor.ID); err != nil {
		t.Fatalf("MarkPreviewDeleted: %v", err)
	}
	if orphans, err := repo.ListOrphanedPreviews(ctx); err != nil || len(orphans) != 0 {
		t.Fatalf("ListOrphanedPreviews after mark = %d / %v, want none", len(orphans), err)
	}

	// An is_preview application without a live binding is the sweep's second
	// work list; marking its binding deleted puts it back in scope even when
	// the application row itself survived.
	previewApp, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), Name: "wh-app-pr-11", Provider: "github",
		Repo: base.Repo, CloneUrl: base.CloneUrl, Branch: "feat/w", BuildPack: "auto",
		BaseDomain: "pr-11-wh-app.example.com", IsPreview: true,
	})
	if err != nil {
		t.Fatalf("CreateApplication(preview sibling): %v", err)
	}
	previewAppID := uuid.UUID(previewApp.ID.Bytes)
	listed := func() bool {
		apps, err := repo.ListOrphanedPreviewApplications(ctx, time.Now().UTC().Add(time.Hour))
		if err != nil {
			t.Fatalf("ListOrphanedPreviewApplications: %v", err)
		}
		for _, id := range apps {
			if id == previewAppID {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("the unbound is_preview application is not in the sweep's work list")
	}
	preview11, err := repo.UpsertPreview(ctx, Preview{
		ApplicationID: baseID, TeamID: userID, Provider: "github", Repo: base.Repo,
		PRNumber: 11, Host: "pr-11-wh-app.example.com", State: PreviewActive,
		PreviewApplicationID: previewAppID,
	})
	if err != nil {
		t.Fatalf("UpsertPreview(PR 11): %v", err)
	}
	if listed() {
		t.Fatal("a live-bound preview application is in the sweep's work list")
	}
	if err := repo.MarkPreviewsDeletedForSibling(ctx, previewAppID); err != nil {
		t.Fatalf("MarkPreviewsDeletedForSibling: %v", err)
	}
	if got, err := repo.GetPreview(ctx, baseID, 11); err != nil || got.State != PreviewDeleted {
		t.Fatalf("binding after MarkPreviewsDeletedForSibling = %+v / %v", got, err)
	}
	if !listed() {
		t.Fatal("a preview application whose binding was marked deleted is not in the sweep's work list")
	}
	if preview11.ID == uuid.Nil {
		t.Error("preview 11 lost its id")
	}

	// Deleting the base application cascades its previews too.
	if _, err := repo.UpsertPreview(ctx, Preview{
		ApplicationID: baseID, TeamID: userID, Provider: "github", Repo: base.Repo,
		PRNumber: 9, Branch: "feat/z", HeadSHA: "fff",
		Host: "pr-9-wh-app.example.com", State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview(second PR): %v", err)
	}
	if err := st.DeleteApplication(ctx, pgUUID(baseID)); err != nil {
		t.Fatalf("DeleteApplication(base): %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM preview_deploys WHERE application_id = $1", pgUUID(baseID)).Scan(&remaining); err != nil {
		t.Fatalf("count previews: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d previews survived the base delete, want 0", remaining)
	}
}

// TestStoreRepositoryTargetsCarryPreviewFields pins the enriched delivery
// target row the preview logic derives the sibling from.
func TestStoreRepositoryTargetsCarryPreviewFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)
	repo := newStoreRepository(st, "integration-secret")

	email := fmt.Sprintf("be-8.1-targets-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	params := createApplicationParams(userID)
	params.BaseDomain = "wh-app.example.com"
	createdApp, err := st.CreateApplication(ctx, params)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(createdApp.ID.Bytes)
	if _, err := repo.CreateWebhook(ctx, Hook{
		ApplicationID: appID, Provider: "github", Repo: createdApp.Repo, HookID: "1",
		URL: "https://cp.example/api/v1/webhooks/github",
	}, "s"); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	targets, err := repo.Targets(ctx, "github", "octo/gotham")
	if err != nil || len(targets) != 1 {
		t.Fatalf("Targets = %d / %v, want one", len(targets), err)
	}
	target := targets[0]
	if target.Name != "wh-app" || target.BaseDomain != "wh-app.example.com" ||
		target.UserID != userID || target.TeamID != userID {
		t.Errorf("target = %+v, want name/domain/user/team from the application", target)
	}
}

// createApplicationParams builds the row the webhook tests watch. The ID comes
// from the database default, so callers read it back from the returned row.
func createApplicationParams(userID uuid.UUID) sqlc.CreateApplicationParams {
	return sqlc.CreateApplicationParams{
		UserID:    pgUUID(userID),
		Name:      "wh-app",
		Provider:  "github",
		Repo:      "Octo/Gotham",
		CloneUrl:  "https://github.com/Octo/Gotham.git",
		Branch:    "main",
		BuildPack: "auto",
	}
}
