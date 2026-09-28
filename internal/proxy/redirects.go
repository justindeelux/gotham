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

// DomainRedirect is one domain_redirects row as this package uses it: a rule
// that sends one exact source host to one exact target host over HTTPS. Code
// is the operator's intent (301 permanent or 302 temporary); Traefik
// special-cases only GET, so GET answers 301/302 while HEAD and every other
// method answer 308/307.
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

// CreateRedirectInput is the validated input of a new redirect rule.
type CreateRedirectInput struct {
	// ApplicationID owns the rule and its node (required).
	ApplicationID uuid.UUID
	SourceDomain  string
	TargetDomain  string
	// Code defaults to 301 when zero.
	Code int
	// PreservePath defaults to true when nil.
	PreservePath *bool
	// Enabled defaults to true when nil.
	Enabled *bool
}

// UpdateRedirectInput is a partial update: nil fields stay unchanged.
type UpdateRedirectInput struct {
	SourceDomain *string
	TargetDomain *string
	Code         *int
	PreservePath *bool
	Enabled      *bool
}

// RedirectService is the CRUD surface over domain_redirects.
type RedirectService interface {
	CreateRedirect(ctx context.Context, in CreateRedirectInput) (DomainRedirect, error)
	// ListRedirects returns every rule newest first, or one application's
	// rules when applicationID is not uuid.Nil.
	ListRedirects(ctx context.Context, applicationID uuid.UUID) ([]DomainRedirect, error)
	GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error)
	UpdateRedirect(ctx context.Context, id uuid.UUID, in UpdateRedirectInput) (DomainRedirect, error)
	DeleteRedirect(ctx context.Context, id uuid.UUID) error
}

// RedirectWrite is the repository-level redirect write shape.
type RedirectWrite struct {
	ApplicationID uuid.UUID
	SourceDomain  string
	TargetDomain  string
	Code          int
	PreservePath  bool
	Enabled       bool
}

// ApplicationDomain is one application's base domain, used by the ownership
// guard so a redirect source can never shadow a routed host.
type ApplicationDomain struct {
	ID     uuid.UUID
	Domain string
}

// RedirectEndpoint is one enabled redirect rule's source and target hosts,
// used by the two-directional no-chain guard: a proposed target must not be
// another enabled rule's source, and a proposed source must not be another
// enabled rule's target.
type RedirectEndpoint struct {
	ID     uuid.UUID
	Source string
	Target string
}

// RedirectStore is the persistence seam of the redirect surface. The
// production implementation is storeRedirect over *store.Store; tests
// substitute a fake.
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

// storeRedirect adapts the shared store to the RedirectStore seam.
type storeRedirect struct {
	store *store.Store
}

// NewStoreRedirect adapts the shared store to the RedirectStore seam. It is
// exported so internal/server can build the production redirect service over
// one store.
func NewStoreRedirect(st *store.Store) RedirectStore {
	return storeRedirect{store: st}
}

// CreateRedirect stores one rule, mapping the unique source index to
// ErrConflict.
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

// GetRedirect returns one rule, or ErrNotFound.
func (s storeRedirect) GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error) {
	row, err := s.store.GetDomainRedirect(ctx, pgUUID(id))
	if err != nil {
		return DomainRedirect{}, mapSSLReadError(err)
	}
	return redirectFromRow(row), nil
}

// GetRedirectBySource returns the rule claiming a source host, or ErrNotFound.
func (s storeRedirect) GetRedirectBySource(ctx context.Context, source string) (DomainRedirect, error) {
	row, err := s.store.GetDomainRedirectBySource(ctx, source)
	if err != nil {
		return DomainRedirect{}, mapSSLReadError(err)
	}
	return redirectFromRow(row), nil
}

// ListRedirects returns every rule, newest first.
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

// ListRedirectsByApplication returns one application's rules, newest first.
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

// UpdateRedirect persists the mutable fields.
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

// DeleteRedirect removes one rule.
func (s storeRedirect) DeleteRedirect(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteDomainRedirect(ctx, pgUUID(id)); err != nil {
		return mapRedirectWriteError(err)
	}
	return nil
}

// ListApplicationBaseDomains returns every application's base domain for the
// ownership guard.
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

// ListEnabledRedirects returns the endpoints of every enabled rule, across
// every node: the no-chain guard is global, not node-local, so a chain cannot
// be introduced even when its two rules live on different nodes.
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

// GetApplication returns the application a rule belongs to, or ErrNotFound.
func (s storeRedirect) GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error) {
	row, err := s.store.GetApplication(ctx, pgUUID(id))
	if err != nil {
		return ApplicationInfo{}, mapSSLReadError(err)
	}
	return applicationInfoFromRow(row), nil
}

// redirectFromRow maps a stored redirect row into the domain model.
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

// mapRedirectWriteError turns unique and foreign-key violations into
// ErrConflict: the unique source index and the application reference surface
// as actionable conflicts instead of a 500 (the service pre-checks cover the
// common paths; these mappings catch the races).
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

