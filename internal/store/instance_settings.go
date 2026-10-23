package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetInstanceSettings(ctx context.Context) (sqlc.InstanceSetting, error) {
	return s.queries.GetInstanceSettings(ctx)
}

func (s *Store) UpdateInstanceGeneral(ctx context.Context, params sqlc.UpdateInstanceGeneralParams, audit sqlc.InsertInstanceAuditParams) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceGeneral(ctx, params); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}

func (s *Store) UpdateInstanceSystem(ctx context.Context, params sqlc.UpdateInstanceSystemParams, audit sqlc.InsertInstanceAuditParams) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceSystem(ctx, params); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}

func (s *Store) UpdateInstanceNetwork(ctx context.Context, params sqlc.UpdateInstanceNetworkParams, audit sqlc.InsertInstanceAuditParams) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceNetwork(ctx, params); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}
