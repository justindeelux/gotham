package proxy

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const defaultResyncTimeout = time.Minute

var sslMutationMu sync.Mutex

func (s *sslService) mutate(ctx context.Context, fn func(context.Context) error) error {
	sslMutationMu.Lock()
	defer sslMutationMu.Unlock()
	return fn(ctx)
}

type SSLConfig struct {
	Store         SSLStore
	Secret        string
	Logger        *slog.Logger
	Resync        func(ctx context.Context) error
	ResyncTimeout time.Duration
}

type sslService struct {
	store         SSLStore
	secret        string
	logger        *slog.Logger
	resync        func(ctx context.Context) error
	resyncTimeout time.Duration
}

var (
	_ DNSProviderService = (*sslService)(nil)
	_ CertificateService = (*sslService)(nil)
)

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

func NewDefaultProviderService(cfg SSLConfig) DNSProviderService {
	if cfg.Store == nil || !Enabled() {
		return nil
	}
	return newSSLService(cfg)
}

func NewDefaultCertificateService(cfg SSLConfig) CertificateService {
	if cfg.Store == nil || !Enabled() {
		return nil
	}
	return newSSLService(cfg)
}

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
