package timeline

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"
	"github.com/dusto/tend/client/clienttest"

	"github.com/dusto/tend-ui/internal/bridge"
)

// TestPickFollowsForeignWorkspaceSession is the tend-du1.16 review fix: the rail
// lists sessions daemon-wide, so picking one from another workspace must work —
// pick must list daemon-wide (empty workspace id), not only the launch workspace.
func TestPickFollowsForeignWorkspaceSession(t *testing.T) {
	srv := clienttest.New(t)
	var gotWS api.WorkspaceID = "unset"
	srv.Handle("session.list", func(params json.RawMessage) (any, error) {
		var p api.SessionListParams
		_ = json.Unmarshal(params, &p)
		gotWS = p.WorkspaceID
		return api.SessionListResult{Sessions: []api.SessionInfo{
			{SessionID: "ses-launch", WorkspaceID: "ws-launch", StreamID: "session:ses-launch", Status: api.StatusIdle},
			{SessionID: "ses-foreign", WorkspaceID: "ws-other", StreamID: "session:ses-foreign", Status: api.StatusIdle},
		}}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, client.Options{Socket: srv.Socket(), ClientID: "test", MinPluginToDaemon: minPluginToDaemon})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tl := New("/launch/dir", bridge.NewHub[string]())
	tl.Select("ses-foreign") // a row from a workspace other than the launch dir

	sess, ok, err := tl.pick(ctx, conn)
	if err != nil || !ok {
		t.Fatalf("pick: ok=%v err=%v", ok, err)
	}
	if gotWS != "" {
		t.Errorf("session.list workspace id = %q, want empty (daemon-wide)", gotWS)
	}
	if sess.SessionID != "ses-foreign" || sess.StreamID != "session:ses-foreign" {
		t.Errorf("pick = %+v, want the selected foreign-workspace session", sess)
	}
}
