package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) ListProxiedApplications(ctx context.Context) ([]sqlc.ListProxiedApplicationsRow, error) {
	return s.queries.ListProxiedApplications(ctx)
}

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
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return active, false, nil
	case hasActive && active.ContentHash == contentHash:
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return active, true, nil
	case hasPending && pending.ContentHash == contentHash:
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

func (s *Store) AbortProxyConfigVersion(ctx context.Context, serverID, versionID pgtype.UUID) error {
	return s.queries.DeleteProxyConfigVersion(ctx, sqlc.DeleteProxyConfigVersionParams{
		ID:       versionID,
		ServerID: serverID,
	})
}

func (s *Store) NewestActiveProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.NewestActiveProxyConfigVersion(ctx, serverID)
}

func (s *Store) PendingProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.PendingProxyConfigVersion(ctx, serverID)
}

func (s *Store) PreviousProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.PreviousProxyConfigVersion(ctx, serverID)
}

func (s *Store) CreateDNSProvider(ctx context.Context, params sqlc.CreateDNSProviderParams) (sqlc.DnsProvider, error) {
	return s.queries.CreateDNSProvider(ctx, params)
}

func (s *Store) GetDNSProvider(ctx context.Context, id pgtype.UUID) (sqlc.DnsProvider, error) {
	return s.queries.GetDNSProvider(ctx, id)
}

func (s *Store) ListDNSProviders(ctx context.Context) ([]sqlc.DnsProvider, error) {
	return s.queries.ListDNSProviders(ctx)
}

func (s *Store) UpdateDNSProvider(ctx context.Context, params sqlc.UpdateDNSProviderParams) (sqlc.DnsProvider, error) {
	return s.queries.UpdateDNSProvider(ctx, params)
}

func (s *Store) UpdateDNSProviderMeta(ctx context.Context, params sqlc.UpdateDNSProviderMetaParams) (sqlc.DnsProvider, error) {
	return s.queries.UpdateDNSProviderMeta(ctx, params)
}

func (s *Store) DeleteDNSProvider(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteDNSProvider(ctx, id)
}

func (s *Store) CountEnabledDomainCertificatesByProvider(ctx context.Context, providerID pgtype.UUID) (int64, error) {
	return s.queries.CountEnabledDomainCertificatesByProvider(ctx, providerID)
}

func (s *Store) CountDomainCertificatesByProvider(ctx context.Context, providerID pgtype.UUID) (int64, error) {
	return s.queries.CountDomainCertificatesByProvider(ctx, providerID)
}

func (s *Store) ListEnabledDomainCertificatesByProvider(ctx context.Context, providerID pgtype.UUID) ([]sqlc.DomainCertificate, error) {
	return s.queries.ListEnabledDomainCertificatesByProvider(ctx, providerID)
}

func (s *Store) CreateDomainCertificate(ctx context.Context, params sqlc.CreateDomainCertificateParams) (sqlc.DomainCertificate, error) {
	return s.queries.CreateDomainCertificate(ctx, params)
}

func (s *Store) CreateDomainCertificateIfAbsent(ctx context.Context, params sqlc.CreateDomainCertificateIfAbsentParams) (int64, error) {
	return s.queries.CreateDomainCertificateIfAbsent(ctx, params)
}

func (s *Store) DeleteAutoDomainCertificate(ctx context.Context, params sqlc.DeleteAutoDomainCertificateParams) (int64, error) {
	return s.queries.DeleteAutoDomainCertificate(ctx, params)
}

func (s *Store) BackfillApplicationCertificates(ctx context.Context) (int64, error) {
	return s.queries.BackfillApplicationCertificates(ctx)
}

func (s *Store) GetDomainCertificate(ctx context.Context, id pgtype.UUID) (sqlc.DomainCertificate, error) {
	return s.queries.GetDomainCertificate(ctx, id)
}

func (s *Store) ListDomainCertificatesByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.DomainCertificate, error) {
	return s.queries.ListDomainCertificatesByApplication(ctx, applicationID)
}

func (s *Store) ListProxiedCertificates(ctx context.Context) ([]sqlc.ListProxiedCertificatesRow, error) {
	return s.queries.ListProxiedCertificates(ctx)
}

func (s *Store) ListProxiedApplicationDomains(ctx context.Context) ([]sqlc.ListProxiedApplicationDomainsRow, error) {
	return s.queries.ListProxiedApplicationDomains(ctx)
}

