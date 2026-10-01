package webhooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	if orphans, err := repo.ListOrphanedPreviews(ctx); err != nil || len(orphans) != 0 {
		t.Fatalf("ListOrphanedPreviews = %d / %v, want none", len(orphans), err)
	}
	if apps, err := repo.ListOrphanedPreviewApplications(ctx, time.Now().UTC().Add(time.Hour)); err != nil || len(apps) != 0 {
		t.Fatalf("ListOrphanedPreviewApplications = %d / %v, want none", len(apps), err)
	}

	// The claim: the live binding's head dedupes a start, an in-flight lease
	// dedupes a replay, a different head is a new transition, and a historical
	// head is NOT a permanent handled set (N1) — the claim approves it again.
	claimStart := func(head string) PreviewClaimResult {
		t.Helper()
		result, err := repo.ClaimPreviewDelivery(ctx, PreviewClaim{
			ApplicationID: baseID, PRNumber: 7, Kind: ReservationStart,
			HeadSHA: head, LiveLimit: 5,
		})
		if err != nil {
			t.Fatalf("ClaimPreviewDelivery(%s): %v", head, err)
		}
		return result
	}
	// The binding currently names abc123: delivering it again is a duplicate.
	if dup := claimStart("abc123"); !dup.Duplicate {
		t.Fatalf("current-head claim = %+v, want duplicate", dup)
	}
	// A new head is approved and holds an in-flight lease.
	approved := claimStart("sha-b")
	if !approved.Approved || approved.Reservation.ID == uuid.Nil || approved.Binding == nil {
		t.Fatalf("new-head claim = %+v, want approved with a lease and the binding", approved)
	}
	// The same head while its lease is live is a duplicate.
	if dup := claimStart("sha-b"); !dup.Duplicate {
		t.Fatalf("in-flight claim = %+v, want duplicate", dup)
	}
	// A different PR at the same head is independent (F2).
	if other, err := repo.ClaimPreviewDelivery(ctx, PreviewClaim{
		ApplicationID: baseID, PRNumber: 8, Kind: ReservationStart, HeadSHA: "sha-b", LiveLimit: 5,
	}); err != nil || !other.Approved {
		t.Fatalf("second PR at the same head = %+v / %v, want approved", other, err)
	}
	// The queue succeeded: the binding names the new head and the lease goes.
	if _, err := repo.UpsertPreview(ctx, Preview{
		ApplicationID: baseID, TeamID: userID, Provider: "github", Repo: base.Repo,
		PRNumber: 7, Branch: "feat/x", HeadSHA: "sha-b",
		PreviewApplicationID: siblingID, Host: "pr-7-wh-app.example.com", State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview(sha-b): %v", err)
	}
	if err := repo.ReleasePreviewDelivery(ctx, approved.Reservation.ID); err != nil {
		t.Fatalf("ReleasePreviewDelivery: %v", err)
	}
	if dup := claimStart("sha-b"); !dup.Duplicate {
		t.Fatalf("current-head claim after the queue = %+v, want duplicate", dup)
	}
	// N1: a force-push back to the historical head must deploy again.
	back := claimStart("abc123")
	if !back.Approved {
		t.Fatalf("historical-head claim = %+v, want approved (not a handled set)", back)
	}
	// A close marker reserves once.
	closeResult, err := repo.ClaimPreviewDelivery(ctx, PreviewClaim{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationClose,
	})
	if err != nil || !closeResult.Approved {
		t.Fatalf("close claim = %+v / %v, want approved", closeResult, err)
	}
	if dup, err := repo.ClaimPreviewDelivery(ctx, PreviewClaim{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationClose,
	}); err != nil || !dup.Duplicate {
		t.Fatalf("second close claim = %+v / %v, want duplicate", dup, err)
	}
	// The atomic close completion deletes the binding and clears the ledger.
	closed, err := repo.MarkPreviewClosed(ctx, baseID, 7)
	if err != nil {
		t.Fatalf("MarkPreviewClosed: %v", err)
	}
	if closed.State != PreviewDeleted || closed.DeletedAt.IsZero() {
		t.Errorf("closed preview = %+v", closed)
	}
	// A stale reservation can never poison a reopen: the completion cleared
	// every row, and the same historical head is claimable again.
	if reopened := claimStart("abc123"); !reopened.Approved {
		t.Fatalf("reopen claim = %+v, want approved", reopened)
	}
	if err := repo.ReleasePreviewDelivery(ctx, uuid.MustParse("00000000-0000-0000-0000-000000000000")); err != nil {
		t.Errorf("releasing a missing reservation: %v, want nil", err)
	}
	// Expired leases are purged by the claim (and by the sweep's global pass),
	// so a crashed delivery cannot suppress its revision forever.
	expired, err := repo.ClaimPreviewDelivery(ctx, PreviewClaim{
		ApplicationID: baseID, PRNumber: 7, Kind: ReservationStart, HeadSHA: "sha-expired",
	})
	if err != nil || !expired.Approved {
		t.Fatalf("expired-lease setup = %+v / %v", expired, err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE preview_deliveries SET expires_at = now() - interval '1 minute' WHERE id = $1",
		pgUUID(expired.Reservation.ID),
	); err != nil {
		t.Fatalf("expire the lease: %v", err)
	}
	if purged, err := repo.PurgeExpiredPreviewReservations(ctx); err != nil || purged != 1 {
		t.Fatalf("PurgeExpiredPreviewReservations = %d / %v, want 1", purged, err)
	}
	if again := claimStart("sha-expired"); !again.Approved {
		t.Fatalf("claim after the purge = %+v, want approved", again)
	}
	if err := repo.ClearPreviewDeliveries(ctx, baseID, 7); err != nil {
		t.Fatalf("ClearPreviewDeliveries: %v", err)
	}
	if err := repo.ClearPreviewDeliveries(ctx, baseID, 8); err != nil {
		t.Fatalf("ClearPreviewDeliveries(PR 8): %v", err)
	}

	// A re-opened PR refreshes the same binding row (the unique pair) instead
	// of inserting a second one.
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
	if _, err := repo.MarkPreviewClosed(ctx, survivor.ApplicationID, survivor.PRNumber); err != nil {
		t.Fatalf("MarkPreviewClosed: %v", err)
	}
	if orphans, err := repo.ListOrphanedPreviews(ctx); err != nil || len(orphans) != 0 {
		t.Fatalf("ListOrphanedPreviews after close = %d / %v, want none", len(orphans), err)
	}

	// A persisted close intent is the sweep's teardown-retry list. The sibling
	// link is NULL here: the listing only cares about the state and age.
	if _, err := repo.UpsertPreview(ctx, Preview{
		ApplicationID: baseID, TeamID: userID, Provider: "github", Repo: base.Repo,
		PRNumber: 12, Host: "pr-12-wh-app.example.com",
		State:     PreviewClosing,
		CreatedAt: time.Now().UTC().Add(-time.Hour), UpdatedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertPreview(closing): %v", err)
	}
	// The fake/test flow uses the binding row's updated_at; force it into the
	// past through the schema so the cutoff is deterministic.
	if _, err := pool.Exec(ctx, "UPDATE preview_deploys SET updated_at = now() - interval '1 hour' WHERE application_id = $1 AND pr_number = 12", pgUUID(baseID)); err != nil {
		t.Fatalf("age the closing binding: %v", err)
	}
	if closing, err := repo.ListClosingPreviews(ctx, time.Now().UTC().Add(-previewSweepGrace)); err != nil || len(closing) != 1 || closing[0].PRNumber != 12 {
		t.Fatalf("ListClosingPreviews = %+v / %v, want the closing binding", closing, err)
	}
	if closing, err := repo.ListClosingPreviews(ctx, time.Now().UTC().Add(-2*time.Hour)); err != nil || len(closing) != 0 {
		t.Fatalf("ListClosingPreviews(older cutoff) = %d / %v, want none", len(closing), err)
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

// claimFixture creates a user and a base application for claim-level tests.
func claimFixture(t *testing.T, ctx context.Context, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	email := fmt.Sprintf("be-8.1-claim-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})
	base, err := st.CreateApplication(ctx, createApplicationParams(userID))
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return userID, uuid.UUID(base.ID.Bytes)
}

// openClaimFixture opens the integration database and returns the store.
func openClaimFixture(t *testing.T, ctx context.Context) *store.Store {
	t.Helper()
	dsn := integrationDSN()
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

// TestStoreClosingSameHeadClaimRetries is the N4 database-level proof: a claim
// at a closing binding's own head answers retryable, and the fenced write
// refuses to touch it.
func TestStoreClosingSameHeadClaimRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	if _, err := st.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID: pgUUID(baseID), TeamID: pgUUID(userID), Provider: "github",
		Repo: "octo/gotham", PrNumber: 7, Branch: "feat/x", HeadSha: "head-a",
		Host: "pr-7.example.com", State: "closing",
	}); err != nil {
		t.Fatalf("UpsertPreviewDeploy: %v", err)
	}
	result, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-a", LiveLimit: 5,
	})
	if err != nil {
		t.Fatalf("ClaimPreviewDelivery: %v", err)
	}
	if result.Duplicate || !result.Retryable || result.Approved {
		t.Fatalf("closing same-head claim = %+v, want retryable (not duplicate)", result)
	}

	// The fenced write refuses a closing binding even with a valid lease.
	lease, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-a", LiveLimit: 5, // still closing: retryable again
	})
	if err != nil || !lease.Retryable {
		t.Fatalf("second closing claim = %+v / %v, want retryable", lease, err)
	}
	if _, err := st.DB.Exec(ctx, "UPDATE preview_deploys SET state = 'active' WHERE application_id = $1 AND pr_number = 7", pgUUID(baseID)); err != nil {
		t.Fatalf("unsettle closing: %v", err)
	}
	approved, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-b", LiveLimit: 5,
	})
	if err != nil || !approved.Approved {
		t.Fatalf("claim after closing = %+v / %v, want approved", approved, err)
	}
	if _, err := st.DB.Exec(ctx, "UPDATE preview_deploys SET state = 'closing' WHERE application_id = $1 AND pr_number = 7", pgUUID(baseID)); err != nil {
		t.Fatalf("re-close: %v", err)
	}
	write, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, ReservationID: approved.Reservation.ID,
		LeaseHeadSHA: "head-b", HeadSHA: "head-b", State: "active",
	})
	if err != nil {
		t.Fatalf("WritePreviewBinding: %v", err)
	}
	if write.Refused != store.PreviewWriteClosingRefused {
		t.Fatalf("closing write = %+v, want refused closing", write)
	}
}

