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

	// Ownership: another user must see nothing rather than someone's app.
	if _, err := repo.GetApplication(ctx, appID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign application error = %v, want ErrNotFound", err)
	}
	app, err := repo.GetApplication(ctx, appID, userID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if app.Repo != "Octo/Gotham" || app.Branch != "main" {
		t.Errorf("application = %+v", app)
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
