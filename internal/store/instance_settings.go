package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Instance-settings persistence (JUS-92): the singleton settings row and its
// audit trail. The instance package owns validation and the audit payloads.

// GetInstanceSettings returns the singleton settings row.
func (s *Store) GetInstanceSettings(ctx context.Context) (sqlc.InstanceSetting, error) {
	return s.queries.GetInstanceSettings(ctx)
}

// UpdateInstanceGeneral stores the general section and audits it atomically.
func (s *Store) UpdateInstanceGeneral(ctx context.Context, params sqlc.UpdateInstanceGeneralParams, audit sqlc.InsertInstanceAuditParams) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceGeneral(ctx, params); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}

// UpdateInstanceSystem stores the system section and audits it atomically.
func (s *Store) UpdateInstanceSystem(ctx context.Context, params sqlc.UpdateInstanceSystemParams, audit sqlc.InsertInstanceAuditParams) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceSystem(ctx, params); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}

// UpdateInstanceNetwork stores the network section (and its pending marker)
// and audits it atomically.
func (s *Store) UpdateInstanceNetwork(ctx context.Context, params sqlc.UpdateInstanceNetworkParams, audit sqlc.InsertInstanceAuditParams) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceNetwork(ctx, params); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}
