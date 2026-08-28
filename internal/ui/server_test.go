package ui_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dusto/tend-ui/internal/ui"
)

// get fetches u and returns the status code and body, closing the body itself
// so callers hold no *http.Response (keeps bodyclose happy).
func get(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServerServesShellAndAssets(t *testing.T) {
	s, err := ui.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	// The app shell renders at the tokenized base.
	status, body := get(t, s.Base())
	if status != http.StatusOK {
		t.Fatalf("shell status = %d", status)
	}
	for _, want := range []string{"<title>tend-ui</title>", `href="assets/app.css"`, "assets/htmx.min.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("shell missing %q", want)
		}
	}

	// An embedded asset is served under the token path.
	status, css := get(t, s.Base()+"assets/app.css")
	if status != http.StatusOK {
		t.Fatalf("asset status = %d", status)
	}
	if !strings.Contains(css, "--teal") {
		t.Error("app.css did not come through the embed")
	}
}

func TestServerTokenGuardsRoot(t *testing.T) {
	s, err := ui.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	// A request without the per-run token in the path has no handler → 404.
	u, _ := url.Parse(s.Base())
	u.Path = "/"
	status, _ := get(t, u.String())
	if status != http.StatusNotFound {
		t.Errorf("untokened root status = %d, want 404", status)
	}
}
