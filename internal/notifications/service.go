package notifications

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/updates"
)

const (
	defaultWorkers        = 4
	defaultQueueSize      = 64
	defaultSendTimeout    = 10 * time.Second
	dispatchReadTimeout   = 5 * time.Second
	deliveryRecordTimeout = 5 * time.Second

	defaultSweepInterval = 15 * time.Second
	defaultRetryBase     = 30 * time.Second
	maxRetryDelay        = time.Hour
)

type NotificationService interface {
	ListChannels(ctx context.Context, userID uuid.UUID) ([]ChannelView, error)
	GetChannel(ctx context.Context, userID, channelID uuid.UUID) (ChannelView, error)
	CreateChannel(ctx context.Context, userID uuid.UUID, req ChannelRequest) (ChannelView, error)
	UpdateChannel(ctx context.Context, userID, channelID uuid.UUID, req ChannelRequest) (ChannelView, error)
	DeleteChannel(ctx context.Context, userID, channelID uuid.UUID) error
	TestChannel(ctx context.Context, userID, channelID uuid.UUID) (TestResult, error)
	ListDeliveries(ctx context.Context, userID, channelID uuid.UUID) ([]DeliveryView, error)
	ListDeliveryFailures(ctx context.Context, userID uuid.UUID) ([]DeliveryFailureView, error)
	RetryDelivery(ctx context.Context, userID, deliveryID uuid.UUID) error
	ListNotifications(ctx context.Context, userID uuid.UUID, limit int) (NotificationPage, error)
	MarkNotificationsSeen(ctx context.Context, userID uuid.UUID, seenAt time.Time) error
}

type Config struct {
	Store       *store.Store
	Repository  Repository
	Secret      string
	Logger      *slog.Logger
	Mailer      Mailer
	HTTPClient  *http.Client
	Publisher   Publisher
	Workers     int
	QueueSize   int
	SendTimeout time.Duration
	Now         func() time.Time

	SweepInterval time.Duration
	RetryBase     time.Duration
	MaxAttempts   int
	Lease         time.Duration
}

func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

type Service struct {
	repo        Repository
	secret      string
	logger      *slog.Logger
	client      *http.Client
	mailer      Mailer
	workers     int
	sendTimeout time.Duration
	now         func() time.Time

	factory func(Channel) (Notifier, error)
	emitter *Emitter

	baseCtx    context.Context
	baseCancel context.CancelFunc
	wake       chan struct{}
	wg         sync.WaitGroup
	startOnce  sync.Once
	closeOnce  sync.Once

	sweepInterval time.Duration
	retryBase     time.Duration
	maxAttempts   int
	lease         time.Duration
}

var (
	_ NotificationService      = (*Service)(nil)
	_ deploy.Notifier          = (*Service)(nil)
	_ databases.BackupNotifier = (*Service)(nil)
)

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
	sweepInterval := cfg.SweepInterval
	if sweepInterval <= 0 {
		sweepInterval = defaultSweepInterval
	}
	retryBase := cfg.RetryBase
	if retryBase <= 0 {
		retryBase = defaultRetryBase
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = maxDeliveryAttempts
	}
	lease := cfg.Lease
	if lease <= 0 {
		lease = deliveryLease
	}
	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		secret = randomSecret()
		logger.Warn("notifications: GOTHAM_SECRET_KEY is empty; channel configs are sealed with an ephemeral key and will not survive a restart")
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())

	service := &Service{
		repo:          cfg.repository(),
		secret:        secret,
		logger:        logger,
		client:        client,
		mailer:        mailer,
		workers:       workers,
		sendTimeout:   sendTimeout,
		now:           now,
		emitter:       NewEmitter(cfg.Publisher),
		baseCtx:       baseCtx,
		baseCancel:    baseCancel,
		wake:          make(chan struct{}, 1),
		sweepInterval: sweepInterval,
		retryBase:     retryBase,
		maxAttempts:   maxAttempts,
		lease:         lease,
	}
	service.factory = service.notifierFor
	return service
}

