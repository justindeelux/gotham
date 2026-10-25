package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
)

type TokenVerifier interface {
	VerifyAccessToken(token string) (*auth.Claims, error)
}

type connectionVerifier interface {
	VerifyConnection(context.Context, string) (*auth.Claims, string, error)
}

type connectionRevalidator interface {
	RevalidateConnection(context.Context, string, *auth.Claims) (*auth.Claims, string, error)
}

type connectionState struct {
	claims  *auth.Claims
	role    string
	version uint64
}
type connectionStateKey struct{}

func ConnectionRole(ctx context.Context) (string, bool) {
	state, ok := ctx.Value(connectionStateKey{}).(*connectionState)
	if !ok || state.role == "" {
		return "", false
	}
	return state.role, true
}

func AuthorizationVersion(ctx context.Context) uint64 {
	if state, ok := ctx.Value(connectionStateKey{}).(*connectionState); ok {
		return state.version
	}
	return 0
}

const AuthorizationInterval = time.Second
const AuthorizationTimeout = time.Second

func (h *Handler) verifyConnection(ctx context.Context, token string) (*auth.Claims, string, error) {
	if verifier, ok := h.verify.(connectionVerifier); ok {
		return verifier.VerifyConnection(ctx, token)
	}
	claims, err := h.verify.VerifyAccessToken(token)
	return claims, "", err
}

const RealtimeEnv = "REALTIME_ENABLED"

func RealtimeEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(RealtimeEnv)), "false")
}

const (
	writeWait                 = 10 * time.Second
	pingPeriod                = 25 * time.Second
	maxSubscriptionsPerClient = 32
	authCacheTTL              = 30 * time.Second
)

type SubscriptionAuthorizer func(ctx context.Context, serverID, userID uuid.UUID) error

type TaskAuthorizer func(ctx context.Context, projectID string, userID uuid.UUID) error

type Handler struct {
	hub       *Hub
	verify    TokenVerifier
	authorize SubscriptionAuthorizer
	logger    *slog.Logger

	channelAuthorizeFactory func() func(context.Context, string, uuid.UUID) error
	frameFilter             func(context.Context, uuid.UUID, []byte) []byte
	channelAuthorize        func(context.Context, string, uuid.UUID) error
	taskAuthorize           TaskAuthorizer
	taskSnapshot            func(projectID string) []string

	writeWait    time.Duration
	pingPeriod   time.Duration
	authCacheTTL time.Duration
}

func NewHandler(hub *Hub, verifier TokenVerifier, logger *slog.Logger, authorize SubscriptionAuthorizer) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		hub:          hub,
		verify:       verifier,
		authorize:    authorize,
		logger:       logger,
		writeWait:    writeWait,
		pingPeriod:   pingPeriod,
		authCacheTTL: authCacheTTL,
	}
}

func (h *Handler) SetTaskFeed(authorize TaskAuthorizer, snapshot func(projectID string) []string) {
	h.taskAuthorize = authorize
	h.taskSnapshot = snapshot
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeUnauth(w)
		return
	}
	if h.verify == nil {
		writeUnauth(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), AuthorizationTimeout)
	defer cancel()
	claims, role, err := h.verifyConnection(ctx, token)
	if err != nil {
		writeUnauth(w)
		return
	}

	if userID, parseErr := uuid.Parse(claims.Subject); parseErr == nil {
		r = r.WithContext(context.WithValue(r.Context(), userIDKey{}, userID))
	}
	r = r.WithContext(context.WithValue(r.Context(), connectionStateKey{}, &connectionState{claims: claims, role: role, version: 1}))
	websocket.Handler(h.serveConn).ServeHTTP(w, r)
}

func writeUnauth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
}

type clientRequest struct {
	Subscribe   string `json:"subscribe"`
	Unsubscribe string `json:"unsubscribe"`
}

