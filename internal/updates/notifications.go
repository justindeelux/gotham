package updates

import (
	"context"
	"time"
)

type AvailableUpdate struct {
	Version string
	Channel Channel
	At      time.Time
}

type AvailabilityNotifier interface {
	UpdateAvailable(context.Context, AvailableUpdate)
}

type updateNotificationKey struct {
	channel Channel
	version string
}

func (s *service) notifyAvailable(ctx context.Context, channel Channel, release *Release) {
	if s.notifier == nil || release == nil {
		return
	}
	key := updateNotificationKey{channel: channel, version: release.Version}
	s.mu.Lock()
	if s.notified[key] {
		s.mu.Unlock()
		return
	}
	s.notified[key] = true
	s.mu.Unlock()
	update := AvailableUpdate{Version: release.Version, Channel: channel, At: time.Now().UTC()}
	go func() {
		notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.notifier.UpdateAvailable(notifyCtx, update)
	}()
}
