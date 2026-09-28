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

// Create validates and stores one certificate config for an application.
func (s *sslService) CreateCertificate(ctx context.Context, in CreateCertificateInput) (DomainCertificate, error) {
	if in.ApplicationID == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: application_id is required", ErrValidation)
	}
	app, err := s.store.GetApplication(ctx, in.ApplicationID)
	if err != nil {
		return DomainCertificate{}, err
	}
	domain, err := certificateDomain(app)
	if err != nil {
		return DomainCertificate{}, err
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
		return DomainCertificate{}, err
	}
	if _, err := s.store.GetCertificateByApplication(ctx, draft.applicationID); err == nil {
		return DomainCertificate{}, fmt.Errorf("%w: the application already has a certificate configuration", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return DomainCertificate{}, err
	}

	certificate, err := s.store.CreateCertificate(ctx, CertificateWrite{
		ApplicationID: draft.applicationID,
		Domain:        draft.domain,
		Enabled:       draft.enabled,
		Challenge:     draft.challenge,
		DNSProviderID: draft.providerID,
		Wildcard:      draft.wildcard,
	})
	if err != nil {
		return DomainCertificate{}, err
	}
	s.notifyResync(ctx)
	return certificate, nil
}

// List returns every certificate config, newest first.
func (s *sslService) ListCertificates(ctx context.Context) ([]DomainCertificate, error) {
	return s.store.ListCertificates(ctx)
}

// Get returns one certificate config.
func (s *sslService) GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error) {
	if id == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	return s.store.GetCertificate(ctx, id)
}

// Update applies a partial update. The application is re-read so the recorded
// domain always matches the application's current base_domain; a domain
// change made through the deploy API therefore re-targets the certificate on
// the next explicit update instead of issuing for a stale host.
func (s *sslService) UpdateCertificate(ctx context.Context, id uuid.UUID, in UpdateCertificateInput) (DomainCertificate, error) {
	if id == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	existing, err := s.store.GetCertificate(ctx, id)
	if err != nil {
		return DomainCertificate{}, err
	}
	app, err := s.store.GetApplication(ctx, existing.ApplicationID)
	if err != nil {
		return DomainCertificate{}, err
	}
	domain, err := certificateDomain(app)
	if err != nil {
		return DomainCertificate{}, err
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
			// Switching back to HTTP-01 drops the DNS provider reference so
			// the stored row always satisfies the schema constraint.
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
		return DomainCertificate{}, err
	}

	certificate, err := s.store.UpdateCertificate(ctx, id, CertificateWrite{
		ApplicationID: draft.applicationID,
		Domain:        draft.domain,
		Enabled:       draft.enabled,
		Challenge:     draft.challenge,
		DNSProviderID: draft.providerID,
		Wildcard:      draft.wildcard,
	})
	if err != nil {
		return DomainCertificate{}, err
	}
	s.notifyResync(ctx)
	return certificate, nil
}

// Delete removes one certificate config; the application stays routable over
// plain HTTP exactly as in BE-6.1.
func (s *sslService) DeleteCertificate(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	if _, err := s.store.GetCertificate(ctx, id); err != nil {
		return err
	}
	if err := s.store.DeleteCertificate(ctx, id); err != nil {
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
