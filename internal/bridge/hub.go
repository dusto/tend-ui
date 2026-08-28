// Package bridge connects tend-ui to the tend daemon: it dials the socket via
// the shared client, subscribes to a workspace's event stream, and fans the
// events out to the UI's SSE endpoint. tendd stays a pure protocol daemon — this
// is the adapter between its JSON-RPC event bus and the browser (see ADR 0005).
package bridge

import (
	"sync"

	"github.com/dusto/tend/api"
)

// Hub fans daemon events out to browser SSE subscribers. It is the boundary
// between the single daemon pump (Broadcast, on the connection's read goroutine)
// and the N browser connections (each a Subscribe). Safe for concurrent use.
type Hub struct {
	mu   sync.Mutex
	subs map[chan api.Event]struct{}
}

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{subs: make(map[chan api.Event]struct{})} }

// Subscribe registers a subscriber and returns its channel plus a cancel func
// that removes and closes it. The channel is buffered; a subscriber that falls
// behind drops events (see Broadcast) rather than stalling the daemon pump.
func (h *Hub) Subscribe() (<-chan api.Event, func()) {
	ch := make(chan api.Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Broadcast delivers ev to every subscriber, non-blocking: a subscriber whose
// buffer is full is skipped for this event so a slow browser cannot block the
// daemon connection.
func (h *Hub) Broadcast(ev api.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