// TestStoreExpiredWorkerCannotPromote is the N5 database-level proof: a worker
// whose lease lapsed cannot promote a binding once five other previews are
// live; the fenced write refuses it.
func TestStoreExpiredWorkerCannotPromote(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	slow, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 1, Kind: store.PreviewClaimStart,
		HeadSHA: "sha-1", LiveLimit: 5,
	})
	if err != nil || !slow.Approved {
		t.Fatalf("slow claim = %+v / %v, want approved", slow, err)
	}
	if _, err := st.DB.Exec(ctx,
		"UPDATE preview_deliveries SET expires_at = now() - interval '1 minute' WHERE id = $1",
		slow.Reservation.ID,
	); err != nil {
		t.Fatalf("expire the slow lease: %v", err)
	}

	// Five other previews take every slot.
	for pr := 2; pr <= 6; pr++ {
		claim, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
			ApplicationID: pgUUID(baseID), PrNumber: int32(pr), Kind: store.PreviewClaimStart,
			HeadSHA: fmt.Sprintf("sha-%d", pr), LiveLimit: 5,
		})
		if err != nil || !claim.Approved {
			t.Fatalf("claim PR %d = %+v / %v, want approved", pr, claim, err)
		}
		write, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
			ApplicationID: pgUUID(baseID), PrNumber: int32(pr), ReservationID: claim.Reservation.ID,
			LeaseHeadSHA: fmt.Sprintf("sha-%d", pr), HeadSHA: fmt.Sprintf("sha-%d", pr),
			TeamID: pgUUID(userID), State: "active", ConsumeLease: true,
		})
		if err != nil || write.Refused != "" {
			t.Fatalf("bind PR %d = %+v / %v, want written", pr, write, err)
		}
	}

	// The slow worker resumes: its promotion is refused and no sixth binding
	// appears.
	write, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
		ApplicationID: pgUUID(baseID), PrNumber: 1, ReservationID: slow.Reservation.ID,
		LeaseHeadSHA: "sha-1", HeadSHA: "sha-1", State: "active", ConsumeLease: true,
	})
	if err != nil {
		t.Fatalf("WritePreviewBinding(slow): %v", err)
	}
	if write.Refused != store.PreviewWriteLeaseRefused {
		t.Fatalf("slow write = %+v, want refused lease", write)
	}
	var live int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM preview_deploys WHERE application_id = $1 AND state <> 'deleted'",
		pgUUID(baseID),
	).Scan(&live); err != nil {
		t.Fatalf("count live: %v", err)
	}
	if live != 5 {
		t.Fatalf("live previews = %d, want 5 (the expired worker must not promote)", live)
	}
}

