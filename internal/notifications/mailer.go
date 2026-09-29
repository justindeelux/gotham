package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// defaultSMTPPort is the submission port used when the channel names none.
const defaultSMTPPort = 587

// defaultSMTPTimeout bounds one delivery when the caller's context carries no
// deadline of its own.
const defaultSMTPTimeout = 10 * time.Second

// Mail is one outgoing notification email together with the transport
// settings of its channel. Passing the whole document to the Mailer keeps the
// SMTP implementation stateless and lets tests assert the effective delivery
// settings with a fake.
type Mail struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	Subject  string
	Body     string
}

// Mailer delivers one notification email. net/smtp implements it in
// production (smtpMailer); tests inject a fake.
type Mailer interface {
	Send(ctx context.Context, mail Mail) error
}

// smtpMailer sends through a plain SMTP relay: optional STARTTLS when the
// server offers it, optional PLAIN auth when a username is configured. It is
// the only implementation that touches the network.
type smtpMailer struct {
	timeout time.Duration
}

// Send implements Mailer. The context bounds the whole conversation through
// the connection deadline.
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

	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("notifications: dial smtp %s: %w", net.JoinHostPort(host, strconv.Itoa(port)), err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	defer func() { _ = conn.Close() }()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("notifications: smtp handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("notifications: smtp starttls: %w", err)
		}
	}
	if username := strings.TrimSpace(mail.Username); username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, mail.Password, host)); err != nil {
			return fmt.Errorf("notifications: smtp auth: %w", err)
		}
	}
	if err := client.Mail(mail.From); err != nil {
		return fmt.Errorf("notifications: smtp sender: %w", err)
	}
	for _, recipient := range mail.To {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("notifications: smtp recipient: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("notifications: smtp data: %w", err)
	}
	if _, err := writer.Write(renderMessage(mail)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("notifications: smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("notifications: smtp write: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("notifications: smtp quit: %w", err)
	}
	return nil
}

// renderMessage builds the RFC 5322 document (headers only; no MIME parts are
// needed for a plain-text notification).
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

// sanitizeHeader strips CR/LF so a crafted name or subject can never inject a
// header.
func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

// defaultMailer returns the SMTP implementation; a nil Mailer in Config means
// production behavior.
func defaultMailer(timeout time.Duration) Mailer {
	return &smtpMailer{timeout: timeout}
}
