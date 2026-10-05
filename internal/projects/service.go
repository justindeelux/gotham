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
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// ProductionEnvironment is the name of the environment created with every
// project.
const ProductionEnvironment = "production"

// Name policy: the API contract allows 1-64 characters per project and
// environment name, unique case-insensitively within the team or project.
// Descriptions are free text capped at 512 characters; the contract is
// silent on the cap, so over-long descriptions answer 400 instead of being
// silently truncated.
const (
	maxNameLength        = 64
	maxDescriptionLength = 512
)

// ResourceCounter reports how many workloads hang off a project or an
// environment. StoreCounter implements it over the resource tables.
type ResourceCounter interface {
	// CountProjectResources tallies the workloads of every environment of a
	// project.
	CountProjectResources(ctx context.Context, projectID uuid.UUID) (ResourceCounts, error)
	// CountEnvironmentResources tallies the workloads of one environment.
	CountEnvironmentResources(ctx context.Context, environmentID uuid.UUID) (ResourceCounts, error)
	// CountProjectPreviews tallies the live previews of every environment
	// of a project. Previews stay out of the display counts, but they
	// still block the project delete.
	CountProjectPreviews(ctx context.Context, projectID uuid.UUID) (int, error)
	// CountEnvironmentPreviews tallies the live previews of one
	// environment (see above).
	CountEnvironmentPreviews(ctx context.Context, environmentID uuid.UUID) (int, error)
}

// ProjectService is the control-plane surface the HTTP layer depends on. It
// is implemented by Service and (in tests) by fakes.
type ProjectService interface {
	// ListProjects returns the active team's projects, newest first.
	ListProjects(ctx context.Context, userID uuid.UUID) ([]Project, error)
	// CreateProject stores a project with its production environment in one
	// transaction (owner/admin).
	CreateProject(ctx context.Context, userID uuid.UUID, name, description string) (Project, Environment, error)
	// GetProject returns one project of the active team with its
	// environments (404 for others).
	GetProject(ctx context.Context, userID, projectID uuid.UUID) (Project, []Environment, error)
	// UpdateProject applies a rename and/or description change
	// (owner/admin). A nil field leaves the stored value unchanged.
	UpdateProject(ctx context.Context, userID, projectID uuid.UUID, name, description *string) (Project, error)
	// DeleteProject removes a project and its environments (owner/admin).
	// A project that still holds resources is refused with
	// ErrProjectNotEmpty.
	DeleteProject(ctx context.Context, userID, projectID uuid.UUID) error
	// ListEnvironments returns one project's environments, oldest first.
	ListEnvironments(ctx context.Context, userID, projectID uuid.UUID) ([]Environment, error)
	// CreateEnvironment stores an environment of a project (owner/admin).
	CreateEnvironment(ctx context.Context, userID, projectID uuid.UUID, name string) (Environment, error)
	// UpdateEnvironment renames one environment of the active team
	// (owner/admin).
	UpdateEnvironment(ctx context.Context, userID, environmentID uuid.UUID, name string) (Environment, error)
	// DeleteEnvironment removes one environment of the active team
	// (owner/admin). The project's last environment cannot be deleted, and
	// an environment that still holds resources is refused.
	DeleteEnvironment(ctx context.Context, userID, environmentID uuid.UUID) error
	// GetEnvironmentResources returns one environment of the active team with
	// its project and workloads (404 for others). Previews are included only
	// when includePreviews is true.
	GetEnvironmentResources(ctx context.Context, userID, environmentID uuid.UUID, includePreviews bool) (EnvironmentResources, error)
}

// ApplicationLister lists the applications of one environment for the
// resources surface. It is satisfied by the deploy service.
type ApplicationLister interface {
	ListApplications(ctx context.Context, userID uuid.UUID, filter deploy.ApplicationFilter) ([]deploy.Application, error)
}