func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("notifications: generate secret: %v", err))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func NewDefaultService(cfg Config) NotificationService {
	if cfg.repository() == nil || !Enabled() {
		return nil
	}
	service := NewService(cfg)
	service.Start()
	return service
}

func (s *Service) Start() {
	if s == nil {
		return
	}
	s.start()
}

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

func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errNotReady
	}
	return nil
}

func (s *Service) view(channel Channel) (ChannelView, error) {
	config, err := openConfig(s.secret, channel.SealedConfig)
	if err != nil {
		return ChannelView{}, err
	}
	return ChannelView{
		ID:                channel.ID,
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

func resourceIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func (s *Service) ListChannels(ctx context.Context, userID uuid.UUID) ([]ChannelView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	channels, err := s.repo.ListChannels(ctx)
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
	if err := s.attachHealth(ctx, userID, views); err != nil {
		return nil, err
	}
	return views, nil
}

func (s *Service) attachHealth(ctx context.Context, userID uuid.UUID, views []ChannelView) error {
	owner, err := s.repo.IsOwner(ctx, userID)
	if err != nil || !owner {
		return err
	}
	health, err := s.repo.ChannelHealth(ctx)
	if err != nil {
		return err
	}
	for i := range views {
		item, ok := health[views[i].ID]
		if !ok {
			item = ChannelHealth{Status: "unknown"}
		}
		views[i].Health = &item
	}
	return nil
}

func (s *Service) GetChannel(ctx context.Context, userID, channelID uuid.UUID) (ChannelView, error) {
	if err := s.ready(); err != nil {
		return ChannelView{}, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return ChannelView{}, ErrNotFound
	}
	channel, err := s.repo.GetChannel(ctx, channelID)
	if err != nil {
		return ChannelView{}, err
	}
	view, err := s.view(channel)
	if err != nil {
		return ChannelView{}, err
	}
	views := []ChannelView{view}
	if err := s.attachHealth(ctx, userID, views); err != nil {
		return ChannelView{}, err
	}
	return views[0], nil
}

func (s *Service) CreateChannel(ctx context.Context, userID uuid.UUID, req ChannelRequest) (ChannelView, error) {
	if err := s.ready(); err != nil {
		return ChannelView{}, err
	}
	if userID == uuid.Nil {
		return ChannelView{}, ErrNotFound
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
	config := req.Config.normalized()
	if err := config.validate(kind); err != nil {
		return ChannelView{}, err
	}
	sealed, err := sealConfig(s.secret, config)
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

func (s *Service) UpdateChannel(ctx context.Context, userID, channelID uuid.UUID, req ChannelRequest) (ChannelView, error) {
	if err := s.ready(); err != nil {
		return ChannelView{}, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return ChannelView{}, ErrNotFound
	}
	existing, err := s.repo.GetChannel(ctx, channelID)
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
		merged := stored.merged(req.Config).normalized()
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

func (s *Service) DeleteChannel(ctx context.Context, userID, channelID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return ErrNotFound
	}
	return s.repo.DeleteChannel(ctx, channelID)
}

func (s *Service) TestChannel(ctx context.Context, userID, channelID uuid.UUID) (TestResult, error) {
	if err := s.ready(); err != nil {
		return TestResult{}, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return TestResult{}, ErrNotFound
	}
	channel, err := s.repo.GetChannel(ctx, channelID)
	if err != nil {
		return TestResult{}, err
	}
	notifier, err := s.factory(channel)
	if err != nil {
		return TestResult{}, err
	}
	event := Event{
		Kind:         EventTest,
		Outcome:      OutcomeSuccess,
		ResourceType: channel.ResourceType,
		ResourceID:   channel.ResourceID,
		Name:         channel.Name,
		At:           s.now().UTC(),
	}
	sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()
	if err := notifier.Notify(sendCtx, event); err != nil {
		summary := truncateText(err.Error(), maxDeliveryError)
		s.recordDelivery(ctx, channel.ID, testEventType, DeliveryFailed, summary)
		return TestResult{OK: false, Message: truncateText(err.Error(), maxDeliveryError)}, nil
	}
	s.recordDelivery(ctx, channel.ID, testEventType, DeliverySucceeded, "")
	return TestResult{OK: true, Message: "test event delivered"}, nil
}

func (s *Service) ListDeliveries(ctx context.Context, userID, channelID uuid.UUID) ([]DeliveryView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil || channelID == uuid.Nil {
		return nil, ErrNotFound
	}
	if _, err := s.repo.GetChannel(ctx, channelID); err != nil {
		return nil, err
	}
	deliveries, err := s.repo.ListDeliveries(ctx, channelID, deliveryHistoryLimit)
	if err != nil {
		return nil, err
	}
	views := make([]DeliveryView, 0, len(deliveries))
	for _, delivery := range deliveries {
		views = append(views, deliveryView(delivery))
	}
	return views, nil
}

func deliveryView(delivery Delivery) DeliveryView {
	view := DeliveryView{
		ID:           delivery.ID,
		ChannelID:    delivery.ChannelID,
		EventType:    delivery.EventType,
		Status:       delivery.Status,
		ErrorSummary: delivery.ErrorSummary,
		CreatedAt:    delivery.CreatedAt,
		Attempts:     delivery.Attempts,
		CompletedAt:  delivery.CompletedAt,
		Retryable:    delivery.Status == DeliveryFailed,
	}
	if delivery.Status == DeliveryRetrying && !delivery.NextAttemptAt.IsZero() {
		next := delivery.NextAttemptAt
		view.NextRetryAt = &next
	}
	return view
}

func (s *Service) ListDeliveryFailures(ctx context.Context, userID uuid.UUID) ([]DeliveryFailureView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	owner, err := s.repo.IsOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, ErrForbidden
	}
	failures, err := s.repo.ListTerminalFailures(ctx, deliveryHistoryLimit)
	if err != nil {
		return nil, err
	}
	views := make([]DeliveryFailureView, 0, len(failures))
	for _, failure := range failures {
		views = append(views, DeliveryFailureView{
			ID:           failure.ID,
			ChannelID:    failure.ChannelID,
			ChannelName:  failure.ChannelName,
			ChannelKind:  failure.ChannelKind,
			EventType:    failure.EventType,
			ErrorSummary: failure.ErrorSummary,
			Attempts:     failure.Attempts,
			FailedAt:     failure.FailedAt,
			Retryable:    true,
		})
	}
	return views, nil
}

func (s *Service) RetryDelivery(ctx context.Context, userID, deliveryID uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	owner, err := s.repo.IsOwner(ctx, userID)
	if err != nil {
		return err
	}
	if !owner {
		return ErrForbidden
	}
	if deliveryID == uuid.Nil {
		return ErrNotFound
	}
	ok, err := s.repo.RetryDelivery(ctx, deliveryID, s.now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	s.start()
	s.kick()
	return nil
}

func (s *Service) ListNotifications(ctx context.Context, userID uuid.UUID, limit int) (NotificationPage, error) {
	if err := s.ready(); err != nil {
		return NotificationPage{}, err
	}
	if userID == uuid.Nil {
		return NotificationPage{Notifications: []NotificationView{}}, nil
	}
	notifications, unread, err := s.repo.ListNotifications(ctx, userID, limit)
	if err != nil {
		return NotificationPage{}, err
	}
	views := make([]NotificationView, 0, len(notifications))
	for _, notification := range notifications {
		views = append(views, notificationView(notification))
	}
	return NotificationPage{Notifications: views, UnreadCount: unread}, nil
}

func notificationView(notification Notification) NotificationView {
	return NotificationView{
		ID:           notification.ID,
		EventKey:     notification.EventKey,
		ResourceType: notification.ResourceType,
		ResourceID:   resourceIDString(notification.ResourceID),
		ProjectID:    resourceIDString(notification.ProjectID),
		Name:         notification.Name,
		Host:         notification.Host,
		Error:        notification.Error,
		Detail:       notification.Detail,
		CreatedAt:    notification.CreatedAt,
	}
}

func (s *Service) MarkNotificationsSeen(ctx context.Context, userID uuid.UUID, seenAt time.Time) error {
	if err := s.ready(); err != nil {
		return err
	}
	if userID == uuid.Nil {
		return ErrForbidden
	}
	if seenAt.IsZero() {
		seenAt = s.now().UTC()
	}
	return s.repo.MarkNotificationsSeen(ctx, userID, seenAt)
}

func (s *Service) buildNotification(event Event) Notification {
	notification := Notification{}
	if s == nil || s.repo == nil {
		return notification
	}
	writeCtx, cancel := context.WithTimeout(s.baseCtx, dispatchReadTimeout)
	defer cancel()
	projectID, err := s.repo.ResourceProject(writeCtx, event.ResourceType, event.ResourceID)
	if err != nil {
		projectID = uuid.Nil
	}
	at := event.At
	if at.IsZero() {
		at = s.now().UTC()
	}
	notification = Notification{
		ID:           uuid.New(),
		EventKey:     event.Key(),
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		ProjectID:    projectID,
		Name:         event.Name,
		Host:         event.Host,
		Error:        truncateText(event.Error, maxEventError),
		Detail:       event.Detail,
		CreatedAt:    at.UTC(),
		DedupeKey:    event.Identity(),
	}
	return notification
}

func (s *Service) publishNotification(ctx context.Context, notification Notification) {
	if s.emitter == nil || s.emitter.pub == nil {
		return
	}
	recipients, err := s.repo.ListNotificationRecipients(ctx, notification.ProjectID)
	if err != nil {
		if s.baseCtx.Err() == nil {
			s.logger.Error("notifications: resolve realtime recipients",
				"event", string(notification.EventKey), "resource_id", notification.ResourceID.String(), "error", err)
		}
		return
	}
	view := notificationView(notification)
	for _, recipient := range recipients {
		if err := s.emitter.Emit(ctx, recipient, view); err != nil {
			s.logger.Warn("notifications: publish realtime notification",
				"user_id", recipient.String(), "notification_id", notification.ID.String(), "error", err)
		}
	}
}

func (s *Service) recordDelivery(ctx context.Context, channelID uuid.UUID, eventType string, status DeliveryState, errorSummary string) {
	if s == nil || s.repo == nil || channelID == uuid.Nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryRecordTimeout)
	defer cancel()
	now := s.now().UTC()
	completed := now
	if err := s.repo.CreateDelivery(writeCtx, Delivery{
		ID:            uuid.New(),
		ChannelID:     channelID,
		EventType:     eventType,
		Status:        status,
		ErrorSummary:  truncateText(errorSummary, maxDeliveryError),
		CreatedAt:     now,
		NextAttemptAt: now,
		CompletedAt:   &completed,
		UpdatedAt:     now,
	}); err != nil {
		s.logger.Warn("notifications: record delivery",
			"channel_id", channelID.String(), "status", string(status), "error", err)
	}
}

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
		ResourceType: ResourceApplication,
		ResourceID:   result.ApplicationID,
		Name:         result.Application,
		Host:         result.Host,
		Error:        result.Error,
		At:           result.FinishedAt,
	})
}

