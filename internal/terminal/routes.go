package terminal

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/auth"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// AllowedOriginsEnv optionally lists extra browser origins (comma-separated,
// scheme included) allowed to open a terminal WebSocket. Same-origin requests
// are always allowed; an empty list therefore keeps a same-origin panel working
// without configuration.
const AllowedOriginsEnv = "TERMINAL_ALLOWED_ORIGINS"

// TokenVerifier validates the access token carried by a WebSocket handshake.
// It mirrors AuthService.VerifyAccessToken.
type TokenVerifier interface {
	VerifyAccessToken(token string) (*auth.Claims, error)
}

// Marker identifies one sign-in lineage. Refresh-token rotation revokes the
// presented session row and inserts a replacement that inherits the original
// sign-in created_at, so the marker survives normal rotation while logout,
// revoke-all, account deletion and password reset remove every row carrying it.
type Marker struct {
	SignInAt time.Time
}

// Authorizer performs attach-time authorization and periodic revalidation of
// an open terminal session.
//
// Authorize runs once, at the WebSocket handshake, and returns the sign-in
// lineage marker for the session. Revalidate runs on every interval:
//
//   - (true, nil)  keep the shell;
//   - (false, nil) authorization is definitively revoked (logout, revoke-all,
//     account deletion, demotion) and the shell ends;
//   - (_, err)     the lookup failed transiently; the shell is kept and the
//     error is retried, failing closed only after several consecutive errors.
//
// Authorize must refuse a token that carries no session id: without a sid there
// is no lineage a logout could revoke.
type Authorizer interface {
	Authorize(ctx context.Context, claims *auth.Claims) (Marker, error)
	Revalidate(ctx context.Context, claims *auth.Claims, marker Marker) (bool, error)
}

// Revalidation and liveness defaults. They are fields on the handler (not
// package globals) so tests can shorten them without racing each other.
const (
	// defaultRevalidatePeriod bounds how long a de-authorized operator keeps a
	// shell. A revoked session, a role/email change or an expired access token
	// is noticed within this window.
	defaultRevalidatePeriod = 30 * time.Second
	// defaultPingPeriod is how often the server sends a heartbeat frame.
	defaultPingPeriod = 25 * time.Second
	// defaultIdleTimeout closes a half-open socket that sends nothing (not even
	// a heartbeat) for this long.
	defaultIdleTimeout = 60 * time.Second
	// defaultRevalidateTimeout bounds one authorization lookup, so a hung
	// database cannot delay revocation indefinitely.
	defaultRevalidateTimeout = 5 * time.Second
	// revalidateMaxErrors is how many consecutive transient lookup failures are
	// tolerated before the shell fails closed (3 ticks is about 90 s at the
	// production interval).
	revalidateMaxErrors = 3
	// defaultWriteTimeout bounds one frame write we initiate.
	defaultWriteTimeout = 10 * time.Second
)

// handler serves the terminal HTTP and WebSocket surface.
type handler struct {
	svc        *Service
	verify     TokenVerifier
	authorizer Authorizer
	userIDFrom UserIDFunc
	logger     *slog.Logger

	revalidatePeriod  time.Duration
	revalidateTimeout time.Duration
	pingPeriod        time.Duration
	idleTimeout       time.Duration
	writeTimeout      time.Duration

	// writeMu serializes our own frame writes so one writer's deadline reset
	// cannot clear another writer's in-flight deadline.
	writeMu sync.Mutex
}

// handlerOptions overrides the liveness timers (tests).
type handlerOptions struct {
	revalidatePeriod  time.Duration
	revalidateTimeout time.Duration
	pingPeriod        time.Duration
	idleTimeout       time.Duration
	writeTimeout      time.Duration
}

