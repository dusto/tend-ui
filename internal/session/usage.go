package session

// Usage is the token/context accounting the header shows for the followed
// session, accumulated from its stream's usage events. Zero values render as
// "no usage yet"; HasContext/HasToken flag which parts the provider has reported.
type Usage struct {
	// Context window fill (agent_context_usage). ContextWindow is 0 when the
	// provider omits the window size.
	ContextUsed   int
	ContextWindow int
	HasContext    bool

	// The most recent turn's token counts (agent_token_usage). These are
	// latest-event-wins, so they stay correct across replay and compaction — unlike
	// a client-reconstructed session cumulative, which a compacted stream would
	// under-report and a replay-from-0 fallback would double-count. A true session
	// total would need a daemon-reported cumulative field.
	LastInput  int
	LastOutput int
	LastTotal  int
	HasToken   bool

	// PromptApprox is the daemon's approximate token estimate of the last prompt
	// it composed (agent_prompt_usage) — a heuristic, flagged [approx] in the UI.
	PromptApprox int
	HasPrompt    bool
}

// ContextPercent is the window fill as a whole percent, or -1 when the window
// size is unknown (so the UI shows the raw count without a percent).
func (u Usage) ContextPercent() int {
	if u.ContextWindow <= 0 {
		return -1
	}
	return u.ContextUsed * 100 / u.ContextWindow
}
