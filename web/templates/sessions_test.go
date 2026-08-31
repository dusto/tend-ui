package templates

import (
	"context"
	"strings"
	"testing"

	"github.com/dusto/tend/api"
)

func TestGroupByWorkspacePreservesOrderAndLabels(t *testing.T) {
	sessions := []api.SessionInfo{
		{SessionID: "s1", WorkspaceID: "wsA", WorktreeRoot: "/home/u/work/tend"},
		{SessionID: "s2", WorkspaceID: "wsB", WorktreeRoot: "/home/u/work/tend-ui"},
		{SessionID: "s3", WorkspaceID: "wsA", WorktreeRoot: "/home/u/work/tend"},
	}
	groups := groupByWorkspace(sessions)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Label != "tend" || groups[1].Label != "tend-ui" {
		t.Errorf("labels = %q / %q, want tend / tend-ui", groups[0].Label, groups[1].Label)
	}
	// wsA keeps both its sessions, in order; grouping is not interleaved.
	if len(groups[0].Sessions) != 2 || groups[0].Sessions[0].SessionID != "s1" || groups[0].Sessions[1].SessionID != "s3" {
		t.Errorf("first group sessions = %+v", groups[0].Sessions)
	}
	if groups[0].Title != "/home/u/work/tend" {
		t.Errorf("group title = %q, want full worktree root", groups[0].Title)
	}
}

func TestWorkspaceLabelFallsBackToID(t *testing.T) {
	if got := workspaceLabel(api.SessionInfo{WorkspaceID: "abcdef123456"}); got != "abcdef12" {
		t.Errorf("label = %q, want short workspace id when no worktree root", got)
	}
}

func TestSessionRailRendersWorkspaceHeaders(t *testing.T) {
	sessions := []api.SessionInfo{
		{SessionID: "s1", ProviderID: "claude", WorkspaceID: "wsA", WorktreeRoot: "/home/u/work/tend", Status: api.StatusRunning},
		{SessionID: "s2", ProviderID: "codex", WorkspaceID: "wsB", WorktreeRoot: "/home/u/work/tend-ui", Status: api.StatusIdle},
	}
	var b strings.Builder
	if err := SessionRail(sessions, "s1").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		`class="rail-group"`,         // each repo is a group
		`class="rail-ws-name">tend<`, // repo name header
		`class="rail-ws-name">tend-ui<`,
		`class="rail-sessions"`, // sessions nested under the repo
		"/home/u/work/tend",     // full worktree root on hover
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rail missing %q:\n%s", want, out)
		}
	}
	// The nested container comes after its repo header (hierarchy, not a flat list).
	if strings.Index(out, `class="rail-ws-name">tend<`) > strings.Index(out, `class="rail-sessions"`) {
		t.Errorf("repo header should precede its nested sessions:\n%s", out)
	}
}