// RedirectConfig wires the redirect CRUD service. Store is required; Resync is
// optional and receives a best-effort trigger after every successful mutation
// (a failed resync never fails the mutation).
type RedirectConfig struct {
	// Store is the redirect persistence seam.
	Store RedirectStore
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Resync pushes the new desired state to the nodes. nil disables it.
	Resync func(ctx context.Context) error
	// ResyncTimeout bounds one resync; default one minute.
	ResyncTimeout time.Duration
}

// redirectService implements RedirectService over one store and resync hook.
type redirectService struct {
	store         RedirectStore
	logger        *slog.Logger
	resync        func(ctx context.Context) error
	resyncTimeout time.Duration
}

// Compile-time guarantee that redirectService satisfies the CRUD contract.
var _ RedirectService = (*redirectService)(nil)

// NewDefaultRedirectService builds the production redirect CRUD service, or
// nil (a nil interface) when there is no store or FEATURE_PROXY=false, so the
// HTTP wiring can pass its result to Mount unconditionally.
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
	return &redirectService{store: cfg.Store, logger: logger, resync: cfg.Resync, resyncTimeout: timeout}
}

// CreateRedirect validates and stores one rule: the application must exist, be
// assigned to a node and not be held back by a domain-uniqueness conflict; the
// source must be a valid exact host that shadows no application base domain
// and no other rule; the target must be a valid exact host that neither points
// at another enabled rule's source nor is the target of one (the no-chain
// guard checks both directions, globally), so a sequential conflicting write
// cannot complete a chain. The guard is committed-state check-then-write, not
// a database constraint: a racing write can still persist a chain, which the
// generator then holds back per snapshot (see checkRedirectWrites and
// redirectsForServer for the exact limits). Redirect rules are emitted only
// for enabled rules of domain-healthy applications on their node.
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

// ListRedirects returns every rule, or one application's rules.
func (s *redirectService) ListRedirects(ctx context.Context, applicationID uuid.UUID) ([]DomainRedirect, error) {
	if applicationID == uuid.Nil {
		return s.store.ListRedirects(ctx)
	}
	return s.store.ListRedirectsByApplication(ctx, applicationID)
}

// GetRedirect returns one rule.
func (s *redirectService) GetRedirect(ctx context.Context, id uuid.UUID) (DomainRedirect, error) {
	if id == uuid.Nil {
		return DomainRedirect{}, fmt.Errorf("%w: redirect id is required", ErrValidation)
	}
	return s.store.GetRedirect(ctx, id)
}

// UpdateRedirect applies a partial update. The owning application is re-read
// so a rule can never be updated into an invalid application state (a
// disabled domain or a detached node), and every updated value is re-validated
// against the current claims of other rules and applications.
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

// DeleteRedirect removes one rule; the redirect stops with the next sync.
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

// checkRedirectWrites enforces the write-time guard rails against all other
// state. excludeID is the rule being updated (uuid.Nil on create).
//
// The ownership guard is global: every application's base domain and every
// enabled rule in the database is considered, not only those on the rule's
// node, because the generated per-node routers are not the only way a source
// could be claimed. The no-chain guard is two-directional: a proposed target
// may not be another enabled rule's source, and a proposed source may not be
// another enabled rule's target, so a sequential conflicting write (create,
// update or enable) is rejected regardless of insertion order. It is a
// check-then-write against committed state: a concurrent racing write is
// caught by the generator's hold-back instead (see redirectsForServer), never
// by a database constraint, and live fleet-wide chain-freedom additionally
// requires every affected node to converge — SyncServer writes one node and
// SyncAll applies nodes separately, with no atomic fleet replacement.
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

// validateRedirectApplication rejects an application the rule could never
// serve: a domain-disabled one (its route is already held back by the
// uniqueness conflict, and its redirects are held back with it) or one with no
// node to generate on.
func validateRedirectApplication(app ApplicationInfo) error {
	if app.DomainDisabled {
		return fmt.Errorf("%w: the application's domain is disabled; resolve the duplicate-domain conflict first", ErrValidation)
	}
	if app.ServerID == uuid.Nil {
		return fmt.Errorf("%w: the application is not assigned to a server, so its redirects cannot be generated", ErrValidation)
	}
	return nil
}

// validateRedirectDomains normalizes and validates the source and target
// hosts and rejects the self redirect. It returns the canonical values.
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

// validateRedirectCode accepts only the two intents Traefik's redirectRegex
// can express: 301 (permanent) and 302 (temporary).
func validateRedirectCode(code int) error {
	if code != RedirectCodePermanent && code != RedirectCodeTemporary {
		return fmt.Errorf("%w: the redirect code must be %d or %d", ErrValidation, RedirectCodePermanent, RedirectCodeTemporary)
	}
	return nil
}

// notifyResync triggers the best-effort resync after a committed mutation. It
// never fails the caller: the durable state is already correct, and the next
// sync (manual or deploy-driven) converges the nodes.
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
