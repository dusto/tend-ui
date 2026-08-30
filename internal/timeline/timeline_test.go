package timeline

import (
	"encoding/json"
	"strings"
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

func TestUsageAccumulatesAndResetsOnSwitch(t *testing.T) {
	tl := New("/repo", bridge.NewHub[string]())
	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "e1")
	resub := make(chan struct{}, 1)
	push := func(typ string, payload any) {
		p, _ := json.Marshal(api.EventPushParams{Event: api.Event{
			StreamID: "session:ses-1", Type: typ, Payload: mustJSON(payload),
		}})
		tl.onNotify("event.push", p, resub)
	}

	push("agent_context_usage", api.AgentContextUsage{UsedTokens: 124800, WindowTokens: 200000})
	push("agent_token_usage", api.AgentTokenUsage{InputTokens: 8200, OutputTokens: 1100, TotalTokens: 9300})
	push("agent_token_usage", api.AgentTokenUsage{InputTokens: 500, OutputTokens: 200, TotalTokens: 700})
	push("agent_prompt_usage", api.AgentPromptUsage{TokensApprox: 6400, Approximate: true})

	u := tl.Usage()
	if !u.HasContext || u.ContextPercent() != 62 {
		t.Errorf("context = %+v, want 62%%", u)
	}
	// Per-turn tokens are latest-event-wins (the second turn), not a cumulative.
	if u.LastInput != 500 || u.LastOutput != 200 || u.LastTotal != 700 {
		t.Errorf("last turn = %d/%d/%d, want 500/200/700", u.LastInput, u.LastOutput, u.LastTotal)
	}
	if !u.HasPrompt || u.PromptApprox != 6400 {
		t.Errorf("prompt = %+v", u)
	}

	// Switching sessions resets the accounting.
	tl.setStream(api.SessionInfo{SessionID: "ses-2", StreamID: "session:ses-2"}, "e1")
	if u := tl.Usage(); u.HasContext || u.HasToken {
		t.Errorf("usage not reset on switch: %+v", u)
	}
}

func TestSubscriptionClosedFiltersByStream(t *testing.T) {
	tl := New("/repo", bridge.NewHub[string]())
	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "e1")
	resub := make(chan struct{}, 1)
	closeOf := func(stream api.StreamID) {
		p, _ := json.Marshal(api.SubscriptionClosedParams{StreamID: stream, Reason: "overflow"})
		tl.onNotify("event.subscription_closed", p, resub)
	}

	// A close for a stream we switched away from must NOT trigger a re-subscribe.
	closeOf("session:ses-OLD")
	select {
	case <-resub:
		t.Fatal("stale-stream close triggered a re-subscribe")
	default:
	}

	// A close for the current stream does.
	closeOf("session:ses-1")
	select {
	case <-resub:
	default:
		t.Fatal("current-stream close did not trigger a re-subscribe")
	}
}

