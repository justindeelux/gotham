package servers

import (
	"context"
	"time"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type StateChange struct {
	Name   string
	Host   string
	Online bool
	At     time.Time
}

type StateNotifier interface {
	ServerStateChanged(context.Context, StateChange)
}

func (s *ServerService) SetStateNotifier(notifier StateNotifier) {
	s.notifier = notifier
}

func (s *ServerService) notifyState(ctx context.Context, row sqlc.Server, online bool) {
	if s.notifier == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.notifier.ServerStateChanged(notifyCtx, StateChange{
		Name: row.Name, Host: row.Ip, Online: online, At: s.now().UTC(),
	})
}

func (s *ServerService) RunOfflineSweep(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	s.runOfflineSweep(ctx, ticker.C)
}

func (s *ServerService) runOfflineSweep(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			s.sweepOffline(sweepCtx)
			cancel()
		}
	}
}
