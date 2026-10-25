package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ResourceProject resolves persisted relationships, never IDs supplied by a client.
func (s *Store) ResourceProject(ctx context.Context, kind string, id uuid.UUID) (uuid.UUID, error) {
	queries := map[string]string{
		"project":     `SELECT id FROM projects WHERE id=$1`,
		"environment": `SELECT project_id FROM environments WHERE id=$1`,
		"application": `SELECT e.project_id FROM applications r JOIN environments e ON e.id=r.environment_id WHERE r.id=$1`,
		"database":    `SELECT e.project_id FROM databases r JOIN environments e ON e.id=r.environment_id WHERE r.id=$1 AND r.deleted_at IS NULL`,
		"service":     `SELECT e.project_id FROM services r JOIN environments e ON e.id=r.environment_id WHERE r.id=$1 AND r.deleted_at IS NULL`,
		"deployment":  `SELECT e.project_id FROM deployments d JOIN applications a ON a.id=d.application_id JOIN environments e ON e.id=a.environment_id WHERE d.id=$1`,
		"backup":      `SELECT e.project_id FROM backups b JOIN databases d ON d.id=b.database_id JOIN environments e ON e.id=d.environment_id WHERE b.id=$1`,
		"schedule":    `SELECT e.project_id FROM backup_schedules b JOIN databases d ON d.id=b.database_id JOIN environments e ON e.id=d.environment_id WHERE b.id=$1`,
		"channel": `SELECT e.project_id
 FROM notification_channels n
 JOIN applications a ON n.resource_type='application' AND a.id=n.resource_id
 JOIN environments e ON e.id=a.environment_id
 WHERE n.id=$1

 UNION ALL SELECT e.project_id
 FROM notification_channels n
 JOIN databases d ON n.resource_type='database' AND d.id=n.resource_id
 JOIN environments e ON e.id=d.environment_id
 WHERE n.id=$1`,
	}
	query, ok := queries[kind]
	if !ok || id == uuid.Nil {
		return uuid.Nil, pgx.ErrNoRows
	}
	var project uuid.UUID
	err := s.DB.QueryRow(ctx, query, id).Scan(&project)
	return project, err
}

func (s *Store) HasProjectGrant(ctx context.Context, userID, projectID uuid.UUID) (bool, error) {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_members WHERE user_id=$1 AND project_id=$2)`, userID, projectID).Scan(&allowed)
	return allowed, err
}

type ProjectMember struct {
	UserID    uuid.UUID  `json:"user_id"`
	GrantedBy *uuid.UUID `json:"granted_by"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s *Store) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]ProjectMember, error) {
	rows, err := s.DB.Query(ctx, `SELECT user_id, granted_by, created_at FROM project_members WHERE project_id=$1 ORDER BY created_at, user_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ProjectMember, 0)
	for rows.Next() {
		var member ProjectMember
		if err := rows.Scan(&member.UserID, &member.GrantedBy, &member.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

func (s *Store) GrantProject(ctx context.Context, projectID, userID, ownerID uuid.UUID) error {
	result, err := s.DB.Exec(ctx, `INSERT INTO project_members(project_id,user_id,granted_by) SELECT p.id,u.id,$3
 FROM projects p CROSS
 JOIN users u
 WHERE p.id=$1 AND u.id=$2 AND u.role='member'
 ON CONFLICT (project_id,user_id) DO NOTHING`, projectID, userID, ownerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		allowed, err := s.HasProjectGrant(ctx, userID, projectID)
		if err != nil {
			return err
		}
		if !allowed {
			return pgx.ErrNoRows
		}
	}
	return nil
}

func (s *Store) RevokeProject(ctx context.Context, projectID, userID uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	return err
}

// ReplaceProjectMembers validates and replaces the entire grant set atomically.
func (s *Store) ReplaceProjectMembers(ctx context.Context, projectID, ownerID uuid.UUID, users []uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1 FOR UPDATE`, projectID).Scan(&existing); err != nil {
		return err
	}
	for _, userID := range users {
		var member uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND role='member' FOR KEY SHARE`, userID).Scan(&member); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND NOT (user_id=ANY($2::uuid[]))`, projectID, users); err != nil {
		return err
	}
	for _, userID := range users {
		if _, err := tx.Exec(ctx, `INSERT INTO project_members(project_id,user_id,granted_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, projectID, userID, ownerID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GrantedResourceIDs filters a whole collection in one query before serialization.
func (s *Store) GrantedResourceIDs(ctx context.Context, userID uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	allowed := make(map[uuid.UUID]bool)
	if len(ids) == 0 {
		return allowed, nil
	}
	relations := map[string]string{
		"project":     `projects r JOIN project_members m ON m.project_id=r.id`,
		"application": `applications r JOIN environments e ON e.id=r.environment_id JOIN project_members m ON m.project_id=e.project_id`,
		"database":    `databases r JOIN environments e ON e.id=r.environment_id JOIN project_members m ON m.project_id=e.project_id`,
		"service":     `services r JOIN environments e ON e.id=r.environment_id JOIN project_members m ON m.project_id=e.project_id`,
		"deployment":  `deployments r JOIN applications a ON a.id=r.application_id JOIN environments e ON e.id=a.environment_id JOIN project_members m ON m.project_id=e.project_id`,
		"channel":     `notification_channels r LEFT JOIN applications a ON r.resource_type='application' AND a.id=r.resource_id LEFT JOIN databases d ON r.resource_type='database' AND d.id=r.resource_id JOIN environments e ON e.id=COALESCE(a.environment_id,d.environment_id) JOIN project_members m ON m.project_id=e.project_id`,
	}
	relation, ok := relations[kind]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	// The relation comes exclusively from the fixed allowlist above.
	query := "SELECT r.id FROM " + relation + " WHERE m.user_id=$1 AND r.id=ANY($2::uuid[])"
	if kind == "database" || kind == "service" {
		query += " AND r.deleted_at IS NULL"
	}
	rows, err := s.DB.Query(ctx, query, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		allowed[id] = true
	}
	return allowed, rows.Err()
}
