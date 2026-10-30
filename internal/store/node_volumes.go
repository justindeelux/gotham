package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// RecordedVolumeOwners is every recorded resource that can own a Docker
// volume. The orphaned-volume view subtracts this set from what the node
// reports.
type RecordedVolumeOwners struct {
	Applications []uuid.UUID
	Services     []uuid.UUID
	Databases    []string
}

func (s *Store) ListRecordedVolumeOwners(ctx context.Context) (RecordedVolumeOwners, error) {
	applications, err := s.queries.ListApplicationIDs(ctx)
	if err != nil {
		return RecordedVolumeOwners{}, err
	}
	services, err := s.queries.ListServiceIDs(ctx)
	if err != nil {
		return RecordedVolumeOwners{}, err
	}
	databases, err := s.queries.ListDatabaseVolumeNames(ctx)
	if err != nil {
		return RecordedVolumeOwners{}, err
	}
	owners := RecordedVolumeOwners{
		Applications: make([]uuid.UUID, 0, len(applications)),
		Services:     make([]uuid.UUID, 0, len(services)),
		Databases:    make([]string, 0, len(databases)),
	}
	for _, id := range applications {
		owners.Applications = append(owners.Applications, uuid.UUID(id.Bytes))
	}
	for _, id := range services {
		owners.Services = append(owners.Services, uuid.UUID(id.Bytes))
	}
	for _, name := range databases {
		if name = strings.TrimSpace(name); name != "" {
			owners.Databases = append(owners.Databases, name)
		}
	}
	return owners, nil
}
