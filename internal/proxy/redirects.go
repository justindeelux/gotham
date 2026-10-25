package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type DomainRedirect struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	SourceDomain  string
	TargetDomain  string
	Code          int
	PreservePath  bool
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateRedirectInput struct {
	ApplicationID uuid.UUID
	SourceDomain  string
	TargetDomain  string
	Code          int
	PreservePath  *bool
	Enabled       *bool
}

type UpdateRedirectInput struct {
	SourceDomain *string
	TargetDomain *string
	Code         *int
	PreservePath *bool
	Enabled      *bool
}

type RedirectService interface {
	CreateRedirect(ctx context.Context, in CreateRedirectInput) (DomainRedirect, error)
	ListRedirects(ctx context.Context, applicationID uuid.UUID) ([]DomainRedirect, error)
	GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error)
	UpdateRedirect(ctx context.Context, id uuid.UUID, in UpdateRedirectInput) (DomainRedirect, error)
	DeleteRedirect(ctx context.Context, id uuid.UUID) error
}

type RedirectWrite struct {
	ApplicationID uuid.UUID
	SourceDomain  string
	TargetDomain  string
	Code          int
	PreservePath  bool
	Enabled       bool
}

type ApplicationDomain struct {
	ID     uuid.UUID
	Domain string
}

type RedirectEndpoint struct {
	ID     uuid.UUID
	Source string
	Target string
}

type RedirectStore interface {
	GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error)
	CreateRedirect(ctx context.Context, in RedirectWrite) (DomainRedirect, error)
	GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error)
	GetRedirectBySource(ctx context.Context, source string) (DomainRedirect, error)
	ListRedirects(ctx context.Context) ([]DomainRedirect, error)
	ListRedirectsByApplication(ctx context.Context, applicationID uuid.UUID) ([]DomainRedirect, error)
	UpdateRedirect(ctx context.Context, id uuid.UUID, in RedirectWrite) (DomainRedirect, error)
	DeleteRedirect(ctx context.Context, id uuid.UUID) error
	ListApplicationBaseDomains(ctx context.Context) ([]ApplicationDomain, error)
	ListEnabledRedirects(ctx context.Context) ([]RedirectEndpoint, error)
}

type storeRedirect struct {
	store *store.Store
}

func NewStoreRedirect(st *store.Store) RedirectStore {
	return storeRedirect{store: st}
}

func (s storeRedirect) CreateRedirect(ctx context.Context, in RedirectWrite) (DomainRedirect, error) {
	row, err := s.store.CreateDomainRedirect(ctx, sqlc.CreateDomainRedirectParams{
		ApplicationID: pgUUID(in.ApplicationID),
		SourceDomain:  in.SourceDomain,
		TargetDomain:  in.TargetDomain,
		Code:          int16(in.Code),
		PreservePath:  in.PreservePath,
		Enabled:       in.Enabled,
	})
	if err != nil {
		return DomainRedirect{}, mapRedirectWriteError(err)
	}
	return redirectFromRow(row), nil
}

func (s storeRedirect) GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error) {
	row, err := s.store.GetDomainRedirect(ctx, pgUUID(id))
	if err != nil {
		return DomainRedirect{}, mapSSLReadError(err)
	}
	return redirectFromRow(row), nil
}

func (s storeRedirect) GetRedirectBySource(ctx context.Context, source string) (DomainRedirect, error) {
	row, err := s.store.GetDomainRedirectBySource(ctx, source)
	if err != nil {
		return DomainRedirect{}, mapSSLReadError(err)
	}
	return redirectFromRow(row), nil
}

func (s storeRedirect) ListRedirects(ctx context.Context) ([]DomainRedirect, error) {
	rows, err := s.store.ListDomainRedirects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DomainRedirect, 0, len(rows))
	for _, row := range rows {
		out = append(out, redirectFromRow(row))
	}
	return out, nil
}

func (s storeRedirect) ListRedirectsByApplication(ctx context.Context, applicationID uuid.UUID) ([]DomainRedirect, error) {
	rows, err := s.store.ListDomainRedirectsByApplication(ctx, pgUUID(applicationID))
	if err != nil {
		return nil, err
	}
	out := make([]DomainRedirect, 0, len(rows))
	for _, row := range rows {
		out = append(out, redirectFromRow(row))
	}
	return out, nil
}

func (s storeRedirect) UpdateRedirect(ctx context.Context, id uuid.UUID, in RedirectWrite) (DomainRedirect, error) {
	row, err := s.store.UpdateDomainRedirect(ctx, sqlc.UpdateDomainRedirectParams{
		ID:           pgUUID(id),
		SourceDomain: in.SourceDomain,
		TargetDomain: in.TargetDomain,
		Code:         int16(in.Code),
		PreservePath: in.PreservePath,
		Enabled:      in.Enabled,
	})
	if err != nil {
		return DomainRedirect{}, mapRedirectWriteError(err)
	}
	return redirectFromRow(row), nil
}

func (s storeRedirect) DeleteRedirect(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDomainRedirect(ctx, pgUUID(id)); err != nil {
		return mapRedirectWriteError(err)
	}
	return nil
}

