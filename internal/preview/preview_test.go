package preview

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// get fetches url and returns status, headers, and body, closing the body itself
// so no *http.Response escapes (keeps bodyclose happy).
func get(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

func TestServesStoredArtifactWithSandboxHeaders(t *testing.T) {
	s := newTestServer(t)
	url, ok := s.URL("text/html; charset=utf-8", []byte("<h1>hi</h1>"))
	if !ok {
		t.Fatal("URL returned ok=false for real content")
	}
	if !strings.HasPrefix(url, s.Base()) {
		t.Errorf("preview url %q not under base %q", url, s.Base())
	}

	status, header, body := get(t, url)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if body != "<h1>hi</h1>" {
		t.Errorf("body = %q", body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	// The sandbox boundary: scripts run, but every exfil channel is locked —
	// external loads, network APIs, form posts, and document navigation — plus a
	// server-side sandbox directive.
	csp := header.Get("Content-Security-Policy")
	for _, want := range []string{
		"sandbox allow-scripts",
		"default-src 'none'",
		"connect-src 'none'",
		"form-action 'none'",
		"navigate-to 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
	if header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}

func TestUnknownArtifactIs404(t *testing.T) {
	s := newTestServer(t)
	status, _, _ := get(t, s.Base()+"a/deadbeef")
	if status != http.StatusNotFound {
		t.Errorf("unknown artifact status = %d, want 404", status)
	}
}

func TestNonArtifactRouteIs404(t *testing.T) {
	s := newTestServer(t)
	// No shell endpoints exist on this origin.
	status, _, _ := get(t, s.Base())
	if status != http.StatusNotFound {
		t.Errorf("root status = %d, want 404 (no shell here)", status)
	}
}

func TestURLRejectsEmpty(t *testing.T) {
	s := newTestServer(t)
	if _, ok := s.URL("text/html", nil); ok {
		t.Error("empty content should not get a preview url")
	}
	if _, ok := s.URL("", []byte("x")); ok {
		t.Error("empty media type should not get a preview url")
	}
}

func TestStoreEvictsOldest(t *testing.T) {
	st := newStore(2)
	a := st.put("text/plain", []byte("a"))
	b := st.put("text/plain", []byte("b"))
	c := st.put("text/plain", []byte("c")) // evicts a
	if _, ok := st.get(a); ok {
		t.Error("oldest artifact should have been evicted")
	}
	if _, ok := st.get(b); !ok {
		t.Error("b should still be present")
	}
	if _, ok := st.get(c); !ok {
		t.Error("c should be present")
	}
}
