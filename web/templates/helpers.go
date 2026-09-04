package templates

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/internal/session"
)

// approvalTitle is a human-facing headline for a pending approval by kind.
func approvalTitle(a api.ApprovalSummary) string {
	switch a.Kind {
	case "file_edit":
		return "Apply file edit?"
	case "pane_run":
		return "Run command?"
	case "pane_open":
		return "Open a shell pane?"
	case "filesystem_access":
		return "Allow filesystem access?"
	case "code_action":
		return "Apply code action?"
	case "agent_tool":
		return "Allow tool call?"
	default:
		return "Approve " + a.Kind + "?"
	}
}

// approvalToolName is the provider-native tool's title for an agent_tool
// approval (e.g. "Write file"), or "" for another kind.
func approvalToolName(a api.ApprovalSummary) string {
	if d := approvalDetail(a); d.AgentTool != nil {
		return d.AgentTool.Title
	}
	return ""
}

// approvalToolInput is the pretty-printed input for an agent_tool approval, so a
// client can review the exact call the agent's own tool is about to make. "" for
// another kind or an empty input.
func approvalToolInput(a api.ApprovalSummary) string {
	if d := approvalDetail(a); d.AgentTool != nil {
		return prettyJSON(d.AgentTool.RawInput)
	}
	return ""
}

// artifactFileName is the display name for an artifact: the file's basename,
// with any file:// prefix stripped.
func artifactFileName(uri string) string {
	return filepath.Base(strings.TrimPrefix(uri, "file://"))
}

// artifactExt is the lowercased extension of an artifact's uri.
func artifactExt(uri string) string {
	return strings.ToLower(filepath.Ext(strings.TrimPrefix(uri, "file://")))
}

// artifactIsMarkdown reports whether an artifact renders as markdown (safe tier).
func artifactIsMarkdown(uri string) bool {
	switch artifactExt(uri) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// artifactIsImage reports whether an artifact is a binary image — not inlinable
// as text; a real preview needs the sandbox (tend-du1.9).
func artifactIsImage(uri string) bool {
	switch artifactExt(uri) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".bmp":
		return true
	}
	return false
}

// artifactIsRich reports whether an artifact is text that RENDERS to something
// visual (an SVG, an HTML page, a mermaid diagram). Its source/diff is shown
// safely in the shell, but the rendered form must run in the sandbox (tend-du1.9),
// so the card shows a note pointing there.
func artifactIsRich(uri string) bool {
	switch artifactExt(uri) {
	case ".svg", ".html", ".htm", ".mmd", ".mermaid":
		return true
	}
	return false
}

// prettyJSON renders raw JSON indented for display, falling back to the trimmed
// raw text when it does not parse and "" when empty.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return strings.TrimSpace(string(raw))
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(out)
}

// approvalDetail decodes an approval's Detail into the typed view. Returns the
// zero value when it does not decode.
func approvalDetail(a api.ApprovalSummary) api.ApprovalDetail {
	var d api.ApprovalDetail
	_ = json.Unmarshal(a.Detail, &d)
	return d
}

// approvalDiff returns the unified diff for a file-edit approval (all targets
// joined), or "" for a non-edit kind.
func approvalDiff(a api.ApprovalSummary) string {
	d := approvalDetail(a)
	if d.FileEdit == nil {
		return ""
	}
	var b strings.Builder
	for i, tgt := range d.FileEdit.Targets {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tgt.URI + "\n" + tgt.Diff)
	}
	return b.String()
}

// approvalCommand returns the command for a pane-run approval, or "".
func approvalCommand(a api.ApprovalSummary) string {
	if d := approvalDetail(a); d.PaneRun != nil {
		return d.PaneRun.Command
	}
	return ""
}

// diffLineClass classifies a unified-diff line for coloring.
func diffLineClass(line string) string {
	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		return "d-file"
	case strings.HasPrefix(line, "@@"):
		return "d-hunk"
	case strings.HasPrefix(line, "+"):
		return "d-add"
	case strings.HasPrefix(line, "-"):
		return "d-del"
	default:
		return "d-ctx"
	}
}

// diffLines splits a diff into its lines for per-line coloring.
func diffLines(diff string) []string { return strings.Split(diff, "\n") }

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

// statusClass2 maps a tool-call status to a CSS modifier.
func statusClass2(status string) string {
	switch status {
	case "completed":
		return "ok"
	case "failed", "error":
		return "fail"
	default:
		return "run"
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
