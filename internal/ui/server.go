// Package ui runs tend-ui's in-process loopback HTTP server — the adapter the
// webview loads. It binds 127.0.0.1 on a random port and gates every route with
// an unguessable per-run token in the URL path, so nothing else on the machine
// can reach it. The tend daemon stays a pure protocol daemon; all HTTP lives
// here (see the tend repo's ADR 0005). Later beads add the daemon client, the
// SSE event stream, and the session surfaces; this is the shell.
package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"

	"github.com/dusto/tend-ui/web"
	"github.com/dusto/tend-ui/web/templates"
)

// Server is the loopback UI server. Base is the tokenized URL the webview loads.
type Server struct {
	ln    net.Listener
	token string
	base  string
}

// newToken returns a fresh unguessable per-run token.
func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("ui: token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// NewServer starts the loopback server and returns it. The caller navigates the
// webview to Base() and must Close it on shutdown.
func NewServer() (*Server, error) {
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

	mux := http.NewServeMux()
	// Static assets (htmx, Alpine, css, fonts). More specific than the index
	// pattern, so ServeMux routes "<token>/assets/..." here.
	mux.Handle(prefix+"assets/", http.StripPrefix(prefix+"assets/", http.FileServer(http.FS(assets))))
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

	s := &Server{
		ln:    ln,
		token: token,
		base:  fmt.Sprintf("http://%s%s", ln.Addr().String(), prefix),
	}
	go func() { _ = http.Serve(ln, mux) }()
	return s, nil
}

// Base is the tokenized URL the webview should load.
func (s *Server) Base() string { return s.base }

// Close stops the server.
func (s *Server) Close() error { return s.ln.Close() }
