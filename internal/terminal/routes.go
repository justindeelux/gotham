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

const AllowedOriginsEnv = "TERMINAL_ALLOWED_ORIGINS"

type TokenVerifier interface {
	VerifyAccessToken(token string) (*auth.Claims, error)
}

type Marker struct {
	SignInAt time.Time
}

type Authorizer interface {
	Authorize(ctx context.Context, claims *auth.Claims, target Target) (Marker, error)
	Revalidate(ctx context.Context, claims *auth.Claims, marker Marker, target Target) (bool, error)
}

const (
	defaultRevalidatePeriod  = 30 * time.Second
	defaultPingPeriod        = 25 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultRevalidateTimeout = 5 * time.Second
	revalidateMaxErrors      = 3
	defaultWriteTimeout      = 10 * time.Second
)

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

	writeMu sync.Mutex
}

type handlerOptions struct {
	revalidatePeriod  time.Duration
	revalidateTimeout time.Duration
	pingPeriod        time.Duration
	idleTimeout       time.Duration
	writeTimeout      time.Duration
}

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

func Mount(r chi.Router, authMiddleware func(http.Handler) http.Handler, svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger) {
	mount(r, authMiddleware, svc, verifier, authorizer, userIDFrom, logger, handlerOptions{})
}

func mount(r chi.Router, authMiddleware func(http.Handler) http.Handler, svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger, options handlerOptions) {
	h := newHandler(svc, verifier, authorizer, userIDFrom, logger, options)
	if h == nil {
		return
	}
	r.Group(func(protected chi.Router) {
		protected.Use(authMiddleware)
		protected.Post("/v1/servers/{id}/terminal/sessions", h.create)
		protected.Delete("/v1/servers/{id}/terminal/sessions/{sessionID}", h.close)
	})
	r.Get("/v1/servers/{id}/terminal/sessions/{sessionID}/ws", h.attach)
}

func MountApplications(r chi.Router, authMiddleware func(http.Handler) http.Handler, svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger) {
	mountApplications(r, authMiddleware, svc, verifier, authorizer, userIDFrom, logger, handlerOptions{})
}

func mountApplications(r chi.Router, authMiddleware func(http.Handler) http.Handler, svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger, options handlerOptions) {
	if svc == nil || svc.Applications() == nil {
		return
	}
	h := newHandler(svc, verifier, authorizer, userIDFrom, logger, options)
	if h == nil {
		return
	}
	r.Group(func(protected chi.Router) {
		protected.Use(authMiddleware)
		protected.Get("/v1/applications/{id}/terminal/targets", h.listTargets)
		protected.Post("/v1/applications/{id}/terminal/sessions", h.createApplication)
		protected.Delete("/v1/applications/{id}/terminal/sessions/{sessionID}", h.closeApplication)
	})
	r.Get("/v1/applications/{id}/terminal/sessions/{sessionID}/ws", h.attachApplication)
}

