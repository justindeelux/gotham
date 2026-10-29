package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

const (
	// EnvDomain and EnvDomains are injected by the platform when a service
	// document is rendered; users cannot set them in the environment map.
	EnvDomain  = "GOTHAM_DOMAIN"
	EnvDomains = "GOTHAM_DOMAINS"

	SourceManaged = "managed"
	SourceCompose = "compose"

	// domainSeed lets the first render extract label routes from a document
	// that references GOTHAM_DOMAIN before the real host set is known.
	domainSeed = "gotham-derived-domain.invalid"
)

// DomainInput is the write shape for a managed service domain.
type DomainInput struct {
	Domain         string `json:"domain"`
	ComposeService string `json:"compose_service"`
	Port           int32  `json:"port"`
	Protocol       string `json:"protocol"`
	Disabled       *bool  `json:"disabled,omitempty"`
}

// DomainView is one row of the domain list: a managed row, or a route the
// compose document declares read-only.
type DomainView struct {
	ServiceDomain
	Source   string `json:"source"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	shadowed bool
}

func IsReservedEnv(key string) bool {
	return key == EnvDomain || key == EnvDomains
}

// RenderService renders the document with GOTHAM_DOMAIN and GOTHAM_DOMAINS
// derived from the managed domains and the label routes the document declares.
func RenderService(document string, env map[string]string, managed []ServiceDomain) (RenderedSpec, []string, error) {
	probeEnv := cloneEnv(env)
	probeEnv[EnvDomain] = domainSeed
	probeEnv[EnvDomains] = domainSeed
	probe, err := Render(document, probeEnv)
	if err != nil {
		return RenderedSpec{}, nil, err
	}

	hosts := make([]string, 0, len(managed)+len(probe.Spec.Domains))
	for _, domain := range managed {
		if domain.Disabled {
			continue
		}
		hosts = append(hosts, proxy.NormalizeDomain(domain.Domain))
	}
	for _, route := range probe.Spec.Domains {
		if route.Domain == domainSeed {
			continue
		}
		hosts = append(hosts, route.Domain)
	}
	hosts = normalizeHosts(hosts)

	runtimeEnv := cloneEnv(env)
	runtimeEnv[EnvDomain] = ""
	runtimeEnv[EnvDomains] = ""
	if len(hosts) > 0 {
		runtimeEnv[EnvDomain] = hosts[0]
		runtimeEnv[EnvDomains] = strings.Join(hosts, ",")
	}
	rendered, err := Render(document, runtimeEnv)
	if err != nil {
		return RenderedSpec{}, nil, err
	}
	return rendered, hosts, nil
}

func referencesDomainEnv(document string) bool {
	return strings.Contains(document, EnvDomain)
}

func cloneEnv(env map[string]string) map[string]string {
	clone := make(map[string]string, len(env)+2)
	for key, value := range env {
		clone[key] = value
	}
	return clone
}

// enabledManagedHostSet is the set of hosts the managed rows already route.
// A compose label that renders to one of them is shadowed, not a duplicate.
func enabledManagedHostSet(managed []ServiceDomain) map[string]bool {
	hosts := make(map[string]bool, len(managed))
	for _, domain := range managed {
		if !domain.Disabled {
			hosts[proxy.NormalizeDomain(domain.Domain)] = true
		}
	}
	return hosts
}

func normalizeHosts(hosts []string) []string {
	normalized := make([]string, 0, len(hosts))
	seen := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		host = proxy.NormalizeDomain(host)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		normalized = append(normalized, host)
	}
	return normalized
}

func sameHosts(a, b []string) bool {
	left := normalizeHosts(a)
	right := normalizeHosts(b)
	sort.Strings(left)
	sort.Strings(right)
	return slices.Equal(left, right)
}

func (s *service) normalizeDomain(service Service, managed []ServiceDomain, in DomainInput) (ServiceDomain, error) {
	host := proxy.NormalizeDomain(in.Domain)
	if err := proxy.ValidateDomain(host); err != nil {
		return ServiceDomain{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if in.Port < 1 || in.Port > 65535 {
		return ServiceDomain{}, fmt.Errorf("%w: port must be between 1 and 65535", ErrValidation)
	}
	protocol := proxy.NormalizeProtocol(in.Protocol)
	if protocol != proxy.ProtocolHTTP && protocol != proxy.ProtocolHTTPS {
		return ServiceDomain{}, fmt.Errorf("%w: protocol must be %q or %q",
			ErrValidation, proxy.ProtocolHTTP, proxy.ProtocolHTTPS)
	}
	composeService := strings.TrimSpace(in.ComposeService)
	if composeService == "" {
		return ServiceDomain{}, fmt.Errorf("%w: compose_service is required", ErrValidation)
	}
	rendered, _, err := RenderService(service.ComposeYAML, service.Env, managed)
	if err != nil {
		return ServiceDomain{}, RedactError(err, service.Env)
	}
	if !slices.Contains(rendered.Spec.Services, composeService) {
		return ServiceDomain{}, fmt.Errorf("%w: no compose service %q in this project", ErrValidation, composeService)
	}
	domain := ServiceDomain{
		ServiceID:      service.ID,
		ComposeService: composeService,
		Port:           in.Port,
		Domain:         host,
		Protocol:       protocol,
	}
	if in.Disabled != nil {
		domain.Disabled = *in.Disabled
	}
	return domain, nil
}

// decoratePending marks services whose stored document uses GOTHAM_DOMAIN and
// whose effective host set no longer matches the last successful deploy.
func (s *service) decoratePending(ctx context.Context, services []Service) ([]Service, error) {
	for i := range services {
		service := &services[i]
		if !referencesDomainEnv(service.ComposeYAML) {
			continue
		}
		managed, err := s.repo.ListServiceDomains(ctx, service.ID)
		if err != nil {
			return nil, err
		}
		_, hosts, err := RenderService(service.ComposeYAML, service.Env, managed)
		if err != nil {
			continue
		}
		deploys, err := s.repo.ListServiceDeploys(ctx, service.ID, deployHistoryLimit)
		if err != nil {
			return nil, err
		}
		deployed := []string(nil)
		found := false
		for _, deploy := range deploys {
			if deploy.State == DeployRunning {
				deployed = deploy.Domains
				found = true
				break
			}
		}
		if found && !sameHosts(hosts, deployed) {
			service.PendingChanges = []string{"domains"}
		}
	}
	return services, nil
}

func (s *service) ListDomains(ctx context.Context, userID, serviceID uuid.UUID) ([]DomainView, error) {
	service, err := s.service(ctx, userID, serviceID, false)
	if err != nil {
		return nil, err
	}
	managed, err := s.repo.ListServiceDomains(ctx, service.ID)
	if err != nil {
		return nil, err
	}
	rendered, _, renderErr := RenderService(service.ComposeYAML, service.Env, managed)
	managedHosts := enabledManagedHostSet(managed)
	views := make([]DomainView, 0, len(managed))
	if renderErr == nil {
		for _, route := range rendered.Spec.Domains {
			views = append(views, DomainView{
				ServiceDomain: ServiceDomain{
					ServiceID:      service.ID,
					ComposeService: route.Service,
					Port:           route.Port,
					Domain:         route.Domain,
					Protocol:       proxy.ProtocolHTTP,
				},
				Source:   SourceCompose,
				shadowed: managedHosts[proxy.NormalizeDomain(route.Domain)],
			})
		}
	}
	for _, domain := range managed {
		views = append(views, DomainView{ServiceDomain: domain, Source: SourceManaged})
	}
	s.diagnoseDomains(ctx, service, views)
	return views, nil
}

func (s *service) AddDomain(ctx context.Context, userID, serviceID uuid.UUID, in DomainInput) (ServiceDomain, error) {
	if !Enabled() {
		return ServiceDomain{}, ErrDisabled
	}
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return ServiceDomain{}, err
	}
	defer release()

	managed, err := s.repo.ListServiceDomains(ctx, service.ID)
	if err != nil {
		return ServiceDomain{}, err
	}
	domain, err := s.normalizeDomain(service, managed, in)
	if err != nil {
		return ServiceDomain{}, err
	}
	releaseDomain, err := s.guardDomains.Acquire(ctx, domain.Domain)
	if err != nil {
		return ServiceDomain{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	defer releaseDomain()
	if err := s.checkDomainClaim(ctx, service.ID, domain.Domain); err != nil {
		return ServiceDomain{}, err
	}
	domain.ID = uuid.New()
	created, err := s.repo.CreateServiceDomain(ctx, domain)
	if err != nil {
		return ServiceDomain{}, err
	}
	s.syncProxy(ctx, service.ServerID)
	return created, nil
}

func (s *service) UpdateDomain(ctx context.Context, userID, serviceID, domainID uuid.UUID, in DomainInput) (ServiceDomain, error) {
	if !Enabled() {
		return ServiceDomain{}, ErrDisabled
	}
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return ServiceDomain{}, err
	}
	defer release()
	if domainID == uuid.Nil {
		return ServiceDomain{}, fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	managed, err := s.repo.ListServiceDomains(ctx, service.ID)
	if err != nil {
		return ServiceDomain{}, err
	}
	var target *ServiceDomain
	for i := range managed {
		if managed[i].ID == domainID {
			target = &managed[i]
			break
		}
	}
	if target == nil {
		return ServiceDomain{}, ErrNotFound
	}
	next, err := s.normalizeDomain(service, managed, in)
	if err != nil {
		return ServiceDomain{}, err
	}
	next.ID = target.ID
	next.CreatedAt = target.CreatedAt
	if in.Disabled == nil {
		next.Disabled = target.Disabled
		if next.Domain != target.Domain {
			next.Disabled = false
		}
	}
	releaseDomain, err := s.guardDomains.Acquire(ctx, next.Domain)
	if err != nil {
		return ServiceDomain{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	defer releaseDomain()
	if next.Domain != target.Domain || (target.Disabled && !next.Disabled) {
		if err := s.checkDomainClaim(ctx, service.ID, next.Domain); err != nil {
			return ServiceDomain{}, err
		}
	}
	updated, err := s.repo.UpdateServiceDomain(ctx, next)
	if err != nil {
		return ServiceDomain{}, err
	}
	s.syncProxy(ctx, service.ServerID)
	return updated, nil
}

func (s *service) RemoveDomain(ctx context.Context, userID, serviceID, domainID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return err
	}
	defer release()
	if domainID == uuid.Nil {
		return fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	if err := s.repo.DeleteServiceDomain(ctx, service.ID, domainID); err != nil {
		return err
	}
	s.syncProxy(ctx, service.ServerID)
	return nil
}

// checkDomainClaim rejects a host another resource already owns. A duplicate
// between this service's own compose label and a managed row is allowed: the
// list reports it as held back, exactly like label duplicates.
func (s *service) checkDomainClaim(ctx context.Context, serviceID uuid.UUID, host string) error {
	claimed, err := s.repo.ApplicationDomainClaim(ctx, host)
	if err != nil {
		return err
	}
	if claimed {
		return fmt.Errorf("%w: domain already in use by another application", ErrDomainConflict)
	}
	claim, claimErr := s.repo.ServiceDomainClaim(ctx, host)
	if claimErr != nil && !errors.Is(claimErr, ErrNotFound) {
		return claimErr
	}
	if claimErr == nil {
		if claim.ServiceID == serviceID {
			return fmt.Errorf("%w: the domain is already attached to this service", ErrValidation)
		}
		return fmt.Errorf("%w: domain already in use by another service", ErrDomainConflict)
	}
	sources, err := s.repo.ListRedirectSources(ctx)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if proxy.NormalizeDomain(source) == host {
			return fmt.Errorf("%w: the domain is already used by a redirect rule", ErrDomainConflict)
		}
	}
	services, err := s.repo.ListRoutableServices(ctx)
	if err != nil {
		return err
	}
	for _, other := range services {
		if other.ID == serviceID {
			continue
		}
		rendered, _, renderErr := RenderService(other.ComposeYAML, other.Env, nil)
		if renderErr != nil {
			continue
		}
		for _, route := range rendered.Spec.Domains {
			if route.Domain == host {
				return fmt.Errorf("%w: the domain is already declared by another service", ErrDomainConflict)
			}
		}
	}
	return nil
}
