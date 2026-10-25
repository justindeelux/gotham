package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
)

const ProductionEnvironment = "production"

const (
	maxNameLength        = 64
	maxDescriptionLength = 512
)

type ResourceCounter interface {
	CountProjectResources(ctx context.Context, projectID uuid.UUID) (ResourceCounts, error)
	CountEnvironmentResources(ctx context.Context, environmentID uuid.UUID) (ResourceCounts, error)
	CountProjectPreviews(ctx context.Context, projectID uuid.UUID) (int, error)
	CountEnvironmentPreviews(ctx context.Context, environmentID uuid.UUID) (int, error)
}

type ProjectService interface {
	ListProjects(ctx context.Context, userID uuid.UUID) ([]Project, error)
	CreateProject(ctx context.Context, userID uuid.UUID, name, description string) (Project, Environment, error)
	GetProject(ctx context.Context, userID, projectID uuid.UUID) (Project, []Environment, error)
	UpdateProject(ctx context.Context, userID, projectID uuid.UUID, name, description *string) (Project, error)
	DeleteProject(ctx context.Context, userID, projectID uuid.UUID) error
	ListEnvironments(ctx context.Context, userID, projectID uuid.UUID) ([]Environment, error)
	CreateEnvironment(ctx context.Context, userID, projectID uuid.UUID, name string) (Environment, error)
	UpdateEnvironment(ctx context.Context, userID, environmentID uuid.UUID, name string) (Environment, error)
	DeleteEnvironment(ctx context.Context, userID, environmentID uuid.UUID) error
	GetEnvironmentResources(ctx context.Context, userID, environmentID uuid.UUID, includePreviews bool) (EnvironmentResources, error)
	GetProjectVariables(ctx context.Context, userID, projectID uuid.UUID) ([]Variable, error)
	ReplaceProjectVariables(ctx context.Context, userID, projectID uuid.UUID, inputs []VariableInput) ([]Variable, error)
	GetEnvironmentVariables(ctx context.Context, userID, environmentID uuid.UUID) ([]Variable, error)
	ReplaceEnvironmentVariables(ctx context.Context, userID, environmentID uuid.UUID, inputs []VariableInput) ([]Variable, error)
	PreviewBases(ctx context.Context, userID uuid.UUID, previewAppIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
}

type ApplicationLister interface {
	ListApplications(ctx context.Context, userID uuid.UUID, filter deploy.ApplicationFilter) ([]deploy.Application, error)
}

type ServiceLister interface {
	List(ctx context.Context, userID uuid.UUID, filter services.ServiceFilter) ([]services.Service, error)
}

type DatabaseLister interface {
	List(ctx context.Context, userID uuid.UUID, filter databases.DatabaseFilter) ([]databases.Database, error)
}

type Config struct {
	Store        *store.Store
	Repository   Repository
	Counter      ResourceCounter
	Applications ApplicationLister
	Services     ServiceLister
	Databases    DatabaseLister
	Secret       string
	Logger       *slog.Logger
}

func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

type Service struct {
	repo         Repository
	counter      ResourceCounter
	applications ApplicationLister
	services     ServiceLister
	databases    DatabaseLister
	secret       string
	logger       *slog.Logger
}

var _ ProjectService = (*Service)(nil)

func NewService(cfg Config) *Service {
	if cfg.Counter == nil {
		panic("projects: ResourceCounter is required (wire StoreCounter)")
	}
	return &Service{
		repo:         cfg.repository(),
		counter:      cfg.Counter,
		applications: cfg.Applications,
		services:     cfg.Services,
		databases:    cfg.Databases,
		secret:       cfg.Secret,
		logger:       loggerOr(cfg.Logger),
	}
}

func NewDefaultService(cfg Config) ProjectService {
	if cfg.repository() == nil {
		return nil
	}
	return NewService(cfg)
}

func (s *Service) ListProjects(ctx context.Context, userID uuid.UUID) ([]Project, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	rows, err := s.repo.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	projects := make([]Project, 0, len(rows))
	for _, row := range rows {
		withCounts, err := s.withProjectCounts(ctx, row)
		if err != nil {
			return nil, err
		}
		projects = append(projects, withCounts)
	}
	return projects, nil
}

func (s *Service) CreateProject(ctx context.Context, userID uuid.UUID, name, description string) (Project, Environment, error) {
	if err := s.ready(); err != nil {
		return Project{}, Environment{}, err
	}
	if userID == uuid.Nil {
		return Project{}, Environment{}, ErrNotFound
	}
	clean, err := validateName(name)
	if err != nil {
		return Project{}, Environment{}, err
	}
	cleanDescription, err := validateDescription(description)
	if err != nil {
		return Project{}, Environment{}, err
	}
	if err := requestreplay.Fingerprint(ctx, "", []string{clean, cleanDescription}); err != nil {
		return Project{}, Environment{}, err
	}
	project, environment, err := s.repo.CreateProject(ctx, clean, cleanDescription)
	if err != nil {
		return Project{}, Environment{}, err
	}
	withCounts, err := s.withProjectCounts(ctx, project, withEnvironments(1))
	if err != nil {
		return Project{}, Environment{}, err
	}
	return withCounts, environment, nil
}

func (s *Service) GetProject(ctx context.Context, userID, projectID uuid.UUID) (Project, []Environment, error) {
	if err := s.ready(); err != nil {
		return Project{}, nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return Project{}, nil, ErrNotFound
	}
	project, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return Project{}, nil, err
	}
	rows, err := s.repo.ListEnvironments(ctx, projectID)
	if err != nil {
		return Project{}, nil, err
	}
	environments := make([]Environment, 0, len(rows))
	for _, row := range rows {
		withCounts, err := s.withEnvironmentCounts(ctx, row)
		if err != nil {
			return Project{}, nil, err
		}
		environments = append(environments, withCounts)
	}
	withCounts, err := s.withProjectCounts(ctx, project, withEnvironments(len(environments)))
	if err != nil {
		return Project{}, nil, err
	}
	return withCounts, environments, nil
}

