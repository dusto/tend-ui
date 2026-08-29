package timeline

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"

	"github.com/dusto/tend-ui/internal/bridge"
)

const (
	minPluginToDaemon = "0.8.0"
	reconnectDelay    = 2 * time.Second
)

var (
	errConnClosed = errors.New("timeline: daemon connection closed")
	errSwitch     = errors.New("timeline: session switched")
)

// clearFrame is an out-of-band SSE payload that empties the browser's timeline
// container on a session switch (htmx swaps it by id), so a new session's blocks
// do not append below the previous one's.
const clearFrame = `<div id="timeline" class="timeline" hx-swap-oob="innerHTML"></div>`

// Timeline follows one session's event stream on the daemon and pushes rendered
// timeline blocks to a Hub for the UI's /timeline SSE endpoint. It auto-picks a
// session (a running one, else the first) for the launch directory's workspace;
// a session picker arrives with the session-list surface. Cursor resume,
// subscription_closed re-subscribe, and reconnect mirror the workspace bridge.
type Timeline struct {
	dir string
	hub *bridge.Hub[string]

	mu      sync.Mutex // guards coalescer across the read + Run goroutines
	coal    *coalescer
	stream  api.StreamID // the session stream currently followed
	epoch   string
	lastSeq atomic.Uint64

	// selected is the session the user chose to follow, or "" for auto-pick.
	selected atomic.Pointer[api.SessionID]
	// current is the session id actually being followed (for the rail highlight).
	current atomic.Pointer[api.SessionID]
	// switchCh signals the follow loop to re-pick after a Select.
	switchCh chan struct{}
}

// New returns a Timeline that pushes rendered blocks to hub.
func New(dir string, hub *bridge.Hub[string]) *Timeline {
	t := &Timeline{dir: dir, hub: hub, switchCh: make(chan struct{}, 1)}
	t.coal = newCoalescer(func(html string) { hub.Broadcast(html) })
	return t
}

// Select tells the timeline to follow session id (replacing the auto-pick). The
// follow loop switches without waiting for a reconnect.
func (t *Timeline) Select(id api.SessionID) {
	sid := id
	t.selected.Store(&sid)
	select {
	case t.switchCh <- struct{}{}:
	default:
	}
}

// Current returns the session the timeline is currently following, or "" when it
// has no session yet.
func (t *Timeline) Current() api.SessionID {
	if p := t.current.Load(); p != nil {
		return *p
	}
	return ""
}

