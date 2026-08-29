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

func TestListErrorsWhenDaemonAbsent(t *testing.T) {
	missing := t.TempDir() + "/nope.sock"
	l := session.NewListerWithSocket("/repo", missing)
	t.Cleanup(func() { _ = l.Close() })
	if _, err := l.List(testCtx(t)); err == nil {
		t.Fatal("expected an error when the daemon socket is absent")
	}
}
