package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
)

const (
	inviteTokenBytes = 32
	inviteTTL        = 7 * 24 * time.Hour
)

// ProjectRef is a project a member can reach.
type ProjectRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Invite is an owner-issued invitation. The raw token is never part of it.
type Invite struct {
	ID         uuid.UUID    `json:"id"`
	Email      string       `json:"email"`
	Projects   []ProjectRef `json:"projects"`
	ExpiresAt  time.Time    `json:"expires_at"`
	AcceptedAt *time.Time   `json:"accepted_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

// InvitePreview is the public part of a pending invite: the address it was
// issued to, so the registration form can prefill it.
type InvitePreview struct {
	Email string `json:"email"`
}

// Account is a user together with its project grants.
type Account struct {
	User
	Projects []ProjectRef `json:"projects"`
}

// CreateInvite stores an invite for the owner. It returns the raw token exactly
// once; only its hash is persisted.
func (s *Service) CreateInvite(ctx context.Context, ownerID uuid.UUID, email string, projectIDs []uuid.UUID) (*Invite, string, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return nil, "", err
	}
	if _, err := s.store.GetUserByEmail(ctx, normalized); err == nil {
		return nil, "", ErrEmailTaken
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", fmt.Errorf("auth: invite lookup: %w", err)
	}

	projectIDs = uniqueIDs(projectIDs)
	token, hash, err := newInviteToken()
	if err != nil {
		return nil, "", err
	}
	row, err := s.store.CreateInvite(ctx, normalized, hash, ownerID, s.now().Add(inviteTTL), projectIDs)
	if err != nil {
		return nil, "", err
	}
	invite, err := s.toInvite(ctx, row)
	if err != nil {
		return nil, "", err
	}
	return &invite, token, nil
}

// ListInvites returns every invite, newest first.
func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.store.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	names, err := s.projectNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	invites := make([]Invite, 0, len(rows))
	for _, row := range rows {
		invites = append(invites, toInvite(row, names))
	}
	return invites, nil
}

// RevokeInvite deletes a pending invite. A consumed invite is gone with it.
func (s *Service) RevokeInvite(ctx context.Context, inviteID uuid.UUID) error {
	deleted, err := s.store.DeleteInvite(ctx, inviteID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

// PeekInvite resolves a presented token without consuming it. Unknown, expired
// and already accepted tokens all look the same to the caller.
func (s *Service) PeekInvite(ctx context.Context, token string) (*InvitePreview, error) {
	row, err := s.pendingInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	return &InvitePreview{Email: row.Email}, nil
}

func (s *Service) pendingInvite(ctx context.Context, token string) (store.InviteRow, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return store.InviteRow{}, store.ErrInviteUnavailable
	}
	hash := hashInviteToken(token)
	row, err := s.store.InviteByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.InviteRow{}, store.ErrInviteUnavailable
		}
		return store.InviteRow{}, fmt.Errorf("auth: invite lookup: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(hash)) != 1 {
		return store.InviteRow{}, store.ErrInviteUnavailable
	}
	if row.AcceptedAt != nil || !s.now().Before(row.ExpiresAt) {
		return store.InviteRow{}, store.ErrInviteUnavailable
	}
	return row, nil
}

// ListUsers returns every account for the owner's member list.
func (s *Service) ListUsers(ctx context.Context) ([]Account, error) {
	rows, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	names, err := s.accountProjectNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, toAccount(row, names))
	}
	return accounts, nil
}

// RemoveMember deletes a member account while keeping the resources they
// created: those move to the owner. The owner is refused; sessions, API tokens
// and grants are revoked in the same transaction.
func (s *Service) RemoveMember(ctx context.Context, ownerID, userID uuid.UUID) error {
	return s.store.DeleteMember(ctx, ownerID, userID)
}

// ResetMember drops a member's sessions and two-factor configuration. The owner
// is refused, so no member reset can lock the owner out.
func (s *Service) ResetMember(ctx context.Context, userID uuid.UUID) error {
	return s.store.ResetMember(ctx, userID)
}

// SetMemberProjects replaces the member's project grants. The owner and unknown
// users are refused.
func (s *Service) SetMemberProjects(ctx context.Context, ownerID, userID uuid.UUID, projectIDs []uuid.UUID) (*Account, error) {
	if err := s.store.ReplaceMemberProjects(ctx, ownerID, userID, uniqueIDs(projectIDs)); err != nil {
		return nil, err
	}
	return s.account(ctx, userID)
}

func (s *Service) account(ctx context.Context, userID uuid.UUID) (*Account, error) {
	rows, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	names, err := s.accountProjectNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ID == userID {
			account := toAccount(row, names)
			return &account, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) toInvite(ctx context.Context, row store.InviteRow) (Invite, error) {
	names, err := s.store.ProjectNames(ctx, row.ProjectIDs)
	if err != nil {
		return Invite{}, err
	}
	index := make(map[uuid.UUID]string, len(names))
	for _, name := range names {
		index[name.ID] = name.Name
	}
	return toInvite(row, index), nil
}

func (s *Service) projectNames(ctx context.Context, rows []store.InviteRow) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0)
	for _, row := range rows {
		ids = append(ids, row.ProjectIDs...)
	}
	return s.projectNameMap(ctx, ids)
}

func (s *Service) accountProjectNames(ctx context.Context, rows []store.Account) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0)
	for _, row := range rows {
		ids = append(ids, row.ProjectIDs...)
	}
	return s.projectNameMap(ctx, ids)
}

func (s *Service) projectNameMap(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	names, err := s.store.ProjectNames(ctx, uniqueIDs(ids))
	if err != nil {
		return nil, err
	}
	index := make(map[uuid.UUID]string, len(names))
	for _, name := range names {
		index[name.ID] = name.Name
	}
	return index, nil
}

func toInvite(row store.InviteRow, names map[uuid.UUID]string) Invite {
	return Invite{
		ID:         row.ID,
		Email:      row.Email,
		Projects:   projectRefs(row.ProjectIDs, names),
		ExpiresAt:  row.ExpiresAt,
		AcceptedAt: row.AcceptedAt,
		CreatedAt:  row.CreatedAt,
	}
}

func toAccount(row store.Account, names map[uuid.UUID]string) Account {
	return Account{
		User: User{
			ID:          row.ID.String(),
			Email:       row.Email,
			Role:        row.Role,
			CreatedAt:   row.CreatedAt,
			DisplayName: row.DisplayName,
			HasPassword: row.HasPassword,
		},
		Projects: projectRefs(row.ProjectIDs, names),
	}
}

func projectRefs(ids []uuid.UUID, names map[uuid.UUID]string) []ProjectRef {
	refs := make([]ProjectRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, ProjectRef{ID: id, Name: names[id]})
	}
	return refs
}

// acceptInvite consumes the invite for the freshly created account and installs
// its grants. It runs after account creation, so the caller compensates a
// failure by deleting the account.
func (s *Service) acceptInvite(ctx context.Context, token, email string, userID uuid.UUID) error {
	_, err := s.store.AcceptInvite(ctx, hashInviteToken(strings.TrimSpace(token)), email, userID)
	return err
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	return unique
}

func newInviteToken() (token string, hash string, err error) {
	buf := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate invite token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashInviteToken(token), nil
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