func (s *Service) RedeployNeeded(_ context.Context, result deploy.RedeployResult) {
	if s == nil || !Enabled() {
		return
	}
	s.Dispatch(Event{
		Kind:         EventRedeploy,
		Outcome:      OutcomeSuccess,
		ResourceType: ResourceApplication,
		ResourceID:   result.ApplicationID,
		Name:         result.Application,
		Detail:       strings.Join(deploy.SortPendingChanges(result.Changes), ","),
		At:           result.At,
	})
}

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
		ResourceType: ResourceDatabase,
		ResourceID:   result.DatabaseID,
		Name:         result.Database,
		Error:        result.Error,
		At:           result.FinishedAt,
	})
}

func (s *Service) ServerStateChanged(_ context.Context, change servers.StateChange) {
	if s == nil || !Enabled() {
		return
	}
	outcome := OutcomeFailure
	reason := "No heartbeat received for at least 30 seconds"
	if change.Online {
		outcome = OutcomeSuccess
		reason = ""
	}
	s.Dispatch(Event{
		Kind: EventServer, Outcome: outcome, Name: change.Name,
		Host: change.Host, Error: reason, At: change.At,
	})
}

func (s *Service) CertificateExpiring(_ context.Context, expiry proxy.CertificateExpiry) {
	if s == nil || !Enabled() {
		return
	}
	s.Dispatch(Event{
		Kind: EventCertificate, Outcome: OutcomeFailure, Name: expiry.Domain, Host: expiry.Domain,
		Error: "Certificate expires at " + expiry.NotAfter.UTC().Format(time.RFC3339), At: expiry.At,
	})
}

