package projects

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeRepository struct {
	mu           sync.Mutex
	projects     map[uuid.UUID]Project
	environments map[uuid.UUID]Environment
	variables    map[uuid.UUID]map[uuid.UUID][]SharedVariable
	previewBases map[uuid.UUID]uuid.UUID
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		projects:     make(map[uuid.UUID]Project),
		environments: make(map[uuid.UUID]Environment),
		variables:    make(map[uuid.UUID]map[uuid.UUID][]SharedVariable),
		previewBases: make(map[uuid.UUID]uuid.UUID),
	}
}

func (f *fakeRepository) CreateProject(_ context.Context, name, description string) (Project, Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, project := range f.projects {
		if equalFold(project.Name, name) {
			return Project{}, Environment{}, ErrProjectExists
		}
	}
	project := Project{ID: uuid.New(), Name: name, Description: description}
	environment := Environment{ID: uuid.New(), ProjectID: project.ID, Name: ProductionEnvironment}
	f.projects[project.ID] = project
	f.environments[environment.ID] = environment
	return project, environment, nil
}

func (f *fakeRepository) ListProjects(_ context.Context) ([]Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	projects := make([]Project, 0, len(f.projects))
	for _, project := range f.projects {
		projects = append(projects, project)
	}
	return projects, nil
}

func (f *fakeRepository) GetProject(_ context.Context, projectID uuid.UUID) (Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (f *fakeRepository) UpdateProject(_ context.Context, projectID uuid.UUID, name, description string) (Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok {
		return Project{}, ErrNotFound
	}
	for id, other := range f.projects {
		if id != projectID && equalFold(other.Name, name) {
			return Project{}, ErrProjectExists
		}
	}
	project.Name, project.Description = name, description
	f.projects[projectID] = project
	return project, nil
}

func (f *fakeRepository) DeleteProject(_ context.Context, projectID uuid.UUID) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	delete(f.projects, projectID)
	for id, environment := range f.environments {
		if environment.ProjectID == projectID {
			delete(f.environments, id)
		}
	}
	return nil, nil
}

func (f *fakeRepository) CreateEnvironment(_ context.Context, projectID uuid.UUID, name string) (Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[projectID]; !ok {
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

func (f *fakeRepository) ListEnvironments(_ context.Context, projectID uuid.UUID) ([]Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	environments := make([]Environment, 0, len(f.environments))
	for _, environment := range f.environments {
		if environment.ProjectID == projectID {
			environments = append(environments, environment)
		}
	}
	return environments, nil
}

func (f *fakeRepository) GetEnvironment(_ context.Context, environmentID uuid.UUID) (Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	environment, ok := f.environments[environmentID]
	if !ok {
		return Environment{}, ErrNotFound
	}
	return environment, nil
}

func (f *fakeRepository) UpdateEnvironment(_ context.Context, environmentID uuid.UUID, name string) (Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	environment, ok := f.environments[environmentID]
	if !ok {
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

func (f *fakeRepository) DeleteEnvironmentIfNotLast(_ context.Context, environmentID uuid.UUID) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	environment, ok := f.environments[environmentID]
	if !ok {
		return nil, ErrNotFound
	}
	remaining := 0
	for _, other := range f.environments {
		if other.ProjectID == environment.ProjectID {
			remaining++
		}
	}
	if remaining <= 1 {
		return nil, ErrLastEnvironment
	}
	delete(f.environments, environmentID)
	return nil, nil
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

func (f *fakeRepository) PreviewBaseIDs(_ context.Context, previewAppIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bases := make(map[uuid.UUID]uuid.UUID, len(previewAppIDs))
	for _, id := range previewAppIDs {
		if base, ok := f.previewBases[id]; ok {
			bases[id] = base
		}
	}
	return bases, nil
}

type fakeCounter struct {
	mu           sync.Mutex
	environments map[uuid.UUID]ResourceCounts
	projects     map[uuid.UUID]ResourceCounts
	previewEnvs  map[uuid.UUID]int
	previewProjs map[uuid.UUID]int
	err          error
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{
		environments: make(map[uuid.UUID]ResourceCounts),
		projects:     make(map[uuid.UUID]ResourceCounts),
		previewEnvs:  make(map[uuid.UUID]int),
		previewProjs: make(map[uuid.UUID]int),
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

func (f *fakeCounter) CountProjectPreviews(_ context.Context, projectID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previewProjs[projectID], f.err
}

func (f *fakeCounter) CountEnvironmentPreviews(_ context.Context, environmentID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previewEnvs[environmentID], f.err
}

func newTestService(repo *fakeRepository, counter ResourceCounter, _ uuid.UUID) (*Service, context.Context) {
	svc := NewService(Config{Repository: repo, Counter: counter, Logger: discardLogger()})
	return svc, context.Background()
}

func equalFold(a, b string) bool {
	return strings.EqualFold(a, b)
}

func (f *fakeRepository) ListVariables(_ context.Context, projectID, environmentID uuid.UUID) ([]SharedVariable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := append([]SharedVariable{}, f.variables[projectID][environmentID]...)
	sortSharedVariables(rows)
	return rows, nil
}

func (f *fakeRepository) ReplaceVariables(_ context.Context, projectID, environmentID uuid.UUID, build func(existing []SharedVariable) ([]SharedVariable, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing := append([]SharedVariable{}, f.variables[projectID][environmentID]...)
	sortSharedVariables(existing)
	vars, err := build(existing)
	if err != nil {
		return err
	}
	scopes := f.variables[projectID]
	if scopes == nil {
		scopes = make(map[uuid.UUID][]SharedVariable)
		f.variables[projectID] = scopes
	}
	scopes[environmentID] = append([]SharedVariable{}, vars...)
	return nil
}

func sortSharedVariables(rows []SharedVariable) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
}
