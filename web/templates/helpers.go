package templates

import (
	"path/filepath"
	"strconv"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/internal/session"
)

// SessionHeaderData bundles what the shell needs to render the focused session's
// header on first paint (the /header endpoint re-renders it on poll).
type SessionHeaderData struct {
	Session api.SessionInfo
	Usage   session.Usage
	Found   bool
}

// itoa renders an int (used for counts in templates).
func itoa(n int) string { return strconv.Itoa(n) }

// thousands formats n with thousands separators (e.g. 124800 -> "124,800").
func thousands(n int) string {
	s := strconv.Itoa(n)
	neg := ""
	if n < 0 {
		neg, s = "-", s[1:]
	}
	if len(s) <= 3 {
		return neg + s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return neg + string(out)
}

// clampPct bounds a percent to [0,100] for a meter width.
func clampPct(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// contextLabel renders the context-window fill: "124,800 / 200,000 · 62%" when
// the window is known, else the raw used count.
func contextLabel(u session.Usage) string {
	if pct := u.ContextPercent(); pct >= 0 {
		return thousands(u.ContextUsed) + " / " + thousands(u.ContextWindow) + " · " + strconv.Itoa(pct) + "%"
	}
	return thousands(u.ContextUsed) + " tokens"
}

// statusClass maps a session status to a CSS modifier.
func statusClass(s api.SessionStatus) string {
	switch s {
	case api.StatusRunning:
		return "running"
	case api.StatusWaitingApproval, api.StatusWaitingClarification:
		return "wait"
	case api.StatusError:
		return "error"
	case api.StatusEnded:
		return "ended"
	default:
		return "idle"
	}
}

// statusLabel is the short human-facing status text.
func statusLabel(s api.SessionStatus) string {
	switch s {
	case api.StatusWaitingApproval:
		return "approval"
	case api.StatusWaitingClarification:
		return "clarify"
	default:
		return string(s)
	}
}

// evTime renders an event's wall-clock time (empty when unset).
func evTime(ev api.Event) string {
	if ev.TS.IsZero() {
		return ""
	}
	return ev.TS.Format("15:04:05")
}

// evRoot is the basename of an event's worktree root, for a compact tag.
func evRoot(ev api.Event) string {
	if ev.WorktreeRoot == "" {
		return ""
	}
	return filepath.Base(ev.WorktreeRoot)
}
