package ws

import (
	"sync"
	"sync/atomic"
)

// Message is the JSON envelope exchanged with WebSocket clients.
type Message struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
}

// Message types sent from the server to clients.
const (
	TypeLog        = "log"
	TypeDisconnect = "disconnect"
	TypeSubscribed = "subscribed"
	// TypeDenied reports a subscription the server refused (the client never
	// joined the channel). An older client that does not know the type ignores
	// the frame, so adding it stays backward compatible.
	TypeDenied = "denied"
	// TypePing is the server heartbeat. It is a no-op for clients that do not
	// know the type; its purpose is to keep a write in flight so a dead peer
	// fails the connection's write deadline (see handler.serveConn).
	TypePing = "ping"
)

// clientSendBuffer bounds how many frames may queue for one connection before
// the hub starts dropping (and eventually disconnecting) a slow reader.
const clientSendBuffer = 64

// slowClientDropLimit is how many consecutive dropped frames mark a client as
// too slow to keep. A single successful delivery resets the run.
const slowClientDropLimit = 8

// Client is one WebSocket connection attached to the hub.
type Client struct {
	hub  *Hub
	send chan []byte
	// kick is closed once when the hub decides the client must go (slow reader
	// or hub shutdown). serveConn selects on it and releases the connection.
	kick     chan struct{}
	kickOnce sync.Once
	// dropRun counts consecutive broadcast drops for this client. Only the hub
	// goroutine touches it, so it needs no lock.
	dropRun int

	mu       sync.Mutex
	channels map[string]bool
}

// kickClient signals serveConn to stop. Safe to call more than once.
func (c *Client) kickClient() {
	c.kickOnce.Do(func() { close(c.kick) })
}

// deliver enqueues payload without blocking. It reports false when the send
// buffer is full, in which case the caller counts a drop.
func (c *Client) deliver(payload []byte) bool {
	select {
	case c.send <- payload:
		c.dropRun = 0
		return true
	default:
		c.dropRun++
		if c.dropRun >= slowClientDropLimit {
			c.kickClient()
		}
		return false
	}
}

// Hub fans out published payloads to subscribed clients. The zero value is
// not usable; build hubs with NewHub and drive them with Run.
type Hub struct {
	register   chan *Client
	unregister chan *Client
	subCh      chan subscription
	broadcast  chan broadcast

	mu      sync.RWMutex
	clients map[*Client]struct{}
	rooms   map[string]map[*Client]struct{}

	// dropped counts frames the hub discarded because a client's send buffer
	// was full. It makes silent drops observable (and testable).
	dropped atomic.Uint64

	done chan struct{}
	once sync.Once
}

type subscription struct {
	client    *Client
	channel   string
	subscribe bool
}

type broadcast struct {
	channel string
	payload []byte
}

// NewHub builds an idle hub; call Run to start it.
func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		subCh:      make(chan subscription),
		broadcast:  make(chan broadcast, 256),
		clients:    make(map[*Client]struct{}),
		rooms:      make(map[string]map[*Client]struct{}),
		done:       make(chan struct{}),
	}
}

// Run drives the hub until Close. It owns the client and room sets.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			// Wake every attached connection so no serveConn goroutine survives
			// the hub. Closing kick (not send) keeps a concurrent broadcast
			// from ever sending on a closed channel.
			h.mu.Lock()
			for c := range h.clients {
				c.kickClient()
			}
			h.mu.Unlock()
			return
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, c)
			for channel, members := range h.rooms {
				delete(members, c)
				if len(members) == 0 {
					delete(h.rooms, channel)
				}
			}
			h.mu.Unlock()
			close(c.send)
		case s := <-h.subCh:
			h.mu.Lock()
			if s.subscribe {
				members, ok := h.rooms[s.channel]
				if !ok {
					members = make(map[*Client]struct{})
					h.rooms[s.channel] = members
				}
				members[s.client] = struct{}{}
				s.client.mu.Lock()
				s.client.channels[s.channel] = true
				s.client.mu.Unlock()
			} else {
				if members, ok := h.rooms[s.channel]; ok {
					delete(members, s.client)
					if len(members) == 0 {
						delete(h.rooms, s.channel)
					}
				}
				s.client.mu.Lock()
				delete(s.client.channels, s.channel)
				s.client.mu.Unlock()
			}
			h.mu.Unlock()
		case b := <-h.broadcast:
			h.mu.RLock()
			members := h.rooms[b.channel]
			for c := range members {
				if !c.deliver(b.payload) {
					h.dropped.Add(1)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Close stops the hub and wakes every attached connection. Publish after
// Close is a no-op.
func (h *Hub) Close() {
	h.once.Do(func() { close(h.done) })
}

// newClient attaches a connection send queue to the hub.
func (h *Hub) newClient() *Client {
	c := &Client{
		hub:      h,
		send:     make(chan []byte, clientSendBuffer),
		kick:     make(chan struct{}),
		channels: make(map[string]bool),
	}
	select {
	case h.register <- c:
	case <-h.done:
	}
	return c
}

// remove detaches a client, releasing its room memberships. It never blocks
// once the hub has shut down, so a connection finishing during shutdown cannot
// deadlock (B1-9).
func (h *Hub) remove(c *Client) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

// subscribe adds the client to a channel.
func (h *Hub) subscribe(c *Client, channel string) {
	select {
	case h.subCh <- subscription{client: c, channel: channel, subscribe: true}:
	case <-h.done:
	}
}

// unsubscribe removes the client from a channel.
func (h *Hub) unsubscribe(c *Client, channel string) {
	select {
	case h.subCh <- subscription{client: c, channel: channel, subscribe: false}:
	case <-h.done:
	}
}

// Broadcast publishes payload to every client subscribed to channel. It is
// safe for concurrent use and lets in-memory publishers bypass Redis, which is
// also the REALTIME_ENABLED=false fallback path.
func (h *Hub) Broadcast(channel string, payload []byte) {
	select {
	case <-h.done:
		return
	default:
	}
	select {
	case h.broadcast <- broadcast{channel: channel, payload: payload}:
	case <-h.done:
	default:
		// Hub saturated: drop rather than block log streaming, but make the
		// discard observable (U4).
		h.dropped.Add(1)
	}
}

// Drops reports how many frames were dropped for slow clients. Exposed so the
// condition is observable (metrics/tests) rather than silent.
func (h *Hub) Drops() uint64 {
	return h.dropped.Load()
}

// Subscribers reports how many clients listen on channel.
func (h *Hub) Subscribers(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[channel])
}
