package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/internal/netguard"
)

const defaultSMTPPort = 587

const defaultSMTPTimeout = 10 * time.Second

type Mail struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	Subject  string
	Body     string
	// AllowPrivate lets an owner channel reach a LAN relay; member channels
	// only reach public addresses.
	AllowPrivate bool
}

type Mailer interface {
	Send(ctx context.Context, mail Mail) error
}

type smtpMailer struct {
	timeout time.Duration
}

func (m *smtpMailer) Send(ctx context.Context, mail Mail) error {
	host := strings.TrimSpace(mail.Host)
	if host == "" {
		return fmt.Errorf("%w: smtp host is not configured", ErrValidation)
	}
	port := mail.Port
	if port == 0 {
		port = defaultSMTPPort
	}
	timeout := m.timeout
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))

	from, err := envelopeAddress(mail.From)
	if err != nil {
		return err
	}
	recipients := make([]string, 0, len(mail.To))
	for _, raw := range mail.To {
		recipient, err := envelopeAddress(raw)
		if err != nil {
			return err
		}
		recipients = append(recipients, recipient)
	}

	dialer := &net.Dialer{Timeout: timeout, Control: netguard.Control(mail.AllowPrivate)}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if errors.Is(err, netguard.ErrDenied) {
			return fmt.Errorf("%w: smtp destination is not allowed; edit and save the channel as the owner to allow it", ErrValidation)
		}
		return fmt.Errorf("notifications: dial smtp %s: %s", address, transportCategory(err))
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	defer func() { _ = conn.Close() }()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return smtpFailure("handshake", err)
	}
	defer func() { _ = client.Close() }()

	startTLS := false
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return smtpFailure("starttls", err)
		}
		startTLS = true
	}
	if username := strings.TrimSpace(mail.Username); username != "" {
		if err := credentialsRequireTLS(host, startTLS); err != nil {
			return err
		}
		if err := client.Auth(smtp.PlainAuth("", username, mail.Password, host)); err != nil {
			return smtpFailure("auth", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return smtpFailure("sender", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return smtpFailure("recipient", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return smtpFailure("data", err)
	}
	if _, err := writer.Write(renderMessage(mail)); err != nil {
		_ = writer.Close()
		return smtpFailure("write", err)
	}
	if err := writer.Close(); err != nil {
		return smtpFailure("write", err)
	}
	if err := client.Quit(); err != nil {
		return smtpFailure("quit", err)
	}
	return nil
}

func envelopeAddress(raw string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || parsed.Address == "" {
		return "", fmt.Errorf("%w: config.from and config.to must be valid email addresses", ErrValidation)
	}
	return parsed.Address, nil
}

func credentialsRequireTLS(host string, startTLS bool) error {
	if startTLS || isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf(
		"notifications: smtp relay %s does not offer STARTTLS; refusing to send credentials in plaintext",
		host)
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func smtpFailure(step string, err error) error {
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		return fmt.Errorf("notifications: smtp %s failed (status %d)", step, protoErr.Code)
	}
	return fmt.Errorf("notifications: smtp %s failed: %s", step, transportCategory(err))
}

func renderMessage(mail Mail) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", sanitizeHeader(mail.From))
	fmt.Fprintf(&b, "To: %s\r\n", sanitizeHeader(strings.Join(mail.To, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", sanitizeHeader(mail.Subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("\r\n")
	body := strings.ReplaceAll(mail.Body, "\n", "\r\n")
	b.WriteString(body)
	return b.Bytes()
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

func defaultMailer(timeout time.Duration) Mailer {
	return &smtpMailer{timeout: timeout}
}
