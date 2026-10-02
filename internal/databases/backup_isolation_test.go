package databases

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestBackupManagerTeamIsolation is the F2 regression at the service layer: the
// backup surface must authorize the parent database's team and the caller's
// role. Before the fix, the routes resolved no team scope, so the creator
// fallback in Scope.AuthorizeResource let a demoted (read_only) or removed
// creator keep scheduling backups, restoring and deleting them.
func TestBackupManagerTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	bob := uuid.New()

	databaseRepo := newFakeRepository()
	database := databaseRepo.seed(Database{
		UserID:   bob,
		TeamID:   teamB,
		ServerID: databaseRepo.seedServer(),
		Name:     "team-db",
		Engine:   "postgres",
		Status:   StatusRunning,
	})
	manager := NewBackupService(BackupConfig{
		Repository:         newFakeBackupRepository(),
		DatabaseRepository: databaseRepo,
		Containers:         &fakeContainers{},
		Secret:             testSecret,
		Logger:             discardLogger(),
	})
	bg := context.Background()

	// The demoted creator is still a member of the team (read_only): reads are
	// allowed, every mutation is refused.
	viewer := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleReadOnly})
	if _, err := manager.ListBackups(viewer, bob, database.ID, 0); err != nil {
		t.Fatalf("read_only ListBackups: %v", err)
	}
	if _, err := manager.ListSchedules(viewer, bob, database.ID); err != nil {
		t.Fatalf("read_only ListSchedules: %v", err)
	}
	if _, err := manager.CreateBackup(viewer, bob, database.ID, CreateBackupRequest{}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only CreateBackup = %v, want ErrForbidden", err)
	}
	if _, err := manager.CreateSchedule(viewer, bob, database.ID, ScheduleRequest{Cron: "0 0 * * *"}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only CreateSchedule = %v, want ErrForbidden", err)
	}
	if _, err := manager.RestoreBackup(viewer, bob, database.ID, RestoreRequest{BackupID: uuid.New()}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only RestoreBackup = %v, want ErrForbidden", err)
	}
	if err := manager.DeleteBackup(viewer, bob, database.ID, uuid.New()); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only DeleteBackup = %v, want ErrForbidden", err)
	}

	// A member of another team cannot even see the database.
	stranger := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleOwner})
	if _, err := manager.ListBackups(stranger, uuid.New(), database.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign ListBackups = %v, want ErrNotFound", err)
	}
	if _, err := manager.CreateBackup(stranger, uuid.New(), database.ID, CreateBackupRequest{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign CreateBackup = %v, want ErrNotFound", err)
	}

	// An owner/admin of the database's team may schedule backups again.
	admin := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleAdmin})
	if _, err := manager.CreateSchedule(admin, bob, database.ID, ScheduleRequest{Cron: "0 0 * * *"}); err != nil {
		t.Fatalf("admin CreateSchedule: %v", err)
	}
}
