package notifications

import (
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

const FeatureEnv = "FEATURE_NOTIFICATIONS"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const (
	ResourceApplication = "application"
	ResourceDatabase    = "database"
)

const maxNameLength = 100

type Kind string

const (
	KindDiscord  Kind = "discord"
	KindSlack    Kind = "slack"
	KindTelegram Kind = "telegram"
	KindEmail    Kind = "email"
)

func (k Kind) Valid() bool {
	switch k {
	case KindDiscord, KindSlack, KindTelegram, KindEmail:
		return true
	default:
		return false
	}
}

func ParseKind(raw string) (Kind, error) {
	kind := Kind(strings.TrimSpace(raw))
	if !kind.Valid() {
		return "", fmt.Errorf("%w: kind must be one of discord, slack, telegram, email", ErrValidation)
	}
	return kind, nil
}

type EventKey string

const (
	EventDeploySuccess EventKey = "deploy_success"
	EventDeployFailure EventKey = "deploy_failure"
	EventBackupSuccess EventKey = "backup_success"
	EventBackupFailure EventKey = "backup_failure"
	EventServerOffline EventKey = "server_offline"
	EventServerOnline  EventKey = "server_online"
)

var defaultEvents = []EventKey{EventDeploySuccess, EventDeployFailure, EventBackupSuccess, EventBackupFailure}

var AllEvents = append(append([]EventKey(nil), defaultEvents...), EventServerOffline, EventServerOnline)

func (k EventKey) Valid() bool {
	for _, known := range AllEvents {
		if k == known {
			return true
		}
	}
	return false
}

type EventKind string

const (
	EventDeploy EventKind = "deploy"
	EventBackup EventKind = "backup"
	EventTest   EventKind = "test"
	EventServer EventKind = "server"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

type Event struct {
	Kind         EventKind
	Outcome      Outcome
	ResourceType string
	ResourceID   uuid.UUID
	Name         string
	Host         string
	Error        string
	At           time.Time
}

func (e Event) Key() EventKey {
	switch {
	case e.Kind == EventServer && e.Outcome == OutcomeSuccess:
		return EventServerOnline
	case e.Kind == EventServer && e.Outcome == OutcomeFailure:
		return EventServerOffline
	case e.Kind == EventDeploy && e.Outcome == OutcomeSuccess:
		return EventDeploySuccess
	case e.Kind == EventDeploy && e.Outcome == OutcomeFailure:
		return EventDeployFailure
	case e.Kind == EventBackup && e.Outcome == OutcomeSuccess:
		return EventBackupSuccess
	case e.Kind == EventBackup && e.Outcome == OutcomeFailure:
		return EventBackupFailure
	default:
		return ""
	}
}

func (e Event) Summary() string {
	if e.Kind == EventTest {
		return "Test notification: " + e.Name
	}
	if e.Kind == EventServer {
		if e.Outcome == OutcomeSuccess {
			return "Server is back online: " + e.Name
		}
		return "Server is offline: " + e.Name
	}
	noun := "Deploy"
	if e.Kind == EventBackup {
		noun = "Backup"
	}
	verb := "failed"
	if e.Outcome == OutcomeSuccess {
		verb = "succeeded"
	}
	return fmt.Sprintf("%s %s: %s", noun, verb, e.Name)
}

const maxEventError = 500

func (e Event) Message() string {
	lines := []string{e.Summary()}
	if e.Host != "" {
		lines = append(lines, "Host: "+e.Host)
	}
	if e.Error != "" {
		lines = append(lines, "Error: "+truncateText(e.Error, maxEventError))
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	lines = append(lines, "Time: "+at.UTC().Format(time.RFC3339))
	return strings.Join(lines, "\n")
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 {
		return ""
	}
	if utf16Length(text) <= limit {
		return text
	}
	budget := limit - 1
	var b strings.Builder
	used := 0
	for _, r := range text {
		width := utf16.RuneLen(r)
		if width <= 0 {
			width = 1
		}
		if used+width > budget {
			break
		}
		b.WriteRune(r)
		used += width
	}
	return b.String() + "…"
}

func utf16Length(text string) int {
	total := 0
	for _, r := range text {
		width := utf16.RuneLen(r)
		if width <= 0 {
			width = 1
		}
		total += width
	}
	return total
}

type ChannelConfig struct {
	WebhookURL string   `json:"webhook_url,omitempty"`
	BotToken   string   `json:"bot_token,omitempty"`
	ChatID     string   `json:"chat_id,omitempty"`
	Host       string   `json:"host,omitempty"`
	Port       int      `json:"port,omitempty"`
	Username   string   `json:"username,omitempty"`
	Password   string   `json:"password,omitempty"`
	From       string   `json:"from,omitempty"`
	To         []string `json:"to,omitempty"`
}

func (c ChannelConfig) validate(kind Kind) error {
	switch kind {
	case KindDiscord, KindSlack:
		if err := validateWebhookURL(c.WebhookURL); err != nil {
			return err
		}
	case KindTelegram:
		token := strings.TrimSpace(c.BotToken)
		if token == "" {
			return fmt.Errorf("%w: config.bot_token is required for telegram", ErrValidation)
		}
		if !telegramTokenPattern.MatchString(token) {
			return fmt.Errorf("%w: config.bot_token is not a valid telegram bot token", ErrValidation)
		}
		if strings.TrimSpace(c.ChatID) == "" {
			return fmt.Errorf("%w: config.chat_id is required for telegram", ErrValidation)
		}
	case KindEmail:
		host := strings.TrimSpace(c.Host)
		if host == "" {
			return fmt.Errorf("%w: config.host is required for email", ErrValidation)
		}
		if err := validateOutboundHost(host); err != nil {
			return err
		}
		if c.Port < 0 || c.Port > 65535 {
			return fmt.Errorf("%w: config.port must be between 0 and 65535", ErrValidation)
		}
		if _, err := mail.ParseAddress(strings.TrimSpace(c.From)); err != nil {
			return fmt.Errorf("%w: config.from must be a valid email address", ErrValidation)
		}
		if len(c.To) == 0 {
			return fmt.Errorf("%w: config.to must name at least one recipient", ErrValidation)
		}
		for _, recipient := range c.To {
			if _, err := mail.ParseAddress(strings.TrimSpace(recipient)); err != nil {
				return fmt.Errorf("%w: config.to contains an invalid email address", ErrValidation)
			}
		}
	default:
		return fmt.Errorf("%w: kind must be one of discord, slack, telegram, email", ErrValidation)
	}
	return nil
}

var telegramTokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

func validateWebhookURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%w: config.webhook_url is required", ErrValidation)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: config.webhook_url must be an http(s) URL", ErrValidation)
	}
	return validateOutboundHost(parsed.Hostname())
}