// TestStoreClosedBindingCannotBeResurrected is the R-1 database-level proof: a
// completed close clears the ledger, so a racing worker's promotion is refused
// and the binding stays deleted.
func TestStoreClosedBindingCannotBeResurrected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	claim, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-b", LiveLimit: 5,
	})
	if err != nil || !claim.Approved {
		t.Fatalf("claim = %+v / %v, want approved", claim, err)
	}
	if _, err := st.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID: pgUUID(baseID), TeamID: pgUUID(userID), Provider: "github",
		Repo: "octo/gotham", PrNumber: 7, Branch: "feat/x", HeadSha: "head-a",
		Host: "pr-7.example.com", State: "active",
	}); err != nil {
		t.Fatalf("UpsertPreviewDeploy: %v", err)
	}
	// The close completes while the worker is in flight.
	if _, err := st.MarkPreviewClosed(ctx, pgUUID(baseID), 7); err != nil {
		t.Fatalf("MarkPreviewClosed: %v", err)
	}

	write, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, ReservationID: claim.Reservation.ID,
		LeaseHeadSHA: "head-b", HeadSHA: "head-b", State: "active", ConsumeLease: true,
	})
	if err != nil {
		t.Fatalf("WritePreviewBinding: %v", err)
	}
	if write.Refused != store.PreviewWriteLeaseRefused {
		t.Fatalf("racing write = %+v, want refused lease", write)
	}
	binding, err := st.GetPreviewDeploy(ctx, pgUUID(baseID), 7)
	if err != nil {
		t.Fatalf("GetPreviewDeploy: %v", err)
	}
	if binding.State != "deleted" {
		t.Fatalf("binding state = %q, want deleted (never resurrected)", binding.State)
	}
}

