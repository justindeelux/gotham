package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type storeInvites struct {
	store *store.Store
}

func (a storeInvites) PeekInvite(ctx context.Context, token string) (string, string, error) {
	invite, err := a.store.GetInviteByTokenHash(ctx, hashTestToken(token))
	if err != nil {
		return "", "", err
	}
	team, err := a.store.GetTeam(ctx, invite.TeamID)
	if err != nil {
		return "", "", err
	}
	return team.Name, invite.Email, nil
}

func (a storeInvites) AcceptInvite(ctx context.Context, userID uuid.UUID, token string) error {
	_, err := a.store.AcceptInvite(ctx, hashTestToken(token), pgUUID(userID))
	return err
}

func hashTestToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newTestInvite(t *testing.T, st *store.Store, email string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	owner, err := st.CreateUser(ctx, uniqueEmail("invite-owner"), nil)
	if err != nil {
		t.Fatalf("create invite owner: %v", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("invite token: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	if _, err := st.CreateInvite(ctx, sqlc.CreateInviteParams{
		TeamID:    owner.ID,
		Email:     email,
		Role:      "read_only",
		TokenHash: hashTestToken(token),
		InvitedBy: owner.ID,
		ExpiresAt: pgTimestamp(time.Now().Add(24 * time.Hour)),
	}); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanup, "DELETE FROM invites WHERE email = $1", email)
		_, _ = st.DB.Exec(cleanup, "DELETE FROM teams WHERE id = $1", owner.ID)
		_, _ = st.DB.Exec(cleanup, "DELETE FROM users WHERE id = $1", owner.ID)
	})
	return token
}

func TestServiceRegisterClosedWithoutInvite(t *testing.T) {
	svc, st := scratchService(t)
	requireClosedInstance(t, svc)
	ctx := context.Background()

	email := uniqueEmail("closed")
	cleanupUser(t, st, email)

	_, err := svc.Register(ctx, email, "s3cret-password", "", nil, SessionMeta{})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register error = %v, want ErrRegistrationClosed", err)
	}

	_, err = svc.Register(ctx, email, "s3cret-password", "not-a-real-token", storeInvites{st}, SessionMeta{})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register(bad token) error = %v, want ErrRegistrationClosed", err)
	}

	_, err = svc.Register(ctx, email, "s3cret-password", newTestInvite(t, st, uniqueEmail("other")), storeInvites{st}, SessionMeta{})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register(foreign invite) error = %v, want ErrRegistrationClosed", err)
	}

	if _, err := st.GetUserByEmail(ctx, email); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("closed registrations must not create an account, got %v", err)
	}
}

func TestServiceRegisterInviteAccepts(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("invited")
	cleanupUser(t, st, email)
	token := newTestInvite(t, st, email)

	result, err := svc.Register(ctx, email, "s3cret-password", token, storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("Register returned an incomplete token pair: %+v", result)
	}

	userID, err := uuid.Parse(result.User.ID)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	invite, err := st.GetInviteByTokenHash(ctx, hashTestToken(token))
	if err != nil {
		t.Fatalf("GetInviteByTokenHash: %v", err)
	}
	if !invite.AcceptedAt.Valid {
		t.Error("invite was not marked accepted")
	}

	if _, err := st.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
		TeamID: invite.TeamID,
		UserID: pgUUID(userID),
	}); err != nil {
		t.Fatalf("membership missing: %v", err)
	}

	if _, err := svc.Register(ctx, email, "s3cret-password", token, storeInvites{st}, SessionMeta{}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("Register(replayed token) error = %v, want ErrEmailTaken", err)
	}

	other := uniqueEmail("replay")
	cleanupUser(t, st, other)
	if _, err := svc.Register(ctx, other, "s3cret-password", token, storeInvites{st}, SessionMeta{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register(foreign replay) error = %v, want ErrRegistrationClosed", err)
	}
}
