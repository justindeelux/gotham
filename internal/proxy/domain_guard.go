package proxy

import (
	"context"
	"fmt"
	"net/url"
)

// DomainGuard serializes domain writes with panel URL changes. Release only
// after persistence; a nil guard leaves standalone services unchanged.
type DomainGuard func(context.Context, ...string) (release func(), err error)

func (g DomainGuard) Acquire(ctx context.Context, hosts ...string) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	return g(ctx, hosts...)
}

// CheckControlPlaneHost rejects all existing app, compose-service and redirect
// sources, including stopped applications and disabled redirects.
func (s *SyncService) CheckControlPlaneHost(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := NormalizeDomain(u.Hostname())
	if err := ValidateDomain(host); err != nil {
		return fmt.Errorf("invalid control-plane host: %w", err)
	}
	if source, ok := s.source.(storeSource); ok {
		domains, err := source.store.ListProxiedApplicationDomains(ctx)
		if err != nil {
			return err
		}
		for _, domain := range domains {
			if NormalizeDomain(domain.Domain) == host {
				return fmt.Errorf("host %q is already used by an application", host)
			}
		}
	}
	apps, err := s.source.ListProxiedApplications(ctx)
	if err != nil {
		return err
	}
	for _, app := range apps {
		for _, domain := range appDomains(app) {
			if domain == host {
				return fmt.Errorf("host %q is already used by an application", host)
			}
		}
	}
	services, err := s.listServices(ctx)
	if err != nil {
		return err
	}
	for _, service := range services {
		if service.Unroutable != "" {
			return fmt.Errorf("cannot check service domains: %s", service.Unroutable)
		}
		for _, domain := range service.Domains {
			if NormalizeDomain(domain.Host) == host {
				return fmt.Errorf("host %q is already used by a service", host)
			}
		}
	}
	if s.redirects != nil {
		rules, err := s.redirects.ListRedirectRules(ctx)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if NormalizeDomain(rule.SourceDomain) == host {
				return fmt.Errorf("host %q is already used by a redirect", host)
			}
		}
	}
	return nil
}
