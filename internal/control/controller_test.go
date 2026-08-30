package control_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client/clienttest"

	"github.com/dusto/tend-ui/internal/control"
)

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return c
}

// baseDaemon returns a clienttest server answering workspace.open, so the
// Controller can connect.
func baseDaemon(t *testing.T) *clienttest.Server {
	srv := clienttest.New(t)
	srv.Handle("workspace.open", func(json.RawMessage) (any, error) {
		return api.WorkspaceInfo{WorkspaceID: "ws-1", WorktreeRoot: "/repo"}, nil
	})
	return srv
}

func TestApprovalsAndRespond(t *testing.T) {
	srv := baseDaemon(t)
	srv.Handle("approval.list", func(json.RawMessage) (any, error) {
		return api.ApprovalListResult{Approvals: []api.ApprovalSummary{
			{ApprovalID: "ap-1", SessionID: "s1", Kind: "file_edit"},
		}}, nil
	})
	var (
		mu      sync.Mutex
		respond api.ApprovalRespondParams
		gotResp bool
	)
	srv.Handle("approval.respond", func(p json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(p, &respond)
		gotResp = true
		return api.ApprovalRespondResult{}, nil
	})

	c := control.NewWithSocket("/repo", srv.Socket())
	t.Cleanup(func() { _ = c.Close() })

	got, err := c.Approvals(ctx(t), "s1")
	if err != nil {
		t.Fatalf("Approvals: %v", err)
	}
	if len(got) != 1 || got[0].ApprovalID != "ap-1" {
		t.Fatalf("Approvals = %+v", got)
	}

	if err := c.Respond(ctx(t), "ap-1", true); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !gotResp || respond.ApprovalID != "ap-1" || !respond.Approved {
		t.Errorf("respond = %+v (got=%v)", respond, gotResp)
	}
}

func TestPromptDispatchesInBackground(t *testing.T) {
	srv := baseDaemon(t)
	prompted := make(chan api.AgentPromptParams, 1)
	srv.Handle("agent.prompt", func(p json.RawMessage) (any, error) {
		var pp api.AgentPromptParams
		_ = json.Unmarshal(p, &pp)
		prompted <- pp
		return api.AgentPromptResult{Status: api.StatusIdle}, nil
	})

	c := control.NewWithSocket("/repo", srv.Socket())
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Prompt(ctx(t), "s1", "do the thing"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	select {
	case pp := <-prompted:
		if pp.SessionID != "s1" || pp.Text != "do the thing" {
			t.Errorf("prompt = %+v", pp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent.prompt was not dispatched")
	}
}

func TestCancelAndStop(t *testing.T) {
	srv := baseDaemon(t)
	seen := make(chan string, 2)
	srv.Handle("agent.cancel", func(json.RawMessage) (any, error) { seen <- "cancel"; return struct{}{}, nil })
	srv.Handle("agent.stop", func(json.RawMessage) (any, error) { seen <- "stop"; return struct{}{}, nil })

	c := control.NewWithSocket("/repo", srv.Socket())
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Cancel(ctx(t), "s1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := c.Stop(ctx(t), "s1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case m := <-seen:
			got[m] = true
		case <-time.After(time.Second):
			t.Fatal("cancel/stop not received")
		}
	}
	if !got["cancel"] || !got["stop"] {
		t.Errorf("got %v", got)
	}
}
