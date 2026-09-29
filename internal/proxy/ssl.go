package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// DNSProviderType is one entry of the DNS provider allowlist the generator
// knows how to configure. Traefik answers DNS-01 challenges through lego,
// which reads one credential variable per provider type; the environment
// mapping below is the only place that coupling lives.
type DNSProviderType string

const (
	// ProviderCloudflare is the Cloudflare DNS provider (lego "cloudflare").
	ProviderCloudflare DNSProviderType = "cloudflare"
	// ProviderDigitalOcean is the DigitalOcean DNS provider (lego
	// "digitalocean").
	ProviderDigitalOcean DNSProviderType = "digitalocean"
)

// ChallengeMode selects how a certificate is issued.
type ChallengeMode string

const (
	// ChallengeHTTP01 answers the ACME challenge through Traefik's HTTP
	// entrypoint with the shared default resolver.
	ChallengeHTTP01 ChallengeMode = "http-01"
	// ChallengeDNS01 answers the ACME challenge through TXT records written
	// by a configured DNS provider; it is required for wildcards.
	ChallengeDNS01 ChallengeMode = "dns-01"
)

// DNSResolverPrefix names the per-provider certificate resolvers in the
// generated static configuration; the provider type is appended, e.g.
// letsencrypt-dns-cloudflare.
const DNSResolverPrefix = "letsencrypt-dns-"

// MaxProviderZones bounds one provider's zone list so a typo cannot turn the
// generator input into an unbounded document.
const MaxProviderZones = 32

// maxCredentialBytes bounds a DNS API token before sealing.
const maxCredentialBytes = 4096

// maxProviderName bounds the operator-facing provider label.
const maxProviderName = 128

// dnsProviderEnv maps a provider type to the environment variable Traefik
// (lego) reads the credential from. The value is the only credential channel:
// it must never appear in generated configuration, logs or API responses.
var dnsProviderEnv = map[DNSProviderType]string{
	ProviderCloudflare:   "CF_DNS_API_TOKEN",
	ProviderDigitalOcean: "DO_AUTH_TOKEN",
}

// DNSProviderTypeAllowed reports whether t is part of the allowlist.
func DNSProviderTypeAllowed(t DNSProviderType) bool {
	_, ok := dnsProviderEnv[t]
	return ok
}

// DNSProviderEnvVar returns the credential environment variable of a provider
// type, or "" for an unknown type.
func DNSProviderEnvVar(t DNSProviderType) string {
	return dnsProviderEnv[t]
}

// DNSResolverName returns the deterministic resolver name of a provider type,
// or "" for an unknown type.
func DNSResolverName(t DNSProviderType) string {
	if !DNSProviderTypeAllowed(t) {
		return ""
	}
	return DNSResolverPrefix + string(t)
}

// ChallengeAllowed reports whether mode is a supported challenge.
func ChallengeAllowed(mode ChallengeMode) bool {
	return mode == ChallengeHTTP01 || mode == ChallengeDNS01
}

// MatchZone returns the most specific configured zone the domain belongs to,
// or "" when no zone covers it. Matching is suffix-based on a label boundary
// (app.example.com is under example.com, notexample.com is not) and the
// longest match wins so overlapping zones stay deterministic.
func MatchZone(zones []string, domain string) string {
	domain = NormalizeDomain(domain)
	best := ""
	for _, zone := range zones {
		zone = NormalizeDomain(zone)
		if zone == "" {
			continue
		}
		if domain == zone || strings.HasSuffix(domain, "."+zone) {
			if len(zone) > len(best) {
				best = zone
			}
		}
	}
	return best
}

