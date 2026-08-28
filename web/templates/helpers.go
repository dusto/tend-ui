package templates

import (
	"path/filepath"

	"github.com/dusto/tend/api"
)

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