// Run connects and follows until ctx is cancelled, reconnecting with a fixed
// backoff (which also re-picks the session if the current one is gone).
func (t *Timeline) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := t.follow(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("tend-ui: timeline session ended, reconnecting", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (t *Timeline) follow(ctx context.Context) error {
	resub := make(chan struct{}, 1)
	conn, err := client.Dial(ctx, client.Options{
		ClientID:          "tend-ui-timeline",
		MinPluginToDaemon: minPluginToDaemon,
		OnNotify: func(method string, params json.RawMessage) {
			t.onNotify(method, params, resub)
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var ws api.WorkspaceInfo
	if err := conn.Call(ctx, "workspace.open", api.WorkspaceOpenParams{Dir: t.dir}, &ws); err != nil {
		return err
	}

	// Follow the selected (or auto-picked) session; re-pick and re-subscribe in
	// place when the user switches, without dropping the connection.
	for ctx.Err() == nil {
		sess, ok, err := t.pick(ctx, conn, ws.WorkspaceID)
		if err != nil {
			return err
		}
		if !ok {
			// No session to follow yet: wait for a switch (a select or a new
			// session appearing) rather than busy-looping.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-conn.Done():
				return errConnClosed
			case <-t.switchCh:
				continue
			case <-time.After(reconnectDelay):
				continue
			}
		}
		t.setStream(sess, string(ws.DaemonEpoch))
		if err := t.subscribe(ctx, conn); err != nil {
			return err
		}
		slog.Info("tend-ui: following session timeline",
			"session", sess.SessionID, "stream", sess.StreamID, "from_seq", t.lastSeq.Load())

		if err := t.serve(ctx, conn, resub); err != nil {
			if errors.Is(err, errSwitch) {
				continue
			}
			return err
		}
	}
	return ctx.Err()
}

// serve blocks relaying the current stream until the connection or ctx ends, a
// re-subscribe is needed (same stream), or the user switches sessions (errSwitch,
// so the caller re-picks).
func (t *Timeline) serve(ctx context.Context, conn *client.Conn, resub chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Done():
			return errConnClosed
		case <-t.switchCh:
			return errSwitch
		case <-resub:
			if err := t.subscribe(ctx, conn); err != nil {
				return err
			}
		}
	}
}

// setStream points the timeline at sess's stream. When the stream (or the daemon
// epoch) changed it resets the cursor and the coalescer buffer and tells the
// browser to clear the old session's blocks, so a switch does not interleave two
// sessions.
func (t *Timeline) setStream(sess api.SessionInfo, epoch string) {
	sid := sess.SessionID
	t.current.Store(&sid)
	if sess.StreamID == t.stream && epoch == t.epoch {
		return
	}
	t.mu.Lock()
	t.stream = sess.StreamID
	t.epoch = epoch
	t.lastSeq.Store(0)
	t.coal.reset()
	t.mu.Unlock()
	t.hub.Broadcast(clearFrame)
}

// pick chooses the session to follow: the user's selection if it still exists,
// otherwise a running session, otherwise the first listed.
func (t *Timeline) pick(ctx context.Context, conn *client.Conn, ws api.WorkspaceID) (api.SessionInfo, bool, error) {
	var res api.SessionListResult
	if err := conn.Call(ctx, "session.list", api.SessionListParams{WorkspaceID: ws}, &res); err != nil {
		return api.SessionInfo{}, false, err
	}
	if len(res.Sessions) == 0 {
		return api.SessionInfo{}, false, nil
	}
	if sel := t.selected.Load(); sel != nil {
		for _, s := range res.Sessions {
			if s.SessionID == *sel {
				return s, true, nil
			}
		}
	}
	for _, s := range res.Sessions {
		if s.Status == api.StatusRunning {
			return s, true, nil
		}
	}
	return res.Sessions[0], true, nil
}

// subscribe subscribes to the current stream from the cursor. A compacted cursor
// resumes at the daemon's summary boundary (older turns render as a summary
// block); any other resume failure falls back to a full replay once.
func (t *Timeline) subscribe(ctx context.Context, conn *client.Conn) error {
	from := t.lastSeq.Load()
	err := conn.Call(ctx, "events.subscribe",
		api.EventsSubscribeParams{StreamID: t.stream, LastSeq: from}, &api.EventsSubscribeResult{})
	if err == nil || from == 0 {
		return err
	}
	resume := uint64(0)
	if e, ok := client.AsError(err); ok && e.Code == api.ErrCursorCompacted {
		var data api.CursorCompactedData
		if json.Unmarshal(e.Data, &data) == nil {
			resume = data.BoundarySeq
		}
		slog.Warn("tend-ui: timeline cursor compacted, resuming at boundary", "from_seq", from, "boundary", resume)
	} else {
		slog.Warn("tend-ui: timeline resume failed, replaying from start", "from_seq", from, "err", err)
	}
	t.lastSeq.Store(resume)
	return conn.Call(ctx, "events.subscribe",
		api.EventsSubscribeParams{StreamID: t.stream, LastSeq: resume}, &api.EventsSubscribeResult{})
}

// onNotify runs on the connection read goroutine. It advances the cursor and
// folds each event into the coalescer; subscription recovery is handed to the
// follow loop (a Call here would deadlock the read loop).
func (t *Timeline) onNotify(method string, params json.RawMessage, resub chan<- struct{}) {
	switch method {
	case "event.push":
		var p api.EventPushParams
		if err := json.Unmarshal(params, &p); err != nil {
			return
		}
		t.lastSeq.Store(p.Event.CursorSeq)
		t.mu.Lock()
		t.coal.handle(p.Event)
		t.mu.Unlock()
	case "event.subscription_closed":
		select {
		case resub <- struct{}{}:
		default:
		}
	}
}

// reset clears any buffered chunk text (a switched stream must not merge into a
// half-built block).
func (c *coalescer) reset() {
	c.buf.Reset()
	c.kind = ""
}
