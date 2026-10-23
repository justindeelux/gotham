package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type TerminalContainer struct {
	Service     string
	ContainerID string
	Name        string
	Image       string
	State       string
	Replica     int
	Label       string
}

type TerminalTargetSet struct {
	ApplicationID   uuid.UUID
	ApplicationName string
	ServerID        uuid.UUID
	TeamID          uuid.UUID
	OwnerID         uuid.UUID
	SourceType      string
	Containers      []TerminalContainer
}

type TerminalApplicationAccess struct {
	ID       uuid.UUID
	TeamID   uuid.UUID
	OwnerID  uuid.UUID
	ServerID uuid.UUID
}

type TerminalResolver interface {
	ListTerminalContainers(ctx context.Context, userID, appID uuid.UUID) (TerminalTargetSet, error)
	ResolveTerminalContainer(ctx context.Context, userID, appID uuid.UUID, service, containerID string) (TerminalTargetSet, TerminalContainer, error)
	TerminalApplicationAccess(ctx context.Context, appID uuid.UUID) (TerminalApplicationAccess, error)
}

var _ TerminalResolver = (*Service)(nil)

func (s *Service) ListTerminalContainers(ctx context.Context, userID, appID uuid.UUID) (TerminalTargetSet, error) {
	return s.terminalTargets(ctx, userID, appID)
}

func (s *Service) ResolveTerminalContainer(ctx context.Context, userID, appID uuid.UUID, service, containerID string) (TerminalTargetSet, TerminalContainer, error) {
	set, err := s.terminalTargets(ctx, userID, appID)
	if err != nil {
		return TerminalTargetSet{}, TerminalContainer{}, err
	}
	service = strings.TrimSpace(service)
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return set, TerminalContainer{}, fmt.Errorf("%w: a container must be selected", ErrValidation)
	}
	for _, candidate := range set.Containers {
		if candidate.ContainerID != containerID {
			continue
		}
		if candidate.Service != service {
			continue
		}
		return set, candidate, nil
	}
	return set, TerminalContainer{}, fmt.Errorf("%w: the selected container is not part of this application's running deployment", ErrContainerNotFound)
}

func (s *Service) TerminalApplicationAccess(ctx context.Context, appID uuid.UUID) (TerminalApplicationAccess, error) {
	if s == nil || s.repo == nil {
		return TerminalApplicationAccess{}, errors.New("deploy: repository is not configured")
	}
	if appID == uuid.Nil {
		return TerminalApplicationAccess{}, fmt.Errorf("%w: invalid application id", ErrValidation)
	}
	app, err := s.repo.GetApplication(ctx, appID)
	if err != nil {
		return TerminalApplicationAccess{}, err
	}
	return TerminalApplicationAccess{
		ID:       app.ID,
		TeamID:   app.TeamID,
		OwnerID:  app.UserID,
		ServerID: app.ServerID,
	}, nil
}

func (s *Service) terminalDeploymentSnapshot(ctx context.Context, userID, appID uuid.UUID) (Application, *Deployment, error) {
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, nil, err
	}
	if app.ServerID == uuid.Nil {
		return Application{}, nil, ErrServerNotFound
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, nil, err
	}
	deployments, err := s.repo.ListDeployments(ctx, app.ID)
	if err != nil {
		return Application{}, nil, err
	}
	var running *Deployment
	for i := range deployments {
		if !deployments[i].State.Terminal() {
			return Application{}, nil, fmt.Errorf("%w: a deployment is in progress", ErrConflict)
		}
		if running == nil && deployments[i].State == StateRunning && deployments[i].ContainerID != "" {
			running = &deployments[i]
		}
	}
	if running == nil {
		return Application{}, nil, fmt.Errorf("%w: application has no running deployment", ErrNoRunningDeployment)
	}
	return app, running, nil
}

