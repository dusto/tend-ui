package bridge

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dusto/tend/api"
)

func TestHubBroadcastDelivers(t *testing.T) {
	h := NewHub()
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
	h := NewHub()
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
	h := NewHub()
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
	h := NewHub()
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
	b := New("/repo", NewHub())
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
	h := NewHub()
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
