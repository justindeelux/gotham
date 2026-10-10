package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// ListProxiedApplications returns every application that declares a base
// domain, ordered by creation time so the proxy generator's input and its
// duplicate-domain disposition are deterministic. Each row carries the
// container of the newest running deployment (empty when none is running)
// and the disabled flag set by the domain-uniqueness migration.
func (s *Store) ListProxiedApplications(ctx context.Context) ([]sqlc.ListProxiedApplicationsRow, error) {
	return s.queries.ListProxiedApplications(ctx)
}

// PrepareProxyConfigVersion durably records the intent to make files the
// active configuration of a node, before the node is touched (R2). It is
// transactional: unchanged active content records nothing, a same-hash
// pending row is reused for a promotion retry, and any other pending row is
// replaced by a new one. It reports whether the node content differs from the
// stored active version (changed=false means the active snapshot already
// matches and must not be superseded).
func (s *Store) PrepareProxyConfigVersion(ctx context.Context, serverID pgtype.UUID, files []byte, contentHash string) (sqlc.ProxyConfigVersion, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	active, activeErr := queries.NewestActiveProxyConfigVersion(ctx, serverID)
	if activeErr != nil && !errors.Is(activeErr, pgx.ErrNoRows) {
		return sqlc.ProxyConfigVersion{}, false, activeErr
	}
	hasActive := activeErr == nil

	pending, pendingErr := queries.PendingProxyConfigVersion(ctx, serverID)
	if pendingErr != nil && !errors.Is(pendingErr, pgx.ErrNoRows) {
		return sqlc.ProxyConfigVersion{}, false, pendingErr
	}
	hasPending := pendingErr == nil

	switch {
	case hasActive && active.ContentHash == contentHash && !hasPending:
		// Unchanged: the active snapshot survives and nothing is recorded.
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return active, false, nil
	case hasActive && active.ContentHash == contentHash:
		// Restoration: the node may serve the pending content, so the active
		// row is pushed again and promoted (promotion clears the stale
		// pending row only after the push succeeds).
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return active, true, nil
	case hasPending && pending.ContentHash == contentHash:
		// Promotion retry of an ambiguous earlier push.
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return pending, true, nil
	}

	if err := queries.ClearPendingProxyConfigVersions(ctx, serverID); err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	row, err := queries.InsertPendingProxyConfigVersion(ctx, sqlc.InsertPendingProxyConfigVersionParams{
		ServerID:    serverID,
		Files:       files,
		ContentHash: contentHash,
	})
	if err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	return row, true, nil
}

// PromoteProxyConfigVersion activates a prepared version: the pending mark is
// cleared, the version becomes the active one, the previous active version is
// superseded (starting its retention window) and expired predecessors are
// pruned. Any failure rolls the whole promotion back.
func (s *Store) PromoteProxyConfigVersion(ctx context.Context, serverID, versionID pgtype.UUID, before pgtype.Timestamptz) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	if err := queries.PromoteProxyConfigVersion(ctx, versionID); err != nil {
		return err
	}
	if err := queries.ClearPendingProxyConfigVersions(ctx, serverID); err != nil {
		return err
	}
	if err := queries.SupersedeActiveProxyConfigVersions(ctx, sqlc.SupersedeActiveProxyConfigVersionsParams{
		ServerID: serverID,
		ID:       versionID,
	}); err != nil {
		return err
	}
	if err := queries.PruneSupersededProxyConfigVersions(ctx, sqlc.PruneSupersededProxyConfigVersionsParams{
		ServerID:     serverID,
		SupersededAt: before,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AbortProxyConfigVersion drops a pending record after a push that provably
// never touched the node.
func (s *Store) AbortProxyConfigVersion(ctx context.Context, serverID, versionID pgtype.UUID) error {
	return s.queries.DeleteProxyConfigVersion(ctx, sqlc.DeleteProxyConfigVersionParams{
		ID:       versionID,
		ServerID: serverID,
	})
}

// NewestActiveProxyConfigVersion returns the version the node is believed to
// serve.
func (s *Store) NewestActiveProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.NewestActiveProxyConfigVersion(ctx, serverID)
}

// PendingProxyConfigVersion returns the newest recorded-but-unpromoted
// version.
func (s *Store) PendingProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.PendingProxyConfigVersion(ctx, serverID)
}

// PreviousProxyConfigVersion returns the newest replaced predecessor.
func (s *Store) PreviousProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.PreviousProxyConfigVersion(ctx, serverID)
}

// CreateDNSProvider stores one sealed DNS provider credential and returns the
// row. The ciphertext is opened by the proxy service, never here.
func (s *Store) CreateDNSProvider(ctx context.Context, params sqlc.CreateDNSProviderParams) (sqlc.DnsProvider, error) {
	return s.queries.CreateDNSProvider(ctx, params)
}