// ServiceLister lists the services of one environment for the resources
// surface. It is satisfied by the services service.
type ServiceLister interface {
	List(ctx context.Context, userID uuid.UUID, filter services.ServiceFilter) ([]services.Service, error)
}

// DatabaseLister lists the databases of one environment for the resources
// surface. It is satisfied by the databases service.
type DatabaseLister interface {
	List(ctx context.Context, userID uuid.UUID, filter databases.DatabaseFilter) ([]databases.Database, error)
}

// Config wires a Service. Store (or an explicit Repository) backs
// persistence; Counter is required and has no default, so PE-2 cannot forget
// to wire the real resource counter.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is
	// set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Counter tallies workloads per project and environment. Required: the
	// server wires StoreCounter.
	Counter ResourceCounter
	// Applications lists an environment's applications for the resources
	// surface. Nil answers it with a clear error instead of panicking.
	Applications ApplicationLister
	// Services lists an environment's services for the resources surface.
	Services ServiceLister
	// Databases lists an environment's databases for the resources surface.
	Databases DatabaseLister
	// Logger defaults to slog.Default.
	Logger *slog.Logger
}

// repository resolves the configured repository implementation.
func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

// Service is the projects domain service: it owns project and environment
// lifecycle rules. Team isolation is enforced here, on the team each call
// resolves, so the rules hold for every caller — the route middlewares only
// pre-filter. It is safe for concurrent use.
type Service struct {
	repo         Repository
	counter      ResourceCounter
	applications ApplicationLister
	services     ServiceLister
	databases    DatabaseLister
	logger       *slog.Logger
}

// Compile-time guarantee that Service satisfies the route-level contract.
var _ ProjectService = (*Service)(nil)

// NewService builds a Service from cfg. It panics when Counter is nil: the
// counter is a required constructor argument, so a missing wire (PE-2's real
// counter) fails fast at startup instead of silently reporting zero
// resources.
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
		logger:       loggerOr(cfg.Logger),
	}
}

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil (a nil ProjectService) when there is no database, so callers
// can pass its result to Mount unconditionally.
func NewDefaultService(cfg Config) ProjectService {
	if cfg.repository() == nil {
		return nil
	}
	return NewService(cfg)
}

