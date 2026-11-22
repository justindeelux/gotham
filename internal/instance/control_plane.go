package instance

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (s *Service) GuardDomains(ctx context.Context, hosts ...string) (func(), error) {
	s.domainLock.Lock()
	rec, err := s.repo.Load(ctx)
	if err == nil {
		u, parseErr := url.Parse(s.general(rec).ControlPlaneURL.Value)
		if parseErr == nil && u.Hostname() != "" {
			for _, host := range hosts {
				if strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(u.Hostname(), ".")) {
					err = fmt.Errorf("host %q is reserved for the control plane", host)
				}
			}
		}
	}
	if err != nil {
		s.domainLock.Unlock()
		return nil, err
	}
	return s.domainLock.Unlock, nil
}

func (s *Service) ControlPlaneURLs(ctx context.Context) ([]string, error) {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	return []string{s.general(rec).ControlPlaneURL.Value}, nil
}

func (s *Service) ReconcilePanelCertificates(ctx context.Context) error {
	s.domainLock.Lock()
	defer s.domainLock.Unlock()
	if s.SyncPanelCertificates == nil {
		return nil
	}
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return err
	}
	return s.SyncPanelCertificates(ctx, []string{s.general(rec).ControlPlaneURL.Value})
}

func (s *Service) syncPanelCertificates(ctx context.Context, rec Stored) {
	if s.SyncPanelCertificates == nil {
		return
	}
	urls := []string{s.general(rec).ControlPlaneURL.Value}
	certCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.SyncPanelCertificates(certCtx, urls); err != nil {
		s.logger.Warn("instance panel certificate reconciliation failed", "error", err)
	}
}

func (s *Service) syncControlPlaneURL(ctx context.Context) error {
	if rec, err := s.repo.Load(ctx); err == nil {
		s.syncPanelCertificates(ctx, rec)
	} else {
		s.logger.Warn("instance panel certificate settings read failed", "error", err)
	}
	if s.ResyncProxy == nil {
		return nil
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return s.ResyncProxy(syncCtx)
}
