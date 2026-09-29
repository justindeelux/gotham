package notifications

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// FeatureEnv is the kill switch for the whole notifications surface:
// FEATURE_NOTIFICATIONS=false unmounts the channel routes and turns the
// deploy/backup hooks into no-ops, leaving the core flows untouched. Only an
// explicit false disables it — unset (or any other value) keeps it enabled,
// matching deploy.Enabled and databases.Enabled.
const FeatureEnv = "FEATURE_NOTIFICATIONS"

// Enabled reports whether the notifications feature is on.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// Resource types a channel can override. They mirror the event sources:
// applications for deploys, databases for backups.
const (
	// ResourceApplication scopes a channel to one application's deploys.
	ResourceApplication = "application"
	// ResourceDatabase scopes a channel to one database's backups.
	ResourceDatabase = "database"
)

// maxNameLength bounds a channel name.
const maxNameLength = 100

// Kind is the transport of one notification channel. The values are the
// strings stored in notification_channels.kind.
type Kind string

// Channel kinds.
const (
	// KindDiscord posts a JSON message to a Discord webhook URL.
	KindDiscord Kind = "discord"
	// KindSlack posts a JSON message to a Slack incoming webhook URL.
	KindSlack Kind = "slack"
	// KindTelegram calls the Bot API sendMessage method.
	KindTelegram Kind = "telegram"
	// KindEmail sends an email through SMTP.
	KindEmail Kind = "email"
)

// Valid reports whether k is one of the four stored kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindDiscord, KindSlack, KindTelegram, KindEmail:
		return true
	default:
		return false
	}
}

// ParseKind parses a kind string, rejecting anything else.
func ParseKind(raw string) (Kind, error) {
	kind := Kind(strings.TrimSpace(raw))
	if !kind.Valid() {
		return "", fmt.Errorf("%w: kind must be one of discord, slack, telegram, email", ErrValidation)
	}
	return kind, nil
}

// EventKey is one subscribable event. The values are the strings stored in
// notification_channels.events and are what a channel selects.
type EventKey string

// Event keys.
const (
	// EventDeploySuccess — a deployment reached running.
	EventDeploySuccess EventKey = "deploy_success"
	// EventDeployFailure — a deployment reached failed.
	EventDeployFailure EventKey = "deploy_failure"
	// EventBackupSuccess — a backup run completed.
	EventBackupSuccess EventKey = "backup_success"
	// EventBackupFailure — a backup run failed.
	EventBackupFailure EventKey = "backup_failure"
)

// AllEvents lists every event in subscription order. A new channel subscribes
// to all of them unless the request names a subset.
var AllEvents = []EventKey{EventDeploySuccess, EventDeployFailure, EventBackupSuccess, EventBackupFailure}

// Valid reports whether k is a known event key.
func (k EventKey) Valid() bool {
	for _, known := range AllEvents {
		if k == known {
			return true
		}
	}
	return false
}

// EventKind discriminates the event source.
type EventKind string

// Event sources.
const (
	// EventDeploy — a deployment lifecycle event.
	EventDeploy EventKind = "deploy"
	// EventBackup — a backup lifecycle event.
	EventBackup EventKind = "backup"
)

// Outcome is the terminal result of an event source.
type Outcome string

// Outcomes.
const (
	// OutcomeSuccess — the run finished successfully.
	OutcomeSuccess Outcome = "success"
	// OutcomeFailure — the run failed.
	OutcomeFailure Outcome = "failure"
)

// Event is one terminal deploy or backup result to deliver. It is the whole
// payload a notifier formats; no secret ever travels on it.
type Event struct {
	// Kind is the event source (deploy or backup).
	Kind EventKind
	// Outcome is success or failure.
	Outcome Outcome
	// TeamID selects the team whose channels receive the event.
	TeamID uuid.UUID
	// ResourceType and ResourceID identify the application or database the
	// event belongs to; channels scoped to it are selected too.
	ResourceType string
	ResourceID   uuid.UUID
	// Name is the application or database name.
	Name string
	// Host is the optional application host (domain) for deploy events.
	Host string
	// Error is the failure text; empty on success.
	Error string
	// At is when the run reached its terminal state.
	At time.Time
}

