package session_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client/clienttest"

	"github.com/dusto/tend-ui/internal/session"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestListReturnsSessionsAndReusesConnection(t *testing.T) {
	srv := clienttest.New(t)
	srv.Handle("workspace.open", func(json.RawMessage) (any, error) {
		return api.WorkspaceInfo{WorkspaceID: "ws-1", WorktreeRoot: "/repo"}, nil
	})
	calls := 0
	srv.Handle("session.list", func(json.RawMessage) (any, error) {
		calls++
		return api.SessionListResult{Sessions: []api.SessionInfo{
			{SessionID: "ses-1", ProviderID: "claude", Status: api.StatusRunning},
			{SessionID: "ses-2", ProviderID: "codex", Status: api.StatusIdle},
		}}, nil
	})

	l := session.NewListerWithSocket("/repo", srv.Socket())
	t.Cleanup(func() { _ = l.Close() })

	for i := 0; i < 2; i++ { // second call reuses the connection (no re-dial)
		got, err := l.List(testCtx(t))
		if err != nil {
			t.Fatalf("List #%d: %v", i, err)
		}
		if len(got) != 2 || got[0].SessionID != "ses-1" {
			t.Fatalf("List #%d = %+v", i, got)
		}
	}
	if calls != 2 {
		t.Errorf("session.list called %d times, want 2", calls)
	}
}

func TestListIsDaemonWide(t *testing.T) {
	// A standalone surface lists every workspace's sessions, so session.list must
	// be called with an EMPTY workspace id (the daemon filters only when it is set).
	srv := clienttest.New(t)
	srv.Handle("workspace.open", func(json.RawMessage) (any, error) {
		return api.WorkspaceInfo{WorkspaceID: "ws-launch", WorktreeRoot: "/repo"}, nil
	})
	var gotWS api.WorkspaceID = "unset"
	srv.Handle("session.list", func(params json.RawMessage) (any, error) {
		var p api.SessionListParams
		_ = json.Unmarshal(params, &p)
		gotWS = p.WorkspaceID
		return api.SessionListResult{Sessions: []api.SessionInfo{
			{SessionID: "ses-a", WorkspaceID: "ws-1", WorktreeRoot: "/a"},
			{SessionID: "ses-b", WorkspaceID: "ws-2", WorktreeRoot: "/b"},
		}}, nil
	})

	l := session.NewListerWithSocket("/repo", srv.Socket())
	t.Cleanup(func() { _ = l.Close() })

	got, err := l.List(testCtx(t))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotWS != "" {
		t.Errorf("session.list workspace id = %q, want empty (daemon-wide)", gotWS)
	}
	// Sessions from different workspaces are all returned.
	if len(got) != 2 {
		t.Fatalf("List = %+v, want two sessions across workspaces", got)
	}
}

func TestListErrorsWhenDaemonAbsent(t *testing.T) {
	missing := t.TempDir() + "/nope.sock"
	l := session.NewListerWithSocket("/repo", missing)
	t.Cleanup(func() { _ = l.Close() })
	if _, err := l.List(testCtx(t)); err == nil {
		t.Fatal("expected an error when the daemon socket is absent")
	}
}
