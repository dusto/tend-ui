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

func TestOnNotifyBroadcastsEventPush(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	defer cancel()
	b := New("/repo", h)

	params, _ := json.Marshal(api.EventPushParams{Event: api.Event{Type: "approval_requested", Seq: 7}})
	b.onNotify("event.push", params)

	select {
	case ev := <-ch:
		if ev.Type != "approval_requested" || ev.Seq != 7 {
			t.Errorf("got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("event.push was not broadcast")
	}
}

func TestOnNotifyIgnoresOtherMethods(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	defer cancel()
	b := New("/repo", h)

	b.onNotify("prompt.raise", json.RawMessage(`{}`))
	select {
	case <-ch:
		t.Fatal("prompt.raise must not be broadcast on the workspace event hub")
	case <-time.After(50 * time.Millisecond):
	}
}