// TestStoreCloseSerializesWithPromotion is the real-concurrent R-1 regression:
// a close that starts while a promotion is inside its write waits for the
// base-application lock, so the promotion cannot be overwritten after the
// fence's reads; the close then wins and the binding stays deleted. A BEFORE
// INSERT trigger on the test's application pauses the promotion inside the
// write while holding that lock.
func TestStoreCloseSerializesWithPromotion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	// Seed the existing binding (head-a) so the close has a row to update.
	if _, err := st.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID: pgUUID(baseID), TeamID: pgUUID(userID), Provider: "github",
		Repo: "octo/gotham", PrNumber: 7, Branch: "feat/x", HeadSha: "head-a",
		Host: "pr-7.example.com", State: "active",
	}); err != nil {
		t.Fatalf("UpsertPreviewDeploy: %v", err)
	}
	claim, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-b", LiveLimit: 5,
	})
	if err != nil || !claim.Approved {
		t.Fatalf("claim = %+v / %v, want approved", claim, err)
	}

	// Pause the promotion inside its upsert, after the app lock is held.
	appLiteral := baseID.String()
	if _, err := st.DB.Exec(ctx, `
		DROP TRIGGER IF EXISTS be81_fence_gate_trg ON preview_deploys;
		CREATE OR REPLACE FUNCTION be81_fence_gate_fn() RETURNS trigger AS $$
		BEGIN
			IF TG_OP = 'INSERT' THEN
				PERFORM pg_sleep(0.8);
			END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER be81_fence_gate_trg BEFORE INSERT OR UPDATE ON preview_deploys
			FOR EACH ROW WHEN (NEW.application_id = '`+appLiteral+`'::uuid AND NEW.state <> 'deleted')
			EXECUTE FUNCTION be81_fence_gate_fn();`); err != nil {
		t.Fatalf("install the fence gate: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, `
			DROP TRIGGER IF EXISTS be81_fence_gate_trg ON preview_deploys;
			DROP FUNCTION IF EXISTS be81_fence_gate_fn();`); err != nil {
			t.Logf("cleanup fence gate: %v", err)
		}
	})

	promoted := make(chan store.PreviewBindingWriteResult, 1)
	promoteErr := make(chan error, 1)
	go func() {
		result, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
			ApplicationID: pgUUID(baseID), PrNumber: 7, ReservationID: claim.Reservation.ID,
			LeaseHeadSHA: "head-b", HeadSHA: "head-b", TeamID: pgUUID(userID),
			State: "active", ConsumeLease: true,
		})
		promoted <- result
		promoteErr <- err
	}()

	// Wait until the promotion is inside the gate (sleeping in the trigger
	// while holding the app lock).
	deadline := time.Now().Add(10 * time.Second)
	for {
		var entered int
		if err := st.DB.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event = 'PgSleep' AND query LIKE '%preview_deploys%'`).Scan(&entered); err != nil {
			t.Fatalf("poll the fence gate: %v", err)
		}
		if entered > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the promotion never entered the fence gate")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The close starts while the promotion holds the lock: it must wait, not
	// complete underneath it.
	closed := make(chan error, 1)
	go func() {
		_, err := st.MarkPreviewClosed(context.Background(), pgUUID(baseID), 7)
		closed <- err
	}()
	select {
	case err := <-closed:
		t.Fatalf("close completed during the promotion (%v): it must serialize on the application lock", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := <-promoteErr; err != nil {
		t.Fatalf("promotion: %v", err)
	}
	if result := <-promoted; result.Refused != "" || result.Binding.State != "active" {
		t.Fatalf("promotion = %+v, want written active", result)
	}
	// The close now wins: its completion must land after (and over) the
	// promotion, leaving the binding deleted.
	if err := <-closed; err != nil {
		t.Fatalf("close: %v", err)
	}
	binding, err := st.GetPreviewDeploy(ctx, pgUUID(baseID), 7)
	if err != nil {
		t.Fatalf("GetPreviewDeploy: %v", err)
	}
	if binding.State != "deleted" {
		t.Fatalf("binding state = %q, want deleted (the close is never overwritten)", binding.State)
	}
	var leases int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM preview_deliveries WHERE application_id = $1 AND pr_number = 7",
		pgUUID(baseID),
	).Scan(&leases); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if leases != 0 {
		t.Fatalf("leases = %d, want the close completion to clear them", leases)
	}
}

// TestStoreLeaseExpiryDuringLockWait is the N6 regression: a lease that
// expires while the writer waits for the base-application lock must be
// refused, because lease validity is evaluated with the actual current time
// after the lock is granted, not the transaction-start time.
func TestStoreLeaseExpiryDuringLockWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	claim, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-b", LiveLimit: 5,
	})
	if err != nil || !claim.Approved {
		t.Fatalf("claim = %+v / %v, want approved", claim, err)
	}
	if _, err := st.DB.Exec(ctx,
		"UPDATE preview_deliveries SET expires_at = clock_timestamp() + interval '700 milliseconds' WHERE id = $1",
		claim.Reservation.ID,
	); err != nil {
		t.Fatalf("shorten the lease: %v", err)
	}

	// Hold the application lock on a separate connection so the writer waits.
	holder, err := st.DB.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock holder: %v", err)
	}
	defer holder.Release()
	holdTx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	defer func() { _ = holdTx.Rollback(ctx) }()
	var locked pgtype.UUID
	if err := holdTx.QueryRow(ctx, "SELECT id FROM applications WHERE id = $1 FOR UPDATE", pgUUID(baseID)).Scan(&locked); err != nil {
		t.Fatalf("hold the application lock: %v", err)
	}

	written := make(chan store.PreviewBindingWriteResult, 1)
	writeErr := make(chan error, 1)
	go func() {
		result, err := st.WritePreviewBinding(context.Background(), store.PreviewBindingWriteParams{
			ApplicationID: pgUUID(baseID), PrNumber: 7, ReservationID: claim.Reservation.ID,
			LeaseHeadSHA: "head-b", HeadSHA: "head-b", TeamID: pgUUID(userID),
			State: "active", ConsumeLease: true,
		})
		written <- result
		writeErr <- err
	}()
	// Let the lease lapse while the writer waits for the lock, then release it.
	time.Sleep(1200 * time.Millisecond)
	if err := holdTx.Commit(ctx); err != nil {
		t.Fatalf("release the application lock: %v", err)
	}

	if err := <-writeErr; err != nil {
		t.Fatalf("WritePreviewBinding: %v", err)
	}
	result := <-written
	if result.Refused != store.PreviewWriteLeaseRefused {
		t.Fatalf("expired-during-wait write = %+v, want refused lease", result)
	}
	if _, err := st.GetPreviewDeploy(ctx, pgUUID(baseID), 7); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("binding after the refused write = %+v / %v, want none", err, err)
	}
}

