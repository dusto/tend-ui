package timeline

import (
	"testing"

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
