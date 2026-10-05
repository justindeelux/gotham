package projects

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// StoreCounter implements ResourceCounter over the resource tables: it
// tallies the applications, services and databases attached to a project or
// an environment (Phase 13, PE-2). Previews count: they are rows that block
// a delete like any other workload.
type StoreCounter struct {
	Store *store.Store
}

// Compile-time guarantee.
var _ ResourceCounter = StoreCounter{}

// CountProjectResources implements ResourceCounter.
func (c StoreCounter) CountProjectResources(ctx context.Context, projectID uuid.UUID) (ResourceCounts, error) {
	applications, err := c.Store.CountApplicationsByProject(ctx, pgUUID(projectID))
	if err != nil {
		return ResourceCounts{}, fmt.Errorf("projects: count project applications: %w", err)
	}
	services, err := c.Store.CountServicesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return ResourceCounts{}, fmt.Errorf("projects: count project services: %w", err)
	}
	databases, err := c.Store.CountDatabasesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return ResourceCounts{}, fmt.Errorf("projects: count project databases: %w", err)
	}
	return ResourceCounts{
		Applications: int(applications),
		Services:     int(services),
		Databases:    int(databases),
	}, nil
}

// CountEnvironmentResources implements ResourceCounter.
func (c StoreCounter) CountEnvironmentResources(ctx context.Context, environmentID uuid.UUID) (ResourceCounts, error) {
	applications, err := c.Store.CountApplicationsByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return ResourceCounts{}, fmt.Errorf("projects: count environment applications: %w", err)
	}
	services, err := c.Store.CountServicesByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return ResourceCounts{}, fmt.Errorf("projects: count environment services: %w", err)
	}
	databases, err := c.Store.CountDatabasesByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return ResourceCounts{}, fmt.Errorf("projects: count environment databases: %w", err)
	}
	return ResourceCounts{
		Applications: int(applications),
		Services:     int(services),
		Databases:    int(databases),
	}, nil
}

// CountProjectPreviews implements ResourceCounter.
func (c StoreCounter) CountProjectPreviews(ctx context.Context, projectID uuid.UUID) (int, error) {
	count, err := c.Store.CountPreviewApplicationsByProject(ctx, pgUUID(projectID))
	if err != nil {
		return 0, fmt.Errorf("projects: count project previews: %w", err)
	}
	return int(count), nil
}

// CountEnvironmentPreviews implements ResourceCounter.
func (c StoreCounter) CountEnvironmentPreviews(ctx context.Context, environmentID uuid.UUID) (int, error) {
	count, err := c.Store.CountPreviewApplicationsByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return 0, fmt.Errorf("projects: count environment previews: %w", err)
	}
	return int(count), nil
}
