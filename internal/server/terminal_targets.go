package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/terminal"
)

const (
	maxTerminalTargetServers             = 50
	maxTerminalTargetApplications        = 50
	maxTerminalTargetContainers          = 50
	maxTerminalTargetScannedApplications = 100
	terminalTargetWorkers                = 4
)

type terminalTargetsSource struct {
	servers  ServerService
	apps     deploy.DeployService
	resolver deploy.TerminalResolver
	isOwner  bool
}

func (s *Server) terminalTargetsSourceFor(r *http.Request) terminal.TargetsSource {
	source := terminalTargetsSource{isOwner: s.isOwner(r)}
	if s.servers != nil {
		source.servers = s.servers
	}
	if s.deploy != nil {
		source.apps = s.deploy
		if resolver, ok := s.deploy.(deploy.TerminalResolver); ok {
			source.resolver = resolver
		}
	}
	return source
}

func (s terminalTargetsSource) ListTargets(ctx context.Context, userID uuid.UUID) (terminal.Targets, error) {
	targets := terminal.Targets{
		Servers:      make([]terminal.HostTarget, 0),
		Applications: make([]terminal.ContainerTargetGroup, 0),
		Errors:       make([]terminal.TargetError, 0),
	}
	if s.isOwner && s.servers != nil {
		hosts, err := s.servers.List(ctx)
		if err != nil {
			targets.Errors = append(targets.Errors, terminal.TargetError{
				Source: "servers",
				Code:   terminal.TargetErrorUnavailable,
			})
		} else {
			var truncated bool
			targets.Servers, truncated = readyHostTargets(hosts)
			targets.Truncated = targets.Truncated || truncated
		}
	}
	if s.apps != nil && s.resolver != nil {
		s.collectContainers(ctx, userID, &targets)
	}
	terminal.SortTargets(&targets)
	return targets, nil
}

func readyHostTargets(hosts []servers.Server) ([]terminal.HostTarget, bool) {
	targets := make([]terminal.HostTarget, 0, len(hosts))
	for _, host := range hosts {
		if host.Status != "ready" {
			continue
		}
		if len(targets) == maxTerminalTargetServers {
			return targets, true
		}
		targets = append(targets, terminal.HostTarget{
			ID:     host.ID,
			Name:   host.Name,
			Status: host.Status,
		})
	}
	return targets, false
}

func (s terminalTargetsSource) collectContainers(ctx context.Context, userID uuid.UUID, targets *terminal.Targets) {
	applications, err := s.apps.ListApplications(ctx, userID, deploy.ApplicationFilter{})
	if err != nil {
		code := terminal.TargetErrorUnavailable
		if errors.Is(err, deploy.ErrDisabled) {
			code = terminal.TargetErrorDisabled
		}
		targets.Errors = append(targets.Errors, terminal.TargetError{
			Source: "applications",
			Code:   code,
		})
		return
	}
	if len(applications) == 0 {
		return
	}
	sort.SliceStable(applications, func(i, j int) bool {
		if !applications[i].UpdatedAt.Equal(applications[j].UpdatedAt) {
			return applications[i].UpdatedAt.After(applications[j].UpdatedAt)
		}
		if applications[i].Name != applications[j].Name {
			return applications[i].Name < applications[j].Name
		}
		return applications[i].ID.String() < applications[j].ID.String()
	})
	if len(applications) > maxTerminalTargetScannedApplications {
		applications = applications[:maxTerminalTargetScannedApplications]
		targets.Truncated = true
	}

	type resolved struct {
		group *terminal.ContainerTargetGroup
		err   *terminal.TargetError
	}
	results := make([]resolved, len(applications))
	var (
		next    int64
		handled int64
		timed   int64
		workers sync.WaitGroup
	)
	worker := func() {
		defer workers.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			index := int(atomic.AddInt64(&next, 1)) - 1
			if index >= len(applications) {
				return
			}
			application := applications[index]
			atomic.AddInt64(&handled, 1)
			set, err := s.resolver.ListTerminalContainers(ctx, userID, application.ID)
			if err != nil {
				if ctx.Err() != nil ||
					errors.Is(err, context.DeadlineExceeded) ||
					errors.Is(err, context.Canceled) {
					atomic.AddInt64(&timed, 1)
					continue
				}
				if entry := targetErrorForApplication(application, err); entry != nil {
					results[index].err = entry
				}
				continue
			}
			if len(set.Containers) == 0 {
				continue
			}
			group := terminal.ContainerTargetGroup{
				ApplicationID:   application.ID,
				ApplicationName: application.Name,
				ServerID:        application.ServerID,
				SourceType:      set.SourceType,
				Containers:      make([]terminal.ContainerTarget, 0, len(set.Containers)),
			}
			for _, container := range set.Containers {
				group.Containers = append(group.Containers, terminal.ContainerTarget{
					Service:       container.Service,
					ContainerID:   container.ContainerID,
					Name:          container.Name,
					Image:         container.Image,
					State:         container.State,
					Replica:       container.Replica,
					Label:         container.Label,
					ServerID:      application.ServerID,
					ApplicationID: application.ID,
				})
				if len(group.Containers) == maxTerminalTargetContainers {
					break
				}
			}
			results[index].group = &group
		}
	}
	workerCount := min(terminalTargetWorkers, len(applications))
	workers.Add(workerCount)
	for range workerCount {
		go worker()
	}
	workers.Wait()

	for _, result := range results {
		if result.group != nil {
			targets.Applications = append(targets.Applications, *result.group)
		}
		if result.err != nil {
			targets.Errors = append(targets.Errors, *result.err)
		}
	}
	unresolved := int64(len(applications)) - atomic.LoadInt64(&handled)
	timeouts := atomic.LoadInt64(&timed)
	if unresolved > 0 {
		timeouts += unresolved
	}
	if timeouts > 0 {
		targets.Errors = append(targets.Errors, terminal.TargetError{
			Source: "applications",
			Code:   terminal.TargetErrorTimeout,
			Count:  int(timeouts),
		})
	}
}

func targetErrorForApplication(application deploy.Application, err error) *terminal.TargetError {
	switch {
	case err == nil, errors.Is(err, terminal.ErrForbidden):
		return nil
	case errors.Is(err, deploy.ErrNoRunningDeployment),
		errors.Is(err, deploy.ErrConflict),
		errors.Is(err, deploy.ErrDeployInFlight),
		errors.Is(err, deploy.ErrContainerNotFound),
		errors.Is(err, deploy.ErrContainerNotRunning):
		return nil
	case errors.Is(err, deploy.ErrDisabled):
		return &terminal.TargetError{Source: "application", ID: application.ID.String(), Name: application.Name, Code: terminal.TargetErrorDisabled}
	case errors.Is(err, deploy.ErrAgentUnavailable), errors.Is(err, deploy.ErrNotFound), errors.Is(err, deploy.ErrServerNotFound):
		return &terminal.TargetError{Source: "application", ID: application.ID.String(), Name: application.Name, Code: terminal.TargetErrorUnavailable}
	default:
		return &terminal.TargetError{Source: "application", ID: application.ID.String(), Name: application.Name, Code: terminal.TargetErrorInternal}
	}
}

func (s *Server) terminalTargetsAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.RequireAuth(s.requireOwnerAccount(s.requireInteractiveSession(next)))
	}
}
