package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/clientip"
)

const oauthStateCookieMaxAge = 600

const oauthFlowCookieMaxAge = 600

const oauthRandomBytes = 32

const (
	oauthExchangeTTL           = 60 * time.Second
	oauthExchangeCleanupPeriod = time.Minute
	oauthExchangeCapacity      = 10000
)

var errOAuthCodeStoreFull = errors.New("oauth: exchange code store full")

const oauthFailureRedirect = "/login?error=oauth_failed"

const oauthCallbackPath = "/oauth/callback"

type oauthExchangeRequest struct {
	Code string `json:"code"`
}

type OAuthService interface {
	Begin(ctx context.Context, providerName, redirectBase string) (url string, state string, err error)
	Callback(ctx context.Context, providerName, code, state string, meta auth.SessionMeta) (*auth.AuthResult, error)
	Close()
}

func (s *Server) mountOAuthRoutes(api chi.Router) {
	api.Route("/v1/auth/oauth", func(r chi.Router) {
		r.With(s.rateLimit).Get("/{provider}/login", s.handleOAuthLogin)
		r.Get("/{provider}/callback", s.handleOAuthCallback)
		r.With(s.rateLimit).Post("/exchange", s.handleOAuthExchange)
	})
}

func (s *Server) insecureOAuthRejected(r *http.Request) bool {
	if s.isSecureRequest(r) {
		return false
	}
	base := s.oauthRedirectBase()
	return base == "" || strings.HasPrefix(base, "https://")
}