func (h *Handler) serveConn(conn *websocket.Conn) {
	defer func() { _ = conn.Close() }()

	if h.channelAuthorizeFactory != nil {
		connectionHandler := *h
		connectionHandler.channelAuthorize = h.channelAuthorizeFactory()
		h = &connectionHandler
	}
	request := conn.Request()
	userID, _ := request.Context().Value(userIDKey{}).(uuid.UUID)

	client := h.hub.newClient()
	client.mu.Lock()
	client.userID = userID.String()
	client.mu.Unlock()
	defer h.hub.remove(client)
	select {
	case <-h.hub.done:
		return
	default:
	}

	subs := make(map[string]bool, 8)
	authorized := make(map[uuid.UUID]authDecision, 4)

	for _, channel := range initialChannels(request) {
		h.subscribeChannel(conn, client, channel, request.Context(), userID, subs, authorized)
	}

	incoming := make(chan clientRequest, 8)
	connDone := make(chan struct{})
	defer close(connDone)
	go func() {
		defer close(incoming)
		pumpRequests(func(req *clientRequest) error {
			return websocket.JSON.Receive(conn, req)
		}, incoming, connDone)
	}()

	check := func() bool {
		ctx, cancel := context.WithTimeout(request.Context(), AuthorizationTimeout)
		defer cancel()
		state := request.Context().Value(connectionStateKey{}).(*connectionState)
		token := request.URL.Query().Get("token")
		var claims *auth.Claims
		var role string
		var err error
		if verifier, ok := h.verify.(connectionRevalidator); ok {
			claims, role, err = verifier.RevalidateConnection(ctx, token, state.claims)
		} else {
			claims, role, err = h.verifyConnection(ctx, token)
		}
		if err != nil || (userID != uuid.Nil && claims.Subject != userID.String()) {
			return false
		}
		state.role = role
		state.version++
		return h.subscriptionsAllowed(ctx, userID, subs)
	}
	revalidate := time.NewTicker(AuthorizationInterval)
	defer revalidate.Stop()
	ticker := time.NewTicker(h.pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-client.send:
			if !ok {
				return
			}
			if h.frameFilter != nil {
				payload = h.frameFilter(request.Context(), userID, payload)
				if payload == nil {
					return
				}
			}
			if !h.writeRaw(conn, payload) {
				return
			}
		case req, ok := <-incoming:
			if !ok {
				return
			}
			switch {
			case req.Subscribe != "":
				h.subscribeChannel(conn, client, req.Subscribe, request.Context(), userID, subs, authorized)
			case req.Unsubscribe != "":
				if subs[req.Unsubscribe] {
					delete(subs, req.Unsubscribe)
					h.hub.unsubscribe(client, req.Unsubscribe)
				}
			}
		case <-client.kick:
			h.logger.Warn("ws: disconnecting slow client",
				"user_id", userID.String(), "subscriptions", len(subs))
			return
		case <-client.revalidate:
			if !check() {
				return
			}
		case <-revalidate.C:
			if !check() {
				return
			}
		case <-ticker.C:
			if !h.writeRaw(conn, mustJSON(Message{Type: TypePing})) {
				return
			}
		}
	}
}

func (h *Handler) subscribeChannel(conn *websocket.Conn, client *Client, channel string, ctx context.Context, userID uuid.UUID, subs map[string]bool, authorized map[uuid.UUID]authDecision) {
	ctx, cancel := context.WithTimeout(ctx, AuthorizationTimeout)
	defer cancel()
	if !h.channelAllowed(ctx, channel, userID, authorized) {
		_ = h.writeMessage(conn, Message{Channel: channel, Type: TypeDenied})
		return
	}
	if subs[channel] {
		_ = h.writeMessage(conn, Message{Channel: channel, Type: TypeSubscribed})
		return
	}
	if len(subs) >= maxSubscriptionsPerClient {
		h.logger.Warn("ws: subscription limit reached",
			"user_id", userID.String(), "channel", channel, "limit", maxSubscriptionsPerClient)
		_ = h.writeMessage(conn, Message{Channel: channel, Type: TypeDenied, Data: "subscription limit reached"})
		return
	}
	h.hub.subscribe(client, channel)
	subs[channel] = true
	if projectID, ok := taskChannelProjectID(channel); ok && h.taskSnapshot != nil {
		for _, frame := range h.taskSnapshot(projectID) {
			if !h.channelAllowed(ctx, channel, userID, authorized) {
				client.kickClient()
				return
			}
			payload := []byte(frame)
			if h.frameFilter != nil {
				payload = h.frameFilter(ctx, userID, payload)
				if payload == nil {
					client.kickClient()
					return
				}
			}
			_ = h.writeFrame(conn, payload)
		}
	}
	_ = h.writeMessage(conn, Message{Channel: channel, Type: TypeSubscribed})
}

