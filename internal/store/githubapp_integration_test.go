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

// TestGitHubAppPersistence runs migration 00038 and exercises the GitHub App
// queries: owner scoping, installation upsert, atomic cache replace (revoked
// repos disappear), per-connection application counts, push targets and the
// delete cascade. It skips when no database is reachable.
func TestGitHubAppPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	email := fmt.Sprintf("gs5-githubapp-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})
	other, err := st.CreateUser(ctx, "other-"+email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", other.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", other.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})

	app, err := st.CreateGitHubApp(ctx, sqlc.CreateGitHubAppParams{
		UserID:              user.ID,
		AppID:               123,
		Slug:                "gotham-test",
		Name:                "gotham-test",
		BaseUrl:             "https://github.com",
		ApiBaseUrl:          "https://api.github.com",
		ClientID:            "cid",
		WebhookSecretCipher: "sealed-secret",
		PrivateKeyCipher:    "sealed-key",
	})
	if err != nil {
		t.Fatalf("CreateGitHubApp: %v", err)
	}

	// Owner scoping: another user cannot read the app.
	if _, err := st.GetGitHubAppByIDAndUser(ctx, sqlc.GetGitHubAppByIDAndUserParams{
		ID:     app.ID,
		UserID: other.ID,
	}); err == nil {
		t.Fatal("another user read the app")
	}
	if _, err := st.GetGitHubAppByIDAndUser(ctx, sqlc.GetGitHubAppByIDAndUserParams{
		ID:     app.ID,
		UserID: user.ID,
	}); err != nil {
		t.Fatalf("owner read: %v", err)
	}

	inst, err := st.UpsertGitHubInstallation(ctx, sqlc.UpsertGitHubInstallationParams{
		GithubAppID:    app.ID,
		InstallationID: 999,
		Account:        "acme",
	})
	if err != nil {
		t.Fatalf("UpsertGitHubInstallation: %v", err)
	}
	again, err := st.UpsertGitHubInstallation(ctx, sqlc.UpsertGitHubInstallationParams{
		GithubAppID:    app.ID,
		InstallationID: 999,
		Account:        "acme-renamed",
	})
	if err != nil {
		t.Fatalf("UpsertGitHubInstallation again: %v", err)
	}
	if again.ID != inst.ID || again.Account != "acme-renamed" {
		t.Fatalf("upsert is not idempotent: %+v", again)
	}

	// The installation join backs webhook verification.
	byInstall, err := st.ListGitHubAppsByInstallationID(ctx, 999)
	if err != nil {
		t.Fatalf("ListGitHubAppsByInstallationID: %v", err)
	}
	if len(byInstall) != 1 || byInstall[0].WebhookSecretCipher != "sealed-secret" {
		t.Fatalf("by installation = %+v", byInstall)
	}

	repos := func(names ...string) []sqlc.UpsertGitHubRepoCacheParams {
		params := make([]sqlc.UpsertGitHubRepoCacheParams, 0, len(names))
		for i, name := range names {
			params = append(params, sqlc.UpsertGitHubRepoCacheParams{
				GithubAppID:    app.ID,
				InstallationID: 999,
				ExternalID:     fmt.Sprintf("%d", i),
				Name:           name,
				FullName:       "acme/" + name,
				DefaultBranch:  "main",
				CloneUrl:       "https://github.com/acme/" + name + ".git",
			})
		}
		return params
	}
	if err := st.ReplaceGitHubRepoCache(ctx, app.ID, 999, repos("web", "api")); err != nil {
		t.Fatalf("ReplaceGitHubRepoCache: %v", err)
	}
	// Revoking "api" upstream removes it from the cache.
	if err := st.ReplaceGitHubRepoCache(ctx, app.ID, 999, repos("web")); err != nil {
		t.Fatalf("ReplaceGitHubRepoCache again: %v", err)
	}
	cached, err := st.ListGitHubRepoCache(ctx, sqlc.ListGitHubRepoCacheParams{
		GithubAppID:    app.ID,
		InstallationID: 999,
	})
	if err != nil {
		t.Fatalf("ListGitHubRepoCache: %v", err)
	}
	if len(cached) != 1 || cached[0].FullName != "acme/web" {
		t.Fatalf("cache after revoke = %+v", cached)
	}

	// Per-connection counts and push targets join applications against the
	// granted repos. Seed one application watching acme/web directly.
	var projectID, envID, serverID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO projects (team_id, name) VALUES ($1, $2) RETURNING id`,
		user.ID, "shop").Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO environments (project_id, name) VALUES ($1, $2) RETURNING id`,
		projectID, "production").Scan(&envID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("gs5-%d", time.Now().UnixNano())).Scan(&serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	var appID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO applications
		(user_id, team_id, server_id, environment_id, name, provider, repo, clone_url, source_type, branch, build_pack, port, host_port, github_app_id)
		VALUES ($1, $1, $2, $3, 'web', 'github', 'acme/web', 'https://github.com/acme/web.git', 'github_app', 'main', 'dockerfile', 3000, 0, $4)
		RETURNING id`, user.ID, serverID, envID, app.ID).Scan(&appID); err != nil {
		t.Fatalf("seed application: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM applications WHERE id = $1", appID); err != nil {
			t.Logf("cleanup application: %v", err)
		}
	})
	// The disconnect warning names applications by link, like push routing
	// and cloning: a linked row counts even when its grant was revoked, and
	// an unlinked row never counts even when granted.
	names, err := st.ListGitHubAppApplicationNames(ctx, sqlc.ListGitHubAppApplicationNamesParams{
		UserID:      user.ID,
		GithubAppID: app.ID,
	})
	if err != nil {
		t.Fatalf("ListGitHubAppApplicationNames: %v", err)
	}
	if len(names) != 1 || names[0] != "web" {
		t.Fatalf("linked applications = %+v, want [web]", names)
	}
	targets, err := st.ListGitHubAppPushTargets(ctx, sqlc.ListGitHubAppPushTargetsParams{
		UserID:      user.ID,
		GithubAppID: app.ID,
		Repo:        "acme/web",
	})
	if err != nil {
		t.Fatalf("ListGitHubAppPushTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].Branch != "main" {
		t.Fatalf("push targets = %+v", targets)
	}
	// Unlinked rows never deploy through the app, even watching the repo.
	if _, err := pool.Exec(ctx, `INSERT INTO applications
		(user_id, team_id, server_id, environment_id, name, provider, repo, clone_url, source_type, branch, build_pack, port, host_port)
		VALUES ($1, $1, $2, $3, 'legacy', 'github', 'acme/web', 'git@github.com:acme/web.git', 'github_app', 'main', 'dockerfile', 3000, 0)`,
		user.ID, serverID, envID); err != nil {
		t.Fatalf("seed legacy application: %v", err)
	}
	if targets, err := st.ListGitHubAppPushTargets(ctx, sqlc.ListGitHubAppPushTargetsParams{
		UserID:      user.ID,
		GithubAppID: app.ID,
		Repo:        "acme/web",
	}); err != nil || len(targets) != 1 {
		t.Fatalf("targets with legacy row = %+v, %v", targets, err)
	}
	// The unlinked row watches a granted repo but is not linked: the
	// disconnect warning ignores it.
	if names, err := st.ListGitHubAppApplicationNames(ctx, sqlc.ListGitHubAppApplicationNamesParams{
		UserID:      user.ID,
		GithubAppID: app.ID,
	}); err != nil || len(names) != 1 {
		t.Fatalf("linked applications with legacy row = %+v, %v", names, err)
	}
	// A linked row whose grant was revoked still counts: the disconnect
	// clears its link.
	if _, err := pool.Exec(ctx, `INSERT INTO applications
		(user_id, team_id, server_id, environment_id, name, provider, repo, clone_url, source_type, branch, build_pack, port, host_port, github_app_id)
		VALUES ($1, $1, $2, $3, 'stale', 'github', 'acme/gone', 'https://github.com/acme/gone.git', 'github_app', 'main', 'dockerfile', 3000, 0, $4)`,
		user.ID, serverID, envID, app.ID); err != nil {
		t.Fatalf("seed revoked application: %v", err)
	}
	if names, err := st.ListGitHubAppApplicationNames(ctx, sqlc.ListGitHubAppApplicationNamesParams{
		UserID:      user.ID,
		GithubAppID: app.ID,
	}); err != nil || len(names) != 2 || names[0] != "stale" || names[1] != "web" {
		t.Fatalf("linked applications with revoked row = %+v, %v", names, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM applications WHERE name = 'stale' AND user_id = $1`, user.ID); err != nil {
		t.Fatalf("cleanup revoked application: %v", err)
	}
	if targets, err := st.ListGitHubAppPushTargets(ctx, sqlc.ListGitHubAppPushTargetsParams{
		UserID: other.ID,
		Repo:   "acme/web",
	}); err != nil || len(targets) != 0 {
		t.Fatalf("another user's targets = %+v, %v", targets, err)
	}

	// Delete cascades to installations and cache.
	if _, err := st.DeleteGitHubApp(ctx, sqlc.DeleteGitHubAppParams{
		ID:     app.ID,
		UserID: user.ID,
	}); err != nil {
		t.Fatalf("DeleteGitHubApp: %v", err)
	}
	// Deleting the connection unlinks instead of deleting the application.
	var link pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT github_app_id FROM applications WHERE id = $1`, appID).Scan(&link); err != nil {
		t.Fatalf("link after delete: %v", err)
	}
	if link.Valid {
		t.Fatal("application still linked after connection delete")
	}
	insts, err := st.ListGitHubInstallations(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListGitHubInstallations: %v", err)
	}
	if len(insts) != 0 {
		t.Fatalf("installations after delete = %+v", insts)
	}
	cached, err = st.ListGitHubRepoCache(ctx, sqlc.ListGitHubRepoCacheParams{
		GithubAppID:    app.ID,
		InstallationID: 999,
	})
	if err != nil {
		t.Fatalf("ListGitHubRepoCache: %v", err)
	}
	if len(cached) != 0 {
		t.Fatalf("cache after delete = %+v", cached)
	}
}
