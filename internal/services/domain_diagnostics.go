package services

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

const (
	DomainStatusRouted   = "routed"
	DomainStatusHeld     = "held"
	DomainStatusShadowed = "shadowed"
	DomainStatusUnknown  = "unknown"
)

// diagnoseDomains fills Status and Reason for the list. It mirrors the rules
// the proxy applies when it decides whether a service host becomes a route.
func (s *service) diagnoseDomains(ctx context.Context, service Service, views []DomainView) {
	hostCounts := make(map[string]int, len(views))
	for _, view := range views {
		if view.shadowed {
			continue
		}
		hostCounts[proxy.NormalizeDomain(view.Domain)]++
	}
	sources, _ := s.repo.ListRedirectSources(ctx)
	redirects := make(map[string]bool, len(sources))
	for _, source := range sources {
		redirects[proxy.NormalizeDomain(source)] = true
	}
	otherLabels := s.otherServiceLabelHosts(ctx, service.ID)

	agent, agentErr := s.dialAgent(ctx, service.ServerID)
	var containers []ComposeContainer
	if agentErr == nil {
		defer func() { _ = agent.Close() }()
		containers, agentErr = agent.Ps(ctx, ProjectName(service.ID))
	}

	for i := range views {
		host := proxy.NormalizeDomain(views[i].Domain)
		held := func(reason string) {
			views[i].Status = DomainStatusHeld
			views[i].Reason = reason
		}
		switch {
		case views[i].shadowed:
			views[i].Status = DomainStatusShadowed
			views[i].Reason = "the managed domain of this service wins"
			continue
		case views[i].Disabled:
			held("the domain is disabled")
			continue
		case proxy.ValidateDomain(host) != nil:
			held("invalid domain")
			continue
		case hostCounts[host] > 1:
			held("duplicate domain across compose services on this node; all conflicting bindings are held back")
			continue
		}
		if claim, err := s.repo.ApplicationDomainClaim(ctx, host); err == nil && claim {
			held("domain is already routed by an application")
			continue
		}
		if claim, err := s.repo.ServiceDomainClaim(ctx, host); err == nil && claim.ServiceID != service.ID {
			held("domain is already used by another service")
			continue
		}
		if otherLabels[host] {
			held("domain is already declared by another service")
			continue
		}
		if redirects[host] {
			held("domain is already used by a redirect rule")
			continue
		}
		if agentErr != nil {
			views[i].Status = DomainStatusUnknown
			views[i].Reason = "routing status unavailable: " + Redact(agentErr.Error(), service.Env)
			continue
		}
		if reason := composePortReason(containers, views[i].ComposeService, views[i].Port); reason != "" {
			held(reason)
			continue
		}
		views[i].Status = DomainStatusRouted
	}
}

// otherServiceLabelHosts lists the hosts other services declare as compose
// labels, so the list can explain a hold-back that happened after this
// service was written.
func (s *service) otherServiceLabelHosts(ctx context.Context, serviceID uuid.UUID) map[string]bool {
	hosts := map[string]bool{}
	services, err := s.repo.ListRoutableServices(ctx)
	if err != nil {
		return hosts
	}
	for _, other := range services {
		if other.ID == serviceID {
			continue
		}
		managed, err := s.repo.ListServiceDomains(ctx, other.ID)
		if err != nil {
			continue
		}
		managedHosts := enabledManagedHostSet(managed)
		rendered, _, err := RenderService(other.ComposeYAML, other.Env, managed)
		if err != nil {
			continue
		}
		for _, route := range rendered.Spec.Domains {
			if !managedHosts[route.Domain] {
				hosts[route.Domain] = true
			}
		}
	}
	return hosts
}

func composePortReason(containers []ComposeContainer, composeService string, port int32) string {
	var found *ComposeContainer
	for i := range containers {
		container := &containers[i]
		if container.Service != composeService || !strings.EqualFold(container.State, "running") {
			continue
		}
		found = container
		break
	}
	if found == nil {
		return "no running container for the compose service found on the node"
	}
	if publishedContainerPort(found.Ports, port) {
		return ""
	}
	return "container port " + strconv.Itoa(int(port)) + " is not published; add it to the compose service ports"
}

// publishedContainerPort understands the "0.0.0.0:8080->80/tcp" shape that
// compose ps reports.
func publishedContainerPort(entries []string, port int32) bool {
	for _, entry := range entries {
		left, right, ok := strings.Cut(entry, "->")
		if !ok {
			continue
		}
		target, _, _ := strings.Cut(right, "/")
		containerPort, err := strconv.Atoi(strings.TrimSpace(target))
		if err != nil || int32(containerPort) != port {
			continue
		}
		host := left
		if index := strings.LastIndex(left, ":"); index >= 0 {
			host = left[index+1:]
		}
		hostPort, err := strconv.Atoi(strings.TrimSpace(host))
		if err == nil && hostPort > 0 {
			return true
		}
	}
	return false
}
