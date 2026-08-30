// Package control gives tend-ui the ability to act on the daemon, not just
// observe it: answer approvals, prompt a session, and cancel/stop it. It holds a
// single prompt-capable connection (the read paths register as observers, which
// cannot resolve approvals), lazily (re)dialed. Every mutation still goes through
// the daemon's own gates — this is the client side of the interactive console
// (tend-du1.12).
package control

import (
	"context"
	"log/slog"
	"sync"

	"github.com/dusto/tend/api"
	"github.com/dusto/tend/client"
)

const minPluginToDaemon = "0.8.0"

// Controller performs daemon mutations on behalf of the UI over one
// prompt-capable connection. Safe for concurrent use.
type Controller struct {
	dir    string
	socket string // "" = the daemon's default socket

	mu   sync.Mutex
	conn *client.Conn
}

// New returns a Controller for the workspace containing dir.
func New(dir string) *Controller { return &Controller{dir: dir} }

// NewWithSocket is New against an explicit socket path (tests).
func NewWithSocket(dir, socket string) *Controller {
	return &Controller{dir: dir, socket: socket}
}

// conn returns a live prompt-capable connection, dialing (and opening the
// workspace) on first use or after the previous one dropped.
func (c *Controller) ensure(ctx context.Context) (*client.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		select {
		case <-c.conn.Done(): // dropped; redial below
			c.conn = nil
		default:
			return c.conn, nil
		}
	}
	conn, err := client.Dial(ctx, client.Options{
		Socket:            c.socket,
		ClientID:          "tend-ui-control",
		MinPluginToDaemon: minPluginToDaemon,
		// Prompt-capable so it can resolve approvals (approval.respond rejects a
		// non-prompt-capable client). Observer role: it drives sessions and answers
		// prompts but serves no editor-local operations.
		PromptCapable: true,
	})
	if err != nil {
		return nil, err
	}
	if err := conn.Call(ctx, "workspace.open", api.WorkspaceOpenParams{Dir: c.dir}, &api.WorkspaceInfo{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.conn = conn
	return conn, nil
}

// Approvals returns the pending approvals for a session (empty sessionID lists
// all in the workspace). Each carries its detail (a file edit's diff, a pane
// run's command) for the UI to render.
func (c *Controller) Approvals(ctx context.Context, sessionID api.SessionID) ([]api.ApprovalSummary, error) {
	conn, err := c.ensure(ctx)
	if err != nil {
		return nil, err
	}
	var res api.ApprovalListResult
	if err := conn.Call(ctx, "approval.list", api.ApprovalListParams{SessionID: sessionID}, &res); err != nil {
		return nil, err
	}
	return res.Approvals, nil
}

// Respond resolves a pending approval (approve or deny).
func (c *Controller) Respond(ctx context.Context, id api.ApprovalID, approved bool) error {
	conn, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	return conn.Call(ctx, "approval.respond",
		api.ApprovalRespondParams{ApprovalID: id, Approved: approved}, &api.ApprovalRespondResult{})
}

// Prompt runs a prompt turn on a session. agent.prompt blocks until the turn
// ends, so it runs in the background (its output streams to the session timeline)
// and Prompt returns as soon as the turn is dispatched. The returned error only
// reports a failure to dispatch (no connection); turn errors surface as session
// events.
func (c *Controller) Prompt(ctx context.Context, sessionID api.SessionID, text string) error {
	conn, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	go func() {
		// Detached from the request: the turn outlives the HTTP handler.
		if err := conn.Call(context.Background(), "agent.prompt",
			api.AgentPromptParams{SessionID: sessionID, Text: text}, &api.AgentPromptResult{}); err != nil {
			slog.Warn("tend-ui: prompt turn failed", "session", sessionID, "err", err)
		}
	}()
	return nil
}

// Cancel returns the session's in-flight turn to idle.
func (c *Controller) Cancel(ctx context.Context, sessionID api.SessionID) error {
	conn, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	return conn.Call(ctx, "agent.cancel", api.AgentCancelParams{SessionID: sessionID}, nil)
}

// Stop ends a session and releases its provider process.
func (c *Controller) Stop(ctx context.Context, sessionID api.SessionID) error {
	conn, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	return conn.Call(ctx, "agent.stop", api.AgentStopParams{SessionID: sessionID}, nil)
}

// Close releases the connection.
func (c *Controller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}
