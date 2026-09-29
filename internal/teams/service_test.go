package teams

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

// discardLogger keeps the service's operational logging out of the test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestService wires the real service onto the in-memory repository.
func newTestService(repo *fakeRepository) *Service {
	return NewService(Config{Repository: repo, Logger: discardLogger()})
}

// seedTeam creates a team owned by owner with the given extra members.
func seedTeam(t *testing.T, repo *fakeRepository, owner uuid.UUID, members map[uuid.UUID]Role) uuid.UUID {
	t.Helper()
	repo.seedUser(owner, "owner@example.com")
	teamID := uuid.New()
	repo.seedTeam(Team{ID: teamID, Name: "team"}, owner)
	for userID, role := range members {
		repo.seedMember(teamID, userID, role)
	}
	return teamID
}

func TestServiceCreateTeamAddsOwner(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	repo.seedUser(userID, "creator@example.com")
	svc := newTestService(repo)

	created, err := svc.Create(context.Background(), userID, "  Platform  ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Platform" {
		t.Errorf("name = %q, want the trimmed value", created.Name)
	}

	member, err := repo.GetMember(context.Background(), created.ID, userID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if member.Role != RoleOwner {
		t.Errorf("creator role = %q, want owner", member.Role)
	}

	all, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].ID != created.ID || all[0].Role != RoleOwner {
		t.Fatalf("List = %+v, want the created team with the owner role", all)
	}
}

func TestServiceCreateTeamRejectsEmptyName(t *testing.T) {
	svc := newTestService(newFakeRepository())
	if _, err := svc.Create(context.Background(), uuid.New(), "   "); !errors.Is(err, ErrValidation) {
		t.Fatalf("Create(empty) = %v, want ErrValidation", err)
	}
}

func TestServiceMembershipResolvesPersonalTeamWithoutLookup(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo)
	userID := uuid.New()

	role, err := svc.Membership(context.Background(), PersonalTeamID(userID), userID)
	if err != nil || role != RoleOwner {
		t.Fatalf("Membership(personal) = %q, %v; want owner", role, err)
	}
	if _, err := svc.Membership(context.Background(), uuid.New(), userID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Membership(unknown team) = %v, want ErrNotFound", err)
	}
	if _, err := svc.Membership(context.Background(), uuid.Nil, userID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Membership(zero) = %v, want ErrNotFound", err)
	}
}