// normalizeZones trims, lowercases, validates and deduplicates a provider's
// zone list, returning it sorted so stored and generated state is stable.
func normalizeZones(zones []string) ([]string, error) {
	if len(zones) == 0 {
		return nil, fmt.Errorf("%w: at least one DNS zone is required", ErrValidation)
	}
	if len(zones) > MaxProviderZones {
		return nil, fmt.Errorf("%w: at most %d DNS zones are allowed", ErrValidation, MaxProviderZones)
	}
	seen := make(map[string]bool, len(zones))
	out := make([]string, 0, len(zones))
	for _, zone := range zones {
		normalized := NormalizeDomain(zone)
		if err := ValidateDomain(normalized); err != nil {
			return nil, fmt.Errorf("%w: invalid DNS zone %q", ErrValidation, strings.TrimSpace(zone))
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out, nil
}

// DNSProvider is one dns_providers row as this package uses it. The sealed
// credential is opened only in memory to build the Traefik container
// environment; it is never rendered, logged or returned through the API.
type DNSProvider struct {
	ID uuid.UUID
	// Provider is the allowlist type driving the resolver name and the
	// credential environment variable.
	Provider DNSProviderType
	// Name is the optional operator-facing label.
	Name string
	// Zones are the DNS zones the credential may write challenge records in.
	Zones []string
	// Enabled marks a provider usable for DNS-01.
	Enabled bool
	// SealedCredential is base64(nonce||ciphertext) from providers.SealSecret.
	SealedCredential string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DomainCertificate is one domain_certificates row as this package uses it.
// Domain records the application host the configuration was made for; the
// generator refuses to activate the certificate when the application's
// current base_domain no longer matches it.
type DomainCertificate struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Domain        string
	Enabled       bool
	Challenge     ChallengeMode
	// DNSProviderID is uuid.Nil for HTTP-01.
	DNSProviderID uuid.UUID
	Wildcard      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ApplicationInfo is the application state the certificate service validates
// a configuration against, and the redirect service validates ownership
// against.
type ApplicationInfo struct {
	ID uuid.UUID
	// TeamID is the team that owns the application; per-application SSL and
	// redirect resources authorize against it.
	TeamID         uuid.UUID
	BaseDomain     string
	DomainDisabled bool
	// ServerID is the node hosting the application; uuid.Nil when none is
	// assigned (a redirect rule cannot be generated then).
	ServerID uuid.UUID
}

// DNSProviderWrite is the repository-level provider write shape.
type DNSProviderWrite struct {
	Provider         DNSProviderType
	Name             string
	Zones            []string
	SealedCredential string
	Enabled          bool
}

// CertificateWrite is the repository-level certificate write shape.
type CertificateWrite struct {
	ApplicationID uuid.UUID
	Domain        string
	Enabled       bool
	Challenge     ChallengeMode
	DNSProviderID uuid.UUID
	Wildcard      bool
}

// SSLStore is the persistence seam of the SSL surface (dns_providers and
// domain_certificates). The production implementation is storeSSL over
// *store.Store; tests substitute a fake.
type SSLStore interface {
	CreateProvider(ctx context.Context, in DNSProviderWrite) (DNSProvider, error)
	GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error)
	ListProviders(ctx context.Context) ([]DNSProvider, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error)
	// UpdateProviderMeta writes the mutable non-credential fields and leaves
	// the stored ciphertext untouched, so an update that does not rotate the
	// credential can never restore an older sealed value.
	UpdateProviderMeta(ctx context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error)
	DeleteProvider(ctx context.Context, id uuid.UUID) error
	CountCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error)
	CountEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error)
	ListEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) ([]DomainCertificate, error)

	CreateCertificate(ctx context.Context, in CertificateWrite) (DomainCertificate, error)
	GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error)
	GetCertificateByApplication(ctx context.Context, applicationID uuid.UUID) (DomainCertificate, error)
	ListCertificates(ctx context.Context) ([]DomainCertificate, error)
	UpdateCertificate(ctx context.Context, id uuid.UUID, in CertificateWrite) (DomainCertificate, error)
	DeleteCertificate(ctx context.Context, id uuid.UUID) error

	GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error)
}

// storeSSL adapts the shared store to the SSLStore seam.
type storeSSL struct {
	store *store.Store
}

// NewStoreSSL adapts the shared store to the SSLStore seam. It is exported so
// internal/server can build the production SSL services over one store.
func NewStoreSSL(st *store.Store) SSLStore {
	return storeSSL{store: st}
}

// CreateProvider stores one sealed provider row.
func (s storeSSL) CreateProvider(ctx context.Context, in DNSProviderWrite) (DNSProvider, error) {
	row, err := s.store.CreateDNSProvider(ctx, sqlc.CreateDNSProviderParams{
		Provider:   string(in.Provider),
		Name:       in.Name,
		Zones:      in.Zones,
		Ciphertext: in.SealedCredential,
		Enabled:    in.Enabled,
	})
	if err != nil {
		return DNSProvider{}, mapSSLWriteError(err)
	}
	return dnsProviderFromRow(row), nil
}

// GetProvider returns one provider row, or ErrNotFound.
func (s storeSSL) GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error) {
	row, err := s.store.GetDNSProvider(ctx, pgUUID(id))
	if err != nil {
		return DNSProvider{}, mapSSLReadError(err)
	}
	return dnsProviderFromRow(row), nil
}

// ListProviders returns every configured provider, newest first.
func (s storeSSL) ListProviders(ctx context.Context) ([]DNSProvider, error) {
	rows, err := s.store.ListDNSProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DNSProvider, 0, len(rows))
	for _, row := range rows {
		out = append(out, dnsProviderFromRow(row))
	}
	return out, nil
}