// UserIDFunc resolves the authenticated user set by the server's auth
// middleware. It is passed in so this package never imports the HTTP server.
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// Mount registers the terminal session routes on the /api router.
//
//	POST   /v1/servers/{id}/terminal/sessions
//	DELETE /v1/servers/{id}/terminal/sessions/{sessionID}
//	GET    /v1/servers/{id}/terminal/sessions/{sessionID}/ws
//
// The two control routes run behind auth (the server passes its platform-
// operator chain). The WebSocket route authenticates in-band because browsers
// cannot set an Authorization header on a WebSocket; the token arrives as a
// "gotham.token.<jwt>" subprotocol or a Bearer header, never in the query.
func Mount(r chi.Router, authMiddleware func(http.Handler) http.Handler, svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger) {
	mount(r, authMiddleware, svc, verifier, authorizer, userIDFrom, logger, handlerOptions{})
}

// mount is Mount with test-only timer overrides.
func mount(r chi.Router, authMiddleware func(http.Handler) http.Handler, svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger, options handlerOptions) {
	if svc == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{
		svc:               svc,
		verify:            verifier,
		authorizer:        authorizer,
		userIDFrom:        userIDFrom,
		logger:            logger,
		revalidatePeriod:  options.revalidatePeriod,
		revalidateTimeout: options.revalidateTimeout,
		pingPeriod:        options.pingPeriod,
		idleTimeout:       options.idleTimeout,
		writeTimeout:      options.writeTimeout,
	}
	if h.revalidatePeriod <= 0 {
		h.revalidatePeriod = defaultRevalidatePeriod
	}
	if h.revalidateTimeout <= 0 {
		h.revalidateTimeout = defaultRevalidateTimeout
	}
	if h.pingPeriod <= 0 {
		h.pingPeriod = defaultPingPeriod
	}
	if h.idleTimeout <= 0 {
		h.idleTimeout = defaultIdleTimeout
	}
	if h.writeTimeout <= 0 {
		h.writeTimeout = defaultWriteTimeout
	}

	r.Group(func(protected chi.Router) {
		protected.Use(authMiddleware)
		protected.Post("/v1/servers/{id}/terminal/sessions", h.create)
		protected.Delete("/v1/servers/{id}/terminal/sessions/{sessionID}", h.close)
	})
	r.Get("/v1/servers/{id}/terminal/sessions/{sessionID}/ws", h.attach)
}

// createResponse tells the client how to attach.
type createResponse struct {
	ID            string    `json:"id"`
	ServerID      string    `json:"server_id"`
	Target        string    `json:"target"`
	Root          string    `json:"root"`
	Cwd           string    `json:"cwd"`
	Cols          uint32    `json:"cols"`
	Rows          uint32    `json:"rows"`
	ExpiresAt     time.Time `json:"expires_at"`
	WebSocketPath string    `json:"websocket_path"`
	Subprotocol   string    `json:"subprotocol"`
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverParam(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	var req CreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return
	}
	session, err := h.svc.Create(r.Context(), userID, serverID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createResponse{
		ID:            session.ID.String(),
		ServerID:      session.ServerID.String(),
		Target:        session.Target,
		Root:          session.Root,
		Cwd:           session.Cwd,
		Cols:          session.Cols,
		Rows:          session.Rows,
		ExpiresAt:     session.ExpiresAt,
		WebSocketPath: "/api/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws",
		Subprotocol:   Subprotocol,
	})
}

