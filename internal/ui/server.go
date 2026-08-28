// Package ui runs tend-ui's in-process loopback HTTP server — the adapter the
// webview loads. It binds 127.0.0.1 on a random port and gates every route with
// an unguessable per-run token in the URL path, so nothing else on the machine
// can reach it. The tend daemon stays a pure protocol daemon; all HTTP lives
// here (see the tend repo's ADR 0005). Later beads add the daemon client, the
// SSE event stream, and the session surfaces; this is the shell.
package ui

import (
	"bufio"
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
	"github.com/dusto/tend-ui/web"
	"github.com/dusto/tend-ui/web/templates"
)

// sseHeartbeat is how often an SSE endpoint emits a comment ping to keep the
// connection (and any intermediary) from idling out.
const sseHeartbeat = 15 * time.Second

// Server is the loopback UI server. Base is the tokenized URL the webview loads.
// It streams two hubs to the browser: workspace events (the activity feed) and
// pre-rendered session-timeline blocks.
type Server struct {
	ln    net.Listener
	token string
	base  string
	evHub *bridge.Hub[api.Event]
	tlHub *bridge.Hub[string]
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
// timeline blocks. The caller navigates the webview to Base() and must Close it.
func NewServer(evHub *bridge.Hub[api.Event], tlHub *bridge.Hub[string]) (*Server, error) {
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
	// The app shell. Only the exact token root renders it; anything else under
	// the token that is not an asset is a 404.
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = templates.Shell(prefix).Render(r.Context(), w)
	})

	go func() { _ = http.Serve(ln, mux) }()
	return s, nil
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