// UpdateProvider persists the mutable provider fields.
func (s storeSSL) UpdateProvider(ctx context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error) {
	row, err := s.store.UpdateDNSProvider(ctx, sqlc.UpdateDNSProviderParams{
		ID:         pgUUID(id),
		Provider:   string(in.Provider),
		Name:       in.Name,
		Zones:      in.Zones,
		Ciphertext: in.SealedCredential,
		Enabled:    in.Enabled,
	})
	if err != nil {
		return DNSProvider{}, mapSSLWriteError(err)
	}
	return dnsProviderFromRow(row), nil
}

// UpdateProviderMeta persists everything but the sealed credential.
func (s storeSSL) UpdateProviderMeta(ctx context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error) {
	row, err := s.store.UpdateDNSProviderMeta(ctx, sqlc.UpdateDNSProviderMetaParams{
		ID:       pgUUID(id),
		Provider: string(in.Provider),
		Name:     in.Name,
		Zones:    in.Zones,
		Enabled:  in.Enabled,
	})
	if err != nil {
		return DNSProvider{}, mapSSLWriteError(err)
	}
	return dnsProviderFromRow(row), nil
}

// DeleteProvider removes a provider row.
func (s storeSSL) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDNSProvider(ctx, pgUUID(id)); err != nil {
		return mapSSLWriteError(err)
	}
	return nil
}

// CountCertificatesByProvider counts every certificate config referencing a
// provider (the delete guard).
func (s storeSSL) CountCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error) {
	return s.store.CountDomainCertificatesByProvider(ctx, pgUUID(providerID))
}

// CountEnabledCertificatesByProvider counts enabled certificate configs
// referencing a provider (the disable/type-change guard).
func (s storeSSL) CountEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error) {
	return s.store.CountEnabledDomainCertificatesByProvider(ctx, pgUUID(providerID))
}

// ListEnabledCertificatesByProvider lists the enabled certificate configs
// referencing a provider (the zone-narrowing guard).
func (s storeSSL) ListEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) ([]DomainCertificate, error) {
	rows, err := s.store.ListEnabledDomainCertificatesByProvider(ctx, pgUUID(providerID))
	if err != nil {
		return nil, err
	}
	out := make([]DomainCertificate, 0, len(rows))
	for _, row := range rows {
		out = append(out, certificateFromRow(row))
	}
	return out, nil
}

// CreateCertificate stores one certificate config, mapping the unique
// application index to ErrConflict.
func (s storeSSL) CreateCertificate(ctx context.Context, in CertificateWrite) (DomainCertificate, error) {
	row, err := s.store.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: pgUUID(in.ApplicationID),
		Domain:        in.Domain,
		Enabled:       in.Enabled,
		Challenge:     string(in.Challenge),
		DnsProviderID: pgUUID(in.DNSProviderID),
		Wildcard:      in.Wildcard,
	})
	if err != nil {
		return DomainCertificate{}, mapSSLWriteError(err)
	}
	return certificateFromRow(row), nil
}

// GetCertificate returns one certificate config, or ErrNotFound.
func (s storeSSL) GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error) {
	row, err := s.store.GetDomainCertificate(ctx, pgUUID(id))
	if err != nil {
		return DomainCertificate{}, mapSSLReadError(err)
	}
	return certificateFromRow(row), nil
}

// GetCertificateByApplication returns the certificate config of an
// application, or ErrNotFound.
func (s storeSSL) GetCertificateByApplication(ctx context.Context, applicationID uuid.UUID) (DomainCertificate, error) {
	row, err := s.store.GetDomainCertificateByApplication(ctx, pgUUID(applicationID))
	if err != nil {
		return DomainCertificate{}, mapSSLReadError(err)
	}
	return certificateFromRow(row), nil
}

// ListCertificates returns every certificate config, newest first.
func (s storeSSL) ListCertificates(ctx context.Context) ([]DomainCertificate, error) {
	rows, err := s.store.ListDomainCertificates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DomainCertificate, 0, len(rows))
	for _, row := range rows {
		out = append(out, certificateFromRow(row))
	}
	return out, nil
}

// UpdateCertificate persists the mutable certificate fields.
func (s storeSSL) UpdateCertificate(ctx context.Context, id uuid.UUID, in CertificateWrite) (DomainCertificate, error) {
	row, err := s.store.UpdateDomainCertificate(ctx, sqlc.UpdateDomainCertificateParams{
		ID:            pgUUID(id),
		Domain:        in.Domain,
		Enabled:       in.Enabled,
		Challenge:     string(in.Challenge),
		DnsProviderID: pgUUID(in.DNSProviderID),
		Wildcard:      in.Wildcard,
	})
	if err != nil {
		return DomainCertificate{}, mapSSLWriteError(err)
	}
	return certificateFromRow(row), nil
}

