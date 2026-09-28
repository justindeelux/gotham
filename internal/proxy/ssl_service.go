package proxy

import (
	"context"
	"log/slog"
	"time"
)

// defaultResyncTimeout bounds one best-effort proxy resync triggered by an SSL
// mutation. A sync that must bootstrap the Traefik container (image pull) may
// exceed it; the manual POST /v1/proxy/sync completes it then.
const defaultResyncTimeout = time.Minute

// SSLConfig wires the DNS provider and certificate services. Store is
// required; Resync is optional and receives a best-effort trigger after every
// successful mutation (a failed resync never fails the mutation).
type SSLConfig struct {
	// Store is the SSL persistence seam.
	Store SSLStore
	// Secret opens and seals DNS provider credentials. An empty key keeps the
	// documented development fallback (a well-known key) used elsewhere.
	Secret string
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Resync pushes the new desired state to the nodes. nil disables it.
	Resync func(ctx context.Context) error
	// ResyncTimeout bounds one resync; default one minute.
	ResyncTimeout time.Duration
}

// sslService implements DNSProviderService and CertificateService over one
// store, secret key and resync hook. Every mutation that can change the
// generated configuration (providers and certificate intents alike) triggers
// a best-effort resync, so the nodes converge without an extra manual sync.
type sslService struct {
	store         SSLStore
	secret        string
	logger        *slog.Logger
	resync        func(ctx context.Context) error
	resyncTimeout time.Duration
}

// Compile-time guarantees that one service satisfies both CRUD contracts.
var (
	_ DNSProviderService = (*sslService)(nil)
	_ CertificateService = (*sslService)(nil)
)

// newSSLService builds the shared service state.
func newSSLService(cfg SSLConfig) *sslService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.ResyncTimeout
	if timeout <= 0 {
		timeout = defaultResyncTimeout
	}
	return &sslService{
		store:         cfg.Store,
		secret:        cfg.Secret,
		logger:        logger,
		resync:        cfg.Resync,
		resyncTimeout: timeout,
	}
}

// NewDefaultProviderService builds the production DNS provider CRUD service,
// or nil (a nil interface) when there is no store or FEATURE_PROXY=false, so
// the HTTP wiring can pass its result to Mount unconditionally.
func NewDefaultProviderService(cfg SSLConfig) DNSProviderService {
	if cfg.Store == nil || !Enabled() {
		return nil
	}
	return newSSLService(cfg)
}

// NewDefaultCertificateService builds the production certificate CRUD
// service, or nil under the same conditions as NewDefaultProviderService.
func NewDefaultCertificateService(cfg SSLConfig) CertificateService {
	if cfg.Store == nil || !Enabled() {
		return nil
	}
	return newSSLService(cfg)
}

// notifyResync triggers the best-effort resync after a committed mutation. It
// never fails the caller: a failed resync is logged, and the mutation's
// durable state is already correct — the next sync (manual or deploy-driven)
// converges the nodes.
func (s *sslService) notifyResync(ctx context.Context) {
	if s == nil || s.resync == nil {
		return
	}
	resyncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.resyncTimeout)
	defer cancel()
	if err := s.resync(resyncCtx); err != nil {
		s.logger.Warn("proxy: SSL configuration resync failed", "error", err)
	}
}