func validateOutboundHost(host string) error {
	host = strings.TrimSpace(host)
	if split, _, err := net.SplitHostPort(host); err == nil {
		host = split
	}
	host = strings.Trim(host, "[]")
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("%w: link-local and metadata addresses are not allowed", ErrValidation)
	}
	return nil
}

func (c ChannelConfig) isZero() bool {
	return c.WebhookURL == "" && c.BotToken == "" && c.ChatID == "" && c.Host == "" &&
		c.Port == 0 && c.Username == "" && c.Password == "" && c.From == "" && c.To == nil
}

func (c ChannelConfig) normalized() ChannelConfig {
	normalized := c
	normalized.WebhookURL = strings.TrimSpace(c.WebhookURL)
	normalized.BotToken = strings.TrimSpace(c.BotToken)
	normalized.ChatID = strings.TrimSpace(c.ChatID)
	normalized.Host = strings.TrimSpace(c.Host)
	normalized.Username = strings.TrimSpace(c.Username)
	normalized.From = strings.TrimSpace(c.From)
	if c.To != nil {
		normalized.To = make([]string, 0, len(c.To))
		for _, recipient := range c.To {
			normalized.To = append(normalized.To, strings.TrimSpace(recipient))
		}
	}
	return normalized
}

func (c ChannelConfig) merged(update ChannelConfig) ChannelConfig {
	merged := c
	if update.WebhookURL != "" && update.WebhookURL != maskSecret(c.WebhookURL) {
		merged.WebhookURL = update.WebhookURL
	}
	if update.BotToken != "" && update.BotToken != maskSecret(c.BotToken) {
		merged.BotToken = update.BotToken
	}
	if update.ChatID != "" {
		merged.ChatID = update.ChatID
	}
	if update.Host != "" {
		merged.Host = update.Host
	}
	if update.Port != 0 {
		merged.Port = update.Port
	}
	if update.Username != "" {
		merged.Username = update.Username
	}
	if update.Password != "" && update.Password != maskSecret(c.Password) {
		merged.Password = update.Password
	}
	if update.From != "" {
		merged.From = update.From
	}
	if update.To != nil {
		merged.To = append([]string(nil), update.To...)
	}
	return merged
}

func (c ChannelConfig) hasSecrets() bool {
	return c.WebhookURL != "" || c.BotToken != "" || c.Password != ""
}

func (c ChannelConfig) redacted() ChannelConfig {
	return ChannelConfig{
		WebhookURL: maskSecret(c.WebhookURL),
		BotToken:   maskSecret(c.BotToken),
		ChatID:     c.ChatID,
		Host:       c.Host,
		Port:       c.Port,
		Username:   c.Username,
		Password:   maskSecret(c.Password),
		From:       c.From,
		To:         append([]string(nil), c.To...),
	}
}

func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "…"
	}
	return "…" + secret[len(secret)-4:]
}

