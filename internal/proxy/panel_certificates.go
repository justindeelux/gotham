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

// ReconcilePanelCertificates retains both hosts during a trial, creates new
// HTTPS intents before deleting retired ones, and never changes app intents.
func (s *SyncService) ReconcilePanelCertificates(ctx context.Context, urls []string) error {
	if s.panelCertificates == nil {
		return nil
	}
	ssl := newSSLService(SSLConfig{Store: s.panelCertificates, Logger: s.logger})
	return ssl.reconcilePanelCertificates(ctx, urls)
}

func (s *sslService) reconcilePanelCertificates(ctx context.Context, urls []string) error {
	return s.mutate(ctx, func(ctx context.Context) error {
		wanted := map[string]bool{}
		for _, raw := range urls {
			u, err := url.Parse(raw)
			if err != nil {
				return fmt.Errorf("proxy: invalid panel URL: %w", err)
			}
			if u.Scheme != "https" {
				continue
			}
			domain := NormalizeDomain(u.Hostname())
			if err := ValidateDomain(domain); err != nil {
				return err
			}
			wanted[domain] = true
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

// RunPanelCertificates observes issuance/expiry for the server lifetime and
// pushes only changed configuration, so redirects follow certificate readiness.
func (s *SyncService) RunPanelCertificates(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.refreshPanelCertificates(ctx); err != nil && ctx.Err() == nil {
			s.logger.Debug("proxy: refresh panel certificate", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
