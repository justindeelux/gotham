package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Account is one user row plus the projects granted to it. It never carries a
// password hash or a TOTP secret.
type Account struct {
	ID          uuid.UUID
	Email       string
	Role        string
	DisplayName *string
	CreatedAt   time.Time
	HasPassword bool
	ProjectIDs  []uuid.UUID
}

// ListAccounts returns every account and its project grants for the owner's
// member list.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, email, role, display_name, created_at, password_hash IS NOT NULL
FROM users ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]Account, 0)
	index := make(map[uuid.UUID]int)
	for rows.Next() {
		var account Account
		if err := rows.Scan(&account.ID, &account.Email, &account.Role, &account.DisplayName, &account.CreatedAt, &account.HasPassword); err != nil {
			return nil, err
		}
		account.ProjectIDs = []uuid.UUID{}
		index[account.ID] = len(accounts)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	grants, err := s.DB.Query(ctx, `SELECT user_id, project_id FROM project_members ORDER BY project_id`)
	if err != nil {
		return nil, err
	}
	defer grants.Close()
	for grants.Next() {
		var userID, projectID uuid.UUID
		if err := grants.Scan(&userID, &projectID); err != nil {
			return nil, err
		}
		if at, ok := index[userID]; ok {
			accounts[at].ProjectIDs = append(accounts[at].ProjectIDs, projectID)
		}
	}
	return accounts, grants.Err()
}

// lockMember resolves the target of a member mutation while holding its row,
// so the owner check and the change cannot race another owner action.
func lockMember(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgx.ErrNoRows
		}
		return err
	}
	if role != "member" {
		return ErrOwnerProtected
	}
	return nil
}

// memberResourceTables lists the tables whose rows survive the member but move
// to the owner: workloads, their history and backup destinations belong to the
// instance, not to the person who created them. Every other user_id reference
// is personal (sessions, API tokens, TOTP, grants, source providers, GitHub App
// connections) and is removed by the users cascade.
var memberResourceTables = []string{"applications", "databases", "services", "backup_targets"}

// DeleteMember removes a member account while keeping every resource they
// created. Sessions and API tokens are revoked, shared resources are handed to
// the owner, and the account is deleted in one transaction.
func (s *Store) DeleteMember(ctx context.Context, ownerID, userID uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockMember(ctx, tx, userID); err != nil {
		return err
	}
	// backup_targets are owner-level destinations, but a legacy row may still
	// carry a member's unique (user_id, name); rename a colliding one before
	// handing it over so the transfer cannot fail.
	if _, err := tx.Exec(ctx, `UPDATE backup_targets SET name = name || ' (' || left(id::text, 8) || ')'
WHERE user_id=$2 AND EXISTS (SELECT 1 FROM backup_targets o WHERE o.user_id=$1 AND o.name=backup_targets.name)`,
		ownerID, userID); err != nil {
		return err
	}
	for _, table := range memberResourceTables {
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET user_id=$1 WHERE user_id=$2`, ownerID, userID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResetMember drops every session and the two-factor configuration of a member
// and advances the credential version, so the member must sign in again and
// re-enrol a device. The owner is refused.
func (s *Store) ResetMember(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockMember(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET credential_version=credential_version+1, updated_at=now() WHERE id=$1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReplaceMemberProjects validates and replaces the entire grant set of one
// member atomically. The owner and unknown users are refused.
func (s *Store) ReplaceMemberProjects(ctx context.Context, ownerID, userID uuid.UUID, projectIDs []uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockMember(ctx, tx, userID); err != nil {
		return err
	}
	if len(projectIDs) > 0 {
		var known int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM projects WHERE id=ANY($1::uuid[])`, projectIDs).Scan(&known); err != nil {
			return err
		}
		if known != len(projectIDs) {
			return ErrUnknownProject
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE user_id=$1 AND NOT (project_id=ANY($2::uuid[]))`, userID, projectIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, user_id, granted_by)
SELECT p.id, $2, $3 FROM projects p WHERE p.id=ANY($1::uuid[])
ON CONFLICT DO NOTHING`, projectIDs, userID, ownerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
