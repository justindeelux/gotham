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
// the connection deadline. Every step failure is classified: a relay's own
// response text (which is arbitrary remote content) never reaches the error.
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

	// The SMTP envelope needs bare addresses: a validated "Ops <ops@x>" must
	// become MAIL FROM:<ops@x>, not the display string (which a relay rejects
	// with 501). Display names stay in the rendered headers.
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

	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
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

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return smtpFailure("starttls", err)
		}
	}
	if username := strings.TrimSpace(mail.Username); username != "" {
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

// envelopeAddress reduces an RFC 5322 mailbox ("Ops <ops@example.com>") to the
// bare address the SMTP envelope requires.
func envelopeAddress(raw string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || parsed.Address == "" {
		return "", fmt.Errorf("%w: config.from and config.to must be valid email addresses", ErrValidation)
	}
	return parsed.Address, nil
}

// smtpFailure renders one SMTP step failure without forwarding remote text: a
// *textproto.Error contributes only its numeric status. A hostile or
// misconfigured relay must never be able to inject credentials or arbitrary
// content into a delivery error.
func smtpFailure(step string, err error) error {
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		return fmt.Errorf("notifications: smtp %s failed (status %d)", step, protoErr.Code)
	}
	return fmt.Errorf("notifications: smtp %s failed: %s", step, transportCategory(err))
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
