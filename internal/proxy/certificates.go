package proxy

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type CreateCertificateInput struct {
	ApplicationID uuid.UUID
	ServiceID     uuid.UUID
	Domain        string
	Enabled       *bool
	Challenge     ChallengeMode
	DNSProviderID uuid.UUID
	Wildcard      bool
}

type UpdateCertificateInput struct {
	Domain        *string
	Enabled       *bool
	Challenge     *ChallengeMode
	DNSProviderID *uuid.UUID
	Wildcard      *bool
}

type CertificateService interface {
	CreateCertificate(ctx context.Context, in CreateCertificateInput) (DomainCertificate, error)
	ListCertificates(ctx context.Context) ([]DomainCertificate, error)
	GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error)
	UpdateCertificate(ctx context.Context, id uuid.UUID, in UpdateCertificateInput) (DomainCertificate, error)
	DeleteCertificate(ctx context.Context, id uuid.UUID) error
}

type certificateDraft struct {
	applicationID uuid.UUID
	serviceID     uuid.UUID
	domain        string
	enabled       bool
	challenge     ChallengeMode
	providerID    uuid.UUID
	wildcard      bool
}

// certificateOwner is the routing owner a certificate belongs to. The control
// plane is the owner with neither id set.
type certificateOwner struct {
	applicationID  uuid.UUID
	serviceID      uuid.UUID
	domainDisabled bool
	domains        []string
}

func (o certificateOwner) hasDomain(domain string) bool {
	for _, owned := range o.domains {
		if owned == domain {
			return true
		}
	}
	return false
}

func (o certificateOwner) defaultDomain() (string, error) {
	if o.domainDisabled {
		return "", fmt.Errorf("%w: the owner's domains are disabled; resolve the duplicate-domain conflict first", ErrValidation)
	}
	if len(o.domains) == 0 {
		return "", fmt.Errorf("%w: the owner has no valid base domain to certify", ErrValidation)
	}
	domain := NormalizeDomain(o.domains[0])
	if err := ValidateDomain(domain); err != nil {
		return "", fmt.Errorf("%w: the owner has no valid base domain to certify", ErrValidation)
	}
	return domain, nil
}

func (o certificateOwner) label() string {
	if o.serviceID != uuid.Nil {
		return "service"
	}
	return "application"
}

func ownerFromApplication(info ApplicationInfo) certificateOwner {
	domains := info.Domains
	if len(domains) == 0 && info.BaseDomain != "" {
		domains = []string{NormalizeDomain(info.BaseDomain)}
	}
	return certificateOwner{
		applicationID:  info.ID,
		domainDisabled: info.DomainDisabled,
		domains:        domains,
	}
}

func ownerFromService(info ServiceInfo) certificateOwner {
	return certificateOwner{
		serviceID:      info.ID,
		domainDisabled: info.DomainDisabled,
		domains:        info.Domains,
	}
}

func (s *sslService) certificateOwnerFor(ctx context.Context, applicationID, serviceID uuid.UUID) (certificateOwner, error) {
	if serviceID != uuid.Nil {
		info, err := s.store.GetService(ctx, serviceID)
		if err != nil {
			return certificateOwner{}, err
		}
		return ownerFromService(info), nil
	}
	info, err := s.store.GetApplication(ctx, applicationID)
	if err != nil {
		return certificateOwner{}, err
	}
	return ownerFromApplication(info), nil
}

func (s *sslService) certificatesByOwner(ctx context.Context, owner certificateOwner) ([]DomainCertificate, error) {
	if owner.serviceID != uuid.Nil {
		return s.store.ListCertificatesByService(ctx, owner.serviceID)
	}
	return s.store.ListCertificatesByApplication(ctx, owner.applicationID)
}

