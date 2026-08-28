package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"
)

// minPluginToDaemon is the lowest plugin->daemon contract tend-ui needs to open
// a workspace and follow its stream (workspace.open + events.subscribe are
// foundational). Raise it as the UI starts calling newer methods.
const minPluginToDaemon = "0.8.0"

// reconnectDelay is the pause before re-dialing after the daemon session ends.
const reconnectDelay = 2 * time.Second

// errConnClosed reports that the daemon connection ended while following.
var errConnClosed = errors.New("bridge: daemon connection closed")

// Bridge follows a directory's workspace event stream on the daemon and relays
// each event to the Hub. It reconnects on failure so the UI recovers when the
// daemon restarts or is started after the UI.
type Bridge struct {
	dir string
	hub *Hub
}

// New returns a Bridge that follows the workspace for dir.
func New(dir string, hub *Hub) *Bridge {
	return &Bridge{dir: dir, hub: hub}
}

// Run connects and follows until ctx is cancelled, reconnecting with a fixed
// backoff. It logs transient failures and returns only when ctx is done.
func (b *Bridge) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := b.follow(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("tend-ui: daemon session ended, reconnecting", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

// follow dials the daemon, opens the workspace for dir, subscribes to its
// workspace stream, and blocks relaying events until the connection or ctx ends.
func (b *Bridge) follow(ctx context.Context) error {
	conn, err := client.Dial(ctx, client.Options{
		ClientID:          "tend-ui",
		MinPluginToDaemon: minPluginToDaemon,
		OnNotify:          b.onNotify,
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var ws api.WorkspaceInfo
	if err := conn.Call(ctx, "workspace.open", api.WorkspaceOpenParams{Dir: b.dir}, &ws); err != nil {
		return err
	}
	stream := api.WorkspaceStream(ws.WorkspaceID)
	// LastSeq 0: replay the retained log then follow live, so a UI that attaches
	// after activity still sees it.
	if err := conn.Call(ctx, "events.subscribe",
		api.EventsSubscribeParams{StreamID: stream, LastSeq: 0}, &api.EventsSubscribeResult{}); err != nil {
		return err
	}
	slog.Info("tend-ui: following workspace stream", "workspace", ws.WorkspaceID, "root", ws.WorktreeRoot)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-conn.Done():
		return errConnClosed
	}
}

// onNotify runs on the connection's read goroutine (see client.Options.OnNotify):
// it decodes event.push and hands the event to the Hub, which never blocks.
func (b *Bridge) onNotify(method string, params json.RawMessage) {
	if method != "event.push" {
		return
	}
	var p api.EventPushParams
	if err := json.Unmarshal(params, &p); err != nil {
		slog.Warn("tend-ui: bad event.push", "err", err)
		return
	}
	b.hub.Broadcast(p.Event)
}
