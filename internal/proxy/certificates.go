package proxy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// CreateCertificateInput is the validated input of a new certificate config.
// The recorded domain always comes from the application's current
// base_domain; callers never supply it.
type CreateCertificateInput struct {
	ApplicationID uuid.UUID
	// Enabled defaults to true when nil.
	Enabled *bool
	// Challenge defaults to http-01 when empty.
	Challenge ChallengeMode
	// DNSProviderID is required for dns-01 and must be empty for http-01.
	DNSProviderID uuid.UUID
	// Wildcard requests a wildcard certificate for the provider's zone; it is
	// only valid with dns-01.
	Wildcard bool
}

// UpdateCertificateInput is a partial update: nil fields stay unchanged. Any
// update re-records the application's current base_domain, so an intentional
// reconfiguration clears a stale-host divergence.
type UpdateCertificateInput struct {
	Enabled       *bool
	Challenge     *ChallengeMode
	DNSProviderID *uuid.UUID
	Wildcard      *bool
}

// CertificateService is the CRUD surface over domain_certificates.
type CertificateService interface {
	CreateCertificate(ctx context.Context, in CreateCertificateInput) (DomainCertificate, error)
	ListCertificates(ctx context.Context) ([]DomainCertificate, error)
	GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error)
	UpdateCertificate(ctx context.Context, id uuid.UUID, in UpdateCertificateInput) (DomainCertificate, error)
	DeleteCertificate(ctx context.Context, id uuid.UUID) error
}

// certificateDraft is the resolved configuration validated before a write.
type certificateDraft struct {
	applicationID uuid.UUID
	domain        string
	enabled       bool
	challenge     ChallengeMode
	providerID    uuid.UUID
	wildcard      bool
}

// Create validates and stores one certificate config for an application,
// inside the shared mutation boundary: the provider checks and the write are
// one critical section with the provider mutations, so an enabled config can
// never be inserted against a provider that a concurrent request disables.
func (s *sslService) CreateCertificate(ctx context.Context, in CreateCertificateInput) (DomainCertificate, error) {
	if in.ApplicationID == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: application_id is required", ErrValidation)
	}
	var certificate DomainCertificate
	err := s.mutate(ctx, func(ctx context.Context) error {
		app, err := s.store.GetApplication(ctx, in.ApplicationID)
		if err != nil {
			return err
		}
		if err := authorizeApp(ctx, app.TeamID, true); err != nil {
			return err
		}
		domain, err := certificateDomain(app)
		if err != nil {
			return err
		}

		draft := certificateDraft{
			applicationID: app.ID,
			domain:        domain,
			enabled:       true,
			challenge:     in.Challenge,
			providerID:    in.DNSProviderID,
			wildcard:      in.Wildcard,
		}
		if in.Enabled != nil {
			draft.enabled = *in.Enabled
		}
		if draft.challenge == "" {
			draft.challenge = ChallengeHTTP01
		}
		if err := s.validateCertificate(ctx, draft); err != nil {
			return err
		}
		if _, err := s.store.GetCertificateByApplication(ctx, draft.applicationID); err == nil {
			return fmt.Errorf("%w: the application already has a certificate configuration", ErrConflict)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}

		created, err := s.store.CreateCertificate(ctx, CertificateWrite{
			ApplicationID: draft.applicationID,
			Domain:        draft.domain,
			Enabled:       draft.enabled,
			Challenge:     draft.challenge,
			DNSProviderID: draft.providerID,
			Wildcard:      draft.wildcard,
		})
		if err != nil {
			return err
		}
		certificate = created
		return nil
	})
	if err != nil {
		return DomainCertificate{}, err
	}
	s.notifyResync(ctx)
	return certificate, nil
}

// List returns the certificate configs of the caller's active team, newest
// first.
func (s *sslService) ListCertificates(ctx context.Context) ([]DomainCertificate, error) {
	certificates, err := s.store.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}
	filtered := filterByTeam(ctx, s.store, certificates, func(c DomainCertificate) uuid.UUID {
		return c.ApplicationID
	})
	if filtered == nil {
		return []DomainCertificate{}, nil
	}
	return filtered, nil
}

// Get returns one certificate config of an application the caller's active
// team owns; a foreign team's certificate answers ErrNotFound.
func (s *sslService) GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error) {
	if id == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	certificate, err := s.store.GetCertificate(ctx, id)
	if err != nil {
		return DomainCertificate{}, err
	}
	app, err := s.store.GetApplication(ctx, certificate.ApplicationID)
	if err != nil {
		return DomainCertificate{}, err
	}
	if err := authorizeApp(ctx, app.TeamID, false); err != nil {
		return DomainCertificate{}, err
	}
	return certificate, nil
}

