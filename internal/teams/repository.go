package teams

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Repository persists teams, memberships and invites. It is implemented over
// *store.Store (sqlc) in production and by fakes in tests.
type Repository interface {
	// CreateTeam stores a team with userID as its owner in one transaction.
	CreateTeam(ctx context.Context, team Team, ownerID uuid.UUID) (Team, error)
	// GetTeam returns a team by ID, or ErrNotFound.
	GetTeam(ctx context.Context, teamID uuid.UUID) (Team, error)
	// GetTeamForUser returns a team with the caller's role, or ErrNotFound
	// when the caller is not a member. It is the authorization read of every
	// team-scoped route.
	GetTeamForUser(ctx context.Context, teamID, userID uuid.UUID) (Team, error)
	// ListTeamsByUser returns the caller's teams, each with the caller's role.
	ListTeamsByUser(ctx context.Context, userID uuid.UUID) ([]Team, error)
	// RenameTeam updates a team's name and returns the row, or ErrNotFound.
	RenameTeam(ctx context.Context, teamID uuid.UUID, name string) (Team, error)
	// DeleteTeam removes a team; memberships, invites and team resources
	// cascade.
	DeleteTeam(ctx context.Context, teamID uuid.UUID) error
	// CreateMember adds a membership, or ErrConflict when it already exists.
	CreateMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (Member, error)
	// GetMember returns one membership, or ErrNotFound.
	GetMember(ctx context.Context, teamID, userID uuid.UUID) (Member, error)
	// ListMembers returns a team's members with their account email.
	ListMembers(ctx context.Context, teamID uuid.UUID) ([]Member, error)
	// UpdateMemberRole changes a membership's role, or ErrNotFound.
	UpdateMemberRole(ctx context.Context, teamID, userID uuid.UUID, role Role) (Member, error)
	// DeleteMember removes a membership.
	DeleteMember(ctx context.Context, teamID, userID uuid.UUID) error
	// CountOwners reports how many owners a team has.
	CountOwners(ctx context.Context, teamID uuid.UUID) (int64, error)
	// CreateInvite stores a hashed invite and returns it.
	CreateInvite(ctx context.Context, invite Invite, tokenHash string) (Invite, error)
	// ListInvites returns a team's invites, newest first.
	ListInvites(ctx context.Context, teamID uuid.UUID) ([]Invite, error)
	// GetInviteByTokenHash returns the invite behind a token hash, or
	// ErrNotFound.
	GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error)
	// AcceptInvite consumes a pending invite and inserts the membership in one
	// transaction. A replayed or expired token answers ErrInviteUsed /
	// ErrInviteExpired; an existing membership answers ErrConflict.
	AcceptInvite(ctx context.Context, tokenHash string, userID uuid.UUID) (Invite, error)
	// DeleteInvite removes one invite of a team.
	DeleteInvite(ctx context.Context, teamID, inviteID uuid.UUID) error
	// UserEmail returns the account email of userID, or ErrNotFound.
	UserEmail(ctx context.Context, userID uuid.UUID) (string, error)
}

// storeRepository adapts *store.Store to Repository.
type storeRepository struct {
	store *store.Store
}

// Compile-time guarantee.
var _ Repository = (*storeRepository)(nil)

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

// CreateTeam implements Repository.
func (r *storeRepository) CreateTeam(ctx context.Context, team Team, ownerID uuid.UUID) (Team, error) {
	row, err := r.store.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:         pgUUID(team.ID),
		Name:       team.Name,
		IsPersonal: team.IsPersonal,
	}, pgUUID(ownerID))
	if err != nil {
		return Team{}, fmt.Errorf("teams: create team: %w", err)
	}
	return teamFromRow(row), nil
}