// TestStoreSiblingDeleteClearsLedger is the LOW regression: deleting a
// preview sibling closes its binding and clears the pull request's ledger, so
// no lease survives to authorize a stale promotion.
func TestStoreSiblingDeleteClearsLedger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	sibling, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), Name: "octo-gotham-pr-7", Provider: "github",
		Repo: "octo/gotham", CloneUrl: "https://github.com/octo/gotham.git",
		Branch: "feat/x", BuildPack: "auto", BaseDomain: "pr-7.example.com", IsPreview: true,
	})
	if err != nil {
		t.Fatalf("CreateApplication(sibling): %v", err)
	}
	siblingID := uuid.UUID(sibling.ID.Bytes)
	if _, err := st.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID: pgUUID(baseID), TeamID: pgUUID(userID), Provider: "github",
		Repo: "octo/gotham", PrNumber: 7, Branch: "feat/x", HeadSha: "head-a",
		Host: "pr-7.example.com", State: "active", PreviewApplicationID: pgUUID(siblingID),
	}); err != nil {
		t.Fatalf("UpsertPreviewDeploy: %v", err)
	}
	// A live lease a stale worker could still use to promote.
	claim, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: 7, Kind: store.PreviewClaimStart,
		HeadSHA: "head-b", LiveLimit: 5,
	})
	if err != nil || !claim.Approved {
		t.Fatalf("claim = %+v / %v, want approved", claim, err)
	}

	if err := st.MarkPreviewDeploysDeletedForSibling(ctx, pgUUID(siblingID)); err != nil {
		t.Fatalf("MarkPreviewDeploysDeletedForSibling: %v", err)
	}
	binding, err := st.GetPreviewDeploy(ctx, pgUUID(baseID), 7)
	if err != nil {
		t.Fatalf("GetPreviewDeploy: %v", err)
	}
	if binding.State != "deleted" {
		t.Fatalf("binding state = %q, want deleted", binding.State)
	}
	var leases int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM preview_deliveries WHERE application_id = $1 AND pr_number = 7",
		pgUUID(baseID),
	).Scan(&leases); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if leases != 0 {
		t.Fatalf("leases = %d, want the sibling delete to clear the ledger", leases)
	}
}

