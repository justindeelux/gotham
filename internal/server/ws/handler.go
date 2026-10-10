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

// Connection hardening defaults. writeWait bounds every frame write so a
// non-reading client fails instead of pinning the handler; pingPeriod keeps a
// write in flight so a dead peer is noticed even while the log is quiet.
const (
	writeWait                 = 10 * time.Second
	pingPeriod                = 25 * time.Second
	maxSubscriptionsPerClient = 32
	// authCacheTTL bounds how long an accepted node authorization is reused on
	// one connection. Denials are never cached, so a revoked user loses access
	// within the TTL and a newly granted user is admitted immediately (U1).
	authCacheTTL = 30 * time.Second
)

// SubscriptionAuthorizer authorizes one log subscription for the authenticated
// user. Returning an error refuses the channel (the client gets a "denied"
// frame and never joins the room). A nil authorizer keeps the pre-teams
// behavior for tests and non-DB builds.
type SubscriptionAuthorizer func(ctx context.Context, serverID, userID uuid.UUID) error

// TaskAuthorizer authorizes one background-task subscription for the
// authenticated user: teamID is the team suffix of a tasks:{teamID} channel.
// A nil authorizer allows everything, matching the pre-teams compatibility
// paths elsewhere.
type TaskAuthorizer func(ctx context.Context, teamID string, userID uuid.UUID) error

// Handler serves the WS /api/v1/ws endpoint.
type Handler struct {
	hub       *Hub
	verify    TokenVerifier
	authorize SubscriptionAuthorizer
	logger    *slog.Logger

	// taskAuthorize gates tasks:{teamID} subscriptions; taskSnapshot replays
	// the team's running tasks right after the join, so a fresh mount and a
	// reconnect restore state. Both are optional and set via SetTaskFeed.
	taskAuthorize TaskAuthorizer
	taskSnapshot  func(teamID string) []string

	// writeWait, pingPeriod and authCacheTTL are fields so tests can shorten
	// them; NewHandler sets the production defaults.
	writeWait    time.Duration
	pingPeriod   time.Duration
	authCacheTTL time.Duration
}

// NewHandler builds a handler bound to hub. verify may be nil, in which case
// every connection is rejected with 401. authorize may be nil (no per-channel
// authorization).
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

// SetTaskFeed wires background-task subscriptions: authorize gates
// tasks:{teamID} joins, snapshot replays the team's running tasks after each
// join (initial mount, navigation, reconnect). Either may be nil.
func (h *Handler) SetTaskFeed(authorize TaskAuthorizer, snapshot func(teamID string) []string) {
	h.taskAuthorize = authorize
	h.taskSnapshot = snapshot
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
	// A connection that upgraded after the hub shut down must not linger: Run
	// has returned, so nothing else will kick it (U9).
	select {
	case <-h.hub.done:
		return
	default:
	}

	// Per-connection state: the accepted subscriptions (also the cap), and the
	// time-bounded authorization result per node. Caching an allow means N
	// channels on one node cost one authorization instead of N DB reads
	// (B1-10); denials are not cached so they can be re-checked (U1).
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

	ticker := time.NewTicker(h.pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-client.send:
			if !ok {
				return
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
		case <-ticker.C:
			if !h.writeRaw(conn, mustJSON(Message{Type: TypePing})) {
				return
			}
		}
	}
}

// subscribeChannel validates and joins one channel, enforcing the
// per-connection subscription cap.
func (h *Handler) subscribeChannel(conn *websocket.Conn, client *Client, channel string, ctx context.Context, userID uuid.UUID, subs map[string]bool, authorized map[uuid.UUID]authDecision) {
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
	if !h.channelAllowed(ctx, channel, userID, authorized) {
		_ = h.writeMessage(conn, Message{Channel: channel, Type: TypeDenied})
		return
	}
	h.hub.subscribe(client, channel)
	subs[channel] = true
	// A task join replays the running snapshot so the progress card restores
	// after mount and reconnect. The frames go direct (not via the hub), so
	// only the joining client sees them.
	if teamID, ok := taskChannelTeamID(channel); ok && h.taskSnapshot != nil {
		for _, frame := range h.taskSnapshot(teamID) {
			_ = h.writeFrame(conn, []byte(frame))
		}
	}
	_ = h.writeMessage(conn, Message{Channel: channel, Type: TypeSubscribed})
}

// writeMessage writes one JSON frame under the connection's write deadline.
func (h *Handler) writeMessage(conn *websocket.Conn, msg Message) error {
	return h.writeFrame(conn, mustJSON(msg))
}

// writeRaw writes a pre-marshalled frame under the connection's write
// deadline. It reports false when the write failed or timed out.
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

// mustJSON marshals a frame; Message is always marshallable, so a failure is
// impossible and is panicked rather than swallowed.
func mustJSON(msg Message) []byte {
	payload, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return payload
}

// pumpRequests forwards frames from receive into incoming until receive fails
// or stop closes. The select on stop is what lets the reader exit
// deterministically when the main loop has already returned while the reader
// was blocked on a full incoming (B1-6).
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

// userIDKey is the context key carrying the authenticated user through the
// WebSocket upgrade.
type userIDKey struct{}

// authDecision is a cached allow for one node, valid until expiry.
type authDecision struct {
	allowed bool
	expiry  time.Time
}

// channelAllowed authorizes one subscription, reusing a per-connection,
// time-bounded cache of node decisions so repeat channels on one node do not
// re-read the DB. Only logs:{serverID}:{containerID} channels reach a node;
// every other channel is a pure client-side room name. A nil authorizer
// (tests, non-DB builds) allows everything, matching the pre-teams
// compatibility paths elsewhere.
//
// Denials are never cached (U1): a user removed from a team is re-checked and
// loses access within authCacheTTL, and a user added after a denial is admitted
// on the next attempt.
func (h *Handler) channelAllowed(ctx context.Context, channel string, userID uuid.UUID, authorized map[uuid.UUID]authDecision) bool {
	if h.authorize == nil {
		return true
	}
	if teamID, ok := taskChannelTeamID(channel); ok {
		if h.taskAuthorize == nil {
			return true
		}
		if err := h.taskAuthorize(ctx, teamID, userID); err != nil {
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

// taskChannelTeamID extracts the team ID from a tasks:{teamID} channel.
// Channels of any other shape are not task rooms.
func taskChannelTeamID(channel string) (string, bool) {
	if !strings.HasPrefix(channel, "tasks:") {
		return "", false
	}
	teamID := strings.TrimSpace(strings.TrimPrefix(channel, "tasks:"))
	if teamID == "" {
		return "", false
	}
	return teamID, true
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
