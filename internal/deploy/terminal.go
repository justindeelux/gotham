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

// TerminalContainer is one running container of an application that a
// container terminal may target. The control plane derives it from the
// application's current deployment and the node's Docker/Compose metadata; a
// browser never supplies a trusted container id.
type TerminalContainer struct {
	// Service is the compose service name (empty for a single-container
	// application).
	Service string
	// ContainerID is the full Docker container id. It is matched exactly when a
	// session is created, so an id from another application or an older
	// deployment is refused.
	ContainerID string
	// Name is the Docker container name.
	Name string
	// Image is the image reference the container runs.
	Image string
	// State is the Docker state ("running" for every returned target).
	State string
	// Replica is the deterministic 1-based position of the container within its
	// service. Ordering is by service, then container name, then id, so the
	// same running set always produces the same visible selection.
	Replica int
	// Label is the human-readable target label ("web · gotham-…-web-1").
	Label string
}

// TerminalTargetSet is the resolved terminal surface of one application.
type TerminalTargetSet struct {
	ApplicationID   uuid.UUID
	ApplicationName string
	ServerID        uuid.UUID
	TeamID          uuid.UUID
	OwnerID         uuid.UUID
	SourceType      string
	Containers      []TerminalContainer
}

// TerminalApplicationAccess is the authorization-relevant slice of an
// application row. The server's terminal authorizer loads it by id at the
// handshake and on every revalidation tick.
type TerminalApplicationAccess struct {
	ID       uuid.UUID
	TeamID   uuid.UUID
	OwnerID  uuid.UUID
	ServerID uuid.UUID
}

// TerminalResolver resolves application container targets. It is implemented
// by *Service and consumed by the HTTP server through a narrow adapter, so the
// transport package never imports the deploy domain.
type TerminalResolver interface {
	// ListTerminalContainers resolves the running targets of one application
	// for a caller with application write access. A deployment in progress, no
	// running deployment and stopped containers are explicit errors.
	ListTerminalContainers(ctx context.Context, userID, appID uuid.UUID) (TerminalTargetSet, error)
	// ResolveTerminalContainer re-resolves the target set and validates the
	// requested service and container id against it.
	ResolveTerminalContainer(ctx context.Context, userID, appID uuid.UUID, service, containerID string) (TerminalTargetSet, TerminalContainer, error)
	// TerminalApplicationAccess returns the application's team, owner and node
	// without authorizing the caller. Callers must authorize separately; it
	// exists for the WebSocket handshake, which authenticates in-band.
	TerminalApplicationAccess(ctx context.Context, appID uuid.UUID) (TerminalApplicationAccess, error)
}

var _ TerminalResolver = (*Service)(nil)

// ListTerminalContainers implements TerminalResolver.
func (s *Service) ListTerminalContainers(ctx context.Context, userID, appID uuid.UUID) (TerminalTargetSet, error) {
	return s.terminalTargets(ctx, userID, appID)
}

// ResolveTerminalContainer implements TerminalResolver.
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
		// The service name, when supplied, must match the container's own
		// service. A target labelled as another service is refused rather than
		// silently attached.
		if candidate.Service != service {
			continue
		}
		return set, candidate, nil
	}
	return set, TerminalContainer{}, fmt.Errorf("%w: the selected container is not part of this application's running deployment", ErrContainerNotFound)
}

// TerminalApplicationAccess implements TerminalResolver.
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

