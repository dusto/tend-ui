// Package session lists the daemon's sessions for the UI's session rail. It
// holds a lazily-(re)dialed daemon connection and calls session.list on demand;
// the rail polls it. Selecting a session is handled by the timeline follower —
// this package only reads.
package session

import (
	"context"
	"sync"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"
)

const minPluginToDaemon = "0.8.0"

// Lister returns the daemon's sessions for a workspace. It keeps one connection
// and reconnects lazily, so repeated List calls (the rail's poll) don't re-dial
// each time. Safe for concurrent use.
type Lister struct {
	dir    string
	socket string // "" = the daemon's default socket

	mu   sync.Mutex
	conn *client.Conn
	ws   api.WorkspaceID
}

// NewLister returns a Lister for the workspace containing dir, using the
// daemon's default socket.
func NewLister(dir string) *Lister { return &Lister{dir: dir} }

// NewListerWithSocket is NewLister against an explicit socket path (tests).
func NewListerWithSocket(dir, socket string) *Lister {
	return &Lister{dir: dir, socket: socket}
}

// List returns the sessions for the launch directory's workspace. It dials and
// opens the workspace on first use (or after a dropped connection); a call error
// drops the connection so the next List redials.
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
		var ws api.WorkspaceInfo
		if err := conn.Call(ctx, "workspace.open", api.WorkspaceOpenParams{Dir: l.dir}, &ws); err != nil {
			_ = conn.Close()
			return nil, err
		}
		l.conn = conn
		l.ws = ws.WorkspaceID
	}

	var res api.SessionListResult
	if err := l.conn.Call(ctx, "session.list", api.SessionListParams{WorkspaceID: l.ws}, &res); err != nil {
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