// Key maps the event to its subscription key.
func (e Event) Key() EventKey {
	switch {
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

// Summary is the one-line headline of the event, used as the email subject
// and the first line of every message body.
func (e Event) Summary() string {
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

// maxEventError bounds the failure text embedded in a message so a huge
// engine dump can never push a payload past a channel's size limit.
const maxEventError = 500

// Message renders the event as plain text, the format Discord, Slack,
// Telegram and email all carry. Only data the event actually holds is
// rendered — no invented fields.
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

// truncateText trims text and caps it, keeping the head.
func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// ChannelConfig is the transport configuration of one channel. A single struct keeps
// the stored JSON and the redaction rules trivial; which fields are required
// depends on the channel kind and is enforced by validate.
type ChannelConfig struct {
	// WebhookURL is the Discord/Slack incoming webhook (a secret).
	WebhookURL string `json:"webhook_url,omitempty"`
	// BotToken is the Telegram bot token (a secret).
	BotToken string `json:"bot_token,omitempty"`
	// ChatID is the Telegram chat the bot posts to.
	ChatID string `json:"chat_id,omitempty"`
	// Host, Port, Username and Password are the SMTP relay settings; Password
	// is a secret and Username may be empty for an open relay.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// From is the envelope and header sender; To are the recipients.
	From string   `json:"from,omitempty"`
	To   []string `json:"to,omitempty"`
}

// validate checks the config against the channel kind.
func (c ChannelConfig) validate(kind Kind) error {
	switch kind {
	case KindDiscord, KindSlack:
		if err := validateWebhookURL(c.WebhookURL); err != nil {
			return err
		}
	case KindTelegram:
		if strings.TrimSpace(c.BotToken) == "" {
			return fmt.Errorf("%w: config.bot_token is required for telegram", ErrValidation)
		}
		if strings.TrimSpace(c.ChatID) == "" {
			return fmt.Errorf("%w: config.chat_id is required for telegram", ErrValidation)
		}
	case KindEmail:
		if strings.TrimSpace(c.Host) == "" {
			return fmt.Errorf("%w: config.host is required for email", ErrValidation)
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

// validateWebhookURL accepts an absolute http(s) URL. It deliberately does not
// restrict private addresses: channels are configured by team owners, exactly
// like backup targets' S3 endpoints.
func validateWebhookURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%w: config.webhook_url is required", ErrValidation)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: config.webhook_url must be an http(s) URL", ErrValidation)
	}
	return nil
}

// isZero reports whether an update carries no config change at all.
func (c ChannelConfig) isZero() bool {
	return c.WebhookURL == "" && c.BotToken == "" && c.ChatID == "" && c.Host == "" &&
		c.Port == 0 && c.Username == "" && c.Password == "" && c.From == "" && c.To == nil
}

// merged applies the non-empty fields of an update over the stored config, so
// a UI that resends a masked secret cannot wipe it and a partial update only
// touches what it names.
func (c ChannelConfig) merged(update ChannelConfig) ChannelConfig {
	merged := c
	if update.WebhookURL != "" {
		merged.WebhookURL = update.WebhookURL
	}
	if update.BotToken != "" {
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
	if update.Password != "" {
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

// hasSecrets reports whether the config carries any secret material; the read
// DTO exposes this as a boolean so a client can render "configured" without
// receiving the value.
func (c ChannelConfig) hasSecrets() bool {
	return c.WebhookURL != "" || c.BotToken != "" || c.Password != ""
}

// redacted returns the config with every secret masked: at most the last four
// characters survive, matching what the settings UI displays. Non-secret
// fields (chat id, SMTP host/port/user/from/to) pass through.
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

// maskSecret keeps at most the last four characters of a secret.
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "…"
	}
	return "…" + secret[len(secret)-4:]
}

// sealConfig encrypts the whole transport config with the deployment key.
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

// openConfig reverses sealConfig.
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

// Channel is one stored notification channel. SealedConfig is the ciphertext
// of the transport config; it is opened only to build a notifier or a redacted
// read view.
type Channel struct {
	ID      uuid.UUID
	TeamID  uuid.UUID
	Name    string
	Kind    Kind
	Enabled bool
	Events  []EventKey
	// ResourceType is empty for a team-wide channel.
	ResourceType string
	ResourceID   uuid.UUID
	SealedConfig string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Wants reports whether the channel subscribes to the event.
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

// ChannelView is the read representation of a channel: the wire DTO the API
// serves. Secrets are masked and SecretsConfigured tells a client whether a
// secret is stored without ever returning it.
type ChannelView struct {
	ID                uuid.UUID     `json:"id"`
	TeamID            uuid.UUID     `json:"team_id"`
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

// ChannelRequest is the body of the channel create and update endpoints. On
// update, zero values leave the stored field unchanged; Enabled is a pointer
// because false is meaningful, ResourceType/ResourceID are pointers so an
// empty string can clear a resource override (both must be named together),
// and a non-nil Events slice replaces the subscription (an empty one is
// rejected).
type ChannelRequest struct {
	Name         string        `json:"name,omitempty"`
	Kind         Kind          `json:"kind,omitempty"`
	Enabled      *bool         `json:"enabled,omitempty"`
	ResourceType *string       `json:"resource_type,omitempty"`
	ResourceID   *string       `json:"resource_id,omitempty"`
	Events       []EventKey    `json:"events,omitempty"`
	Config       ChannelConfig `json:"config,omitempty"`
}

// TestResult is the answer of the send-test action. It never carries the
// channel's secrets.
type TestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// parseResource validates a resource override pair: both empty means
// team-wide, otherwise the type must be known and the id a UUID.
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

// normalizeEvents validates an explicit subscription, defaulting to every
// event when the request names none. Duplicates are dropped.
func normalizeEvents(events []EventKey) ([]EventKey, error) {
	if events == nil {
		return append([]EventKey(nil), AllEvents...), nil
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
