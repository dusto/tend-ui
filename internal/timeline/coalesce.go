// Package timeline follows a session's event stream on the daemon and turns it
// into a live timeline for the UI. Streamed agent message/thought text arrives
// as chunks with no id — consecutive chunks form one block until a different
// event type arrives — so the coalescer buffers text and flushes a complete
// block on the next non-text event or turn end. Completed blocks are rendered to
// HTML and pushed to the UI over SSE.
package timeline

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/a-h/templ"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/web/templates"
)

// coalescer converts a session event stream into rendered timeline blocks. It
// accumulates agent message/thought text across chunks and emits a complete
// block (via emit) when the run of chunks ends; discrete events render directly.
// It is driven from a single goroutine (the follow loop), so it needs no locking.
type coalescer struct {
	emit func(html string)

	buf  strings.Builder
	kind string // "message" | "thought" | "" — what buf is accumulating
}

// newCoalescer returns a coalescer that calls emit with each completed block's
// HTML.
func newCoalescer(emit func(html string)) *coalescer {
	return &coalescer{emit: emit}
}

// flush renders and emits whatever text is buffered, then clears it.
func (c *coalescer) flush() {
	if c.buf.Len() == 0 {
		c.kind = ""
		return
	}
	text := c.buf.String()
	c.buf.Reset()
	switch c.kind {
	case "message":
		c.emit(render(templates.TLMessage(text)))
	case "thought":
		c.emit(render(templates.TLThought(text)))
	}
	c.kind = ""
}

// handle folds one event into the timeline: text chunks accumulate, everything
// else flushes the buffer and renders directly.
func (c *coalescer) handle(ev api.Event) {
	switch ev.Type {
	case "agent_message_chunk":
		var p api.AgentMessageChunk
		if decode(ev, &p) {
			c.accumulate("message", p.Text)
		}
	case "agent_thought_chunk":
		var p api.AgentThoughtChunk
		if decode(ev, &p) {
			c.accumulate("thought", p.Text)
		}
	default:
		c.flush()
		c.renderDiscrete(ev)
	}
}

// accumulate appends chunk text to the buffer, flushing first if the kind
// changed (a message interrupted by a thought, or vice versa).
func (c *coalescer) accumulate(kind, chunk string) {
	if c.kind != kind {
		c.flush()
		c.kind = kind
	}
	c.buf.WriteString(chunk)
}

// renderDiscrete renders the non-streamed event types the first cut surfaces.
// Unhandled types (mode/model/usage updates, plan, tool_call_update, …) are
// intentionally dropped here for now.
func (c *coalescer) renderDiscrete(ev api.Event) {
	switch ev.Type {
	case "user_prompt":
		var p api.UserPrompt
		if decode(ev, &p) {
			c.emit(render(templates.TLPrompt(p.Text, p.Attachments)))
		}
	case "tool_call":
		var p api.ToolCall
		if decode(ev, &p) {
			c.emit(render(templates.TLToolCall(p.ToolCallID, p.Name, argSummary(p.RawInput), toolKind(p.Name))))
		}
	case "agent_error":
		var p api.AgentError
		if decode(ev, &p) {
			c.emit(render(templates.TLError(p.Message)))
		}
	case "summary":
		// A compaction record replacing a range of raw turns (served on replay).
		// Render the condensed text as a collapsed block so compacted history is
		// preserved rather than dropped; Event.Summary carries the range.
		var p api.ContextSummary
		if decode(ev, &p) {
			var from, to uint64
			if ev.Summary != nil {
				from, to = ev.Summary.FromSeq, ev.Summary.ToSeq
			}
			c.emit(render(templates.TLSummary(p.Text, from, to)))
		}
	case "turn_end":
		c.emit(render(templates.TLTurnEnd()))
	}
}

// decode unmarshals an event payload into dst, reporting success.
func decode(ev api.Event, dst any) bool {
	return json.Unmarshal(ev.Payload, dst) == nil
}

// render renders a templ component to a string.
func render(c templ.Component) string {
	var b strings.Builder
	_ = c.Render(context.Background(), &b)
	return b.String()
}
