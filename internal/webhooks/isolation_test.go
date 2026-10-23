package webhooks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

func TestWebhookManagementEnforcesTeamRole(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	creator := uuid.New()

	repo := newFakeRepository()
	repo.app.UserID = creator
	repo.app.TeamID = teamB
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})
	bg := context.Background()

	viewer := teams.WithScope(bg, teams.Scope{UserID: creator, TeamID: teamB, Role: teams.RoleReadOnly})
	if _, err := svc.CreateWebhook(viewer, creator, repo.app.ID, "https://cp.example.com/api/v1/webhooks"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only CreateWebhook = %v, want ErrForbidden", err)
	}
	if _, err := svc.DeleteWebhook(viewer, creator, repo.app.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only DeleteWebhook = %v, want ErrForbidden", err)
	}
	installer.mu.Lock()
	created := len(installer.created)
	installer.mu.Unlock()
	if created != 0 {
		t.Fatalf("the Git host was called %d times for a read_only member", created)
	}

	stranger := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.CreateWebhook(stranger, uuid.New(), repo.app.ID, "https://cp.example.com/api/v1/webhooks"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign CreateWebhook = %v, want ErrNotFound", err)
	}

	admin := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleAdmin})
	hook, err := svc.CreateWebhook(admin, uuid.New(), repo.app.ID, "https://cp.example.com/api/v1/webhooks")
	if err != nil {
		t.Fatalf("admin CreateWebhook: %v", err)
	}
	if hook.ApplicationID != repo.app.ID {
		t.Fatalf("created hook = %+v", hook)
	}
}
