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
	errNoSession  = errors.New("timeline: no session to follow")
)

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
}

// New returns a Timeline that pushes rendered blocks to hub.
func New(dir string, hub *bridge.Hub[string]) *Timeline {
	t := &Timeline{dir: dir, hub: hub}
	t.coal = newCoalescer(func(html string) { hub.Broadcast(html) })
	return t
}

// Run connects and follows until ctx is cancelled, reconnecting with a fixed
// backoff (which also re-picks the session if the current one is gone).
func (t *Timeline) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := t.follow(ctx); err != nil && ctx.Err() == nil && !errors.Is(err, errNoSession) {
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
	sess, ok, err := pickSession(ctx, conn, ws.WorkspaceID)
	if err != nil {
		return err
	}
	if !ok {
		return errNoSession
	}

	// A new session stream (or a daemon restart) invalidates the cursor and the
	// in-progress coalescer buffer.
	if sess.StreamID != t.stream || string(ws.DaemonEpoch) != t.epoch {
		t.mu.Lock()
		t.stream = sess.StreamID
		t.epoch = string(ws.DaemonEpoch)
		t.lastSeq.Store(0)
		t.coal.reset()
		t.mu.Unlock()
	}

	if err := t.subscribe(ctx, conn); err != nil {
		return err
	}
	slog.Info("tend-ui: following session timeline",
		"session", sess.SessionID, "stream", sess.StreamID, "from_seq", t.lastSeq.Load())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Done():
			return errConnClosed
		case <-resub:
			if err := t.subscribe(ctx, conn); err != nil {
				return err
			}
		}
	}
}

// subscribe subscribes to the current stream from the cursor, falling back to a
// full replay once if the resume fails (e.g. the cursor was compacted away).
func (t *Timeline) subscribe(ctx context.Context, conn *client.Conn) error {
	from := t.lastSeq.Load()
	err := conn.Call(ctx, "events.subscribe",
		api.EventsSubscribeParams{StreamID: t.stream, LastSeq: from}, &api.EventsSubscribeResult{})
	if err != nil && from != 0 {
		slog.Warn("tend-ui: timeline resume failed, replaying from start", "from_seq", from, "err", err)
		t.lastSeq.Store(0)
		err = conn.Call(ctx, "events.subscribe",
			api.EventsSubscribeParams{StreamID: t.stream, LastSeq: 0}, &api.EventsSubscribeResult{})
	}
	return err
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

// pickSession returns the session to follow for a workspace: a running one if
// present, else the first listed. ok is false when the workspace has no session.
func pickSession(ctx context.Context, conn *client.Conn, ws api.WorkspaceID) (api.SessionInfo, bool, error) {
	var res api.SessionListResult
	if err := conn.Call(ctx, "session.list", api.SessionListParams{WorkspaceID: ws}, &res); err != nil {
		return api.SessionInfo{}, false, err
	}
	if len(res.Sessions) == 0 {
		return api.SessionInfo{}, false, nil
	}
	for _, s := range res.Sessions {
		if s.Status == api.StatusRunning {
			return s, true, nil
		}
	}
	return res.Sessions[0], true, nil
}
