package notifications

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSMTP is a minimal SMTP server that records the envelope of one
// conversation and captures the DATA payload. It speaks just enough of the
// protocol for net/smtp: greeting, EHLO, MAIL, RCPT, DATA, QUIT — no STARTTLS
// and no AUTH, so the mailer's TLS/auth branches stay out of this test.
type mockSMTP struct {
	listener net.Listener

	// rcptReject, when set, makes RCPT TO answer 501 with that text instead
	// of accepting the recipient.
	rcptReject string
	// advertiseAuth makes EHLO offer AUTH PLAIN, which lets a test exercise
	// the credential path (loopback plaintext is allowed by design).
	advertiseAuth bool

	mu           sync.Mutex
	from         string
	rcpt         []string
	data         string
	authAttempts int
	authPayload  string

	done chan struct{}
}

// startMockSMTP listens on a loopback port and serves one conversation.
func startMockSMTP(t *testing.T) *mockSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &mockSMTP{listener: listener, done: make(chan struct{})}
	go server.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		<-server.done
	})
	return server
}

// serve runs the conversation until QUIT or EOF.
func (s *mockSMTP) serve() {
	defer close(s.done)
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(line string) {
		_, _ = writer.WriteString(line + "\r\n")
		_ = writer.Flush()
	}
	reply("220 mock ESMTP")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			if s.advertiseAuth {
				_, _ = writer.WriteString("250-mock\r\n250 AUTH PLAIN\r\n")
				_ = writer.Flush()
				continue
			}
			reply("250 mock")
		case strings.HasPrefix(command, "AUTH PLAIN"):
			s.mu.Lock()
			s.authAttempts++
			s.authPayload = strings.TrimSpace(strings.TrimPrefix(command, "AUTH PLAIN"))
			s.mu.Unlock()
			reply("235 2.7.0 Authentication successful")
		case strings.HasPrefix(command, "MAIL FROM:"):
			s.mu.Lock()
			s.from = strings.Trim(command[len("MAIL FROM:"):], "<>")
			s.mu.Unlock()
			reply("250 OK")
		case strings.HasPrefix(command, "RCPT TO:"):
			if s.rcptReject != "" {
				reply("501 " + s.rcptReject)
				continue
			}
			s.mu.Lock()
			s.rcpt = append(s.rcpt, strings.Trim(command[len("RCPT TO:"):], "<>"))
			s.mu.Unlock()
			reply("250 OK")
		case command == "DATA":
			reply("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" || line == ".\n" {
					break
				}
				body.WriteString(line)
			}
			s.mu.Lock()
			s.data = body.String()
			s.mu.Unlock()
			reply("250 OK")
		case command == "QUIT":
			reply("221 Bye")
			return
		default:
			reply("250 OK")
		}
	}
}

// envelope returns the recorded conversation.
func (s *mockSMTP) envelope() (from string, rcpt []string, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, append([]string(nil), s.rcpt...), s.data
}

// auth returns how many AUTH attempts arrived and the payload of the last.
func (s *mockSMTP) auth() (attempts int, payload string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authAttempts, s.authPayload
}

// TestSMTPMailerSendsBareEnvelopeAddresses runs the real net/smtp path against
// the local mock: display-name addresses must reach MAIL FROM/RCPT TO as bare
// addresses (a relay answers a raw display string with 501), while the
// rendered headers keep the display form.
func TestSMTPMailerSendsBareEnvelopeAddresses(t *testing.T) {
	tests := []struct {
		name       string
		from       string
		to         string
		wantFrom   string
		wantTo     string
		wantHeader string
	}{
		{
			name:       "display names",
			from:       "Ops <ops@example.com>",
			to:         "\"On Call\" <oncall@example.com>",
			wantFrom:   "ops@example.com",
			wantTo:     "oncall@example.com",
			wantHeader: "From: Ops <ops@example.com>",
		},
		{
			name:       "plain addresses",
			from:       "ops@example.com",
			to:         "oncall@example.com",
			wantFrom:   "ops@example.com",
			wantTo:     "oncall@example.com",
			wantHeader: "From: ops@example.com",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := startMockSMTP(t)
			host, portText, err := net.SplitHostPort(server.listener.Addr().String())
			if err != nil {
				t.Fatalf("split addr: %v", err)
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				t.Fatalf("port: %v", err)
			}

			mailer := &smtpMailer{timeout: 5 * time.Second}
			err = mailer.Send(context.Background(), Mail{
				Host:    host,
				Port:    port,
				From:    tc.from,
				To:      []string{tc.to},
				Subject: "[Gotham] envelope test",
				Body:    "hello\n",
			})
			if err != nil {
				t.Fatalf("Send: %v", err)
			}

			from, rcpt, data := server.envelope()
			if from != tc.wantFrom {
				t.Errorf("MAIL FROM = %q, want %q", from, tc.wantFrom)
			}
			if len(rcpt) != 1 || rcpt[0] != tc.wantTo {
				t.Errorf("RCPT TO = %v, want [%s]", rcpt, tc.wantTo)
			}
			if !strings.Contains(data, tc.wantHeader+"\r\n") {
				t.Errorf("message = %q, want the display header %q", data, tc.wantHeader)
			}
			if !strings.Contains(data, "Subject: [Gotham] envelope test") {
				t.Errorf("message = %q, want the subject", data)
			}
		})
	}
}