func (h *handler) close(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverParam(w, r)
	if !ok {
		return
	}
	sessionID, ok := sessionParam(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	if err := h.svc.Close(r.Context(), userID, serverID, sessionID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// attach authenticates, authorizes and upgrades to the private terminal stream.
func (h *handler) attach(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverParam(w, r)
	if !ok {
		return
	}
	sessionID, ok := sessionParam(w, r)
	if !ok {
		return
	}
	if !originAllowed(r) {
		writeJSON(w, http.StatusForbidden, apiError{Message: "origin is not allowed"})
		return
	}
	claims, userID, err := h.authenticate(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	if h.authorizer == nil {
		writeJSON(w, http.StatusForbidden, apiError{Message: "platform operator access is required"})
		return
	}
	authCtx, authCancel := context.WithTimeout(r.Context(), h.revalidateTimeout)
	marker, authErr := h.authorizer.Authorize(authCtx, claims)
	authCancel()
	if authErr != nil {
		h.logger.Warn("terminal: authorization refused", "error", authErr)
		writeJSON(w, http.StatusForbidden, apiError{Message: "platform operator access is required"})
		return
	}
	// Ownership and single-use are checked before the upgrade so the client
	// gets a real 404/409. Attach itself runs in the handshake: a plain GET
	// (no Upgrade headers) is rejected by the WebSocket reader before that
	// point, so a non-upgrade request can never burn the session.
	if err := h.svc.Validate(r.Context(), userID, serverID, sessionID); err != nil {
		h.writeServiceError(w, err)
		return
	}

	var session *Session
	server := websocket.Server{
		// The handshake selects exactly one subprotocol when the client offered
		// it; the x/net server rejects the upgrade when more than one remains
		// (the token travels as a second offered protocol and is not echoed).
		Handshake: func(config *websocket.Config, req *http.Request) error {
			offered := false
			for _, header := range req.Header.Values("Sec-WebSocket-Protocol") {
				for _, protocol := range strings.Split(header, ",") {
					if strings.TrimSpace(protocol) == Subprotocol {
						offered = true
					}
				}
			}
			if offered {
				config.Protocol = []string{Subprotocol}
			} else {
				config.Protocol = nil
			}
			attached, err := h.svc.Attach(req.Context(), userID, serverID, sessionID)
			if err != nil {
				return err
			}
			session = attached
			return nil
		},
		Handler: func(conn *websocket.Conn) {
			if session == nil {
				_ = conn.Close()
				return
			}
			h.serve(conn, session, claims, marker)
		},
	}
	server.ServeHTTP(w, r)
}

// serve relays one WebSocket to one agent terminal stream.
func (h *handler) serve(conn *websocket.Conn, session *Session, claims *auth.Claims, marker Marker) {
	defer func() { _ = conn.Close() }()
	defer h.svc.Release(session.ID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Re-authorize for the life of the stream from server-side state only:
	// logout/revocation, a current-role demotion and a removed PLATFORM_ADMINS
	// entry end the shell within revalidatePeriod. The handshake token's own
	// expiry is deliberately not consulted here: the browser cannot present a
	// refreshed token on an open socket, and the session contract is 8h.
	go h.revalidate(ctx, cancel, conn, session, claims, marker)

	// A session expiry (or an explicit DELETE) cancels the relay, which closes
	// the agent stream and tears the PTY down.
	go func() {
		select {
		case <-session.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	conn.MaxPayloadBytes = maxWSFrame
	// Terminal output is raw bytes that may split a UTF-8 sequence; it must
	// travel in binary frames so the browser never applies text decoding.
	conn.PayloadType = websocket.BinaryFrame
	h.svc.Confirm(session.ID)
	select {
	case <-session.Done():
		// The pending lifetime can elapse between attach and this point; the
		// session is already gone, so never start a shell nobody owns.
		h.fail(conn, session, "the session expired before it started")
		return
	default:
	}

	if h.svc.Registry() == nil {
		h.fail(conn, session, "the node registry is unavailable")
		return
	}
	node, err := h.svc.Registry().Get(ctx, session.ServerID)
	if err != nil || node == nil {
		h.fail(conn, session, "the node is unavailable")
		return
	}
	client, err := h.svc.Dial(ctx, node)
	if err != nil {
		h.fail(conn, session, "the node agent is unavailable")
		return
	}
	defer func() { _ = client.Close() }()

	stream, err := client.OpenTerminal(ctx)
	if err != nil {
		h.fail(conn, session, relayError(err))
		return
	}
	defer func() { _ = stream.CloseSend() }()

	if err := stream.Send(&agentv1.TerminalRequest{
		Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
			Target: session.Target,
			Root:   session.Root,
			Cwd:    session.Cwd,
			Cols:   session.Cols,
			Rows:   session.Rows,
		}},
	}); err != nil {
		h.fail(conn, session, relayError(err))
		return
	}
	opened, err := recvOpened(ctx, stream)
	if err != nil {
		h.fail(conn, session, relayError(err))
		return
	}
	payload, _ := json.Marshal(openedEvent{
		Shell:       opened.GetShell(),
		Cols:        opened.GetCols(),
		Rows:        opened.GetRows(),
		ServiceUser: opened.GetServiceUser(),
	})
	if err := h.sendFrame(conn, opOpened, payload); err != nil {
		return
	}

	keepaliveDone := make(chan struct{})
	go h.keepalive(ctx, cancel, conn, keepaliveDone)
	defer close(keepaliveDone)

	pumpDone := make(chan error, 1)
	go func() {
		err := h.pumpAgentToClient(conn, stream)
		pumpDone <- err
		// The shell ended (or the stream failed): close the socket so the
		// client reader below returns instead of blocking forever.
		_ = conn.Close()
	}()

	readErr := readClientFrames(stream, conn, h.idleTimeout)
	cancel()
	_ = client.Close()

	var pumpErr error
	select {
	case pumpErr = <-pumpDone:
	case <-time.After(openTimeout):
	}

	switch {
	case readErr != nil && !errors.Is(readErr, io.EOF) && !isClosedError(readErr):
		h.logger.Info("terminal: client stream ended", "session_id", session.ID.String(), "error", readErr)
	case pumpErr != nil && !errors.Is(pumpErr, context.Canceled) && !isClosedError(pumpErr):
		h.logger.Info("terminal: agent stream ended", "session_id", session.ID.String(), "error", pumpErr)
	}
}

// revalidate polls server-side authorization state while the relay is active:
// the account must still exist, its current role (or PLATFORM_ADMINS entry) must
// still grant operator access, and a live session carrying the same sign-in
// lineage must still exist. It never re-verifies the access token: the browser
// cannot present a refreshed token on an open socket, so the token's 15-minute
// expiry must not bound an 8-hour shell. A transient lookup error keeps the
// shell and is retried; after revalidateMaxErrors consecutive failures the
// shell fails closed.
func (h *handler) revalidate(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, session *Session, claims *auth.Claims, marker Marker) {
	if claims == nil || h.authorizer == nil {
		return
	}
	ticker := time.NewTicker(h.revalidatePeriod)
	defer ticker.Stop()

	consecutiveErrors := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-session.Done():
			return
		case <-ticker.C:
			checkCtx, checkCancel := context.WithTimeout(ctx, h.revalidateTimeout)
			ok, err := h.authorizer.Revalidate(checkCtx, claims, marker)
			checkCancel()

			if err != nil {
				// A tick that raced a normal close sees a cancelled context and
				// a failed lookup; that is not a revocation.
				if ctx.Err() != nil || sessionClosed(session) {
					return
				}
				consecutiveErrors++
				h.logger.Warn("terminal: revalidation unavailable",
					"session_id", session.ID.String(), "consecutive", consecutiveErrors, "error", err)
				if consecutiveErrors < revalidateMaxErrors {
					continue
				}
			} else {
				consecutiveErrors = 0
				if ok {
					continue
				}
			}

			message := "terminal session was revoked"
			if err != nil {
				message = "terminal session authorization could not be confirmed"
			}
			_ = h.sendFrame(conn, opError, []byte(message))
			h.logger.Info("terminal: session authorization ended",
				"session_id", session.ID.String(), "consecutive_errors", consecutiveErrors)
			cancel()
			_ = conn.Close()
			return
		}
	}
}

// sessionClosed reports whether the service released the session.
func sessionClosed(session *Session) bool {
	select {
	case <-session.Done():
		return true
	default:
		return false
	}
}

// keepalive sends a heartbeat frame so a client can distinguish a quiet shell
// from a dead socket, and so a half-open connection is noticed by the idle
// read deadline.
func (h *handler) keepalive(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(h.pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := h.sendFrame(conn, opPing, nil); err != nil {
				cancel()
				_ = conn.Close()
				return
			}
		}
	}
}

// readClientFrames forwards browser input to the agent until the socket closes
// or the client asks to close. It is the only goroutine that sends requests
// after the open message.
func readClientFrames(stream agentv1.TerminalService_OpenTerminalClient, conn *websocket.Conn, idleTimeout time.Duration) error {
	for {
		// Any inbound frame (input, resize, heartbeat) refreshes the deadline.
		if err := conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return err
		}
		var payload []byte
		if err := websocket.Message.Receive(conn, &payload); err != nil {
			return err
		}
		if len(payload) == 0 {
			return errors.New("terminal: empty frame")
		}
		switch payload[0] {
		case opInput:
			data := payload[1:]
			if len(data) == 0 {
				continue
			}
			if len(data) > maxInputBytes {
				return errors.New("terminal: input frame too large")
			}
			if err := stream.Send(&agentv1.TerminalRequest{
				Part: &agentv1.TerminalRequest_Input{Input: append([]byte(nil), data...)},
			}); err != nil {
				return err
			}
		case opResize:
			if len(payload) != 5 {
				return errors.New("terminal: malformed resize frame")
			}
			cols := uint32(binary.BigEndian.Uint16(payload[1:3]))
			rows := uint32(binary.BigEndian.Uint16(payload[3:5]))
			if err := ValidateDimensions(cols, rows); err != nil {
				return err
			}
			if err := stream.Send(&agentv1.TerminalRequest{
				Part: &agentv1.TerminalRequest_Resize{Resize: &agentv1.TerminalResize{Cols: cols, Rows: rows}},
			}); err != nil {
				return err
			}
		case opHeartbeat:
			// Client heartbeat; it only refreshes the idle deadline.
			continue
		case opClose:
			_ = stream.CloseSend()
			return io.EOF
		default:
			return errors.New("terminal: unknown frame opcode")
		}
	}
}

// pumpAgentToClient forwards agent output and the final exit event.
func (h *handler) pumpAgentToClient(conn *websocket.Conn, stream agentv1.TerminalService_OpenTerminalClient) error {
	for {
		response, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch event := response.GetEvent().(type) {
		case *agentv1.TerminalResponse_Output:
			if err := h.sendFrame(conn, opOutput, event.Output); err != nil {
				return err
			}
		case *agentv1.TerminalResponse_Exited:
			payload, _ := json.Marshal(exitedEvent{
				ExitCode: event.Exited.GetExitCode(),
				Signal:   event.Exited.GetSignal(),
			})
			_ = h.sendFrame(conn, opExited, payload)
			return nil
		default:
			return errors.New("terminal: unexpected agent event")
		}
	}
}

// recvOpened waits for the agent's opened event with a bounded timeout.
func recvOpened(ctx context.Context, stream agentv1.TerminalService_OpenTerminalClient) (*agentv1.TerminalOpened, error) {
	type result struct {
		opened *agentv1.TerminalOpened
		err    error
	}
	done := make(chan result, 1)
	go func() {
		response, err := stream.Recv()
		if err != nil {
			done <- result{err: err}
			return
		}
		done <- result{opened: response.GetOpened()}
	}()
	select {
	case value := <-done:
		if value.err != nil {
			return nil, value.err
		}
		if value.opened == nil {
			return nil, errors.New("terminal: agent did not acknowledge the open")
		}
		return value.opened, nil
	case <-time.After(openTimeout):
		return nil, status.Error(codes.DeadlineExceeded, "terminal: agent did not acknowledge the open")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sendFrame writes one server frame with a bounded write deadline and then
// clears it. The deadline must not outlive this write: the x/net read path also
// writes on the same connection (a protocol-level pong, or a close reply), and
// a stale deadline left by our last frame would make that write fail with
// i/o timeout and tear down an otherwise healthy shell.
func (h *handler) sendFrame(conn *websocket.Conn, opcode byte, payload []byte) error {
	frame := make([]byte, len(payload)+1)
	frame[0] = opcode
	copy(frame[1:], payload)

	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err != nil {
		return err
	}
	err := websocket.Message.Send(conn, frame)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

// fail reports a bounded error frame and ends the session.
func (h *handler) fail(conn *websocket.Conn, session *Session, message string) {
	if len(message) > maxErrorBytes {
		message = message[:maxErrorBytes]
	}
	_ = h.sendFrame(conn, opError, []byte(message))
	h.logger.Info("terminal: session failed", "session_id", session.ID.String(), "reason", message)
}

// authenticate extracts and verifies the access token from the handshake.
func (h *handler) authenticate(r *http.Request) (*auth.Claims, uuid.UUID, error) {
	if h.verify == nil {
		return nil, uuid.Nil, errors.New("terminal: no token verifier")
	}
	token := tokenFromRequest(r)
	if token == "" {
		return nil, uuid.Nil, errors.New("terminal: no access token")
	}
	claims, err := h.verify.VerifyAccessToken(token)
	if err != nil || claims == nil {
		return nil, uuid.Nil, errors.New("terminal: invalid access token")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return nil, uuid.Nil, errors.New("terminal: token has no user")
	}
	return claims, userID, nil
}

// tokenFromRequest reads the access token from the Authorization header or,
// for browsers, a subprotocol. The URL is never inspected.
func tokenFromRequest(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		if scheme, value, ok := strings.Cut(header, " "); ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(value)
		}
	}
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, protocol := range strings.Split(header, ",") {
			protocol = strings.TrimSpace(protocol)
			if strings.HasPrefix(protocol, TokenProtocolPrefix) {
				return strings.TrimPrefix(protocol, TokenProtocolPrefix)
			}
		}
	}
	return ""
}

// originAllowed accepts a missing Origin (non-browser clients), the panel's own
// origin, and any explicitly configured extra origin. A browser always sends
// Origin, so a hostile page cannot drive a terminal with an operator's cookie
// or token.
func originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return false
	}
	// Same origin means the same scheme as well as the same host: an https
	// page must not drive an http connection (and vice versa).
	if strings.EqualFold(parsed.Scheme, requestScheme(r)) && strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	for _, allowed := range strings.Split(os.Getenv(AllowedOriginsEnv), ",") {
		entry, err := url.Parse(strings.TrimSpace(allowed))
		if err != nil || entry.Host == "" || entry.Scheme == "" {
			continue
		}
		if strings.EqualFold(entry.Scheme, parsed.Scheme) && strings.EqualFold(entry.Host, parsed.Host) {
			return true
		}
	}
	return false
}