// GetTeam implements Repository.
func (r *storeRepository) GetTeam(ctx context.Context, teamID uuid.UUID) (Team, error) {
	row, err := r.store.GetTeam(ctx, pgUUID(teamID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, fmt.Errorf("teams: get team: %w", err)
	}
	return teamFromRow(row), nil
}

// GetTeamForUser implements Repository.
func (r *storeRepository) GetTeamForUser(ctx context.Context, teamID, userID uuid.UUID) (Team, error) {
	row, err := r.store.GetTeamForUser(ctx, sqlc.GetTeamForUserParams{
		ID:     pgUUID(teamID),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, fmt.Errorf("teams: get team for user: %w", err)
	}
	return Team{
		ID:         uuidFromPG(row.ID),
		Name:       row.Name,
		IsPersonal: row.IsPersonal,
		Role:       Role(row.MemberRole),
		CreatedAt:  timeFromPG(row.CreatedAt),
		UpdatedAt:  timeFromPG(row.UpdatedAt),
	}, nil
}

// ListTeamsByUser implements Repository.
func (r *storeRepository) ListTeamsByUser(ctx context.Context, userID uuid.UUID) ([]Team, error) {
	rows, err := r.store.ListTeamsByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("teams: list teams: %w", err)
	}
	teams := make([]Team, 0, len(rows))
	for _, row := range rows {
		teams = append(teams, Team{
			ID:         uuidFromPG(row.ID),
			Name:       row.Name,
			IsPersonal: row.IsPersonal,
			Role:       Role(row.MemberRole),
			CreatedAt:  timeFromPG(row.CreatedAt),
			UpdatedAt:  timeFromPG(row.UpdatedAt),
		})
	}
	return teams, nil
}

// RenameTeam implements Repository.
func (r *storeRepository) RenameTeam(ctx context.Context, teamID uuid.UUID, name string) (Team, error) {
	row, err := r.store.UpdateTeam(ctx, sqlc.UpdateTeamParams{
		ID:   pgUUID(teamID),
		Name: name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, fmt.Errorf("teams: rename team: %w", err)
	}
	return teamFromRow(row), nil
}

// DeleteTeam implements Repository.
func (r *storeRepository) DeleteTeam(ctx context.Context, teamID uuid.UUID) error {
	if err := r.store.DeleteTeam(ctx, pgUUID(teamID)); err != nil {
		return fmt.Errorf("teams: delete team: %w", err)
	}
	return nil
}

// CreateMember implements Repository.
func (r *storeRepository) CreateMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (Member, error) {
	row, err := r.store.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: pgUUID(teamID),
		UserID: pgUUID(userID),
		Role:   string(role),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Member{}, ErrConflict
		}
		return Member{}, fmt.Errorf("teams: create member: %w", err)
	}
	return memberFromRow(row), nil
}

// GetMember implements Repository.
func (r *storeRepository) GetMember(ctx context.Context, teamID, userID uuid.UUID) (Member, error) {
	row, err := r.store.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
		TeamID: pgUUID(teamID),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}
		return Member{}, fmt.Errorf("teams: get member: %w", err)
	}
	return memberFromRow(row), nil
}

// ListMembers implements Repository.
func (r *storeRepository) ListMembers(ctx context.Context, teamID uuid.UUID) ([]Member, error) {
	rows, err := r.store.ListTeamMembers(ctx, pgUUID(teamID))
	if err != nil {
		return nil, fmt.Errorf("teams: list members: %w", err)
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{
			UserID:    uuidFromPG(row.UserID),
			Email:     row.Email,
			Role:      Role(row.Role),
			CreatedAt: timeFromPG(row.CreatedAt),
		})
	}
	return members, nil
}

// UpdateMemberRole implements Repository.
func (r *storeRepository) UpdateMemberRole(ctx context.Context, teamID, userID uuid.UUID, role Role) (Member, error) {
	row, err := r.store.UpdateTeamMemberRole(ctx, sqlc.UpdateTeamMemberRoleParams{
		TeamID: pgUUID(teamID),
		UserID: pgUUID(userID),
		Role:   string(role),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}
		return Member{}, fmt.Errorf("teams: update member role: %w", err)
	}
	return memberFromRow(row), nil
}

// DeleteMember implements Repository.
func (r *storeRepository) DeleteMember(ctx context.Context, teamID, userID uuid.UUID) error {
	if err := r.store.DeleteTeamMember(ctx, sqlc.DeleteTeamMemberParams{
		TeamID: pgUUID(teamID),
		UserID: pgUUID(userID),
	}); err != nil {
		return fmt.Errorf("teams: delete member: %w", err)
	}
	return nil
}

// CountOwners implements Repository.
func (r *storeRepository) CountOwners(ctx context.Context, teamID uuid.UUID) (int64, error) {
	count, err := r.store.CountTeamOwners(ctx, pgUUID(teamID))
	if err != nil {
		return 0, fmt.Errorf("teams: count owners: %w", err)
	}
	return count, nil
}

