package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// Service tunables. The queue is bounded on purpose: a stalled endpoint must
// never grow the deploy/backup path's memory, so a full queue drops events
// with a log line instead of blocking.
const (
	defaultWorkers   = 4
	defaultQueueSize = 64
	// defaultSendTimeout bounds one channel delivery.
	defaultSendTimeout = 10 * time.Second
	// dispatchReadTimeout bounds the channel lookup of one dispatched event.
	dispatchReadTimeout = 5 * time.Second
)

// NotificationService is the control-plane surface the HTTP layer depends on.
// It is implemented by Service and by fakes in the route tests.
type NotificationService interface {
	// ListChannels returns the active team's channels, oldest first.
	ListChannels(ctx context.Context, userID uuid.UUID) ([]ChannelView, error)
	// GetChannel returns one channel of the active team (404 for others).
	GetChannel(ctx context.Context, userID, channelID uuid.UUID) (ChannelView, error)
	// CreateChannel validates, seals and stores a channel (owner/admin).
	CreateChannel(ctx context.Context, userID uuid.UUID, req ChannelRequest) (ChannelView, error)
	// UpdateChannel applies a partial update to a channel (owner/admin).
	UpdateChannel(ctx context.Context, userID, channelID uuid.UUID, req ChannelRequest) (ChannelView, error)
	// DeleteChannel removes a channel (owner/admin).
	DeleteChannel(ctx context.Context, userID, channelID uuid.UUID) error
	// TestChannel sends a synthetic deploy-success event to one channel and
	// reports the delivery result without exposing any secret.
	TestChannel(ctx context.Context, userID, channelID uuid.UUID) (TestResult, error)
}

// Config wires a Service. Store (or an explicit Repository) is required for
// anything beyond tests; Secret is the key providers.SealSecret seals the
// channel configs with. Mailer, HTTPClient and the worker tunables fall back
// to their production defaults.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is
	// set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Secret is the deployment secret (GOTHAM_SECRET_KEY).
	Secret string
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Mailer delivers email notifications; nil selects the net/smtp
	// implementation.
	Mailer Mailer
	// HTTPClient delivers webhook and Telegram notifications; nil selects an
	// http.Client bounded by SendTimeout.
	HTTPClient *http.Client
	// Workers is the delivery pool size, QueueSize the event buffer.
	Workers   int
	QueueSize int
	// SendTimeout bounds one delivery.
	SendTimeout time.Duration
	// Now overrides the clock (tests).
	Now func() time.Time
}

// repository resolves the configured repository implementation.
func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

// Service is the notifications domain service: it owns channel lifecycle and
// the asynchronous delivery of deploy/backup events. It is safe for
// concurrent use.
type Service struct {
	repo        Repository
	secret      string
	logger      *slog.Logger
	client      *http.Client
	mailer      Mailer
	workers     int
	sendTimeout time.Duration
	now         func() time.Time

	// factory builds the notifier of one channel. It is a field so tests can
	// inject a fake without a network.
	factory func(Channel) (Notifier, error)

	baseCtx    context.Context
	baseCancel context.CancelFunc
	queue      chan Event
	wg         sync.WaitGroup
	startOnce  sync.Once
	closeOnce  sync.Once
}

// Compile-time guarantee that Service satisfies the route-level contract and
// both domain hooks.
var (
	_ NotificationService      = (*Service)(nil)
	_ deploy.Notifier          = (*Service)(nil)
	_ databases.BackupNotifier = (*Service)(nil)
)

// NewService builds a Service from cfg. Construction starts no goroutines; the
// worker pool boots on the first dispatched event.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	sendTimeout := cfg.SendTimeout
	if sendTimeout <= 0 {
		sendTimeout = defaultSendTimeout
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	mailer := cfg.Mailer
	if mailer == nil {
		mailer = defaultMailer(sendTimeout)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())

	service := &Service{
		repo:        cfg.repository(),
		secret:      cfg.Secret,
		logger:      logger,
		client:      client,
		mailer:      mailer,
		workers:     workers,
		sendTimeout: sendTimeout,
		now:         now,
		baseCtx:     baseCtx,
		baseCancel:  baseCancel,
		queue:       make(chan Event, queueSize),
	}
	service.factory = service.notifierFor
	return service
}

// NewDefaultService builds the production service for the HTTP wiring and
// primes its worker pool, so the server lifecycle owns start and close. It
// returns nil (a nil NotificationService) when there is no database or
// FEATURE_NOTIFICATIONS=false, so callers can pass its result to Mount
// unconditionally and leave both domain hooks unwired.
func NewDefaultService(cfg Config) NotificationService {
	if cfg.repository() == nil || !Enabled() {
		return nil
	}
	service := NewService(cfg)
	service.Start()
	return service
}

