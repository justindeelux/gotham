package projects

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

type StoreCounter struct {
	Store *store.Store
}

var _ ResourceCounter = StoreCounter{}

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

func (c StoreCounter) CountProjectPreviews(ctx context.Context, projectID uuid.UUID) (int, error) {
	count, err := c.Store.CountPreviewApplicationsByProject(ctx, pgUUID(projectID))
	if err != nil {
		return 0, fmt.Errorf("projects: count project previews: %w", err)
	}
	return int(count), nil
}

func (c StoreCounter) CountEnvironmentPreviews(ctx context.Context, environmentID uuid.UUID) (int, error) {
	count, err := c.Store.CountPreviewApplicationsByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return 0, fmt.Errorf("projects: count environment previews: %w", err)
	}
	return int(count), nil
}