func (s *Service) UpdateProject(ctx context.Context, userID, projectID uuid.UUID, name, description *string) (Project, error) {
	if err := s.ready(); err != nil {
		return Project{}, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return Project{}, ErrNotFound
	}
	existing, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	nextName, nextDescription := existing.Name, existing.Description
	if name != nil {
		clean, err := validateName(*name)
		if err != nil {
			return Project{}, err
		}
		nextName = clean
	}
	if description != nil {
		cleanDescription, err := validateDescription(*description)
		if err != nil {
			return Project{}, err
		}
		nextDescription = cleanDescription
	}
	updated, err := s.repo.UpdateProject(ctx, projectID, nextName, nextDescription)
	if err != nil {
		return Project{}, err
	}
	return s.withProjectCounts(ctx, updated)
}

func (s *Service) DeleteProject(ctx context.Context, userID, projectID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return ErrNotFound
	}
	if _, err := s.repo.GetProject(ctx, projectID); err != nil {
		return err
	}
	counts, err := s.counter.CountProjectResources(ctx, projectID)
	if err != nil {
		return err
	}
	if counts.Applications+counts.Services+counts.Databases > 0 {
		return ErrProjectNotEmpty
	}
	previews, err := s.counter.CountProjectPreviews(ctx, projectID)
	if err != nil {
		return err
	}
	if previews > 0 {
		return ErrProjectNotEmpty
	}
	volumes, err := s.repo.DeleteProject(ctx, projectID)
	if err != nil {
		return err
	}
	s.logOrphanVolumes(projectID, volumes)
	return nil
}

func (s *Service) ListEnvironments(ctx context.Context, userID, projectID uuid.UUID) ([]Environment, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return nil, ErrNotFound
	}
	rows, err := s.repo.ListEnvironments(ctx, projectID)
	if err != nil {
		return nil, err
	}
	environments := make([]Environment, 0, len(rows))
	for _, row := range rows {
		withCounts, err := s.withEnvironmentCounts(ctx, row)
		if err != nil {
			return nil, err
		}
		environments = append(environments, withCounts)
	}
	return environments, nil
}

func (s *Service) CreateEnvironment(ctx context.Context, userID, projectID uuid.UUID, name string) (Environment, error) {
	if err := s.ready(); err != nil {
		return Environment{}, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return Environment{}, ErrNotFound
	}
	clean, err := validateName(name)
	if err != nil {
		return Environment{}, err
	}
	if err := requestreplay.Fingerprint(ctx, "", clean); err != nil {
		return Environment{}, err
	}
	environment, err := s.repo.CreateEnvironment(ctx, projectID, clean)
	if err != nil {
		return Environment{}, err
	}
	return s.withEnvironmentCounts(ctx, environment)
}

func (s *Service) UpdateEnvironment(ctx context.Context, userID, environmentID uuid.UUID, name string) (Environment, error) {
	if err := s.ready(); err != nil {
		return Environment{}, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return Environment{}, ErrNotFound
	}
	clean, err := validateName(name)
	if err != nil {
		return Environment{}, err
	}
	environment, err := s.repo.UpdateEnvironment(ctx, environmentID, clean)
	if err != nil {
		return Environment{}, err
	}
	return s.withEnvironmentCounts(ctx, environment)
}