// DeleteCertificate removes one certificate config.
func (s storeSSL) DeleteCertificate(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDomainCertificate(ctx, pgUUID(id)); err != nil {
		return mapSSLWriteError(err)
	}
	return nil
}

// GetApplication returns the application a certificate config targets, or
// ErrNotFound.
func (s storeSSL) GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error) {
	row, err := s.store.GetApplication(ctx, pgUUID(id))
	if err != nil {
		return ApplicationInfo{}, mapSSLReadError(err)
	}
	return applicationInfoFromRow(row), nil
}

// applicationInfoFromRow maps a stored application row into the validation
// view shared by the certificate and redirect services.
func applicationInfoFromRow(row sqlc.Application) ApplicationInfo {
	return ApplicationInfo{
		ID:             uuidFromPG(row.ID),
		TeamID:         uuidFromPG(row.TeamID),
		BaseDomain:     row.BaseDomain,
		DomainDisabled: row.BaseDomainDisabled,
		ServerID:       uuidFromPG(row.ServerID),
	}
}

// dnsProviderFromRow maps a stored provider row into the domain model.
func dnsProviderFromRow(row sqlc.DnsProvider) DNSProvider {
	provider := DNSProvider{
		ID:               uuidFromPG(row.ID),
		Provider:         DNSProviderType(row.Provider),
		Name:             row.Name,
		Zones:            append([]string{}, row.Zones...),
		Enabled:          row.Enabled,
		SealedCredential: row.Ciphertext,
	}
	if row.CreatedAt.Valid {
		provider.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		provider.UpdatedAt = row.UpdatedAt.Time
	}
	return provider
}

// certificateFromRow maps a stored certificate row into the domain model.
func certificateFromRow(row sqlc.DomainCertificate) DomainCertificate {
	cert := DomainCertificate{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		Domain:        row.Domain,
		Enabled:       row.Enabled,
		Challenge:     ChallengeMode(row.Challenge),
		DNSProviderID: uuidFromPG(row.DnsProviderID),
		Wildcard:      row.Wildcard,
	}
	if row.CreatedAt.Valid {
		cert.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		cert.UpdatedAt = row.UpdatedAt.Time
	}
	return cert
}

// mapSSLReadError maps a missing row to ErrNotFound.
func mapSSLReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// mapSSLWriteError turns unique and foreign-key violations into ErrConflict:
// the enabled-provider-per-type index, the one-certificate-per-application
// index and the provider reference all surface as actionable conflicts
// instead of a 500 (the service pre-checks cover the common paths; these
// mappings catch the races).
func mapSSLWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case "dns_providers_enabled_type_idx":
				return fmt.Errorf("%w: an enabled DNS provider of that type already exists", ErrConflict)
			case "domain_certificates_application_unique":
				return fmt.Errorf("%w: the application already has a certificate configuration", ErrConflict)
			}
			return fmt.Errorf("%w: the write conflicts with existing state", ErrConflict)
		case "23503":
			return fmt.Errorf("%w: the referenced object no longer exists", ErrConflict)
		}
	}
	return err
}

// secretUsable reports whether a deployment secret can key credential
// cryptography. An empty (or whitespace-only) secret is refused: the AES key
// would be SHA-256(""), a public value that gives stored tokens no
// confidentiality, and the environment fingerprint HMAC would be publicly
// derivable.
func secretUsable(secret string) bool {
	return strings.TrimSpace(secret) != ""
}

// sealCredential encrypts a plaintext DNS API token for storage. It refuses
// an unusable deployment secret instead of falling back to the well-known
// empty-string key.
func sealCredential(secret, plain string) (string, error) {
	if !secretUsable(secret) {
		return "", fmt.Errorf("%w: refusing to store DNS provider credentials", ErrSecret)
	}
	sealed, err := providers.SealSecret(secret, plain)
	if err != nil {
		return "", fmt.Errorf("proxy: seal DNS credential: %w", err)
	}
	return sealed, nil
}

// openCredential reverses sealCredential. An unusable deployment secret is
// refused as well, so a stored value can never be opened under the public
// empty key even if a legacy row was sealed with it.
func openCredential(secret, sealed string) (string, error) {
	if !secretUsable(secret) {
		return "", fmt.Errorf("%w: refusing to open DNS provider credentials", ErrSecret)
	}
	plain, err := providers.OpenSecret(secret, sealed)
	if err != nil {
		return "", fmt.Errorf("proxy: open DNS credential: %w", err)
	}
	return plain, nil
}
