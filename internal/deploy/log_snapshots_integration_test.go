package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestBoundedBuildLogHistoricalRows(t *testing.T) {
	if !integrationDSNExplicit() {
		t.Skip("set GOTHAM_TEST_DSN to a disposable database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := integrationDSN()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	repo := newStoreRepository(st, testSecretKey)
	user, err := st.CreateUser(ctx, "snapshot-"+uuid.NewString()+"@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID) })
	team, env, server := seedProjectEnvironment(t, ctx, st)
	row, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{UserID: user.ID, TeamID: pgUUID(team), EnvironmentID: pgUUID(env), ServerID: pgUUID(server), Name: "snapshot", Provider: "github", Repo: "acme/demo", CloneUrl: "https://github.com/acme/demo.git", BuildPack: "dockerfile"})
	if err != nil {
		t.Fatal(err)
	}
	app := uuidFromPG(row.ID)
	dep, err := repo.CreateDeployment(ctx, Deployment{ApplicationID: app, State: StateBuilding, Kind: KindDeploy})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, raw, want string
		tail, bytes     int
		more            bool
	}{
		{"unpersisted", "", "", 100, 64 << 10, false},
		{"exact", "abc", "abc", 100, 3, false},
		{"extra", "abcd", "abcd", 100, 3, true},
		{"trailing-newline", "old\na\nb\n", "a\nb\n", 2, 100, false},
		{"no-trailing-newline", "old\na\nb", "a\nb", 2, 100, false},
		{"giant-first-line", strings.Repeat("x", 2<<20) + "\nfinal\n", strings.Repeat("x", 65), 2, 64, true},
		{"utf8", strings.Repeat("世", 1<<20), strings.Repeat("世", 65), 100, 64, true},
		{"current-cap", sanitizeBuildLog(strings.Repeat("old\n", 1<<18) + "tail\n"), "tail\n", 1, 64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.UpdateDeploymentBuildLog(ctx, dep.ID, tc.raw); err != nil {
				t.Fatal(err)
			}
			got, state, more, err := repo.GetBoundedBuildLog(ctx, app, dep.ID, LogOptions{Tail: tc.tail, MaxBytes: tc.bytes})
			if err != nil || got != tc.want || state != StateBuilding || more != tc.more || len(got) > 4*(tc.bytes+1) || !utf8.ValidString(got) {
				t.Fatalf("bounded row bytes=%d state=%s more=%v err=%v", len(got), state, more, err)
			}
		})
	}
	if _, _, _, err = repo.GetBoundedBuildLog(ctx, uuid.New(), dep.ID, LogOptions{Tail: 1, MaxBytes: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong pairing %v", err)
	}
}
