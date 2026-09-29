package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Notifier delivers one event over one channel's transport. Implementations
// are per-channel and stateless; a delivery failure is returned so the
// dispatcher can log it without failing the other channels.
type Notifier interface {
	Notify(ctx context.Context, event Event) error
}

// defaultTelegramBaseURL is the Telegram Bot API root; tests point it at an
// httptest server through newTelegramNotifier.
const defaultTelegramBaseURL = "https://api.telegram.org"

// discordNotifier posts {"content": ...} to a Discord incoming webhook.
type discordNotifier struct {
	url    string
	client *http.Client
}

// newDiscordNotifier builds a Discord notifier for a webhook URL.
func newDiscordNotifier(webhookURL string, client *http.Client) *discordNotifier {
	return &discordNotifier{url: webhookURL, client: client}
}

// Notify implements Notifier.
func (n *discordNotifier) Notify(ctx context.Context, event Event) error {
	return postJSON(ctx, n.client, n.url, map[string]string{"content": event.Message()})
}

// slackNotifier posts {"text": ...} to a Slack incoming webhook.
type slackNotifier struct {
	url    string
	client *http.Client
}

// newSlackNotifier builds a Slack notifier for an incoming webhook URL.
func newSlackNotifier(webhookURL string, client *http.Client) *slackNotifier {
	return &slackNotifier{url: webhookURL, client: client}
}

// Notify implements Notifier.
func (n *slackNotifier) Notify(ctx context.Context, event Event) error {
	return postJSON(ctx, n.client, n.url, map[string]string{"text": event.Message()})
}

// telegramNotifier calls the Bot API sendMessage method.
type telegramNotifier struct {
	token   string
	chatID  string
	baseURL string
	client  *http.Client
}

// newTelegramNotifier builds a Telegram notifier. baseURL defaults to the Bot
// API root; tests pass an httptest server.
func newTelegramNotifier(token, chatID, baseURL string, client *http.Client) *telegramNotifier {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultTelegramBaseURL
	}
	return &telegramNotifier{
		token:   token,
		chatID:  chatID,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

// Notify implements Notifier. The token travels in the URL path (the Bot API
// contract), so a failed call never reports the endpoint, only the host.
func (n *telegramNotifier) Notify(ctx context.Context, event Event) error {
	endpoint := n.baseURL + "/bot" + n.token + "/sendMessage"
	return postJSON(ctx, n.client, endpoint, map[string]string{
		"chat_id": n.chatID,
		"text":    event.Message(),
	})
}

// emailNotifier sends the event through the configured SMTP Mailer.
type emailNotifier struct {
	mailer Mailer
	config ChannelConfig
}

// newEmailNotifier builds an email notifier over mailer.
func newEmailNotifier(mailer Mailer, config ChannelConfig) *emailNotifier {
	return &emailNotifier{mailer: mailer, config: config}
}

// Notify implements Notifier.
func (n *emailNotifier) Notify(ctx context.Context, event Event) error {
	if n.mailer == nil {
		return fmt.Errorf("%w: mailer is not configured", ErrValidation)
	}
	return n.mailer.Send(ctx, Mail{
		Host:     n.config.Host,
		Port:     n.config.Port,
		Username: n.config.Username,
		Password: n.config.Password,
		From:     n.config.From,
		To:       append([]string(nil), n.config.To...),
		Subject:  defaultSubjectPrefix + event.Summary(),
		Body:     event.Message() + "\n",
	})
}

// defaultSubjectPrefix marks notification mail as coming from Gotham.
const defaultSubjectPrefix = "[Gotham] "

// postJSON sends one JSON document and treats only a 2xx answer as success.
// The response body is drained (bounded) so the connection can be reused.
// Error messages never contain the request URL: a webhook URL and a Telegram
// bot token live inside it.
func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any) error {
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notifications: encode payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notifications: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notifications: post to %s: %w", endpointHost(endpoint), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("notifications: %s answered %s: %s",
			endpointHost(endpoint), resp.Status, strings.TrimSpace(string(detail)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

// endpointHost renders "scheme://host" of an endpoint for error messages.
func endpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "endpoint"
	}
	return parsed.Scheme + "://" + parsed.Host
}