// requestScheme is the scheme the browser used to reach the panel. A TLS
// connection is https; behind a terminating proxy X-Forwarded-Proto carries
// the original scheme. This only affects Origin comparison, never trust in
// the proxy for authentication.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if value := strings.ToLower(strings.TrimSpace(first)); value != "" {
			return value
		}
	}
	return "http"
}

// writeServiceError maps a session error onto an HTTP status.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, ErrAlreadyAttached):
		writeJSON(w, http.StatusConflict, apiError{Message: "the session is already attached"})
	case errors.Is(err, ErrTooManySessions):
		writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many terminal sessions"})
	case errors.Is(err, ErrUnsupportedTarget):
		writeJSON(w, http.StatusNotImplemented, apiError{Message: "the terminal target is not supported"})
	case errors.Is(err, ErrInvalid):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	default:
		h.logger.Error("terminal: session request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// relayError is the bounded user-facing reason for a failed agent stream. It
// never includes a host path or the agent's raw message: the cwd-invalid case
// is recognised by the agent's stable "terminal: invalid working directory"
// prefix and reported as a fixed phrase.
func relayError(err error) string {
	switch status.Code(err) {
	case codes.Unimplemented:
		return "the node agent does not support terminal sessions"
	case codes.ResourceExhausted:
		return "the node is running the maximum number of terminal sessions"
	case codes.NotFound:
		return "the working directory no longer exists"
	case codes.PermissionDenied:
		return "the working directory is not readable"
	case codes.FailedPrecondition:
		return "the node cannot start a shell"
	case codes.InvalidArgument:
		if strings.HasPrefix(status.Convert(err).Message(), "terminal: invalid working directory") {
			return "invalid working directory"
		}
		return "the terminal request was rejected"
	case codes.DeadlineExceeded:
		return "the node did not start the shell in time"
	default:
		return "the terminal stream failed"
	}
}

// isClosedError reports a write/read on a closed WebSocket as normal end.
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "use of closed network connection") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "broken pipe")
}

func serverParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

func sessionParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid session id"})
		return uuid.Nil, false
	}
	return id, true
}

type apiError struct {
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
