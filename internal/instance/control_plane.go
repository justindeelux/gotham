package instance

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GuardDomains reserves a domain write against concurrent panel changes in the
// single control-plane process. The caller must release after persisting it.
func (s *Service) GuardDomains(ctx context.Context, hosts ...string) (func(), error) {
	s.domainLock.Lock()
	rec, err := s.repo.Load(ctx)
	if err == nil {
		urls := []string{s.general(rec).ControlPlaneURL.Value}
		if rec.PendingControlPlaneURL != nil && rec.PendingControlPlaneURL.Previous != nil {
			urls = append(urls, *rec.PendingControlPlaneURL.Previous)
		}
		for _, raw := range urls {
			u, parseErr := url.Parse(raw)
			if parseErr != nil || u.Hostname() == "" {
				continue
			}
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

// ControlPlaneURLs reads the proposed and fallback routes from one stored snapshot.
func (s *Service) ControlPlaneURLs(ctx context.Context) ([]string, error) {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	urls := []string{s.general(rec).ControlPlaneURL.Value}
	if rec.PendingControlPlaneURL != nil && rec.PendingControlPlaneURL.Previous != nil {
		urls = append(urls, *rec.PendingControlPlaneURL.Previous)
	}
	return urls, nil
}

func (s *Service) syncControlPlaneURL(ctx context.Context) error {
	if s.ResyncProxy == nil {
		return ErrDomainNotSynced
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return s.ResyncProxy(syncCtx)
}

// scheduleDomainRevert is called with domainLock held; restart reconciliation
// re-arms it so expiry never depends on another API request.
func (s *Service) scheduleDomainRevert(deadline time.Time) {
	if s.domainClosed {
		return
	}
	if s.domainTimer != nil {
		s.domainTimer.Stop()
	}
	s.domainTimer = time.AfterFunc(deadline.Sub(s.now()), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := s.reconcileControlPlaneURL(ctx); err != nil {
			s.logger.Error("instance control-plane URL auto-revert failed", "error", err)
			s.domainLock.Lock()
			s.scheduleDomainRevert(s.now().Add(5 * time.Second))
			s.domainLock.Unlock()
		}
	})
}

func (s *Service) reconcileControlPlaneURL(ctx context.Context) error {
	s.domainLock.Lock()
	defer s.domainLock.Unlock()
	if s.domainClosed {
		return nil
	}
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return err
	}
	if rec.PendingControlPlaneURL == nil {
		return nil
	}
	if s.now().Before(rec.PendingControlPlaneURL.Deadline) {
		s.scheduleDomainRevert(rec.PendingControlPlaneURL.Deadline)
		return nil
	}
	return s.revertControlPlaneURL(ctx, uuid.Nil, rec)
}

func (s *Service) revertControlPlaneURL(ctx context.Context, actor uuid.UUID, rec Stored) error {
	rec.ControlPlaneURL, rec.PendingControlPlaneURL = rec.PendingControlPlaneURL.Previous, nil
	if err := s.repo.Save(ctx, actor, SectionGeneral, rec, map[string]any{"event": "control-plane URL reverted"}); err != nil {
		return err
	}
	if s.domainTimer != nil {
		s.domainTimer.Stop()
	}
	// Both routers were retained during the trial: a failed cleanup still leaves the old host reachable.
	if err := s.syncControlPlaneURL(ctx); err != nil {
		s.logger.Warn("instance reverted URL; proxy cleanup failed", "error", err)
	}
	return nil
}

// ConfirmControlPlaneURL requires a request through the new host after sync.
func (s *Service) ConfirmControlPlaneURL(ctx context.Context, actor uuid.UUID, requestHost string) (State, error) {
	if err := s.reconcileControlPlaneURL(ctx); err != nil {
		return State{}, err
	}
	s.domainLock.Lock()
	defer s.domainLock.Unlock()
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	p := rec.PendingControlPlaneURL
	if p == nil {
		return State{}, ErrNoDomainPending
	}
	// Recheck after acquiring the lock: another write or sync may have consumed the deadline.
	if !s.now().Before(p.Deadline) {
		if err := s.revertControlPlaneURL(ctx, actor, rec); err != nil {
			return State{}, err
		}
		return State{}, ErrNoDomainPending
	}
	if !p.Synced {
		return State{}, ErrDomainNotSynced
	}
	confirmationURL := p.Proposed
	// Clearing the URL retires the old route, so confirm that intent through the old host.
	if confirmationURL == "" && p.Previous != nil {
		confirmationURL = *p.Previous
	}
	proposed, _ := url.Parse(confirmationURL)
	request, err := url.Parse("http://" + requestHost)
	if err != nil || proposed == nil || proposed.Hostname() == "" || !strings.EqualFold(strings.TrimSuffix(request.Hostname(), "."), strings.TrimSuffix(proposed.Hostname(), ".")) {
		return State{}, ErrConfirmDomainHost
	}
	rec.PendingControlPlaneURL = nil
	if err := s.repo.Save(ctx, actor, SectionGeneral, rec, map[string]any{"event": "control-plane URL confirmed"}); err != nil {
		return State{}, err
	}
	if s.domainTimer != nil {
		s.domainTimer.Stop()
	}
	if err := s.syncControlPlaneURL(ctx); err != nil {
		s.logger.Warn("instance confirmed URL; proxy cleanup failed", "error", err)
	}
	return s.stateLocked(ctx, rec), nil
}

// RevertControlPlaneURL cancels the trial immediately.
func (s *Service) RevertControlPlaneURL(ctx context.Context, actor uuid.UUID) (State, error) {
	s.domainLock.Lock()
	defer s.domainLock.Unlock()
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	if rec.PendingControlPlaneURL == nil {
		return State{}, ErrNoDomainPending
	}
	if err := s.revertControlPlaneURL(ctx, actor, rec); err != nil {
		return State{}, err
	}
	rec, err = s.repo.Load(ctx)
	return s.stateLocked(ctx, rec), err
}

// Close stops this process's timer; the persisted trial is resumed on restart.
func (s *Service) Close() {
	s.domainLock.Lock()
	defer s.domainLock.Unlock()
	s.domainClosed = true
	if s.domainTimer != nil {
		s.domainTimer.Stop()
	}
}
