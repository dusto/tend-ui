package timeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dusto/tend/api"
)

// ev builds a session event of the given type carrying payload.
func ev(typ string, payload any) api.Event {
	raw, _ := json.Marshal(payload)
	return api.Event{Type: typ, Payload: raw}
}

// collect drives a coalescer through evs and returns the emitted block HTML.
func collect(evs ...api.Event) []string {
	var out []string
	c := newCoalescer(func(html string) { out = append(out, html) })
	for _, e := range evs {
		c.handle(e)
	}
	return out
}

func TestCoalesceMergesConsecutiveMessageChunks(t *testing.T) {
	// Three message chunks flush as ONE message block when a discrete event
	// (turn_end) follows.
	out := collect(
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "Hello, "}),
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "world"}),
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "!"}),
		ev("turn_end", api.TurnEnd{}),
	)
	if len(out) != 2 {
		t.Fatalf("blocks = %d, want 2 (one message + turn divider)\n%v", len(out), out)
	}
	if !strings.Contains(out[0], "Hello, world!") {
		t.Errorf("message block did not merge chunks: %q", out[0])
	}
	if !strings.Contains(out[0], "agent") {
		t.Errorf("first block is not an agent message: %q", out[0])
	}
	if !strings.Contains(out[1], "turn complete") {
		t.Errorf("second block is not the turn divider: %q", out[1])
	}
}

func TestCoalesceFlushesMessageBeforeToolCall(t *testing.T) {
	// A tool_call interrupts a message: the message flushes first, then the tool.
	out := collect(
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "let me read the file"}),
		ev("tool_call", api.ToolCall{Name: "read_buffer"}),
	)
	if len(out) != 2 {
		t.Fatalf("blocks = %d, want 2\n%v", len(out), out)
	}
	if !strings.Contains(out[0], "let me read the file") {
		t.Errorf("message not flushed before tool: %q", out[0])
	}
	if !strings.Contains(out[1], "read_buffer") {
		t.Errorf("tool block missing name: %q", out[1])
	}
}

func TestCoalesceSeparatesThoughtFromMessage(t *testing.T) {
	// A message chunk after thought chunks flushes the thought as its own block.
	out := collect(
		ev("agent_thought_chunk", api.AgentThoughtChunk{Text: "considering "}),
		ev("agent_thought_chunk", api.AgentThoughtChunk{Text: "options"}),
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "here is the answer"}),
		ev("turn_end", api.TurnEnd{}),
	)
	if len(out) != 3 {
		t.Fatalf("blocks = %d, want 3 (thought, message, divider)\n%v", len(out), out)
	}
	if !strings.Contains(out[0], "considering options") || !strings.Contains(out[0], "thinking") {
		t.Errorf("thought block wrong: %q", out[0])
	}
	if !strings.Contains(out[1], "here is the answer") {
		t.Errorf("message block wrong: %q", out[1])
	}
}

func TestCoalesceRendersPromptAndError(t *testing.T) {
	out := collect(
		ev("user_prompt", api.UserPrompt{Text: "fix the bug", Attachments: 2}),
		ev("agent_error", api.AgentError{Message: "provider timed out"}),
	)
	if len(out) != 2 {
		t.Fatalf("blocks = %d, want 2\n%v", len(out), out)
	}
	if !strings.Contains(out[0], "fix the bug") || !strings.Contains(out[0], "you") {
		t.Errorf("prompt block wrong: %q", out[0])
	}
	if !strings.Contains(out[0], "2 attachment") {
		t.Errorf("prompt block missing attachment count: %q", out[0])
	}
	if !strings.Contains(out[1], "provider timed out") {
		t.Errorf("error block wrong: %q", out[1])
	}
}

func TestCoalesceResetDropsBufferedText(t *testing.T) {
	// A stream switch (reset) must not merge a half-built message into the next.
	var out []string
	c := newCoalescer(func(html string) { out = append(out, html) })
	c.handle(ev("agent_message_chunk", api.AgentMessageChunk{Text: "stale partial"}))
	c.reset()
	c.handle(ev("agent_message_chunk", api.AgentMessageChunk{Text: "fresh"}))
	c.handle(ev("turn_end", api.TurnEnd{}))

	for _, b := range out {
		if strings.Contains(b, "stale partial") {
			t.Fatalf("reset did not drop buffered text: %q", b)
		}
	}
	if len(out) == 0 || !strings.Contains(out[0], "fresh") {
		t.Fatalf("fresh block missing after reset: %v", out)
	}
}