func (s *Service) UpdateAvailable(_ context.Context, update updates.AvailableUpdate) {
	if s == nil || !Enabled() {
		return
	}
	s.Dispatch(Event{
		Kind: EventUpdate, Outcome: OutcomeSuccess,
		Name: update.Version + " (" + string(update.Channel) + ")", At: update.At,
	})
}

func (s *Service) Dispatch(event Event) {
	if s == nil {
		return
	}
	if err := s.Enqueue(event); err != nil && s.repo != nil && Enabled() {
		// The producer never retries these events, so make the loss visible.
		event.At = s.now().UTC()
		s.logger.Error("notifications: dispatch failed",
			"event", string(event.Key()), "identity", event.Identity(), "error", err)
	}
}

// Enqueue persists the inbox row and one pending delivery per matching channel
// before it wakes a worker. A persisted delivery survives a restart even when
// the wake is lost; a duplicate producer conflicts on the local identity.
func (s *Service) Enqueue(event Event) error {
	if s == nil || s.repo == nil || !Enabled() {
		return errNotReady
	}
	if event.At.IsZero() {
		event.At = s.now().UTC()
	}
	s.start()

	notification := s.buildNotification(event)

	readCtx, cancel := context.WithTimeout(s.baseCtx, dispatchReadTimeout)
	channels, err := s.repo.ListChannelsForEvent(readCtx, event.ResourceType, event.ResourceID)
	cancel()
	if err != nil {
		s.logger.Error("notifications: resolve channels for event",
			"event", string(event.Key()), "resource_id", event.ResourceID.String(), "error", err)
		return fmt.Errorf("notifications: resolve channels: %w", err)
	}
	identity := event.Identity()
	payload := event.encode()
	now := s.now().UTC()
	// Incident transitions are persisted in lifecycle order; their own time is
	// the stable ordering key the claim uses across restarts.
	createdAt := now
	if event.IncidentID != uuid.Nil && !event.At.IsZero() {
		createdAt = event.At.UTC()
	}
	seen := make(map[uuid.UUID]struct{}, len(channels))
	deliveries := make([]Delivery, 0, len(channels))
	for _, channel := range channels {
		if _, ok := seen[channel.ID]; ok {
			continue
		}
		seen[channel.ID] = struct{}{}
		if !channel.Enabled || !channel.Wants(event) {
			continue
		}
		deliveries = append(deliveries, Delivery{
			ID:            uuid.New(),
			ChannelID:     channel.ID,
			EventType:     string(event.Key()),
			Status:        DeliveryPending,
			CreatedAt:     createdAt,
			DedupeKey:     identity,
			Payload:       payload,
			IncidentID:    event.IncidentID,
			NextAttemptAt: createdAt,
			ExpiresAt:     createdAt.Add(maxDeliveryAge),
		})
	}
	writeCtx, writeCancel := context.WithTimeout(s.baseCtx, dispatchReadTimeout)
	defer writeCancel()
	inserted, err := s.repo.PersistEvent(writeCtx, notification, deliveries)
	if err != nil {
		s.logger.Error("notifications: persist event",
			"event", string(event.Key()), "identity", identity,
			"resource_id", event.ResourceID.String(), "error", err)
		return err
	}
	if inserted {
		publishCtx, publishCancel := context.WithTimeout(s.baseCtx, dispatchReadTimeout)
		s.publishNotification(publishCtx, notification)
		publishCancel()
	}
	s.kick()
	return nil
}

