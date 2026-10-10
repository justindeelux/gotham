package instance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Section names used for audit rows and Repository.Save.
const (
	SectionGeneral = "general"
	SectionNetwork = "network"
	SectionSystem  = "system"
)

// Repository persists the settings record. It is implemented over
// *store.Store in production and by a fake in tests.
type Repository interface {
	// Load returns the stored record.
	Load(ctx context.Context) (Stored, error)
	// Save persists one section of rec and appends an audit row with the
	// given change description. actor is uuid.Nil for system actions.
	Save(ctx context.Context, actor uuid.UUID, section string, rec Stored, changes any) error
}

type storeRepository struct{ store *store.Store }

var _ Repository = (*storeRepository)(nil)

// NewStoreRepository builds the PostgreSQL-backed repository.
func NewStoreRepository(st *store.Store) Repository { return &storeRepository{store: st} }

func (r *storeRepository) Load(ctx context.Context) (Stored, error) {
	row, err := r.store.GetInstanceSettings(ctx)
	if err != nil {
		return Stored{}, fmt.Errorf("instance: load: %w", err)
	}
	rec := Stored{
		ControlPlaneURL: row.ControlPlaneUrl,
		InstanceName:    row.InstanceName,
		Timezone:        row.Timezone,
		Network: Network{
			DNSServers: row.DnsServers,
			IPv4:       IPConfig{Mode: row.Ipv4Mode, Address: row.Ipv4Address, Gateway: row.Ipv4Gateway},
			IPv6: IPv6Config{
				Enabled: row.Ipv6Enabled, Mode: row.Ipv6Mode,
				Address: row.Ipv6Address, Gateway: row.Ipv6Gateway,
			},
		},
		System: System{Hostname: row.Hostname, NTPEnabled: row.NtpEnabled, NTPServers: row.NtpServers},
	}
	if len(row.PendingNetwork) > 0 {
		var p Pending
		if err := json.Unmarshal(row.PendingNetwork, &p); err != nil {
			return Stored{}, fmt.Errorf("instance: decode pending: %w", err)
		}
		if row.PendingDeadline.Valid {
			p.Deadline = row.PendingDeadline.Time
		}
		rec.Pending = &p
	}
	return rec, nil
}

func (r *storeRepository) Save(ctx context.Context, actor uuid.UUID, section string, rec Stored, changes any) error {
	payload, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("instance: encode audit: %w", err)
	}
	audit := sqlc.InsertInstanceAuditParams{Section: section, Changes: payload}
	if actor != uuid.Nil {
		audit.ActorID = pgtype.UUID{Bytes: actor, Valid: true}
	}
	switch section {
	case SectionGeneral:
		err = r.store.UpdateInstanceGeneral(ctx, sqlc.UpdateInstanceGeneralParams{
			ControlPlaneUrl: rec.ControlPlaneURL, InstanceName: rec.InstanceName, Timezone: rec.Timezone,
		}, audit)
	case SectionSystem:
		err = r.store.UpdateInstanceSystem(ctx, sqlc.UpdateInstanceSystemParams{
			Hostname: rec.System.Hostname, NtpEnabled: rec.System.NTPEnabled, NtpServers: nonNil(rec.System.NTPServers),
		}, audit)
	case SectionNetwork:
		params := sqlc.UpdateInstanceNetworkParams{
			DnsServers:  nonNil(rec.Network.DNSServers),
			Ipv4Mode:    rec.Network.IPv4.Mode,
			Ipv4Address: rec.Network.IPv4.Address, Ipv4Gateway: rec.Network.IPv4.Gateway,
			Ipv6Enabled: rec.Network.IPv6.Enabled, Ipv6Mode: rec.Network.IPv6.Mode,
			Ipv6Address: rec.Network.IPv6.Address, Ipv6Gateway: rec.Network.IPv6.Gateway,
		}
		if rec.Pending != nil {
			if params.PendingNetwork, err = json.Marshal(rec.Pending); err != nil {
				return fmt.Errorf("instance: encode pending: %w", err)
			}
			params.PendingDeadline = pgtype.Timestamptz{Time: rec.Pending.Deadline, Valid: true}
		}
		err = r.store.UpdateInstanceNetwork(ctx, params, audit)
	default:
		return fmt.Errorf("instance: unknown section %q", section)
	}
	if err != nil {
		return fmt.Errorf("instance: save %s: %w", section, err)
	}
	return nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
