// Package ui runs tend-ui's in-process loopback HTTP server — the adapter the
// webview loads. It binds 127.0.0.1 on a random port and gates every route with
// an unguessable per-run token in the URL path, so nothing else on the machine
// can reach it. The tend daemon stays a pure protocol daemon; all HTTP lives
// here (see the tend repo's ADR 0005). Later beads add the daemon client, the
// SSE event stream, and the session surfaces; this is the shell.
package ui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/internal/bridge"
	"github.com/dusto/tend-ui/internal/session"
	"github.com/dusto/tend-ui/web"
	"github.com/dusto/tend-ui/web/templates"
)

// sseHeartbeat is how often an SSE endpoint emits a comment ping to keep the
// connection (and any intermediary) from idling out.
const sseHeartbeat = 15 * time.Second

// SessionSurface is the timeline follower the session rail drives: it lists
// nothing itself, but the rail's Select switches which session the timeline
// follows, and Current marks the followed session in the rail. *timeline.Timeline
// satisfies it.
type SessionSurface interface {
	Select(id api.SessionID)
	Current() api.SessionID
	Usage() session.Usage
	ToolCalls() []session.ToolRef
}

// Lister returns the workspace's sessions for the rail. *session.Lister
// satisfies it.
type Lister interface {
	List(ctx context.Context) ([]api.SessionInfo, error)
}

// Server is the loopback UI server. Base is the tokenized URL the webview loads.
// It streams two hubs to the browser: workspace events (the activity feed) and
// pre-rendered session-timeline blocks; it also serves the session rail and
// routes selection to the timeline.
type Server struct {
	ln    net.Listener
	token string
	base  string
	evHub *bridge.Hub[api.Event]
	tlHub *bridge.Hub[string]
	list  Lister
	tl    SessionSurface
}

// newToken returns a fresh unguessable per-run token.
func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("ui: token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// NewServer starts the loopback server and returns it. evHub carries raw
// workspace events (the activity feed); tlHub carries pre-rendered session
// timeline blocks; list backs the session rail and tl receives its selection.
// The caller navigates the webview to Base() and must Close it.
func NewServer(evHub *bridge.Hub[api.Event], tlHub *bridge.Hub[string], list Lister, tl SessionSurface) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("ui: listen: %w", err)
	}
	token, err := newToken()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	prefix := "/" + token + "/"

	assets, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("ui: assets: %w", err)
	}

	s := &Server{
		ln:    ln,
		token: token,
		base:  fmt.Sprintf("http://%s%s", ln.Addr().String(), prefix),
		evHub: evHub,
		tlHub: tlHub,
		list:  list,
		tl:    tl,
	}

	mux := http.NewServeMux()
	// Static assets (htmx, Alpine, css, fonts). More specific than the index
	// pattern, so ServeMux routes "<token>/assets/..." here.
	mux.Handle(prefix+"assets/", http.StripPrefix(prefix+"assets/", http.FileServer(http.FS(assets))))
	// The live workspace event feed and the session timeline, each as SSE for
	// htmx's sse extension.
	mux.HandleFunc(prefix+"events", func(w http.ResponseWriter, r *http.Request) {
		serveSSE(w, r, s.evHub, "ev", func(ev api.Event) string { return renderEventLine(r, ev) })
	})
	mux.HandleFunc(prefix+"timeline", func(w http.ResponseWriter, r *http.Request) {
		serveSSE(w, r, s.tlHub, "item", func(html string) string { return html })
	})
	// The session rail (htmx-polled) and selection, and the focused-session header.
	mux.HandleFunc(prefix+"sessions", s.handleSessions)
	mux.HandleFunc(prefix+"select", s.handleSelect)
	mux.HandleFunc(prefix+"header", s.handleHeader)
	mux.HandleFunc(prefix+"jump", s.handleJump)
	// The app shell. Only the exact token root renders it; anything else under
	// the token that is not an asset is a 404.
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix {
			http.NotFound(w, r)
			return
		}
		sessions, _ := s.list.List(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = templates.Shell(prefix, sessions, s.tl.Current(), s.headerData(sessions), s.tl.ToolCalls()).Render(r.Context(), w)
	})

	go func() { _ = http.Serve(ln, mux) }()
	return s, nil
}

// handleSessions renders the session rail fragment (htmx polls this every few
// seconds to keep status live).
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.list.List(r.Context())
	if err != nil {
		// The daemon may be down between polls; render an empty rail rather than an
		// error so the poll keeps retrying.
		sessions = nil
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.SessionRail(sessions, s.tl.Current()).Render(r.Context(), w)
}

// handleJump renders the tool-call jump-index rail (htmx polls it to keep the
// list and per-call status live).
func (s *Server) handleJump(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.JumpIndex(s.tl.ToolCalls()).Render(r.Context(), w)
}

// handleHeader renders the focused-session header fragment (htmx polls it to
// keep provider/model/task/status and usage live).
func (s *Server) handleHeader(w http.ResponseWriter, r *http.Request) {
	sessions, _ := s.list.List(r.Context())
	h := s.headerData(sessions)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.SessionHeader(h.Session, h.Usage, h.Found).Render(r.Context(), w)
}

// headerData resolves the followed session (by tl.Current) within sessions and
// pairs it with the timeline's usage. Found is false when nothing is followed.
func (s *Server) headerData(sessions []api.SessionInfo) templates.SessionHeaderData {
	current := s.tl.Current()
	for _, sess := range sessions {
		if sess.SessionID == current {
			return templates.SessionHeaderData{Session: sess, Usage: s.tl.Usage(), Found: true}
		}
	}
	return templates.SessionHeaderData{}
}

// handleSelect switches the timeline to the posted session and returns a fresh,
// empty timeline panel that reconnects the SSE stream for it.
func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.FormValue("session")
	if id == "" {
		http.Error(w, "session is required", http.StatusBadRequest)
		return
	}
	s.tl.Select(api.SessionID(id))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.TimelinePanel().Render(r.Context(), w)
}

// serveSSE streams a hub to the browser as Server-Sent Events, one `event:
// <eventName>` frame per value with toHTML(v) as the payload. It runs until the
// client disconnects; a periodic comment keeps the connection alive.
func serveSSE[T any](w http.ResponseWriter, r *http.Request, hub *bridge.Hub[T], eventName string, toHTML func(T) string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	values, cancel := hub.Subscribe()
	defer cancel()

	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString(": connected\n\n") // open the stream immediately
	_ = bw.Flush()
	flusher.Flush()

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = bw.WriteString(": ping\n\n")
			if bw.Flush() != nil {
				return
			}
			flusher.Flush()
		case v, open := <-values:
			if !open {
				return
			}
			writeSSEFrame(bw, eventName, toHTML(v))
			if bw.Flush() != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// renderEventLine renders a workspace event to its EventLine HTML fragment.
func renderEventLine(r *http.Request, ev api.Event) string {
	var b strings.Builder
	_ = templates.EventLine(ev).Render(r.Context(), &b)
	return b.String()
}

// writeSSEFrame writes one SSE event with the given name and HTML payload. The
// payload may span lines; each becomes its own data: line (the client rejoins
// them with newlines).
func writeSSEFrame(w *bufio.Writer, event, payload string) {
	_, _ = w.WriteString("event: " + event + "\n")
	for _, line := range strings.Split(payload, "\n") {
		_, _ = w.WriteString("data: " + line + "\n")
	}
	_, _ = w.WriteString("\n")
}

// Base is the tokenized URL the webview should load.
func (s *Server) Base() string { return s.base }

// Close stops the server.
func (s *Server) Close() error { return s.ln.Close() }