func (s *Store) ListPrimaryApplicationDomains(ctx context.Context, applicationIDs []pgtype.UUID) ([]sqlc.ListPrimaryApplicationDomainsRow, error) {
	if len(applicationIDs) == 0 {
		return []sqlc.ListPrimaryApplicationDomainsRow{}, nil
	}
	return s.queries.ListPrimaryApplicationDomains(ctx, applicationIDs)
}

func (s *Store) ListApplicationDomainsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.ApplicationDomain, error) {
	return s.queries.ListApplicationDomainsByApplication(ctx, applicationID)
}

func (s *Store) GetApplicationDomainByNameAnyApp(ctx context.Context, domain string) (sqlc.ApplicationDomain, error) {
	return s.queries.GetApplicationDomainByNameAnyApp(ctx, domain)
}

func (s *Store) CreateApplicationDomain(ctx context.Context, params sqlc.CreateApplicationDomainParams) (sqlc.ApplicationDomain, error) {
	return s.queries.CreateApplicationDomain(ctx, params)
}

func (s *Store) UpdateApplicationDomain(ctx context.Context, params sqlc.UpdateApplicationDomainParams) (sqlc.ApplicationDomain, error) {
	return s.queries.UpdateApplicationDomain(ctx, params)
}

func (s *Store) ClearPrimaryApplicationDomains(ctx context.Context, applicationID pgtype.UUID) error {
	return s.queries.ClearPrimaryApplicationDomains(ctx, applicationID)
}

func (s *Store) DeleteApplicationDomain(ctx context.Context, id, applicationID pgtype.UUID) error {
	return s.queries.DeleteApplicationDomain(ctx, sqlc.DeleteApplicationDomainParams{
		ID:            id,
		ApplicationID: applicationID,
	})
}

func (s *Store) DeleteApplicationDomainsByApplication(ctx context.Context, applicationID pgtype.UUID) error {
	return s.queries.DeleteApplicationDomainsByApplication(ctx, applicationID)
}

func (s *Store) ListDomainCertificates(ctx context.Context) ([]sqlc.DomainCertificate, error) {
	return s.queries.ListDomainCertificates(ctx)
}

func (s *Store) UpdateDomainCertificate(ctx context.Context, params sqlc.UpdateDomainCertificateParams) (sqlc.DomainCertificate, error) {
	return s.queries.UpdateDomainCertificate(ctx, params)
}

func (s *Store) DeleteDomainCertificate(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteDomainCertificate(ctx, id)
}

func (s *Store) CreateDomainRedirect(ctx context.Context, params sqlc.CreateDomainRedirectParams) (sqlc.DomainRedirect, error) {
	return s.queries.CreateDomainRedirect(ctx, params)
}

func (s *Store) GetDomainRedirect(ctx context.Context, id pgtype.UUID) (sqlc.DomainRedirect, error) {
	return s.queries.GetDomainRedirect(ctx, id)
}

func (s *Store) GetDomainRedirectBySource(ctx context.Context, source string) (sqlc.DomainRedirect, error) {
	return s.queries.GetDomainRedirectBySource(ctx, source)
}

func (s *Store) ListDomainRedirects(ctx context.Context) ([]sqlc.DomainRedirect, error) {
	return s.queries.ListDomainRedirects(ctx)
}

func (s *Store) ListDomainRedirectsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.DomainRedirect, error) {
	return s.queries.ListDomainRedirectsByApplication(ctx, applicationID)
}

func (s *Store) UpdateDomainRedirect(ctx context.Context, params sqlc.UpdateDomainRedirectParams) (sqlc.DomainRedirect, error) {
	return s.queries.UpdateDomainRedirect(ctx, params)
}

func (s *Store) DeleteDomainRedirect(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteDomainRedirect(ctx, id)
}

func (s *Store) ListApplicationBaseDomains(ctx context.Context) ([]sqlc.ListApplicationBaseDomainsRow, error) {
	return s.queries.ListApplicationBaseDomains(ctx)
}

func (s *Store) ListEnabledRedirects(ctx context.Context) ([]sqlc.ListEnabledRedirectsRow, error) {
	return s.queries.ListEnabledRedirects(ctx)
}

func (s *Store) ListRedirectRules(ctx context.Context) ([]sqlc.ListRedirectRulesRow, error) {
	return s.queries.ListRedirectRules(ctx)
}

func (s *Store) ListCertificateStatusTargets(ctx context.Context) ([]sqlc.ListCertificateStatusTargetsRow, error) {
	return s.queries.ListCertificateStatusTargets(ctx)
}