func (s storeRedirect) ListApplicationBaseDomains(ctx context.Context) ([]ApplicationDomain, error) {
	rows, err := s.store.ListApplicationBaseDomains(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ApplicationDomain, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApplicationDomain{ID: uuidFromPG(row.ID), Domain: row.BaseDomain})
	}
	return out, nil
}

func (s storeRedirect) ListEnabledRedirects(ctx context.Context) ([]RedirectEndpoint, error) {
	rows, err := s.store.ListEnabledRedirects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RedirectEndpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, RedirectEndpoint{
			ID:     uuidFromPG(row.ID),
			Source: row.SourceDomain,
			Target: row.TargetDomain,
		})
	}
	return out, nil
}

func (s storeRedirect) GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error) {
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

func redirectFromRow(row sqlc.DomainRedirect) DomainRedirect {
	redirect := DomainRedirect{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		SourceDomain:  row.SourceDomain,
		TargetDomain:  row.TargetDomain,
		Code:          int(row.Code),
		PreservePath:  row.PreservePath,
		Enabled:       row.Enabled,
	}
	if row.CreatedAt.Valid {
		redirect.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		redirect.UpdatedAt = row.UpdatedAt.Time
	}
	return redirect
}

func mapRedirectWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: the source host is already used by another redirect rule", ErrConflict)
		case "23503":
			return fmt.Errorf("%w: the referenced application no longer exists", ErrConflict)
		}
	}
	return err
}

type RedirectConfig struct {
	GuardDomains  DomainGuard
	Store         RedirectStore
	Logger        *slog.Logger
	Resync        func(ctx context.Context) error
	ResyncTimeout time.Duration
}

type redirectService struct {
	guardDomains  DomainGuard
	store         RedirectStore
	logger        *slog.Logger
	resync        func(ctx context.Context) error
	resyncTimeout time.Duration
}

var _ RedirectService = (*redirectService)(nil)

func NewDefaultRedirectService(cfg RedirectConfig) RedirectService {
	if cfg.Store == nil || !Enabled() {
		return nil
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.ResyncTimeout
	if timeout <= 0 {
		timeout = defaultResyncTimeout
	}
	return &redirectService{guardDomains: cfg.GuardDomains, store: cfg.Store, logger: logger, resync: cfg.Resync, resyncTimeout: timeout}
}

func (s *redirectService) CreateRedirect(ctx context.Context, in CreateRedirectInput) (DomainRedirect, error) {
	if in.ApplicationID == uuid.Nil {
		return DomainRedirect{}, fmt.Errorf("%w: application_id is required", ErrValidation)
	}
	app, err := s.store.GetApplication(ctx, in.ApplicationID)
	if err != nil {
		return DomainRedirect{}, err
	}
	if err := validateRedirectApplication(app); err != nil {
		return DomainRedirect{}, err
	}

	code := in.Code
	if code == 0 {
		code = RedirectCodePermanent
	}
	if err := validateRedirectCode(code); err != nil {
		return DomainRedirect{}, err
	}
	preservePath := true
	if in.PreservePath != nil {
		preservePath = *in.PreservePath
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	source, target, err := validateRedirectDomains(in.SourceDomain, in.TargetDomain)
	if err != nil {
		return DomainRedirect{}, err
	}
	releaseDomain, domainErr := s.guardDomains.Acquire(ctx, source)
	if domainErr != nil {
		return DomainRedirect{}, fmt.Errorf("%w: %v", ErrConflict, domainErr)
	}
	defer releaseDomain()
	if err := s.checkRedirectWrites(ctx, uuid.Nil, source, target, enabled); err != nil {
		return DomainRedirect{}, err
	}

	created, err := s.store.CreateRedirect(ctx, RedirectWrite{
		ApplicationID: app.ID,
		SourceDomain:  source,
		TargetDomain:  target,
		Code:          code,
		PreservePath:  preservePath,
		Enabled:       enabled,
	})
	if err != nil {
		return DomainRedirect{}, err
	}
	s.notifyResync(ctx)
	return created, nil
}

func (s *redirectService) ListRedirects(ctx context.Context, applicationID uuid.UUID) ([]DomainRedirect, error) {
	var (
		redirects []DomainRedirect
		err       error
	)
	if applicationID == uuid.Nil {
		redirects, err = s.store.ListRedirects(ctx)
	} else {
		redirects, err = s.store.ListRedirectsByApplication(ctx, applicationID)
	}
	if err != nil {
		return nil, err
	}
	return redirects, nil
}

func (s *redirectService) GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error) {
	if id == uuid.Nil {
		return DomainRedirect{}, fmt.Errorf("%w: redirect id is required", ErrValidation)
	}
	redirect, err := s.store.GetRedirect(ctx, id)
	if err != nil {
		return DomainRedirect{}, err
	}
	return redirect, nil
}

func (s *redirectService) UpdateRedirect(ctx context.Context, id uuid.UUID, in UpdateRedirectInput) (DomainRedirect, error) {
	if id == uuid.Nil {
		return DomainRedirect{}, fmt.Errorf("%w: redirect id is required", ErrValidation)
	}
	existing, err := s.store.GetRedirect(ctx, id)
	if err != nil {
		return DomainRedirect{}, err
	}
	app, err := s.store.GetApplication(ctx, existing.ApplicationID)
	if err != nil {
		return DomainRedirect{}, err
	}
	if err := validateRedirectApplication(app); err != nil {
		return DomainRedirect{}, err
	}

	draft := RedirectWrite{
		ApplicationID: existing.ApplicationID,
		SourceDomain:  existing.SourceDomain,
		TargetDomain:  existing.TargetDomain,
		Code:          existing.Code,
		PreservePath:  existing.PreservePath,
		Enabled:       existing.Enabled,
	}
	if in.SourceDomain != nil {
		draft.SourceDomain = *in.SourceDomain
	}
	if in.TargetDomain != nil {
		draft.TargetDomain = *in.TargetDomain
	}
	if in.Code != nil {
		draft.Code = *in.Code
	}
	if in.PreservePath != nil {
		draft.PreservePath = *in.PreservePath
	}
	if in.Enabled != nil {
		draft.Enabled = *in.Enabled
	}

	source, target, err := validateRedirectDomains(draft.SourceDomain, draft.TargetDomain)
	if err != nil {
		return DomainRedirect{}, err
	}
	if err := validateRedirectCode(draft.Code); err != nil {
		return DomainRedirect{}, err
	}
	draft.SourceDomain, draft.TargetDomain = source, target
	releaseDomain, domainErr := s.guardDomains.Acquire(ctx, source)
	if domainErr != nil {
		return DomainRedirect{}, fmt.Errorf("%w: %v", ErrConflict, domainErr)
	}
	defer releaseDomain()
	if err := s.checkRedirectWrites(ctx, id, source, target, draft.Enabled); err != nil {
		return DomainRedirect{}, err
	}

	updated, err := s.store.UpdateRedirect(ctx, id, draft)
	if err != nil {
		return DomainRedirect{}, err
	}
	s.notifyResync(ctx)
	return updated, nil
}

func (s *redirectService) DeleteRedirect(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: redirect id is required", ErrValidation)
	}
	if _, err := s.store.GetRedirect(ctx, id); err != nil {
		return err
	}
	if err := s.store.DeleteRedirect(ctx, id); err != nil {
		return err
	}
	s.notifyResync(ctx)
	return nil
}

