package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	endpoint := n.baseURL + "/bot" + url.PathEscape(n.token) + "/sendMessage"
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
//
// Errors are classified, never forwarded: a webhook URL and a Telegram bot
// token live inside the request URL, and a remote body could echo them, so
// neither the transport error text nor the response body may reach a log line
// or an API response. Only the endpoint host plus a category or HTTP status
// survives.
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
		// The underlying parse error echoes the endpoint, which may carry a
		// credential.
		return fmt.Errorf("notifications: build request for %s: invalid endpoint", endpointHost(endpoint))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notifications: post to %s: %s", endpointHost(endpoint), transportCategory(err))
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain a bounded prefix so the connection can be reused; the body is
	// never echoed because a remote server may reflect the request path.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("notifications: %s answered %s", endpointHost(endpoint), resp.Status)
	}
	return nil
}

// transportCategory classifies a transport failure without forwarding its
// text: a *url.Error stringifies the full request URL (and so the channel's
// credential), and remote text must never travel either.
func transportCategory(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timed out"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "connection failed"
	}
}

// endpointHost renders "scheme://host" of an endpoint for error messages.
func endpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "endpoint"
	}
	return parsed.Scheme + "://" + parsed.Host
}
