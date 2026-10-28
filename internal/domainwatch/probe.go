package domainwatch

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrInvalidPath        = errors.New("domainwatch: path must be a relative path on the managed domain")
	ErrInvalidStatusRange = errors.New("domainwatch: expected status must be between 100 and 599 and ordered")
	ErrInvalidTimeout     = errors.New("domainwatch: timeout must be between 1 and 30 seconds")
	ErrUnsafeRedirect     = errors.New("domainwatch: redirect destination is not allowed")
	ErrBlockedDestination = errors.New("domainwatch: destination resolves to a blocked address")
)

var (
	errPath          = ErrInvalidPath
	errExpectedRange = ErrInvalidStatusRange
	errTimeout       = ErrInvalidTimeout
)

// Prober performs one GET request through the existing outbound path. It
// validates TLS normally, follows at most three same-host redirects, reads a
// bounded body and refuses loopback, private and link-local destinations.
type Prober struct {
	Now      func() time.Time
	Resolver *net.Resolver
	// Dial overrides the safe dialer for tests only.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func NewProber() *Prober {
	return &Prober{Now: time.Now, Resolver: net.DefaultResolver}
}

func (p *Prober) Probe(ctx context.Context, target Target) Result {
	at := p.now()
	rawURL, err := buildURL(target)
	if err != nil {
		return Result{Outcome: OutcomeUnsupported, Reason: ReasonBlockedDestination, Detail: err.Error(), At: at}
	}
	timeout := target.Settings.Timeout
	if timeout < MinTimeout || timeout > MaxTimeout {
		timeout = DefaultTimeout
	}
	client := p.clientFor(timeout, rawURL)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{Outcome: OutcomeFailed, Reason: ReasonTransport, Detail: sanitize(err.Error()), At: at}
	}
	req.Header.Set("User-Agent", "Gotham-WebsiteCheck/1")
	started := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(started)
	if err != nil {
		reason, unsupported := classifyProbeError(err, probeCtx)
		outcome := OutcomeFailed
		if unsupported {
			outcome = OutcomeUnsupported
		}
		return Result{Outcome: outcome, Reason: reason, Latency: latency, Detail: sanitize(err.Error()), At: at}
	}
	defer resp.Body.Close()
	// Drain a bounded prefix so a slow or endless body cannot hold the probe.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, bodyLimit))

	if resp.StatusCode < target.Settings.ExpectedMin || resp.StatusCode > target.Settings.ExpectedMax {
		return Result{
			Outcome:    OutcomeFailed,
			Reason:     ReasonUnexpectedStatus,
			StatusCode: resp.StatusCode,
			Latency:    latency,
			Detail:     fmt.Sprintf("expected %d-%d", target.Settings.ExpectedMin, target.Settings.ExpectedMax),
			At:         at,
		}
	}
	return Result{
		Outcome:    OutcomeHealthy,
		StatusCode: resp.StatusCode,
		Latency:    latency,
		At:         at,
	}
}

func (p *Prober) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p *Prober) clientFor(timeout time.Duration, rawURL string) *http.Client {
	original, _ := url.Parse(rawURL)
	transport := &http.Transport{
		// A probe must not inherit an interactive shell's HTTP proxy: the
		// managed domain is the only destination, reached through Gotham's
		// own routing.
		Proxy:                 nil,
		DialContext:           p.dialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          2,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return redirectPolicy(original, req, via)
		},
	}
}

func redirectPolicy(original *url.URL, req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: too many redirects", ErrUnsafeRedirect)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme", ErrUnsafeRedirect)
	}
	if req.URL.User != nil {
		return fmt.Errorf("%w: credentials are not allowed", ErrUnsafeRedirect)
	}
	if original == nil || !strings.EqualFold(req.URL.Host, original.Host) {
		return fmt.Errorf("%w: host changed", ErrUnsafeRedirect)
	}
	return nil
}

func (p *Prober) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if p != nil && p.Dial != nil {
		return p.Dial(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return nil, ErrBlockedDestination
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		// Wrap so the caller can identify a DNS failure without exposing
		// resolver internals to the user.
		return nil, &dnsFailure{err: err}
	}
	blocked := false
	for _, address := range addresses {
		if blockedIP(address.IP) {
			blocked = true
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	if blocked {
		return nil, ErrBlockedDestination
	}
	if err == nil {
		err = errors.New("no address")
	}
	return nil, err
}

type dnsFailure struct{ err error }

func (e *dnsFailure) Error() string { return "dns lookup failed: " + e.err.Error() }
func (e *dnsFailure) Unwrap() error { return e.err }

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func classifyProbeError(err error, ctx context.Context) (string, bool) {
	if errors.Is(err, ErrBlockedDestination) {
		return ReasonBlockedDestination, true
	}
	if errors.Is(err, ErrUnsafeRedirect) {
		return ReasonUnsafeRedirect, false
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout, false
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return ReasonTimeout, false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ReasonDNS, false
	}
	var dnsWrap *dnsFailure
	if errors.As(err, &dnsWrap) {
		return ReasonDNS, false
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return ReasonTLS, false
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return ReasonTLS, false
	}
	var invalidErr x509.CertificateInvalidError
	if errors.As(err, &invalidErr) {
		return ReasonTLS, false
	}
	if strings.Contains(strings.ToLower(err.Error()), "tls:") ||
		strings.Contains(strings.ToLower(err.Error()), "x509:") {
		return ReasonTLS, false
	}
	return ReasonTransport, false
}

func buildURL(target Target) (string, error) {
	path, err := normalizePath(target.Settings.Path)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(strings.TrimSpace(target.Protocol))
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidPath
	}
	host := strings.TrimSpace(target.Domain)
	if host == "" || strings.ContainsAny(host, "/@?#") {
		return "", ErrInvalidPath
	}
	// The managed domain is the only accepted host; no client-supplied host
	// or port can reach the prober.
	if _, _, err := net.SplitHostPort(host); err == nil {
		return "", ErrInvalidPath
	}
	u := url.URL{Scheme: scheme, Host: strings.ToLower(host), Path: path}
	return u.String(), nil
}

func sanitize(detail string) string {
	detail = strings.ReplaceAll(detail, "\n", " ")
	detail = strings.TrimSpace(detail)
	if len(detail) > maxEvidenceLen {
		detail = detail[:maxEvidenceLen]
	}
	return detail
}