// Update applies a partial update inside the shared mutation boundary. The
// application is re-read so the recorded domain always matches the
// application's current base_domain; a domain change made through the deploy
// API therefore re-targets the certificate on the next explicit update
// instead of issuing for a stale host. Enabling a config is one critical
// section with the provider guards.
func (s *sslService) UpdateCertificate(ctx context.Context, id uuid.UUID, in UpdateCertificateInput) (DomainCertificate, error) {
	if id == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	var certificate DomainCertificate
	err := s.mutate(ctx, func(ctx context.Context) error {
		existing, err := s.store.GetCertificate(ctx, id)
		if err != nil {
			return err
		}
		app, err := s.store.GetApplication(ctx, existing.ApplicationID)
		if err != nil {
			return err
		}
		if err := authorizeApp(ctx, app.TeamID, true); err != nil {
			return err
		}
		domain, err := certificateDomain(app)
		if err != nil {
			return err
		}

		draft := certificateDraft{
			applicationID: app.ID,
			domain:        domain,
			enabled:       existing.Enabled,
			challenge:     existing.Challenge,
			providerID:    existing.DNSProviderID,
			wildcard:      existing.Wildcard,
		}
		if in.Enabled != nil {
			draft.enabled = *in.Enabled
		}
		if in.Challenge != nil {
			draft.challenge = *in.Challenge
			if draft.challenge == ChallengeHTTP01 {
				// Switching back to HTTP-01 drops the DNS provider reference
				// so the stored row always satisfies the schema constraint.
				draft.providerID = uuid.Nil
				draft.wildcard = false
			}
		}
		if in.DNSProviderID != nil {
			draft.providerID = *in.DNSProviderID
		}
		if in.Wildcard != nil {
			draft.wildcard = *in.Wildcard
		}
		if err := s.validateCertificate(ctx, draft); err != nil {
			return err
		}

		updated, err := s.store.UpdateCertificate(ctx, id, CertificateWrite{
			ApplicationID: draft.applicationID,
			Domain:        draft.domain,
			Enabled:       draft.enabled,
			Challenge:     draft.challenge,
			DNSProviderID: draft.providerID,
			Wildcard:      draft.wildcard,
		})
		if err != nil {
			return err
		}
		certificate = updated
		return nil
	})
	if err != nil {
		return DomainCertificate{}, err
	}
	s.notifyResync(ctx)
	return certificate, nil
}

// Delete removes one certificate config inside the shared mutation boundary;
// the application stays routable over plain HTTP exactly as in BE-6.1.
func (s *sslService) DeleteCertificate(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	err := s.mutate(ctx, func(ctx context.Context) error {
		existing, err := s.store.GetCertificate(ctx, id)
		if err != nil {
			return err
		}
		app, err := s.store.GetApplication(ctx, existing.ApplicationID)
		if err != nil {
			return err
		}
		if err := authorizeApp(ctx, app.TeamID, true); err != nil {
			return err
		}
		return s.store.DeleteCertificate(ctx, id)
	})
	if err != nil {
		return err
	}
	s.notifyResync(ctx)
	return nil
}

// validateCertificate enforces the challenge/provider matrix:
//
//   - http-01 uses the shared default resolver and must not name a provider;
//   - dns-01 requires a configured provider (enabled while the config is
//     enabled) whose zones cover the domain;
//   - wildcard is dns-01 only.
func (s *sslService) validateCertificate(ctx context.Context, draft certificateDraft) error {
	if err := ValidateDomain(draft.domain); err != nil {
		return fmt.Errorf("%w: invalid certificate domain: %v", ErrValidation, err)
	}
	if !ChallengeAllowed(draft.challenge) {
		return fmt.Errorf("%w: challenge must be %q or %q", ErrValidation, ChallengeHTTP01, ChallengeDNS01)
	}
	if draft.wildcard && draft.challenge != ChallengeDNS01 {
		return fmt.Errorf("%w: wildcard certificates require the dns-01 challenge", ErrValidation)
	}
	if draft.challenge == ChallengeHTTP01 {
		if draft.providerID != uuid.Nil {
			return fmt.Errorf("%w: http-01 does not use a DNS provider", ErrValidation)
		}
		return nil
	}
	if draft.providerID == uuid.Nil {
		return fmt.Errorf("%w: dns-01 requires a DNS provider", ErrValidation)
	}
	provider, err := s.store.GetProvider(ctx, draft.providerID)
	if err != nil {
		if err == ErrNotFound {
			return fmt.Errorf("%w: the DNS provider does not exist", ErrValidation)
		}
		return err
	}
	if draft.enabled && !provider.Enabled {
		return fmt.Errorf("%w: the DNS provider is disabled", ErrValidation)
	}
	if MatchZone(provider.Zones, draft.domain) == "" {
		return fmt.Errorf("%w: the domain is not under any zone served by the DNS provider", ErrValidation)
	}
	if draft.wildcard {
		// The wildcard base must sit inside a configured zone so the challenge
		// record can be written there, and the generated request always keeps
		// the exact host as the main name because a wildcard matches one label.
		if base, ok := WildcardBase(draft.domain, provider.Zones); !ok {
			return fmt.Errorf("%w: the wildcard base is not inside any zone served by the DNS provider", ErrValidation)
		} else if MatchZone(provider.Zones, base) == "" {
			return fmt.Errorf("%w: the wildcard base %q is not inside any zone served by the DNS provider", ErrValidation, base)
		}
	}
	return nil
}

// certificateDomain resolves the domain a certificate config records, from
// the application's current base_domain.
func certificateDomain(app ApplicationInfo) (string, error) {
	if app.DomainDisabled {
		return "", fmt.Errorf("%w: the application's domain is disabled; resolve the duplicate-domain conflict first", ErrValidation)
	}
	domain := NormalizeDomain(app.BaseDomain)
	if err := ValidateDomain(domain); err != nil {
		return "", fmt.Errorf("%w: the application has no valid base domain to certify", ErrValidation)
	}
	return domain, nil
}