func sealConfig(secret string, config ChannelConfig) (string, error) {
	plain, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("notifications: encode channel config: %w", err)
	}
	sealed, err := providers.SealSecret(secret, string(plain))
	if err != nil {
		return "", fmt.Errorf("notifications: seal channel config: %w", err)
	}
	return sealed, nil
}

func openConfig(secret, sealed string) (ChannelConfig, error) {
	plain, err := providers.OpenSecret(secret, sealed)
	if err != nil {
		return ChannelConfig{}, fmt.Errorf("notifications: open channel config: %w", err)
	}
	var config ChannelConfig
	if err := json.Unmarshal([]byte(plain), &config); err != nil {
		return ChannelConfig{}, fmt.Errorf("notifications: decode channel config: %w", err)
	}
	return config, nil
}

type Channel struct {
	ID           uuid.UUID
	Name         string
	Kind         Kind
	Enabled      bool
	Events       []EventKey
	ResourceType string
	ResourceID   uuid.UUID
	SealedConfig string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (c Channel) Wants(event Event) bool {
	key := event.Key()
	if key == "" {
		return false
	}
	for _, subscribed := range c.Events {
		if subscribed == key {
			return true
		}
	}
	return false
}

type ChannelView struct {
	ID                uuid.UUID     `json:"id"`
	Name              string        `json:"name"`
	Kind              Kind          `json:"kind"`
	Enabled           bool          `json:"enabled"`
	Events            []EventKey    `json:"events"`
	ResourceType      string        `json:"resource_type,omitempty"`
	ResourceID        string        `json:"resource_id,omitempty"`
	Config            ChannelConfig `json:"config"`
	SecretsConfigured bool          `json:"secrets_configured"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type ChannelRequest struct {
	Name         string        `json:"name,omitempty"`
	Kind         Kind          `json:"kind,omitempty"`
	Enabled      *bool         `json:"enabled,omitempty"`
	ResourceType *string       `json:"resource_type,omitempty"`
	ResourceID   *string       `json:"resource_id,omitempty"`
	Events       []EventKey    `json:"events,omitempty"`
	Config       ChannelConfig `json:"config,omitempty"`
}

type TestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

const (
	maxDeliveryError     = 300
	deliveryRetention    = 50
	deliveryHistoryLimit = 20
	testEventType        = "test"
)

type DeliveryStatus string

const (
	DeliverySucceeded DeliveryStatus = "success"
	DeliveryFailed    DeliveryStatus = "failure"
)

type Delivery struct {
	ID           uuid.UUID
	ChannelID    uuid.UUID
	EventType    string
	Status       DeliveryStatus
	ErrorSummary string
	CreatedAt    time.Time
}

type DeliveryView struct {
	ID           uuid.UUID      `json:"id"`
	ChannelID    uuid.UUID      `json:"channel_id"`
	EventType    string         `json:"event_type"`
	Status       DeliveryStatus `json:"status"`
	ErrorSummary string         `json:"error_summary,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

const notificationRetention = 500

type Notification struct {
	ID           uuid.UUID
	EventKey     EventKey
	ResourceType string
	ResourceID   uuid.UUID
	ProjectID    uuid.UUID
	Name         string
	Host         string
	Error        string
	CreatedAt    time.Time
}

type NotificationView struct {
	ID           uuid.UUID `json:"id"`
	EventKey     EventKey  `json:"event_key"`
	ResourceType string    `json:"resource_type,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Error        string    `json:"error"`
	CreatedAt    time.Time `json:"created_at"`
}

type NotificationPage struct {
	Notifications []NotificationView `json:"notifications"`
	UnreadCount   int64              `json:"unread_count"`
}

func parseResource(resourceType, resourceID string) (string, uuid.UUID, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" && resourceID == "" {
		return "", uuid.Nil, nil
	}
	if resourceType != ResourceApplication && resourceType != ResourceDatabase {
		return "", uuid.Nil, fmt.Errorf("%w: resource_type must be application or database", ErrValidation)
	}
	if resourceID == "" {
		return "", uuid.Nil, fmt.Errorf("%w: resource_id is required when resource_type is set", ErrValidation)
	}
	id, err := uuid.Parse(resourceID)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("%w: invalid resource_id", ErrValidation)
	}
	return resourceType, id, nil
}

func normalizeEvents(events []EventKey) ([]EventKey, error) {
	if events == nil {
		return append([]EventKey(nil), defaultEvents...), nil
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%w: events must name at least one event", ErrValidation)
	}
	seen := make(map[EventKey]bool, len(events))
	normalized := make([]EventKey, 0, len(events))
	for _, event := range events {
		if !event.Valid() {
			return nil, fmt.Errorf("%w: unknown event %q", ErrValidation, event)
		}
		if seen[event] {
			continue
		}
		seen[event] = true
		normalized = append(normalized, event)
	}
	return normalized, nil
}