func (s *Service) terminalTargets(ctx context.Context, userID, appID uuid.UUID) (TerminalTargetSet, error) {
	if !Enabled() {
		return TerminalTargetSet{}, ErrDisabled
	}
	app, running, err := s.terminalDeploymentSnapshot(ctx, userID, appID)
	if err != nil {
		return TerminalTargetSet{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	node, err := s.dialNode(callCtx, app.ServerID)
	if err != nil {
		return TerminalTargetSet{}, err
	}
	defer func() {
		if closeErr := node.Close(); closeErr != nil {
			s.logger.Debug("deploy: close terminal node", "application_id", app.ID, "error", closeErr)
		}
	}()

	set := TerminalTargetSet{
		ApplicationID:   app.ID,
		ApplicationName: app.Name,
		ServerID:        app.ServerID,
		TeamID:          app.TeamID,
		OwnerID:         app.UserID,
		SourceType:      NormalizeSourceType(app.SourceType, app.Provider),
	}
	if set.SourceType == SourceCompose {
		project := services.ProjectName(app.ID)
		containers, err := node.ComposePs(callCtx, project)
		if err != nil {
			return TerminalTargetSet{}, err
		}
		set.Containers = runningComposeTargets(containers)
		if len(set.Containers) == 0 {
			return TerminalTargetSet{}, fmt.Errorf("%w: the compose project has no running container", ErrContainerNotRunning)
		}
		if composeTargetsNeedFullIDs(set.Containers) {
			listed, listErr := node.ContainersByLabel(callCtx, map[string]string{
				composeProjectLabel: project,
			}, false, false)
			if listErr != nil {
				return TerminalTargetSet{}, listErr
			}
			set.Containers = s.resolveComposeTargetIDs(set.Containers, listed, project, app.ID)
			if len(set.Containers) == 0 {
				return TerminalTargetSet{}, fmt.Errorf(
					"%w: no compose container could be resolved to a full id; refresh the targets", ErrContainerNotFound)
			}
		}
		found := false
		for _, candidate := range set.Containers {
			if containerIDMatches(candidate.ContainerID, running.ContainerID) {
				found = true
				break
			}
		}
		if !found {
			return TerminalTargetSet{}, fmt.Errorf("%w: the deployment's compose container is not running", ErrContainerNotRunning)
		}
		return set, nil
	}

	containers, err := node.Containers(callCtx)
	if err != nil {
		return TerminalTargetSet{}, err
	}
	found := findContainer(containers, running.ContainerID)
	if found == nil {
		return TerminalTargetSet{}, fmt.Errorf("%w: the application container no longer exists; redeploy the application", ErrContainerNotFound)
	}
	if !strings.EqualFold(found.GetState(), "running") {
		return TerminalTargetSet{}, fmt.Errorf("%w: the application container is %s; start the application first",
			ErrContainerNotRunning, containerState(found))
	}
	set.Containers = []TerminalContainer{{
		ContainerID: found.GetId(),
		Name:        found.GetName(),
		Image:       found.GetImage(),
		State:       found.GetState(),
		Replica:     1,
		Label:       terminalTargetLabel("", found.GetName(), found.GetId()),
	}}
	return set, nil
}

func runningComposeTargets(containers []ComposeContainer) []TerminalContainer {
	targets := make([]TerminalContainer, 0, len(containers))
	for _, container := range containers {
		if !strings.EqualFold(container.State, "running") || strings.TrimSpace(container.ID) == "" {
			continue
		}
		targets = append(targets, TerminalContainer{
			Service:     container.Service,
			ContainerID: container.ID,
			Name:        container.Name,
			Image:       container.Image,
			State:       container.State,
		})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Service != targets[j].Service {
			return targets[i].Service < targets[j].Service
		}
		if targets[i].Name != targets[j].Name {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].ContainerID < targets[j].ContainerID
	})
	counts := make(map[string]int, len(targets))
	for i := range targets {
		counts[targets[i].Service]++
		targets[i].Replica = counts[targets[i].Service]
		targets[i].Label = terminalTargetLabel(targets[i].Service, targets[i].Name, targets[i].ContainerID)
	}
	return targets
}

func composeTargetsNeedFullIDs(targets []TerminalContainer) bool {
	for _, target := range targets {
		if !isFullContainerID(target.ContainerID) {
			return true
		}
	}
	return false
}

const (
	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
)

func (s *Service) resolveComposeTargetIDs(targets []TerminalContainer, listed []*agentv1.ContainerInfo, project string, appID uuid.UUID) []TerminalContainer {
	resolved := make([]TerminalContainer, 0, len(targets))
	for _, target := range targets {
		id, ok := resolveComposeTargetID(target, composeContainerCandidates(listed, project, target.Service))
		if !ok {
			s.logger.Warn("deploy: compose terminal target could not be resolved to a full container id; omitting it",
				"application_id", appID, "project", project, "service", target.Service,
				"container", target.Name, "container_id", target.ContainerID)
			continue
		}
		target.ContainerID = id
		target.Label = terminalTargetLabel(target.Service, target.Name, id)
		resolved = append(resolved, target)
	}
	return resolved
}

func composeContainerCandidates(listed []*agentv1.ContainerInfo, project, service string) []*agentv1.ContainerInfo {
	candidates := make([]*agentv1.ContainerInfo, 0, len(listed))
	for _, container := range listed {
		if container == nil || !strings.EqualFold(container.GetState(), "running") {
			continue
		}
		labels := container.GetLabels()
		if labels[composeProjectLabel] != project || labels[composeServiceLabel] != service {
			continue
		}
		if !isFullContainerID(container.GetId()) {
			continue
		}
		candidates = append(candidates, container)
	}
	return candidates
}

func resolveComposeTargetID(target TerminalContainer, candidates []*agentv1.ContainerInfo) (string, bool) {
	if isFullContainerID(target.ContainerID) {
		return target.ContainerID, true
	}
	if !isShortContainerID(target.ContainerID) {
		return "", false
	}
	name := strings.TrimSpace(target.Name)
	matched := ""
	for _, candidate := range candidates {
		id := candidate.GetId()
		if !isFullContainerID(id) || !strings.HasPrefix(id, target.ContainerID) {
			continue
		}
		if name != "" && candidate.GetName() != name {
			continue
		}
		if matched != "" && matched != id {
			return "", false
		}
		matched = id
	}
	return matched, matched != ""
}

func isShortContainerID(id string) bool {
	if len(id) < 4 || len(id) > 63 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isFullContainerID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func containerIDMatches(full, recorded string) bool {
	recorded = strings.TrimSpace(recorded)
	if recorded == "" {
		return false
	}
	if full == recorded {
		return true
	}
	return len(recorded) < len(full) && strings.HasPrefix(full, recorded)
}

func findContainer(containers []*agentv1.ContainerInfo, id string) *agentv1.ContainerInfo {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	for _, container := range containers {
		if container.GetId() == id {
			return container
		}
	}
	return nil
}

func containerState(container *agentv1.ContainerInfo) string {
	if state := strings.TrimSpace(container.GetState()); state != "" {
		return state
	}
	return "stopped"
}

func terminalTargetLabel(service, name, id string) string {
	label := strings.TrimSpace(name)
	if label == "" {
		label = shortID(id)
	}
	if service = strings.TrimSpace(service); service != "" {
		return service + " · " + label
	}
	return label
}
