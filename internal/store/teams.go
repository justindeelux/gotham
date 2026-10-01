package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateUser inserts a user with the given email and password hash (nil when
// the account has no local password) and returns the stored row. Every account
// also gets its personal team with the user as owner, in the same transaction:
// a user without a personal team cannot use the API (teams row first, then the
// owner membership). The personal team's ID is the user's ID (see migration
// 00019), which is what lets RequireTeam resolve "no X-Team-Id header" without
// a lookup and what keeps old rows attributable to their creator.
func (s *Store) CreateUser(ctx context.Context, email string, passwordHash *string) (sqlc.User, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	user, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:         user.ID,
		Name:       personalTeamName(email),
		IsPersonal: true,
	}); err != nil {
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: user.ID,
		UserID: user.ID,
		Role:   "owner",
	}); err != nil {
		return sqlc.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, err
	}
	return user, nil
}

// firstUserLockKey is the advisory-lock key serializing first-account creation
// ("gotham" truncated to an int64). It is a constant, not a value, so every
// process uses the same lock.
const firstUserLockKey = int64(0x676f7468616d)

// ErrInstanceHasAccount reports that CreateFirstUser found the instance
// already populated. The caller maps it to the closed-registration answer.
var ErrInstanceHasAccount = errors.New("store: instance already has an account")

// CreateFirstUser inserts the bootstrap account (and its personal team) only
// while no account exists. The emptiness check and the insert share one
// transaction, so concurrent first registrations serialize: exactly one wins
// and the loser gets ErrInstanceHasAccount.
func (s *Store) CreateFirstUser(ctx context.Context, email string, passwordHash *string) (sqlc.User, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The emptiness check and the insert must serialize across transactions:
	// under READ COMMITTED two concurrent INSERT ... WHERE NOT EXISTS both see
	// an empty table and both insert. The advisory lock is held until commit,
	// so the loser re-evaluates NOT EXISTS against the winner's row and gets
	// zero rows (ErrInstanceHasAccount).
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", firstUserLockKey); err != nil {
		return sqlc.User{}, err
	}

	queries := s.queries.WithTx(tx)
	user, err := queries.CreateFirstUser(ctx, sqlc.CreateFirstUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, ErrInstanceHasAccount
		}
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:         user.ID,
		Name:       personalTeamName(email),
		IsPersonal: true,
	}); err != nil {
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: user.ID,
		UserID: user.ID,
		Role:   "owner",
	}); err != nil {
		return sqlc.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, err
	}
	return user, nil
}