func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) start() {
	s.startOnce.Do(func() {
		for i := 0; i < s.workers; i++ {
			s.wg.Add(1)
			go s.worker()
		}
		// Resume work persisted by a previous process.
		s.kick()
	})
}

func (s *Service) worker() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case <-s.wake:
			s.processDeliveries()
		case <-ticker.C:
			s.processDeliveries()
		}
	}
}

// processDeliveries reclaims abandoned leases, expires stale work and sends
// bounded claimed batches until the queue has nothing eligible.
func (s *Service) processDeliveries() {
	if s == nil || s.repo == nil {
		return
	}
	now := s.now().UTC()
	if _, err := s.repo.ReclaimDeliveries(s.baseCtx, now, s.maxAttempts); err != nil && s.baseCtx.Err() == nil {
		s.logger.Warn("notifications: reclaim abandoned deliveries", "error", err)
	}
	if _, err := s.repo.ExpireDeliveries(s.baseCtx, now); err != nil && s.baseCtx.Err() == nil {
		s.logger.Warn("notifications: expire stale deliveries", "error", err)
	}
	if _, err := s.repo.CleanupDeliveries(s.baseCtx, now.Add(-terminalRetention)); err != nil && s.baseCtx.Err() == nil {
		s.logger.Warn("notifications: clean up terminal deliveries", "error", err)
	}
	if _, err := s.repo.PruneDelivered(s.baseCtx, deliveryRetention); err != nil && s.baseCtx.Err() == nil {
		s.logger.Warn("notifications: prune delivered history", "error", err)
	}
	for {
		claimed, err := s.repo.ClaimDeliveries(s.baseCtx, now, now.Add(s.lease), deliveryClaimBatch)
		if err != nil {
			if s.baseCtx.Err() == nil {
				s.logger.Warn("notifications: claim deliveries", "error", err)
			}
			return
		}
		for _, delivery := range claimed {
			s.sendDelivery(delivery)
		}
		if len(claimed) < deliveryClaimBatch {
			return
		}
		now = s.now().UTC()
	}
}

