package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// ListDomains returns one application's domains, primary first, then oldest
// first (JUS-89).
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

// AddDomain attaches one more hostname to an application (JUS-89). The host
// is validated (syntax), normalized (case-insensitive) and claimed
// platform-wide: a host any other application owns, or any enabled redirect
// source, answers 409, and a host already attached to this application
// answers 400. The first domain of a domainless application becomes its
// primary; every mutation resyncs the hosting node best effort.
func (s *Service) AddDomain(ctx context.Context, userID, appID uuid.UUID, domain string) (ApplicationDomain, error) {
	if !Enabled() {
		return ApplicationDomain{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return ApplicationDomain{}, err
	}
	next := proxy.NormalizeDomain(domain)
	if err := proxy.ValidateDomain(next); err != nil {
		return ApplicationDomain{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return ApplicationDomain{}, err
	}
	if err := s.checkDomainClaim(ctx, app.ID, next); err != nil {
		return ApplicationDomain{}, err
	}
	existing, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return ApplicationDomain{}, err
	}
	created, err := s.repo.CreateApplicationDomain(ctx, ApplicationDomain{
		ApplicationID: app.ID,
		Domain:        next,
		IsPrimary:     len(existing) == 0,
	})
	if err != nil {
		return ApplicationDomain{}, err
	}
	if created.IsPrimary {
		app.BaseDomain = next
		app.BaseDomainDisabled = false
		if _, err := s.repo.UpdateApplication(ctx, app); err != nil {
			return ApplicationDomain{}, err
		}
	}
	s.syncProxy(ctx, app)
	return created, nil
}

// RemoveDomain detaches one hostname from an application (JUS-89). Removing
// the primary promotes the oldest remaining row deterministically
// (created_at, id) and mirrors it onto base_domain; removing the last domain
// clears the mirror. Only the removed domain's routers disappear.
func (s *Service) RemoveDomain(ctx context.Context, userID, appID, domainID uuid.UUID) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	if domainID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return Application{}, err
	}
	domains, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return Application{}, err
	}
	var target *ApplicationDomain
	for i := range domains {
		if domains[i].ID == domainID {
			target = &domains[i]
			break
		}
	}
	if target == nil {
		return Application{}, ErrNotFound
	}
	if err := s.repo.DeleteApplicationDomain(ctx, app.ID, target.ID); err != nil {
		return Application{}, err
	}
	if target.IsPrimary {
		remaining := make([]ApplicationDomain, 0, len(domains)-1)
		for _, domain := range domains {
			if domain.ID != target.ID {
				remaining = append(remaining, domain)
			}
		}
		if len(remaining) == 0 {
			app.BaseDomain = ""
			app.BaseDomainDisabled = false
		} else {
			promoted, err := s.repo.SetPrimaryApplicationDomain(ctx, app.ID, remaining[0].ID)
			if err != nil {
				return Application{}, err
			}
			for _, domain := range promoted {
				if domain.IsPrimary {
					app.BaseDomain = domain.Domain
					app.BaseDomainDisabled = domain.Disabled
				}
			}
		}
		if _, err := s.repo.UpdateApplication(ctx, app); err != nil {
			return Application{}, err
		}
	}
	s.syncProxy(ctx, app)
	return app, nil
}

// SetPrimaryDomain makes one attached hostname the application's primary
// (JUS-89): the mirror, the legacy router name and the default certificate
// host follow it. A disabled row cannot be promoted: it lost a uniqueness
// conflict, and promoting it would not route it.
func (s *Service) SetPrimaryDomain(ctx context.Context, userID, appID, domainID uuid.UUID) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	if domainID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: invalid domain id", ErrValidation)
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	if app, err = s.repo.GetApplication(ctx, app.ID); err != nil {
		return Application{}, err
	}
	domains, err := s.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return Application{}, err
	}
	var target *ApplicationDomain
	for i := range domains {
		if domains[i].ID == domainID {
			target = &domains[i]
			break
		}
	}
	if target == nil {
		return Application{}, ErrNotFound
	}
	if target.Disabled {
		return Application{}, fmt.Errorf("%w: the domain is disabled by a duplicate-domain conflict; remove it or set a new domain", ErrValidation)
	}
	if target.IsPrimary {
		return app, nil
	}
	promoted, err := s.repo.SetPrimaryApplicationDomain(ctx, app.ID, target.ID)
	if err != nil {
		return Application{}, err
	}
	for _, domain := range promoted {
		if domain.IsPrimary {
			app.BaseDomain = domain.Domain
			app.BaseDomainDisabled = domain.Disabled
		}
	}
	updated, err := s.repo.UpdateApplication(ctx, app)
	if err != nil {
		return Application{}, err
	}
	s.syncProxy(ctx, updated)
	return updated, nil
}

// checkDomainClaim enforces the platform-wide uniqueness of a normalized host
// (JUS-89): a claim by another application is a 409, a claim by this
// application a 400, and a host shadowed by an enabled redirect source a 400
// (two routers must never match the same host).
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

// reconcilePrimaryRow makes the domain rows match a new primary value after
// a base_domain write: empty clears every row, otherwise the primary row
// takes the value (re-enabled, mirroring the explicit-change semantics), or
// is created when the application had none. A conflicting host surfaces as
// ErrDomainConflict before the application row is touched. When the value
// names an existing alias the rows swap: the alias is promoted (re-enabled)
// and the old primary row is dropped, so replace-semantics hold.
func (s *Service) reconcilePrimaryRow(ctx context.Context, appID uuid.UUID, primary string) error {
	if primary == "" {
		return s.repo.DeleteApplicationDomains(ctx, appID)
	}
	domains, err := s.repo.ListApplicationDomains(ctx, appID)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		if domain.Domain != primary || domain.IsPrimary {
			continue
		}
		if domain.Disabled {
			if _, err := s.repo.UpdateApplicationDomain(ctx, ApplicationDomain{
				ID:            domain.ID,
				ApplicationID: appID,
				Domain:        domain.Domain,
				IsPrimary:     domain.IsPrimary,
				Disabled:      false,
			}); err != nil {
				return err
			}
		}
		if _, err := s.repo.SetPrimaryApplicationDomain(ctx, appID, domain.ID); err != nil {
			return err
		}
		for _, old := range domains {
			if old.IsPrimary {
				if err := s.repo.DeleteApplicationDomain(ctx, appID, old.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, domain := range domains {
		if domain.IsPrimary {
			if domain.Domain == primary {
				return nil
			}
			_, err := s.repo.UpdateApplicationDomain(ctx, ApplicationDomain{
				ID:            domain.ID,
				ApplicationID: appID,
				Domain:        primary,
				IsPrimary:     true,
				Disabled:      false,
			})
			return err
		}
	}
	_, err = s.repo.CreateApplicationDomain(ctx, ApplicationDomain{
		ApplicationID: appID,
		Domain:        primary,
		IsPrimary:     true,
	})
	return err
}