// terminalDeploymentSnapshot authorizes the caller, then reads the application
// and its newest running deployment under the per-application lock. The lock is
// released before any node dial or RPC.
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
	// Re-read under the lock so a concurrent move cannot resolve the previous
	// node while the caller still holds the old authorization decision.
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, nil, err
	}
	deployments, err := s.repo.ListDeployments(ctx, app.ID)
	if err != nil {
		return Application{}, nil, err
	}
	var running *Deployment
	for i := range deployments { // newest first
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

// terminalTargets is the single resolution path for listing and creating a
// container session. It proves application write access first, snapshots the
// running deployment under the application lock, then dials the node outside
// that lock: a slow node must not delay deploys or other mutations. A stale
// snapshot is safe because create re-resolves, attach re-resolves again (after
// the pending window) and the agent verifies the exact container is running.
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
		containers, err := node.ComposePs(callCtx, services.ProjectName(app.ID))
		if err != nil {
			return TerminalTargetSet{}, err
		}
		set.Containers = runningComposeTargets(containers)
		if len(set.Containers) == 0 {
			return TerminalTargetSet{}, fmt.Errorf("%w: the compose project has no running container", ErrContainerNotRunning)
		}
		// `docker compose ps --format json` reports the 12-character short id
		// on some Compose versions. The terminal contract, the create-time
		// match and the agent's strict validation all require the full
		// 64-character id, so every running target is normalized here, the
		// single place a Compose container id enters the terminal flow. The
		// ids come from the node's container list (Docker's summary `Id` is
		// always full); ComposePs itself keeps its shape for the deploy and
		// services paths.
		if composeTargetsNeedFullIDs(set.Containers) {
			listed, listErr := node.Containers(callCtx)
			if listErr != nil {
				return TerminalTargetSet{}, listErr
			}
			if err := resolveComposeTargetIDs(set.Containers, listed); err != nil {
				return TerminalTargetSet{}, err
			}
		}
		// The release's recorded web container must be part of the running set:
		// an older release's leftover container is never a terminal target.
		// Compose deployments recorded the CLI's id, which can be the short
		// prefix of the normalized id.
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

// runningComposeTargets keeps the running compose containers and assigns the
// deterministic service/replica order the UI displays and the create call
// validates against.
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

// composeTargetsNeedFullIDs reports whether any resolved Compose target still
// carries a short CLI id. The node's container list is only read when it does,
// so a Compose version that already prints full ids costs no extra RPC.
func composeTargetsNeedFullIDs(targets []TerminalContainer) bool {
	for _, target := range targets {
		if !isFullContainerID(target.ContainerID) {
			return true
		}
	}
	return false
}

// resolveComposeTargetIDs replaces each target's CLI-reported id with the full
// Docker id read from the node's container list. It runs on the slice in place
// so the caller's deterministic service/replica order and visible labels stay
// unchanged. A running container that the list does not contain (it vanished
// between the two reads, or the name/prefix is ambiguous) is refused rather
// than attached to a guessed id.
func resolveComposeTargetIDs(targets []TerminalContainer, listed []*agentv1.ContainerInfo) error {
	for i := range targets {
		if isFullContainerID(targets[i].ContainerID) {
			continue
		}
		id, ok := fullContainerIDFor(targets[i], listed)
		if !ok {
			label := strings.TrimSpace(targets[i].Name)
			if label == "" {
				label = targets[i].ContainerID
			}
			return fmt.Errorf("%w: the compose container %q is no longer present on the node; refresh the targets",
				ErrContainerNotFound, label)
		}
		targets[i].ContainerID = id
		targets[i].Label = terminalTargetLabel(targets[i].Service, targets[i].Name, id)
	}
	return nil
}

// fullContainerIDFor resolves one Compose target against the node's container
// list: an exact container name is authoritative (Docker names are unique on a
// node), and a unique full-id prefix is the fallback when Compose printed no
// name. A short id is never returned and an ambiguous prefix never resolves.
func fullContainerIDFor(target TerminalContainer, listed []*agentv1.ContainerInfo) (string, bool) {
	if name := strings.TrimSpace(target.Name); name != "" {
		for _, container := range listed {
			if container.GetName() == name && isFullContainerID(container.GetId()) {
				return container.GetId(), true
			}
		}
	}
	prefix := strings.TrimSpace(target.ContainerID)
	if prefix == "" {
		return "", false
	}
	matched := ""
	for _, container := range listed {
		id := container.GetId()
		if !isFullContainerID(id) || !strings.HasPrefix(id, prefix) {
			continue
		}
		if matched != "" && matched != id {
			return "", false
		}
		matched = id
	}
	return matched, matched != ""
}

// isFullContainerID reports whether id is the full lowercase hexadecimal Docker
// id the terminal contract uses.
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

// containerIDMatches reports whether full (a resolved target id) is the
// container the running deployment recorded. A Compose deployment persisted
// the CLI's id, which is a 12-character prefix on some Compose versions; the
// full id starts with it. A single-container deployment records the full id.
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

// findContainer resolves a container by exact id.
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

// containerState renders the state for an actionable error.
func containerState(container *agentv1.ContainerInfo) string {
	if state := strings.TrimSpace(container.GetState()); state != "" {
		return state
	}
	return "stopped"
}

// terminalTargetLabel builds the visible target label from the compose service
// and the container name (or its short id when the name is unavailable).
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