func (s *sslService) CreateCertificate(ctx context.Context, in CreateCertificateInput) (DomainCertificate, error) {
	if in.ApplicationID == uuid.Nil && in.ServiceID == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: application_id or service_id is required", ErrValidation)
	}
	if in.ApplicationID != uuid.Nil && in.ServiceID != uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: application_id and service_id are mutually exclusive", ErrValidation)
	}
	var certificate DomainCertificate
	err := s.mutate(ctx, func(ctx context.Context) error {
		owner, err := s.certificateOwnerFor(ctx, in.ApplicationID, in.ServiceID)
		if err != nil {
			return err
		}
		domain := NormalizeDomain(in.Domain)
		if domain == "" {
			domain, err = owner.defaultDomain()
			if err != nil {
				return err
			}
		} else {
			if err := ValidateDomain(domain); err != nil {
				return fmt.Errorf("%w: invalid certificate domain: %v", ErrValidation, err)
			}
			if !owner.hasDomain(domain) {
				return fmt.Errorf("%w: %q is not attached to the %s", ErrValidation, domain, owner.label())
			}
		}

		draft := certificateDraft{
			applicationID: owner.applicationID,
			serviceID:     owner.serviceID,
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
		existing, err := s.certificatesByOwner(ctx, owner)
		if err != nil {
			return err
		}
		for _, certificate := range existing {
			if certificate.Domain == draft.domain {
				return fmt.Errorf("%w: the %s already has a certificate configuration for that domain", ErrConflict, owner.label())
			}
		}

		created, err := s.store.CreateCertificate(ctx, CertificateWrite{
			ApplicationID: draft.applicationID,
			ServiceID:     draft.serviceID,
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

func (s *sslService) ListCertificates(ctx context.Context) ([]DomainCertificate, error) {
	certificates, err := s.store.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}
	visible := make([]DomainCertificate, 0, len(certificates))
	panel := panelCertificateContext(ctx)
	for _, cert := range certificates {
		controlPlane := cert.ApplicationID == uuid.Nil && cert.ServiceID == uuid.Nil
		if controlPlane == panel {
			visible = append(visible, cert)
		}
	}
	return visible, nil
}

func (s *sslService) GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error) {
	if id == uuid.Nil {
		return DomainCertificate{}, fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	certificate, err := s.store.GetCertificate(ctx, id)
	if err != nil {
		return DomainCertificate{}, err
	}
	return certificate, nil
}

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
		owner, err := s.certificateOwner(ctx, existing)
		if err != nil {
			return err
		}

		domain := existing.Domain
		if in.Domain != nil {
			domain = NormalizeDomain(*in.Domain)
			if err := ValidateDomain(domain); err != nil {
				return fmt.Errorf("%w: invalid certificate domain: %v", ErrValidation, err)
			}
		}
		if !owner.hasDomain(domain) {
			return fmt.Errorf("%w: %q is not attached to the %s", ErrValidation, domain, owner.label())
		}

		draft := certificateDraft{
			applicationID: owner.applicationID,
			serviceID:     owner.serviceID,
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
		if draft.domain != existing.Domain {
			siblings, err := s.certificatesByOwner(ctx, owner)
			if err != nil {
				return err
			}
			for _, sibling := range siblings {
				if sibling.ID != id && sibling.Domain == draft.domain {
					return fmt.Errorf("%w: the %s already has a certificate configuration for that domain", ErrConflict, owner.label())
				}
			}
		}

		updated, err := s.store.UpdateCertificate(ctx, id, CertificateWrite{
			ApplicationID: draft.applicationID,
			ServiceID:     draft.serviceID,
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

func (s *sslService) DeleteCertificate(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: certificate id is required", ErrValidation)
	}
	err := s.mutate(ctx, func(ctx context.Context) error {
		existing, err := s.store.GetCertificate(ctx, id)
		if err != nil {
			return err
		}
		if _, err := s.certificateOwner(ctx, existing); err != nil {
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
		if base, ok := WildcardBase(draft.domain, provider.Zones); !ok {
			return fmt.Errorf("%w: the wildcard base is not inside any zone served by the DNS provider", ErrValidation)
		} else if MatchZone(provider.Zones, base) == "" {
			return fmt.Errorf("%w: the wildcard base %q is not inside any zone served by the DNS provider", ErrValidation, base)
		}
	}
	return nil
}
