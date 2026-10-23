package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

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