func TestCoalesceRendersCompactionSummary(t *testing.T) {
	// A summary event (compaction record) renders a collapsed block carrying the
	// condensed text and the [from,to] range — it must not be dropped. It also
	// flushes any pending message first.
	summary := ev("summary", api.ContextSummary{Text: "earlier: set up the daemon and wired the client"})
	summary.Kind = api.KindSummary
	summary.Summary = &api.SummaryInfo{FromSeq: 4, ToSeq: 19}

	out := collect(
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "recent line"}),
		summary,
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "after"}),
		ev("turn_end", api.TurnEnd{}),
	)
	if len(out) != 4 {
		t.Fatalf("blocks = %d, want 4 (message, summary, message, divider)\n%v", len(out), out)
	}
	if !strings.Contains(out[1], "earlier: set up the daemon") {
		t.Errorf("summary text dropped: %q", out[1])
	}
	if !strings.Contains(out[1], "summary") || !strings.Contains(out[1], "4") || !strings.Contains(out[1], "19") {
		t.Errorf("summary block missing kind/range: %q", out[1])
	}
}

func TestCoalesceIgnoresUnhandledTypes(t *testing.T) {
	// An unhandled discrete type (agent_model_updated) flushes the buffer but
	// emits nothing itself.
	out := collect(
		ev("agent_message_chunk", api.AgentMessageChunk{Text: "hi"}),
		ev("agent_model_updated", map[string]any{"session_id": "s1"}),
	)
	if len(out) != 1 || !strings.Contains(out[0], "hi") {
		t.Fatalf("want just the flushed message, got %v", out)
	}
}

func TestArgSummaryKnownKeys(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"file uri", `{"uri":"file:///repo/main.go"}`, "/repo/main.go"},
		{"file_path", `{"file_path":"/repo/x.go"}`, "/repo/x.go"},
		{"shell command", `{"command":"go test ./..."}`, "go test ./..."},
		{"multiline command collapses", `{"command":"go build\n  ./..."}`, "go build ./..."},
		{"search pattern", `{"pattern":"func main"}`, "func main"},
		{"fetch url", `{"url":"https://example.com"}`, "https://example.com"},
		{"generic fallback key=value", `{"weird_field":"hello"}`, "weird_field=hello"},
		{"scalar bool", `{"recursive":true}`, "recursive=true"},
		{"empty", ``, ""},
		{"no scalar", `{"nested":{"a":1}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := argSummary(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("argSummary(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestArgSummaryClipsLongValue(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := argSummary(json.RawMessage(`{"command":"` + long + `"}`))
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 141 {
		t.Errorf("expected clipped 140+ellipsis, got len=%d suffix=%q", len([]rune(got)), got)
	}
}

func TestArgFullPrettyPrints(t *testing.T) {
	got := argFull(json.RawMessage(`{"command":"ls","recursive":true}`))
	if !strings.Contains(got, "\"command\": \"ls\"") || !strings.Contains(got, "\n") {
		t.Errorf("argFull did not pretty-print: %q", got)
	}
	if argFull(nil) != "" {
		t.Errorf("argFull(nil) should be empty")
	}
}

func TestToolCallRendersArgAndFullInput(t *testing.T) {
	out := collect(ev("tool_call", api.ToolCall{
		ToolCallID: "t1", Name: "run_terminal",
		RawInput: json.RawMessage(`{"command":"go test ./..."}`),
	}))
	if len(out) != 1 {
		t.Fatalf("expected 1 block, got %d", len(out))
	}
	html := out[0]
	for _, want := range []string{"go test ./...", "tl-tool-detail", "<summary>input</summary>"} {
		if !strings.Contains(html, want) {
			t.Errorf("tool_call html missing %q:\n%s", want, html)
		}
	}
}
