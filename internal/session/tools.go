package session

// ToolRef is one tool call in the followed session, tracked for the timeline's
// jump-index (click to scroll to where it ran) and its live status. There is no
// daemon "artifact" event yet, so tool calls are the closest proxy for "things
// the agent produced"; rich artifact previews await a daemon artifact channel.
type ToolRef struct {
	// ID is the daemon tool_call_id; the inline card is anchored at "tc-"+ID.
	ID string
	// Name is the tool (read_buffer, edit_buffer, lsp.diagnostics, …).
	Name string
	// Kind classifies the row for the timeline filter: "edit" for buffer
	// mutations (write_buffer/edit_buffer), else "tool".
	Kind string
	// Arg is a short human-facing summary of the input (e.g. the file uri), or "".
	Arg string
	// Status is the last reported tool status: "running" until a tool_call_update
	// reports "completed"/"failed"/…
	Status string
}
