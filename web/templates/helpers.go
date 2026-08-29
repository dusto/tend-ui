package templates

import (
	"path/filepath"
	"strconv"

	"github.com/dusto/tend/api"
)

// itoa renders an int (used for counts in templates).
func itoa(n int) string { return strconv.Itoa(n) }

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
