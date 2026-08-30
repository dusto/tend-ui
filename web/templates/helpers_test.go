package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dusto/tend/api"
)

func agentToolApproval(t *testing.T) api.ApprovalSummary {
	t.Helper()
	detail, err := json.Marshal(api.ApprovalDetail{
		Kind: api.ApprovalAgentTool,
		AgentTool: &api.AgentToolApproval{
			ToolCallID: "tc-1",
			Title:      "Write file",
			ToolKind:   "edit",
			RawInput:   json.RawMessage(`{"file_path":"/x.go","content":"package x"}`),
		},
	})
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	return api.ApprovalSummary{ApprovalID: "ap-1", Kind: api.ApprovalAgentTool, Detail: detail}
}

func TestAgentToolApprovalHelpers(t *testing.T) {
	a := agentToolApproval(t)

	if got := approvalTitle(a); got != "Allow tool call?" {
		t.Errorf("title = %q", got)
	}
	if got := approvalToolName(a); got != "Write file" {
		t.Errorf("tool name = %q, want %q", got, "Write file")
	}
	input := approvalToolInput(a)
	if !strings.Contains(input, "file_path") || !strings.Contains(input, "\n") {
		t.Errorf("tool input not pretty-printed: %q", input)
	}
	// A file-edit helper must not claim this agent-tool approval.
	if approvalDiff(a) != "" || approvalCommand(a) != "" {
		t.Errorf("agent_tool approval should have no file diff / pane command")
	}
}

func TestNonAgentToolApprovalHasNoToolFields(t *testing.T) {
	a := api.ApprovalSummary{Kind: api.ApprovalPaneRun}
	if approvalToolName(a) != "" || approvalToolInput(a) != "" {
		t.Errorf("non-agent_tool approval should have no tool name/input")
	}
}
