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
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/internal/terminal"
)

// terminal targets aggregation (JUS-265).
//
// The add-tab picker reads one authorised list. Host shells are a platform
// operator capability; container targets come from the same deploy resolver
// the per-application discovery route uses, so a target is never something the
// caller could not already open from its application page. The active team
// scope (set by RequireTeam) decides which applications are considered, and the
// resolver re-proves application write access for each one.

// Bounds: one picker read must stay cheap on an installation with many nodes
// and applications. Overflow is simply not listed; the picker is a launcher,
// not an inventory.
const (
	maxTerminalTargetServers      = 50
	maxTerminalTargetApplications = 50
	maxTerminalTargetContainers   = 50
	// maxTerminalTargetScannedApplications bounds how many application rows one
	// picker read resolves. The cap is a scan bound, never the visible cap: the
	// visible 50 is applied after resolution so stopped/forbidden apps cannot
	// hide a running one.
	maxTerminalTargetScannedApplications = 100
	// terminalTargetWorkers bounds concurrent node dials from one picker read.
	terminalTargetWorkers = 4
)

// terminalTargetsSource is the request-scoped adapter between the transport's
// discovery contract and the servers/deploy domains.
type terminalTargetsSource struct {
	servers    ServerService
	apps       deploy.DeployService
	resolver   deploy.TerminalResolver
	isOperator bool
}

// terminalTargetsSourceFor captures the request's operator decision once, so
// the later list cannot accidentally re-read a changed request context.
func (s *Server) terminalTargetsSourceFor(r *http.Request) terminal.TargetsSource {
	source := terminalTargetsSource{isOperator: s.isPlatformOperator(r)}
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

// ListTargets returns everything the caller may open. Per-source failures are
// reported in Targets.Errors; only a truly unusable request (the caller is
// gone) returns an error.
func (s terminalTargetsSource) ListTargets(ctx context.Context, userID uuid.UUID) (terminal.Targets, error) {
	targets := terminal.Targets{
		Servers:      make([]terminal.HostTarget, 0),
		Applications: make([]terminal.ContainerTargetGroup, 0),
		Errors:       make([]terminal.TargetError, 0),
	}
	// Host shells: only a platform operator may open one, so a non-operator's
	// list simply has no servers (never an authorization error entry).
	if s.isOperator && s.servers != nil {
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

// readyHostTargets keeps nodes an operator can actually start a shell on and
// reports whether the bound cut the ready set.
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

// collectContainers lists the active team's applications and resolves each
// application's running containers with a bounded worker pool. The visible cap
// is applied after filtering (in the transport response), so an application
// with no running container never consumes a slot. One slow or broken node
// never blocks the rest of the list: the fan-out stops launching work on
// ctx.Done and collapses all deadline/cancel outcomes into one timeout entry.
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
	// Most recently updated first: a just-redeployed application is the one an
	// operator is most likely to open, and the order is deterministic.
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

	// Each application is resolved independently because the deploy resolver
	// authorizes and locks per application; sharing one node read would need a
	// deploy-domain batch API. The worker bound keeps that at four dials.
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

	// Results stay in the application order, which is also the order of the
	// visible cap: the first 50 resolvable applications win.
	for _, result := range results {
		if result.group != nil {
			targets.Applications = append(targets.Applications, *result.group)
		}
		if result.err != nil {
			targets.Errors = append(targets.Errors, *result.err)
		}
	}
	// Everything the deadline cut short (never started or cancelled mid-call)
	// is one bounded entry with a count, not up to 100 "internal" rows.
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

// targetErrorForApplication classifies one resolution failure. A capability or
// lifecycle gap is not an error the picker should announce:
//
//   - ErrForbidden: the caller may read the team but not open a shell in this
//     application (read_only, or another member's legacy application).
//   - No running deployment / deploy in flight / stopped container: nothing to
//     open right now, and the application's own page owns the explanation.
//
// Everything else is a source failure worth a retry.
func targetErrorForApplication(application deploy.Application, err error) *terminal.TargetError {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, teams.ErrForbidden):
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

// terminalTargetsAuth is the add-tab discovery chain: authentication, an
// interactive JWT session (a scoped API token must never enumerate shells) and
// the active team scope the application list reads. It deliberately carries no
// operator check: a non-operator still sees their container targets.
func (s *Server) terminalTargetsAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.RequireAuth(s.requireInteractiveSession(s.RequireTeam(next)))
	}
}
