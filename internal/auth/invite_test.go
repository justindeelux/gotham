package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// registerTestMember creates a member through an owner invite, the only path
// open once the instance has an owner.
func registerTestMember(t *testing.T, svc *Service, email string) *AuthResult {
	t.Helper()
	ctx := context.Background()
	var ownerID uuid.UUID
	if err := svc.store.DB.QueryRow(ctx, `SELECT id FROM users WHERE role='owner'`).Scan(&ownerID); err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	_, token, err := svc.CreateInvite(ctx, ownerID, email, nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	result, err := svc.RegisterInvited(ctx, email, "s3cret-password", token, SessionMeta{})
	if err != nil {
		t.Fatalf("RegisterInvited: %v", err)
	}
	return result
}

func scratchProject(t *testing.T, svc *Service, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := svc.store.DB.QueryRow(context.Background(),
		`INSERT INTO projects (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id
}

func TestInviteLifecycle(t *testing.T) {
	svc, _ := scratchService(t)
	ctx := context.Background()
	owner, err := svc.Register(ctx, uniqueEmail("invite-owner"), "owner-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register owner: %v", err)
	}
	ownerID := uuid.MustParse(owner.User.ID)
	projectID := scratchProject(t, svc, "invite-project")

	email := uniqueEmail("invite-member")
	invite, token, err := svc.CreateInvite(ctx, ownerID, strings.ToUpper(email), []uuid.UUID{projectID, projectID})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if token == "" {
		t.Fatal("CreateInvite returned an empty token")
	}
	if invite.Email != email {
		t.Fatalf("invite email = %q, want normalized %q", invite.Email, email)
	}
	if len(invite.Projects) != 1 || invite.Projects[0].ID != projectID || invite.Projects[0].Name != "invite-project" {
		t.Fatalf("invite projects = %+v", invite.Projects)
	}
	if invite.AcceptedAt != nil {
		t.Fatal("fresh invite is already accepted")
	}

	var storedHash string
	if err := svc.store.DB.QueryRow(ctx, `SELECT token_hash FROM invites WHERE id=$1`, invite.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	if storedHash != hashInviteToken(token) || strings.Contains(storedHash, token) {
		t.Fatalf("stored hash %q is not the token hash", storedHash)
	}

	pending, err := svc.ListInvites(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListInvites = %v, %v", pending, err)
	}
	preview, err := svc.PeekInvite(ctx, token)
	if err != nil || preview.Email != email {
		t.Fatalf("PeekInvite = %+v, %v", preview, err)
	}

	wrongEmail := uniqueEmail("invite-wrong")
	if _, err := svc.RegisterInvited(ctx, wrongEmail, "s3cret-password", token, SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("RegisterInvited(wrong email) error = %v, want ErrRegistrationClosed", err)
	}
	if _, err := svc.store.GetUserByEmail(ctx, wrongEmail); err == nil {
		t.Fatal("mismatched invite created an account")
	}

	member, err := svc.RegisterInvited(ctx, email, "s3cret-password", token, SessionMeta{})
	if err != nil {
		t.Fatalf("RegisterInvited: %v", err)
	}
	if member.User.Role != RoleMember {
		t.Fatalf("invited role = %q, want %q", member.User.Role, RoleMember)
	}
	allowed, err := svc.store.HasProjectGrant(ctx, uuid.MustParse(member.User.ID), projectID)
	if err != nil || !allowed {
		t.Fatalf("invite grant = %v, %v", allowed, err)
	}

	if _, err := svc.PeekInvite(ctx, token); !errors.Is(err, store.ErrInviteUnavailable) {
		t.Fatalf("PeekInvite(consumed) error = %v, want ErrInviteUnavailable", err)
	}
	if _, err := svc.RegisterInvited(ctx, uniqueEmail("invite-replay"), "s3cret-password", token, SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("RegisterInvited(replay) error = %v, want ErrRegistrationClosed", err)
	}

	invites, err := svc.ListInvites(ctx)
	if err != nil || len(invites) != 1 || invites[0].AcceptedAt == nil {
		t.Fatalf("consumed invite list = %+v, %v", invites, err)
	}
}

func TestInviteExpiryAndRevoke(t *testing.T) {
	svc, _ := scratchService(t)
	ctx := context.Background()
	owner, err := svc.Register(ctx, uniqueEmail("invite-expiry-owner"), "owner-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register owner: %v", err)
	}
	ownerID := uuid.MustParse(owner.User.ID)

	email := uniqueEmail("invite-expired")
	invite, token, err := svc.CreateInvite(ctx, ownerID, email, nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := svc.store.DB.Exec(ctx, `UPDATE invites SET expires_at=$1 WHERE id=$2`, time.Now().Add(-time.Hour), invite.ID); err != nil {
		t.Fatalf("expire invite: %v", err)
	}
	if _, err := svc.PeekInvite(ctx, token); !errors.Is(err, store.ErrInviteUnavailable) {
		t.Fatalf("PeekInvite(expired) error = %v, want ErrInviteUnavailable", err)
	}
	if _, err := svc.RegisterInvited(ctx, email, "s3cret-password", token, SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("RegisterInvited(expired) error = %v, want ErrRegistrationClosed", err)
	}
	if _, err := svc.store.GetUserByEmail(ctx, email); err == nil {
		t.Fatal("expired invite created an account")
	}

	revoked, revokeToken, err := svc.CreateInvite(ctx, ownerID, uniqueEmail("invite-revoked"), nil)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if err := svc.RevokeInvite(ctx, revoked.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if _, err := svc.PeekInvite(ctx, revokeToken); !errors.Is(err, store.ErrInviteUnavailable) {
		t.Fatalf("PeekInvite(revoked) error = %v, want ErrInviteUnavailable", err)
	}
	if err := svc.RevokeInvite(ctx, revoked.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeInvite(again) error = %v, want ErrNotFound", err)
	}
}

func TestInviteRejectsTakenEmailAndUnknownProject(t *testing.T) {
	svc, _ := scratchService(t)
	ctx := context.Background()
	ownerEmail := uniqueEmail("invite-guard-owner")
	owner, err := svc.Register(ctx, ownerEmail, "owner-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Register owner: %v", err)
	}
	ownerID := uuid.MustParse(owner.User.ID)

	if _, _, err := svc.CreateInvite(ctx, ownerID, ownerEmail, nil); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("CreateInvite(existing email) error = %v, want ErrEmailTaken", err)
	}
	if _, _, err := svc.CreateInvite(ctx, ownerID, uniqueEmail("invite-unknown-project"), []uuid.UUID{uuid.New()}); !errors.Is(err, store.ErrUnknownProject) {
		t.Fatalf("CreateInvite(unknown project) error = %v, want ErrUnknownProject", err)
	}
}
