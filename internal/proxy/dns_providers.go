package proxy

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type CreateDNSProviderInput struct {
	Provider   DNSProviderType
	Name       string
	Zones      []string
	Credential string
	Enabled    *bool
}

type UpdateDNSProviderInput struct {
	Provider   *DNSProviderType
	Name       *string
	Zones      *[]string
	Credential *string
	Enabled    *bool
}

type DNSProviderService interface {
	CreateProvider(ctx context.Context, in CreateDNSProviderInput) (DNSProvider, error)
	ListProviders(ctx context.Context) ([]DNSProvider, error)
	GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, in UpdateDNSProviderInput) (DNSProvider, error)
	DeleteProvider(ctx context.Context, id uuid.UUID) error
}

func (s *sslService) CreateProvider(ctx context.Context, in CreateDNSProviderInput) (DNSProvider, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var provider DNSProvider
	err := s.mutate(ctx, func(ctx context.Context) error {
		write, err := s.providerWrite(in.Provider, in.Name, in.Zones, in.Credential, enabled)
		if err != nil {
			return err
		}
		if enabled {
			existing, err := s.store.ListProviders(ctx)
			if err != nil {
				return err
			}
			for _, provider := range existing {
				if provider.Enabled && provider.Provider == write.Provider {
					return fmt.Errorf("%w: an enabled %s provider already exists", ErrConflict, write.Provider)
				}
			}
		}
		created, err := s.store.CreateProvider(ctx, write)
		if err != nil {
			return err
		}
		provider = created
		return nil
	})
	if err != nil {
		return DNSProvider{}, err
	}
	s.notifyResync(ctx)
	return provider, nil
}

func (s *sslService) ListProviders(ctx context.Context) ([]DNSProvider, error) {
	return s.store.ListProviders(ctx)
}

func (s *sslService) GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error) {
	if id == uuid.Nil {
		return DNSProvider{}, fmt.Errorf("%w: provider id is required", ErrValidation)
	}
	return s.store.GetProvider(ctx, id)
}

func (s *sslService) UpdateProvider(ctx context.Context, id uuid.UUID, in UpdateDNSProviderInput) (DNSProvider, error) {
	if id == uuid.Nil {
		return DNSProvider{}, fmt.Errorf("%w: provider id is required", ErrValidation)
	}
	var updated DNSProvider
	err := s.mutate(ctx, func(ctx context.Context) error {
		existing, err := s.store.GetProvider(ctx, id)
		if err != nil {
			return err
		}

		next := existing
		if in.Provider != nil {
			next.Provider = *in.Provider
		}
		if in.Name != nil {
			next.Name = strings.TrimSpace(*in.Name)
		}
		if in.Zones != nil {
			zones, err := normalizeZones(*in.Zones)
			if err != nil {
				return err
			}
			next.Zones = zones
		}
		if in.Enabled != nil {
			next.Enabled = *in.Enabled
		}

		rotate := in.Credential != nil
		sealed := existing.SealedCredential
		if rotate {
			credential, err := validateCredential(*in.Credential)
			if err != nil {
				return err
			}
			sealed, err = sealCredential(s.secret, credential)
			if err != nil {
				return err
			}
		}

		if err := s.validateProviderState(next); err != nil {
			return err
		}
		if err := s.guardProviderChange(ctx, existing, next); err != nil {
			return err
		}

		write := DNSProviderWrite{
			Provider:         next.Provider,
			Name:             next.Name,
			Zones:            next.Zones,
			SealedCredential: sealed,
			Enabled:          next.Enabled,
		}
		if rotate {
			row, err := s.store.UpdateProvider(ctx, id, write)
			if err != nil {
				return err
			}
			updated = row
			return nil
		}
		row, err := s.store.UpdateProviderMeta(ctx, id, write)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return DNSProvider{}, err
	}
	s.notifyResync(ctx)
	return updated, nil
}

func (s *sslService) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: provider id is required", ErrValidation)
	}
	err := s.mutate(ctx, func(ctx context.Context) error {
		if _, err := s.store.GetProvider(ctx, id); err != nil {
			return err
		}
		count, err := s.store.CountCertificatesByProvider(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("%w: provider is referenced by %d certificate configuration(s); remove or re-point them first", ErrConflict, count)
		}
		return s.store.DeleteProvider(ctx, id)
	})
	if err != nil {
		return err
	}
	s.notifyResync(ctx)
	return nil
}

func (s *sslService) providerWrite(providerType DNSProviderType, name string, zones []string, credential string, enabled bool) (DNSProviderWrite, error) {
	plain, err := validateCredential(credential)
	if err != nil {
		return DNSProviderWrite{}, err
	}
	normalized, err := normalizeZones(zones)
	if err != nil {
		return DNSProviderWrite{}, err
	}
	next := DNSProvider{
		Provider: providerType,
		Name:     strings.TrimSpace(name),
		Zones:    normalized,
		Enabled:  enabled,
	}
	if err := s.validateProviderState(next); err != nil {
		return DNSProviderWrite{}, err
	}
	sealed, err := sealCredential(s.secret, plain)
	if err != nil {
		return DNSProviderWrite{}, err
	}
	return DNSProviderWrite{
		Provider:         next.Provider,
		Name:             next.Name,
		Zones:            next.Zones,
		SealedCredential: sealed,
		Enabled:          next.Enabled,
	}, nil
}

func (s *sslService) validateProviderState(provider DNSProvider) error {
	if !DNSProviderTypeAllowed(provider.Provider) {
		return fmt.Errorf("%w: provider must be one of %s", ErrValidation, allowedProviderList())
	}
	if len(provider.Name) > maxProviderName {
		return fmt.Errorf("%w: provider name exceeds %d characters", ErrValidation, maxProviderName)
	}
	if len(provider.Zones) == 0 {
		return fmt.Errorf("%w: at least one DNS zone is required", ErrValidation)
	}
	for _, zone := range provider.Zones {
		if err := ValidateDomain(zone); err != nil {
			return fmt.Errorf("%w: invalid DNS zone %q", ErrValidation, zone)
		}
	}
	return nil
}

func (s *sslService) guardProviderChange(ctx context.Context, existing, next DNSProvider) error {
	count, err := s.store.CountEnabledCertificatesByProvider(ctx, existing.ID)
	if err != nil {
		return err
	}
	if count > 0 {
		switch {
		case !next.Enabled:
			return fmt.Errorf("%w: provider is used by %d enabled certificate configuration(s); disable them first", ErrConflict, count)
		case next.Provider != existing.Provider:
			return fmt.Errorf("%w: provider type is used by %d enabled certificate configuration(s); disable them first", ErrConflict, count)
		}
	}

	if !next.Enabled || next.Provider != existing.Provider || sameStrings(existing.Zones, next.Zones) {
		return nil
	}
	certs, err := s.store.ListEnabledCertificatesByProvider(ctx, existing.ID)
	if err != nil {
		return err
	}
	for _, cert := range certs {
		if MatchZone(next.Zones, cert.Domain) == "" {
			return fmt.Errorf("%w: zone change would strand certificate configuration for %s; remove or re-point it first", ErrConflict, cert.Domain)
		}
	}
	return nil
}

func validateCredential(credential string) (string, error) {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", fmt.Errorf("%w: credential is required", ErrValidation)
	}
	if len(credential) > maxCredentialBytes {
		return "", fmt.Errorf("%w: credential exceeds %d bytes", ErrValidation, maxCredentialBytes)
	}
	return credential, nil
}

func allowedProviderList() string {
	return fmt.Sprintf("%q or %q", ProviderCloudflare, ProviderDigitalOcean)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