// Start boots the delivery worker pool. It is idempotent: the server calls it
// at wiring time and Dispatch starts the pool on first use for callers that
// never do (tests).
func (s *Service) Start() {
	if s == nil {
		return
	}
	s.start()
}

// Close stops the worker pool, abandoning queued events and letting in-flight
// deliveries finish. It is idempotent and safe on a service that never
// dispatched an event.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.baseCancel()
		s.wg.Wait()
	})
	return nil
}

// ready reports a service built without its repository, so the CRUD surface
// fails with a clear error instead of panicking.
func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errNotReady
	}
	return nil
}

// teamIDFor resolves the team a channel call operates in: the request's active
// team, or the caller's personal team when no team context is present (the
// pre-teams path), matching every other resource surface.
func teamIDFor(ctx context.Context, userID uuid.UUID) uuid.UUID {
	scope := teams.ScopeFor(ctx, userID)
	if scope.Active() {
		return scope.TeamID
	}
	return teams.PersonalTeamID(userID)
}

// authorizeWrite enforces the caller's team role: a read_only member can read
// but never create, update, delete or test a channel. Without a team context
// (non-HTTP callers, tests) the pre-teams behavior applies.
func authorizeWrite(ctx context.Context, userID uuid.UUID) error {
	if !teams.ScopeFor(ctx, userID).CanWrite() {
		return ErrForbidden
	}
	return nil
}

// view opens a stored channel into its redacted read representation.
func (s *Service) view(channel Channel) (ChannelView, error) {
	config, err := openConfig(s.secret, channel.SealedConfig)
	if err != nil {
		return ChannelView{}, err
	}
	return ChannelView{
		ID:                channel.ID,
		TeamID:            channel.TeamID,
		Name:              channel.Name,
		Kind:              channel.Kind,
		Enabled:           channel.Enabled,
		Events:            append([]EventKey(nil), channel.Events...),
		ResourceType:      channel.ResourceType,
		ResourceID:        resourceIDString(channel.ResourceID),
		Config:            config.redacted(),
		SecretsConfigured: config.hasSecrets(),
		CreatedAt:         channel.CreatedAt,
		UpdatedAt:         channel.UpdatedAt,
	}, nil
}

