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
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/internal/netguard"
)

type Notifier interface {
	Notify(ctx context.Context, event Event) error
}

const defaultTelegramBaseURL = "https://api.telegram.org"

const (
	discordContentLimit = 2000
	slackTextLimit      = 4000
	telegramTextLimit   = 4096
)

type discordNotifier struct {
	url    string
	client *http.Client
}

type discordPayload struct {
	Content         string          `json:"content"`
	AllowedMentions discordMentions `json:"allowed_mentions"`
}

type discordMentions struct {
	Parse []string `json:"parse"`
}

func newDiscordNotifier(webhookURL string, client *http.Client) *discordNotifier {
	return &discordNotifier{url: webhookURL, client: client}
}

func (n *discordNotifier) Notify(ctx context.Context, event Event) error {
	payload := discordPayload{
		Content:         truncateText(event.Message(), discordContentLimit),
		AllowedMentions: discordMentions{Parse: []string{}},
	}
	return postJSON(ctx, n.client, n.url, payload)
}

type slackNotifier struct {
	url    string
	client *http.Client
}

func newSlackNotifier(webhookURL string, client *http.Client) *slackNotifier {
	return &slackNotifier{url: webhookURL, client: client}
}

func (n *slackNotifier) Notify(ctx context.Context, event Event) error {
	return postJSON(ctx, n.client, n.url, slackPayload{
		Text:        truncateText(slackEscape(event.Message()), slackTextLimit),
		LinkNames:   false,
		UnfurlLinks: false,
		UnfurlMedia: false,
	})
}

type slackPayload struct {
	Text        string `json:"text"`
	LinkNames   bool   `json:"link_names"`
	UnfurlLinks bool   `json:"unfurl_links"`
	UnfurlMedia bool   `json:"unfurl_media"`
}

func slackEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

type telegramNotifier struct {
	token   string
	chatID  string
	baseURL string
	client  *http.Client
}

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

func (n *telegramNotifier) Notify(ctx context.Context, event Event) error {
	endpoint := n.baseURL + "/bot" + url.PathEscape(n.token) + "/sendMessage"
	return postJSON(ctx, n.client, endpoint, map[string]string{
		"chat_id": n.chatID,
		"text":    truncateText(event.Message(), telegramTextLimit),
	})
}

type emailNotifier struct {
	mailer Mailer
	config ChannelConfig
}

func newEmailNotifier(mailer Mailer, config ChannelConfig) *emailNotifier {
	return &emailNotifier{mailer: mailer, config: config}
}

func (n *emailNotifier) Notify(ctx context.Context, event Event) error {
	if n.mailer == nil {
		return fmt.Errorf("%w: mailer is not configured", ErrValidation)
	}
	return n.mailer.Send(ctx, Mail{
		Host:         n.config.Host,
		Port:         n.config.Port,
		Username:     n.config.Username,
		Password:     n.config.Password,
		From:         n.config.From,
		To:           append([]string(nil), n.config.To...),
		Subject:      defaultSubjectPrefix + event.Summary(),
		Body:         event.Message() + "\n",
		AllowPrivate: n.config.AllowPrivate,
	})
}

const defaultSubjectPrefix = "[Gotham] "

// sendFailure carries the retry classification of a provider attempt without
// exposing credentials or raw remote content. The message is the existing
// bounded, user-visible summary.
type sendFailure struct {
	message    string
	permanent  bool
	retryAfter time.Duration
}

func (e *sendFailure) Error() string { return e.message }

// classifyDeliveryError separates permanent configuration/auth/rejection
// failures from transient connection, timeout, 429 and 5xx failures.
func classifyDeliveryError(err error, kind Kind) (permanent bool, retryAfter time.Duration) {
	var failure *sendFailure
	if errors.As(err, &failure) {
		return failure.permanent, failure.retryAfter
	}
	if errors.Is(err, ErrValidation) {
		return true, 0
	}
	if kind == KindEmail {
		text := err.Error()
		for _, marker := range []string{"smtp auth failed (status 5", "smtp sender failed (status 5", "smtp recipient failed (status 5"} {
			if strings.Contains(text, marker) {
				return true, 0
			}
		}
	}
	return false, 0
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any) error {
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return &sendFailure{message: "notifications: encode payload failed", permanent: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return &sendFailure{message: fmt.Sprintf("notifications: build request for %s: invalid endpoint", endpointHost(endpoint)), permanent: true}
	}
	req.Header.Set("Content-Type", "application/json")
	guarded := *client
	guarded.CheckRedirect = checkRedirect
	resp, err := guarded.Do(req)
	if err != nil {
		return &sendFailure{
			message: fmt.Sprintf("notifications: post to %s: %s", endpointHost(endpoint), transportCategory(err)),
			permanent: errors.Is(err, errRedirectRefused) ||
				errors.Is(err, netguard.ErrDenied),
		}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == http.StatusTooManyRequests {
			if seconds, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
				return &sendFailure{
					message:    fmt.Sprintf("notifications: %s rate limited (retry after %ds)", endpointHost(endpoint), seconds),
					retryAfter: time.Duration(seconds) * time.Second,
				}
			}
			return &sendFailure{message: fmt.Sprintf("notifications: %s rate limited", endpointHost(endpoint))}
		}
		return &sendFailure{
			message:   fmt.Sprintf("notifications: %s answered status %d", endpointHost(endpoint), resp.StatusCode),
			permanent: resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout,
		}
	}
	return nil
}

const maxRetryAfterSeconds = 86400

func parseRetryAfter(raw string) (int, bool) {
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds <= 0 || seconds > maxRetryAfterSeconds {
		return 0, false
	}
	return seconds, true
}

const redirectLimit = 10

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
	case errors.Is(err, netguard.ErrDenied):
		return "destination is not allowed"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "connection failed"
	}
}

// outboundClient guards an HTTP client with the member/owner address policy at
// dial time. A caller-supplied transport that is not an *http.Transport is
// returned as-is.
func outboundClient(base *http.Client, allowPrivate bool) *http.Client {
	if base == nil {
		base = &http.Client{}
	}
	transport, ok := base.Transport.(*http.Transport)
	if base.Transport == nil {
		transport, ok = http.DefaultTransport.(*http.Transport)
	}
	if !ok {
		return base
	}
	cloned := transport.Clone()
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   netguard.Control(allowPrivate),
	}
	cloned.DialContext = dialer.DialContext
	client := *base
	client.Transport = cloned
	return &client
}

func endpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "endpoint"
	}
	return parsed.Scheme + "://" + parsed.Host
}