func (h *Handler) writeMessage(conn *websocket.Conn, msg Message) error {
	return h.writeFrame(conn, mustJSON(msg))
}

func (h *Handler) writeRaw(conn *websocket.Conn, payload []byte) bool {
	return h.writeFrame(conn, payload) == nil
}

func (h *Handler) writeFrame(conn *websocket.Conn, payload []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(h.writeWait)); err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		h.logger.Debug("ws: client write failed", "error", err)
		return err
	}
	return nil
}

func mustJSON(msg Message) []byte {
	payload, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return payload
}

func pumpRequests(receive func(*clientRequest) error, incoming chan<- clientRequest, stop <-chan struct{}) {
	for {
		var req clientRequest
		if err := receive(&req); err != nil {
			return
		}
		select {
		case incoming <- req:
		case <-stop:
			return
		}
	}
}

type userIDKey struct{}

type authDecision struct {
	allowed bool
	expiry  time.Time
}

func (h *Handler) channelAllowed(ctx context.Context, channel string, userID uuid.UUID, authorized map[uuid.UUID]authDecision) bool {
	if h.channelAuthorize != nil {
		return h.channelAuthorize(ctx, channel, userID) == nil
	}
	if h.authorize == nil {
		return true
	}
	if projectID, ok := taskChannelProjectID(channel); ok {
		if h.taskAuthorize == nil {
			return true
		}
		if err := h.taskAuthorize(ctx, projectID, userID); err != nil {
			h.logger.Debug("ws: task subscription denied",
				"channel", channel, "user_id", userID.String(), "error", err)
			return false
		}
		return true
	}
	serverID, ok := logChannelServerID(channel)
	if !ok {
		return true
	}
	if decision, seen := authorized[serverID]; seen && time.Now().Before(decision.expiry) {
		return decision.allowed
	}
	if err := h.authorize(ctx, serverID, userID); err != nil {
		h.logger.Debug("ws: subscription denied",
			"channel", channel, "user_id", userID.String(), "error", err)
		delete(authorized, serverID)
		return false
	}
	authorized[serverID] = authDecision{allowed: true, expiry: time.Now().Add(h.authCacheTTL)}
	return true
}

func taskChannelProjectID(channel string) (string, bool) {
	if !strings.HasPrefix(channel, "tasks:") {
		return "", false
	}
	projectID := strings.TrimSpace(strings.TrimPrefix(channel, "tasks:"))
	if projectID == "" {
		return "", false
	}
	return projectID, true
}

func logChannelServerID(channel string) (uuid.UUID, bool) {
	parts := strings.SplitN(channel, ":", 3)
	if len(parts) != 3 || parts[0] != "logs" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimSpace(parts[1]))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func initialChannels(r *http.Request) []string {
	query := r.URL.Query()
	if channels := query["channel"]; len(channels) > 0 {
		out := make([]string, 0, len(channels))
		for _, channel := range channels {
			if strings.TrimSpace(channel) != "" {
				out = append(out, channel)
			}
		}
		return out
	}
	serverID := strings.TrimSpace(query.Get("server"))
	containerID := strings.TrimSpace(query.Get("container"))
	if serverID != "" && containerID != "" {
		return []string{LogChannel(serverID, containerID)}
	}
	return nil
}

func (h *Handler) subscriptionsAllowed(ctx context.Context, userID uuid.UUID, subs map[string]bool) bool {
	for channel := range subs {
		if !h.channelAllowed(ctx, channel, userID, make(map[uuid.UUID]authDecision)) {
			return false
		}
	}
	return true
}