func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")

	if s.insecureOAuthRejected(r) {
		s.logger.Warn("oauth: insecure login rejected", "provider", provider)
		s.redirectOAuthFailure(w, r)
		return
	}

	url, state, err := s.oauth.Begin(r.Context(), provider, s.oauthRedirectBase())
	if err != nil {
		if errors.Is(err, auth.ErrProviderDisabled) {
			writeJSON(w, http.StatusNotFound, apiError{Message: "oauth provider not available"})
			return
		}
		s.logger.Error("oauth: begin", "provider", provider, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	binding, err := newOAuthFlowBinding()
	if err != nil {
		s.logger.Error("oauth: flow binding", "provider", provider, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	http.SetCookie(w, s.oauthStateCookie(r, state, oauthStateCookieMaxAge))
	http.SetCookie(w, s.oauthFlowCookie(r, binding, oauthFlowCookieMaxAge))
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if s.insecureOAuthRejected(r) {
		s.logger.Warn("oauth: insecure callback rejected", "provider", provider)
		s.redirectOAuthFailure(w, r)
		return
	}

	cookieValue, ok := oauthCookieValue(r, s.oauthStateCookieName(r))
	if !ok || queryState == "" || !constantTimeEqual(cookieValue, queryState) {
		s.logger.Warn("oauth: state cookie mismatch", "provider", provider)
		s.redirectOAuthFailure(w, r)
		return
	}

	flowValue, ok := oauthCookieValue(r, s.oauthFlowCookieName(r))
	if !ok || flowValue == "" {
		s.logger.Warn("oauth: flow cookie missing", "provider", provider)
		s.redirectOAuthFailure(w, r)
		return
	}

	result, err := s.oauth.Callback(r.Context(), provider, code, queryState, s.sessionMeta(r))
	if err != nil {
		s.logger.Warn("oauth: callback failed", "provider", provider, "error", err)
		s.redirectOAuthFailure(w, r)
		return
	}

	exchangeCode, err := s.oauthCodes.NewCode(result, flowValue, s.isSecureRequest(r))
	if err != nil {
		s.logger.Error("oauth: issue exchange code", "provider", provider, "error", err)
		s.redirectOAuthFailure(w, r)
		return
	}

	http.SetCookie(w, s.oauthStateCookie(r, "", -1))
	http.Redirect(w, r, s.oauthSuccessLocation(exchangeCode), http.StatusFound)
}

func (s *Server) handleOAuthExchange(w http.ResponseWriter, r *http.Request) {
	if s.insecureOAuthRejected(r) {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req oauthExchangeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	flowValue, ok := oauthCookieValue(r, s.oauthFlowCookieName(r))
	if !ok || flowValue == "" {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	result, ok := s.oauthCodes.Exchange(req.Code, flowValue, s.isSecureRequest(r))
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	http.SetCookie(w, s.oauthFlowCookie(r, "", -1))
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

func (s *Server) oauthSuccessLocation(code string) string {
	return s.oauthRedirectBase() + oauthCallbackPath + "?code=" + url.QueryEscape(code)
}

func (s *Server) oauthRedirectBase() string {
	raw := strings.TrimSpace(s.cfg.Snapshot().OAuth.GitHub.RedirectURL)
	if raw == "" {
		raw = s.controlPlaneURL()
	}
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (s *Server) controlPlaneURL() string {
	if s.instanceSvc == nil {
		return ""
	}
	cp, err := s.instanceSvc.ControlPlaneURL(context.Background())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cp)
}

func (s *Server) controlPlaneURLSource() func(context.Context) string {
	if s.instanceSvc == nil {
		return nil
	}
	return func(ctx context.Context) string {
		cp, err := s.instanceSvc.ControlPlaneURL(ctx)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(cp)
	}
}

func (s *Server) redirectOAuthFailure(w http.ResponseWriter, r *http.Request) {
	s.clearOAuthCookies(w, r)
	http.Redirect(w, r, oauthFailureRedirect, http.StatusFound)
}

func (s *Server) clearOAuthCookies(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{
		auth.StateCookieName,
		auth.FlowCookieName,
		auth.StateCookieNameSecure,
		auth.FlowCookieNameSecure,
	} {
		http.SetCookie(w, s.oauthCookie(r, name, "", -1))
	}
}

func newOAuthFlowBinding() (string, error) {
	buf := make([]byte, oauthRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth: generate flow binding: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Server) oauthStateCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return s.oauthCookie(r, s.oauthStateCookieName(r), value, maxAge)
}

func (s *Server) oauthFlowCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return s.oauthCookie(r, s.oauthFlowCookieName(r), value, maxAge)
}

func (s *Server) oauthStateCookieName(r *http.Request) string {
	if s.isSecureRequest(r) {
		return auth.StateCookieNameSecure
	}
	return auth.StateCookieName
}

func (s *Server) oauthFlowCookieName(r *http.Request) string {
	if s.isSecureRequest(r) {
		return auth.FlowCookieNameSecure
	}
	return auth.FlowCookieName
}

func oauthCookieValue(r *http.Request, name string) (string, bool) {
	value := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			count++
			value = cookie.Value
		}
	}
	if count != 1 {
		return "", false
	}
	return value, true
}

func (s *Server) oauthCookie(r *http.Request, name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.oauthCookieSecure(r, name),
	}
}

func (s *Server) oauthCookieSecure(r *http.Request, name string) bool {
	if strings.HasPrefix(name, "__Host-") {
		return true
	}
	return s.isSecureRequest(r)
}

func (s *Server) isSecureRequest(r *http.Request) bool {
	return clientip.IsSecure(r, s.trustedProxies)
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type oauthCodeStore struct {
	mu      sync.Mutex
	entries map[string]oauthExchangeEntry
	now     func() time.Time
	stop    chan struct{}
	once    sync.Once
}

type oauthExchangeEntry struct {
	result    *auth.AuthResult
	binding   string
	secure    bool
	expiresAt time.Time
}

func newOAuthCodeStore() *oauthCodeStore {
	s := &oauthCodeStore{
		entries: make(map[string]oauthExchangeEntry),
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

func (s *oauthCodeStore) NewCode(result *auth.AuthResult, binding string, secure bool) (string, error) {
	buf := make([]byte, oauthRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth: generate exchange code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= oauthExchangeCapacity {
		return "", errOAuthCodeStoreFull
	}
	s.entries[code] = oauthExchangeEntry{
		result:    result,
		binding:   binding,
		secure:    secure,
		expiresAt: s.now().Add(oauthExchangeTTL),
	}

	return code, nil
}

func (s *oauthCodeStore) Exchange(code, binding string, secure bool) (*auth.AuthResult, bool) {
	s.mu.Lock()
	entry, ok := s.entries[code]
	if ok {
		delete(s.entries, code)
	}
	s.mu.Unlock()

	if !ok {
		return nil, false
	}
	if s.now().After(entry.expiresAt) {
		return nil, false
	}
	if entry.secure != secure {
		return nil, false
	}
	if !constantTimeEqual(entry.binding, binding) {
		return nil, false
	}
	return entry.result, true
}

func (s *oauthCodeStore) cleanupLoop() {
	ticker := time.NewTicker(oauthExchangeCleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.cleanup(now)
		}
	}
}

func (s *oauthCodeStore) cleanup(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for code, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, code)
		}
	}
}

func (s *oauthCodeStore) Close() {
	s.once.Do(func() { close(s.stop) })
}