// GetDNSProvider returns one DNS provider row.
func (s *Store) GetDNSProvider(ctx context.Context, id pgtype.UUID) (sqlc.DnsProvider, error) {
	return s.queries.GetDNSProvider(ctx, id)
}

// ListDNSProviders returns every configured DNS provider, newest first.
func (s *Store) ListDNSProviders(ctx context.Context) ([]sqlc.DnsProvider, error) {
	return s.queries.ListDNSProviders(ctx)
}

// UpdateDNSProvider persists the mutable provider fields and returns the row.
func (s *Store) UpdateDNSProvider(ctx context.Context, params sqlc.UpdateDNSProviderParams) (sqlc.DnsProvider, error) {
	return s.queries.UpdateDNSProvider(ctx, params)
}

// UpdateDNSProviderMeta persists the mutable provider fields without touching
// the sealed credential, so a non-rotation update cannot restore a stale
// ciphertext over a concurrent rotation.
func (s *Store) UpdateDNSProviderMeta(ctx context.Context, params sqlc.UpdateDNSProviderMetaParams) (sqlc.DnsProvider, error) {
	return s.queries.UpdateDNSProviderMeta(ctx, params)
}

// DeleteDNSProvider removes a provider row. Foreign keys keep certificate
// configs from dangling, but the service checks references first so the API
// can answer a clear conflict.
func (s *Store) DeleteDNSProvider(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteDNSProvider(ctx, id)
}

// CountEnabledDomainCertificatesByProvider counts enabled certificate configs
// still referencing a provider (the disable/delete/type-change guard).
func (s *Store) CountEnabledDomainCertificatesByProvider(ctx context.Context, providerID pgtype.UUID) (int64, error) {
	return s.queries.CountEnabledDomainCertificatesByProvider(ctx, providerID)
}

// CountDomainCertificatesByProvider counts every certificate config
// referencing a provider (the delete guard, including disabled configs).
func (s *Store) CountDomainCertificatesByProvider(ctx context.Context, providerID pgtype.UUID) (int64, error) {
	return s.queries.CountDomainCertificatesByProvider(ctx, providerID)
}

// ListEnabledDomainCertificatesByProvider returns the enabled certificate
// configs referencing a provider (the zone-narrowing guard).
func (s *Store) ListEnabledDomainCertificatesByProvider(ctx context.Context, providerID pgtype.UUID) ([]sqlc.DomainCertificate, error) {
	return s.queries.ListEnabledDomainCertificatesByProvider(ctx, providerID)
}

// CreateDomainCertificate stores one per-application certificate config.
func (s *Store) CreateDomainCertificate(ctx context.Context, params sqlc.CreateDomainCertificateParams) (sqlc.DomainCertificate, error) {
	return s.queries.CreateDomainCertificate(ctx, params)
}

// GetDomainCertificate returns one certificate config row.
func (s *Store) GetDomainCertificate(ctx context.Context, id pgtype.UUID) (sqlc.DomainCertificate, error) {
	return s.queries.GetDomainCertificate(ctx, id)
}

// ListDomainCertificatesByApplication returns one application's certificate
// intents, oldest first (JUS-89: one intent per domain).
func (s *Store) ListDomainCertificatesByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.DomainCertificate, error) {
	return s.queries.ListDomainCertificatesByApplication(ctx, applicationID)
}

// ListProxiedCertificates returns every certificate intent for the Traefik
// generator (JUS-89).
func (s *Store) ListProxiedCertificates(ctx context.Context) ([]sqlc.ListProxiedCertificatesRow, error) {
	return s.queries.ListProxiedCertificates(ctx)
}

// ListProxiedApplicationDomains returns every domain row joined to its
// application's node for the Traefik generator (JUS-89).
func (s *Store) ListProxiedApplicationDomains(ctx context.Context) ([]sqlc.ListProxiedApplicationDomainsRow, error) {
	return s.queries.ListProxiedApplicationDomains(ctx)
}

// ListApplicationDomainsByApplication returns one application's domain rows,
// primary first, then oldest first (JUS-89).
func (s *Store) ListApplicationDomainsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.ApplicationDomain, error) {
	return s.queries.ListApplicationDomainsByApplication(ctx, applicationID)
}

// GetApplicationDomainByNameAnyApp resolves the platform-wide claim on a
// host, or pgx.ErrNoRows.
func (s *Store) GetApplicationDomainByNameAnyApp(ctx context.Context, domain string) (sqlc.ApplicationDomain, error) {
	return s.queries.GetApplicationDomainByNameAnyApp(ctx, domain)
}

// CreateApplicationDomain stores one domain row.
func (s *Store) CreateApplicationDomain(ctx context.Context, params sqlc.CreateApplicationDomainParams) (sqlc.ApplicationDomain, error) {
	return s.queries.CreateApplicationDomain(ctx, params)
}

