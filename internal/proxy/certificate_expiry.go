package proxy

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type CertificateExpiry struct {
	Domain   string
	NotAfter time.Time
	At       time.Time
}

type CertificateExpiryNotifier interface {
	CertificateExpiring(context.Context, CertificateExpiry)
}

type CertificateExpiryStore interface {
	ListCertificates(context.Context) ([]DomainCertificate, error)
}

type CertificateExpiryConfig struct {
	Store    CertificateExpiryStore
	Status   CertificateStatusService
	Notifier CertificateExpiryNotifier
	Logger   *slog.Logger
}

type certificateExpiryKey struct {
	id       uuid.UUID
	notAfter time.Time
}

type expiryObservation struct {
	notAfter time.Time
	count    uint8
}

type CertificateExpiryObserver struct {
	store    CertificateExpiryStore
	status   CertificateStatusService
	notifier CertificateExpiryNotifier
	logger   *slog.Logger
	now      func() time.Time
	observed map[uuid.UUID]expiryObservation
	notified map[certificateExpiryKey]bool
}

func NewCertificateExpiryObserver(cfg CertificateExpiryConfig) *CertificateExpiryObserver {
	if cfg.Store == nil || cfg.Status == nil || cfg.Notifier == nil || !Enabled() {
		return nil
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &CertificateExpiryObserver{
		store: cfg.Store, status: cfg.Status, notifier: cfg.Notifier, logger: logger,
		now: time.Now, observed: make(map[uuid.UUID]expiryObservation),
		notified: make(map[certificateExpiryKey]bool),
	}
}

func (o *CertificateExpiryObserver) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	o.run(ctx, ticker.C)
}

func (o *CertificateExpiryObserver) run(ctx context.Context, ticks <-chan time.Time) {
	for ctx.Err() == nil {
		o.observe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (o *CertificateExpiryObserver) observe(ctx context.Context) {
	certificates, err := o.store.ListCertificates(ctx)
	if err != nil {
		o.logger.Warn("proxy: list certificates for expiry observation", "error", err)
		return
	}
	statuses := o.status.CertificateStatuses(ctx, certificates)
	if ctx.Err() != nil {
		return
	}
	now := o.now().UTC()
	for _, certificate := range certificates {
		observation := statuses[certificate.ID]
		if !certificate.Enabled || observation.Status != CertificateStatusPresent || observation.NotAfter.IsZero() {
			continue
		}
		key := certificateExpiryKey{id: certificate.ID, notAfter: observation.NotAfter.UTC()}
		state := o.observed[certificate.ID]
		if !state.notAfter.Equal(observation.NotAfter) {
			state = expiryObservation{notAfter: observation.NotAfter}
		}
		if o.notified[key] {
			o.observed[certificate.ID] = state
			continue
		}
		if observation.NotAfter.After(now.Add(7 * 24 * time.Hour)) {
			state.count = 0
			o.observed[certificate.ID] = state
			continue
		}
		state.count++
		o.observed[certificate.ID] = state
		if state.count == 2 {
			o.notified[key] = true
			notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			o.notifier.CertificateExpiring(notifyCtx, CertificateExpiry{
				Domain: certificate.Domain, NotAfter: observation.NotAfter.UTC(), At: now,
			})
			cancel()
		}
	}
}