// TestSMTPMailerHidesRelayText proves a relay's rejection text (arbitrary
// remote content) never reaches the delivery error: only the step and numeric
// status survive.
func TestSMTPMailerHidesRelayText(t *testing.T) {
	server := startMockSMTP(t)
	server.rcptReject = "5.1.1 rejected: secret-relay-token"
	host, portText, _ := net.SplitHostPort(server.listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	mailer := &smtpMailer{timeout: 5 * time.Second}
	err := mailer.Send(context.Background(), Mail{
		Host:    host,
		Port:    port,
		From:    "ops@example.com",
		To:      []string{"oncall@example.com"},
		Subject: "subject",
		Body:    "body",
	})
	if err == nil {
		t.Fatal("Send = nil error, want the relay rejection")
	}
	if strings.Contains(err.Error(), "secret-relay-token") {
		t.Errorf("error %q leaks the relay response text", err)
	}
	if !strings.Contains(err.Error(), "status 501") {
		t.Errorf("error = %q, want the numeric status", err)
	}
}

// TestSMTPMailerRejectsInvalidEnvelopeAddress proves an address that slipped
// past config validation is refused locally instead of producing a malformed
// SMTP command.
func TestSMTPMailerRejectsInvalidEnvelopeAddress(t *testing.T) {
	server := startMockSMTP(t)
	host, portText, _ := net.SplitHostPort(server.listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	mailer := &smtpMailer{timeout: 5 * time.Second}
	err := mailer.Send(context.Background(), Mail{
		Host:    host,
		Port:    port,
		From:    "not an address",
		To:      []string{"oncall@example.com"},
		Subject: "subject",
		Body:    "body",
	})
	if err == nil {
		t.Fatal("Send = nil error, want a validation failure")
	}
	if !strings.Contains(err.Error(), "valid email address") {
		t.Errorf("error = %q, want the validation message", err)
	}
}

// TestCredentialsRequireTLS pins the STARTTLS policy: a remote relay that did
// not negotiate TLS is refused when credentials are configured, a loopback
// relay is exempt.
func TestCredentialsRequireTLS(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		startTLS bool
		wantErr  bool
	}{
		{name: "remote without starttls", host: "smtp.example.com", wantErr: true},
		{name: "remote with starttls", host: "smtp.example.com", startTLS: true},
		{name: "loopback ipv4", host: "127.0.0.1"},
		{name: "loopback ipv6", host: "::1"},
		{name: "loopback scoped", host: "[::1%lo0]"},
		{name: "localhost name", host: "localhost"},
		{name: "private relay without starttls", host: "10.0.0.5", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := credentialsRequireTLS(tc.host, tc.startTLS)
			if tc.wantErr && err == nil {
				t.Fatalf("credentialsRequireTLS(%q, %v) = nil, want an error", tc.host, tc.startTLS)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("credentialsRequireTLS(%q, %v) = %v, want nil", tc.host, tc.startTLS, err)
			}
			if err != nil && !strings.Contains(err.Error(), "STARTTLS") {
				t.Errorf("error = %q, want the STARTTLS refusal", err)
			}
		})
	}
}

// TestSMTPMailerAuthenticatesOnLoopback proves the loopback exemption works
// end to end: a local relay without STARTTLS still accepts PLAIN auth.
func TestSMTPMailerAuthenticatesOnLoopback(t *testing.T) {
	server := startMockSMTP(t)
	server.advertiseAuth = true
	host, portText, _ := net.SplitHostPort(server.listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	mailer := &smtpMailer{timeout: 5 * time.Second}
	err := mailer.Send(context.Background(), Mail{
		Host:     host,
		Port:     port,
		Username: "ops@gotham.dev",
		Password: "smtp-secret",
		From:     "ops@gotham.dev",
		To:       []string{"oncall@gotham.dev"},
		Subject:  "subject",
		Body:     "body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	attempts, payload := server.auth()
	if attempts != 1 {
		t.Errorf("AUTH attempts = %d, want 1", attempts)
	}
	if payload == "" {
		t.Error("no AUTH payload was recorded")
	}
	from, _, _ := server.envelope()
	if from != "ops@gotham.dev" {
		t.Errorf("MAIL FROM = %q, want the envelope sender", from)
	}
}