func TestToolTrackingAndStatusUpdate(t *testing.T) {
	hub := bridge.NewHub[string]()
	out, cancel := hub.Subscribe()
	defer cancel()
	tl := New("/repo", hub)
	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "e1")
	<-out // drain clear frame

	resub := make(chan struct{}, 1)
	push := func(typ string, payload any) {
		p, _ := json.Marshal(api.EventPushParams{Event: api.Event{
			StreamID: "session:ses-1", Type: typ, Payload: mustJSON(payload),
		}})
		tl.onNotify("event.push", p, resub)
	}

	push("tool_call", api.ToolCall{ToolCallID: "t1", Name: "edit_buffer", RawInput: mustJSON(map[string]string{"uri": "file:///repo/a.go"})})
	card := <-out // the coalescer's tool-call card
	// The initial embedded chip must NOT be an OOB swap, or htmx drops it when the
	// card is appended, leaving later status updates with no target.
	if strings.Contains(card, "hx-swap-oob") {
		t.Errorf("initial tool card chip must not carry hx-swap-oob: %q", card)
	}
	if !strings.Contains(card, `id="tcs-t1"`) {
		t.Errorf("initial card missing the status target: %q", card)
	}

	tools := tl.ToolCalls()
	if len(tools) != 1 || tools[0].Name != "edit_buffer" || tools[0].Kind != "edit" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].Arg != "/repo/a.go" || tools[0].Status != "running" {
		t.Errorf("tool arg/status = %q/%q", tools[0].Arg, tools[0].Status)
	}

	// A status update sets the ref status and pushes an OOB status swap.
	push("tool_call_update", api.ToolCallUpdate{ToolCallID: "t1", Status: "completed"})
	select {
	case frame := <-out:
		if !strings.Contains(frame, `id="tcs-t1"`) || !strings.Contains(frame, "completed") || !strings.Contains(frame, "hx-swap-oob") {
			t.Errorf("status OOB frame wrong: %q", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("tool_call_update did not broadcast a status swap")
	}
	if tl.ToolCalls()[0].Status != "completed" {
		t.Errorf("status not updated: %+v", tl.ToolCalls())
	}

	// Switching sessions clears the tool list.
	tl.setStream(api.SessionInfo{SessionID: "ses-2", StreamID: "session:ses-2"}, "e1")
	if len(tl.ToolCalls()) != 0 {
		t.Errorf("tools not reset on switch: %+v", tl.ToolCalls())
	}
}

func TestToolCallUpdateRefinesInput(t *testing.T) {
	hub := bridge.NewHub[string]()
	out, cancel := hub.Subscribe()
	defer cancel()
	tl := New("/repo", hub)
	tl.setStream(api.SessionInfo{SessionID: "ses-1", StreamID: "session:ses-1"}, "e1")
	<-out // drain clear frame

	resub := make(chan struct{}, 1)
	push := func(typ string, payload any) {
		p, _ := json.Marshal(api.EventPushParams{Event: api.Event{
			StreamID: "session:ses-1", Type: typ, Payload: mustJSON(payload),
		}})
		tl.onNotify("event.push", p, resub)
	}

	// The provider opens the tool_call with an EMPTY input (args not streamed yet).
	push("tool_call", api.ToolCall{ToolCallID: "t1", Name: "Write", RawInput: mustJSON(map[string]any{})})
	<-out // the coalescer's tool-call card
	if got := tl.ToolCalls()[0].Arg; got != "" {
		t.Fatalf("initial arg should be empty, got %q", got)
	}

	// A refine update carries the populated input, often with an empty status.
	push("tool_call_update", api.ToolCallUpdate{
		ToolCallID: "t1", Status: "",
		RawInput: mustJSON(map[string]any{"file_path": "/x.go", "content": "package x"}),
	})
	select {
	case frame := <-out:
		// The refine OOB-swaps the arg and input-detail targets, and must NOT blank
		// the status chip (no status target in this frame).
		if !strings.Contains(frame, `id="tca-t1"`) || !strings.Contains(frame, "hx-swap-oob") {
			t.Errorf("refine frame missing arg OOB swap: %q", frame)
		}
		if !strings.Contains(frame, `id="tcd-t1"`) || !strings.Contains(frame, "file_path") {
			t.Errorf("refine frame missing input detail: %q", frame)
		}
		if strings.Contains(frame, `id="tcs-t1"`) {
			t.Errorf("refine must not touch the status chip: %q", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("tool_call_update did not broadcast a refine swap")
	}
	ref := tl.ToolCalls()[0]
	if ref.Arg != "/x.go" {
		t.Errorf("refined arg = %q, want /x.go", ref.Arg)
	}
	if !strings.Contains(ref.Full, "file_path") {
		t.Errorf("refined full input not stored: %q", ref.Full)
	}
	if ref.Status != "running" {
		t.Errorf("empty-status refine must not change status, got %q", ref.Status)
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