func (s *Service) sendDelivery(delivery Delivery) {
	event, err := decodeEvent(delivery.Payload)
	if err != nil {
		s.finishDelivery(delivery, DeliveryFailed, "invalid delivery payload", time.Time{})
		return
	}
	channel, err := s.repo.GetChannel(s.baseCtx, delivery.ChannelID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return // the channel row and its deliveries were deleted together
		}
		s.retryOrFail(delivery, 0, "channel lookup failed")
		return
	}
	if !channel.Enabled || !channel.Wants(event) {
		s.cancelDelivery(delivery, "channel disabled or unsubscribed")
		return
	}
	if event.IncidentID != uuid.Nil && event.Action != "recovered" && s.hasLaterIncidentTransition(delivery) {
		s.cancelDelivery(delivery, "incident already progressed")
		return
	}
	if event.Action == "recovered" && !s.openingDelivered(delivery) {
		s.cancelDelivery(delivery, "opening was not delivered")
		return
	}
	notifier, err := s.factory(channel)
	if err != nil {
		s.finishDelivery(delivery, DeliveryFailed, truncateText(err.Error(), maxDeliveryError), time.Time{})
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(s.baseCtx), s.sendTimeout)
	defer cancel()
	if err := notifier.Notify(sendCtx, event); err != nil {
		s.logger.Warn("notifications: delivery failed",
			"channel_id", channel.ID.String(), "kind", string(channel.Kind),
			"event", string(event.Key()), "error", err)
		permanent, retryAfter := classifyDeliveryError(err, channel.Kind)
		if permanent {
			s.finishDelivery(delivery, DeliveryFailed, err.Error(), time.Time{})
			return
		}
		s.retryOrFail(delivery, retryAfter, err.Error())
		return
	}
	s.logger.Info("notifications: delivered",
		"channel_id", channel.ID.String(), "kind", string(channel.Kind), "event", string(event.Key()))
	s.finishDelivery(delivery, DeliverySucceeded, "", time.Time{})
}

