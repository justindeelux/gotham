package ws

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
)

// TokenVerifier validates the query-string token. It mirrors
// AuthService.VerifyAccessToken so the handler reuses the JWT validation from
// internal/server/auth.go instead of inventing a new scheme.
type TokenVerifier interface {
	VerifyAccessToken(token string) (*auth.Claims, error)
}

// RealtimeEnv is the flag disabling Redis pub/sub in favour of the poll
// fallback.
const RealtimeEnv = "REALTIME_ENABLED"

// RealtimeEnabled reports whether the Redis realtime path is active. Only an
// explicit REALTIME_ENABLED=false disables it; anything else (including unset)
// keeps it enabled.
func RealtimeEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(RealtimeEnv)), "false")
}

// SubscriptionAuthorizer authorizes one log subscription for the authenticated
// user. Returning an error refuses the channel (the client gets a "denied"
// frame and never joins the room). A nil authorizer keeps the pre-teams
// behavior for tests and non-DB builds.
type SubscriptionAuthorizer func(ctx context.Context, serverID, userID uuid.UUID) error

// Handler serves the WS /api/v1/ws endpoint.
type Handler struct {
	hub       *Hub
	verify    TokenVerifier
	authorize SubscriptionAuthorizer
	logger    *slog.Logger
}

// NewHandler builds a handler bound to hub. verify may be nil, in which case
// every connection is rejected with 401. authorize may be nil (no per-channel
// authorization).
func NewHandler(hub *Hub, verifier TokenVerifier, logger *slog.Logger, authorize SubscriptionAuthorizer) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{hub: hub, verify: verifier, authorize: authorize, logger: logger}
}

// Mount registers WS /api/v1/ws on the /api router, starts the hub and —
// unless REALTIME_ENABLED=false — the Redis pub/sub bridge on
// logs:{serverID}:{containerID} (config key redis.addr). authorize gates every
// log subscription on the node's team; nil leaves subscriptions open. It
// returns the hub so publishers can broadcast without Redis. Mount is intended
// as a one-line call from Server.routes.
func Mount(api chi.Router, verifier TokenVerifier, redisAddr string, logger *slog.Logger, authorize SubscriptionAuthorizer) *Hub {
	hub := NewHub()
	go hub.Run()

	if logger == nil {
		logger = slog.Default()
	}

	if RealtimeEnabled() && strings.TrimSpace(redisAddr) != "" {
		rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
		bridge := NewBridge(hub, rdb)
		go func() {
			if err := bridge.Run(context.Background()); err != nil && err != context.Canceled {
				logger.Warn("ws: redis bridge stopped", "error", err)
			}
		}()
	} else {
		logger.Info("ws: realtime bridge disabled, clients should poll")
	}

	handler := NewHandler(hub, verifier, logger, authorize)
	api.Get("/v1/ws", handler.ServeHTTP)
	return hub
}

// ServeHTTP authenticates the query token, then upgrades to WebSocket.
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
	claims, err := h.verify.VerifyAccessToken(token)
	if err != nil {
		writeUnauth(w)
		return
	}

	// The authenticated user travels in the request context through the
	// upgrade, so subscription frames can be authorized per channel.
	if userID, parseErr := uuid.Parse(claims.Subject); parseErr == nil {
		r = r.WithContext(context.WithValue(r.Context(), userIDKey{}, userID))
	}
	websocket.Handler(h.serveConn).ServeHTTP(w, r)
}

func writeUnauth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
}

// clientRequest is a control frame sent by the client after connecting.
type clientRequest struct {
	Subscribe   string `json:"subscribe"`
	Unsubscribe string `json:"unsubscribe"`
}

// serveConn runs one upgraded connection: optional ?channel= (repeatable) or
// ?server=…&container=… preselects subscriptions, then {"subscribe":…} /
// {"unsubscribe":…} frames adjust them while send pumps hub messages.
func (h *Handler) serveConn(conn *websocket.Conn) {
	defer func() { _ = conn.Close() }()

	request := conn.Request()
	userID, _ := request.Context().Value(userIDKey{}).(uuid.UUID)

	client := h.hub.newClient()
	defer h.hub.remove(client)

	for _, channel := range initialChannels(request) {
		if !h.allowChannel(request.Context(), channel, userID) {
			_ = websocket.JSON.Send(conn, Message{Channel: channel, Type: TypeDenied})
			continue
		}
		h.hub.subscribe(client, channel)
		_ = websocket.JSON.Send(conn, Message{Channel: channel, Type: TypeSubscribed})
	}

	incoming := make(chan clientRequest, 8)
	go func() {
		defer close(incoming)
		for {
			var req clientRequest
			if err := websocket.JSON.Receive(conn, &req); err != nil {
				return
			}
			incoming <- req
		}
	}()

	for {
		select {
		case payload, ok := <-client.send:
			if !ok {
				return
			}
			if _, err := conn.Write(payload); err != nil {
				return
			}
		case req, ok := <-incoming:
			if !ok {
				return
			}
			switch {
			case req.Subscribe != "":
				if !h.allowChannel(request.Context(), req.Subscribe, userID) {
					_ = websocket.JSON.Send(conn, Message{Channel: req.Subscribe, Type: TypeDenied})
					continue
				}
				h.hub.subscribe(client, req.Subscribe)
				_ = websocket.JSON.Send(conn, Message{Channel: req.Subscribe, Type: TypeSubscribed})
			case req.Unsubscribe != "":
				h.hub.unsubscribe(client, req.Unsubscribe)
			}
		}
	}
}

// userIDKey is the context key carrying the authenticated user through the
// WebSocket upgrade.
type userIDKey struct{}

// allowChannel authorizes one subscription: only logs:{serverID}:{containerID}
// channels reach a node, and every other channel is a pure client-side room
// name. A nil authorizer (tests, non-DB builds) allows everything, matching the
// pre-teams compatibility paths elsewhere.
func (h *Handler) allowChannel(ctx context.Context, channel string, userID uuid.UUID) bool {
	if h.authorize == nil {
		return true
	}
	serverID, ok := logChannelServerID(channel)
	if !ok {
		return true
	}
	if err := h.authorize(ctx, serverID, userID); err != nil {
		h.logger.Debug("ws: subscription denied",
			"channel", channel, "user_id", userID.String(), "error", err)
		return false
	}
	return true
}

// logChannelServerID extracts the server ID from a logs:{serverID}:{containerID}
// channel. Channels of any other shape have no server to authorize.
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

// initialChannels reads the preselected subscriptions from the query string:
// repeated ?channel=logs:s:c wins, else ?server=s&container=c builds one.
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