// resourceIDString renders a resource id for the wire, empty when unset.
func resourceIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// ListChannels implements NotificationService.
func (s *Service) ListChannels(ctx context.Context, userID uuid.UUID) ([]ChannelView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	channels, err := s.repo.ListChannels(ctx, teamIDFor(ctx, userID))
	if err != nil {
		return nil, err
	}
	views := make([]ChannelView, 0, len(channels))
	for _, channel := range channels {
		view, err := s.view(channel)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// GetChannel implements NotificationService.
func (s *Service) GetChannel(ctx context.Context, userID, channelID uuid.UUID) (ChannelView, error) {
	if err := s.ready(); err != nil {
		return ChannelView{}, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return ChannelView{}, ErrNotFound
	}
	channel, err := s.repo.GetChannel(ctx, teamIDFor(ctx, userID), channelID)
	if err != nil {
		return ChannelView{}, err
	}
	return s.view(channel)
}

// CreateChannel implements NotificationService: it validates the request,
// seals the config and stores the row.
func (s *Service) CreateChannel(ctx context.Context, userID uuid.UUID, req ChannelRequest) (ChannelView, error) {
	if err := s.ready(); err != nil {
		return ChannelView{}, err
	}
	if userID == uuid.Nil {
		return ChannelView{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return ChannelView{}, err
	}
	kind, err := ParseKind(string(req.Kind))
	if err != nil {
		return ChannelView{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ChannelView{}, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if len(name) > maxNameLength {
		return ChannelView{}, fmt.Errorf("%w: name must be at most %d characters", ErrValidation, maxNameLength)
	}
	resourceType := ""
	resourceID := uuid.Nil
	if req.ResourceType != nil || req.ResourceID != nil {
		rawType, rawID := "", ""
		if req.ResourceType != nil {
			rawType = *req.ResourceType
		}
		if req.ResourceID != nil {
			rawID = *req.ResourceID
		}
		resourceType, resourceID, err = parseResource(rawType, rawID)
		if err != nil {
			return ChannelView{}, err
		}
	}
	events, err := normalizeEvents(req.Events)
	if err != nil {
		return ChannelView{}, err
	}
	if err := req.Config.validate(kind); err != nil {
		return ChannelView{}, err
	}
	sealed, err := sealConfig(s.secret, req.Config)
	if err != nil {
		return ChannelView{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := s.now().UTC()
	created, err := s.repo.CreateChannel(ctx, Channel{
		ID:           uuid.New(),
		TeamID:       teamIDFor(ctx, userID),
		Name:         name,
		Kind:         kind,
		Enabled:      enabled,
		Events:       events,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		SealedConfig: sealed,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return ChannelView{}, err
	}
	return s.view(created)
}

// UpdateChannel implements NotificationService: zero fields leave the stored
// value unchanged; secrets are only replaced when the request carries new
// ones. The channel kind is immutable.
func (s *Service) UpdateChannel(ctx context.Context, userID, channelID uuid.UUID, req ChannelRequest) (ChannelView, error) {
	if err := s.ready(); err != nil {
		return ChannelView{}, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return ChannelView{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return ChannelView{}, err
	}
	teamID := teamIDFor(ctx, userID)
	existing, err := s.repo.GetChannel(ctx, teamID, channelID)
	if err != nil {
		return ChannelView{}, err
	}
	if req.Kind != "" && req.Kind != existing.Kind {
		return ChannelView{}, fmt.Errorf("%w: kind cannot be changed after creation", ErrValidation)
	}
	if req.Name != "" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return ChannelView{}, fmt.Errorf("%w: name is required", ErrValidation)
		}
		if len(name) > maxNameLength {
			return ChannelView{}, fmt.Errorf("%w: name must be at most %d characters", ErrValidation, maxNameLength)
		}
		existing.Name = name
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := applyResourceUpdate(&existing, req.ResourceType, req.ResourceID); err != nil {
		return ChannelView{}, err
	}
	if req.Events != nil {
		events, err := normalizeEvents(req.Events)
		if err != nil {
			return ChannelView{}, err
		}
		existing.Events = events
	}
	if !req.Config.isZero() {
		stored, err := openConfig(s.secret, existing.SealedConfig)
		if err != nil {
			return ChannelView{}, err
		}
		merged := stored.merged(req.Config)
		if err := merged.validate(existing.Kind); err != nil {
			return ChannelView{}, err
		}
		sealed, err := sealConfig(s.secret, merged)
		if err != nil {
			return ChannelView{}, err
		}
		existing.SealedConfig = sealed
	}
	existing.UpdatedAt = s.now().UTC()
	updated, err := s.repo.UpdateChannel(ctx, existing)
	if err != nil {
		return ChannelView{}, err
	}
	return s.view(updated)
}

// applyResourceUpdate resolves the tri-state resource pair of a PATCH: absent
// leaves the override alone, an empty pair clears it, and a named pair sets
// it. Naming only one half is rejected.
func applyResourceUpdate(channel *Channel, resourceType, resourceID *string) error {
	if (resourceType == nil) != (resourceID == nil) {
		return fmt.Errorf("%w: resource_type and resource_id must be provided together", ErrValidation)
	}
	if resourceType == nil {
		return nil
	}
	kind, id, err := parseResource(*resourceType, *resourceID)
	if err != nil {
		return err
	}
	channel.ResourceType, channel.ResourceID = kind, id
	return nil
}

// DeleteChannel implements NotificationService.
func (s *Service) DeleteChannel(ctx context.Context, userID, channelID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return err
	}
	return s.repo.DeleteChannel(ctx, teamIDFor(ctx, userID), channelID)
}

// TestChannel implements NotificationService: it delivers a synthetic
// deploy-success event to the one channel and reports what happened. The
// delivery is synchronous so the operator sees the result immediately, and
// the answer never carries a secret.
func (s *Service) TestChannel(ctx context.Context, userID, channelID uuid.UUID) (TestResult, error) {
	if err := s.ready(); err != nil {
		return TestResult{}, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return TestResult{}, ErrNotFound
	}
	if err := authorizeWrite(ctx, userID); err != nil {
		return TestResult{}, err
	}
	channel, err := s.repo.GetChannel(ctx, teamIDFor(ctx, userID), channelID)
	if err != nil {
		return TestResult{}, err
	}
	notifier, err := s.factory(channel)
	if err != nil {
		return TestResult{}, err
	}
	event := Event{
		Kind:         EventDeploy,
		Outcome:      OutcomeSuccess,
		TeamID:       channel.TeamID,
		ResourceType: channel.ResourceType,
		ResourceID:   channel.ResourceID,
		Name:         "notifications test",
		At:           s.now().UTC(),
	}
	sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()
	if err := notifier.Notify(sendCtx, event); err != nil {
		return TestResult{OK: false, Message: truncateText(err.Error(), 300)}, nil
	}
	return TestResult{OK: true, Message: "test event delivered"}, nil
}

// DeployFinished implements deploy.Notifier: one terminal deployment result,
// queued for asynchronous delivery. It never blocks the deploy path.
func (s *Service) DeployFinished(_ context.Context, result deploy.DeployResult) {
	if s == nil || !Enabled() {
		return
	}
	outcome := OutcomeFailure
	if result.State == deploy.StateRunning {
		outcome = OutcomeSuccess
	}
	s.Dispatch(Event{
		Kind:         EventDeploy,
		Outcome:      outcome,
		TeamID:       result.TeamID,
		ResourceType: ResourceApplication,
		ResourceID:   result.ApplicationID,
		Name:         result.Application,
		Host:         result.Host,
		Error:        result.Error,
		At:           result.FinishedAt,
	})
}

// BackupFinished implements databases.BackupNotifier: one terminal backup
// result, queued for asynchronous delivery.
func (s *Service) BackupFinished(_ context.Context, result databases.BackupResult) {
	if s == nil || !Enabled() {
		return
	}
	outcome := OutcomeFailure
	if result.Status == databases.BackupCompleted {
		outcome = OutcomeSuccess
	}
	s.Dispatch(Event{
		Kind:         EventBackup,
		Outcome:      outcome,
		TeamID:       result.TeamID,
		ResourceType: ResourceDatabase,
		ResourceID:   result.DatabaseID,
		Name:         result.Database,
		Error:        result.Error,
		At:           result.FinishedAt,
	})
}

// Dispatch queues one event for asynchronous delivery. It never blocks: the
// channel lookup and every send run on the worker pool, and a full queue drops
// the event with a log line instead of stalling the caller.
func (s *Service) Dispatch(event Event) {
	if s == nil || s.repo == nil || !Enabled() {
		return
	}
	if event.At.IsZero() {
		event.At = s.now().UTC()
	}
	if event.TeamID == uuid.Nil {
		return
	}
	s.start()
	select {
	case s.queue <- event:
	default:
		s.logger.Warn("notifications: queue is full; dropping event",
			"event", string(event.Key()), "resource_id", event.ResourceID.String())
	}
}

// start boots the worker pool exactly once.
func (s *Service) start() {
	s.startOnce.Do(func() {
		for i := 0; i < s.workers; i++ {
			s.wg.Add(1)
			go s.worker()
		}
	})
}

// worker consumes queued events until the service closes.
func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case event := <-s.queue:
			s.deliver(event)
		}
	}
}

// deliver resolves the channels one event covers and sends to each. Failures
// are isolated: a broken channel is logged and never stops the others.
func (s *Service) deliver(event Event) {
	readCtx, cancel := context.WithTimeout(s.baseCtx, dispatchReadTimeout)
	channels, err := s.repo.ListChannelsForEvent(readCtx, event.TeamID, event.ResourceType, event.ResourceID)
	cancel()
	if err != nil {
		if s.baseCtx.Err() == nil {
			s.logger.Error("notifications: resolve channels for event",
				"event", string(event.Key()), "team_id", event.TeamID.String(), "error", err)
		}
		return
	}
	seen := make(map[uuid.UUID]struct{}, len(channels))
	for _, channel := range channels {
		if _, ok := seen[channel.ID]; ok {
			continue
		}
		seen[channel.ID] = struct{}{}
		if !channel.Enabled || !channel.Wants(event) {
			continue
		}
		s.send(channel, event)
	}
}

// send builds the channel's notifier and delivers the event under its own
// timeout.
func (s *Service) send(channel Channel, event Event) {
	notifier, err := s.factory(channel)
	if err != nil {
		s.logger.Error("notifications: build notifier",
			"channel_id", channel.ID.String(), "kind", string(channel.Kind), "error", err)
		return
	}
	// Detached from the base context so a shutdown does not cut an in-flight
	// delivery short with a spurious cancellation; the timeout still bounds it.
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(s.baseCtx), s.sendTimeout)
	defer cancel()
	if err := notifier.Notify(sendCtx, event); err != nil {
		s.logger.Warn("notifications: delivery failed",
			"channel_id", channel.ID.String(), "kind", string(channel.Kind),
			"event", string(event.Key()), "error", err)
		return
	}
	s.logger.Info("notifications: delivered",
		"channel_id", channel.ID.String(), "kind", string(channel.Kind), "event", string(event.Key()))
}

// notifierFor builds the transport notifier of one stored channel, opening
// its sealed config.
func (s *Service) notifierFor(channel Channel) (Notifier, error) {
	config, err := openConfig(s.secret, channel.SealedConfig)
	if err != nil {
		return nil, err
	}
	switch channel.Kind {
	case KindDiscord:
		return newDiscordNotifier(config.WebhookURL, s.client), nil
	case KindSlack:
		return newSlackNotifier(config.WebhookURL, s.client), nil
	case KindTelegram:
		return newTelegramNotifier(config.BotToken, config.ChatID, "", s.client), nil
	case KindEmail:
		return newEmailNotifier(s.mailer, config), nil
	default:
		return nil, fmt.Errorf("%w: unknown channel kind %q", ErrValidation, channel.Kind)
	}
}
