// Package preview serves agent-authored artifact content in a sandbox isolated
// from the privileged shell. Per ADR 0005 the shell (its owned HTML/CSS/JS, which
// may reach the daemon) and untrusted agent content must not share an origin, so
// this is a SECOND loopback HTTP server on its own port — its own browser origin —
// with an unguessable per-run token. It serves ONLY stored artifacts under a
// strict Content-Security-Policy; it has no daemon socket and none of the shell's
// endpoints. The shell embeds each artifact in a sandboxed iframe pointing here,
// so rich content (HTML, SVG; mermaid later) renders with no privileged access
// and no path back to the shell or the daemon.
package preview

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// contentSecurityPolicy constrains what served artifact content may do. The
// content renders and may run its OWN inline scripts (needed for interactive
// agent HTML and, later, mermaid), but default-src 'none' blocks loading any
// external resource and connect-src 'none' blocks fetch/XHR/WebSocket — so agent
// content cannot phone home or reach the shell/daemon. Combined with the iframe's
// sandbox attribute (an opaque origin, no same-origin access), this is the
// preview boundary.
const contentSecurityPolicy = "default-src 'none'; " +
	"img-src data: blob:; media-src data: blob:; " +
	"style-src 'unsafe-inline'; font-src data:; " +
	"script-src 'unsafe-inline'; connect-src 'none'; " +
	"form-action 'none'; base-uri 'none'"

// maxArtifacts caps how many artifacts the per-run store retains, evicting the
// oldest, so a long session cannot grow the store without bound. The server is
// torn down with the app; this bounds a single run.
const maxArtifacts = 256

// Server is the loopback preview sandbox. Safe for concurrent use.
type Server struct {
	ln    net.Listener
	token string
	base  string
	store *store
}

// NewServer starts the preview server on a fresh loopback port with an
// unguessable per-run token and returns it. Close stops it.
func NewServer() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("preview: listen: %w", err)
	}
	token, err := newToken()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	prefix := "/" + token + "/"
	s := &Server{
		ln:    ln,
		token: token,
		base:  fmt.Sprintf("http://%s%s", ln.Addr().String(), prefix),
		store: newStore(maxArtifacts),
	}
	mux := http.NewServeMux()
	// The ONLY route: a stored artifact. Everything else 404s — there are no shell
	// endpoints and no daemon access on this origin.
	mux.HandleFunc(prefix+"a/", s.serveArtifact)
	go func() { _ = http.Serve(ln, mux) }()
	return s, nil
}

// Base is the tokenized preview URL prefix (…/<token>/).
func (s *Server) Base() string { return s.base }

// URL stores content of mediaType and returns the sandboxed URL that renders it,
// or ok=false when there is nothing to preview (empty content or media type).
func (s *Server) URL(mediaType string, content []byte) (url string, ok bool) {
	if mediaType == "" || len(content) == 0 {
		return "", false
	}
	id := s.store.put(mediaType, content)
	return s.base + "a/" + id, true
}

func (s *Server) serveArtifact(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/"+s.token+"/a/")
	art, ok := s.store.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", art.mediaType)
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	// Do NOT set X-Frame-Options so the shell (a different loopback origin) can
	// frame this; the CSP above is what constrains the content.
	_, _ = w.Write(art.content)
}

// Close stops the server.
func (s *Server) Close() error { return s.ln.Close() }

// newToken returns a fresh unguessable per-run token.
func newToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("preview: token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// artifact is one stored preview payload.
type artifact struct {
	mediaType string
	content   []byte
}

// store holds artifacts by unguessable id with a bounded, oldest-first eviction.
type store struct {
	mu    sync.Mutex
	max   int
	items map[string]artifact
	order []string // insertion order, for eviction
}

func newStore(max int) *store {
	return &store{max: max, items: make(map[string]artifact)}
}

func (s *store) put(mediaType string, content []byte) string {
	id := mustID()
	// Copy the content so a caller's buffer reuse can't mutate what we serve.
	buf := make([]byte, len(content))
	copy(buf, content)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = artifact{mediaType: mediaType, content: buf}
	s.order = append(s.order, id)
	for len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.items, oldest)
	}
	return id
}

func (s *store) get(id string) (artifact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.items[id]
	return a, ok
}

func mustID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
