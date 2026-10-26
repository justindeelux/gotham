package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

type DomainInput struct {
	Domain   string
	Port     int32
	Service  string
	Protocol string
}

func normalizeDomainInput(app Application, in DomainInput) (ApplicationDomain, error) {
	domain := proxy.NormalizeDomain(in.Domain)
	if err := proxy.ValidateDomain(domain); err != nil {
		return ApplicationDomain{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if in.Port < 0 || in.Port > 65535 {
		return ApplicationDomain{}, fmt.Errorf("%w: port must be between 0 and 65535", ErrValidation)
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
	s.markPendingChanges(ctx, app, PendingChangeDomains)
	s.syncProxyServer(ctx, app.ServerID)
	return created, nil
}

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
	if domainSettingsChanged(*target, next) {
		s.markPendingChanges(ctx, app, PendingChangeDomains)
	}
	s.syncProxyServer(ctx, app.ServerID)
	return updated, nil
}

func domainSettingsChanged(previous, next ApplicationDomain) bool {
	return previous.Domain != next.Domain ||
		previous.Port != next.Port ||
		previous.Service != next.Service ||
		previous.Protocol != next.Protocol
}

func ensureHTTPSIntent(ctx context.Context, repo Repository, app Application, domain ApplicationDomain) error {
	if domain.Protocol != ProtocolHTTPS {
		return nil
	}
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

func hasEnabledWildcardIntent(intents []CertificateIntent) bool {
	for _, intent := range intents {
		if intent.Enabled && intent.Wildcard {
			return true
		}
	}
	return false
}

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
	s.markPendingChanges(ctx, app, PendingChangeDomains)
	s.syncProxyServer(ctx, app.ServerID)
	return nil
}

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
	s.markPendingChanges(ctx, app, PendingChangeDomains)
	s.syncProxyServer(ctx, app.ServerID)
	return nil
}

func (s *Service) checkDomainClaim(ctx context.Context, appID uuid.UUID, domain string) error {
	claim, claimErr := s.repo.DomainClaim(ctx, domain)
	if claimErr != nil && !errors.Is(claimErr, ErrNotFound) {
		return claimErr
	}
	// Read both namespaces before comparing claims, regardless of their owner.
	sources, err := s.repo.ListRedirectSources(ctx)
	if err != nil {
		return err
	}
	if claimErr == nil {
		if claim.ApplicationID == appID {
			return fmt.Errorf("%w: the domain is already attached to this application", ErrValidation)
		}
		return ErrDomainConflict
	}
	for _, source := range sources {
		if proxy.NormalizeDomain(source) == domain {
			return fmt.Errorf("%w: the domain is already used by a redirect rule", ErrValidation)
		}
	}
	return nil
}