func newHandler(svc *Service, verifier TokenVerifier, authorizer Authorizer, userIDFrom UserIDFunc, logger *slog.Logger, options handlerOptions) *handler {
	if svc == nil {
		return nil
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
	return h
}

type createResponse struct {
	ID            string    `json:"id"`
	ServerID      string    `json:"server_id"`
	Target        string    `json:"target"`
	Root          string    `json:"root,omitempty"`
	Cwd           string    `json:"cwd,omitempty"`
	Cols          uint32    `json:"cols"`
	Rows          uint32    `json:"rows"`
	ExpiresAt     time.Time `json:"expires_at"`
	WebSocketPath string    `json:"websocket_path"`
	Subprotocol   string    `json:"subprotocol"`
	ApplicationID string    `json:"application_id,omitempty"`
	Service       string    `json:"service,omitempty"`
	ContainerID   string    `json:"container_id,omitempty"`
	ContainerName string    `json:"container_name,omitempty"`
	TargetLabel   string    `json:"target_label,omitempty"`
}

func newCreateResponse(session *Session, websocketPath string) createResponse {
	response := createResponse{
		ID:            session.ID.String(),
		ServerID:      session.ServerID.String(),
		Target:        session.Target,
		Root:          session.Root,
		Cwd:           session.Cwd,
		Cols:          session.Cols,
		Rows:          session.Rows,
		ExpiresAt:     session.ExpiresAt,
		WebSocketPath: websocketPath,
		Subprotocol:   Subprotocol,
	}
	if session.Target == TargetContainer {
		response.ApplicationID = session.ApplicationID.String()
		response.Service = session.Service
		response.ContainerID = session.ContainerID
		response.ContainerName = session.ContainerName
		response.TargetLabel = session.TargetLabel
	}
	return response
}

type containerTargetResponse struct {
	Service     string `json:"service"`
	ContainerID string `json:"container_id"`
	Name        string `json:"name"`
	Image       string `json:"image"`
	State       string `json:"state"`
	Replica     int    `json:"replica"`
	Label       string `json:"label"`
}

type targetsResponse struct {
	ApplicationID   string                    `json:"application_id"`
	ApplicationName string                    `json:"application_name"`
	ServerID        string                    `json:"server_id"`
	SourceType      string                    `json:"source_type"`
	Containers      []containerTargetResponse `json:"containers"`
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
	writeJSON(w, http.StatusCreated, newCreateResponse(session,
		"/api/v1/servers/"+session.ServerID.String()+"/terminal/sessions/"+session.ID.String()+"/ws"))
}

func (h *handler) listTargets(w http.ResponseWriter, r *http.Request) {
	appID, ok := applicationParam(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	set, err := h.svc.Applications().ListContainers(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := targetsResponse{
		ApplicationID:   set.ApplicationID.String(),
		ApplicationName: set.ApplicationName,
		ServerID:        set.ServerID.String(),
		SourceType:      set.SourceType,
		Containers:      make([]containerTargetResponse, 0, len(set.Containers)),
	}
	for _, container := range set.Containers {
		response.Containers = append(response.Containers, containerTargetResponse{
			Service:     container.Service,
			ContainerID: container.ContainerID,
			Name:        container.Name,
			Image:       container.Image,
			State:       container.State,
			Replica:     container.Replica,
			Label:       container.Label,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) createApplication(w http.ResponseWriter, r *http.Request) {
	appID, ok := applicationParam(w, r)
	if !ok {
		return
	}
	userID, ok := h.userIDFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	var req AppCreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return
	}
	session, err := h.svc.CreateApplication(r.Context(), userID, appID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newCreateResponse(session,
		"/api/v1/applications/"+appID.String()+"/terminal/sessions/"+session.ID.String()+"/ws"))
}

func (h *handler) closeApplication(w http.ResponseWriter, r *http.Request) {
	appID, ok := applicationParam(w, r)
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
	if err := h.svc.CloseApplication(r.Context(), userID, appID, sessionID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	target := Target{Kind: TargetHost, ServerID: serverID}
	authCtx, authCancel := context.WithTimeout(r.Context(), h.revalidateTimeout)
	marker, authErr := h.authorizer.Authorize(authCtx, claims, target)
	authCancel()
	if authErr != nil {
		h.logger.Warn("terminal: authorization refused", "error", authErr)
		writeJSON(w, http.StatusForbidden, apiError{Message: "platform operator access is required"})
		return
	}
	if err := h.svc.Validate(r.Context(), userID, serverID, sessionID); err != nil {
		h.writeServiceError(w, err)
		return
	}

	var session *Session
	server := websocket.Server{
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
			h.serve(conn, session, claims, marker, target)
		},
	}
	server.ServeHTTP(w, r)
}

func (h *handler) attachApplication(w http.ResponseWriter, r *http.Request) {
	appID, ok := applicationParam(w, r)
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
	pending, err := h.svc.ValidateApplication(r.Context(), userID, appID, sessionID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	target := TargetFor(pending)
	if h.authorizer == nil {
		writeJSON(w, http.StatusForbidden, apiError{Message: "application write access is required"})
		return
	}
	authCtx, authCancel := context.WithTimeout(r.Context(), h.revalidateTimeout)
	marker, authErr := h.authorizer.Authorize(authCtx, claims, target)
	authCancel()
	if authErr != nil {
		h.logger.Warn("terminal: application authorization refused", "error", authErr)
		writeJSON(w, http.StatusForbidden, apiError{Message: "application write access is required"})
		return
	}
	if err := h.svc.RevalidateApplicationTarget(r.Context(), userID, appID, pending); err != nil {
		h.writeServiceError(w, err)
		return
	}

	var session *Session
	server := websocket.Server{
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
			attached, err := h.svc.AttachApplication(req.Context(), userID, appID, sessionID)
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
			h.serve(conn, session, claims, marker, target)
		},
	}
	server.ServeHTTP(w, r)
}

func (h *handler) serve(conn *websocket.Conn, session *Session, claims *auth.Claims, marker Marker, target Target) {
	defer func() { _ = conn.Close() }()
	defer h.svc.Release(session.ID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go h.revalidate(ctx, cancel, conn, session, claims, marker, target)

	go func() {
		select {
		case <-session.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	conn.MaxPayloadBytes = maxWSFrame
	conn.PayloadType = websocket.BinaryFrame
	h.svc.Confirm(session.ID)
	select {
	case <-session.Done():
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
		h.fail(conn, session, relayError(err, target))
		return
	}
	defer func() { _ = stream.CloseSend() }()

	if err := stream.Send(&agentv1.TerminalRequest{
		Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
			Target:      session.Target,
			Root:        session.Root,
			Cwd:         session.Cwd,
			Cols:        session.Cols,
			Rows:        session.Rows,
			ContainerId: session.ContainerID,
		}},
	}); err != nil {
		h.fail(conn, session, relayError(err, target))
		return
	}
	opened, err := recvOpened(ctx, stream)
	if err != nil {
		h.fail(conn, session, relayError(err, target))
		return
	}
	payload, _ := json.Marshal(openedEvent{
		Shell:         opened.GetShell(),
		Cols:          opened.GetCols(),
		Rows:          opened.GetRows(),
		ServiceUser:   opened.GetServiceUser(),
		Target:        session.Target,
		Service:       session.Service,
		ContainerID:   session.ContainerID,
		ContainerName: session.ContainerName,
		TargetLabel:   session.TargetLabel,
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

func (h *handler) revalidate(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, session *Session, claims *auth.Claims, marker Marker, target Target) {
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
			ok, err := h.authorizer.Revalidate(checkCtx, claims, marker, target)
			checkCancel()

			if err != nil {
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

func sessionClosed(session *Session) bool {
	select {
	case <-session.Done():
		return true
	default:
		return false
	}
}

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

func readClientFrames(stream agentv1.TerminalService_OpenTerminalClient, conn *websocket.Conn, idleTimeout time.Duration) error {
	for {
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
			continue
		case opClose:
			_ = stream.CloseSend()
			return io.EOF
		default:
			return errors.New("terminal: unknown frame opcode")
		}
	}
}

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

func (h *handler) fail(conn *websocket.Conn, session *Session, message string) {
	if len(message) > maxErrorBytes {
		message = message[:maxErrorBytes]
	}
	_ = h.sendFrame(conn, opError, []byte(message))
	h.logger.Info("terminal: session failed", "session_id", session.ID.String(), "reason", message)
}

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

func originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return false
	}
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

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		switch strings.ToLower(strings.TrimSpace(first)) {
		case "https", "wss":
			return "https"
		case "http", "ws":
			return "http"
		}
	}
	return "http"
}

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
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, apiError{Message: "application write access is required"})
	case errors.Is(err, ErrNoRunningDeployment):
		writeJSON(w, http.StatusConflict, apiError{Message: "the application has no running deployment"})
	case errors.Is(err, ErrContainerNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "the selected container is no longer available; refresh the targets"})
	case errors.Is(err, ErrContainerNotRunning):
		writeJSON(w, http.StatusConflict, apiError{Message: "the selected container is not running; start the application first"})
	case errors.Is(err, ErrDeployInProgress):
		writeJSON(w, http.StatusConflict, apiError{Message: "a deployment is in progress; try again when it finishes"})
	case errors.Is(err, ErrApplicationsDisabled):
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: "application terminals are disabled"})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, apiError{Message: "the node agent is unavailable"})
	case errors.Is(err, ErrInvalid):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	default:
		h.logger.Error("terminal: session request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

func relayError(err error, target Target) string {
	container := target.Kind == TargetContainer
	message := status.Convert(err).Message()
	switch status.Code(err) {
	case codes.Unimplemented:
		return "the node agent does not support terminal sessions"
	case codes.ResourceExhausted:
		return "the node is running the maximum number of terminal sessions"
	case codes.NotFound:
		if container || strings.HasPrefix(message, "terminal: the container") {
			return "the container no longer exists; redeploy the application"
		}
		return "the working directory no longer exists"
	case codes.PermissionDenied:
		return "the working directory is not readable"
	case codes.FailedPrecondition:
		switch {
		case strings.HasPrefix(message, "terminal: the container is not running"):
			return "the container is not running; start the application first"
		case strings.HasPrefix(message, "terminal: the container has no supported shell"):
			return "the container has no supported shell"
		case strings.HasPrefix(message, "terminal: container sessions are not available"):
			return "this node agent does not support container terminals"
		default:
			return "the node cannot start a shell"
		}
	case codes.InvalidArgument:
		if strings.HasPrefix(message, "terminal: invalid working directory") {
			return "invalid working directory"
		}
		if strings.HasPrefix(message, "terminal: a valid container") {
			return "the container target is invalid"
		}
		if strings.HasPrefix(message, "terminal: a bounded root name is required") {
			return "the node agent needs an update to open a terminal without a folder"
		}
		return "the terminal request was rejected"
	case codes.Unavailable:
		if container {
			return "the node agent or Docker daemon is unavailable"
		}
		return "the node agent is unavailable"
	case codes.DeadlineExceeded:
		return "the node did not start the shell in time"
	default:
		return "the terminal stream failed"
	}
}

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

func applicationParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid application id"})
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
