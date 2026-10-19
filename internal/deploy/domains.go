package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// DomainInput is the validated routing payload of a domain write: the host,
// the target container port, the optional compose service and the protocol.
type DomainInput struct {
	Domain   string
	Port     int32
	Service  string
	Protocol string
}

// normalizeDomainInput validates the routing fields every domain write
// shares (JUS-233):
//
//   - the host is a normalized, valid DNS name (it is embedded in a Traefik
//     Host() rule, so it is the write-time security boundary);
//   - the target port is a real container port (1-65535);
//   - the protocol is http or https (empty defaults to http);
//   - a compose service is structurally valid, and a pasted compose document
//     must declare it. Single-container applications carry no service.
//
// An empty service on a compose application is allowed: it targets the
// application's release service (ComposeService), exactly like a domain
// created before a service was named explicitly.
func normalizeDomainInput(app Application, in DomainInput) (ApplicationDomain, error) {
	domain := proxy.NormalizeDomain(in.Domain)
	if err := proxy.ValidateDomain(domain); err != nil {
		return ApplicationDomain{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if in.Port < 1 || in.Port > 65535 {
		return ApplicationDomain{}, fmt.Errorf("%w: port must be between 1 and 65535", ErrValidation)
	}
	protocol := NormalizeProtocol(in.Protocol)
	if !ValidProtocol(protocol) {
		return ApplicationDomain{}, fmt.Errorf("%w: protocol must be %q or %q", ErrValidation, ProtocolHTTP, ProtocolHTTPS)
	}
	service := strings.TrimSpace(in.Service)
	if isComposeApp(app) {
		if service != "" {
			if err := ValidateComposeService(app.ComposeContent, service); err != nil {
				return ApplicationDomain{}, err
			}
		}
	} else if service != "" {
		return ApplicationDomain{}, fmt.Errorf("%w: a compose service is only valid for compose applications", ErrValidation)
	}
	return ApplicationDomain{
		ApplicationID: app.ID,
		Domain:        domain,
		Port:          in.Port,
		Service:       service,
		Protocol:      protocol,
	}, nil
}

// ListDomains returns one application's domains, primary first, then oldest
// first.
func (s *Service) ListDomains(ctx context.Context, userID, appID uuid.UUID) ([]ApplicationDomain, error) {
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return nil, err
	}
	domains, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	if domains == nil {
		return []ApplicationDomain{}, nil
	}
	return domains, nil
}

// AddDomain attaches one more hostname to an application. The host is
// validated, normalized and claimed platform-wide: a host any other
// application owns, or any enabled redirect source, answers 409/400, and a
// host already attached to this application answers 400. The first domain of
// a domainless application becomes its primary. A domain served over https
// gets the default per-host certificate intent; every mutation resyncs the
// hosting node best effort.
func (s *Service) AddDomain(ctx context.Context, userID, appID uuid.UUID, in DomainInput) (ApplicationDomain, error) {
	if !Enabled() {
		return ApplicationDomain{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return ApplicationDomain{}, err
	}
	domain, err := normalizeDomainInput(app, in)
	if err != nil {
		return ApplicationDomain{}, err
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return ApplicationDomain{}, err
	}
	releaseDomain, domainErr := s.guardDomains.Acquire(ctx, domain.Domain)
	if domainErr != nil {
		return ApplicationDomain{}, fmt.Errorf("%w: %v", ErrValidation, domainErr)
	}
	defer releaseDomain()
	if err := s.checkDomainClaim(ctx, app.ID, domain.Domain); err != nil {
		return ApplicationDomain{}, err
	}
	existing, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return ApplicationDomain{}, err
	}
	domain.IsPrimary = len(existing) == 0
	var created ApplicationDomain
	err = s.repo.InTx(ctx, func(tx Repository) error {
		row, err := tx.CreateApplicationDomain(ctx, domain)
		if err != nil {
			return err
		}
		created = row
		return ensureHTTPSIntent(ctx, tx, app, row)
	})
	if err != nil {
		return ApplicationDomain{}, err
	}
	s.syncProxyServer(ctx, app.ServerID)
	return created, nil
}

// UpdateDomain edits one attached hostname: its host, target port, compose
// service and protocol. The edit keeps the middleware contract identical to
// AddDomain, including the platform-wide host claim and the certificate
// intent of an https domain.
func (s *Service) UpdateDomain(ctx context.Context, userID, appID, domainID uuid.UUID, in DomainInput) (ApplicationDomain, error) {
	if !Enabled() {
		return ApplicationDomain{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return ApplicationDomain{}, err
	}
	if domainID == uuid.Nil {
		return ApplicationDomain{}, fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return ApplicationDomain{}, err
	}
	domains, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return ApplicationDomain{}, err
	}
	var target *ApplicationDomain
	for i := range domains {
		if domains[i].ID == domainID {
			target = &domains[i]
			break
		}
	}
	if target == nil {
		return ApplicationDomain{}, ErrNotFound
	}
	next, err := normalizeDomainInput(app, in)
	if err != nil {
		return ApplicationDomain{}, err
	}
	next.ID = target.ID
	next.IsPrimary = target.IsPrimary
	next.Disabled = target.Disabled
	previousHost := target.Domain
	// Editing the host is the explicit conflict resolution: once the new host
	// passes the platform-wide claim, a row the uniqueness migration disabled
	// is re-enabled, mirroring the old base-domain update semantics.
	if next.Domain != previousHost {
		next.Disabled = false
	}
	releaseDomain, domainErr := s.guardDomains.Acquire(ctx, next.Domain)
	if domainErr != nil {
		return ApplicationDomain{}, fmt.Errorf("%w: %v", ErrValidation, domainErr)
	}
	defer releaseDomain()
	if next.Domain != previousHost {
		if err := s.checkDomainClaim(ctx, app.ID, next.Domain); err != nil {
			return ApplicationDomain{}, err
		}
	}
	var updated ApplicationDomain
	err = s.repo.InTx(ctx, func(tx Repository) error {
		row, err := tx.UpdateApplicationDomain(ctx, next)
		if err != nil {
			return err
		}
		updated = row
		// A moved host must not leave its default certificate behind: the
		// auto-created http-01 intent names the old host, and a stale order
		// would outrank the new one.
		if next.Domain != previousHost {
			if err := tx.DeleteAutoCertificateIntent(ctx, app.ID, previousHost); err != nil {
				return err
			}
		}
		return ensureHTTPSIntent(ctx, tx, app, row)
	})
	if err != nil {
		return ApplicationDomain{}, err
	}
	s.syncProxyServer(ctx, app.ServerID)
	return updated, nil
}

// ensureHTTPSIntent records the default per-host http-01 intent of an https
// domain when it has none. An operator's existing wildcard or dns-01 intent
// is left untouched (the insert is ON CONFLICT DO NOTHING); an http domain
// needs no order at all.
func ensureHTTPSIntent(ctx context.Context, repo Repository, app Application, domain ApplicationDomain) error {
	if domain.Protocol != ProtocolHTTPS {
		return nil
	}
	// An enabled wildcard intent already covers the new host with its SAN,
	// and the generator prefers an exact-host intent, so a per-host order
	// would outrank it.
	intents, err := repo.ListCertificateIntents(ctx, app.ID)
	if err != nil {
		return err
	}
	if hasEnabledWildcardIntent(intents) {
		return nil
	}
	return repo.CreateCertificateIntentIfAbsent(ctx, CertificateIntent{
		ApplicationID: app.ID,
		Domain:        domain.Domain,
		Enabled:       true,
		Challenge:     string(proxy.ChallengeHTTP01),
	})
}

// hasEnabledWildcardIntent reports whether the application already holds an
// enabled wildcard certificate intent. Such an intent's SAN covers a newly
// attached host, and the generator prefers an exact-host intent, so the
// default per-host HTTP-01 intent must be skipped (JUS-201).
func hasEnabledWildcardIntent(intents []CertificateIntent) bool {
	for _, intent := range intents {
		if intent.Enabled && intent.Wildcard {
			return true
		}
	}
	return false
}

// RemoveDomain detaches one hostname from an application. Removing the
// primary promotes the oldest remaining ENABLED row deterministically
// (created_at, id), or the oldest disabled one when every remaining row is
// disabled. Only the removed domain's routers disappear.
func (s *Service) RemoveDomain(ctx context.Context, userID, appID, domainID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	if domainID == uuid.Nil {
		return fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return err
	}
	domains, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return err
	}
	var target *ApplicationDomain
	for i := range domains {
		if domains[i].ID == domainID {
			target = &domains[i]
			break
		}
	}
	if target == nil {
		return ErrNotFound
	}
	err = s.repo.InTx(ctx, func(tx Repository) error {
		if err := tx.DeleteApplicationDomain(ctx, app.ID, target.ID); err != nil {
			return err
		}
		// The default TLS of a detached host goes with it (JUS-201): the
		// auto-created http-01 intent must not linger, or a re-attach would
		// collide with it. An operator's wildcard or dns-01 intent survives.
		if err := tx.DeleteAutoCertificateIntent(ctx, app.ID, target.Domain); err != nil {
			return err
		}
		if !target.IsPrimary {
			return nil
		}
		remaining, err := tx.ListApplicationDomains(ctx, app.ID)
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			return nil
		}
		promote := remaining[0]
		for _, domain := range remaining {
			if !domain.Disabled {
				promote = domain
				break
			}
		}
		if err := tx.ClearPrimaryApplicationDomains(ctx, app.ID); err != nil {
			return err
		}
		promote.IsPrimary = true
		_, err = tx.UpdateApplicationDomain(ctx, promote)
		return err
	})
	if err != nil {
		return err
	}
	s.syncProxyServer(ctx, app.ServerID)
	return nil
}

