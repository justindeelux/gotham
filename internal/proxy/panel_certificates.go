package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// panelCertificateKey is set only by the platform-admin certificate routes.
type panelCertificateKey struct{}

func panelCertificateContext(ctx context.Context) bool {
	allowed, _ := ctx.Value(panelCertificateKey{}).(bool)
	return allowed
}

func panelCertificateAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), panelCertificateKey{}, true)))
	})
}

// certificateApplication keeps panel intents outside the team-scoped CRUD API.
func (s *sslService) certificateApplication(ctx context.Context, cert DomainCertificate) (ApplicationInfo, error) {
	if cert.ApplicationID == uuid.Nil {
		if !panelCertificateContext(ctx) {
			return ApplicationInfo{}, ErrNotFound
		}
		return ApplicationInfo{Domains: []string{cert.Domain}}, nil
	}
	if panelCertificateContext(ctx) {
		return ApplicationInfo{}, ErrNotFound
	}
	return s.store.GetApplication(ctx, cert.ApplicationID)
}

// ReconcilePanelCertificates creates confirmed hosts' new
// HTTPS intents before deleting retired ones, and never changes app intents.
// It then backfills the default TLS intent of application domains that have
// none (JUS-201), so a box that upgrades gets its existing hosts repaired.
func (s *SyncService) ReconcilePanelCertificates(ctx context.Context, urls []string) error {
	if s.panelCertificates == nil {
		return nil
	}
	ssl := newSSLService(SSLConfig{Store: s.panelCertificates, Logger: s.logger})
	if err := ssl.reconcilePanelCertificates(ctx, urls); err != nil {
		return err
	}
	return s.reconcileApplicationCertificates(ctx, urls)
}

// ControlPlaneHTTPSHosts returns the normalized hosts of the configured
// control-plane URLs that are served over https. It is the single gate that
// turns on TLS for the panel domain and for the default TLS of attached
// application domains, so the two cannot drift. A URL that cannot be parsed is
// an error; a non-https URL is skipped.
func ControlPlaneHTTPSHosts(urls []string) ([]string, error) {
	hosts := make([]string, 0, len(urls))
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("proxy: invalid control-plane URL: %w", err)
		}
		if u.Scheme != "https" {
			continue
		}
		domain := NormalizeDomain(u.Hostname())
		if err := ValidateDomain(domain); err != nil {
			return nil, err
		}
		hosts = append(hosts, domain)
	}
	return hosts, nil
}

// reconcileApplicationCertificates backfills the default enabled http-01
// intent for every enabled application domain that has none (JUS-201): a host
// attached before this change, or one whose intent was never recorded, is
// repaired on startup and on every control-plane URL change. It is gated on
// the same https condition as the panel domain, so an http-only instance
// stays plain-HTTP, and it triggers the proxy rebuild the panel flow relies on
// when it actually inserts rows.
func (s *SyncService) reconcileApplicationCertificates(ctx context.Context, urls []string) error {
	hosts, err := ControlPlaneHTTPSHosts(urls)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return nil
	}
	inserted, err := s.panelCertificates.BackfillApplicationCertificates(ctx)
	if err != nil {
		return err
	}
	if inserted > 0 {
		s.resyncAfterBackfill(ctx)
	}
	return nil
}

// resyncAfterBackfill pushes the freshly recorded intents to every node. It is
// best effort like sslService.notifyResync — the intents are durable and the
// next sync converges — and it inherits the caller's deadline so a settings
// save is never held open by a slow bootstrap.
func (s *SyncService) resyncAfterBackfill(ctx context.Context) {
	syncCtx, cancel := context.WithTimeout(ctx, defaultResyncTimeout)
	defer cancel()
	if _, err := s.SyncAll(syncCtx); err != nil {
		s.logger.Warn("proxy: TLS backfill resync failed", "error", err)
	}
}

func (s *sslService) reconcilePanelCertificates(ctx context.Context, urls []string) error {
	return s.mutate(ctx, func(ctx context.Context) error {
		hosts, err := ControlPlaneHTTPSHosts(urls)
		if err != nil {
			return err
		}
		wanted := make(map[string]bool, len(hosts))
		for _, host := range hosts {
			wanted[host] = true
		}
		certificates, err := s.store.ListCertificates(ctx)
		if err != nil {
			return err
		}
		for _, cert := range certificates {
			if cert.ApplicationID == uuid.Nil {
				continue
			}
			if wanted[cert.Domain] {
				return fmt.Errorf("%w: panel host already has an application certificate", ErrConflict)
			}
		}
		existing := map[string]bool{}
		for _, cert := range certificates {
			if cert.ApplicationID == uuid.Nil {
				existing[cert.Domain] = true
			}
		}
		for domain := range wanted {
			if existing[domain] {
				continue
			}
			if _, err := s.store.CreateCertificate(ctx, CertificateWrite{Domain: domain, Enabled: true, Challenge: ChallengeHTTP01}); err != nil {
				return err
			}
		}
		for _, cert := range certificates {
			if cert.ApplicationID == uuid.Nil && !wanted[cert.Domain] {
				if err := s.store.DeleteCertificate(ctx, cert.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// PanelCertificateState adds issuance state without changing legacy statuses.
type PanelCertificateState struct {
	Status   string     `json:"status"`
	NotAfter *time.Time `json:"not_after,omitempty"`
}

func panelCertificateState(observation CertificateStatusObservation, usable bool) PanelCertificateState {
	state := PanelCertificateState{Status: "pending"}
	if !usable || observation.Status == CertificateStatusUnknown {
		state.Status = "failed"
	}
	if usable && observation.Status == CertificateStatusPresent && observation.NotAfter.After(time.Now()) {
		state.Status = "issued"
		expiry := observation.NotAfter.UTC()
		state.NotAfter = &expiry
	}
	return state
}

// RunPanelCertificates reconciles immediately at startup, then observes issuance/expiry and
// pushes only changed configuration, so redirects follow certificate readiness.
func (s *SyncService) RunPanelCertificates(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.runPanelCertificates(ctx, ticker.C)
}

func (s *SyncService) runPanelCertificates(ctx context.Context, ticks <-chan time.Time) {
	lastError := ""
	for {
		if err := s.reconcileStoredPanelCertificates(ctx); err != nil && ctx.Err() == nil {
			if err.Error() != lastError {
				s.logger.Warn("proxy: reconcile panel certificates", "error", err)
			}
			lastError = err.Error()
		} else {
			lastError = ""
		}
		if err := s.refreshPanelCertificates(ctx); err != nil && ctx.Err() == nil {
			s.logger.Debug("proxy: refresh panel certificate", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (s *SyncService) reconcileStoredPanelCertificates(ctx context.Context) error {
	if s.reconcilePanel == nil || s.panelCertificates == nil {
		return nil
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.reconcilePanel(reconcileCtx)
}

func (s *SyncService) refreshPanelCertificates(ctx context.Context) error {
	if s.certificateStatus == nil {
		return nil
	}
	named, ok := s.nodes.(namedNodeSource)
	if !ok {
		return nil
	}
	nodes, err := named.ListNamedNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Name != "localhost" {
			continue
		}
		response, err := s.ListRouters(ctx, node.ID)
		if err != nil {
			return err
		}
		hasPanel := false
		for _, router := range response.Routers {
			if router.Kind == RouterKindControlPlane && router.Certificate != nil {
				hasPanel = true
			}
		}
		if hasPanel && len(response.Nodes) == 1 && response.Nodes[0].SyncStatus == RouterSyncPending {
			return s.SyncControlPlane(ctx)
		}
	}
	return nil
}
