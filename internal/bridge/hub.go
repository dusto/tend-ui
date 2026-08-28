// Package bridge connects tend-ui to the tend daemon: it dials the socket via
// the shared client, subscribes to event streams, and fans the events out to the
// UI's SSE endpoints. tendd stays a pure protocol daemon — this is the adapter
// between its JSON-RPC event bus and the browser (see ADR 0005).
package bridge

import "sync"

// Hub fans values out to browser SSE subscribers. It is the boundary between a
// single producer (Broadcast, on a connection's read goroutine) and the N
// browser connections (each a Subscribe). Safe for concurrent use. The element
// type is what a producer publishes — raw daemon events for the workspace log,
// rendered HTML fragments for the session timeline.
type Hub[T any] struct {
	mu   sync.Mutex
	subs map[chan T]struct{}
}

// NewHub returns an empty Hub.
func NewHub[T any]() *Hub[T] { return &Hub[T]{subs: make(map[chan T]struct{})} }

// Subscribe registers a subscriber and returns its channel plus a cancel func
// that removes and closes it. The channel is buffered; a subscriber that falls
// behind drops values (see Broadcast) rather than stalling the producer.
func (h *Hub[T]) Subscribe() (<-chan T, func()) {
	ch := make(chan T, 64)
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

// Broadcast delivers v to every subscriber, non-blocking: a subscriber whose
// buffer is full is skipped for this value so a slow browser cannot block the
// daemon connection.
func (h *Hub[T]) Broadcast(v T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- v:
		default:
		}
	}
}
