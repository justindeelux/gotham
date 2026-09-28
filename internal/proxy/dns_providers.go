package proxy

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CreateDNSProviderInput is the validated input of a new DNS provider. The
// credential is plaintext here (write-only) and is sealed before storage.
type CreateDNSProviderInput struct {
	Provider DNSProviderType
	Name     string
	Zones    []string
	// Credential is the provider API token; it never leaves this process.
	Credential string
	// Enabled defaults to true when nil.
	Enabled *bool
}

// UpdateDNSProviderInput is a partial update: nil fields stay unchanged. A
// non-nil Credential rotates the sealed token.
type UpdateDNSProviderInput struct {
	Provider   *DNSProviderType
	Name       *string
	Zones      *[]string
	Credential *string
	Enabled    *bool
}

// DNSProviderService is the CRUD surface over dns_providers. Sealed
// credentials never appear in a returned value's API representation. The
// methods are resource-qualified so one service value can satisfy both SSL
// interfaces.
type DNSProviderService interface {
	CreateProvider(ctx context.Context, in CreateDNSProviderInput) (DNSProvider, error)
	ListProviders(ctx context.Context) ([]DNSProvider, error)
	GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, in UpdateDNSProviderInput) (DNSProvider, error)
	DeleteProvider(ctx context.Context, id uuid.UUID) error
}

// CreateProvider validates and stores one DNS provider.
func (s *sslService) CreateProvider(ctx context.Context, in CreateDNSProviderInput) (DNSProvider, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	write, err := s.providerWrite(in.Provider, in.Name, in.Zones, in.Credential, enabled)
	if err != nil {
		return DNSProvider{}, err
	}
	if enabled {
		// The partial unique index is the enforcement; this pre-check answers
		// a clear conflict in the common case (the index still catches the
		// racing create).
		existing, err := s.store.ListProviders(ctx)
		if err != nil {
			return DNSProvider{}, err
		}
		for _, provider := range existing {
			if provider.Enabled && provider.Provider == write.Provider {
				return DNSProvider{}, fmt.Errorf("%w: an enabled %s provider already exists", ErrConflict, write.Provider)
			}
		}
	}
	provider, err := s.store.CreateProvider(ctx, write)
	if err != nil {
		return DNSProvider{}, err
	}
	s.notifyResync(ctx)
	return provider, nil
}

// List returns every configured provider, newest first.
func (s *sslService) ListProviders(ctx context.Context) ([]DNSProvider, error) {
	return s.store.ListProviders(ctx)
}

// GetProvider returns one provider.
func (s *sslService) GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error) {
	if id == uuid.Nil {
		return DNSProvider{}, fmt.Errorf("%w: provider id is required", ErrValidation)
	}
	return s.store.GetProvider(ctx, id)
}

// UpdateProvider applies a partial update and enforces the reference guards:
// while an enabled certificate config references the provider, it cannot be
// disabled, retyped or narrowed to zones that no longer cover the configured
// domains. Credential rotation is always allowed.
func (s *sslService) UpdateProvider(ctx context.Context, id uuid.UUID, in UpdateDNSProviderInput) (DNSProvider, error) {
	if id == uuid.Nil {
		return DNSProvider{}, fmt.Errorf("%w: provider id is required", ErrValidation)
	}
	existing, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return DNSProvider{}, err
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
			return DNSProvider{}, err
		}
		next.Zones = zones
	}
	if in.Enabled != nil {
		next.Enabled = *in.Enabled
	}

	sealed := existing.SealedCredential
	if in.Credential != nil {
		credential, err := validateCredential(*in.Credential)
		if err != nil {
			return DNSProvider{}, err
		}
		sealed, err = sealCredential(s.secret, credential)
		if err != nil {
			return DNSProvider{}, err
		}
	}

	if err := s.validateProviderState(next); err != nil {
		return DNSProvider{}, err
	}
	if err := s.guardProviderChange(ctx, existing, next); err != nil {
		return DNSProvider{}, err
	}

	updated, err := s.store.UpdateProvider(ctx, id, DNSProviderWrite{
		Provider:         next.Provider,
		Name:             next.Name,
		Zones:            next.Zones,
		SealedCredential: sealed,
		Enabled:          next.Enabled,
	})
	if err != nil {
		return DNSProvider{}, err
	}
	s.notifyResync(ctx)
	return updated, nil
}

// DeleteProvider removes a provider that no certificate config references. A
// referenced provider is a conflict (including disabled certificate configs,
// whose foreign key would otherwise reject the delete after the fact).
func (s *sslService) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: provider id is required", ErrValidation)
	}
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
	if err := s.store.DeleteProvider(ctx, id); err != nil {
		return err
	}
	s.notifyResync(ctx)
	return nil
}

// providerWrite validates a full provider write shape and seals the credential.
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

// validateProviderState checks the allowlist, name bound and zones.
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

// guardProviderChange enforces the reference guards against the certificate
// configs that currently depend on the provider: while an enabled config
// references it, the provider cannot be disabled, retyped, or narrowed to
// zones that no longer cover the configured domains.
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

	// A zone narrowing can only strand certificates when the provider stays
	// enabled and keeps its type.
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

// validateCredential trims and bounds a plaintext API token.
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

// allowedProviderList renders the allowlist for error messages.
func allowedProviderList() string {
	return fmt.Sprintf("%q or %q", ProviderCloudflare, ProviderDigitalOcean)
}

// sameStrings reports slice equality (element-wise).
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