// TestStoreClaimConcurrencyRespectsTheCap is the N3 database-level proof:
// concurrent claims for distinct pull requests of one application never
// approve more than the cap, because every claim locks the base application
// row and counts the live bindings plus in-flight leases in the same
// transaction.
func TestStoreClaimConcurrencyRespectsTheCap(t *testing.T) {
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
	email := fmt.Sprintf("be-8.1-cap-%d@example.com", time.Now().UnixNano())
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
		t.Fatalf("CreateApplication: %v", err)
	}
	baseID := uuid.UUID(base.ID.Bytes)

	const (
		cap      = 5
		attempts = 12
	)
	approved := make(chan int32, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
				ApplicationID: pgUUID(baseID),
				PrNumber:      int32(100 + i),
				Kind:          store.PreviewClaimStart,
				HeadSHA:       fmt.Sprintf("sha-%d", i),
				DeliveryID:    fmt.Sprintf("delivery-%d", i),
				LiveLimit:     cap,
			})
			if err != nil {
				t.Errorf("ClaimPreviewDelivery(%d): %v", i, err)
				return
			}
			if result.Approved {
				approved <- result.Reservation.PrNumber
			}
		}(i)
	}
	wg.Wait()
	close(approved)

	got := make([]int32, 0, attempts)
	for pr := range approved {
		got = append(got, pr)
	}
	if len(got) != cap {
		t.Fatalf("approved = %d, want exactly the cap %d (prs %v)", len(got), cap, got)
	}

	// The database agrees: distinct live-or-in-flight pull requests == cap.
	var live int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT pr_number FROM preview_deploys
			WHERE application_id = $1 AND state <> 'deleted'
			UNION
			SELECT pr_number FROM preview_deliveries
			WHERE application_id = $1 AND kind = 'start' AND expires_at > now()
		) AS live`, pgUUID(baseID)).Scan(&live); err != nil {
		t.Fatalf("count live previews: %v", err)
	}
	if live != cap {
		t.Fatalf("live previews = %d, want %d", live, cap)
	}

	// Complete one of the approved previews (binding written, lease released):
	// a refresh of it is allowed even at the cap, because the claim skips the
	// quota for an existing live binding.
	if _, err := st.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID: pgUUID(baseID), TeamID: pgUUID(userID), Provider: "github",
		Repo: base.Repo, PrNumber: got[0], Branch: "main", HeadSha: "sha-orig",
		Host: "pr-x.example.com", State: "active",
	}); err != nil {
		t.Fatalf("UpsertPreviewDeploy: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"DELETE FROM preview_deliveries WHERE application_id = $1 AND pr_number = $2",
		pgUUID(baseID), got[0],
	); err != nil {
		t.Fatalf("release the completed lease: %v", err)
	}
	existing, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: got[0], Kind: store.PreviewClaimStart,
		HeadSHA: "refresh-sha", DeliveryID: "refresh", LiveLimit: cap,
	})
	if err != nil || !existing.Approved {
		t.Fatalf("refresh claim = %+v / %v, want approved at the cap", existing, err)
	}
}

// TestStoreNoBindingCloseRefusesTheRacingPromote is the F-1 database-level
// interleave: claim (start) → claim (close, no binding yet) → bind. The close
// marker must refuse the promotion, no live binding may appear behind it, and
// once the close clears the ledger the same revision must reopen.
func TestStoreNoBindingCloseRefusesTheRacingPromote(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	const pr = 7
	open, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: pr, Kind: store.PreviewClaimStart,
		HeadSHA: "head-a", LiveLimit: 5,
	})
	if err != nil || !open.Approved {
		t.Fatalf("open claim = %+v / %v, want approved", open, err)
	}
	closeClaim, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: pr, Kind: store.PreviewClaimClose,
		LiveLimit: 5,
	})
	if err != nil || !closeClaim.Approved {
		t.Fatalf("close claim = %+v / %v, want approved", closeClaim, err)
	}

	// The in-flight open's promotion is refused: the close owns the PR.
	write, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
		ApplicationID: pgUUID(baseID), PrNumber: pr, ReservationID: open.Reservation.ID,
		LeaseHeadSHA: "head-a", HeadSHA: "head-a", State: "active",
		ConsumeLease: true, LiveLimit: 5,
	})
	if err != nil {
		t.Fatalf("WritePreviewBinding: %v", err)
	}
	if write.Refused != store.PreviewWriteClosingRefused {
		t.Fatalf("racing promotion = %+v, want refused closing", write)
	}
	var bindings int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM preview_deploys WHERE application_id = $1 AND pr_number = $2",
		pgUUID(baseID), pr,
	).Scan(&bindings); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindings != 0 {
		t.Fatalf("bindings = %d, want none behind the close", bindings)
	}
	// A start arriving while the close is live answers retryable.
	again, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: pr, Kind: store.PreviewClaimStart,
		HeadSHA: "head-a", LiveLimit: 5,
	})
	if err != nil || !again.Retryable {
		t.Fatalf("start during the close = %+v / %v, want retryable", again, err)
	}

	// The no-binding close completes (its ledger clear); the same delivery
	// reopens and promotes.
	if err := st.ClearPreviewDeliveries(ctx, pgUUID(baseID), pr); err != nil {
		t.Fatalf("ClearPreviewDeliveries: %v", err)
	}
	reopen, err := st.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(baseID), PrNumber: pr, Kind: store.PreviewClaimStart,
		HeadSHA: "head-a", LiveLimit: 5,
	})
	if err != nil || !reopen.Approved {
		t.Fatalf("reopen claim = %+v / %v, want approved", reopen, err)
	}
	written, err := st.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
		ApplicationID: pgUUID(baseID), PrNumber: pr, ReservationID: reopen.Reservation.ID,
		LeaseHeadSHA: "head-a", HeadSHA: "head-a", TeamID: pgUUID(userID), State: "active",
		ConsumeLease: true, LiveLimit: 5,
	})
	if err != nil || written.Refused != "" {
		t.Fatalf("reopen promotion = %+v / %v, want stored", written, err)
	}
	if written.Binding.State != "active" || written.Binding.HeadSha != "head-a" {
		t.Fatalf("reopened binding = %+v", written.Binding)
	}
}

// TestStoreUpsertPreviewDeployTakesTheApplicationLock is the F-2 regression:
// the exported upsert serializes with the other preview transitions on the
// per-application row lock — it must wait for a lock holder, and succeed once
// the lock is released.
func TestStoreUpsertPreviewDeployTakesTheApplicationLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := openClaimFixture(t, ctx)
	userID, baseID := claimFixture(t, ctx, st)

	params := sqlc.UpsertPreviewDeployParams{
		ApplicationID: pgUUID(baseID), TeamID: pgUUID(userID), Provider: "github",
		Repo: "octo/gotham", PrNumber: 7, Branch: "feat/x", HeadSha: "head-a",
		Host: "pr-7.example.com", State: "active",
	}

	// Hold the application lock in an outer transaction: an unlocked upsert
	// would slip past it.
	tx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked pgtype.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM applications WHERE id = $1 FOR UPDATE", pgUUID(baseID)).Scan(&locked); err != nil {
		t.Fatalf("take the application lock: %v", err)
	}

	blockedCtx, cancelBlocked := context.WithTimeout(ctx, 3*time.Second)
	defer cancelBlocked()
	if _, err := st.UpsertPreviewDeploy(blockedCtx, params); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("upsert under a held application lock = %v, want the lock to block it", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("release the application lock: %v", err)
	}

	// Without contention the upsert stores the binding.
	stored, err := st.UpsertPreviewDeploy(ctx, params)
	if err != nil {
		t.Fatalf("UpsertPreviewDeploy: %v", err)
	}
	if stored.PrNumber != 7 || stored.HeadSha != "head-a" {
		t.Fatalf("stored binding = %+v", stored)
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