func TestServiceRenameRBAC(t *testing.T) {
	owner, admin, viewer, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(admin, "admin@example.com")
	repo.seedUser(viewer, "viewer@example.com")
	teamID := seedTeam(t, repo, owner, map[uuid.UUID]Role{
		admin:  RoleAdmin,
		viewer: RoleReadOnly,
	})
	svc := newTestService(repo)
	ctx := context.Background()

	cases := []struct {
		name    string
		actor   uuid.UUID
		wantErr error
	}{
		{name: "owner", actor: owner},
		{name: "admin", actor: admin},
		{name: "read_only", actor: viewer, wantErr: ErrForbidden},
		{name: "stranger", actor: stranger, wantErr: ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Rename(ctx, tc.actor, teamID, "renamed")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Rename = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestServiceDeleteTeamRBAC(t *testing.T) {
	owner, admin := uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(admin, "admin@example.com")
	teamID := seedTeam(t, repo, owner, map[uuid.UUID]Role{admin: RoleAdmin})
	svc := newTestService(repo)
	ctx := context.Background()

	if err := svc.Delete(ctx, admin, teamID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin Delete = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(ctx, owner, teamID); err != nil {
		t.Fatalf("owner Delete: %v", err)
	}
	if _, err := repo.GetTeam(ctx, teamID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("team still present after delete: %v", err)
	}

	personal := uuid.New()
	repo.seedTeam(Team{ID: PersonalTeamID(personal), Name: "personal", IsPersonal: true}, personal)
	if err := svc.Delete(ctx, personal, PersonalTeamID(personal)); !errors.Is(err, ErrPersonalTeam) {
		t.Fatalf("Delete(personal) = %v, want ErrPersonalTeam", err)
	}
}

func TestServiceMemberManagement(t *testing.T) {
	owner, admin, otherAdmin := uuid.New(), uuid.New(), uuid.New()
	repo := newFakeRepository()
	for id, email := range map[uuid.UUID]string{
		admin: "admin@example.com", otherAdmin: "other@example.com",
	} {
		repo.seedUser(id, email)
	}
	teamID := seedTeam(t, repo, owner, map[uuid.UUID]Role{
		admin:      RoleAdmin,
		otherAdmin: RoleAdmin,
	})
	svc := newTestService(repo)
	ctx := context.Background()

	// An admin may not touch an owner, nor hand out ownership.
	if _, err := svc.SetMemberRole(ctx, admin, teamID, owner, RoleReadOnly); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin demote owner = %v, want ErrForbidden", err)
	}
	if _, err := svc.SetMemberRole(ctx, admin, teamID, otherAdmin, RoleOwner); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin grant owner = %v, want ErrForbidden", err)
	}
	if err := svc.RemoveMember(ctx, admin, teamID, owner); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin remove owner = %v, want ErrForbidden", err)
	}
	// An admin manages non-owner members.
	if member, err := svc.SetMemberRole(ctx, admin, teamID, otherAdmin, RoleReadOnly); err != nil || member.Role != RoleReadOnly {
		t.Fatalf("admin demote admin = %+v, %v; want read_only", member, err)
	}
	if err := svc.RemoveMember(ctx, admin, teamID, otherAdmin); err != nil {
		t.Fatalf("admin remove read_only: %v", err)
	}
	// The last owner can never be demoted or removed.
	if _, err := svc.SetMemberRole(ctx, owner, teamID, owner, RoleAdmin); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("last owner demote = %v, want ErrLastOwner", err)
	}
	if err := svc.RemoveMember(ctx, owner, teamID, owner); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("last owner remove = %v, want ErrLastOwner", err)
	}
	// The owner may promote a second owner, and then leave.
	if _, err := svc.SetMemberRole(ctx, owner, teamID, admin, RoleOwner); err != nil {
		t.Fatalf("owner promote: %v", err)
	}
	if err := svc.RemoveMember(ctx, owner, teamID, owner); err != nil {
		t.Fatalf("owner leave with a second owner: %v", err)
	}
}

func TestServiceReadOnlyCannotManage(t *testing.T) {
	owner, viewer, target := uuid.New(), uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(viewer, "viewer@example.com")
	repo.seedUser(target, "target@example.com")
	teamID := seedTeam(t, repo, owner, map[uuid.UUID]Role{
		viewer: RoleReadOnly,
		target: RoleReadOnly,
	})
	svc := newTestService(repo)
	ctx := context.Background()

	if err := svc.RemoveMember(ctx, viewer, teamID, target); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only remove = %v, want ErrForbidden", err)
	}
	if _, err := svc.SetMemberRole(ctx, viewer, teamID, target, RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only role change = %v, want ErrForbidden", err)
	}
	if _, err := svc.Rename(ctx, viewer, teamID, "renamed"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only rename = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(ctx, viewer, teamID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only delete = %v, want ErrForbidden", err)
	}
	// Any member may leave, including a read_only one.
	if err := svc.RemoveMember(ctx, viewer, teamID, viewer); err != nil {
		t.Fatalf("read_only leave: %v", err)
	}
}

