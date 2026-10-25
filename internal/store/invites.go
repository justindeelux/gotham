package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrOwnerProtected refuses every member mutation aimed at the owner.
	ErrOwnerProtected = errors.New("store: the owner account is protected")
	// ErrUnknownProject refuses an invite or grant change for a missing project.
	ErrUnknownProject = errors.New("store: unknown project")
	// ErrInviteUnavailable covers unknown, expired and already consumed tokens,
	// so callers cannot tell a replay from a guess.
	ErrInviteUnavailable = errors.New("store: invite unavailable")
)

// InviteRow is a stored invite together with the projects it grants.
type InviteRow struct {
	ID         uuid.UUID
	Email      string
	TokenHash  string
	InvitedBy  *uuid.UUID
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
	ProjectIDs []uuid.UUID
}

const inviteColumns = `id, email, token_hash, invited_by, expires_at, accepted_at, created_at`

func scanInvite(row pgx.Row) (InviteRow, error) {
	var invite InviteRow
	err := row.Scan(&invite.ID, &invite.Email, &invite.TokenHash, &invite.InvitedBy, &invite.ExpiresAt,
		&invite.AcceptedAt, &invite.CreatedAt)
	return invite, err
}

// CreateInvite stores an invite and its project grants atomically. An unknown
// project fails the whole insert instead of silently shrinking the grant set.
func (s *Store) CreateInvite(ctx context.Context, email, tokenHash string, invitedBy uuid.UUID, expiresAt time.Time, projectIDs []uuid.UUID) (InviteRow, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return InviteRow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invite, err := scanInvite(tx.QueryRow(ctx,
		`INSERT INTO invites (email, token_hash, invited_by, expires_at) VALUES ($1,$2,$3,$4) RETURNING `+inviteColumns,
		email, tokenHash, invitedBy, expiresAt))
	if err != nil {
		return InviteRow{}, err
	}
	if len(projectIDs) > 0 {
		tag, err := tx.Exec(ctx, `INSERT INTO invite_projects (invite_id, project_id)
SELECT $1, p.id FROM projects p WHERE p.id=ANY($2::uuid[])`, invite.ID, projectIDs)
		if err != nil {
			return InviteRow{}, err
		}
		if tag.RowsAffected() != int64(len(projectIDs)) {
			return InviteRow{}, ErrUnknownProject
		}
	}
	invite.ProjectIDs = projectIDs
	if err := tx.Commit(ctx); err != nil {
		return InviteRow{}, err
	}
	return invite, nil
}

// ListInvites returns every invite, newest first, with its project grants.
func (s *Store) ListInvites(ctx context.Context) ([]InviteRow, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+inviteColumns+` FROM invites ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invites := make([]InviteRow, 0)
	index := make(map[uuid.UUID]int)
	for rows.Next() {
		row, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		index[row.ID] = len(invites)
		row.ProjectIDs = []uuid.UUID{}
		invites = append(invites, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.attachInviteProjects(ctx, invites, index)
}

func (s *Store) attachInviteProjects(ctx context.Context, invites []InviteRow, index map[uuid.UUID]int) ([]InviteRow, error) {
	rows, err := s.DB.Query(ctx, `SELECT ip.invite_id, ip.project_id FROM invite_projects ip ORDER BY ip.project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var inviteID, projectID uuid.UUID
		if err := rows.Scan(&inviteID, &projectID); err != nil {
			return nil, err
		}
		if at, ok := index[inviteID]; ok {
			invites[at].ProjectIDs = append(invites[at].ProjectIDs, projectID)
		}
	}
	return invites, rows.Err()
}

// InviteByTokenHash resolves a presented token's hash. Unknown tokens yield
// pgx.ErrNoRows so the caller can answer with one generic refusal.
func (s *Store) InviteByTokenHash(ctx context.Context, tokenHash string) (InviteRow, error) {
	invite, err := scanInvite(s.DB.QueryRow(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE token_hash=$1`, tokenHash))
	if err != nil {
		return InviteRow{}, err
	}
	rows, err := s.DB.Query(ctx, `SELECT project_id FROM invite_projects WHERE invite_id=$1 ORDER BY project_id`, invite.ID)
	if err != nil {
		return InviteRow{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			return InviteRow{}, err
		}
		invite.ProjectIDs = append(invite.ProjectIDs, projectID)
	}
	return invite, rows.Err()
}

func (s *Store) DeleteInvite(ctx context.Context, id uuid.UUID) (int64, error) {
	tag, err := s.DB.Exec(ctx, `DELETE FROM invites WHERE id=$1`, id)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ProjectName resolves display names for a set of project ids in one query.
func (s *Store) ProjectNames(ctx context.Context, ids []uuid.UUID) ([]ProjectName, error) {
	if len(ids) == 0 {
		return []ProjectName{}, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT id, name FROM projects WHERE id=ANY($1::uuid[]) ORDER BY name, id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]ProjectName, 0, len(ids))
	for rows.Next() {
		var name ProjectName
		if err := rows.Scan(&name.ID, &name.Name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ProjectName is one project id with its display name.
type ProjectName struct {
	ID   uuid.UUID
	Name string
}

// AcceptInvite consumes a pending, unexpired invite issued to the account's
// address and installs its project grants in the same transaction. A replayed,
// expired, unknown or mismatched invite leaves no rows and changes nothing.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash, email string, userID uuid.UUID) (InviteRow, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return InviteRow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invite, err := scanInvite(tx.QueryRow(ctx,
		`UPDATE invites SET accepted_at=now()
WHERE token_hash=$1 AND accepted_at IS NULL AND expires_at>now() AND lower(email)=lower($2)
RETURNING `+inviteColumns, tokenHash, email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InviteRow{}, ErrInviteUnavailable
		}
		return InviteRow{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, user_id, granted_by)
SELECT ip.project_id, $2, i.invited_by FROM invite_projects ip JOIN invites i ON i.id=ip.invite_id
WHERE ip.invite_id=$1
ON CONFLICT DO NOTHING`, invite.ID, userID); err != nil {
		return InviteRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InviteRow{}, err
	}
	return invite, nil
}