// DeleteUserAndPersonalTeam removes an account created by a registration whose
// invite acceptance then failed. CreateUser writes the user, the personal team
// and the owner membership in one transaction, so the cleanup mirrors it: one
// transaction, else the instance keeps an ownerless team (teams has no foreign
// key to users).
func (s *Store) DeleteUserAndPersonalTeam(ctx context.Context, userID pgtype.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.DeleteTeamMember(ctx, sqlc.DeleteTeamMemberParams{TeamID: userID, UserID: userID}); err != nil {
		return err
	}
	if err := queries.DeleteTeam(ctx, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM users WHERE id = $1", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// personalTeamOrDefault resolves the team of a resource row that names none:
// the personal team of its creator, whose ID is the creator's user ID (see
// migration 00019). Service-layer code always names the active team; this
// fallback keeps pre-teams callers and repository tests writing to the
// creator's personal team instead of failing the NOT NULL constraint.
func personalTeamOrDefault(teamID, userID pgtype.UUID) pgtype.UUID {
	if teamID.Valid {
		return teamID
	}
	return userID
}

// personalTeamName is the display name of an account's personal team. The
// migration's backfill builds the same string in SQL.
func personalTeamName(email string) string {
	return email + "'s team"
}

// CreateTeamWithOwner stores a team and its owner membership in one
// transaction, so a team can never exist without an owner.
func (s *Store) CreateTeamWithOwner(ctx context.Context, params sqlc.CreateTeamParams, ownerID pgtype.UUID) (sqlc.Team, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Team{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	team, err := queries.CreateTeam(ctx, params)
	if err != nil {
		return sqlc.Team{}, err
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: team.ID,
		UserID: ownerID,
		Role:   "owner",
	}); err != nil {
		return sqlc.Team{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Team{}, err
	}
	return team, nil
}

// CreateTeam stores a team row and returns it.
func (s *Store) CreateTeam(ctx context.Context, params sqlc.CreateTeamParams) (sqlc.Team, error) {
	return s.queries.CreateTeam(ctx, params)
}

// GetTeam returns the team with the given ID.
func (s *Store) GetTeam(ctx context.Context, id pgtype.UUID) (sqlc.Team, error) {
	return s.queries.GetTeam(ctx, id)
}

// CountTeamResources reports how many resources (applications, databases,
// compose services, nodes) still belong to a team.
func (s *Store) CountTeamResources(ctx context.Context, id pgtype.UUID) (int64, error) {
	count, err := s.queries.CountTeamResources(ctx, id)
	if err != nil {
		return 0, err
	}
	return int64(count), nil
}

// MembershipWriter mutates memberships inside one MutateTeamMembership
// transaction. It is the locked seam the teams service policy runs on.
type MembershipWriter struct {
	ctx     context.Context
	queries *sqlc.Queries
	teamID  pgtype.UUID
}

// SetRole changes one membership's role inside the transaction.
func (w *MembershipWriter) SetRole(userID pgtype.UUID, role string) (sqlc.TeamMember, error) {
	return w.queries.UpdateTeamMemberRole(w.ctx, sqlc.UpdateTeamMemberRoleParams{
		TeamID: w.teamID,
		UserID: userID,
		Role:   role,
	})
}

// Remove deletes one membership inside the transaction.
func (w *MembershipWriter) Remove(userID pgtype.UUID) error {
	return w.queries.DeleteTeamMember(w.ctx, sqlc.DeleteTeamMemberParams{
		TeamID: w.teamID,
		UserID: userID,
	})
}

// MutateTeamMembership runs fn inside a transaction that locks the team row
// (SELECT ... FOR UPDATE), handing it the locked team and its current
// memberships. Two concurrent membership mutations of the same team therefore
// serialize, so a count-then-write decision (the last-owner check) cannot race.
// The transaction commits when fn returns nil and rolls back on any error;
// a missing team answers pgx.ErrNoRows.
func (s *Store) MutateTeamMembership(ctx context.Context, teamID pgtype.UUID, fn func(team sqlc.Team, members []sqlc.ListTeamMembersRow, writer *MembershipWriter) error) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	team, err := queries.GetTeamForUpdate(ctx, teamID)
	if err != nil {
		return err
	}
	members, err := queries.ListTeamMembers(ctx, teamID)
	if err != nil {
		return err
	}
	if err := fn(team, members, &MembershipWriter{ctx: ctx, queries: queries, teamID: teamID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetTeamForUser returns one team with the caller's role in it, or
// pgx.ErrNoRows when the caller is not a member.
func (s *Store) GetTeamForUser(ctx context.Context, params sqlc.GetTeamForUserParams) (sqlc.GetTeamForUserRow, error) {
	return s.queries.GetTeamForUser(ctx, params)
}

// ListTeamsByUser returns every team the user belongs to, each with the
// caller's role.
func (s *Store) ListTeamsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.ListTeamsByUserRow, error) {
	return s.queries.ListTeamsByUser(ctx, userID)
}

// UpdateTeam renames a team and returns the updated row.
func (s *Store) UpdateTeam(ctx context.Context, params sqlc.UpdateTeamParams) (sqlc.Team, error) {
	return s.queries.UpdateTeam(ctx, params)
}

// DeleteTeam removes a team row; memberships and invites cascade. Resource
// rows are protected by ON DELETE RESTRICT, so a team that still owns one
// cannot be removed here — use DeleteTeamIfEmpty for the guarded path.
func (s *Store) DeleteTeam(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteTeam(ctx, id)
}

// DeleteTeamIfEmpty removes a team that owns no resources or nodes, atomically:
// it locks the team row (SELECT ... FOR UPDATE), counts the owned rows and
// deletes inside one transaction. A concurrent resource insert takes a foreign
// key share lock on the same team row, so it either commits before the count
// (the delete is refused) or blocks until the team is gone and then fails its
// foreign key. It returns the owned row count when the delete was refused and 0
// when the team was removed. A missing team answers pgx.ErrNoRows.
func (s *Store) DeleteTeamIfEmpty(ctx context.Context, id pgtype.UUID) (int64, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if _, err := queries.GetTeamForUpdate(ctx, id); err != nil {
		return 0, err
	}
	count, err := queries.CountTeamResources(ctx, id)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return int64(count), nil
	}
	if err := queries.DeleteTeam(ctx, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return 0, nil
}

// CreateTeamMember adds one membership and returns it.
func (s *Store) CreateTeamMember(ctx context.Context, params sqlc.CreateTeamMemberParams) (sqlc.TeamMember, error) {
	return s.queries.CreateTeamMember(ctx, params)
}

// GetTeamMember returns one membership, or pgx.ErrNoRows when the user is not
// a member of the team.
func (s *Store) GetTeamMember(ctx context.Context, params sqlc.GetTeamMemberParams) (sqlc.TeamMember, error) {
	return s.queries.GetTeamMember(ctx, params)
}

// ListTeamMembers returns the team's members with their account email, oldest
// membership first.
func (s *Store) ListTeamMembers(ctx context.Context, teamID pgtype.UUID) ([]sqlc.ListTeamMembersRow, error) {
	return s.queries.ListTeamMembers(ctx, teamID)
}

// CountTeamOwners reports how many owners a team has; the last owner can never
// be removed or demoted.
func (s *Store) CountTeamOwners(ctx context.Context, teamID pgtype.UUID) (int64, error) {
	return s.queries.CountTeamOwners(ctx, teamID)
}

// CreateInvite stores an invite (the token is stored hashed) and returns it.
func (s *Store) CreateInvite(ctx context.Context, params sqlc.CreateInviteParams) (sqlc.Invite, error) {
	return s.queries.CreateInvite(ctx, params)
}

// ListInvitesByTeam returns a team's invites, newest first.
func (s *Store) ListInvitesByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Invite, error) {
	return s.queries.ListInvitesByTeam(ctx, teamID)
}

// GetInviteByTokenHash returns the invite behind a token hash, or
// pgx.ErrNoRows.
func (s *Store) GetInviteByTokenHash(ctx context.Context, tokenHash string) (sqlc.Invite, error) {
	return s.queries.GetInviteByTokenHash(ctx, tokenHash)
}

// AcceptInvite consumes a pending invite and inserts the membership in one
// transaction. The UPDATE only matches a pending, unexpired invite, so a
// replayed token finds no row (pgx.ErrNoRows) and never creates a second
// membership. A membership that already exists surfaces as a unique-constraint
// violation and rolls the consumption back.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash string, userID pgtype.UUID) (sqlc.Invite, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Invite{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	invite, err := queries.AcceptInvite(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Invite{}, err
		}
		return sqlc.Invite{}, fmt.Errorf("accept invite: %w", err)
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: invite.TeamID,
		UserID: userID,
		Role:   invite.Role,
	}); err != nil {
		return sqlc.Invite{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Invite{}, err
	}
	return invite, nil
}

// DeleteInvite removes one invite of a team.
func (s *Store) DeleteInvite(ctx context.Context, params sqlc.DeleteInviteParams) error {
	return s.queries.DeleteInvite(ctx, params)
}