// ListProjects implements ProjectService.
func (s *Service) ListProjects(ctx context.Context, userID uuid.UUID) ([]Project, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	rows, err := s.repo.ListProjects(ctx, teamIDFor(ctx, userID))
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

// CreateProject implements ProjectService: the project and its production
// environment commit in one repository transaction.
func (s *Service) CreateProject(ctx context.Context, userID uuid.UUID, name, description string) (Project, Environment, error) {
	if err := s.ready(); err != nil {
		return Project{}, Environment{}, err
	}
	if userID == uuid.Nil {
		return Project{}, Environment{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return Project{}, Environment{}, err
	}
	clean, err := validateName(name)
	if err != nil {
		return Project{}, Environment{}, err
	}
	cleanDescription, err := validateDescription(description)
	if err != nil {
		return Project{}, Environment{}, err
	}
	project, environment, err := s.repo.CreateProject(ctx, teamIDFor(ctx, userID), clean, cleanDescription)
	if err != nil {
		return Project{}, Environment{}, err
	}
	withCounts, err := s.withProjectCounts(ctx, project, withEnvironments(1))
	if err != nil {
		return Project{}, Environment{}, err
	}
	return withCounts, environment, nil
}

// GetProject implements ProjectService.
func (s *Service) GetProject(ctx context.Context, userID, projectID uuid.UUID) (Project, []Environment, error) {
	if err := s.ready(); err != nil {
		return Project{}, nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return Project{}, nil, ErrNotFound
	}
	teamID := teamIDFor(ctx, userID)
	project, err := s.repo.GetProject(ctx, teamID, projectID)
	if err != nil {
		return Project{}, nil, err
	}
	rows, err := s.repo.ListEnvironments(ctx, teamID, projectID)
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

// UpdateProject implements ProjectService.
func (s *Service) UpdateProject(ctx context.Context, userID, projectID uuid.UUID, name, description *string) (Project, error) {
	if err := s.ready(); err != nil {
		return Project{}, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return Project{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return Project{}, err
	}
	teamID := teamIDFor(ctx, userID)
	existing, err := s.repo.GetProject(ctx, teamID, projectID)
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
	updated, err := s.repo.UpdateProject(ctx, teamID, projectID, nextName, nextDescription)
	if err != nil {
		return Project{}, err
	}
	return s.withProjectCounts(ctx, updated)
}

// DeleteProject implements ProjectService: a project that still holds
// resources is refused (409); otherwise its environments cascade. A
// racing PE-2 resource insert that commits past the check trips the
// repository's RESTRICT mapping to the same error.
func (s *Service) DeleteProject(ctx context.Context, userID, projectID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return err
	}
	teamID := teamIDFor(ctx, userID)
	if _, err := s.repo.GetProject(ctx, teamID, projectID); err != nil {
		return err
	}
	counts, err := s.counter.CountProjectResources(ctx, projectID)
	if err != nil {
		return err
	}
	if counts.Applications+counts.Services+counts.Databases > 0 {
		return ErrProjectNotEmpty
	}
	// Previews stay out of the display counts but still block the delete.
	previews, err := s.counter.CountProjectPreviews(ctx, projectID)
	if err != nil {
		return err
	}
	if previews > 0 {
		return ErrProjectNotEmpty
	}
	volumes, err := s.repo.DeleteProject(ctx, teamID, projectID)
	if err != nil {
		return err
	}
	s.logOrphanVolumes(projectID, volumes)
	return nil
}

// ListEnvironments implements ProjectService.
func (s *Service) ListEnvironments(ctx context.Context, userID, projectID uuid.UUID) ([]Environment, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return nil, ErrNotFound
	}
	rows, err := s.repo.ListEnvironments(ctx, teamIDFor(ctx, userID), projectID)
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

// CreateEnvironment implements ProjectService.
func (s *Service) CreateEnvironment(ctx context.Context, userID, projectID uuid.UUID, name string) (Environment, error) {
	if err := s.ready(); err != nil {
		return Environment{}, err
	}
	if userID == uuid.Nil || projectID == uuid.Nil {
		return Environment{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return Environment{}, err
	}
	clean, err := validateName(name)
	if err != nil {
		return Environment{}, err
	}
	environment, err := s.repo.CreateEnvironment(ctx, teamIDFor(ctx, userID), projectID, clean)
	if err != nil {
		return Environment{}, err
	}
	return s.withEnvironmentCounts(ctx, environment)
}

// UpdateEnvironment implements ProjectService.
func (s *Service) UpdateEnvironment(ctx context.Context, userID, environmentID uuid.UUID, name string) (Environment, error) {
	if err := s.ready(); err != nil {
		return Environment{}, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return Environment{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return Environment{}, err
	}
	clean, err := validateName(name)
	if err != nil {
		return Environment{}, err
	}
	environment, err := s.repo.UpdateEnvironment(ctx, teamIDFor(ctx, userID), environmentID, clean)
	if err != nil {
		return Environment{}, err
	}
	return s.withEnvironmentCounts(ctx, environment)
}

// DeleteEnvironment implements ProjectService: the project's last
// environment cannot be deleted, and an environment that still holds
// resources is refused. The survivor check and the delete run in one
// repository transaction that locks the parent project row; the pre-checks
// below only preserve the refusal order (last, then resources) in the
// uncontended case, and a racing resource insert trips the repository's
// RESTRICT mapping to the same 409.
func (s *Service) DeleteEnvironment(ctx context.Context, userID, environmentID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return err
	}
	teamID := teamIDFor(ctx, userID)
	environment, err := s.repo.GetEnvironment(ctx, teamID, environmentID)
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
	// Previews stay out of the display counts but still block the delete.
	previews, err := s.counter.CountEnvironmentPreviews(ctx, environmentID)
	if err != nil {
		return err
	}
	if previews > 0 {
		return ErrEnvironmentNotEmpty
	}
	volumes, err := s.repo.DeleteEnvironmentIfNotLast(ctx, teamID, environmentID)
	if err != nil {
		return err
	}
	s.logOrphanVolumes(environmentID, volumes)
	return nil
}

// logOrphanVolumes warns about database volumes a tombstone purge left on
// their nodes: the rows are gone, so nothing will ever remove them.
func (s *Service) logOrphanVolumes(scopeID uuid.UUID, volumes []string) {
	for _, volume := range volumes {
		if volume == "" {
			continue
		}
		s.logger.Warn("projects: purged database tombstone left its volume on the node",
			"scope_id", scopeID, "volume", volume)
	}
}

// GetEnvironmentResources implements ProjectService: one environment of the
// active team with its project and workloads. A foreign environment answers
// ErrNotFound, so IDs cannot be probed across teams. Viewers may read.
func (s *Service) GetEnvironmentResources(ctx context.Context, userID, environmentID uuid.UUID, includePreviews bool) (EnvironmentResources, error) {
	if err := s.ready(); err != nil {
		return EnvironmentResources{}, err
	}
	if userID == uuid.Nil || environmentID == uuid.Nil {
		return EnvironmentResources{}, ErrNotFound
	}
	teamID := teamIDFor(ctx, userID)
	environment, err := s.repo.GetEnvironment(ctx, teamID, environmentID)
	if err != nil {
		return EnvironmentResources{}, err
	}
	project, err := s.repo.GetProject(ctx, teamID, environment.ProjectID)
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

// withProjectCounts attaches the environment and resource counts to a
// project. opts lets callers that already hold one side skip its read.
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

// withEnvironmentCounts attaches the resource counts to an environment.
func (s *Service) withEnvironmentCounts(ctx context.Context, environment Environment) (Environment, error) {
	counts, err := s.counter.CountEnvironmentResources(ctx, environment.ID)
	if err != nil {
		return Environment{}, err
	}
	environment.Resources = counts
	return environment, nil
}

// countOptions carries an already-known environment count past its recount.
type countOptions struct {
	environments *int
}

// countOption overrides one count read.
type countOption func(*countOptions)

// withEnvironments skips the environment-count read.
func withEnvironments(count int) countOption {
	return func(o *countOptions) { o.environments = &count }
}

// ready reports a service built without its required dependencies.
func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("projects: repository is not configured")
	}
	return nil
}

// teamIDFor resolves the team a project call operates in: the request's
// active team, or the caller's personal team when no team context is present
// (the pre-teams path), matching every other resource surface.
func teamIDFor(ctx context.Context, userID uuid.UUID) uuid.UUID {
	scope := teams.ScopeFor(ctx, userID)
	if scope.Active() {
		return scope.TeamID
	}
	return teams.PersonalTeamID(userID)
}

// authorizeWrite enforces the caller's team role: a read_only member can
// read but never mutate. Without a team context (non-HTTP callers, tests)
// the pre-teams behavior applies.
func authorizeWrite(ctx context.Context, userID uuid.UUID) error {
	if !teams.ScopeFor(ctx, userID).CanWrite() {
		return ErrForbidden
	}
	return nil
}

// validateName trims and bounds a project or environment name: 1-64
// characters after trimming, per the API contract. The count is in runes,
// so multi-byte names are measured the way clients count them.
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

// validateDescription bounds a project description.
func validateDescription(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > maxDescriptionLength {
		return "", fmt.Errorf("%w: description must be at most %d characters", ErrValidation, maxDescriptionLength)
	}
	return raw, nil
}

// loggerOr returns logger, or the process default.
func loggerOr(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
