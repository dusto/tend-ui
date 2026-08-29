package timeline

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/internal/bridge"
)

func TestSelectSetsSelectionAndSignals(t *testing.T) {
	tl := New("/repo", bridge.NewHub[string]())

	tl.Select("ses-7")
	if p := tl.selected.Load(); p == nil || *p != "ses-7" {
		t.Fatalf("selected = %v, want ses-7", p)
	}
	// The switch signal is buffered (non-blocking) and coalesces.
	select {
	case <-tl.switchCh:
	default:
		t.Fatal("Select did not signal the switch channel")
	}
	tl.Select("ses-8")
	tl.Select("ses-9") // second signal must not block on the full channel
	if p := tl.selected.Load(); p == nil || *p != "ses-9" {
		t.Errorf("selected = %v, want ses-9", p)
	}
}

func TestCurrentReflectsSetStream(t *testing.T) {
	tl := New("/repo", bridge.NewHub[string]())
	if tl.Current() != "" {
		t.Errorf("Current = %q, want empty before any stream", tl.Current())
	}
	tl.setStream(api.SessionInfo{SessionID: "ses-3", StreamID: "session:ses-3"}, "epoch-1")
	if tl.Current() != "ses-3" {
		t.Errorf("Current = %q, want ses-3", tl.Current())
	}
}

func TestOnNotifyDropsEventsFromOtherStreams(t *testing.T) {
	hub := bridge.NewHub[string]()
	out, cancel := hub.Subscribe()
	defer cancel()
	tl := New("/repo", hub)
	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "e1")
	// Drain the clear frame from the switch to ses-1.
	<-out

	resub := make(chan struct{}, 1)
	push := func(stream api.StreamID, cursor uint64, typ string) {
		p, _ := json.Marshal(api.EventPushParams{Event: api.Event{
			StreamID: stream, CursorSeq: cursor, Type: typ,
			Payload: mustJSON(api.AgentError{Message: typ}),
		}})
		tl.onNotify("event.push", p, resub)
	}

	// An event from a DIFFERENT stream (an old subscription's in-flight frame) is
	// dropped: no block rendered, cursor not advanced.
	push("session:ses-OLD", 999, "agent_error")
	select {
	case b := <-out:
		t.Fatalf("event from another stream leaked into the timeline: %q", b)
	case <-time.After(50 * time.Millisecond):
	}
	if tl.lastSeq.Load() != 0 {
		t.Fatalf("cursor advanced to %d from a foreign-stream event", tl.lastSeq.Load())
	}

	// An event for the current stream is rendered and advances the cursor.
	push("session:ses-1", 7, "agent_error")
	select {
	case <-out:
	case <-time.After(time.Second):
		t.Fatal("current-stream event was not rendered")
	}
	if tl.lastSeq.Load() != 7 {
		t.Errorf("cursor = %d, want 7", tl.lastSeq.Load())
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestSetStreamClearsBrowserOnSwitch(t *testing.T) {
	hub := bridge.NewHub[string]()
	ch, cancel := hub.Subscribe()
	defer cancel()
	tl := New("/repo", hub)

	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "epoch-1")
	select {
	case frame := <-ch:
		if frame != clearFrame {
			t.Errorf("first setStream frame = %q, want clearFrame", frame)
		}
	default:
		t.Fatal("switching to a new stream should broadcast a clear frame")
	}

	// Re-selecting the same stream must NOT clear again.
	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "epoch-1")
	select {
	case <-ch:
		t.Fatal("same-stream setStream should not broadcast a clear frame")
	default:
	}
}
