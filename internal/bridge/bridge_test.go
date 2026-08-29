package bridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"
	"github.com/dusto/tend/client/clienttest"
)

// dialFake dials a clienttest daemon and returns the connection.
func dialFake(t *testing.T, srv *clienttest.Server) *client.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	conn, err := client.Dial(ctx, client.Options{Socket: srv.Socket(), ClientID: "test", MinPluginToDaemon: "0.8.0"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// On a compacted cursor, subscribe resumes at the daemon-provided summary
// boundary rather than replaying from 0.
func TestSubscribeResumesAtCompactionBoundary(t *testing.T) {
	srv := clienttest.New(t)
	var lastReqSeq uint64
	srv.Handle("events.subscribe", func(params json.RawMessage) (any, error) {
		var p api.EventsSubscribeParams
		_ = json.Unmarshal(params, &p)
		lastReqSeq = p.LastSeq
		if p.LastSeq != 0 && p.LastSeq != 42 {
			// The stale cursor (100) is compacted; only 0 or the boundary (42) ok.
			return nil, clienttest.ErrorData(api.ErrCursorCompacted, "compacted",
				api.CursorCompactedData{StreamID: "workspace:ws-1", BoundarySeq: 42})
		}
		return api.EventsSubscribeResult{}, nil
	})

	b := New("/repo", NewHub[api.Event]())
	b.lastSeq.Store(100) // a cursor the daemon has compacted away

	conn := dialFake(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.subscribe(ctx, conn, "workspace:ws-1"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if lastReqSeq != 42 {
		t.Errorf("resubscribed at seq %d, want the boundary 42", lastReqSeq)
	}
	if b.lastSeq.Load() != 42 {
		t.Errorf("cursor = %d, want 42 after compaction resume", b.lastSeq.Load())
	}
}

func TestHubBroadcastDelivers(t *testing.T) {
	h := NewHub[api.Event]()
	ch, cancel := h.Subscribe()
	defer cancel()

	h.Broadcast(api.Event{Type: "provider_started", Seq: 3})
	select {
	case ev := <-ch:
		if ev.Type != "provider_started" || ev.Seq != 3 {
			t.Errorf("got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered")
	}
}

func TestHubCancelClosesAndIsIdempotent(t *testing.T) {
	h := NewHub[api.Event]()
	ch, cancel := h.Subscribe()
	cancel()
	if _, open := <-ch; open {
		t.Error("channel should be closed after cancel")
	}
	// Broadcasting with no subscribers and a second cancel must not panic.
	h.Broadcast(api.Event{})
	cancel()
}

func TestHubBroadcastDropsForSlowSubscriber(t *testing.T) {
	h := NewHub[api.Event]()
	_, cancel := h.Subscribe() // never drained
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			h.Broadcast(api.Event{Seq: uint64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked on a full subscriber (should drop)")
	}
}

func TestOnNotifyBroadcastsAndAdvancesCursor(t *testing.T) {
	h := NewHub[api.Event]()
	ch, cancel := h.Subscribe()
	defer cancel()
	b := New("/repo", h)
	resub := make(chan struct{}, 1)

	params, _ := json.Marshal(api.EventPushParams{
		Event: api.Event{Type: "approval_requested", Seq: 7, CursorSeq: 9},
	})
	b.onNotify("event.push", params, resub)

	select {
	case ev := <-ch:
		if ev.Type != "approval_requested" || ev.Seq != 7 {
			t.Errorf("got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("event.push was not broadcast")
	}
	// The cursor advances to CursorSeq so a resume does not re-deliver it.
	if got := b.lastSeq.Load(); got != 9 {
		t.Errorf("lastSeq = %d, want 9 (CursorSeq)", got)
	}
}

func TestOnNotifySubscriptionClosedSignalsResub(t *testing.T) {
	b := New("/repo", NewHub[api.Event]())
	resub := make(chan struct{}, 1)

	b.onNotify("event.subscription_closed", json.RawMessage(`{}`), resub)
	select {
	case <-resub:
	case <-time.After(time.Second):
		t.Fatal("subscription_closed did not signal a re-subscribe")
	}

	// The signal is non-blocking even when one is already pending.
	b.onNotify("event.subscription_closed", json.RawMessage(`{}`), resub)
	b.onNotify("event.subscription_closed", json.RawMessage(`{}`), resub)
}

func TestOnNotifyIgnoresOtherMethods(t *testing.T) {
	h := NewHub[api.Event]()
	ch, cancel := h.Subscribe()
	defer cancel()
	b := New("/repo", h)
	resub := make(chan struct{}, 1)

	b.onNotify("prompt.raise", json.RawMessage(`{}`), resub)
	select {
	case <-ch:
		t.Fatal("prompt.raise must not be broadcast on the workspace event hub")
	case <-resub:
		t.Fatal("prompt.raise must not trigger a re-subscribe")
	case <-time.After(50 * time.Millisecond):
	}
}