func (s *Service) hasLaterIncidentTransition(delivery Delivery) bool {
	ok, err := s.repo.HasLaterIncidentTransition(s.baseCtx, delivery.IncidentID, delivery.ChannelID, delivery)
	if err != nil {
		s.logger.Warn("notifications: check later incident transition", "error", err)
		return false
	}
	return ok
}

func (s *Service) openingDelivered(delivery Delivery) bool {
	if delivery.IncidentID == uuid.Nil {
		return true
	}
	ok, err := s.repo.HasDeliveredOpening(s.baseCtx, delivery.IncidentID, delivery.ChannelID)
	if err != nil {
		s.logger.Warn("notifications: check delivered opening", "error", err)
		return true
	}
	return ok
}

func (s *Service) retryOrFail(delivery Delivery, retryAfter time.Duration, reason string) {
	now := s.now().UTC()
	if delivery.Attempts >= s.maxAttempts || (!delivery.ExpiresAt.IsZero() && !now.Before(delivery.ExpiresAt)) {
		summary := truncateText(reason, maxDeliveryError)
		if delivery.Attempts >= s.maxAttempts {
			summary = truncateText(reason+"; giving up after "+strconv.Itoa(delivery.Attempts)+" attempts", maxDeliveryError)
		}
		s.finishDelivery(delivery, DeliveryFailed, summary, now)
		return
	}
	s.finishDelivery(delivery, DeliveryRetrying, reason, now.Add(s.retryDelay(delivery.Attempts, retryAfter)))
}

func (s *Service) retryDelay(attempts int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxRetryDelay {
			return maxRetryDelay
		}
		return retryAfter
	}
	delay := s.retryBase
	for i := 1; i < attempts && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	jitter := time.Duration(mathrand.Int63n(int64(delay/5) + 1))
	return delay + jitter
}

func (s *Service) finishDelivery(delivery Delivery, status DeliveryState, summary string, next time.Time) {
	now := s.now().UTC()
	delivery.Status = status
	delivery.ErrorSummary = truncateText(summary, maxDeliveryError)
	delivery.LeaseUntil = nil
	delivery.UpdatedAt = now
	if next.IsZero() {
		delivery.NextAttemptAt = now
	} else {
		delivery.NextAttemptAt = next
	}
	if status == DeliveryFailed || status == DeliverySucceeded || status == DeliveryCancelled {
		completed := now
		delivery.CompletedAt = &completed
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(s.baseCtx), deliveryRecordTimeout)
	defer cancel()
	if err := s.repo.FinishDelivery(writeCtx, delivery); err != nil {
		s.logger.Warn("notifications: record delivery outcome",
			"delivery_id", delivery.ID.String(), "status", string(status), "error", err)
	}
}

func (s *Service) cancelDelivery(delivery Delivery, reason string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(s.baseCtx), deliveryRecordTimeout)
	defer cancel()
	if err := s.repo.CancelDelivery(writeCtx, delivery.ID, truncateText(reason, maxDeliveryError), s.now().UTC()); err != nil {
		s.logger.Warn("notifications: cancel delivery", "delivery_id", delivery.ID.String(), "error", err)
	}
}

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
