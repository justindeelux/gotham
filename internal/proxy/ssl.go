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

type DNSProviderType string

const (
	ProviderCloudflare   DNSProviderType = "cloudflare"
	ProviderDigitalOcean DNSProviderType = "digitalocean"
)

type ChallengeMode string

const (
	ChallengeHTTP01 ChallengeMode = "http-01"
	ChallengeDNS01  ChallengeMode = "dns-01"
)

const DNSResolverPrefix = "letsencrypt-dns-"

const MaxProviderZones = 32

const maxCredentialBytes = 4096

const maxProviderName = 128

var dnsProviderEnv = map[DNSProviderType]string{
	ProviderCloudflare:   "CF_DNS_API_TOKEN",
	ProviderDigitalOcean: "DO_AUTH_TOKEN",
}

func DNSProviderTypeAllowed(t DNSProviderType) bool {
	_, ok := dnsProviderEnv[t]
	return ok
}

func DNSProviderEnvVar(t DNSProviderType) string {
	return dnsProviderEnv[t]
}

func DNSResolverName(t DNSProviderType) string {
	if !DNSProviderTypeAllowed(t) {
		return ""
	}
	return DNSResolverPrefix + string(t)
}

func ChallengeAllowed(mode ChallengeMode) bool {
	return mode == ChallengeHTTP01 || mode == ChallengeDNS01
}

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

type DNSProvider struct {
	ID               uuid.UUID
	Provider         DNSProviderType
	Name             string
	Zones            []string
	Enabled          bool
	SealedCredential string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type DomainCertificate struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Domain        string
	Enabled       bool
	Challenge     ChallengeMode
	DNSProviderID uuid.UUID
	Wildcard      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ApplicationInfo struct {
	ID             uuid.UUID
	DomainDisabled bool
	Domains        []string
	BaseDomain     string
	ServerID       uuid.UUID
}

func (a ApplicationInfo) PrimaryDomain() string {
	if len(a.Domains) > 0 {
		return a.Domains[0]
	}
	return NormalizeDomain(a.BaseDomain)
}

func (a ApplicationInfo) HasDomain(domain string) bool {
	if len(a.Domains) == 0 {
		return NormalizeDomain(a.BaseDomain) == domain
	}
	for _, owned := range a.Domains {
		if owned == domain {
			return true
		}
	}
	return false
}

type DNSProviderWrite struct {
	Provider         DNSProviderType
	Name             string
	Zones            []string
	SealedCredential string
	Enabled          bool
}

type CertificateWrite struct {
	ApplicationID uuid.UUID
	Domain        string
	Enabled       bool
	Challenge     ChallengeMode
	DNSProviderID uuid.UUID
	Wildcard      bool
}

type SSLStore interface {
	CreateProvider(ctx context.Context, in DNSProviderWrite) (DNSProvider, error)
	GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error)
	ListProviders(ctx context.Context) ([]DNSProvider, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error)
	UpdateProviderMeta(ctx context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error)
	DeleteProvider(ctx context.Context, id uuid.UUID) error
	CountCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error)
	CountEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error)
	ListEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) ([]DomainCertificate, error)

	CreateCertificate(ctx context.Context, in CertificateWrite) (DomainCertificate, error)
	GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error)
	ListCertificatesByApplication(ctx context.Context, applicationID uuid.UUID) ([]DomainCertificate, error)
	ListCertificates(ctx context.Context) ([]DomainCertificate, error)
	UpdateCertificate(ctx context.Context, id uuid.UUID, in CertificateWrite) (DomainCertificate, error)
	DeleteCertificate(ctx context.Context, id uuid.UUID) error
	BackfillApplicationCertificates(ctx context.Context) (int64, error)

	GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error)
}

type storeSSL struct {
	store *store.Store
}

func NewStoreSSL(st *store.Store) SSLStore {
	return storeSSL{store: st}
}

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

func (s storeSSL) GetProvider(ctx context.Context, id uuid.UUID) (DNSProvider, error) {
	row, err := s.store.GetDNSProvider(ctx, pgUUID(id))
	if err != nil {
		return DNSProvider{}, mapSSLReadError(err)
	}
	return dnsProviderFromRow(row), nil
}

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

func (s storeSSL) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDNSProvider(ctx, pgUUID(id)); err != nil {
		return mapSSLWriteError(err)
	}
	return nil
}

func (s storeSSL) CountCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error) {
	return s.store.CountDomainCertificatesByProvider(ctx, pgUUID(providerID))
}

func (s storeSSL) CountEnabledCertificatesByProvider(ctx context.Context, providerID uuid.UUID) (int64, error) {
	return s.store.CountEnabledDomainCertificatesByProvider(ctx, pgUUID(providerID))
}

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

func (s storeSSL) GetCertificate(ctx context.Context, id uuid.UUID) (DomainCertificate, error) {
	row, err := s.store.GetDomainCertificate(ctx, pgUUID(id))
	if err != nil {
		return DomainCertificate{}, mapSSLReadError(err)
	}
	return certificateFromRow(row), nil
}

func (s storeSSL) ListCertificatesByApplication(ctx context.Context, applicationID uuid.UUID) ([]DomainCertificate, error) {
	rows, err := s.store.ListDomainCertificatesByApplication(ctx, pgUUID(applicationID))
	if err != nil {
		return nil, err
	}
	out := make([]DomainCertificate, 0, len(rows))
	for _, row := range rows {
		out = append(out, certificateFromRow(row))
	}
	return out, nil
}

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

func (s storeSSL) DeleteCertificate(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDomainCertificate(ctx, pgUUID(id)); err != nil {
		return mapSSLWriteError(err)
	}
	return nil
}

func (s storeSSL) BackfillApplicationCertificates(ctx context.Context) (int64, error) {
	return s.store.BackfillApplicationCertificates(ctx)
}

func (s storeSSL) GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error) {
	row, err := s.store.GetApplication(ctx, pgUUID(id))
	if err != nil {
		return ApplicationInfo{}, mapSSLReadError(err)
	}
	info := applicationInfoFromRow(row)
	domains, err := s.store.ListApplicationDomainsByApplication(ctx, pgUUID(id))
	if err != nil {
		return ApplicationInfo{}, err
	}
	for _, domain := range domains {
		if !domain.Disabled {
			info.Domains = append(info.Domains, domain.Domain)
		}
	}
	info.DomainDisabled = len(domains) > 0 && len(info.Domains) == 0
	return info, nil
}

func applicationInfoFromRow(row sqlc.Application) ApplicationInfo {
	return ApplicationInfo{
		ID:       uuidFromPG(row.ID),
		ServerID: uuidFromPG(row.ServerID),
	}
}

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

func mapSSLReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func mapSSLWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case "dns_providers_enabled_type_idx":
				return fmt.Errorf("%w: an enabled DNS provider of that type already exists", ErrConflict)
			case "domain_certificates_app_domain_unique", "domain_certificates_application_unique":
				return fmt.Errorf("%w: the application already has a certificate configuration for that domain", ErrConflict)
			}
			return fmt.Errorf("%w: the write conflicts with existing state", ErrConflict)
		case "23503":
			return fmt.Errorf("%w: the referenced object no longer exists", ErrConflict)
		}
	}
	return err
}

func secretUsable(secret string) bool {
	return strings.TrimSpace(secret) != ""
}

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
