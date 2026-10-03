package servers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

// TestDeployKeyStampedWithAppTeam covers JUS-5 fix round 2, defect 2: an
// application's deploy key is stamped with the owning app's team, so another
// team that learns its UUID cannot attach it as a node SSH key, while the
// application's own deploy flow keeps working.
func TestDeployKeyStampedWithAppTeam(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	alice, err := st.CreateUser(ctx, fmt.Sprintf("be-r2-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bob, err := st.CreateUser(ctx, fmt.Sprintf("be-r2-b-%d@example.com", suffix), nil)
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
		Name: fmt.Sprintf("deploy-team-%d", suffix),
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

	aliceTeam := teams.WithScope(ctx, teams.Scope{UserID: uuid.UUID(alice.ID.Bytes), TeamID: uuid.UUID(shared.ID.Bytes), Role: teams.RoleOwner})
	bobPersonal := teams.WithScope(ctx, teams.Scope{UserID: uuid.UUID(bob.ID.Bytes), TeamID: teams.PersonalTeamID(uuid.UUID(bob.ID.Bytes)), Role: teams.RoleOwner})

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:    alice.ID,
		TeamID:    shared.ID,
		Name:      "deploy-key-app",
		Provider:  "github",
		Repo:      "acme/demo",
		CloneUrl:  "https://github.com/acme/demo.git",
		Branch:    "main",
		BuildPack: "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteApplication(cleanupCtx, app.ID); err != nil {
			t.Logf("cleanup application: %v", err)
		}
	})

	keyPEM := string(testPrivateKeyPEM(t))
	sealed, err := EncryptKey(keyPEM, "gateway-test-secret")
	if err != nil {
		t.Fatalf("EncryptKey: %v", err)
	}
	mapping, err := st.CreateApplicationDeployKey(ctx, sqlc.CreateApplicationDeployKeyParams{
		ApplicationID: app.ID,
	}, "deploy-key-test", sealed)
	if err != nil {
		t.Fatalf("CreateApplicationDeployKey: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := st.DeleteApplicationDeployKey(cleanupCtx, mapping.ID, app.ID); err != nil {
			t.Logf("cleanup deploy key: %v", err)
		}
	})

	// The sealed row carries the owning app's team, not NULL.
	stored, err := st.GetPrivateKeyByID(ctx, mapping.PrivateKeyID)
	if err != nil {
		t.Fatalf("GetPrivateKeyByID: %v", err)
	}
	if uuidFromPG(stored.TeamID) != uuid.UUID(shared.ID.Bytes) {
		t.Fatalf("deploy key team = %v, want the app's team %v", stored.TeamID, shared.ID)
	}
	deployKeyID := uuidFromPG(mapping.PrivateKeyID)

	// Another team cannot attach it as a node key on either path.
	if _, err := service.Add(bobPersonal, uuid.UUID(bob.ID.Bytes), "bob-node", "127.0.0.1", 22, "root", deployKeyID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("cross-team Add with a deploy key = %v, want ErrValidation", err)
	}
	bobNode, err := service.Add(bobPersonal, uuid.UUID(bob.ID.Bytes), "bob-node", "127.0.0.1", 22, "root", uuid.Nil, "")
	if err != nil {
		t.Fatalf("Add bob node: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(bobNode.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})
	if _, err := service.Update(bobPersonal, bobNode.ID, UpdateParams{SSHKeyID: &deployKeyID}); !errors.Is(err, ErrValidation) {
		t.Errorf("cross-team Update with a deploy key = %v, want ErrValidation", err)
	}

	// The owning team can still attach it, and the application's own flow —
	// mapping to sealed row to decrypted PEM — keeps working.
	if _, err := service.Add(aliceTeam, uuid.UUID(alice.ID.Bytes), "alice-node", "127.0.0.1", 22, "root", deployKeyID, ""); err != nil {
		t.Errorf("own-team Add with the deploy key = %v, want success", err)
	} else {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM servers WHERE ssh_key_id = $1", mapping.PrivateKeyID); err != nil {
				t.Logf("cleanup alice server: %v", err)
			}
		})
	}
	back, err := st.GetApplicationDeployKey(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetApplicationDeployKey: %v", err)
	}
	keyRow, err := st.GetPrivateKeyByID(ctx, back.PrivateKeyID)
	if err != nil {
		t.Fatalf("GetPrivateKeyByID: %v", err)
	}
	if opened, err := DecryptKey(keyRow.EncryptedKey, "gateway-test-secret"); err != nil || opened != keyPEM {
		t.Error("deploy-key mapping no longer opens to the stored PEM")
	}
}

// TestListPrivateKeysByTeam covers JUS-5 fix round 2, defect 4: the key
// listing is scoped by team, so a future caller cannot enumerate another
// team's keys. Legacy (team_id NULL) rows stay listed, mirroring
// ListServersByTeam; attaching them is still gated by keyForTeam.
func TestListPrivateKeysByTeam(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	alice, err := st.CreateUser(ctx, fmt.Sprintf("be-r2l-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bob, err := st.CreateUser(ctx, fmt.Sprintf("be-r2l-b-%d@example.com", suffix), nil)
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
		Name: fmt.Sprintf("list-team-%d", suffix),
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

	aliceTeam := teams.WithScope(ctx, teams.Scope{UserID: uuid.UUID(alice.ID.Bytes), TeamID: uuid.UUID(shared.ID.Bytes), Role: teams.RoleOwner})
	bobPersonal := teams.WithScope(ctx, teams.Scope{UserID: uuid.UUID(bob.ID.Bytes), TeamID: teams.PersonalTeamID(uuid.UUID(bob.ID.Bytes)), Role: teams.RoleOwner})

	teamKey, err := service.AddPrivateKey(aliceTeam, fmt.Sprintf("r2-team-%d", suffix), string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey team: %v", err)
	}
	otherKey, err := service.AddPrivateKey(bobPersonal, fmt.Sprintf("r2-other-%d", suffix), string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey other: %v", err)
	}
	legacyKey, err := service.AddPrivateKey(ctx, fmt.Sprintf("r2-legacy-%d", suffix), string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey legacy: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, id := range []uuid.UUID{teamKey.ID, otherKey.ID, legacyKey.ID} {
			if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(id)); err != nil {
				t.Logf("cleanup delete private key: %v", err)
			}
		}
	})

	rows, err := st.ListPrivateKeysByTeam(ctx, shared.ID)
	if err != nil {
		t.Fatalf("ListPrivateKeysByTeam: %v", err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Name] = true
	}
	if !seen[teamKey.Name] || !seen[legacyKey.Name] {
		t.Errorf("team list = %v, want the team key and the legacy key", seen)
	}
	if seen[otherKey.Name] {
		t.Error("team list leaked another team's key")
	}
}
