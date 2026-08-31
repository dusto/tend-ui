// Package session lists the daemon's sessions for the UI's session rail. It
// holds a lazily-(re)dialed daemon connection and calls session.list on demand;
// the rail polls it. Selecting a session is handled by the timeline follower —
// this package only reads.
//
// tend-ui is a STANDALONE surface: it lists every session the daemon holds, not
// only those in its launch directory's workspace. Scoping the list to the launch
// dir hid sessions living in other worktrees (an empty rail while the connection
// was healthy — tend-du1.16), so List passes an empty workspace id, which the
// daemon treats as "all workspaces". The rail groups the results by workspace.
package session

import (
	"context"
	"sync"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"
)

const minPluginToDaemon = "0.8.0"

// Lister returns every session the daemon holds. It keeps one connection and
// reconnects lazily, so repeated List calls (the rail's poll) don't re-dial each
// time. Safe for concurrent use.
type Lister struct {
	dir    string
	socket string // "" = the daemon's default socket

	mu   sync.Mutex
	conn *client.Conn
}

// NewLister returns a Lister for the workspace containing dir, using the
// daemon's default socket.
func NewLister(dir string) *Lister { return &Lister{dir: dir} }

// NewListerWithSocket is NewLister against an explicit socket path (tests).
func NewListerWithSocket(dir, socket string) *Lister {
	return &Lister{dir: dir, socket: socket}
}

// List returns the sessions for the launch directory's workspace. It dials and
// opens the launch-dir workspace on first use only to register this client's
// home context — the listing itself is daemon-wide (empty workspace id). A call
// error drops the connection so the next List redials.
func (l *Lister) List(ctx context.Context) ([]api.SessionInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		conn, err := client.Dial(ctx, client.Options{
			Socket:            l.socket,
			ClientID:          "tend-ui-sessions",
			MinPluginToDaemon: minPluginToDaemon,
		})
		if err != nil {
			return nil, err
		}
		// Open the launch-dir workspace for client presence, but do NOT scope the
		// listing to it (see the package doc / tend-du1.16).
		if err := conn.Call(ctx, "workspace.open", api.WorkspaceOpenParams{Dir: l.dir}, &api.WorkspaceInfo{}); err != nil {
			_ = conn.Close()
			return nil, err
		}
		l.conn = conn
	}

	// Empty WorkspaceID: the daemon returns sessions from every workspace (it
	// filters only when the id is set).
	var res api.SessionListResult
	if err := l.conn.Call(ctx, "session.list", api.SessionListParams{}, &res); err != nil {
		_ = l.conn.Close()
		l.conn = nil
		return nil, err
	}
	return res.Sessions, nil
}

// Close releases the connection.
func (l *Lister) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != nil {
		err := l.conn.Close()
		l.conn = nil
		return err
	}
	return nil
}