func (s *Service) DeleteEnvironment(ctx context.Context, userID, environmentID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return ErrNotFound
	}
	environment, err := s.repo.GetEnvironment(ctx, environmentID)
	if err != nil {
		return err
	}
	remaining, err := s.repo.CountEnvironments(ctx, environment.ProjectID)
	if err != nil {
		return err
	}
	if remaining <= 1 {
		return ErrLastEnvironment
	}
	counts, err := s.counter.CountEnvironmentResources(ctx, environmentID)
	if err != nil {
		return err
	}
	if counts.Applications+counts.Services+counts.Databases > 0 {
		return ErrEnvironmentNotEmpty
	}
	previews, err := s.counter.CountEnvironmentPreviews(ctx, environmentID)
	if err != nil {
		return err
	}
	if previews > 0 {
		return ErrEnvironmentNotEmpty
	}
	volumes, err := s.repo.DeleteEnvironmentIfNotLast(ctx, environmentID)
	if err != nil {
		return err
	}
	s.logOrphanVolumes(environmentID, volumes)
	return nil
}

func (s *Service) logOrphanVolumes(scopeID uuid.UUID, volumes []string) {
	for _, volume := range volumes {
		if volume == "" {
			continue
		}
		s.logger.Warn("projects: purged database tombstone left its volume on the node",
			"scope_id", scopeID, "volume", volume)
	}
}

func (s *Service) GetEnvironmentResources(ctx context.Context, userID, environmentID uuid.UUID, includePreviews bool) (EnvironmentResources, error) {
	if err := s.ready(); err != nil {
		return EnvironmentResources{}, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return EnvironmentResources{}, ErrNotFound
	}
	environment, err := s.repo.GetEnvironment(ctx, environmentID)
	if err != nil {
		return EnvironmentResources{}, err
	}
	project, err := s.repo.GetProject(ctx, environment.ProjectID)
	if err != nil {
		return EnvironmentResources{}, err
	}
	if s.applications == nil || s.services == nil || s.databases == nil {
		return EnvironmentResources{}, errors.New("projects: resource listers are not configured")
	}
	apps, err := s.applications.ListApplications(ctx, userID, deploy.ApplicationFilter{EnvironmentID: environmentID, IncludePreviews: includePreviews})
	if err != nil {
		return EnvironmentResources{}, err
	}
	if !includePreviews {
		kept := apps[:0]
		for _, application := range apps {
			if !application.IsPreview {
				kept = append(kept, application)
			}
		}
		apps = kept
	}
	svcs, err := s.services.List(ctx, userID, services.ServiceFilter{EnvironmentID: environmentID})
	if err != nil {
		return EnvironmentResources{}, err
	}
	dbs, err := s.databases.List(ctx, userID, databases.DatabaseFilter{EnvironmentID: environmentID})
	if err != nil {
		return EnvironmentResources{}, err
	}
	withCounts, err := s.withEnvironmentCounts(ctx, environment)
	if err != nil {
		return EnvironmentResources{}, err
	}
	withProjectCounts, err := s.withProjectCounts(ctx, project)
	if err != nil {
		return EnvironmentResources{}, err
	}
	if apps == nil {
		apps = []deploy.Application{}
	}
	if svcs == nil {
		svcs = []services.Service{}
	}
	if dbs == nil {
		dbs = []databases.Database{}
	}
	return EnvironmentResources{
		Environment:  withCounts,
		Project:      withProjectCounts,
		Applications: apps,
		Services:     svcs,
		Databases:    dbs,
	}, nil
}

func (s *Service) PreviewBases(ctx context.Context, userID uuid.UUID, previewAppIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	return s.repo.PreviewBaseIDs(ctx, previewAppIDs)
}

func (s *Service) withProjectCounts(ctx context.Context, project Project, opts ...countOption) (Project, error) {
	applied := countOptions{}
	for _, opt := range opts {
		opt(&applied)
	}
	if applied.environments != nil {
		project.EnvironmentCount = *applied.environments
	} else {
		count, err := s.repo.CountEnvironments(ctx, project.ID)
		if err != nil {
			return Project{}, err
		}
		project.EnvironmentCount = count
	}
	counts, err := s.counter.CountProjectResources(ctx, project.ID)
	if err != nil {
		return Project{}, err
	}
	project.Resources = counts
	return project, nil
}

func (s *Service) withEnvironmentCounts(ctx context.Context, environment Environment) (Environment, error) {
	counts, err := s.counter.CountEnvironmentResources(ctx, environment.ID)
	if err != nil {
		return Environment{}, err
	}
	environment.Resources = counts
	return environment, nil
}

type countOptions struct {
	environments *int
}

type countOption func(*countOptions)

func withEnvironments(count int) countOption {
	return func(o *countOptions) { o.environments = &count }
}

func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("projects: repository is not configured")
	}
	return nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return "", fmt.Errorf("%w: name must be at most %d characters", ErrValidation, maxNameLength)
	}
	return name, nil
}

func validateDescription(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > maxDescriptionLength {
		return "", fmt.Errorf("%w: description must be at most %d characters", ErrValidation, maxDescriptionLength)
	}
	return raw, nil
}

func loggerOr(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