// SetPrimaryDomain makes one attached hostname the application's primary: the
// primary row is what display surfaces and preview provisioning use. A
// disabled row cannot be promoted: it lost a uniqueness conflict, and
// promoting it would not route it.
func (s *Service) SetPrimaryDomain(ctx context.Context, userID, appID, domainID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	if domainID == uuid.Nil {
		return fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return err
	}
	domains, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return err
	}
	var target *ApplicationDomain
	for i := range domains {
		if domains[i].ID == domainID {
			target = &domains[i]
			break
		}
	}
	if target == nil {
		return ErrNotFound
	}
	if target.Disabled {
		return fmt.Errorf("%w: the domain is disabled by a duplicate-domain conflict; remove it or set a new domain", ErrValidation)
	}
	if target.IsPrimary {
		return nil
	}
	releaseDomain, domainErr := s.guardDomains.Acquire(ctx, target.Domain)
	if domainErr != nil {
		return fmt.Errorf("%w: %v", ErrValidation, domainErr)
	}
	defer releaseDomain()

	err = s.repo.InTx(ctx, func(tx Repository) error {
		if err := tx.ClearPrimaryApplicationDomains(ctx, app.ID); err != nil {
			return err
		}
		target.IsPrimary = true
		_, err := tx.UpdateApplicationDomain(ctx, *target)
		return err
	})
	if err != nil {
		return err
	}
	s.syncProxyServer(ctx, app.ServerID)
	return nil
}

// checkDomainClaim enforces the platform-wide uniqueness of a normalized host:
// a claim by another application is a 409, a claim by this application a 400,
// and a host shadowed by an enabled redirect source a 400 (two routers must
// never match the same host).
func (s *Service) checkDomainClaim(ctx context.Context, appID uuid.UUID, domain string) error {
	if claim, err := s.repo.DomainClaim(ctx, domain); err == nil {
		if claim.ApplicationID == appID {
			return fmt.Errorf("%w: the domain is already attached to this application", ErrValidation)
		}
		return ErrDomainConflict
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	sources, err := s.repo.ListRedirectSources(ctx)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if proxy.NormalizeDomain(source) == domain {
			return fmt.Errorf("%w: the domain is already used by a redirect rule", ErrValidation)
		}
	}
	return nil
}
