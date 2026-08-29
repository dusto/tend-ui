package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
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
// each event to the Hub. It resumes from the last cursor across re-subscribes
// and reconnects (so events are not replayed into the browser log), and
// reconnects on failure so the UI recovers when the daemon restarts or is
// started after the UI.
type Bridge struct {
	dir string
	hub *Hub[api.Event]

	// lastSeq is the CursorSeq of the last workspace event processed. It is the
	// resume point for events.subscribe. Written on the connection read goroutine
	// (onNotify), read on the Run goroutine.
	lastSeq atomic.Uint64
	// epoch is the daemon epoch the cursor belongs to; a restart (new epoch)
	// invalidates per-stream seqs. Only touched on the Run goroutine.
	epoch string
}

// New returns a Bridge that follows the workspace for dir.
func New(dir string, hub *Hub[api.Event]) *Bridge {
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
// workspace stream (resuming from the cursor), and blocks relaying events until
// the connection or ctx ends — re-subscribing if the daemon drops the
// subscription while the connection stays open.
func (b *Bridge) follow(ctx context.Context) error {
	resub := make(chan struct{}, 1)
	conn, err := client.Dial(ctx, client.Options{
		ClientID:          "tend-ui",
		MinPluginToDaemon: minPluginToDaemon,
		OnNotify: func(method string, params json.RawMessage) {
			b.onNotify(method, params, resub)
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var ws api.WorkspaceInfo
	if err := conn.Call(ctx, "workspace.open", api.WorkspaceOpenParams{Dir: b.dir}, &ws); err != nil {
		return err
	}
	// A daemon restart (new epoch) invalidates per-stream seqs, so resume only
	// within the same epoch; a fresh epoch replays from the start.
	if string(ws.DaemonEpoch) != b.epoch {
		b.epoch = string(ws.DaemonEpoch)
		b.lastSeq.Store(0)
	}
	stream := api.WorkspaceStream(ws.WorkspaceID)

	if err := b.subscribe(ctx, conn, stream); err != nil {
		return err
	}
	slog.Info("tend-ui: following workspace stream",
		"workspace", ws.WorkspaceID, "root", ws.WorktreeRoot, "from_seq", b.lastSeq.Load())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Done():
			return errConnClosed
		case <-resub:
			// The daemon dropped our subscription (per-stream overflow or ended)
			// while the connection is still open — re-subscribe from the cursor so
			// events keep flowing.
			if err := b.subscribe(ctx, conn, stream); err != nil {
				return err
			}
			slog.Info("tend-ui: re-subscribed after subscription_closed", "from_seq", b.lastSeq.Load())
		}
	}
}

// subscribe subscribes to stream from the tracked cursor. If the cursor was
// compacted away, it resumes at the daemon-provided summary boundary (older
// turns arrive as summaries); any other resume failure falls back to a full
// replay once rather than losing the stream.
func (b *Bridge) subscribe(ctx context.Context, conn *client.Conn, stream api.StreamID) error {
	from := b.lastSeq.Load()
	err := conn.Call(ctx, "events.subscribe",
		api.EventsSubscribeParams{StreamID: stream, LastSeq: from}, &api.EventsSubscribeResult{})
	if err == nil || from == 0 {
		return err
	}
	resume := uint64(0)
	if e, ok := client.AsError(err); ok && e.Code == api.ErrCursorCompacted {
		var data api.CursorCompactedData
		if json.Unmarshal(e.Data, &data) == nil {
			resume = data.BoundarySeq
		}
		slog.Warn("tend-ui: cursor compacted, resuming at boundary", "from_seq", from, "boundary", resume)
	} else {
		slog.Warn("tend-ui: cursor resume failed, replaying from start", "from_seq", from, "err", err)
	}
	b.lastSeq.Store(resume)
	return conn.Call(ctx, "events.subscribe",
		api.EventsSubscribeParams{StreamID: stream, LastSeq: resume}, &api.EventsSubscribeResult{})
}

// onNotify runs on the connection's read goroutine (see client.Options.OnNotify).
// It must never block or Call over the same connection (that would deadlock the
// read loop), so subscription recovery is handed to the follow loop via resub.
func (b *Bridge) onNotify(method string, params json.RawMessage, resub chan<- struct{}) {
	switch method {
	case "event.push":
		var p api.EventPushParams
		if err := json.Unmarshal(params, &p); err != nil {
			slog.Warn("tend-ui: bad event.push", "err", err)
			return
		}
		// Advance the cursor to the value the daemon says to store after this
		// record, then fan out. CursorSeq == Seq for a normal event and the
		// range end for a summary, so a resume never re-delivers it.
		b.lastSeq.Store(p.Event.CursorSeq)
		b.hub.Broadcast(p.Event)
	case "event.subscription_closed":
		// Non-blocking signal to the follow loop to re-subscribe.
		select {
		case resub <- struct{}{}:
		default:
		}
	}
}
