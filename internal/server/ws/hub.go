package ws

import (
	"sync"
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
)

// Client is one WebSocket connection attached to the hub.
type Client struct {
	hub  *Hub
	send chan []byte

	mu       sync.Mutex
	channels map[string]bool
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
				select {
				case c.send <- b.payload:
				default:
					// Slow client: drop the message rather than blocking the
					// whole room.
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Close stops the hub. Publish after Close is a no-op.
func (h *Hub) Close() {
	h.once.Do(func() { close(h.done) })
}

// newClient attaches a connection send queue to the hub.
func (h *Hub) newClient() *Client {
	c := &Client{hub: h, send: make(chan []byte, 64), channels: make(map[string]bool)}
	h.register <- c
	return c
}

// remove detaches a client, releasing its room memberships.
func (h *Hub) remove(c *Client) {
	h.unregister <- c
}

// subscribe adds the client to a channel.
func (h *Hub) subscribe(c *Client, channel string) {
	h.subCh <- subscription{client: c, channel: channel, subscribe: true}
}

// unsubscribe removes the client from a channel.
func (h *Hub) unsubscribe(c *Client, channel string) {
	h.subCh <- subscription{client: c, channel: channel, subscribe: false}
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
		// Hub saturated: drop rather than block log streaming.
	}
}

// Subscribers reports how many clients listen on channel.
func (h *Hub) Subscribers(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[channel])
}