func TestServiceInviteLifecycle(t *testing.T) {
	owner, invitee := uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(invitee, "invitee@example.com")
	teamID := seedTeam(t, repo, owner, nil)
	svc := newTestService(repo)
	ctx := context.Background()

	invite, token, err := svc.Invite(ctx, owner, teamID, "Invitee@Example.com", RoleReadOnly)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if token == "" {
		t.Fatal("Invite returned an empty token")
	}
	if invite.Email != "invitee@example.com" {
		t.Errorf("invite email = %q, want the normalized address", invite.Email)
	}
	if stored, ok := repo.inviteFor(token); !ok || stored.ID != invite.ID {
		t.Fatal("the invite is not stored behind the token hash")
	}
	if stored, _ := repo.inviteFor(token); stored.Role != RoleReadOnly {
		t.Errorf("stored role = %q, want read_only", stored.Role)
	}

	team, err := svc.Accept(ctx, invitee, token)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if team.ID != teamID || team.Role != RoleReadOnly {
		t.Fatalf("accepted team = %+v, want the team with the invited role", team)
	}

	// Replaying the token is refused.
	if _, err := svc.Accept(ctx, invitee, token); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("replayed Accept = %v, want ErrInviteUsed", err)
	}
	// An unknown token is not found.
	if _, err := svc.Accept(ctx, invitee, "not-a-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown Accept = %v, want ErrNotFound", err)
	}
}

func TestServiceInviteRejections(t *testing.T) {
	owner, invitee, stranger, viewer := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(invitee, "invitee@example.com")
	repo.seedUser(stranger, "stranger@example.com")
	repo.seedUser(viewer, "viewer@example.com")
	teamID := seedTeam(t, repo, owner, map[uuid.UUID]Role{viewer: RoleReadOnly})
	svc := newTestService(repo)
	ctx := context.Background()

	// A read_only member cannot invite, a stranger cannot even see the team.
	if _, _, err := svc.Invite(ctx, viewer, teamID, invitee.String()+"@example.com", RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Invite = %v, want ErrForbidden", err)
	}
	if _, _, err := svc.Invite(ctx, stranger, teamID, "x@example.com", RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger Invite = %v, want ErrNotFound", err)
	}
	// Ownership can never be handed out through an invite.
	if _, _, err := svc.Invite(ctx, owner, teamID, "x@example.com", RoleOwner); !errors.Is(err, ErrValidation) {
		t.Fatalf("owner-role Invite = %v, want ErrValidation", err)
	}
	if _, _, err := svc.Invite(ctx, owner, teamID, "not-an-email", RoleAdmin); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad email Invite = %v, want ErrValidation", err)
	}

	// An invite addressed to someone else cannot be accepted.
	invite, token, err := svc.Invite(ctx, owner, teamID, "invitee@example.com", RoleAdmin)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := svc.Accept(ctx, stranger, token); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign Accept = %v, want ErrForbidden", err)
	}

	// An expired invite is refused.
	repo.expireInvite(invite.ID)
	if _, err := svc.Accept(ctx, invitee, token); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired Accept = %v, want ErrInviteExpired", err)
	}

	// A revoked invite is gone.
	revoked, revokedToken, err := svc.Invite(ctx, owner, teamID, "invitee@example.com", RoleAdmin)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := svc.RevokeInvite(ctx, owner, teamID, revoked.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if _, err := svc.Accept(ctx, invitee, revokedToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked Accept = %v, want ErrNotFound", err)
	}

	// Listing invites is a management action.
	if _, err := svc.Invites(ctx, viewer, teamID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only Invites = %v, want ErrForbidden", err)
	}
	if invites, err := svc.Invites(ctx, owner, teamID); err != nil || len(invites) != 1 {
		t.Fatalf("owner Invites = %+v, %v; want the one pending invite", invites, err)
	}
}

func TestServiceListOrdersPersonalLast(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	repo.seedUser(userID, "user@example.com")
	personal := Team{ID: PersonalTeamID(userID), Name: "user@example.com's team", IsPersonal: true, CreatedAt: time.Now()}
	repo.seedTeam(personal, userID)
	shared := Team{ID: uuid.New(), Name: "shared", CreatedAt: time.Now().Add(time.Minute)}
	repo.seedTeam(shared, userID)
	svc := newTestService(repo)

	all, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || all[0].ID != shared.ID {
		t.Fatalf("List = %+v, want the shared team before the personal one", all)
	}
}
