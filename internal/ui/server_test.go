package ui_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dusto/tend-ui/internal/ui"
)

func get(t *testing.T, u string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(b)
}

func TestServerServesShellAndAssets(t *testing.T) {
	s, err := ui.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	// The app shell renders at the tokenized base.
	resp, body := get(t, s.Base())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shell status = %d", resp.StatusCode)
	}
	for _, want := range []string{"<title>tend-ui</title>", `href="assets/app.css"`, "assets/htmx.min.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("shell missing %q", want)
		}
	}

	// An embedded asset is served under the token path.
	resp, css := get(t, s.Base()+"assets/app.css")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d", resp.StatusCode)
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
	resp, _ := get(t, u.String())
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("untokened root status = %d, want 404", resp.StatusCode)
	}
}
