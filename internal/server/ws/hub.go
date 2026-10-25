package ws

import (
	"sync"
	"sync/atomic"
)

type Message struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Replay  bool   `json:"replay,omitempty"`
}

const (
	TypeLog        = "log"
	TypeDisconnect = "disconnect"
	TypeSubscribed = "subscribed"
	TypeDenied     = "denied"
	TypeResumed    = "resumed"
	TypeReplayEnd  = "replay_end"
	TypePing       = "ping"
)

const clientSendBuffer = 64

const slowClientDropLimit = 8

type Client struct {
	hub        *Hub
	send       chan []byte
	kick       chan struct{}
	revalidate chan struct{}
	kickOnce   sync.Once
	dropRun    int

	mu       sync.Mutex
	channels map[string]bool
	userID   string
}

func (c *Client) kickClient() {
	c.kickOnce.Do(func() { close(c.kick) })
}

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

type Hub struct {
	register   chan *Client
	unregister chan *Client
	subCh      chan subscription
	broadcast  chan broadcast

	mu      sync.RWMutex
	clients map[*Client]struct{}
	rooms   map[string]map[*Client]struct{}

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

func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
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

func (h *Hub) Close() {
	h.once.Do(func() { close(h.done) })
}

func (h *Hub) newClient() *Client {
	c := &Client{
		hub:        h,
		send:       make(chan []byte, clientSendBuffer),
		kick:       make(chan struct{}),
		revalidate: make(chan struct{}, 1),
		channels:   make(map[string]bool),
	}
	select {
	case h.register <- c:
	case <-h.done:
	}
	return c
}

func (h *Hub) remove(c *Client) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

func (h *Hub) subscribe(c *Client, channel string) {
	select {
	case h.subCh <- subscription{client: c, channel: channel, subscribe: true}:
	case <-h.done:
	}
}

func (h *Hub) unsubscribe(c *Client, channel string) {
	select {
	case h.subCh <- subscription{client: c, channel: channel, subscribe: false}:
	case <-h.done:
	}
}

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
		h.dropped.Add(1)
	}
}

func (h *Hub) Drops() uint64 {
	return h.dropped.Load()
}

func (h *Hub) Subscribers(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[channel])
}
