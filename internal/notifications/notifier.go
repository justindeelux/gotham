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
// token live inside the request URL, and remote content could echo them (the
// response body, the HTTP reason phrase, the transport error text), so none
// of it may reach a log line or an API response. Only the endpoint host plus
// a category or the numeric HTTP status survives.
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
	// A copy with the redirect guard keeps the caller's transport and timeout
	// while refusing to follow the credential-bearing request to another
	// origin or a link-local/metadata address.
	guarded := *client
	guarded.CheckRedirect = checkRedirect
	resp, err := guarded.Do(req)
	if err != nil {
		return fmt.Errorf("notifications: post to %s: %s", endpointHost(endpoint), transportCategory(err))
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain a bounded prefix so the connection can be reused; the body is
	// never echoed because a remote server may reflect the request path.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// resp.Status carries the reason phrase, which the remote endpoint
		// controls; only the numeric code is safe to report.
		return fmt.Errorf("notifications: %s answered status %d", endpointHost(endpoint), resp.StatusCode)
	}
	return nil
}

// redirectLimit bounds followed redirects; the stdlib default is 10.
const redirectLimit = 10

// checkRedirect refuses a redirect that leaves the webhook's origin or that
// lands on a link-local/metadata address. Without it the default client would
// follow a 302 from a compromised endpoint to wherever it likes, carrying the
// credential in the URL path. Same-origin redirects (for example a trailing
// slash) keep working.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= redirectLimit {
		return fmt.Errorf("%w: too many redirects", errRedirectRefused)
	}
	if len(via) > 0 {
		origin := via[0].URL
		if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
			return fmt.Errorf("%w: cross-origin redirect", errRedirectRefused)
		}
	}
	if err := validateOutboundHost(req.URL.Hostname()); err != nil {
		return fmt.Errorf("%w: %v", errRedirectRefused, err)
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
	if errors.Is(err, errRedirectRefused) {
		return "redirect refused"
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