// UpdateApplicationDomain persists the mutable domain fields and returns the
// row.
func (s *Store) UpdateApplicationDomain(ctx context.Context, params sqlc.UpdateApplicationDomainParams) (sqlc.ApplicationDomain, error) {
	return s.queries.UpdateApplicationDomain(ctx, params)
}

// ClearPrimaryApplicationDomains drops the primary flag of an application
// before a promotion.
func (s *Store) ClearPrimaryApplicationDomains(ctx context.Context, applicationID pgtype.UUID) error {
	return s.queries.ClearPrimaryApplicationDomains(ctx, applicationID)
}

// DeleteApplicationDomain removes one domain row of an application.
func (s *Store) DeleteApplicationDomain(ctx context.Context, id, applicationID pgtype.UUID) error {
	return s.queries.DeleteApplicationDomain(ctx, sqlc.DeleteApplicationDomainParams{
		ID:            id,
		ApplicationID: applicationID,
	})
}

// DeleteApplicationDomainsByApplication removes every domain row of an
// application.
func (s *Store) DeleteApplicationDomainsByApplication(ctx context.Context, applicationID pgtype.UUID) error {
	return s.queries.DeleteApplicationDomainsByApplication(ctx, applicationID)
}

// ListDomainCertificates returns every certificate config, newest first.
func (s *Store) ListDomainCertificates(ctx context.Context) ([]sqlc.DomainCertificate, error) {
	return s.queries.ListDomainCertificates(ctx)
}

// UpdateDomainCertificate persists the mutable certificate fields and returns
// the row.
func (s *Store) UpdateDomainCertificate(ctx context.Context, params sqlc.UpdateDomainCertificateParams) (sqlc.DomainCertificate, error) {
	return s.queries.UpdateDomainCertificate(ctx, params)
}

// DeleteDomainCertificate removes one certificate config; the application
// routing itself is untouched.
func (s *Store) DeleteDomainCertificate(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteDomainCertificate(ctx, id)
}

// CreateDomainRedirect stores one per-application redirect rule.
func (s *Store) CreateDomainRedirect(ctx context.Context, params sqlc.CreateDomainRedirectParams) (sqlc.DomainRedirect, error) {
	return s.queries.CreateDomainRedirect(ctx, params)
}

// GetDomainRedirect returns one redirect rule row.
func (s *Store) GetDomainRedirect(ctx context.Context, id pgtype.UUID) (sqlc.DomainRedirect, error) {
	return s.queries.GetDomainRedirect(ctx, id)
}

// GetDomainRedirectBySource returns the rule claiming a source host, or
// pgx.ErrNoRows.
func (s *Store) GetDomainRedirectBySource(ctx context.Context, source string) (sqlc.DomainRedirect, error) {
	return s.queries.GetDomainRedirectBySource(ctx, source)
}

// ListDomainRedirects returns every redirect rule, newest first.
func (s *Store) ListDomainRedirects(ctx context.Context) ([]sqlc.DomainRedirect, error) {
	return s.queries.ListDomainRedirects(ctx)
}

// ListDomainRedirectsByApplication returns one application's redirect rules.
func (s *Store) ListDomainRedirectsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.DomainRedirect, error) {
	return s.queries.ListDomainRedirectsByApplication(ctx, applicationID)
}

// UpdateDomainRedirect persists the mutable redirect fields and returns the
// row.
func (s *Store) UpdateDomainRedirect(ctx context.Context, params sqlc.UpdateDomainRedirectParams) (sqlc.DomainRedirect, error) {
	return s.queries.UpdateDomainRedirect(ctx, params)
}

// DeleteDomainRedirect removes one redirect rule.
func (s *Store) DeleteDomainRedirect(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteDomainRedirect(ctx, id)
}

// ListApplicationBaseDomains returns every application's base domain
// (id + raw value) for the redirect ownership guard.
func (s *Store) ListApplicationBaseDomains(ctx context.Context) ([]sqlc.ListApplicationBaseDomainsRow, error) {
	return s.queries.ListApplicationBaseDomains(ctx)
}

// ListEnabledRedirects returns the endpoints of every enabled redirect rule,
// for the two-directional no-chain guard.
func (s *Store) ListEnabledRedirects(ctx context.Context) ([]sqlc.ListEnabledRedirectsRow, error) {
	return s.queries.ListEnabledRedirects(ctx)
}

// ListRedirectRules returns every redirect rule joined to its application's
// node state, for the proxy generator.
func (s *Store) ListRedirectRules(ctx context.Context) ([]sqlc.ListRedirectRulesRow, error) {
	return s.queries.ListRedirectRules(ctx)
}

// ListCertificateStatusTargets joins certificate intents to their
// application's node for the certificate status service.
func (s *Store) ListCertificateStatusTargets(ctx context.Context) ([]sqlc.ListCertificateStatusTargetsRow, error) {
	return s.queries.ListCertificateStatusTargets(ctx)
}
