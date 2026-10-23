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

const defaultSubjectPrefix = "[Gotham] "

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
		return fmt.Errorf("notifications: build request for %s: invalid endpoint", endpointHost(endpoint))
	}
	req.Header.Set("Content-Type", "application/json")
	guarded := *client
	guarded.CheckRedirect = checkRedirect
	resp, err := guarded.Do(req)
	if err != nil {
		return fmt.Errorf("notifications: post to %s: %s", endpointHost(endpoint), transportCategory(err))
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == http.StatusTooManyRequests {
			if seconds, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
				return fmt.Errorf("notifications: %s rate limited (retry after %ds)", endpointHost(endpoint), seconds)
			}
			return fmt.Errorf("notifications: %s rate limited", endpointHost(endpoint))
		}
		return fmt.Errorf("notifications: %s answered status %d", endpointHost(endpoint), resp.StatusCode)
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
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "connection failed"
	}
}

func endpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "endpoint"
	}
	return parsed.Scheme + "://" + parsed.Host
}