func (s *redirectService) checkRedirectWrites(ctx context.Context, excludeID uuid.UUID, source, target string, enabled bool) error {
	claimed, err := s.store.GetRedirectBySource(ctx, source)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil && claimed.ID != excludeID {
		return fmt.Errorf("%w: the source host %q is already used by another redirect rule", ErrConflict, source)
	}

	domains, err := s.store.ListApplicationBaseDomains(ctx)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		if NormalizeDomain(domain.Domain) == source {
			return fmt.Errorf("%w: the source host %q is an application's base domain", ErrConflict, source)
		}
	}

	if !enabled {
		return nil
	}
	endpoints, err := s.store.ListEnabledRedirects(ctx)
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == excludeID {
			continue
		}
		if NormalizeDomain(endpoint.Source) == target {
			return fmt.Errorf("%w: the target host %q is another redirect rule's source", ErrConflict, target)
		}
		if NormalizeDomain(endpoint.Target) == source {
			return fmt.Errorf("%w: the source host %q is another redirect rule's target", ErrConflict, source)
		}
	}
	return nil
}

func validateRedirectApplication(app ApplicationInfo) error {
	if app.DomainDisabled {
		return fmt.Errorf("%w: the application's domain is disabled; resolve the duplicate-domain conflict first", ErrValidation)
	}
	if app.ServerID == uuid.Nil {
		return fmt.Errorf("%w: the application is not assigned to a server, so its redirects cannot be generated", ErrValidation)
	}
	return nil
}

func validateRedirectDomains(source, target string) (string, string, error) {
	source = NormalizeDomain(source)
	target = NormalizeDomain(target)
	if err := ValidateDomain(source); err != nil {
		return "", "", fmt.Errorf("%w: invalid redirect source: %v", ErrValidation, err)
	}
	if err := ValidateDomain(target); err != nil {
		return "", "", fmt.Errorf("%w: invalid redirect target: %v", ErrValidation, err)
	}
	if source == target {
		return "", "", fmt.Errorf("%w: the source and target hosts must differ", ErrValidation)
	}
	return source, target, nil
}

func validateRedirectCode(code int) error {
	if code != RedirectCodePermanent && code != RedirectCodeTemporary {
		return fmt.Errorf("%w: the redirect code must be %d or %d", ErrValidation, RedirectCodePermanent, RedirectCodeTemporary)
	}
	return nil
}

func (s *redirectService) notifyResync(ctx context.Context) {
	if s == nil || s.resync == nil {
		return
	}
	resyncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.resyncTimeout)
	defer cancel()
	if err := s.resync(resyncCtx); err != nil {
		s.logger.Warn("proxy: redirect configuration resync failed", "error", err)
	}
}
