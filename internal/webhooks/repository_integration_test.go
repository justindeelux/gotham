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

	// One preview of the application, and the stale sweep sees it while it is
	// fresh only.
	list, err := repo.ListPreviews(ctx, baseID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPreviews = %d / %v, want one preview", len(list), err)
	}
	stale, err := repo.ListStalePreviews(ctx, time.Now().UTC().Add(time.Hour))
	if err != nil || len(stale) != 1 {
		t.Fatalf("ListStalePreviews(future) = %d / %v, want one preview", len(stale), err)
	}
	stale, err = repo.ListStalePreviews(ctx, time.Now().UTC().Add(-time.Hour))
	if err != nil || len(stale) != 0 {
		t.Fatalf("ListStalePreviews(past) = %d / %v, want none", len(stale), err)
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
	if _, err := repo.MarkPreviewDeleted(ctx, survivor.ID); err != nil {
		t.Fatalf("MarkPreviewDeleted: %v", err)
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
