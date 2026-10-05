package projects

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// discardLogger keeps the service's operational logging out of the test
// output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeRepository is an in-memory Repository that mimics the SQL semantics
// tests care about: team scoping, case-insensitive name uniqueness and
// project cascades.
type fakeRepository struct {
	mu           sync.Mutex
	projects     map[uuid.UUID]Project
	environments map[uuid.UUID]Environment
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		projects:     make(map[uuid.UUID]Project),
		environments: make(map[uuid.UUID]Environment),
	}
}

func (f *fakeRepository) CreateProject(_ context.Context, teamID uuid.UUID, name, description string) (Project, Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, project := range f.projects {
		if project.TeamID == teamID && equalFold(project.Name, name) {
			return Project{}, Environment{}, ErrProjectExists
		}
	}
	project := Project{ID: uuid.New(), TeamID: teamID, Name: name, Description: description}
	environment := Environment{ID: uuid.New(), ProjectID: project.ID, Name: ProductionEnvironment}
	f.projects[project.ID] = project
	f.environments[environment.ID] = environment
	return project, environment, nil
}

func (f *fakeRepository) ListProjects(_ context.Context, teamID uuid.UUID) ([]Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var projects []Project
	for _, project := range f.projects {
		if project.TeamID == teamID {
			projects = append(projects, project)
		}
	}
	if projects == nil {
		projects = []Project{}
	}
	return projects, nil
}

func (f *fakeRepository) GetProject(_ context.Context, teamID, projectID uuid.UUID) (Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok || project.TeamID != teamID {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (f *fakeRepository) UpdateProject(_ context.Context, teamID, projectID uuid.UUID, name, description string) (Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok || project.TeamID != teamID {
		return Project{}, ErrNotFound
	}
	for id, other := range f.projects {
		if id != projectID && other.TeamID == teamID && equalFold(other.Name, name) {
			return Project{}, ErrProjectExists
		}
	}
	project.Name, project.Description = name, description
	f.projects[projectID] = project
	return project, nil
}

func (f *fakeRepository) DeleteProject(_ context.Context, teamID, projectID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok || project.TeamID != teamID {
		return ErrNotFound
	}
	delete(f.projects, projectID)
	for id, environment := range f.environments {
		if environment.ProjectID == projectID {
			delete(f.environments, id)
		}
	}
	return nil
}

func (f *fakeRepository) CreateEnvironment(_ context.Context, teamID, projectID uuid.UUID, name string) (Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok || project.TeamID != teamID {
		return Environment{}, ErrNotFound
	}
	for _, environment := range f.environments {
		if environment.ProjectID == projectID && equalFold(environment.Name, name) {
			return Environment{}, ErrEnvironmentExists
		}
	}
	environment := Environment{ID: uuid.New(), ProjectID: projectID, Name: name}
	f.environments[environment.ID] = environment
	return environment, nil
}

func (f *fakeRepository) ListEnvironments(_ context.Context, teamID, projectID uuid.UUID) ([]Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok || project.TeamID != teamID {
		return nil, ErrNotFound
	}
	var environments []Environment
	for _, environment := range f.environments {
		if environment.ProjectID == projectID {
			environments = append(environments, environment)
		}
	}
	if environments == nil {
		environments = []Environment{}
	}
	return environments, nil
}

func (f *fakeRepository) GetEnvironment(_ context.Context, teamID, environmentID uuid.UUID) (Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	environment, ok := f.environments[environmentID]
	if !ok {
		return Environment{}, ErrNotFound
	}
	project, ok := f.projects[environment.ProjectID]
	if !ok || project.TeamID != teamID {
		return Environment{}, ErrNotFound
	}
	return environment, nil
}

func (f *fakeRepository) UpdateEnvironment(_ context.Context, teamID, environmentID uuid.UUID, name string) (Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	environment, ok := f.environments[environmentID]
	if !ok {
		return Environment{}, ErrNotFound
	}
	project, ok := f.projects[environment.ProjectID]
	if !ok || project.TeamID != teamID {
		return Environment{}, ErrNotFound
	}
	for id, other := range f.environments {
		if id != environmentID && other.ProjectID == environment.ProjectID && equalFold(other.Name, name) {
			return Environment{}, ErrEnvironmentExists
		}
	}
	environment.Name = name
	f.environments[environmentID] = environment
	return environment, nil
}

func (f *fakeRepository) DeleteEnvironment(_ context.Context, teamID, environmentID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	environment, ok := f.environments[environmentID]
	if !ok {
		return ErrNotFound
	}
	project, ok := f.projects[environment.ProjectID]
	if !ok || project.TeamID != teamID {
		return ErrNotFound
	}
	delete(f.environments, environmentID)
	return nil
}

func (f *fakeRepository) CountEnvironments(_ context.Context, projectID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, environment := range f.environments {
		if environment.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

// fakeCounter is a ResourceCounter with scripted counts.
type fakeCounter struct {
	mu           sync.Mutex
	environments map[uuid.UUID]ResourceCounts
	projects     map[uuid.UUID]ResourceCounts
	err          error
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{
		environments: make(map[uuid.UUID]ResourceCounts),
		projects:     make(map[uuid.UUID]ResourceCounts),
	}
}

func (f *fakeCounter) CountProjectResources(_ context.Context, projectID uuid.UUID) (ResourceCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.projects[projectID], f.err
}

func (f *fakeCounter) CountEnvironmentResources(_ context.Context, environmentID uuid.UUID) (ResourceCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.environments[environmentID], f.err
}

// newTestService wires the real service onto the in-memory repository with
// the given team scope.
func newTestService(repo *fakeRepository, counter ResourceCounter, userID, teamID uuid.UUID, role teams.Role) (*Service, context.Context) {
	svc := NewService(Config{Repository: repo, Counter: counter, Logger: discardLogger()})
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: role})
	return svc, ctx
}

// equalFold compares names the way the lower(name) indexes do.
func equalFold(a, b string) bool {
	return strings.EqualFold(a, b)
}