// CreateInvite implements Repository.
func (r *storeRepository) CreateInvite(ctx context.Context, invite Invite, tokenHash string) (Invite, error) {
	row, err := r.store.CreateInvite(ctx, sqlc.CreateInviteParams{
		TeamID:    pgUUID(invite.TeamID),
		Email:     invite.Email,
		Role:      string(invite.Role),
		TokenHash: tokenHash,
		InvitedBy: pgUUID(invite.InvitedBy),
		ExpiresAt: pgTimestamp(invite.ExpiresAt),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Invite{}, ErrConflict
		}
		return Invite{}, fmt.Errorf("teams: create invite: %w", err)
	}
	return inviteFromRow(row), nil
}

// ListInvites implements Repository.
func (r *storeRepository) ListInvites(ctx context.Context, teamID uuid.UUID) ([]Invite, error) {
	rows, err := r.store.ListInvitesByTeam(ctx, pgUUID(teamID))
	if err != nil {
		return nil, fmt.Errorf("teams: list invites: %w", err)
	}
	invites := make([]Invite, 0, len(rows))
	for _, row := range rows {
		invites = append(invites, inviteFromRow(row))
	}
	return invites, nil
}

// GetInviteByTokenHash implements Repository.
func (r *storeRepository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error) {
	row, err := r.store.GetInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invite{}, ErrNotFound
		}
		return Invite{}, fmt.Errorf("teams: get invite: %w", err)
	}
	return inviteFromRow(row), nil
}

// AcceptInvite implements Repository. The store call consumes the invite and
// inserts the membership in one transaction; a token that is unknown, expired
// or already consumed matches no row.
func (r *storeRepository) AcceptInvite(ctx context.Context, tokenHash string, userID uuid.UUID) (Invite, error) {
	row, err := r.store.AcceptInvite(ctx, tokenHash, pgUUID(userID))
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Invite{}, ErrInviteUsed
		case isUniqueViolation(err):
			return Invite{}, ErrConflict
		default:
			return Invite{}, fmt.Errorf("teams: accept invite: %w", err)
		}
	}
	return inviteFromRow(row), nil
}

// DeleteInvite implements Repository.
func (r *storeRepository) DeleteInvite(ctx context.Context, teamID, inviteID uuid.UUID) error {
	if err := r.store.DeleteInvite(ctx, sqlc.DeleteInviteParams{
		ID:     pgUUID(inviteID),
		TeamID: pgUUID(teamID),
	}); err != nil {
		return fmt.Errorf("teams: delete invite: %w", err)
	}
	return nil
}

// UserEmail implements Repository.
func (r *storeRepository) UserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	row, err := r.store.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("teams: get user email: %w", err)
	}
	return row.Email, nil
}

// teamFromRow maps a stored team row.
func teamFromRow(row sqlc.Team) Team {
	return Team{
		ID:         uuidFromPG(row.ID),
		Name:       row.Name,
		IsPersonal: row.IsPersonal,
		CreatedAt:  timeFromPG(row.CreatedAt),
		UpdatedAt:  timeFromPG(row.UpdatedAt),
	}
}

// memberFromRow maps a stored membership row (without the joined email).
func memberFromRow(row sqlc.TeamMember) Member {
	return Member{
		UserID:    uuidFromPG(row.UserID),
		Role:      Role(row.Role),
		CreatedAt: timeFromPG(row.CreatedAt),
	}
}

// inviteFromRow maps a stored invite row.
func inviteFromRow(row sqlc.Invite) Invite {
	invite := Invite{
		ID:        uuidFromPG(row.ID),
		TeamID:    uuidFromPG(row.TeamID),
		Email:     row.Email,
		Role:      Role(row.Role),
		InvitedBy: uuidFromPG(row.InvitedBy),
		ExpiresAt: timeFromPG(row.ExpiresAt),
		CreatedAt: timeFromPG(row.CreatedAt),
	}
	if row.AcceptedAt.Valid {
		accepted := row.AcceptedAt.Time
		invite.AcceptedAt = &accepted
	}
	return invite
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), raised by the membership primary key or the
// invite token hash.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// pgUUID converts a uuid.UUID for sqlc. The zero UUID becomes an invalid
// pgtype value, which serialises as NULL for nullable columns.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a sqlc UUID column to the domain type (NULL → zero).
func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

// timeFromPG converts a sqlc timestamp column to the domain type.
func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}

// pgTimestamp converts a time.Time for sqlc.
func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
